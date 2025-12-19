package sqlite

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
)

// setupPatternTestStore creates a test store with patterns table.
func setupPatternTestStore(t *testing.T) *Store {
	t.Helper()
	db, _, cleanup := testDB(t)
	t.Cleanup(cleanup)
	createBaseTables(t, db)
	return newStoreFromDB(db)
}

func TestPatternStore_StoreAndGet(t *testing.T) {
	store := setupPatternTestStore(t)

	patternStore := NewPatternStore(store)
	ctx := context.Background()

	// Create test pattern
	pattern := &models.Pattern{
		Name:           "Test Pattern",
		Type:           models.PatternTypeBug,
		Description:    sql.NullString{String: "A test pattern", Valid: true},
		Signature:      []string{"nil", "error"},
		Recommendation: sql.NullString{String: "Always check for nil", Valid: true},
		Frequency:      1,
		Projects:       []string{"project1"},
		ObservationIDs: []int64{1, 2},
		Status:         models.PatternStatusActive,
		Confidence:     0.5,
		LastSeenAt:     time.Now().Format(time.RFC3339),
		LastSeenEpoch:  time.Now().UnixMilli(),
		CreatedAt:      time.Now().Format(time.RFC3339),
		CreatedAtEpoch: time.Now().UnixMilli(),
	}

	// Store pattern
	id, err := patternStore.StorePattern(ctx, pattern)
	if err != nil {
		t.Fatalf("StorePattern() error = %v", err)
	}
	if id <= 0 {
		t.Errorf("Expected positive ID, got %d", id)
	}

	// Get pattern by ID
	retrieved, err := patternStore.GetPatternByID(ctx, id)
	if err != nil {
		t.Fatalf("GetPatternByID() error = %v", err)
	}

	if retrieved.Name != pattern.Name {
		t.Errorf("Expected name %s, got %s", pattern.Name, retrieved.Name)
	}
	if retrieved.Type != pattern.Type {
		t.Errorf("Expected type %s, got %s", pattern.Type, retrieved.Type)
	}
	if len(retrieved.Signature) != len(pattern.Signature) {
		t.Errorf("Expected %d signature elements, got %d",
			len(pattern.Signature), len(retrieved.Signature))
	}
}

func TestPatternStore_GetByName(t *testing.T) {
	store := setupPatternTestStore(t)

	patternStore := NewPatternStore(store)
	ctx := context.Background()

	pattern := createTestPattern("Unique Name Pattern")
	_, err := patternStore.StorePattern(ctx, pattern)
	if err != nil {
		t.Fatalf("StorePattern() error = %v", err)
	}

	// Get by name
	retrieved, err := patternStore.GetPatternByName(ctx, "Unique Name Pattern")
	if err != nil {
		t.Fatalf("GetPatternByName() error = %v", err)
	}
	if retrieved == nil {
		t.Fatal("Expected pattern, got nil")
	}
	if retrieved.Name != "Unique Name Pattern" {
		t.Errorf("Expected name 'Unique Name Pattern', got '%s'", retrieved.Name)
	}

	// Get non-existent pattern
	nonExistent, err := patternStore.GetPatternByName(ctx, "Non Existent")
	if err != nil {
		t.Fatalf("GetPatternByName() error = %v", err)
	}
	if nonExistent != nil {
		t.Errorf("Expected nil for non-existent pattern, got %v", nonExistent)
	}
}

func TestPatternStore_GetActivePatterns(t *testing.T) {
	store := setupPatternTestStore(t)

	patternStore := NewPatternStore(store)
	ctx := context.Background()

	// Create multiple patterns with different statuses
	active1 := createTestPattern("Active 1")
	active1.Frequency = 5
	active2 := createTestPattern("Active 2")
	active2.Frequency = 3
	deprecated := createTestPattern("Deprecated")
	deprecated.Status = models.PatternStatusDeprecated

	patternStore.StorePattern(ctx, active1)
	patternStore.StorePattern(ctx, active2)
	patternStore.StorePattern(ctx, deprecated)

	// Get active patterns
	patterns, err := patternStore.GetActivePatterns(ctx, 10)
	if err != nil {
		t.Fatalf("GetActivePatterns() error = %v", err)
	}

	if len(patterns) != 2 {
		t.Errorf("Expected 2 active patterns, got %d", len(patterns))
	}

	// Check order (should be by frequency descending)
	if len(patterns) >= 2 {
		if patterns[0].Frequency < patterns[1].Frequency {
			t.Errorf("Patterns not ordered by frequency descending")
		}
	}
}

func TestPatternStore_GetPatternsByType(t *testing.T) {
	store := setupPatternTestStore(t)

	patternStore := NewPatternStore(store)
	ctx := context.Background()

	// Create patterns of different types
	bugPattern := createTestPattern("Bug Pattern")
	bugPattern.Type = models.PatternTypeBug

	refactorPattern := createTestPattern("Refactor Pattern")
	refactorPattern.Type = models.PatternTypeRefactor

	patternStore.StorePattern(ctx, bugPattern)
	patternStore.StorePattern(ctx, refactorPattern)

	// Get by type
	bugs, err := patternStore.GetPatternsByType(ctx, models.PatternTypeBug, 10)
	if err != nil {
		t.Fatalf("GetPatternsByType() error = %v", err)
	}
	if len(bugs) != 1 {
		t.Errorf("Expected 1 bug pattern, got %d", len(bugs))
	}

	refactors, err := patternStore.GetPatternsByType(ctx, models.PatternTypeRefactor, 10)
	if err != nil {
		t.Fatalf("GetPatternsByType() error = %v", err)
	}
	if len(refactors) != 1 {
		t.Errorf("Expected 1 refactor pattern, got %d", len(refactors))
	}
}

func TestPatternStore_GetPatternsByProject(t *testing.T) {
	store := setupPatternTestStore(t)

	patternStore := NewPatternStore(store)
	ctx := context.Background()

	// Create patterns with different projects
	pattern1 := createTestPattern("Pattern 1")
	pattern1.Projects = []string{"project-a", "project-b"}

	pattern2 := createTestPattern("Pattern 2")
	pattern2.Projects = []string{"project-b", "project-c"}

	patternStore.StorePattern(ctx, pattern1)
	patternStore.StorePattern(ctx, pattern2)

	// Get by project
	projectA, err := patternStore.GetPatternsByProject(ctx, "project-a", 10)
	if err != nil {
		t.Fatalf("GetPatternsByProject() error = %v", err)
	}
	if len(projectA) != 1 {
		t.Errorf("Expected 1 pattern for project-a, got %d", len(projectA))
	}

	projectB, err := patternStore.GetPatternsByProject(ctx, "project-b", 10)
	if err != nil {
		t.Fatalf("GetPatternsByProject() error = %v", err)
	}
	if len(projectB) != 2 {
		t.Errorf("Expected 2 patterns for project-b, got %d", len(projectB))
	}
}

func TestPatternStore_UpdatePattern(t *testing.T) {
	store := setupPatternTestStore(t)

	patternStore := NewPatternStore(store)
	ctx := context.Background()

	// Create and store pattern
	pattern := createTestPattern("Original Name")
	id, _ := patternStore.StorePattern(ctx, pattern)

	// Update pattern
	pattern.ID = id
	pattern.Name = "Updated Name"
	pattern.Frequency = 10
	pattern.Confidence = 0.9

	err := patternStore.UpdatePattern(ctx, pattern)
	if err != nil {
		t.Fatalf("UpdatePattern() error = %v", err)
	}

	// Verify update
	updated, _ := patternStore.GetPatternByID(ctx, id)
	if updated.Name != "Updated Name" {
		t.Errorf("Expected name 'Updated Name', got '%s'", updated.Name)
	}
	if updated.Frequency != 10 {
		t.Errorf("Expected frequency 10, got %d", updated.Frequency)
	}
	if updated.Confidence != 0.9 {
		t.Errorf("Expected confidence 0.9, got %f", updated.Confidence)
	}
}

func TestPatternStore_DeletePattern(t *testing.T) {
	store := setupPatternTestStore(t)

	patternStore := NewPatternStore(store)
	ctx := context.Background()

	// Create and store pattern
	pattern := createTestPattern("To Delete")
	id, _ := patternStore.StorePattern(ctx, pattern)

	// Delete pattern
	err := patternStore.DeletePattern(ctx, id)
	if err != nil {
		t.Fatalf("DeletePattern() error = %v", err)
	}

	// Verify deletion
	deleted, err := patternStore.GetPatternByID(ctx, id)
	if err != sql.ErrNoRows {
		t.Errorf("Expected ErrNoRows, got %v", err)
	}
	if deleted != nil {
		t.Errorf("Expected nil for deleted pattern")
	}
}

func TestPatternStore_MarkPatternDeprecated(t *testing.T) {
	store := setupPatternTestStore(t)

	patternStore := NewPatternStore(store)
	ctx := context.Background()

	// Create and store pattern
	pattern := createTestPattern("To Deprecate")
	id, _ := patternStore.StorePattern(ctx, pattern)

	// Mark as deprecated
	err := patternStore.MarkPatternDeprecated(ctx, id)
	if err != nil {
		t.Fatalf("MarkPatternDeprecated() error = %v", err)
	}

	// Verify status
	deprecated, _ := patternStore.GetPatternByID(ctx, id)
	if deprecated.Status != models.PatternStatusDeprecated {
		t.Errorf("Expected status 'deprecated', got '%s'", deprecated.Status)
	}
}

func TestPatternStore_MergePatterns(t *testing.T) {
	store := setupPatternTestStore(t)

	patternStore := NewPatternStore(store)
	ctx := context.Background()

	// Create source and target patterns
	source := createTestPattern("Source Pattern")
	source.Frequency = 3
	source.Projects = []string{"proj1", "proj2"}
	source.ObservationIDs = []int64{1, 2, 3}

	target := createTestPattern("Target Pattern")
	target.Frequency = 2
	target.Projects = []string{"proj2", "proj3"}
	target.ObservationIDs = []int64{4, 5}

	sourceID, _ := patternStore.StorePattern(ctx, source)
	targetID, _ := patternStore.StorePattern(ctx, target)

	// Merge
	err := patternStore.MergePatterns(ctx, sourceID, targetID)
	if err != nil {
		t.Fatalf("MergePatterns() error = %v", err)
	}

	// Verify source is marked as merged
	mergedSource, _ := patternStore.GetPatternByID(ctx, sourceID)
	if mergedSource.Status != models.PatternStatusMerged {
		t.Errorf("Expected source status 'merged', got '%s'", mergedSource.Status)
	}
	if !mergedSource.MergedIntoID.Valid || mergedSource.MergedIntoID.Int64 != targetID {
		t.Errorf("Expected source merged_into_id to be %d", targetID)
	}

	// Verify target has combined data
	mergedTarget, _ := patternStore.GetPatternByID(ctx, targetID)
	expectedFrequency := 5 // 3 + 2
	if mergedTarget.Frequency != expectedFrequency {
		t.Errorf("Expected merged frequency %d, got %d", expectedFrequency, mergedTarget.Frequency)
	}
	// Should have 3 unique projects: proj1, proj2, proj3
	if len(mergedTarget.Projects) != 3 {
		t.Errorf("Expected 3 projects after merge, got %d", len(mergedTarget.Projects))
	}
	// Should have 5 observation IDs
	if len(mergedTarget.ObservationIDs) != 5 {
		t.Errorf("Expected 5 observation IDs after merge, got %d", len(mergedTarget.ObservationIDs))
	}
}

func TestPatternStore_FindMatchingPatterns(t *testing.T) {
	store := setupPatternTestStore(t)

	patternStore := NewPatternStore(store)
	ctx := context.Background()

	// Create patterns with known signatures
	pattern1 := createTestPattern("Pattern 1")
	pattern1.Signature = []string{"nil", "error", "handling"}

	pattern2 := createTestPattern("Pattern 2")
	pattern2.Signature = []string{"nil", "pointer", "check"}

	pattern3 := createTestPattern("Pattern 3")
	pattern3.Signature = []string{"refactor", "extract", "method"}

	patternStore.StorePattern(ctx, pattern1)
	patternStore.StorePattern(ctx, pattern2)
	patternStore.StorePattern(ctx, pattern3)

	// Find patterns matching "nil" related signature
	matches, err := patternStore.FindMatchingPatterns(ctx, []string{"nil", "error"}, 0.3)
	if err != nil {
		t.Fatalf("FindMatchingPatterns() error = %v", err)
	}

	if len(matches) < 1 {
		t.Errorf("Expected at least 1 match, got %d", len(matches))
	}

	// Verify no match for unrelated signature
	noMatches, err := patternStore.FindMatchingPatterns(ctx, []string{"completely", "different"}, 0.5)
	if err != nil {
		t.Fatalf("FindMatchingPatterns() error = %v", err)
	}
	if len(noMatches) != 0 {
		t.Errorf("Expected 0 matches for unrelated signature, got %d", len(noMatches))
	}
}

func TestPatternStore_IncrementPatternFrequency(t *testing.T) {
	store := setupPatternTestStore(t)

	patternStore := NewPatternStore(store)
	ctx := context.Background()

	// Create pattern
	pattern := createTestPattern("Frequency Test")
	pattern.Frequency = 1
	pattern.Projects = []string{"proj1"}
	pattern.ObservationIDs = []int64{1}

	id, _ := patternStore.StorePattern(ctx, pattern)

	// Increment frequency
	err := patternStore.IncrementPatternFrequency(ctx, id, "proj2", 2)
	if err != nil {
		t.Fatalf("IncrementPatternFrequency() error = %v", err)
	}

	// Verify
	updated, _ := patternStore.GetPatternByID(ctx, id)
	if updated.Frequency != 2 {
		t.Errorf("Expected frequency 2, got %d", updated.Frequency)
	}
	if len(updated.Projects) != 2 {
		t.Errorf("Expected 2 projects, got %d", len(updated.Projects))
	}
	if len(updated.ObservationIDs) != 2 {
		t.Errorf("Expected 2 observation IDs, got %d", len(updated.ObservationIDs))
	}
}

func TestPatternStore_GetPatternStats(t *testing.T) {
	store := setupPatternTestStore(t)

	patternStore := NewPatternStore(store)
	ctx := context.Background()

	// Create patterns with different types and statuses
	bug := createTestPattern("Bug")
	bug.Type = models.PatternTypeBug
	bug.Frequency = 5

	refactor := createTestPattern("Refactor")
	refactor.Type = models.PatternTypeRefactor
	refactor.Frequency = 3

	deprecated := createTestPattern("Deprecated")
	deprecated.Type = models.PatternTypeArchitecture
	deprecated.Status = models.PatternStatusDeprecated

	patternStore.StorePattern(ctx, bug)
	patternStore.StorePattern(ctx, refactor)
	patternStore.StorePattern(ctx, deprecated)

	// Get stats
	stats, err := patternStore.GetPatternStats(ctx)
	if err != nil {
		t.Fatalf("GetPatternStats() error = %v", err)
	}

	if stats.Total != 3 {
		t.Errorf("Expected total 3, got %d", stats.Total)
	}
	if stats.Active != 2 {
		t.Errorf("Expected 2 active, got %d", stats.Active)
	}
	if stats.Deprecated != 1 {
		t.Errorf("Expected 1 deprecated, got %d", stats.Deprecated)
	}
	if stats.Bugs != 1 {
		t.Errorf("Expected 1 bug, got %d", stats.Bugs)
	}
	if stats.Refactors != 1 {
		t.Errorf("Expected 1 refactor, got %d", stats.Refactors)
	}
	if stats.TotalOccurrences != 9 { // 5 + 3 + 1
		t.Errorf("Expected 9 total occurrences, got %d", stats.TotalOccurrences)
	}
}

func TestPatternStore_CleanupCallback(t *testing.T) {
	store := setupPatternTestStore(t)

	patternStore := NewPatternStore(store)
	ctx := context.Background()

	var deletedIDs []int64
	patternStore.SetCleanupFunc(func(ctx context.Context, ids []int64) {
		deletedIDs = ids
	})

	// Create and delete pattern
	pattern := createTestPattern("Cleanup Test")
	id, _ := patternStore.StorePattern(ctx, pattern)

	patternStore.DeletePattern(ctx, id)

	if len(deletedIDs) != 1 || deletedIDs[0] != id {
		t.Errorf("Expected cleanup callback with ID %d, got %v", id, deletedIDs)
	}
}

// Helper function to create a test pattern
func createTestPattern(name string) *models.Pattern {
	now := time.Now()
	return &models.Pattern{
		Name:           name,
		Type:           models.PatternTypeBug,
		Description:    sql.NullString{String: "Test description", Valid: true},
		Signature:      []string{"test", "pattern"},
		Recommendation: sql.NullString{String: "Test recommendation", Valid: true},
		Frequency:      1,
		Projects:       []string{"test-project"},
		ObservationIDs: []int64{1},
		Status:         models.PatternStatusActive,
		Confidence:     0.5,
		LastSeenAt:     now.Format(time.RFC3339),
		LastSeenEpoch:  now.UnixMilli(),
		CreatedAt:      now.Format(time.RFC3339),
		CreatedAtEpoch: now.UnixMilli(),
	}
}
