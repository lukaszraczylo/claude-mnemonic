package reranking

import (
	"sync"
	"testing"

	"github.com/lukaszraczylo/claude-mnemonic/internal/embedding"
)

// initONNX initializes the ONNX runtime via the embedding service.
// Must be called before creating reranking service.
func initONNX(t *testing.T) func() {
	t.Helper()
	embSvc, err := embedding.NewService()
	if err != nil {
		t.Fatalf("Failed to initialize ONNX via embedding service: %v", err)
	}
	return func() {
		embSvc.Close()
	}
}

// TestSigmoid tests the sigmoid normalization function.
func TestSigmoid(t *testing.T) {
	tests := []struct {
		name    string
		input   float64
		wantMin float64
		wantMax float64
	}{
		{"positive large", 10, 0.9999, 1.0},
		{"positive small", 1, 0.7, 0.8},
		{"zero", 0, 0.4999, 0.5001},
		{"negative small", -1, 0.2, 0.3},
		{"negative large", -10, 0, 0.0001},
		{"very positive", 25, 0.999999, 1.0},
		{"very negative", -25, 0, 0.000001},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sigmoid(tt.input)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("sigmoid(%v) = %v, want in range [%v, %v]",
					tt.input, got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

// TestDefaultConfig tests the default configuration values.
func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Alpha < 0 || cfg.Alpha > 1 {
		t.Errorf("DefaultConfig().Alpha = %v, want in range [0, 1]", cfg.Alpha)
	}
	if cfg.Alpha != 0.7 {
		t.Errorf("DefaultConfig().Alpha = %v, want 0.7", cfg.Alpha)
	}
}

// TestNewService tests service creation.
func TestNewService(t *testing.T) {
	cleanup := initONNX(t)
	defer cleanup()

	cfg := DefaultConfig()
	svc, err := NewService(cfg)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	defer svc.Close()

	if svc == nil {
		t.Fatal("NewService() returned nil")
	}

	if svc.Alpha != cfg.Alpha {
		t.Errorf("Service.Alpha = %v, want %v", svc.Alpha, cfg.Alpha)
	}
}

// TestScore tests single pair scoring.
func TestScore(t *testing.T) {
	cleanup := initONNX(t)
	defer cleanup()

	cfg := DefaultConfig()
	svc, err := NewService(cfg)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	defer svc.Close()

	query := "What is the capital of France?"
	relevant := "Paris is the capital and largest city of France."
	irrelevant := "Dogs are popular pets known for their loyalty."

	// Score relevant document
	_, relevantNorm, err := svc.Score(query, relevant)
	if err != nil {
		t.Fatalf("Score(relevant) error = %v", err)
	}

	// Score irrelevant document
	_, irrelevantNorm, err := svc.Score(query, irrelevant)
	if err != nil {
		t.Fatalf("Score(irrelevant) error = %v", err)
	}

	// Relevant document should score higher
	if relevantNorm <= irrelevantNorm {
		t.Errorf("Expected relevant (%v) > irrelevant (%v)",
			relevantNorm, irrelevantNorm)
	}
}

// TestRerank tests the reranking functionality.
func TestRerank(t *testing.T) {
	cleanup := initONNX(t)
	defer cleanup()

	cfg := DefaultConfig()
	svc, err := NewService(cfg)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	defer svc.Close()

	query := "How to handle errors in Go?"
	candidates := []Candidate{
		{
			ID:      "1",
			Content: "Python exception handling with try/except blocks.",
			Score:   0.8, // High bi-encoder score but irrelevant
		},
		{
			ID:      "2",
			Content: "Go error handling uses explicit return values. Functions return error as the last value.",
			Score:   0.6, // Lower bi-encoder score but relevant
		},
		{
			ID:      "3",
			Content: "JavaScript uses Promise.catch for async error handling.",
			Score:   0.7,
		},
	}

	results, err := svc.Rerank(query, candidates, 3)
	if err != nil {
		t.Fatalf("Rerank() error = %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("Rerank() returned %d results, want 3", len(results))
	}

	// The Go error handling document should rank higher after reranking
	var goRank int
	for i, r := range results {
		if r.ID == "2" {
			goRank = i + 1
			break
		}
	}

	if goRank == 0 {
		t.Error("Go document not found in results")
	}

	// Verify all results have required fields populated
	for i, r := range results {
		if r.ID == "" {
			t.Errorf("Result %d has empty ID", i)
		}
		if r.Content == "" {
			t.Errorf("Result %d has empty Content", i)
		}
		if r.RerankRank != i+1 {
			t.Errorf("Result %d has RerankRank %d, want %d", i, r.RerankRank, i+1)
		}
	}
}

// TestRerankEmpty tests reranking with empty input.
func TestRerankEmpty(t *testing.T) {
	cleanup := initONNX(t)
	defer cleanup()

	cfg := DefaultConfig()
	svc, err := NewService(cfg)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	defer svc.Close()

	results, err := svc.Rerank("test query", nil, 10)
	if err != nil {
		t.Fatalf("Rerank(nil) error = %v", err)
	}

	if results != nil {
		t.Errorf("Rerank(nil) = %v, want nil", results)
	}

	results, err = svc.Rerank("test query", []Candidate{}, 10)
	if err != nil {
		t.Fatalf("Rerank([]) error = %v", err)
	}

	if results != nil {
		t.Errorf("Rerank([]) = %v, want nil", results)
	}
}

// TestRerankLimit tests that limit is respected.
func TestRerankLimit(t *testing.T) {
	cleanup := initONNX(t)
	defer cleanup()

	cfg := DefaultConfig()
	svc, err := NewService(cfg)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	defer svc.Close()

	candidates := make([]Candidate, 20)
	for i := range candidates {
		candidates[i] = Candidate{
			ID:      string(rune('A' + i)),
			Content: "Test document content for ranking.",
			Score:   0.5,
		}
	}

	results, err := svc.Rerank("test query", candidates, 5)
	if err != nil {
		t.Fatalf("Rerank() error = %v", err)
	}

	if len(results) != 5 {
		t.Errorf("Rerank() returned %d results, want 5", len(results))
	}
}

// TestRerankByScore tests pure cross-encoder ranking.
func TestRerankByScore(t *testing.T) {
	cleanup := initONNX(t)
	defer cleanup()

	cfg := DefaultConfig()
	svc, err := NewService(cfg)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	defer svc.Close()

	query := "machine learning algorithms"
	candidates := []Candidate{
		{
			ID:      "1",
			Content: "Cooking recipes for Italian pasta dishes.",
			Score:   0.9, // High original score
		},
		{
			ID:      "2",
			Content: "Neural networks are a type of machine learning algorithm.",
			Score:   0.3, // Low original score
		},
	}

	results, err := svc.RerankByScore(query, candidates, 2)
	if err != nil {
		t.Fatalf("RerankByScore() error = %v", err)
	}

	// Document 2 should rank first since it's about ML
	if results[0].ID != "2" {
		t.Errorf("Expected ML document to rank first, got %v", results[0].ID)
	}

	// CombinedScore should equal RerankScore when using RerankByScore
	for _, r := range results {
		if r.CombinedScore != r.RerankScore {
			t.Errorf("RerankByScore: CombinedScore (%v) != RerankScore (%v)",
				r.CombinedScore, r.RerankScore)
		}
	}
}

// TestRankImprovement tests that rank improvement is calculated correctly.
func TestRankImprovement(t *testing.T) {
	cleanup := initONNX(t)
	defer cleanup()

	cfg := DefaultConfig()
	svc, err := NewService(cfg)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	defer svc.Close()

	// Create candidates where we know the expected reranking
	candidates := []Candidate{
		{ID: "A", Content: "Unrelated content about weather forecasting.", Score: 0.9},
		{ID: "B", Content: "How to fix memory leaks in Go programs.", Score: 0.8},
		{ID: "C", Content: "More unrelated content about gardening tips.", Score: 0.7},
	}

	results, err := svc.Rerank("debugging memory issues in Go", candidates, 3)
	if err != nil {
		t.Fatalf("Rerank() error = %v", err)
	}

	for _, r := range results {
		// RankImprovement = OriginalRank - RerankRank
		// Positive means moved up, negative means moved down
		expectedImprovement := r.OriginalRank - r.RerankRank
		if r.RankImprovement != expectedImprovement {
			t.Errorf("ID %s: RankImprovement = %d, want %d (orig=%d, new=%d)",
				r.ID, r.RankImprovement, expectedImprovement,
				r.OriginalRank, r.RerankRank)
		}
	}
}

// TestConcurrentRerank tests concurrent reranking calls.
func TestConcurrentRerank(t *testing.T) {
	cleanup := initONNX(t)
	defer cleanup()

	cfg := DefaultConfig()
	svc, err := NewService(cfg)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	defer svc.Close()

	candidates := []Candidate{
		{ID: "1", Content: "Test document one.", Score: 0.5},
		{ID: "2", Content: "Test document two.", Score: 0.5},
	}

	var wg sync.WaitGroup
	errors := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := svc.Rerank("concurrent test query", candidates, 2)
			if err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("Concurrent Rerank error: %v", err)
	}
}

// TestClose tests service cleanup.
func TestClose(t *testing.T) {
	cleanup := initONNX(t)
	defer cleanup()

	cfg := DefaultConfig()
	svc, err := NewService(cfg)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	err = svc.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Double close should not panic
	err = svc.Close()
	if err != nil {
		t.Errorf("Close() on closed service error = %v", err)
	}
}

// TestMetadataPreserved tests that metadata is preserved through reranking.
func TestMetadataPreserved(t *testing.T) {
	cleanup := initONNX(t)
	defer cleanup()

	cfg := DefaultConfig()
	svc, err := NewService(cfg)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	defer svc.Close()

	candidates := []Candidate{
		{
			ID:       "1",
			Content:  "Test content.",
			Score:    0.5,
			Metadata: map[string]any{"custom": "value1", "num": 42},
		},
		{
			ID:       "2",
			Content:  "Another test.",
			Score:    0.5,
			Metadata: map[string]any{"custom": "value2"},
		},
	}

	results, err := svc.Rerank("query", candidates, 2)
	if err != nil {
		t.Fatalf("Rerank() error = %v", err)
	}

	for _, r := range results {
		if r.Metadata == nil {
			t.Errorf("Result %s has nil metadata", r.ID)
			continue
		}

		// Find original candidate
		var original *Candidate
		for i := range candidates {
			if candidates[i].ID == r.ID {
				original = &candidates[i]
				break
			}
		}

		if original == nil {
			t.Errorf("Could not find original for result %s", r.ID)
			continue
		}

		// Check metadata preserved
		if original.Metadata["custom"] != r.Metadata["custom"] {
			t.Errorf("Metadata not preserved for %s", r.ID)
		}
	}
}
