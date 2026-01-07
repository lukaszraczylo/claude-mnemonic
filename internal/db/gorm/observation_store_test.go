//go:build fts5

// Package gorm provides GORM-based database operations for claude-mnemonic.
package gorm

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm/logger"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
)

// testObservationStore creates an ObservationStore with a temporary database for testing.
func testObservationStore(t *testing.T) (*ObservationStore, *Store, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "gorm_observation_test_*")
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

	observationStore := NewObservationStore(store, nil, nil, nil)

	cleanup := func() {
		store.Close()
		os.RemoveAll(tmpDir)
	}

	return observationStore, store, cleanup
}

func TestObservationStore_StoreObservation(t *testing.T) {
	observationStore, store, cleanup := testObservationStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create a session first
	sessionStore := NewSessionStore(store)
	_, err := sessionStore.CreateSDKSession(ctx, "claude-1", "test-project", "")
	require.NoError(t, err)

	// Store an observation
	observation := &models.ParsedObservation{
		Type:      models.ObsTypeDecision,
		Title:     "User prefers tabs over spaces",
		Narrative: "Observed in code formatting",
		Concepts:  []string{"coding-style", "preferences"},
	}

	id, epoch, err := observationStore.StoreObservation(ctx, "claude-1", "test-project", observation, 1, 100)
	require.NoError(t, err)
	assert.Greater(t, id, int64(0))
	assert.Greater(t, epoch, int64(0))
}

func TestObservationStore_StoreObservation_AutoCreateSession(t *testing.T) {
	observationStore, _, cleanup := testObservationStore(t)
	defer cleanup()

	ctx := context.Background()

	// Store observation without pre-creating session
	observation := &models.ParsedObservation{
		Type:  models.ObsTypeDiscovery,
		Title: "Test auto-create",
	}

	id, _, err := observationStore.StoreObservation(ctx, "claude-auto", "auto-project", observation, 1, 50)
	require.NoError(t, err)
	assert.Greater(t, id, int64(0))
}

func TestObservationStore_StoreObservation_WithScope(t *testing.T) {
	observationStore, _, cleanup := testObservationStore(t)
	defer cleanup()

	ctx := context.Background()

	tests := []struct {
		name          string
		tags          []string
		expectedScope models.ObservationScope
	}{
		{
			name:          "Global scope - best practice",
			tags:          []string{"best-practice", "testing"},
			expectedScope: models.ScopeGlobal,
		},
		{
			name:          "Global scope - security",
			tags:          []string{"security", "auth"},
			expectedScope: models.ScopeGlobal,
		},
		{
			name:          "Project scope - specific feature",
			tags:          []string{"feature", "implementation"},
			expectedScope: models.ScopeProject,
		},
		{
			name:          "Project scope - no tags",
			tags:          []string{},
			expectedScope: models.ScopeProject,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			observation := &models.ParsedObservation{
				Type:     models.ObsTypeDiscovery,
				Title:    "Test scope determination",
				Concepts: tt.tags,
			}

			id, _, err := observationStore.StoreObservation(ctx, "claude-1", "test-project", observation, 1, 50)
			require.NoError(t, err)

			// Verify scope was set correctly
			observations, err := observationStore.GetObservationsByIDs(ctx, []int64{id}, "default", 10)
			require.NoError(t, err)
			require.Len(t, observations, 1)
			assert.Equal(t, tt.expectedScope, observations[0].Scope)
		})
	}
}

func TestObservationStore_StoreObservation_AsyncCleanup(t *testing.T) {
	observationStore, _, cleanup := testObservationStore(t)
	defer cleanup()

	ctx := context.Background()

	// Track cleanup calls
	var cleanupMutex sync.Mutex
	cleanupCalled := false
	var cleanupIDs []int64

	cleanupFunc := func(ctx context.Context, deletedIDs []int64) {
		cleanupMutex.Lock()
		defer cleanupMutex.Unlock()
		cleanupCalled = true
		cleanupIDs = deletedIDs
	}

	observationStore.cleanupFunc = cleanupFunc

	// Store observations beyond the limit (MaxObservationsPerProject = 100)
	for i := 0; i < 105; i++ {
		observation := &models.ParsedObservation{
			Type:  models.ObsTypeDiscovery,
			Title: "Observation",
		}
		_, _, err := observationStore.StoreObservation(ctx, "claude-1", "test-project", observation, i, 50)
		require.NoError(t, err)
	}

	// Wait for async cleanup to complete
	time.Sleep(200 * time.Millisecond)

	// Verify cleanup was called
	cleanupMutex.Lock()
	defer cleanupMutex.Unlock()
	assert.True(t, cleanupCalled, "Cleanup function should have been called")
	assert.NotEmpty(t, cleanupIDs, "Cleanup should have deleted some observations")
}

func TestObservationStore_GetObservationsByIDs(t *testing.T) {
	observationStore, _, cleanup := testObservationStore(t)
	defer cleanup()

	ctx := context.Background()

	// Store multiple observations with different importance scores
	var ids []int64
	for i := 1; i <= 3; i++ {
		observation := &models.ParsedObservation{
			Type:  models.ObsTypeDiscovery,
			Title: "Test",
		}
		id, _, err := observationStore.StoreObservation(ctx, "claude-1", "test-project", observation, i, 10)
		require.NoError(t, err)
		ids = append(ids, id)

		// Update importance score directly
		observationStore.db.Model(&Observation{}).Where("id = ?", id).Update("importance_score", float64(i))
		time.Sleep(10 * time.Millisecond) // Ensure different timestamps
	}

	tests := []struct {
		name     string
		orderBy  string
		expected []int64
	}{
		{
			name:     "Default ordering - importance desc",
			orderBy:  "default",
			expected: []int64{ids[2], ids[1], ids[0]}, // High to low importance
		},
		{
			name:     "Importance ordering",
			orderBy:  "importance",
			expected: []int64{ids[2], ids[1], ids[0]},
		},
		{
			name:     "Date ascending",
			orderBy:  "date_asc",
			expected: []int64{ids[0], ids[1], ids[2]}, // Oldest to newest
		},
		{
			name:     "Date descending",
			orderBy:  "date_desc",
			expected: []int64{ids[2], ids[1], ids[0]}, // Newest to oldest
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			observations, err := observationStore.GetObservationsByIDs(ctx, ids, tt.orderBy, 10)
			require.NoError(t, err)
			require.Len(t, observations, 3)

			// Verify ordering
			for i, obs := range observations {
				assert.Equal(t, tt.expected[i], obs.ID, "Position %d should have ID %d", i, tt.expected[i])
			}
		})
	}
}

func TestObservationStore_GetObservationsByIDs_Limit(t *testing.T) {
	observationStore, _, cleanup := testObservationStore(t)
	defer cleanup()

	ctx := context.Background()

	// Store multiple observations
	var ids []int64
	for i := 1; i <= 5; i++ {
		observation := &models.ParsedObservation{
			Type:  models.ObsTypeDiscovery,
			Title: "Test",
		}
		id, _, err := observationStore.StoreObservation(ctx, "claude-1", "test-project", observation, i, 10)
		require.NoError(t, err)
		ids = append(ids, id)
	}

	// Get with limit
	observations, err := observationStore.GetObservationsByIDs(ctx, ids, "default", 3)
	require.NoError(t, err)
	assert.Len(t, observations, 3)
}

func TestObservationStore_GetObservationsByIDs_EmptyInput(t *testing.T) {
	observationStore, _, cleanup := testObservationStore(t)
	defer cleanup()

	ctx := context.Background()

	// Get with empty IDs
	observations, err := observationStore.GetObservationsByIDs(ctx, []int64{}, "default", 10)
	require.NoError(t, err)
	assert.Nil(t, observations)
}

func TestObservationStore_GetRecentObservations(t *testing.T) {
	observationStore, _, cleanup := testObservationStore(t)
	defer cleanup()

	ctx := context.Background()

	// Store project-scoped observations
	for i := 1; i <= 3; i++ {
		observation := &models.ParsedObservation{
			Type:  models.ObsTypeDiscovery,
			Title: "Project A fact",
		}
		_, _, err := observationStore.StoreObservation(ctx, "claude-1", "project-a", observation, i, 10)
		require.NoError(t, err)
	}

	// Store global-scoped observation
	observation := &models.ParsedObservation{
		Type:     models.ObsTypeDiscovery,
		Title:    "Global best practice",
		Concepts: []string{"best-practice"},
	}
	_, _, err := observationStore.StoreObservation(ctx, "claude-2", "project-b", observation, 1, 10)
	require.NoError(t, err)

	// Store observation for different project
	observation = &models.ParsedObservation{
		Type:  models.ObsTypeDiscovery,
		Title: "Project B fact",
	}
	_, _, err = observationStore.StoreObservation(ctx, "claude-2", "project-b", observation, 2, 10)
	require.NoError(t, err)

	// Wait for any async cleanup to complete before querying
	time.Sleep(100 * time.Millisecond)

	// Get recent observations for project-a (should include project-a + global)
	observations, err := observationStore.GetRecentObservations(ctx, "project-a", 10)
	require.NoError(t, err)
	assert.Len(t, observations, 4) // 3 project-a + 1 global

	// Verify scope filtering
	projectCount := 0
	globalCount := 0
	for _, obs := range observations {
		if obs.Scope == models.ScopeProject {
			assert.Equal(t, "project-a", obs.Project)
			projectCount++
		} else if obs.Scope == models.ScopeGlobal {
			globalCount++
		}
	}
	assert.Equal(t, 3, projectCount)
	assert.Equal(t, 1, globalCount)
}

func TestObservationStore_GetActiveObservations(t *testing.T) {
	observationStore, _, cleanup := testObservationStore(t)
	defer cleanup()

	ctx := context.Background()

	// Store active observation
	activeObs := &models.ParsedObservation{
		Type:  models.ObsTypeDiscovery,
		Title: "Active observation",
	}
	activeID, _, err := observationStore.StoreObservation(ctx, "claude-1", "test-project", activeObs, 1, 10)
	require.NoError(t, err)

	// Store superseded observation
	supersededObs := &models.ParsedObservation{
		Type:  models.ObsTypeDiscovery,
		Title: "Superseded observation",
	}
	supersededID, _, err := observationStore.StoreObservation(ctx, "claude-1", "test-project", supersededObs, 2, 10)
	require.NoError(t, err)

	// Mark as superseded
	observationStore.db.Model(&Observation{}).Where("id = ?", supersededID).Update("is_superseded", 1)

	// Get active observations (should exclude superseded)
	observations, err := observationStore.GetActiveObservations(ctx, "test-project", 10)
	require.NoError(t, err)
	assert.Len(t, observations, 1)
	assert.Equal(t, activeID, observations[0].ID)
	assert.False(t, observations[0].IsSuperseded)
}

func TestObservationStore_GetSupersededObservations(t *testing.T) {
	observationStore, _, cleanup := testObservationStore(t)
	defer cleanup()

	ctx := context.Background()

	// Store active observation
	activeObs := &models.ParsedObservation{
		Type:  models.ObsTypeDiscovery,
		Title: "Active observation",
	}
	_, _, err := observationStore.StoreObservation(ctx, "claude-1", "test-project", activeObs, 1, 10)
	require.NoError(t, err)

	// Store superseded observation
	supersededObs := &models.ParsedObservation{
		Type:  models.ObsTypeDiscovery,
		Title: "Superseded observation",
	}
	supersededID, _, err := observationStore.StoreObservation(ctx, "claude-1", "test-project", supersededObs, 2, 10)
	require.NoError(t, err)

	// Mark as superseded
	observationStore.db.Model(&Observation{}).Where("id = ?", supersededID).Update("is_superseded", 1)

	// Get superseded observations (should exclude active)
	observations, err := observationStore.GetSupersededObservations(ctx, "test-project", 10)
	require.NoError(t, err)
	assert.Len(t, observations, 1)
	assert.Equal(t, supersededID, observations[0].ID)
	assert.True(t, observations[0].IsSuperseded)
}

func TestObservationStore_GetObservationsByProjectStrict(t *testing.T) {
	observationStore, _, cleanup := testObservationStore(t)
	defer cleanup()

	ctx := context.Background()

	// Store project-scoped observations
	for i := 1; i <= 2; i++ {
		observation := &models.ParsedObservation{
			Type:  models.ObsTypeDiscovery,
			Title: "Project A fact",
		}
		_, _, err := observationStore.StoreObservation(ctx, "claude-1", "project-a", observation, i, 10)
		require.NoError(t, err)
	}

	// Store global-scoped observation
	observation := &models.ParsedObservation{
		Type:     models.ObsTypeDiscovery,
		Title:    "Global best practice",
		Concepts: []string{"best-practice"},
	}
	_, _, err := observationStore.StoreObservation(ctx, "claude-2", "project-b", observation, 1, 10)
	require.NoError(t, err)

	// Get strict project observations (should exclude global)
	observations, err := observationStore.GetObservationsByProjectStrict(ctx, "project-a", 10)
	require.NoError(t, err)
	assert.Len(t, observations, 2) // Only project-a observations

	// Verify all are project-scoped
	for _, obs := range observations {
		assert.Equal(t, models.ScopeProject, obs.Scope)
		assert.Equal(t, "project-a", obs.Project)
	}
}

func TestObservationStore_SearchObservationsFTS(t *testing.T) {
	observationStore, _, cleanup := testObservationStore(t)
	defer cleanup()

	ctx := context.Background()

	// Store observations with searchable content
	observations := []*models.ParsedObservation{
		{
			Type:     models.ObsTypeDiscovery,
			Title:    "User prefers React for frontend development",
			Concepts: []string{"frontend", "react"},
		},
		{
			Type:     models.ObsTypeDiscovery,
			Title:    "Backend uses Go with chi router",
			Concepts: []string{"backend", "golang"},
		},
		{
			Type:     models.ObsTypeDiscovery,
			Title:    "Database is SQLite with FTS5",
			Concepts: []string{"database", "sqlite"},
		},
	}

	for i, obs := range observations {
		_, _, err := observationStore.StoreObservation(ctx, "claude-1", "test-project", obs, i+1, 10)
		require.NoError(t, err)
	}

	// Wait for FTS5 triggers to fire
	time.Sleep(200 * time.Millisecond)

	// Search for "React frontend"
	results, err := observationStore.SearchObservationsFTS(ctx, "React frontend", "test-project", 10)
	require.NoError(t, err)
	assert.NotEmpty(t, results, "Should find observations matching 'React frontend'")

	// Verify results contain relevant observation
	found := false
	for _, obs := range results {
		if obs.Title.String == "User prefers React for frontend development" {
			found = true
			break
		}
	}
	assert.True(t, found, "Should find the React observation")
}

func TestObservationStore_CleanupOldObservations(t *testing.T) {
	observationStore, _, cleanup := testObservationStore(t)
	defer cleanup()

	ctx := context.Background()

	// Store observations beyond the limit WITHOUT async cleanup
	// We disable async cleanup by not setting cleanupFunc
	var allIDs []int64
	for i := 0; i < 105; i++ {
		observation := &models.ParsedObservation{
			Type:  models.ObsTypeDiscovery,
			Title: "Observation",
		}
		id, _, err := observationStore.StoreObservation(ctx, "claude-1", "test-project", observation, i, 10)
		require.NoError(t, err)
		allIDs = append(allIDs, id)
		time.Sleep(2 * time.Millisecond) // Ensure different timestamps
	}

	// Wait for any async cleanups to complete (even though cleanupFunc is nil)
	time.Sleep(200 * time.Millisecond)

	// Verify we have 105 observations initially (async cleanup should have run but deleted items)
	initial, err := observationStore.GetRecentObservations(ctx, "test-project", 200)
	require.NoError(t, err)

	// If async cleanup already happened, we'll have <= 100
	// Run cleanup manually to ensure cleanup logic works
	deletedIDs, err := observationStore.CleanupOldObservations(ctx, "test-project")
	require.NoError(t, err)

	// After cleanup (manual or async), we should have at most 100
	remaining, err := observationStore.GetRecentObservations(ctx, "test-project", 200)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(remaining), 100, "Should have at most 100 observations after cleanup")

	// The number deleted should match how many were over the limit
	expectedDeleted := len(initial) - len(remaining)
	assert.Len(t, deletedIDs, expectedDeleted, "Should delete observations beyond limit")
}

func TestObservationStore_DeleteObservations(t *testing.T) {
	observationStore, _, cleanup := testObservationStore(t)
	defer cleanup()

	ctx := context.Background()

	// Store multiple observations
	var ids []int64
	for i := 1; i <= 5; i++ {
		observation := &models.ParsedObservation{
			Type:  models.ObsTypeDiscovery,
			Title: "Test",
		}
		id, _, err := observationStore.StoreObservation(ctx, "claude-1", "test-project", observation, i, 10)
		require.NoError(t, err)
		ids = append(ids, id)
	}

	// Delete first 3 observations
	_, err := observationStore.DeleteObservations(ctx, ids[:3])
	require.NoError(t, err)

	// Verify only 2 remain
	remaining, err := observationStore.GetRecentObservations(ctx, "test-project", 10)
	require.NoError(t, err)
	assert.Len(t, remaining, 2)

	// Verify deleted observations are gone
	deleted, err := observationStore.GetObservationsByIDs(ctx, ids[:3], "default", 10)
	require.NoError(t, err)
	assert.Empty(t, deleted)
}

// Note: TestObservationStore_MarkObservationsSuperseded is omitted because
// MarkObservationsSuperseded is a ConflictStore method (Phase 4), not ObservationStore

func TestObservationStore_GetAllObservations(t *testing.T) {
	observationStore, _, cleanup := testObservationStore(t)
	defer cleanup()

	ctx := context.Background()

	// Store observations across projects
	_, _, err := observationStore.StoreObservation(ctx, "claude-1", "project-a", &models.ParsedObservation{Type: models.ObsTypeDiscovery, Title: "A1"}, 1, 10)
	require.NoError(t, err)

	_, _, err = observationStore.StoreObservation(ctx, "claude-2", "project-b", &models.ParsedObservation{Type: models.ObsTypeDiscovery, Title: "B1"}, 1, 10)
	require.NoError(t, err)

	// Get all observations (for vector rebuild)
	all, err := observationStore.GetAllObservations(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 2)

	// Verify ordering by ID
	assert.Less(t, all[0].ID, all[1].ID)
}
