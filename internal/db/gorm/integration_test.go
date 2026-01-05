//go:build fts5

// Package gorm provides GORM-based database operations for claude-mnemonic.
package gorm

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm/logger"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
)

// TestIntegration_EndToEndWorkflow verifies a complete workflow
// simulating real usage of the GORM package.
func TestIntegration_EndToEndWorkflow(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gorm_integration_test_*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	cfg := Config{
		Path:     dbPath,
		MaxConns: 4,
		LogLevel: logger.Silent,
	}

	// Step 1: Initialize store
	store, err := NewStore(cfg)
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	// Step 2: Create all store types
	sessionStore := NewSessionStore(store)
	summaryStore := NewSummaryStore(store)
	conflictStore := NewConflictStore(store)
	relationStore := NewRelationStore(store)
	patternStore := NewPatternStore(store)

	// Create observation store with dependencies
	observationStore := NewObservationStore(store, nil, conflictStore, relationStore)
	promptStore := NewPromptStore(store, nil)

	// Step 3: Create a session
	sessionID, err := sessionStore.CreateSDKSession(ctx, "claude-test", "test-project", "")
	require.NoError(t, err)
	assert.Greater(t, sessionID, int64(0))

	// Step 4: Store observations
	obs1 := &models.ParsedObservation{
		Type:     models.ObsTypeDiscovery,
		Title:    "Test Discovery",
		Subtitle: "Testing GORM integration",
		Facts:    []string{"Fact 1", "Fact 2"},
		Concepts: []string{"testing", "integration"},
	}

	obsID1, _, err := observationStore.StoreObservation(ctx, "claude-test", "test-project", obs1, int(sessionID), 1)
	require.NoError(t, err)
	assert.Greater(t, obsID1, int64(0))

	obs2 := &models.ParsedObservation{
		Type:     models.ObsTypeBugfix,
		Title:    "Test Bugfix",
		Facts:    []string{"Fixed bug"},
		Concepts: []string{"bugfix"},
	}

	obsID2, _, err := observationStore.StoreObservation(ctx, "claude-test", "test-project", obs2, int(sessionID), 2)
	require.NoError(t, err)
	assert.Greater(t, obsID2, int64(0))

	// Step 5: Create relations
	now := time.Now()
	relation := &models.ObservationRelation{
		SourceID:        obsID1,
		TargetID:        obsID2,
		RelationType:    models.RelationCauses,
		Confidence:      0.8,
		DetectionSource: models.DetectionSourceFileOverlap,
		CreatedAt:       now.Format(time.RFC3339),
		CreatedAtEpoch:  now.UnixMilli(),
	}

	relID, err := relationStore.StoreRelation(ctx, relation)
	require.NoError(t, err)
	assert.Greater(t, relID, int64(0))

	// Step 6: Update importance scores
	err = observationStore.UpdateImportanceScore(ctx, obsID1, 5.0)
	require.NoError(t, err)

	// Step 7: Increment retrieval counts
	err = observationStore.IncrementRetrievalCount(ctx, []int64{obsID1, obsID2})
	require.NoError(t, err)

	// Step 8: Create a pattern
	pattern := &models.Pattern{
		Name:           "Test Pattern",
		Type:           models.PatternTypeBug,
		Signature:      []string{"bug", "fix"},
		Frequency:      1,
		Projects:       []string{"test-project"},
		ObservationIDs: []int64{obsID1, obsID2},
		Status:         models.PatternStatusActive,
		Confidence:     0.75,
		LastSeenAt:     now.Format(time.RFC3339),
		LastSeenEpoch:  now.UnixMilli(),
		CreatedAt:      now.Format(time.RFC3339),
		CreatedAtEpoch: now.UnixMilli(),
	}

	patternID, err := patternStore.StorePattern(ctx, pattern)
	require.NoError(t, err)
	assert.Greater(t, patternID, int64(0))

	// Step 9: Store a prompt
	promptID, err := promptStore.SaveUserPromptWithMatches(ctx, "claude-test", 1, "Test prompt", 2)
	require.NoError(t, err)
	assert.Greater(t, promptID, int64(0))

	// Step 10: Store a summary
	summary := &models.ParsedSummary{
		Request:      "Test request",
		Investigated: "Test investigation",
		Learned:      "Test learning",
		Completed:    "Test completion",
		NextSteps:    "Test next steps",
		Notes:        "Test notes",
	}

	summaryID, _, err := summaryStore.StoreSummary(ctx, "claude-test", "test-project", summary, 1, 100)
	require.NoError(t, err)
	assert.Greater(t, summaryID, int64(0))

	// Step 11: Verify data retrieval
	retrievedObs, err := observationStore.GetObservationByID(ctx, obsID1)
	require.NoError(t, err)
	require.NotNil(t, retrievedObs)
	assert.Equal(t, "Test Discovery", retrievedObs.Title.String)
	assert.Equal(t, 5.0, retrievedObs.ImportanceScore)
	assert.Equal(t, 1, retrievedObs.RetrievalCount)

	// Step 12: Verify relations
	relations, err := relationStore.GetRelationsByObservationID(ctx, obsID1)
	require.NoError(t, err)
	assert.Len(t, relations, 1)
	assert.Equal(t, obsID2, relations[0].TargetID)

	// Step 13: Verify pattern
	retrievedPattern, err := patternStore.GetPatternByID(ctx, patternID)
	require.NoError(t, err)
	require.NotNil(t, retrievedPattern)
	assert.Equal(t, "Test Pattern", retrievedPattern.Name)

	// Step 14: Verify stats
	stats, err := observationStore.GetObservationFeedbackStats(ctx, "test-project")
	require.NoError(t, err)
	assert.Equal(t, 2, stats.Total)

	t.Log("✅ End-to-end integration test passed!")
}

// TestIntegration_StoreCompatibility verifies that Store methods work correctly.
func TestIntegration_StoreCompatibility(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gorm_store_test_*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	cfg := Config{
		Path:     dbPath,
		MaxConns: 4,
		LogLevel: logger.Silent,
	}

	store, err := NewStore(cfg)
	require.NoError(t, err)
	defer store.Close()

	// Verify raw DB access (needed for vector client)
	rawDB := store.GetRawDB()
	require.NotNil(t, rawDB)
	assert.IsType(t, &sql.DB{}, rawDB)

	// Verify GORM DB access
	gormDB := store.GetDB()
	require.NotNil(t, gormDB)

	// Verify Close works
	err = store.Close()
	require.NoError(t, err)
}

// TestIntegration_ConcurrentAccess verifies thread-safe operations.
func TestIntegration_ConcurrentAccess(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gorm_concurrent_test_*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	cfg := Config{
		Path:     dbPath,
		MaxConns: 4,
		LogLevel: logger.Silent,
	}

	store, err := NewStore(cfg)
	require.NoError(t, err)
	defer store.Close()

	sessionStore := NewSessionStore(store)
	ctx := context.Background()

	// Create session
	sessionID, err := sessionStore.CreateSDKSession(ctx, "claude-concurrent", "test-project", "")
	require.NoError(t, err)

	// Concurrent prompt counter increments
	done := make(chan bool)
	numGoroutines := 10

	for i := 0; i < numGoroutines; i++ {
		go func() {
			_, err := sessionStore.IncrementPromptCounter(ctx, sessionID)
			assert.NoError(t, err)
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Verify final count
	session, err := sessionStore.GetSessionByID(ctx, sessionID)
	require.NoError(t, err)
	assert.Equal(t, int64(numGoroutines), int64(session.PromptCounter))

	t.Log("✅ Concurrent access test passed!")
}

// TestIntegration_WALMode verifies WAL mode is enabled.
func TestIntegration_WALMode(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gorm_wal_test_*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	cfg := Config{
		Path:     dbPath,
		MaxConns: 4,
		LogLevel: logger.Silent,
	}

	store, err := NewStore(cfg)
	require.NoError(t, err)
	defer store.Close()

	// Check WAL mode via raw SQL
	var journalMode string
	err = store.GetRawDB().QueryRow("PRAGMA journal_mode").Scan(&journalMode)
	require.NoError(t, err)
	assert.Equal(t, "wal", journalMode, "WAL mode should be enabled")

	t.Log("✅ WAL mode verification passed!")
}

// TestIntegration_FTS5Search verifies FTS5 functionality.
func TestIntegration_FTS5Search(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gorm_fts5_test_*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	cfg := Config{
		Path:     dbPath,
		MaxConns: 4,
		LogLevel: logger.Silent,
	}

	store, err := NewStore(cfg)
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	sessionStore := NewSessionStore(store)
	observationStore := NewObservationStore(store, nil, nil, nil)

	// Create session
	sessionID, _ := sessionStore.CreateSDKSession(ctx, "claude-fts5", "test-project", "")

	// Store observations with searchable text
	obs1 := &models.ParsedObservation{
		Type:     models.ObsTypeDiscovery,
		Title:    "Database optimization techniques",
		Subtitle: "Improving query performance",
		Facts:    []string{"Use indexes", "Optimize queries"},
		Concepts: []string{"performance", "optimization"},
	}

	obsID1, _, _ := observationStore.StoreObservation(ctx, "claude-fts5", "test-project", obs1, int(sessionID), 1)

	obs2 := &models.ParsedObservation{
		Type:     models.ObsTypeBugfix,
		Title:    "Fixed memory leak",
		Facts:    []string{"Closed connections properly"},
		Concepts: []string{"bugfix", "memory"},
	}

	observationStore.StoreObservation(ctx, "claude-fts5", "test-project", obs2, int(sessionID), 2)

	// Give FTS5 triggers time to process
	time.Sleep(100 * time.Millisecond)

	// Search using FTS5
	results, err := observationStore.SearchObservationsFTS(ctx, "optimization", "test-project", 10)
	require.NoError(t, err)

	// Should find the optimization observation
	assert.NotEmpty(t, results, "FTS5 search should return results")

	found := false
	for _, obs := range results {
		if obs.ID == obsID1 {
			found = true
			break
		}
	}
	assert.True(t, found, "FTS5 should find the optimization observation")

	t.Log("✅ FTS5 search test passed!")
}
