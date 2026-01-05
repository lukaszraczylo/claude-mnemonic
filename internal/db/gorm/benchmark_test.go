//go:build fts5

// Package gorm provides GORM-based database operations for claude-mnemonic.
package gorm

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"gorm.io/gorm/logger"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
)

// setupBenchStore creates a temporary store for benchmarking.
func setupBenchStore(b *testing.B) (*Store, func()) {
	b.Helper()

	tmpDir, err := os.MkdirTemp("", "gorm_bench_*")
	if err != nil {
		b.Fatalf("create temp dir: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "bench.db")
	cfg := Config{
		Path:     dbPath,
		MaxConns: 4,
		LogLevel: logger.Silent,
	}

	store, err := NewStore(cfg)
	if err != nil {
		os.RemoveAll(tmpDir)
		b.Fatalf("NewStore failed: %v", err)
	}

	cleanup := func() {
		store.Close()
		os.RemoveAll(tmpDir)
	}

	return store, cleanup
}

// BenchmarkSessionStore_CreateSDKSession benchmarks session creation (most frequent operation).
func BenchmarkSessionStore_CreateSDKSession(b *testing.B) {
	store, cleanup := setupBenchStore(b)
	defer cleanup()

	sessionStore := NewSessionStore(store)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sessionID := fmt.Sprintf("claude-bench-%d", i)
		_, err := sessionStore.CreateSDKSession(ctx, sessionID, "bench-project", "test prompt")
		if err != nil {
			b.Fatalf("CreateSDKSession failed: %v", err)
		}
	}
}

// BenchmarkSessionStore_CreateSDKSession_Idempotent benchmarks idempotent session creation (INSERT OR IGNORE).
func BenchmarkSessionStore_CreateSDKSession_Idempotent(b *testing.B) {
	store, cleanup := setupBenchStore(b)
	defer cleanup()

	sessionStore := NewSessionStore(store)
	ctx := context.Background()

	// Pre-create session
	sessionStore.CreateSDKSession(ctx, "claude-bench", "bench-project", "test prompt")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := sessionStore.CreateSDKSession(ctx, "claude-bench", "bench-project", "updated prompt")
		if err != nil {
			b.Fatalf("CreateSDKSession failed: %v", err)
		}
	}
}

// BenchmarkObservationStore_StoreObservation benchmarks observation storage (high frequency).
func BenchmarkObservationStore_StoreObservation(b *testing.B) {
	store, cleanup := setupBenchStore(b)
	defer cleanup()

	sessionStore := NewSessionStore(store)
	obsStore := NewObservationStore(store, nil, nil, nil)
	ctx := context.Background()

	// Create session
	sessionID, _ := sessionStore.CreateSDKSession(ctx, "claude-bench", "bench-project", "")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		obs := &models.ParsedObservation{
			Type:      models.ObsTypeDiscovery,
			Title:     fmt.Sprintf("Observation %d", i),
			Narrative: "Benchmark observation content",
		}
		_, _, err := obsStore.StoreObservation(ctx, "claude-bench", "bench-project", obs, int(sessionID), int64(i+1))
		if err != nil {
			b.Fatalf("StoreObservation failed: %v", err)
		}
	}
}

// BenchmarkObservationStore_GetRecentObservations benchmarks recent observation retrieval.
func BenchmarkObservationStore_GetRecentObservations(b *testing.B) {
	store, cleanup := setupBenchStore(b)
	defer cleanup()

	sessionStore := NewSessionStore(store)
	obsStore := NewObservationStore(store, nil, nil, nil)
	ctx := context.Background()

	// Create session and observations
	sessionID, _ := sessionStore.CreateSDKSession(ctx, "claude-bench", "bench-project", "")
	for i := 0; i < 100; i++ {
		obs := &models.ParsedObservation{
			Type:      models.ObsTypeDiscovery,
			Title:     fmt.Sprintf("Observation %d", i),
			Narrative: "Benchmark observation content",
		}
		obsStore.StoreObservation(ctx, "claude-bench", "bench-project", obs, int(sessionID), int64(i+1))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := obsStore.GetRecentObservations(ctx, "bench-project", 20)
		if err != nil {
			b.Fatalf("GetRecentObservations failed: %v", err)
		}
	}
}

// BenchmarkObservationStore_SearchObservationsFTS benchmarks FTS5 search (latency-sensitive).
func BenchmarkObservationStore_SearchObservationsFTS(b *testing.B) {
	store, cleanup := setupBenchStore(b)
	defer cleanup()

	sessionStore := NewSessionStore(store)
	obsStore := NewObservationStore(store, nil, nil, nil)
	ctx := context.Background()

	// Create session and observations with searchable content
	sessionID, _ := sessionStore.CreateSDKSession(ctx, "claude-bench", "bench-project", "")
	for i := 0; i < 100; i++ {
		obs := &models.ParsedObservation{
			Type:      models.ObsTypeDiscovery,
			Title:     fmt.Sprintf("Security best practice %d", i),
			Narrative: "This observation discusses security patterns and authentication mechanisms",
		}
		obsStore.StoreObservation(ctx, "claude-bench", "bench-project", obs, int(sessionID), int64(i+1))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := obsStore.SearchObservationsFTS(ctx, "security authentication", "bench-project", 10)
		if err != nil {
			b.Fatalf("SearchObservationsFTS failed: %v", err)
		}
	}
}

// BenchmarkObservationStore_UpdateImportanceScore benchmarks scoring updates.
func BenchmarkObservationStore_UpdateImportanceScore(b *testing.B) {
	store, cleanup := setupBenchStore(b)
	defer cleanup()

	sessionStore := NewSessionStore(store)
	obsStore := NewObservationStore(store, nil, nil, nil)
	ctx := context.Background()

	// Create session and observation
	sessionID, _ := sessionStore.CreateSDKSession(ctx, "claude-bench", "bench-project", "")
	obs := &models.ParsedObservation{Type: models.ObsTypeDiscovery, Title: "Test"}
	obsID, _, _ := obsStore.StoreObservation(ctx, "claude-bench", "bench-project", obs, int(sessionID), 1)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		score := float64(i%10) + 1.0
		err := obsStore.UpdateImportanceScore(ctx, obsID, score)
		if err != nil {
			b.Fatalf("UpdateImportanceScore failed: %v", err)
		}
	}
}

// BenchmarkObservationStore_UpdateImportanceScores_Bulk benchmarks bulk scoring updates.
func BenchmarkObservationStore_UpdateImportanceScores_Bulk(b *testing.B) {
	store, cleanup := setupBenchStore(b)
	defer cleanup()

	sessionStore := NewSessionStore(store)
	obsStore := NewObservationStore(store, nil, nil, nil)
	ctx := context.Background()

	// Create session and 100 observations
	sessionID, _ := sessionStore.CreateSDKSession(ctx, "claude-bench", "bench-project", "")
	var obsIDs []int64
	for i := 0; i < 100; i++ {
		obs := &models.ParsedObservation{Type: models.ObsTypeDiscovery, Title: fmt.Sprintf("Obs %d", i)}
		obsID, _, _ := obsStore.StoreObservation(ctx, "claude-bench", "bench-project", obs, int(sessionID), int64(i+1))
		obsIDs = append(obsIDs, obsID)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scores := make(map[int64]float64)
		for _, id := range obsIDs {
			scores[id] = float64(i%10) + 1.0
		}
		err := obsStore.UpdateImportanceScores(ctx, scores)
		if err != nil {
			b.Fatalf("UpdateImportanceScores failed: %v", err)
		}
	}
}

// BenchmarkPromptStore_SaveUserPromptWithMatches benchmarks prompt storage with matches.
func BenchmarkPromptStore_SaveUserPromptWithMatches(b *testing.B) {
	store, cleanup := setupBenchStore(b)
	defer cleanup()

	sessionStore := NewSessionStore(store)
	promptStore := NewPromptStore(store, nil)
	ctx := context.Background()

	// Create session
	sessionID, _ := sessionStore.CreateSDKSession(ctx, "claude-bench", "bench-project", "")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := promptStore.SaveUserPromptWithMatches(ctx, "claude-bench", int(sessionID), fmt.Sprintf("Prompt %d", i), i+1)
		if err != nil {
			b.Fatalf("SaveUserPromptWithMatches failed: %v", err)
		}
	}
}

// BenchmarkSummaryStore_StoreSummary benchmarks summary storage.
func BenchmarkSummaryStore_StoreSummary(b *testing.B) {
	store, cleanup := setupBenchStore(b)
	defer cleanup()

	sessionStore := NewSessionStore(store)
	summaryStore := NewSummaryStore(store)
	ctx := context.Background()

	// Create session
	sessionStore.CreateSDKSession(ctx, "claude-bench", "bench-project", "")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		summary := &models.ParsedSummary{
			Request:      fmt.Sprintf("Request %d", i),
			Investigated: "Investigation details",
			Learned:      "Learning summary",
			Completed:    "Completion status",
		}
		_, _, err := summaryStore.StoreSummary(ctx, "claude-bench", "bench-project", summary, i+1, 100)
		if err != nil {
			b.Fatalf("StoreSummary failed: %v", err)
		}
	}
}

// BenchmarkRelationStore_StoreRelation benchmarks relation storage.
func BenchmarkRelationStore_StoreRelation(b *testing.B) {
	store, cleanup := setupBenchStore(b)
	defer cleanup()

	sessionStore := NewSessionStore(store)
	obsStore := NewObservationStore(store, nil, nil, nil)
	relationStore := NewRelationStore(store)
	ctx := context.Background()

	// Create session and observations
	sessionID, _ := sessionStore.CreateSDKSession(ctx, "claude-bench", "bench-project", "")
	obs1 := &models.ParsedObservation{Type: models.ObsTypeDiscovery, Title: "Source"}
	obsID1, _, _ := obsStore.StoreObservation(ctx, "claude-bench", "bench-project", obs1, int(sessionID), 1)
	obs2 := &models.ParsedObservation{Type: models.ObsTypeDiscovery, Title: "Target"}
	obsID2, _, _ := obsStore.StoreObservation(ctx, "claude-bench", "bench-project", obs2, int(sessionID), 2)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		relation := &models.ObservationRelation{
			SourceID:        obsID1,
			TargetID:        obsID2,
			RelationType:    models.RelationCauses,
			Confidence:      0.9,
			DetectionSource: models.DetectionSourceFileOverlap,
		}
		_, err := relationStore.StoreRelation(ctx, relation)
		if err != nil {
			b.Fatalf("StoreRelation failed: %v", err)
		}
	}
}

// BenchmarkPatternStore_StorePattern benchmarks pattern storage.
func BenchmarkPatternStore_StorePattern(b *testing.B) {
	store, cleanup := setupBenchStore(b)
	defer cleanup()

	patternStore := NewPatternStore(store)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pattern := &models.Pattern{
			Name:        fmt.Sprintf("Pattern %d", i),
			Type:        models.PatternTypeBug,
			Description: sql.NullString{String: "Benchmark pattern", Valid: true},
			Frequency:   1,
			Confidence:  0.8,
			Projects:    []string{"bench-project"},
			Status:      models.PatternStatusActive,
		}
		_, err := patternStore.StorePattern(ctx, pattern)
		if err != nil {
			b.Fatalf("StorePattern failed: %v", err)
		}
	}
}
