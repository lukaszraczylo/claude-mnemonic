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

func testPatternStore(t *testing.T) (*PatternStore, *Store, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "gorm_pattern_test_*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "test.db")
	cfg := Config{
		Path:     dbPath,
		MaxConns: 4,
		LogLevel: logger.Silent,
	}

	store, err := NewStore(cfg)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("NewStore failed: %v", err)
	}

	patternStore := NewPatternStore(store)

	cleanup := func() {
		store.Close()
		os.RemoveAll(tmpDir)
	}

	return patternStore, store, cleanup
}

func TestPatternStore_StorePattern(t *testing.T) {
	patternStore, _, cleanup := testPatternStore(t)
	defer cleanup()
	ctx := context.Background()

	now := time.Now()
	pattern := &models.Pattern{
		Name:           "Test Pattern",
		Type:           models.PatternTypeBug,
		Description:    sql.NullString{String: "Test description", Valid: true},
		Signature:      []string{"bug", "error"},
		Recommendation: sql.NullString{String: "Fix it", Valid: true},
		Frequency:      1,
		Projects:       []string{"test-project"},
		ObservationIDs: []int64{1, 2, 3},
		Status:         models.PatternStatusActive,
		Confidence:     0.8,
		LastSeenAt:     now.Format(time.RFC3339),
		LastSeenEpoch:  now.UnixMilli(),
		CreatedAt:      now.Format(time.RFC3339),
		CreatedAtEpoch: now.UnixMilli(),
	}

	id, err := patternStore.StorePattern(ctx, pattern)
	require.NoError(t, err)
	assert.Greater(t, id, int64(0))

	// Verify pattern was stored
	retrieved, err := patternStore.GetPatternByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, pattern.Name, retrieved.Name)
	assert.Equal(t, pattern.Type, retrieved.Type)
	assert.Equal(t, pattern.Signature, retrieved.Signature)
	assert.Equal(t, pattern.Frequency, retrieved.Frequency)
	assert.Equal(t, pattern.Status, retrieved.Status)
	assert.Equal(t, pattern.Confidence, retrieved.Confidence)
}

func TestPatternStore_UpdatePattern(t *testing.T) {
	patternStore, _, cleanup := testPatternStore(t)
	defer cleanup()
	ctx := context.Background()

	now := time.Now()
	pattern := &models.Pattern{
		Name:           "Original",
		Type:           models.PatternTypeBug,
		Frequency:      1,
		Status:         models.PatternStatusActive,
		Confidence:     0.5,
		LastSeenAt:     now.Format(time.RFC3339),
		LastSeenEpoch:  now.UnixMilli(),
		CreatedAt:      now.Format(time.RFC3339),
		CreatedAtEpoch: now.UnixMilli(),
	}

	id, err := patternStore.StorePattern(ctx, pattern)
	require.NoError(t, err)

	// Update pattern
	pattern.ID = id
	pattern.Name = "Updated"
	pattern.Frequency = 5
	pattern.Confidence = 0.9

	err = patternStore.UpdatePattern(ctx, pattern)
	require.NoError(t, err)

	// Verify update
	retrieved, err := patternStore.GetPatternByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "Updated", retrieved.Name)
	assert.Equal(t, 5, retrieved.Frequency)
	assert.Equal(t, 0.9, retrieved.Confidence)
}

func TestPatternStore_GetPatternByName(t *testing.T) {
	patternStore, _, cleanup := testPatternStore(t)
	defer cleanup()
	ctx := context.Background()

	now := time.Now()
	pattern := &models.Pattern{
		Name:           "Unique Pattern",
		Type:           models.PatternTypeRefactor,
		Frequency:      1,
		Status:         models.PatternStatusActive,
		Confidence:     0.7,
		LastSeenAt:     now.Format(time.RFC3339),
		LastSeenEpoch:  now.UnixMilli(),
		CreatedAt:      now.Format(time.RFC3339),
		CreatedAtEpoch: now.UnixMilli(),
	}

	_, err := patternStore.StorePattern(ctx, pattern)
	require.NoError(t, err)

	// Retrieve by name
	retrieved, err := patternStore.GetPatternByName(ctx, "Unique Pattern")
	require.NoError(t, err)
	require.NotNil(t, retrieved)
	assert.Equal(t, "Unique Pattern", retrieved.Name)
	assert.Equal(t, models.PatternTypeRefactor, retrieved.Type)

	// Non-existent pattern
	notFound, err := patternStore.GetPatternByName(ctx, "Nonexistent")
	require.NoError(t, err)
	assert.Nil(t, notFound)
}

func TestPatternStore_GetActivePatterns(t *testing.T) {
	patternStore, _, cleanup := testPatternStore(t)
	defer cleanup()
	ctx := context.Background()

	now := time.Now()

	// Create active patterns
	for i := 0; i < 3; i++ {
		pattern := &models.Pattern{
			Name:           "Pattern " + string(rune('A'+i)),
			Type:           models.PatternTypeBug,
			Frequency:      i + 1, // Different frequencies for sorting
			Status:         models.PatternStatusActive,
			Confidence:     0.8,
			LastSeenAt:     now.Format(time.RFC3339),
			LastSeenEpoch:  now.UnixMilli(),
			CreatedAt:      now.Format(time.RFC3339),
			CreatedAtEpoch: now.UnixMilli(),
		}
		_, err := patternStore.StorePattern(ctx, pattern)
		require.NoError(t, err)
	}

	// Create deprecated pattern (should not be included)
	deprecatedPattern := &models.Pattern{
		Name:           "Deprecated Pattern",
		Type:           models.PatternTypeBug,
		Frequency:      100,
		Status:         models.PatternStatusDeprecated,
		Confidence:     0.9,
		LastSeenAt:     now.Format(time.RFC3339),
		LastSeenEpoch:  now.UnixMilli(),
		CreatedAt:      now.Format(time.RFC3339),
		CreatedAtEpoch: now.UnixMilli(),
	}
	_, err := patternStore.StorePattern(ctx, deprecatedPattern)
	require.NoError(t, err)

	// Get active patterns
	patterns, err := patternStore.GetActivePatterns(ctx, 10)
	require.NoError(t, err)
	assert.Len(t, patterns, 3) // Only active patterns

	// Verify sorted by frequency DESC
	assert.Equal(t, 3, patterns[0].Frequency)
	assert.Equal(t, 2, patterns[1].Frequency)
	assert.Equal(t, 1, patterns[2].Frequency)
}

func TestPatternStore_GetPatternsByType(t *testing.T) {
	patternStore, _, cleanup := testPatternStore(t)
	defer cleanup()
	ctx := context.Background()

	now := time.Now()

	// Create patterns of different types
	bugPattern := &models.Pattern{
		Name:           "Bug Pattern",
		Type:           models.PatternTypeBug,
		Frequency:      1,
		Status:         models.PatternStatusActive,
		Confidence:     0.8,
		LastSeenAt:     now.Format(time.RFC3339),
		LastSeenEpoch:  now.UnixMilli(),
		CreatedAt:      now.Format(time.RFC3339),
		CreatedAtEpoch: now.UnixMilli(),
	}
	_, err := patternStore.StorePattern(ctx, bugPattern)
	require.NoError(t, err)

	refactorPattern := &models.Pattern{
		Name:           "Refactor Pattern",
		Type:           models.PatternTypeRefactor,
		Frequency:      1,
		Status:         models.PatternStatusActive,
		Confidence:     0.7,
		LastSeenAt:     now.Format(time.RFC3339),
		LastSeenEpoch:  now.UnixMilli(),
		CreatedAt:      now.Format(time.RFC3339),
		CreatedAtEpoch: now.UnixMilli(),
	}
	_, err = patternStore.StorePattern(ctx, refactorPattern)
	require.NoError(t, err)

	// Get only bug patterns
	bugPatterns, err := patternStore.GetPatternsByType(ctx, models.PatternTypeBug, 10)
	require.NoError(t, err)
	assert.Len(t, bugPatterns, 1)
	assert.Equal(t, "Bug Pattern", bugPatterns[0].Name)
	assert.Equal(t, models.PatternTypeBug, bugPatterns[0].Type)

	// Get only refactor patterns
	refactorPatterns, err := patternStore.GetPatternsByType(ctx, models.PatternTypeRefactor, 10)
	require.NoError(t, err)
	assert.Len(t, refactorPatterns, 1)
	assert.Equal(t, "Refactor Pattern", refactorPatterns[0].Name)
}

func TestPatternStore_MarkPatternDeprecated(t *testing.T) {
	patternStore, _, cleanup := testPatternStore(t)
	defer cleanup()
	ctx := context.Background()

	now := time.Now()
	pattern := &models.Pattern{
		Name:           "To Deprecate",
		Type:           models.PatternTypeBug,
		Frequency:      1,
		Status:         models.PatternStatusActive,
		Confidence:     0.5,
		LastSeenAt:     now.Format(time.RFC3339),
		LastSeenEpoch:  now.UnixMilli(),
		CreatedAt:      now.Format(time.RFC3339),
		CreatedAtEpoch: now.UnixMilli(),
	}

	id, err := patternStore.StorePattern(ctx, pattern)
	require.NoError(t, err)

	// Mark as deprecated
	err = patternStore.MarkPatternDeprecated(ctx, id)
	require.NoError(t, err)

	// Verify status changed
	retrieved, err := patternStore.GetPatternByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, models.PatternStatusDeprecated, retrieved.Status)
}

func TestPatternStore_MergePatterns(t *testing.T) {
	patternStore, _, cleanup := testPatternStore(t)
	defer cleanup()
	ctx := context.Background()

	now := time.Now()

	// Create source pattern
	source := &models.Pattern{
		Name:           "Source Pattern",
		Type:           models.PatternTypeBug,
		Frequency:      5,
		Projects:       []string{"project-a", "project-b"},
		ObservationIDs: []int64{1, 2, 3},
		Status:         models.PatternStatusActive,
		Confidence:     0.7,
		LastSeenAt:     now.Format(time.RFC3339),
		LastSeenEpoch:  now.UnixMilli(),
		CreatedAt:      now.Format(time.RFC3339),
		CreatedAtEpoch: now.UnixMilli(),
	}
	sourceID, err := patternStore.StorePattern(ctx, source)
	require.NoError(t, err)

	// Create target pattern
	target := &models.Pattern{
		Name:           "Target Pattern",
		Type:           models.PatternTypeBug,
		Frequency:      10,
		Projects:       []string{"project-b", "project-c"},
		ObservationIDs: []int64{3, 4, 5},
		Status:         models.PatternStatusActive,
		Confidence:     0.8,
		LastSeenAt:     now.Format(time.RFC3339),
		LastSeenEpoch:  now.UnixMilli(),
		CreatedAt:      now.Format(time.RFC3339),
		CreatedAtEpoch: now.UnixMilli(),
	}
	targetID, err := patternStore.StorePattern(ctx, target)
	require.NoError(t, err)

	// Merge source into target
	err = patternStore.MergePatterns(ctx, sourceID, targetID)
	require.NoError(t, err)

	// Verify target was updated
	mergedTarget, err := patternStore.GetPatternByID(ctx, targetID)
	require.NoError(t, err)
	assert.Equal(t, 15, mergedTarget.Frequency) // 5 + 10
	assert.ElementsMatch(t, []string{"project-a", "project-b", "project-c"}, mergedTarget.Projects)
	assert.ElementsMatch(t, []int64{1, 2, 3, 4, 5}, mergedTarget.ObservationIDs)

	// Verify source was marked as merged
	mergedSource, err := patternStore.GetPatternByID(ctx, sourceID)
	require.NoError(t, err)
	assert.Equal(t, models.PatternStatusMerged, mergedSource.Status)
	assert.True(t, mergedSource.MergedIntoID.Valid)
	assert.Equal(t, targetID, mergedSource.MergedIntoID.Int64)
}

func TestPatternStore_DeletePattern(t *testing.T) {
	patternStore, _, cleanup := testPatternStore(t)
	defer cleanup()
	ctx := context.Background()

	now := time.Now()
	pattern := &models.Pattern{
		Name:           "To Delete",
		Type:           models.PatternTypeBug,
		Frequency:      1,
		Status:         models.PatternStatusActive,
		Confidence:     0.5,
		LastSeenAt:     now.Format(time.RFC3339),
		LastSeenEpoch:  now.UnixMilli(),
		CreatedAt:      now.Format(time.RFC3339),
		CreatedAtEpoch: now.UnixMilli(),
	}

	id, err := patternStore.StorePattern(ctx, pattern)
	require.NoError(t, err)

	// Delete pattern
	err = patternStore.DeletePattern(ctx, id)
	require.NoError(t, err)

	// Verify deleted
	deleted, err := patternStore.GetPatternByID(ctx, id)
	require.NoError(t, err)
	assert.Nil(t, deleted)
}

func TestPatternStore_IncrementPatternFrequency(t *testing.T) {
	patternStore, _, cleanup := testPatternStore(t)
	defer cleanup()
	ctx := context.Background()

	now := time.Now()
	pattern := &models.Pattern{
		Name:           "Frequency Test",
		Type:           models.PatternTypeBug,
		Frequency:      1,
		Projects:       []string{"project-a"},
		ObservationIDs: []int64{},
		Status:         models.PatternStatusActive,
		Confidence:     0.7,
		LastSeenAt:     now.Format(time.RFC3339),
		LastSeenEpoch:  now.UnixMilli(),
		CreatedAt:      now.Format(time.RFC3339),
		CreatedAtEpoch: now.UnixMilli(),
	}

	id, err := patternStore.StorePattern(ctx, pattern)
	require.NoError(t, err)

	// Increment frequency with new project and observation
	err = patternStore.IncrementPatternFrequency(ctx, id, "project-b", 42)
	require.NoError(t, err)

	// Verify frequency incremented and new data added
	updated, err := patternStore.GetPatternByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, 2, updated.Frequency)
	assert.ElementsMatch(t, []string{"project-a", "project-b"}, updated.Projects)
	assert.Contains(t, updated.ObservationIDs, int64(42))

	// Last seen should be updated (rough check - within last 5 seconds)
	updatedTime, _ := time.Parse(time.RFC3339, updated.LastSeenAt)
	assert.WithinDuration(t, time.Now(), updatedTime, 5*time.Second)
}

func TestPatternStore_GetPatternStats(t *testing.T) {
	patternStore, _, cleanup := testPatternStore(t)
	defer cleanup()
	ctx := context.Background()

	now := time.Now()

	// Create patterns with different statuses and types
	patterns := []*models.Pattern{
		{
			Name:           "Bug 1",
			Type:           models.PatternTypeBug,
			Frequency:      10,
			Status:         models.PatternStatusActive,
			Confidence:     0.8,
			LastSeenAt:     now.Format(time.RFC3339),
			LastSeenEpoch:  now.UnixMilli(),
			CreatedAt:      now.Format(time.RFC3339),
			CreatedAtEpoch: now.UnixMilli(),
		},
		{
			Name:           "Refactor 1",
			Type:           models.PatternTypeRefactor,
			Frequency:      5,
			Status:         models.PatternStatusActive,
			Confidence:     0.7,
			LastSeenAt:     now.Format(time.RFC3339),
			LastSeenEpoch:  now.UnixMilli(),
			CreatedAt:      now.Format(time.RFC3339),
			CreatedAtEpoch: now.UnixMilli(),
		},
		{
			Name:           "Deprecated 1",
			Type:           models.PatternTypeBestPractice,
			Frequency:      3,
			Status:         models.PatternStatusDeprecated,
			Confidence:     0.6,
			LastSeenAt:     now.Format(time.RFC3339),
			LastSeenEpoch:  now.UnixMilli(),
			CreatedAt:      now.Format(time.RFC3339),
			CreatedAtEpoch: now.UnixMilli(),
		},
	}

	for _, p := range patterns {
		_, err := patternStore.StorePattern(ctx, p)
		require.NoError(t, err)
	}

	// Get stats
	stats, err := patternStore.GetPatternStats(ctx)
	require.NoError(t, err)

	assert.Equal(t, 3, stats.Total)
	assert.Equal(t, 2, stats.Active)
	assert.Equal(t, 1, stats.Deprecated)
	assert.Equal(t, 0, stats.Merged)
	assert.Equal(t, 18, stats.TotalOccurrences)       // 10 + 5 + 3
	assert.InDelta(t, 0.7, stats.AvgConfidence, 0.05) // (0.8 + 0.7 + 0.6) / 3
	assert.Equal(t, 1, stats.Bugs)
	assert.Equal(t, 1, stats.Refactors)
	assert.Equal(t, 1, stats.BestPractices)
}
