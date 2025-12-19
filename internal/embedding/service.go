// Package embedding provides text embedding generation with swappable models.
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

// EmbeddingDim is the dimension of embeddings produced by the current model.
// Both all-MiniLM-L6-v2 and bge-small-en-v1.5 produce 384-dimensional embeddings.
const EmbeddingDim = 384

// Model version constants
const (
	// BGEModelVersion is the version string for bge-small-en-v1.5
	BGEModelVersion = "bge-v1.5"
	// BGEModelName is the human-readable name for bge-small-en-v1.5
	BGEModelName = "bge-small-en-v1.5"
	// DefaultModelVersion is the default model to use
	DefaultModelVersion = BGEModelVersion
)

// MaxSequenceLength is the maximum token sequence length for the model.
const MaxSequenceLength = 512

// bgeONNXConfig defines the ONNX configuration for BGE models.
// BGE outputs last_hidden_state and requires mean pooling.
var bgeONNXConfig = ONNXConfig{
	InputNames:  []string{"input_ids", "attention_mask", "token_type_ids"},
	OutputNames: []string{"last_hidden_state"},
	Pooling:     PoolingMean,
	HiddenSize:  EmbeddingDim,
}

// bgeModel is the ONNX-based embedding model implementation.
// Currently supports bge-small-en-v1.5 (previously all-MiniLM-L6-v2).
type bgeModel struct {
	tk      *tokenizer.Tokenizer
	session *ort.DynamicAdvancedSession
	mu      sync.Mutex
	libDir  string     // temp directory containing extracted libraries
	config  ONNXConfig // ONNX configuration for this model
}

// Compile-time check that bgeModel implements EmbeddingModel
var _ EmbeddingModel = (*bgeModel)(nil)

// Compile-time check that bgeModel implements ONNXConfigurer
var _ ONNXConfigurer = (*bgeModel)(nil)

// newBGEModel creates a new BGE embedding model using bundled ONNX runtime and model.
func newBGEModel() (EmbeddingModel, error) {
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

	// Create ONNX session using model-specific configuration
	config := bgeONNXConfig
	session, err := ort.NewDynamicAdvancedSessionWithONNXData(modelData, config.InputNames, config.OutputNames, nil)
	if err != nil {
		return nil, fmt.Errorf("create ONNX session: %w", err)
	}

	return &bgeModel{
		tk:      tk,
		session: session,
		libDir:  libDir,
		config:  config,
	}, nil
}

// ONNXConfig returns the model's ONNX configuration.
func (m *bgeModel) ONNXConfig() ONNXConfig {
	return m.config
}

// Name returns the human-readable model name.
func (m *bgeModel) Name() string {
	return BGEModelName
}

// Version returns the short version string for storage.
func (m *bgeModel) Version() string {
	return BGEModelVersion
}

// Dimensions returns the embedding vector size.
func (m *bgeModel) Dimensions() int {
	return EmbeddingDim
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
func (m *bgeModel) Embed(text string) ([]float32, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if text == "" {
		return make([]float32, EmbeddingDim), nil
	}

	results, err := m.computeBatch([]string{text})
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
func (m *bgeModel) EmbedBatch(texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

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
	embeddings, err := m.computeBatch(nonEmpty)
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
func (m *bgeModel) computeBatch(sentences []string) ([][]float32, error) {
	if len(sentences) == 0 {
		return nil, nil
	}

	// Tokenize all sentences
	inputBatch := make([]tokenizer.EncodeInput, len(sentences))
	for i, sent := range sentences {
		inputBatch[i] = tokenizer.NewSingleEncodeInput(tokenizer.NewRawInputSequence(sent))
	}

	encodings, err := m.tk.EncodeBatch(inputBatch, true)
	if err != nil {
		return nil, fmt.Errorf("tokenize: %w", err)
	}

	batchSize := len(encodings)
	hiddenSize := m.config.HiddenSize

	// Find max sequence length across all encodings (tokenizer may not pad uniformly)
	// Also enforce MaxSequenceLength to prevent model errors
	seqLength := 0
	for _, enc := range encodings {
		if len(enc.Ids) > seqLength {
			seqLength = len(enc.Ids)
		}
	}
	// Truncate to max model sequence length
	if seqLength > MaxSequenceLength {
		seqLength = MaxSequenceLength
	}

	inputShape := ort.NewShape(int64(batchSize), int64(seqLength))

	// Create input tensors (pre-filled with zeros for padding)
	inputIdsData := make([]int64, batchSize*seqLength)
	attentionMaskData := make([]int64, batchSize*seqLength)
	tokenTypeIdsData := make([]int64, batchSize*seqLength)

	for b := 0; b < batchSize; b++ {
		// Copy actual token data (rest remains 0 as padding)
		// Truncate to seqLength to handle long inputs
		copyLen := len(encodings[b].Ids)
		if copyLen > seqLength {
			copyLen = seqLength
		}
		for i := 0; i < copyLen; i++ {
			inputIdsData[b*seqLength+i] = int64(encodings[b].Ids[i])
		}
		copyLen = len(encodings[b].AttentionMask)
		if copyLen > seqLength {
			copyLen = seqLength
		}
		for i := 0; i < copyLen; i++ {
			attentionMaskData[b*seqLength+i] = int64(encodings[b].AttentionMask[i])
		}
		copyLen = len(encodings[b].TypeIds)
		if copyLen > seqLength {
			copyLen = seqLength
		}
		for i := 0; i < copyLen; i++ {
			tokenTypeIdsData[b*seqLength+i] = int64(encodings[b].TypeIds[i])
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

	// Create output tensor based on pooling strategy
	var outputShape ort.Shape

	switch m.config.Pooling {
	case PoolingNone:
		// Direct sentence embedding output: [batch, hidden]
		outputShape = ort.NewShape(int64(batchSize), int64(hiddenSize))
	case PoolingMean, PoolingCLS:
		// Token-level output: [batch, seq_len, hidden]
		outputShape = ort.NewShape(int64(batchSize), int64(seqLength), int64(hiddenSize))
	default:
		outputShape = ort.NewShape(int64(batchSize), int64(hiddenSize))
	}

	outputTensor, err := ort.NewEmptyTensor[float32](outputShape)
	if err != nil {
		return nil, fmt.Errorf("create output tensor: %w", err)
	}
	defer outputTensor.Destroy()

	// Run inference
	inputTensors := []ort.Value{inputIdsTensor, attentionMaskTensor, tokenTypeIdsTensor}
	outputTensors := []ort.Value{outputTensor}

	if err := m.session.Run(inputTensors, outputTensors); err != nil {
		return nil, fmt.Errorf("run inference: %w", err)
	}

	// Extract and pool results based on strategy
	flatOutput := outputTensor.GetData()

	switch m.config.Pooling {
	case PoolingNone:
		// Direct output, no pooling needed
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

	case PoolingMean:
		// Mean pooling over tokens (weighted by attention mask)
		return meanPooling(flatOutput, attentionMaskData, batchSize, seqLength, hiddenSize), nil

	case PoolingCLS:
		// CLS token pooling (first token of each sequence)
		return clsPooling(flatOutput, batchSize, seqLength, hiddenSize), nil

	default:
		return nil, fmt.Errorf("unknown pooling strategy: %s", m.config.Pooling)
	}
}

// meanPooling applies mean pooling over token embeddings, weighted by attention mask.
// Input shape: [batch, seq_len, hidden], attention mask: [batch, seq_len]
// Output shape: [batch, hidden]
func meanPooling(embeddings []float32, attentionMask []int64, batchSize, seqLen, hiddenSize int) [][]float32 {
	results := make([][]float32, batchSize)

	for b := 0; b < batchSize; b++ {
		result := make([]float32, hiddenSize)
		var maskSum float32

		// Sum embeddings weighted by attention mask
		for s := 0; s < seqLen; s++ {
			maskVal := float32(attentionMask[b*seqLen+s])
			maskSum += maskVal

			if maskVal > 0 {
				embOffset := (b*seqLen + s) * hiddenSize
				for h := 0; h < hiddenSize; h++ {
					result[h] += embeddings[embOffset+h] * maskVal
				}
			}
		}

		// Normalize by mask sum (avoid division by zero)
		if maskSum > 0 {
			for h := 0; h < hiddenSize; h++ {
				result[h] /= maskSum
			}
		}

		results[b] = result
	}

	return results
}

// clsPooling extracts the [CLS] token embedding (first token).
// Input shape: [batch, seq_len, hidden]
// Output shape: [batch, hidden]
func clsPooling(embeddings []float32, batchSize, seqLen, hiddenSize int) [][]float32 {
	results := make([][]float32, batchSize)

	for b := 0; b < batchSize; b++ {
		result := make([]float32, hiddenSize)
		// CLS token is at position 0
		embOffset := b * seqLen * hiddenSize
		copy(result, embeddings[embOffset:embOffset+hiddenSize])
		results[b] = result
	}

	return results
}

// Close releases model resources.
func (m *bgeModel) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []error

	if m.session != nil {
		if err := m.session.Destroy(); err != nil {
			errs = append(errs, fmt.Errorf("destroy session: %w", err))
		}
		m.session = nil
	}

	if err := ort.DestroyEnvironment(); err != nil {
		errs = append(errs, fmt.Errorf("destroy environment: %w", err))
	}

	// Optionally clean up extracted library (leave for caching)
	// os.RemoveAll(m.libDir)

	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// Register the BGE model with the default registry at init time
func init() {
	RegisterModel(ModelMetadata{
		Name:        BGEModelName,
		Version:     BGEModelVersion,
		Dimensions:  EmbeddingDim,
		Description: "High-quality semantic search model",
		Default:     true,
	}, newBGEModel)
}

// Service provides thread-safe text embedding generation with model abstraction.
type Service struct {
	model EmbeddingModel
}

// NewService creates a new embedding service using the default model.
func NewService() (*Service, error) {
	return NewServiceWithModel(DefaultModelVersion)
}

// NewServiceWithModel creates a new embedding service using the specified model.
func NewServiceWithModel(version string) (*Service, error) {
	if version == "" {
		version = DefaultModelVersion
	}

	model, err := GetModel(version)
	if err != nil {
		return nil, fmt.Errorf("get model %s: %w", version, err)
	}

	return &Service{model: model}, nil
}

// Name returns the human-readable model name.
func (s *Service) Name() string {
	return s.model.Name()
}

// Version returns the short version string for storage.
func (s *Service) Version() string {
	return s.model.Version()
}

// Dimensions returns the embedding vector size.
func (s *Service) Dimensions() int {
	return s.model.Dimensions()
}

// Embed generates an embedding for a single text.
func (s *Service) Embed(text string) ([]float32, error) {
	return s.model.Embed(text)
}

// EmbedBatch generates embeddings for multiple texts.
func (s *Service) EmbedBatch(texts []string) ([][]float32, error) {
	return s.model.EmbedBatch(texts)
}

// Close releases model resources.
func (s *Service) Close() error {
	return s.model.Close()
}
