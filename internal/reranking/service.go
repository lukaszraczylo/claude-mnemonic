// Package reranking provides cross-encoder reranking for search results.
// Uses MS-MARCO MiniLM L6 v2 cross-encoder model for relevance scoring.
package reranking

import (
	"bytes"
	"fmt"
	"math"
	"sort"
	"sync"

	"github.com/rs/zerolog/log"
	"github.com/sugarme/tokenizer"
	"github.com/sugarme/tokenizer/pretrained"
	ort "github.com/yalue/onnxruntime_go"
)

const (
	// ModelName is the human-readable name for the cross-encoder model
	ModelName = "ms-marco-MiniLM-L6-v2"
	// ModelVersion is the short version string for identification
	ModelVersion = "msmarco-v2"
	// MaxSequenceLength is the maximum combined query+document token length
	MaxSequenceLength = 512
	// DefaultCandidateLimit is the default number of candidates to rerank
	DefaultCandidateLimit = 100
	// DefaultResultLimit is the default number of results to return after reranking
	DefaultResultLimit = 10
)

// Candidate represents a search result candidate for reranking.
type Candidate struct {
	ID         string             // Document ID
	Content    string             // Document text content for scoring
	Score      float64            // Original bi-encoder similarity score
	Metadata   map[string]any     // Preserved metadata
	RerankInfo map[string]float64 // Reranking debug info (optional)
}

// RerankResult represents a reranked search result.
type RerankResult struct {
	ID              string         // Document ID
	Content         string         // Document text content
	OriginalScore   float64        // Original bi-encoder score
	RerankScore     float64        // Cross-encoder relevance score
	CombinedScore   float64        // Weighted combination of scores
	Metadata        map[string]any // Preserved metadata
	OriginalRank    int            // Position before reranking (1-indexed)
	RerankRank      int            // Position after reranking (1-indexed)
	RankImprovement int            // How much the rank improved (positive = moved up)
}

// Service provides cross-encoder reranking functionality.
type Service struct {
	tk      *tokenizer.Tokenizer
	session *ort.DynamicAdvancedSession
	mu      sync.Mutex

	// Weight for combining scores: combined = alpha*rerank + (1-alpha)*original
	// Default 0.7 favors cross-encoder score
	Alpha float64
}

// Config holds configuration for the reranking service.
type Config struct {
	// Alpha is the weight for combining scores (0.0-1.0)
	// Higher values favor cross-encoder scores, lower values favor bi-encoder scores
	Alpha float64
}

// DefaultConfig returns sensible defaults for reranking.
func DefaultConfig() Config {
	return Config{
		Alpha: 0.7, // Favor cross-encoder by default
	}
}

// NewService creates a new cross-encoder reranking service.
// Note: ONNX runtime must be initialized before calling this (via embedding.NewService).
func NewService(cfg Config) (*Service, error) {
	// Load tokenizer from embedded data
	tk, err := pretrained.FromReader(bytes.NewReader(crossEncoderTokenizerData))
	if err != nil {
		return nil, fmt.Errorf("load cross-encoder tokenizer: %w", err)
	}

	// Configure tokenizer for sequence classification (pairs)
	tk.WithTruncation(&tokenizer.TruncationParams{
		MaxLength: MaxSequenceLength,
		Strategy:  tokenizer.LongestFirst,
		Stride:    0,
	})

	// Cross-encoder outputs a single logit for relevance scoring
	inputNames := []string{"input_ids", "attention_mask", "token_type_ids"}
	outputNames := []string{"logits"}

	session, err := ort.NewDynamicAdvancedSessionWithONNXData(
		crossEncoderModelData,
		inputNames,
		outputNames,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("create cross-encoder ONNX session: %w", err)
	}

	alpha := cfg.Alpha
	if alpha <= 0 || alpha > 1 {
		alpha = 0.7
	}

	return &Service{
		tk:      tk,
		session: session,
		Alpha:   alpha,
	}, nil
}

// Rerank reranks candidates using the cross-encoder model.
// Takes a query and list of candidates, returns reranked results.
func (s *Service) Rerank(query string, candidates []Candidate, limit int) ([]RerankResult, error) {
	if len(candidates) == 0 {
		return nil, nil
	}

	if limit <= 0 {
		limit = DefaultResultLimit
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Score all query-document pairs
	scores, err := s.scoreAll(query, candidates)
	if err != nil {
		return nil, fmt.Errorf("score candidates: %w", err)
	}

	// Build results with combined scores
	results := make([]RerankResult, len(candidates))
	for i, c := range candidates {
		// Normalize cross-encoder score to 0-1 range using sigmoid
		normalizedRerank := sigmoid(scores[i])

		results[i] = RerankResult{
			ID:            c.ID,
			Content:       c.Content,
			OriginalScore: c.Score,
			RerankScore:   normalizedRerank,
			CombinedScore: s.Alpha*normalizedRerank + (1-s.Alpha)*c.Score,
			Metadata:      c.Metadata,
			OriginalRank:  i + 1,
		}
	}

	// Sort by combined score (descending)
	sort.Slice(results, func(i, j int) bool {
		return results[i].CombinedScore > results[j].CombinedScore
	})

	// Assign rerank positions and calculate improvement
	for i := range results {
		results[i].RerankRank = i + 1
		results[i].RankImprovement = results[i].OriginalRank - results[i].RerankRank
	}

	// Apply limit
	if len(results) > limit {
		results = results[:limit]
	}

	log.Debug().
		Int("candidates", len(candidates)).
		Int("returned", len(results)).
		Float64("alpha", s.Alpha).
		Msg("Cross-encoder reranking completed")

	return results, nil
}

// RerankByScore reranks candidates and returns sorted by pure cross-encoder score.
// Useful when you want to completely replace bi-encoder ranking.
func (s *Service) RerankByScore(query string, candidates []Candidate, limit int) ([]RerankResult, error) {
	if len(candidates) == 0 {
		return nil, nil
	}

	if limit <= 0 {
		limit = DefaultResultLimit
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	scores, err := s.scoreAll(query, candidates)
	if err != nil {
		return nil, fmt.Errorf("score candidates: %w", err)
	}

	results := make([]RerankResult, len(candidates))
	for i, c := range candidates {
		normalizedRerank := sigmoid(scores[i])
		results[i] = RerankResult{
			ID:            c.ID,
			Content:       c.Content,
			OriginalScore: c.Score,
			RerankScore:   normalizedRerank,
			CombinedScore: normalizedRerank, // Use pure rerank score
			Metadata:      c.Metadata,
			OriginalRank:  i + 1,
		}
	}

	// Sort by rerank score only
	sort.Slice(results, func(i, j int) bool {
		return results[i].RerankScore > results[j].RerankScore
	})

	for i := range results {
		results[i].RerankRank = i + 1
		results[i].RankImprovement = results[i].OriginalRank - results[i].RerankRank
	}

	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// scoreAll scores all query-document pairs using the cross-encoder.
// Returns raw logits (before sigmoid normalization).
func (s *Service) scoreAll(query string, candidates []Candidate) ([]float64, error) {
	batchSize := len(candidates)

	// Tokenize all query-document pairs
	pairs := make([]tokenizer.EncodeInput, batchSize)
	for i, c := range candidates {
		// Cross-encoder takes query and document as a pair
		pairs[i] = tokenizer.NewDualEncodeInput(
			tokenizer.NewRawInputSequence(query),
			tokenizer.NewRawInputSequence(c.Content),
		)
	}

	encodings, err := s.tk.EncodeBatch(pairs, true)
	if err != nil {
		return nil, fmt.Errorf("tokenize pairs: %w", err)
	}

	// Find max sequence length
	seqLength := 0
	for _, enc := range encodings {
		if len(enc.Ids) > seqLength {
			seqLength = len(enc.Ids)
		}
	}
	if seqLength > MaxSequenceLength {
		seqLength = MaxSequenceLength
	}

	inputShape := ort.NewShape(int64(batchSize), int64(seqLength))

	// Create input tensors
	inputIdsData := make([]int64, batchSize*seqLength)
	attentionMaskData := make([]int64, batchSize*seqLength)
	tokenTypeIdsData := make([]int64, batchSize*seqLength)

	for b := 0; b < batchSize; b++ {
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

	// Cross-encoder outputs [batch, 1] logits
	outputShape := ort.NewShape(int64(batchSize), 1)
	outputTensor, err := ort.NewEmptyTensor[float32](outputShape)
	if err != nil {
		return nil, fmt.Errorf("create output tensor: %w", err)
	}
	defer outputTensor.Destroy()

	// Run inference
	inputTensors := []ort.Value{inputIdsTensor, attentionMaskTensor, tokenTypeIdsTensor}
	outputTensors := []ort.Value{outputTensor}

	if err := s.session.Run(inputTensors, outputTensors); err != nil {
		return nil, fmt.Errorf("run cross-encoder inference: %w", err)
	}

	// Extract scores
	flatOutput := outputTensor.GetData()
	scores := make([]float64, batchSize)
	for i := 0; i < batchSize; i++ {
		scores[i] = float64(flatOutput[i])
	}

	return scores, nil
}

// Score scores a single query-document pair.
// Returns the raw cross-encoder logit and normalized score.
func (s *Service) Score(query, document string) (rawScore, normalizedScore float64, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	scores, err := s.scoreAll(query, []Candidate{{Content: document}})
	if err != nil {
		return 0, 0, err
	}

	rawScore = scores[0]
	normalizedScore = sigmoid(rawScore)
	return rawScore, normalizedScore, nil
}

// Close releases model resources.
func (s *Service) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.session != nil {
		if err := s.session.Destroy(); err != nil {
			return fmt.Errorf("destroy cross-encoder session: %w", err)
		}
		s.session = nil
	}

	return nil
}

// sigmoid applies the sigmoid function to normalize scores to 0-1 range.
func sigmoid(x float64) float64 {
	if x > 20 {
		return 1.0
	}
	if x < -20 {
		return 0.0
	}
	return 1.0 / (1.0 + math.Exp(-x))
}
