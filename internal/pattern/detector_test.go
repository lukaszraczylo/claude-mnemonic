package pattern

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/lukaszraczylo/claude-mnemonic/internal/db/sqlite"
	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
)

func TestNewDetector(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	patternStore := sqlite.NewPatternStore(store)
	observationStore := sqlite.NewObservationStore(store)
	config := DefaultConfig()

	detector := NewDetector(patternStore, observationStore, config)
	if detector == nil {
		t.Fatal("Expected non-nil detector")
	}

	if detector.config.MinMatchScore != config.MinMatchScore {
		t.Errorf("Expected MinMatchScore %f, got %f",
			config.MinMatchScore, detector.config.MinMatchScore)
	}
}

func TestDetector_StartStop(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	patternStore := sqlite.NewPatternStore(store)
	observationStore := sqlite.NewObservationStore(store)
	config := DefaultConfig()
	config.AnalysisInterval = 100 * time.Millisecond // Short interval for testing

	detector := NewDetector(patternStore, observationStore, config)

	// Start
	detector.Start()

	// Wait a bit to ensure background goroutine is running
	time.Sleep(50 * time.Millisecond)

	// Stop
	detector.Stop()

	// Verify we can stop without hanging
	// (if this test hangs, the Stop logic is broken)
}

func TestDetector_AnalyzeObservation_NewCandidate(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	patternStore := sqlite.NewPatternStore(store)
	observationStore := sqlite.NewObservationStore(store)
	config := DefaultConfig()
	config.MinFrequencyForPattern = 2

	detector := NewDetector(patternStore, observationStore, config)
	ctx := context.Background()

	// Create an observation
	obs := createTestObservation(1, "nil-handling", []string{"nil", "error-handling"})

	// Analyze first observation
	result, err := detector.AnalyzeObservation(ctx, obs)
	if err != nil {
		t.Fatalf("AnalyzeObservation() error = %v", err)
	}

	// First observation should create a candidate, not a pattern
	if result.MatchedPattern != nil {
		t.Errorf("Expected no pattern match for first observation")
	}
	if result.IsNewPattern {
		t.Errorf("Expected IsNewPattern to be false for first observation")
	}
}

func TestDetector_AnalyzeObservation_PromoteToPattern(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	patternStore := sqlite.NewPatternStore(store)
	observationStore := sqlite.NewObservationStore(store)
	config := DefaultConfig()
	config.MinFrequencyForPattern = 2

	detector := NewDetector(patternStore, observationStore, config)
	ctx := context.Background()

	// Create two similar observations
	obs1 := createTestObservation(1, "Nil pointer handling", []string{"nil", "error-handling"})
	obs2 := createTestObservation(2, "Nil pointer handling", []string{"nil", "error-handling"})

	// Analyze first observation
	_, err := detector.AnalyzeObservation(ctx, obs1)
	if err != nil {
		t.Fatalf("AnalyzeObservation(obs1) error = %v", err)
	}

	// Analyze second observation - should promote to pattern
	result, err := detector.AnalyzeObservation(ctx, obs2)
	if err != nil {
		t.Fatalf("AnalyzeObservation(obs2) error = %v", err)
	}

	if result.MatchedPattern == nil {
		t.Fatal("Expected pattern to be created after second occurrence")
	}
	if !result.IsNewPattern {
		t.Errorf("Expected IsNewPattern to be true for newly promoted pattern")
	}
	if result.MatchedPattern.Frequency != 2 {
		t.Errorf("Expected frequency 2, got %d", result.MatchedPattern.Frequency)
	}
}

func TestDetector_AnalyzeObservation_MatchExisting(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	patternStore := sqlite.NewPatternStore(store)
	observationStore := sqlite.NewObservationStore(store)
	config := DefaultConfig()

	detector := NewDetector(patternStore, observationStore, config)
	ctx := context.Background()

	// Create an existing pattern
	pattern := &models.Pattern{
		Name:           "Existing Pattern",
		Type:           models.PatternTypeBug,
		Signature:      []string{"nil", "error-handling", "pointer"},
		Frequency:      5,
		Projects:       []string{"proj1"},
		ObservationIDs: []int64{1, 2, 3, 4, 5},
		Status:         models.PatternStatusActive,
		Confidence:     0.7,
		LastSeenAt:     time.Now().Format(time.RFC3339),
		LastSeenEpoch:  time.Now().UnixMilli(),
		CreatedAt:      time.Now().Format(time.RFC3339),
		CreatedAtEpoch: time.Now().UnixMilli(),
	}
	patternStore.StorePattern(ctx, pattern)

	// Create observation with similar signature
	obs := createTestObservation(10, "Nil check", []string{"nil", "error-handling"})

	// Analyze - should match existing pattern
	result, err := detector.AnalyzeObservation(ctx, obs)
	if err != nil {
		t.Fatalf("AnalyzeObservation() error = %v", err)
	}

	if result.MatchedPattern == nil {
		t.Fatal("Expected to match existing pattern")
	}
	if result.IsNewPattern {
		t.Errorf("Expected IsNewPattern to be false for existing pattern")
	}
	if result.MatchScore < 0.3 {
		t.Errorf("Expected match score >= 0.3, got %f", result.MatchScore)
	}
}

func TestDetector_AnalyzeObservation_NoMatch(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	patternStore := sqlite.NewPatternStore(store)
	observationStore := sqlite.NewObservationStore(store)
	config := DefaultConfig()
	config.MinMatchScore = 0.5 // Higher threshold

	detector := NewDetector(patternStore, observationStore, config)
	ctx := context.Background()

	// Create an existing pattern with specific signature
	pattern := &models.Pattern{
		Name:           "Specific Pattern",
		Type:           models.PatternTypeBug,
		Signature:      []string{"database", "connection", "pool"},
		Frequency:      3,
		Projects:       []string{"proj1"},
		ObservationIDs: []int64{1, 2, 3},
		Status:         models.PatternStatusActive,
		Confidence:     0.6,
		LastSeenAt:     time.Now().Format(time.RFC3339),
		LastSeenEpoch:  time.Now().UnixMilli(),
		CreatedAt:      time.Now().Format(time.RFC3339),
		CreatedAtEpoch: time.Now().UnixMilli(),
	}
	patternStore.StorePattern(ctx, pattern)

	// Create observation with completely different signature
	obs := createTestObservation(10, "UI Component", []string{"frontend", "react", "component"})

	// Analyze - should not match
	result, err := detector.AnalyzeObservation(ctx, obs)
	if err != nil {
		t.Fatalf("AnalyzeObservation() error = %v", err)
	}

	if result.MatchedPattern != nil {
		t.Errorf("Expected no match for unrelated observation")
	}
}

func TestDetector_CandidateCleanup(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	patternStore := sqlite.NewPatternStore(store)
	observationStore := sqlite.NewObservationStore(store)
	config := DefaultConfig()
	config.MinFrequencyForPattern = 3 // Higher threshold

	detector := NewDetector(patternStore, observationStore, config)

	// Add an old candidate manually
	oldKey := "old|candidate|"
	detector.candidates[oldKey] = &candidatePattern{
		signature:      []string{"old", "candidate"},
		observationIDs: []int64{1},
		projects:       []string{"proj1"},
		patternType:    models.PatternTypeBug,
		title:          "Old Candidate",
		lastSeenEpoch:  time.Now().Add(-8 * 24 * time.Hour).UnixMilli(), // 8 days ago
	}

	// Add a recent candidate
	recentKey := "recent|candidate|"
	detector.candidates[recentKey] = &candidatePattern{
		signature:      []string{"recent", "candidate"},
		observationIDs: []int64{2},
		projects:       []string{"proj1"},
		patternType:    models.PatternTypeBug,
		title:          "Recent Candidate",
		lastSeenEpoch:  time.Now().UnixMilli(),
	}

	// Run cleanup
	detector.cleanupOldCandidates()

	// Old candidate should be removed
	if _, exists := detector.candidates[oldKey]; exists {
		t.Errorf("Expected old candidate to be cleaned up")
	}

	// Recent candidate should remain
	if _, exists := detector.candidates[recentKey]; !exists {
		t.Errorf("Expected recent candidate to remain")
	}
}

func TestDetector_GetPatternInsight(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	patternStore := sqlite.NewPatternStore(store)
	observationStore := sqlite.NewObservationStore(store)
	config := DefaultConfig()

	detector := NewDetector(patternStore, observationStore, config)
	ctx := context.Background()

	// Create pattern with recommendation
	pattern := &models.Pattern{
		Name:           "Insight Test Pattern",
		Type:           models.PatternTypeBestPractice,
		Signature:      []string{"test"},
		Recommendation: sql.NullString{String: "Always write tests first", Valid: true},
		Frequency:      12,
		Projects:       []string{"proj1", "proj2", "proj3"},
		ObservationIDs: []int64{1},
		Status:         models.PatternStatusActive,
		Confidence:     0.8,
		LastSeenAt:     time.Now().Format(time.RFC3339),
		LastSeenEpoch:  time.Now().UnixMilli(),
		CreatedAt:      time.Now().Format(time.RFC3339),
		CreatedAtEpoch: time.Now().UnixMilli(),
	}
	id, _ := patternStore.StorePattern(ctx, pattern)

	// Get insight
	insight, err := detector.GetPatternInsight(ctx, id)
	if err != nil {
		t.Fatalf("GetPatternInsight() error = %v", err)
	}

	// Verify insight contains expected elements
	if insight == "" {
		t.Error("Expected non-empty insight")
	}
	if !containsString(insight, "12 times") {
		t.Errorf("Expected insight to contain frequency, got: %s", insight)
	}
	if !containsString(insight, "3 projects") {
		t.Errorf("Expected insight to contain project count, got: %s", insight)
	}
	if !containsString(insight, "Always write tests first") {
		t.Errorf("Expected insight to contain recommendation, got: %s", insight)
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.MinMatchScore <= 0 || config.MinMatchScore > 1 {
		t.Errorf("Invalid MinMatchScore: %f", config.MinMatchScore)
	}
	if config.MinFrequencyForPattern < 1 {
		t.Errorf("Invalid MinFrequencyForPattern: %d", config.MinFrequencyForPattern)
	}
	if config.AnalysisInterval <= 0 {
		t.Errorf("Invalid AnalysisInterval: %v", config.AnalysisInterval)
	}
	if config.MaxPatternsToTrack <= 0 {
		t.Errorf("Invalid MaxPatternsToTrack: %d", config.MaxPatternsToTrack)
	}
}

func TestGeneratePatternName(t *testing.T) {
	tests := []struct {
		patternType models.PatternType
		signature   []string
		title       string
		wantPrefix  string
	}{
		{models.PatternTypeBug, []string{"nil", "error"}, "", "Bug Pattern:"},
		{models.PatternTypeRefactor, []string{"extract"}, "", "Refactor Pattern:"},
		{models.PatternTypeArchitecture, []string{"service"}, "", "Architecture Pattern:"},
		{models.PatternTypeAntiPattern, []string{"god-class"}, "", "Anti-Pattern:"},
		{models.PatternTypeBestPractice, []string{"testing"}, "", "Best Practice:"},
		{models.PatternTypeBug, []string{}, "Short Title", "Short Title"}, // Use title directly
	}

	for _, tt := range tests {
		name := generatePatternName(tt.patternType, tt.signature, tt.title)
		if !hasPrefix(name, tt.wantPrefix) {
			t.Errorf("generatePatternName(%v, %v, %q) = %q, want prefix %q",
				tt.patternType, tt.signature, tt.title, name, tt.wantPrefix)
		}
	}
}

func TestFormatPatternInsight(t *testing.T) {
	// Pattern without recommendation
	pattern1 := &models.Pattern{
		Type:      models.PatternTypeBug,
		Frequency: 5,
		Projects:  []string{"proj1"},
	}
	insight1 := formatPatternInsight(pattern1)
	if !containsString(insight1, "5 times") {
		t.Errorf("Expected insight to contain frequency")
	}
	if !containsString(insight1, "recurring bug pattern") {
		t.Errorf("Expected bug pattern description")
	}

	// Pattern with recommendation
	pattern2 := &models.Pattern{
		Type:           models.PatternTypeBestPractice,
		Frequency:      10,
		Projects:       []string{"proj1", "proj2"},
		Recommendation: sql.NullString{String: "Do this", Valid: true},
	}
	insight2 := formatPatternInsight(pattern2)
	if !containsString(insight2, "10 times") {
		t.Errorf("Expected insight to contain frequency")
	}
	if !containsString(insight2, "2 projects") {
		t.Errorf("Expected insight to contain project count")
	}
	if !containsString(insight2, "Do this") {
		t.Errorf("Expected insight to contain recommendation")
	}
}

// Helper functions

func setupTestStore(t *testing.T) *sqlite.Store {
	t.Helper()

	// Create temp database file
	tmpFile, err := os.CreateTemp("", "pattern_test_*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpFile.Close()

	t.Cleanup(func() {
		os.Remove(tmpFile.Name())
	})

	store, err := sqlite.NewStore(sqlite.StoreConfig{
		Path:     tmpFile.Name(),
		MaxConns: 1,
		WALMode:  true,
	})
	if err != nil {
		// Check if this is an FTS5 related error
		if containsString(err.Error(), "fts5") || containsString(err.Error(), "no such module") {
			t.Skip("FTS5 not available in this SQLite build")
		}
		t.Fatalf("Failed to create store: %v", err)
	}

	return store
}

func createTestObservation(id int64, title string, concepts []string) *models.Observation {
	return &models.Observation{
		ID:             id,
		SDKSessionID:   "test-session",
		Project:        "test-project",
		Scope:          models.ScopeProject,
		Type:           models.ObsTypeBugfix,
		Title:          sql.NullString{String: title, Valid: true},
		Narrative:      sql.NullString{String: "Test narrative", Valid: true},
		Concepts:       concepts,
		CreatedAt:      time.Now().Format(time.RFC3339),
		CreatedAtEpoch: time.Now().UnixMilli(),
	}
}

func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func hasPrefix(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	return s[:len(prefix)] == prefix
}
