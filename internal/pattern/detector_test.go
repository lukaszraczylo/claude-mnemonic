package pattern

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/lukaszraczylo/claude-mnemonic/internal/db/gorm"
	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
)

func TestNewDetector(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	patternStore := gorm.NewPatternStore(store)
	observationStore := gorm.NewObservationStore(store, nil, nil, nil)
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

	patternStore := gorm.NewPatternStore(store)
	observationStore := gorm.NewObservationStore(store, nil, nil, nil)
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

	patternStore := gorm.NewPatternStore(store)
	observationStore := gorm.NewObservationStore(store, nil, nil, nil)
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

	patternStore := gorm.NewPatternStore(store)
	observationStore := gorm.NewObservationStore(store, nil, nil, nil)
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

	patternStore := gorm.NewPatternStore(store)
	observationStore := gorm.NewObservationStore(store, nil, nil, nil)
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
	_, _ = patternStore.StorePattern(ctx, pattern)

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

	patternStore := gorm.NewPatternStore(store)
	observationStore := gorm.NewObservationStore(store, nil, nil, nil)
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
	_, _ = patternStore.StorePattern(ctx, pattern)

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

	patternStore := gorm.NewPatternStore(store)
	observationStore := gorm.NewObservationStore(store, nil, nil, nil)
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

	patternStore := gorm.NewPatternStore(store)
	observationStore := gorm.NewObservationStore(store, nil, nil, nil)
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

func TestDefaultConfig_AllFieldsValid(t *testing.T) {
	config := DefaultConfig()

	if config.MinMatchScore != 0.3 {
		t.Errorf("MinMatchScore = %f, want 0.3", config.MinMatchScore)
	}
	if config.MinFrequencyForPattern != 2 {
		t.Errorf("MinFrequencyForPattern = %d, want 2", config.MinFrequencyForPattern)
	}
	if config.AnalysisInterval != 5*time.Minute {
		t.Errorf("AnalysisInterval = %v, want 5m", config.AnalysisInterval)
	}
	if config.MaxPatternsToTrack != 1000 {
		t.Errorf("MaxPatternsToTrack = %d, want 1000", config.MaxPatternsToTrack)
	}
	if config.MaxCandidates != 500 {
		t.Errorf("MaxCandidates = %d, want 500", config.MaxCandidates)
	}
}

func TestGeneratePatternName(t *testing.T) {
	tests := []struct {
		patternType models.PatternType
		title       string
		wantPrefix  string
		signature   []string
	}{
		{patternType: models.PatternTypeBug, title: "", wantPrefix: "Bug Pattern:", signature: []string{"nil", "error"}},
		{patternType: models.PatternTypeRefactor, title: "", wantPrefix: "Refactor Pattern:", signature: []string{"extract"}},
		{patternType: models.PatternTypeArchitecture, title: "", wantPrefix: "Architecture Pattern:", signature: []string{"service"}},
		{patternType: models.PatternTypeAntiPattern, title: "", wantPrefix: "Anti-Pattern:", signature: []string{"god-class"}},
		{patternType: models.PatternTypeBestPractice, title: "", wantPrefix: "Best Practice:", signature: []string{"testing"}},
		{patternType: models.PatternTypeBug, title: "Short Title", wantPrefix: "Short Title", signature: []string{}}, // Use title directly
	}

	for _, tt := range tests {
		name := generatePatternName(tt.patternType, tt.signature, tt.title)
		if !hasPrefix(name, tt.wantPrefix) {
			t.Errorf("generatePatternName(%v, %v, %q) = %q, want prefix %q",
				tt.patternType, tt.signature, tt.title, name, tt.wantPrefix)
		}
	}
}

func TestGeneratePatternName_EdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		ptype     models.PatternType
		title     string
		want      string
		signature []string
	}{
		{
			name:      "with title uses title directly",
			ptype:     models.PatternTypeBug,
			signature: []string{"ignored"},
			title:     "Custom Title",
			want:      "Custom Title",
		},
		{
			name:      "long title generates from signature",
			ptype:     models.PatternTypeBug,
			signature: []string{"sig1", "sig2"},
			title:     "This is a very long title that exceeds sixty characters and should be ignored",
			want:      "Bug Pattern: sig1, sig2",
		},
		{
			name:      "empty signature returns Unnamed",
			ptype:     models.PatternTypeBug,
			signature: []string{},
			title:     "",
			want:      "Bug Pattern: Unnamed",
		},
		{
			name:      "single signature element",
			ptype:     models.PatternTypeRefactor,
			signature: []string{"single"},
			title:     "",
			want:      "Refactor Pattern: single",
		},
		{
			name:      "more than 3 signature elements truncates",
			ptype:     models.PatternTypeBestPractice,
			signature: []string{"a", "b", "c", "d", "e"},
			title:     "",
			want:      "Best Practice: a, b, c",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generatePatternName(tt.ptype, tt.signature, tt.title)
			if got != tt.want {
				t.Errorf("generatePatternName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGeneratePatternName_AllTypes(t *testing.T) {
	tests := []struct {
		ptype      models.PatternType
		wantPrefix string
	}{
		{models.PatternTypeBug, "Bug Pattern:"},
		{models.PatternTypeRefactor, "Refactor Pattern:"},
		{models.PatternTypeArchitecture, "Architecture Pattern:"},
		{models.PatternTypeAntiPattern, "Anti-Pattern:"},
		{models.PatternTypeBestPractice, "Best Practice:"},
		{models.PatternType("unknown"), "test"}, // Unknown type has empty prefix, starts with first signature element
	}

	for _, tt := range tests {
		t.Run(string(tt.ptype), func(t *testing.T) {
			name := generatePatternName(tt.ptype, []string{"test", "sig"}, "")
			if !hasPrefix(name, tt.wantPrefix) {
				t.Errorf("Expected prefix %q for type %s, got: %s",
					tt.wantPrefix, tt.ptype, name)
			}
		})
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

func TestFormatPatternInsight_AllTypes(t *testing.T) {
	types := []struct {
		ptype    models.PatternType
		contains string
	}{
		{models.PatternTypeBug, "bug pattern"},
		{models.PatternTypeRefactor, "recognized pattern"},     // Falls to default case
		{models.PatternTypeArchitecture, "recognized pattern"}, // Falls to default case
		{models.PatternTypeAntiPattern, "anti-pattern"},
		{models.PatternTypeBestPractice, "best practice"},
		{models.PatternType("unknown"), "recognized pattern"}, // Falls to default case
	}

	for _, tt := range types {
		t.Run(string(tt.ptype), func(t *testing.T) {
			pattern := &models.Pattern{
				Type:      tt.ptype,
				Frequency: 3,
				Projects:  []string{"proj1"},
			}
			insight := formatPatternInsight(pattern)
			if !containsString(insight, tt.contains) {
				t.Errorf("Expected insight to contain %q for type %s, got: %s",
					tt.contains, tt.ptype, insight)
			}
		})
	}
}

func TestFormatPatternInsight_MultiProject(t *testing.T) {
	pattern := &models.Pattern{
		Type:      models.PatternTypeBug,
		Frequency: 10,
		Projects:  []string{"proj1", "proj2", "proj3"},
	}

	insight := formatPatternInsight(pattern)

	if !containsString(insight, "10 times") {
		t.Error("Expected frequency in insight")
	}
	if !containsString(insight, "3 projects") {
		t.Error("Expected project count in insight")
	}
}

func TestFormatPatternInsight_SingleProject(t *testing.T) {
	pattern := &models.Pattern{
		Type:      models.PatternTypeBestPractice,
		Frequency: 5,
		Projects:  []string{"only-one"},
	}

	insight := formatPatternInsight(pattern)

	if !containsString(insight, "5 times") {
		t.Error("Expected frequency in insight")
	}
	// Single project should NOT mention "projects"
	if containsString(insight, "projects") {
		t.Error("Single project should not mention 'projects'")
	}
}

func TestDetector_SetSyncFunc(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	patternStore := gorm.NewPatternStore(store)
	observationStore := gorm.NewObservationStore(store, nil, nil, nil)
	config := DefaultConfig()

	detector := NewDetector(patternStore, observationStore, config)

	// Initially nil
	if detector.syncFunc != nil {
		t.Error("Expected syncFunc to be nil initially")
	}

	// Set sync func
	var syncCalled bool
	detector.SetSyncFunc(func(p *models.Pattern) {
		syncCalled = true
	})

	if detector.syncFunc == nil {
		t.Error("Expected syncFunc to be set")
	}

	// Verify it can be called
	detector.syncFunc(&models.Pattern{})
	if !syncCalled {
		t.Error("Expected sync function to be called")
	}
}

func TestDetector_CandidateCount(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	patternStore := gorm.NewPatternStore(store)
	observationStore := gorm.NewObservationStore(store, nil, nil, nil)
	config := DefaultConfig()

	detector := NewDetector(patternStore, observationStore, config)

	// Initially zero
	if count := detector.CandidateCount(); count != 0 {
		t.Errorf("Expected 0 candidates, got %d", count)
	}

	// Add some candidates
	detector.candidates["key1"] = &candidatePattern{}
	detector.candidates["key2"] = &candidatePattern{}

	if count := detector.CandidateCount(); count != 2 {
		t.Errorf("Expected 2 candidates, got %d", count)
	}
}

func TestDetector_AnalyzeRecentObservations(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	patternStore := gorm.NewPatternStore(store)
	observationStore := gorm.NewObservationStore(store, nil, nil, nil)
	config := DefaultConfig()

	detector := NewDetector(patternStore, observationStore, config)
	ctx := context.Background()

	// Should not error even with no observations
	err := detector.AnalyzeRecentObservations(ctx)
	if err != nil {
		t.Fatalf("AnalyzeRecentObservations() error = %v", err)
	}
}

func TestGenerateCandidateKey(t *testing.T) {
	tests := []struct {
		name      string
		want      string
		signature []string
	}{
		{
			name:      "single element",
			signature: []string{"error"},
			want:      "error|",
		},
		{
			name:      "multiple elements",
			signature: []string{"error", "handling", "nil"},
			want:      "error|handling|nil|",
		},
		{
			name:      "empty signature",
			signature: []string{},
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateCandidateKey(tt.signature)
			if got != tt.want {
				t.Errorf("generateCandidateKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGenerateCandidateKey_EdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		want      string
		signature []string
	}{
		{
			name:      "nil signature",
			signature: nil,
			want:      "",
		},
		{
			name:      "empty strings in signature",
			signature: []string{"", ""},
			want:      "||",
		},
		{
			name:      "special characters",
			signature: []string{"error|handling", "nil"},
			want:      "error|handling|nil|",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateCandidateKey(tt.signature)
			if got != tt.want {
				t.Errorf("generateCandidateKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestItoa(t *testing.T) {
	tests := []struct {
		want  string
		input int
	}{
		{"0", 0},
		{"1", 1},
		{"10", 10},
		{"123", 123},
		{"-1", -1},
		{"-123", -123},
		{"1000000", 1000000},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := itoa(tt.input)
			if got != tt.want {
				t.Errorf("itoa(%d) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestItoa_EdgeCases(t *testing.T) {
	tests := []struct {
		want  string
		input int
	}{
		{"0", 0},
		{"0", -0},
		{"1", 1},
		{"-1", -1},
		{"9", 9},
		{"10", 10},
		{"99", 99},
		{"100", 100},
		{"999", 999},
		{"1000", 1000},
		{"-999", -999},
		{"-1000", -1000},
		{"2147483647", 2147483647},   // Max int32
		{"-2147483647", -2147483647}, // Min int32 + 1
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := itoa(tt.input)
			if got != tt.want {
				t.Errorf("itoa(%d) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestDetectionResult_ZeroValue(t *testing.T) {
	result := &DetectionResult{}

	if result.MatchedPattern != nil {
		t.Error("Zero value should have nil MatchedPattern")
	}
	if result.MatchScore != 0 {
		t.Error("Zero value should have 0 MatchScore")
	}
	if result.IsNewPattern {
		t.Error("Zero value should have false IsNewPattern")
	}
}

func TestCandidatePattern_Fields(t *testing.T) {
	candidate := &candidatePattern{
		patternType:    models.PatternTypeBug,
		title:          "Test Title",
		signature:      []string{"sig1", "sig2"},
		observationIDs: []int64{1, 2, 3},
		projects:       []string{"proj1", "proj2"},
		lastSeenEpoch:  time.Now().UnixMilli(),
	}

	if candidate.patternType != models.PatternTypeBug {
		t.Error("Wrong pattern type")
	}
	if candidate.title != "Test Title" {
		t.Error("Wrong title")
	}
	if len(candidate.signature) != 2 {
		t.Error("Wrong signature length")
	}
	if len(candidate.observationIDs) != 3 {
		t.Error("Wrong observationIDs length")
	}
	if len(candidate.projects) != 2 {
		t.Error("Wrong projects length")
	}
}

func TestDetector_AnalyzeObservation_EmptySignature(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	patternStore := gorm.NewPatternStore(store)
	observationStore := gorm.NewObservationStore(store, nil, nil, nil)
	config := DefaultConfig()

	detector := NewDetector(patternStore, observationStore, config)
	ctx := context.Background()

	// Create observation with empty concepts/title/narrative
	obs := &models.Observation{
		ID:           1,
		SDKSessionID: "test-session",
		Project:      "test-project",
		Scope:        models.ScopeProject,
		Type:         models.ObsTypeBugfix,
		// All fields that would create signature are empty
	}

	result, err := detector.AnalyzeObservation(ctx, obs)
	if err != nil {
		t.Fatalf("AnalyzeObservation() error = %v", err)
	}

	// Should return empty result for empty signature
	if result.MatchedPattern != nil {
		t.Error("Expected nil pattern for empty signature")
	}
}

func TestDetector_AnalyzeObservation_CandidateEviction(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	patternStore := gorm.NewPatternStore(store)
	observationStore := gorm.NewObservationStore(store, nil, nil, nil)
	config := DefaultConfig()
	config.MaxCandidates = 2           // Very small for testing
	config.MinFrequencyForPattern = 10 // High so nothing gets promoted

	detector := NewDetector(patternStore, observationStore, config)
	ctx := context.Background()

	// Add observations with different signatures until we exceed MaxCandidates
	obs1 := createTestObservation(1, "First", []string{"first", "unique"})
	obs2 := createTestObservation(2, "Second", []string{"second", "unique"})
	obs3 := createTestObservation(3, "Third", []string{"third", "unique"})

	// Analyze all observations
	_, _ = detector.AnalyzeObservation(ctx, obs1)
	time.Sleep(10 * time.Millisecond) // Small delay so timestamps differ
	_, _ = detector.AnalyzeObservation(ctx, obs2)
	time.Sleep(10 * time.Millisecond)
	_, _ = detector.AnalyzeObservation(ctx, obs3)

	// Should have at most MaxCandidates
	if count := detector.CandidateCount(); count > config.MaxCandidates {
		t.Errorf("Expected at most %d candidates, got %d", config.MaxCandidates, count)
	}
}

func TestDetector_PromoteCandidateWithSyncFunc(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	patternStore := gorm.NewPatternStore(store)
	observationStore := gorm.NewObservationStore(store, nil, nil, nil)
	config := DefaultConfig()
	config.MinFrequencyForPattern = 2

	detector := NewDetector(patternStore, observationStore, config)
	ctx := context.Background()

	// Set up sync function to track calls
	var syncedPattern *models.Pattern
	detector.SetSyncFunc(func(p *models.Pattern) {
		syncedPattern = p
	})

	// Create two similar observations to trigger pattern promotion
	obs1 := createTestObservation(1, "Sync Test", []string{"sync", "test"})
	obs2 := createTestObservation(2, "Sync Test", []string{"sync", "test"})

	_, _ = detector.AnalyzeObservation(ctx, obs1)
	result, _ := detector.AnalyzeObservation(ctx, obs2)

	if result.MatchedPattern == nil {
		t.Fatal("Expected pattern to be created")
	}

	if syncedPattern == nil {
		t.Error("Expected sync function to be called")
	}

	if syncedPattern != nil && syncedPattern.Name != result.MatchedPattern.Name {
		t.Errorf("Synced pattern name mismatch: got %s, want %s",
			syncedPattern.Name, result.MatchedPattern.Name)
	}
}

func TestDetector_AnalyzeObservation_UpdateExistingCandidate(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	patternStore := gorm.NewPatternStore(store)
	observationStore := gorm.NewObservationStore(store, nil, nil, nil)
	config := DefaultConfig()
	config.MinFrequencyForPattern = 5 // High enough that we don't promote

	detector := NewDetector(patternStore, observationStore, config)
	ctx := context.Background()

	// Create observations with same signature
	obs1 := createTestObservation(1, "Update Test", []string{"update", "test"})
	obs2 := createTestObservation(2, "Update Test", []string{"update", "test"})
	obs2.Project = "different-project"

	// Analyze first observation
	_, _ = detector.AnalyzeObservation(ctx, obs1)

	// Check candidate count
	if count := detector.CandidateCount(); count != 1 {
		t.Errorf("Expected 1 candidate after first obs, got %d", count)
	}

	// Analyze second observation
	_, _ = detector.AnalyzeObservation(ctx, obs2)

	// Still 1 candidate (same signature)
	if count := detector.CandidateCount(); count != 1 {
		t.Errorf("Expected 1 candidate after second obs, got %d", count)
	}

	// Check that candidate has both projects
	key := generateCandidateKey([]string{"update", "test"})
	candidate := detector.candidates[key]
	if candidate == nil {
		t.Fatal("Expected candidate to exist")
	}
	if len(candidate.projects) != 2 {
		t.Errorf("Expected 2 projects, got %d", len(candidate.projects))
	}
}

func TestDetector_GetPatternInsight_NotFound(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	patternStore := gorm.NewPatternStore(store)
	observationStore := gorm.NewObservationStore(store, nil, nil, nil)
	config := DefaultConfig()

	detector := NewDetector(patternStore, observationStore, config)
	ctx := context.Background()

	// Try to get insight for non-existent pattern
	_, err := detector.GetPatternInsight(ctx, 99999)
	if err == nil {
		t.Error("Expected error for non-existent pattern")
	}
}

// Helper functions

func setupTestStore(t *testing.T) *gorm.Store {
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

	store, err := gorm.NewStore(gorm.Config{
		Path:     tmpFile.Name(),
		MaxConns: 1,
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
