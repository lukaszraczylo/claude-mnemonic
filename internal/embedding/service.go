// Package embedding provides text embedding generation using all-MiniLM-L6-v2.
package embedding

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/sugarme/tokenizer"
	"github.com/sugarme/tokenizer/pretrained"
	ort "github.com/yalue/onnxruntime_go"
)

// EmbeddingDim is the dimension of embeddings produced by all-MiniLM-L6-v2.
const EmbeddingDim = 384

// Service provides thread-safe text embedding generation.
type Service struct {
	tk      *tokenizer.Tokenizer
	session *ort.DynamicAdvancedSession
	mu      sync.Mutex
	libDir  string // temp directory containing extracted libraries
}

// NewService creates a new embedding service using bundled ONNX runtime and model.
func NewService() (*Service, error) {
	// Extract ONNX runtime library to temp directory
	libDir, err := extractONNXLibrary()
	if err != nil {
		return nil, fmt.Errorf("extract ONNX library: %w", err)
	}

	// Set the library path
	libPath := filepath.Join(libDir, onnxRuntimeLibName)
	ort.SetSharedLibraryPath(libPath)

	// Initialize ONNX runtime
	if err := ort.InitializeEnvironment(); err != nil {
		return nil, fmt.Errorf("initialize ONNX runtime: %w", err)
	}

	// Load tokenizer from embedded data
	tk, err := pretrained.FromReader(bytes.NewReader(tokenizerData))
	if err != nil {
		return nil, fmt.Errorf("load tokenizer: %w", err)
	}

	// Create ONNX session with embedded model
	inputNames := []string{"input_ids", "attention_mask", "token_type_ids"}
	outputNames := []string{"sentence_embedding"}

	session, err := ort.NewDynamicAdvancedSessionWithONNXData(modelData, inputNames, outputNames, nil)
	if err != nil {
		return nil, fmt.Errorf("create ONNX session: %w", err)
	}

	return &Service{
		tk:      tk,
		session: session,
		libDir:  libDir,
	}, nil
}

// extractONNXLibrary extracts the embedded ONNX runtime library to a temp directory.
// Uses content hash to avoid re-extracting if already present.
func extractONNXLibrary() (string, error) {
	// Create a hash of the library content for cache key
	hash := sha256.Sum256(onnxRuntimeLib)
	hashStr := hex.EncodeToString(hash[:8]) // Use first 8 bytes

	// Create cache directory
	cacheDir := filepath.Join(os.TempDir(), "claude-mnemonic-onnx", hashStr)
	libPath := filepath.Join(cacheDir, onnxRuntimeLibName)

	// Check if already extracted
	if _, err := os.Stat(libPath); err == nil {
		return cacheDir, nil
	}

	// Create directory
	// #nosec G301 -- Cache directory needs 0755 for user access
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return "", fmt.Errorf("create cache dir: %w", err)
	}

	// Write main library
	// #nosec G306 -- Shared library needs executable permission (0755) for dynamic linker
	if err := os.WriteFile(libPath, onnxRuntimeLib, 0755); err != nil {
		return "", fmt.Errorf("write library: %w", err)
	}

	// Write providers library if present (Linux only)
	if len(onnxRuntimeProvidersLib) > 0 && onnxRuntimeProvidersLibName != "" {
		providersPath := filepath.Join(cacheDir, onnxRuntimeProvidersLibName)
		// #nosec G306 -- Shared library needs executable permission (0755) for dynamic linker
		if err := os.WriteFile(providersPath, onnxRuntimeProvidersLib, 0755); err != nil {
			return "", fmt.Errorf("write providers library: %w", err)
		}
	}

	return cacheDir, nil
}

// Embed generates an embedding for a single text.
// Returns a 384-dimensional float32 vector.
func (s *Service) Embed(text string) ([]float32, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if text == "" {
		return make([]float32, EmbeddingDim), nil
	}

	results, err := s.computeBatch([]string{text})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return make([]float32, EmbeddingDim), nil
	}
	return results[0], nil
}

// EmbedBatch generates embeddings for multiple texts.
// Returns slice of 384-dimensional float32 vectors.
func (s *Service) EmbedBatch(texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Filter out empty texts and track indices
	nonEmpty := make([]string, 0, len(texts))
	indices := make([]int, 0, len(texts))
	for i, t := range texts {
		if t != "" {
			nonEmpty = append(nonEmpty, t)
			indices = append(indices, i)
		}
	}

	// If all texts are empty, return zero vectors
	if len(nonEmpty) == 0 {
		results := make([][]float32, len(texts))
		for i := range results {
			results[i] = make([]float32, EmbeddingDim)
		}
		return results, nil
	}

	// Compute embeddings for non-empty texts
	embeddings, err := s.computeBatch(nonEmpty)
	if err != nil {
		return nil, fmt.Errorf("compute batch embeddings: %w", err)
	}

	// Build result with zero vectors for empty texts
	results := make([][]float32, len(texts))
	for i := range results {
		results[i] = make([]float32, EmbeddingDim)
	}
	for i, idx := range indices {
		results[idx] = embeddings[i]
	}

	return results, nil
}

// computeBatch runs inference on a batch of texts. Must be called with lock held.
func (s *Service) computeBatch(sentences []string) ([][]float32, error) {
	if len(sentences) == 0 {
		return nil, nil
	}

	// Tokenize all sentences
	inputBatch := make([]tokenizer.EncodeInput, len(sentences))
	for i, sent := range sentences {
		inputBatch[i] = tokenizer.NewSingleEncodeInput(tokenizer.NewRawInputSequence(sent))
	}

	encodings, err := s.tk.EncodeBatch(inputBatch, true)
	if err != nil {
		return nil, fmt.Errorf("tokenize: %w", err)
	}

	batchSize := len(encodings)
	seqLength := len(encodings[0].Ids)
	hiddenSize := EmbeddingDim

	inputShape := ort.NewShape(int64(batchSize), int64(seqLength))

	// Create input tensors
	inputIdsData := make([]int64, batchSize*seqLength)
	attentionMaskData := make([]int64, batchSize*seqLength)
	tokenTypeIdsData := make([]int64, batchSize*seqLength)

	for b := 0; b < batchSize; b++ {
		for i, id := range encodings[b].Ids {
			inputIdsData[b*seqLength+i] = int64(id)
		}
		for i, mask := range encodings[b].AttentionMask {
			attentionMaskData[b*seqLength+i] = int64(mask)
		}
		for i, typeId := range encodings[b].TypeIds {
			tokenTypeIdsData[b*seqLength+i] = int64(typeId)
		}
	}

	inputIdsTensor, err := ort.NewTensor(inputShape, inputIdsData)
	if err != nil {
		return nil, fmt.Errorf("create input_ids tensor: %w", err)
	}
	defer inputIdsTensor.Destroy()

	attentionMaskTensor, err := ort.NewTensor(inputShape, attentionMaskData)
	if err != nil {
		return nil, fmt.Errorf("create attention_mask tensor: %w", err)
	}
	defer attentionMaskTensor.Destroy()

	tokenTypeIdsTensor, err := ort.NewTensor(inputShape, tokenTypeIdsData)
	if err != nil {
		return nil, fmt.Errorf("create token_type_ids tensor: %w", err)
	}
	defer tokenTypeIdsTensor.Destroy()

	sentenceOutputShape := ort.NewShape(int64(batchSize), int64(hiddenSize))
	sentenceOutputTensor, err := ort.NewEmptyTensor[float32](sentenceOutputShape)
	if err != nil {
		return nil, fmt.Errorf("create output tensor: %w", err)
	}
	defer sentenceOutputTensor.Destroy()

	// Run inference
	inputTensors := []ort.Value{inputIdsTensor, attentionMaskTensor, tokenTypeIdsTensor}
	outputTensors := []ort.Value{sentenceOutputTensor}

	if err := s.session.Run(inputTensors, outputTensors); err != nil {
		return nil, fmt.Errorf("run inference: %w", err)
	}

	// Extract results
	flatOutput := sentenceOutputTensor.GetData()
	expectedSize := batchSize * hiddenSize
	if len(flatOutput) != expectedSize {
		return nil, fmt.Errorf("unexpected output size: got %d, expected %d", len(flatOutput), expectedSize)
	}

	results := make([][]float32, batchSize)
	for i := 0; i < batchSize; i++ {
		start := i * hiddenSize
		end := start + hiddenSize
		results[i] = make([]float32, hiddenSize)
		copy(results[i], flatOutput[start:end])
	}

	return results, nil
}

// Close releases model resources.
func (s *Service) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var errs []error

	if s.session != nil {
		if err := s.session.Destroy(); err != nil {
			errs = append(errs, fmt.Errorf("destroy session: %w", err))
		}
		s.session = nil
	}

	if err := ort.DestroyEnvironment(); err != nil {
		errs = append(errs, fmt.Errorf("destroy environment: %w", err))
	}

	// Optionally clean up extracted library (leave for caching)
	// os.RemoveAll(s.libDir)

	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}
