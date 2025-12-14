// Package sqlite provides SQLite database operations for claude-mnemonic.
package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testObservationStore creates an ObservationStore with a test database including FTS5.
func testObservationStore(t *testing.T) (*ObservationStore, *Store, func()) {
	t.Helper()

	db, _, cleanup := testDB(t)
	createAllTables(t, db)

	store := newStoreFromDB(db)
	obsStore := NewObservationStore(store)

	return obsStore, store, cleanup
}

func TestObservationStore_StoreAndRetrieve(t *testing.T) {
	obsStore, _, cleanup := testObservationStore(t)
	defer cleanup()

	ctx := context.Background()

	obs := &models.ParsedObservation{
		Type:          models.ObsTypeDiscovery,
		Title:         "Test Observation",
		Subtitle:      "A subtitle",
		Narrative:     "This is a test observation about testing",
		Facts:         []string{"Fact 1", "Fact 2"},
		Concepts:      []string{"testing", "golang"},
		FilesRead:     []string{"test.go"},
		FilesModified: []string{},
	}

	id, epoch, err := obsStore.StoreObservation(ctx, "session-1", "project-a", obs, 1, 100)
	require.NoError(t, err)
	assert.Greater(t, id, int64(0))
	assert.Greater(t, epoch, int64(0))

	// Retrieve by ID
	retrieved, err := obsStore.GetObservationByID(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, retrieved)

	assert.Equal(t, id, retrieved.ID)
	assert.Equal(t, "session-1", retrieved.SDKSessionID)
	assert.Equal(t, "project-a", retrieved.Project)
	assert.Equal(t, models.ObsTypeDiscovery, retrieved.Type)
	assert.Equal(t, "Test Observation", retrieved.Title.String)
	assert.Equal(t, "A subtitle", retrieved.Subtitle.String)
	assert.Equal(t, "This is a test observation about testing", retrieved.Narrative.String)
	assert.Equal(t, []string{"Fact 1", "Fact 2"}, []string(retrieved.Facts))
	assert.Equal(t, []string{"testing", "golang"}, []string(retrieved.Concepts))
}

func TestObservationStore_GetRecentObservations(t *testing.T) {
	obsStore, _, cleanup := testObservationStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create multiple observations
	for i := 0; i < 10; i++ {
		obs := &models.ParsedObservation{
			Type:      models.ObsTypeDiscovery,
			Title:     "Observation " + string(rune('A'+i)),
			Narrative: "Content " + string(rune('A'+i)),
			Concepts:  []string{"test"},
		}
		_, _, err := obsStore.StoreObservation(ctx, "session-1", "project-a", obs, i+1, 100)
		require.NoError(t, err)
		time.Sleep(time.Millisecond) // Ensure different timestamps
	}

	// Get recent with limit 5
	recent, err := obsStore.GetRecentObservations(ctx, "project-a", 5)
	require.NoError(t, err)
	assert.Len(t, recent, 5)

	// Get recent with limit 20 (more than exists)
	recent, err = obsStore.GetRecentObservations(ctx, "project-a", 20)
	require.NoError(t, err)
	assert.Len(t, recent, 10)
}

func TestObservationStore_SearchObservationsFTS(t *testing.T) {
	obsStore, _, cleanup := testObservationStore(t)
	defer cleanup()

	// FTS5 tables are created by testObservationStore via testutil.CreateAllTables
	ctx := context.Background()

	// Create observations with different content
	observations := []struct {
		title     string
		narrative string
	}{
		{"Authentication implementation", "JWT based authentication flow"},
		{"Database setup", "PostgreSQL configuration and migrations"},
		{"Caching layer", "Redis caching implementation"},
		{"User authentication fix", "Fixed authentication bug in login"},
		{"API endpoints", "REST API implementation details"},
	}

	for _, o := range observations {
		obs := &models.ParsedObservation{
			Type:      models.ObsTypeDiscovery,
			Title:     o.title,
			Narrative: o.narrative,
		}
		_, _, err := obsStore.StoreObservation(ctx, "session-1", "project-a", obs, 1, 100)
		require.NoError(t, err)
		time.Sleep(time.Millisecond)
	}

	// Search for authentication - should find 2 observations
	results, err := obsStore.SearchObservationsFTS(ctx, "authentication", "project-a", 50)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 2, "should find at least 2 authentication-related observations")

	// Search for database - should find 1 observation
	results, err = obsStore.SearchObservationsFTS(ctx, "database PostgreSQL", "project-a", 50)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 1, "should find at least 1 database-related observation")
}

func TestObservationStore_SearchObservationsFTS_LimitRespected(t *testing.T) {
	obsStore, _, cleanup := testObservationStore(t)
	defer cleanup()

	// FTS5 tables are created by testObservationStore via testutil.CreateAllTables
	ctx := context.Background()

	// Create 20 observations with similar content
	for i := 0; i < 20; i++ {
		obs := &models.ParsedObservation{
			Type:      models.ObsTypeDiscovery,
			Title:     "Testing observation " + string(rune('A'+i)),
			Narrative: "This is about testing and quality assurance " + string(rune('A'+i)),
		}
		_, _, err := obsStore.StoreObservation(ctx, "session-1", "project-a", obs, 1, 100)
		require.NoError(t, err)
		time.Sleep(time.Millisecond)
	}

	// Search with limit 5
	results, err := obsStore.SearchObservationsFTS(ctx, "testing quality", "project-a", 5)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(results), 5, "should respect limit of 5")

	// Search with limit 15
	results, err = obsStore.SearchObservationsFTS(ctx, "testing quality", "project-a", 15)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(results), 15, "should respect limit of 15")

	// Search with limit 50 (our new default)
	results, err = obsStore.SearchObservationsFTS(ctx, "testing quality", "project-a", 50)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(results), 50, "should respect limit of 50")
	assert.Equal(t, 20, len(results), "should return all 20 matching observations")
}

func TestObservationStore_GlobalScope(t *testing.T) {
	obsStore, _, cleanup := testObservationStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create a project-scoped observation
	projectObs := &models.ParsedObservation{
		Type:      models.ObsTypeDiscovery,
		Title:     "Project specific code",
		Narrative: "This is specific to project-a",
		Concepts:  []string{"project-specific"},
	}
	_, _, err := obsStore.StoreObservation(ctx, "session-1", "project-a", projectObs, 1, 100)
	require.NoError(t, err)

	// Create a global-scoped observation (has a globalizable concept)
	globalObs := &models.ParsedObservation{
		Type:      models.ObsTypeDiscovery,
		Title:     "Security best practice",
		Narrative: "Always validate user input",
		Concepts:  []string{"security", "best-practice"}, // "security" is in GlobalizableConcepts
	}
	_, _, err = obsStore.StoreObservation(ctx, "session-1", "project-a", globalObs, 1, 100)
	require.NoError(t, err)

	// Get recent for project-a - should see both
	results, err := obsStore.GetRecentObservations(ctx, "project-a", 10)
	require.NoError(t, err)
	assert.Len(t, results, 2)

	// Get recent for project-b - should only see global observation
	results, err = obsStore.GetRecentObservations(ctx, "project-b", 10)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "Security best practice", results[0].Title.String)
	assert.Equal(t, models.ScopeGlobal, results[0].Scope)
}

func TestObservationStore_DeleteObservations(t *testing.T) {
	obsStore, _, cleanup := testObservationStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create observations
	var ids []int64
	for i := 0; i < 5; i++ {
		obs := &models.ParsedObservation{
			Type:  models.ObsTypeDiscovery,
			Title: "Observation " + string(rune('A'+i)),
		}
		id, _, err := obsStore.StoreObservation(ctx, "session-1", "project-a", obs, 1, 100)
		require.NoError(t, err)
		ids = append(ids, id)
	}

	// Verify all exist
	all, err := obsStore.GetRecentObservations(ctx, "project-a", 10)
	require.NoError(t, err)
	assert.Len(t, all, 5)

	// Delete first 3
	deleted, err := obsStore.DeleteObservations(ctx, ids[:3])
	require.NoError(t, err)
	assert.Equal(t, int64(3), deleted)

	// Verify only 2 remain
	remaining, err := obsStore.GetRecentObservations(ctx, "project-a", 10)
	require.NoError(t, err)
	assert.Len(t, remaining, 2)
}

func TestObservationStore_GetObservationCount(t *testing.T) {
	obsStore, _, cleanup := testObservationStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create observations for different projects
	for i := 0; i < 5; i++ {
		obs := &models.ParsedObservation{
			Type:  models.ObsTypeDiscovery,
			Title: "Project A observation " + string(rune('0'+i)),
		}
		_, _, err := obsStore.StoreObservation(ctx, "session-1", "project-a", obs, 1, 100)
		require.NoError(t, err)
	}

	for i := 0; i < 3; i++ {
		obs := &models.ParsedObservation{
			Type:  models.ObsTypeDiscovery,
			Title: "Project B observation " + string(rune('0'+i)),
		}
		_, _, err := obsStore.StoreObservation(ctx, "session-1", "project-b", obs, 1, 100)
		require.NoError(t, err)
	}

	// Create a global observation
	globalObs := &models.ParsedObservation{
		Type:     models.ObsTypeDiscovery,
		Title:    "Global observation",
		Concepts: []string{"best-practice"}, // Makes it global
	}
	_, _, err := obsStore.StoreObservation(ctx, "session-1", "project-a", globalObs, 1, 100)
	require.NoError(t, err)

	// Count for project-a includes its own + global
	count, err := obsStore.GetObservationCount(ctx, "project-a")
	require.NoError(t, err)
	assert.Equal(t, 6, count) // 5 project-a + 1 global

	// Count for project-b includes its own + global
	count, err = obsStore.GetObservationCount(ctx, "project-b")
	require.NoError(t, err)
	assert.Equal(t, 4, count) // 3 project-b + 1 global
}

func TestObservationStore_CleanupOldObservations(t *testing.T) {
	obsStore, _, cleanup := testObservationStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create more observations than the limit (MaxObservationsPerProject = 100)
	// We'll create a smaller number and verify the logic works
	for i := 0; i < 10; i++ {
		obs := &models.ParsedObservation{
			Type:  models.ObsTypeDiscovery,
			Title: "Observation " + string(rune('A'+i)),
		}
		_, _, err := obsStore.StoreObservation(ctx, "session-1", "project-a", obs, i+1, 100)
		require.NoError(t, err)
		time.Sleep(time.Millisecond)
	}

	// Cleanup should return empty since we're under the limit
	deletedIDs, err := obsStore.CleanupOldObservations(ctx, "project-a")
	require.NoError(t, err)
	assert.Empty(t, deletedIDs)

	// All 10 should still exist
	count, err := obsStore.GetObservationCount(ctx, "project-a")
	require.NoError(t, err)
	assert.Equal(t, 10, count)
}

func TestObservationStore_SetCleanupFunc(t *testing.T) {
	obsStore, _, cleanup := testObservationStore(t)
	defer cleanup()

	ctx := context.Background()

	// Track cleanup calls
	var cleanupCalledWith []int64
	obsStore.SetCleanupFunc(func(ctx context.Context, deletedIDs []int64) {
		cleanupCalledWith = deletedIDs
	})

	// Store an observation (should trigger cleanup, but won't delete anything under limit)
	obs := &models.ParsedObservation{
		Type:  models.ObsTypeDiscovery,
		Title: "Test observation",
	}
	_, _, err := obsStore.StoreObservation(ctx, "session-1", "project-a", obs, 1, 100)
	require.NoError(t, err)

	// Cleanup func should not have been called since nothing was deleted
	assert.Empty(t, cleanupCalledWith)
}

func TestExtractKeywords(t *testing.T) {
	tests := []struct {
		query    string
		expected []string
	}{
		{
			query:    "What is the authentication flow?",
			expected: []string{"authentication", "flow"},
		},
		{
			query:    "How does the database connection work?",
			expected: []string{"database", "connection"},
		},
		{
			query:    "JWT token validation",
			expected: []string{"token", "validation"},
		},
		{
			query:    "the a an is are", // All stop words
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			keywords := extractKeywords(tt.query)
			for _, exp := range tt.expected {
				assert.Contains(t, keywords, exp, "should contain keyword: "+exp)
			}
		})
	}
}
