// Package sqlite provides SQLite database operations for claude-mnemonic.
package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// testObservationStoreBasic creates an ObservationStore with base tables (no FTS5).
func testObservationStoreBasic(t *testing.T) (*ObservationStore, *Store, func()) {
	t.Helper()

	db, _, cleanup := testDB(t)
	createBaseTables(t, db)

	store := newStoreFromDB(db)
	obsStore := NewObservationStore(store)

	return obsStore, store, cleanup
}

// testObservationStore creates an ObservationStore with a test database including FTS5.
func testObservationStore(t *testing.T) (*ObservationStore, *Store, func()) {
	t.Helper()

	db, _, cleanup := testDB(t)
	createAllTables(t, db)

	store := newStoreFromDB(db)
	obsStore := NewObservationStore(store)

	return obsStore, store, cleanup
}

// ObservationStoreSuite is a test suite for ObservationStore operations.
type ObservationStoreSuite struct {
	suite.Suite
	obsStore *ObservationStore
	store    *Store
	cleanup  func()
}

func (s *ObservationStoreSuite) SetupTest() {
	s.obsStore, s.store, s.cleanup = testObservationStoreBasic(s.T())
}

func (s *ObservationStoreSuite) TearDownTest() {
	if s.cleanup != nil {
		s.cleanup()
	}
}

func TestObservationStoreSuite(t *testing.T) {
	suite.Run(t, new(ObservationStoreSuite))
}

// TestStoreObservation_TableDriven tests observation storage with various scenarios.
func (s *ObservationStoreSuite) TestStoreObservation_TableDriven() {
	ctx := context.Background()

	tests := []struct {
		name         string
		sdkSessionID string
		project      string
		obs          *models.ParsedObservation
		promptNum    int
		tokens       int64
		wantErr      bool
	}{
		{
			name:         "basic discovery observation",
			sdkSessionID: "session-basic",
			project:      "project-a",
			obs: &models.ParsedObservation{
				Type:      models.ObsTypeDiscovery,
				Title:     "Test Title",
				Subtitle:  "Test Subtitle",
				Narrative: "Test narrative content",
				Facts:     []string{"Fact 1", "Fact 2"},
				Concepts:  []string{"testing", "golang"},
			},
			promptNum: 1,
			tokens:    100,
			wantErr:   false,
		},
		{
			name:         "bugfix observation",
			sdkSessionID: "session-bugfix",
			project:      "project-b",
			obs: &models.ParsedObservation{
				Type:          models.ObsTypeBugfix,
				Title:         "Fixed null pointer",
				Narrative:     "Fixed null pointer exception in handler",
				FilesModified: []string{"handler.go"},
			},
			promptNum: 2,
			tokens:    50,
			wantErr:   false,
		},
		{
			name:         "global scope observation",
			sdkSessionID: "session-global",
			project:      "project-c",
			obs: &models.ParsedObservation{
				Type:      models.ObsTypeDiscovery,
				Title:     "Security best practice",
				Narrative: "Always validate user input",
				Concepts:  []string{"security", "best-practice"},
			},
			promptNum: 1,
			tokens:    75,
			wantErr:   false,
		},
		{
			name:         "observation with files",
			sdkSessionID: "session-files",
			project:      "project-d",
			obs: &models.ParsedObservation{
				Type:          models.ObsTypeFeature,
				Title:         "Added authentication",
				Narrative:     "Implemented JWT authentication",
				FilesRead:     []string{"config.go", "auth.go"},
				FilesModified: []string{"handler.go", "middleware.go"},
				FileMtimes:    map[string]int64{"handler.go": 1234567890, "middleware.go": 1234567891},
			},
			promptNum: 3,
			tokens:    200,
			wantErr:   false,
		},
		{
			name:         "minimal observation",
			sdkSessionID: "session-minimal",
			project:      "project-e",
			obs: &models.ParsedObservation{
				Type: models.ObsTypeChange,
			},
			promptNum: 0,
			tokens:    0,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			id, epoch, err := s.obsStore.StoreObservation(ctx, tt.sdkSessionID, tt.project, tt.obs, tt.promptNum, tt.tokens)
			if tt.wantErr {
				s.Error(err)
				return
			}

			s.NoError(err)
			s.Greater(id, int64(0))
			s.Greater(epoch, int64(0))

			// Retrieve and verify
			retrieved, err := s.obsStore.GetObservationByID(ctx, id)
			s.NoError(err)
			s.NotNil(retrieved)
			s.Equal(id, retrieved.ID)
			s.Equal(tt.project, retrieved.Project)
			s.Equal(tt.obs.Type, retrieved.Type)
		})
	}
}

// TestGetObservationByID_NotFound tests retrieval of non-existent observation.
func (s *ObservationStoreSuite) TestGetObservationByID_NotFound() {
	ctx := context.Background()

	obs, err := s.obsStore.GetObservationByID(ctx, 99999)
	s.NoError(err)
	s.Nil(obs)
}

// TestGetRecentObservations_TableDriven tests recent observations retrieval.
func (s *ObservationStoreSuite) TestGetRecentObservations_TableDriven() {
	ctx := context.Background()

	// Create 15 observations
	for i := 0; i < 15; i++ {
		obs := &models.ParsedObservation{
			Type:  models.ObsTypeDiscovery,
			Title: "Observation " + string(rune('A'+i)),
		}
		_, _, err := s.obsStore.StoreObservation(ctx, "session-"+string(rune('0'+i)), "project-a", obs, i, 10)
		s.NoError(err)
		time.Sleep(time.Millisecond) // Ensure different timestamps
	}

	tests := []struct {
		name      string
		project   string
		limit     int
		wantCount int
	}{
		{
			name:      "limit 5",
			project:   "project-a",
			limit:     5,
			wantCount: 5,
		},
		{
			name:      "limit 10",
			project:   "project-a",
			limit:     10,
			wantCount: 10,
		},
		{
			name:      "limit higher than count",
			project:   "project-a",
			limit:     50,
			wantCount: 15,
		},
		{
			name:      "different project (no results)",
			project:   "project-b",
			limit:     10,
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			observations, err := s.obsStore.GetRecentObservations(ctx, tt.project, tt.limit)
			s.NoError(err)
			s.Len(observations, tt.wantCount)
		})
	}
}

// TestDeleteObservations_TableDriven tests observation deletion.
func (s *ObservationStoreSuite) TestDeleteObservations_TableDriven() {
	ctx := context.Background()

	// Create observations
	var ids []int64
	for i := 0; i < 5; i++ {
		obs := &models.ParsedObservation{
			Type:  models.ObsTypeDiscovery,
			Title: "To delete " + string(rune('A'+i)),
		}
		id, _, err := s.obsStore.StoreObservation(ctx, "session-del", "project-del", obs, i, 10)
		s.NoError(err)
		ids = append(ids, id)
	}

	tests := []struct {
		name        string
		toDelete    []int64
		wantDeleted int64
		wantRemain  int
	}{
		{
			name:        "delete none",
			toDelete:    []int64{},
			wantDeleted: 0,
			wantRemain:  5,
		},
		{
			name:        "delete one",
			toDelete:    ids[0:1],
			wantDeleted: 1,
			wantRemain:  4,
		},
		{
			name:        "delete remaining",
			toDelete:    ids[1:],
			wantDeleted: 4,
			wantRemain:  0,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			deleted, err := s.obsStore.DeleteObservations(ctx, tt.toDelete)
			s.NoError(err)
			s.Equal(tt.wantDeleted, deleted)

			remaining, err := s.obsStore.GetAllRecentObservations(ctx, 100)
			s.NoError(err)
			s.Len(remaining, tt.wantRemain)
		})
	}
}

// TestGetObservationsByIDs tests retrieval by multiple IDs.
func (s *ObservationStoreSuite) TestGetObservationsByIDs() {
	ctx := context.Background()

	// Create observations
	var ids []int64
	for i := 0; i < 5; i++ {
		obs := &models.ParsedObservation{
			Type:  models.ObsTypeDiscovery,
			Title: "By ID " + string(rune('A'+i)),
		}
		id, _, err := s.obsStore.StoreObservation(ctx, "session-byid", "project-byid", obs, i, 10)
		s.NoError(err)
		ids = append(ids, id)
		time.Sleep(time.Millisecond)
	}

	tests := []struct {
		name      string
		queryIDs  []int64
		orderBy   string
		limit     int
		wantCount int
	}{
		{
			name:      "empty IDs",
			queryIDs:  []int64{},
			orderBy:   "date_desc",
			limit:     10,
			wantCount: 0,
		},
		{
			name:      "single ID",
			queryIDs:  ids[0:1],
			orderBy:   "date_desc",
			limit:     10,
			wantCount: 1,
		},
		{
			name:      "all IDs",
			queryIDs:  ids,
			orderBy:   "date_desc",
			limit:     10,
			wantCount: 5,
		},
		{
			name:      "with limit less than IDs",
			queryIDs:  ids,
			orderBy:   "date_desc",
			limit:     3,
			wantCount: 3,
		},
		{
			name:      "ascending order",
			queryIDs:  ids,
			orderBy:   "date_asc",
			limit:     10,
			wantCount: 5,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			observations, err := s.obsStore.GetObservationsByIDs(ctx, tt.queryIDs, tt.orderBy, tt.limit)
			if tt.wantCount == 0 {
				s.NoError(err)
				s.Nil(observations)
			} else {
				s.NoError(err)
				s.Len(observations, tt.wantCount)
			}
		})
	}
}

// TestGlobalScope tests global vs project scope.
func (s *ObservationStoreSuite) TestGlobalScope() {
	ctx := context.Background()

	// Create project-scoped observation
	projectObs := &models.ParsedObservation{
		Type:     models.ObsTypeDiscovery,
		Title:    "Project specific",
		Concepts: []string{"project-specific"},
	}
	_, _, err := s.obsStore.StoreObservation(ctx, "session-scope", "project-a", projectObs, 1, 10)
	s.NoError(err)

	// Create global-scoped observation (security concept triggers global)
	globalObs := &models.ParsedObservation{
		Type:     models.ObsTypeDiscovery,
		Title:    "Global security",
		Concepts: []string{"security"},
	}
	_, _, err = s.obsStore.StoreObservation(ctx, "session-scope", "project-a", globalObs, 2, 10)
	s.NoError(err)

	// Project-a should see both
	resultsA, err := s.obsStore.GetRecentObservations(ctx, "project-a", 10)
	s.NoError(err)
	s.Len(resultsA, 2)

	// Project-b should only see global
	resultsB, err := s.obsStore.GetRecentObservations(ctx, "project-b", 10)
	s.NoError(err)
	s.Len(resultsB, 1)
	s.Equal("Global security", resultsB[0].Title.String)
	s.Equal(models.ScopeGlobal, resultsB[0].Scope)
}

// TestSetCleanupFunc tests the cleanup function callback.
func (s *ObservationStoreSuite) TestSetCleanupFunc() {
	ctx := context.Background()

	var calledWith []int64
	s.obsStore.SetCleanupFunc(func(ctx context.Context, deletedIDs []int64) {
		calledWith = deletedIDs
	})

	// Store an observation
	obs := &models.ParsedObservation{
		Type:  models.ObsTypeDiscovery,
		Title: "Test cleanup",
	}
	_, _, err := s.obsStore.StoreObservation(ctx, "session-cleanup", "project-cleanup", obs, 1, 10)
	s.NoError(err)

	// Cleanup should not have been called since nothing was deleted
	s.Empty(calledWith)
}

// TestGetObservationCount tests observation counting.
func (s *ObservationStoreSuite) TestGetObservationCount() {
	ctx := context.Background()

	// Create observations for project-a
	for i := 0; i < 5; i++ {
		obs := &models.ParsedObservation{
			Type: models.ObsTypeDiscovery,
		}
		_, _, err := s.obsStore.StoreObservation(ctx, "session-count", "project-a", obs, i, 10)
		s.NoError(err)
	}

	// Create global observation
	globalObs := &models.ParsedObservation{
		Type:     models.ObsTypeDiscovery,
		Concepts: []string{"security"},
	}
	_, _, err := s.obsStore.StoreObservation(ctx, "session-count", "project-a", globalObs, 6, 10)
	s.NoError(err)

	// Project-a should count 6 (5 project + 1 global)
	count, err := s.obsStore.GetObservationCount(ctx, "project-a")
	s.NoError(err)
	s.Equal(6, count)

	// Project-b should count 1 (only global)
	count, err = s.obsStore.GetObservationCount(ctx, "project-b")
	s.NoError(err)
	s.Equal(1, count)
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

func TestObservationStore_GetObservationsByProjectStrict(t *testing.T) {
	obsStore, _, cleanup := testObservationStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create project-scoped observation for project-a
	projectObs := &models.ParsedObservation{
		Type:      models.ObsTypeDiscovery,
		Title:     "Project A specific",
		Narrative: "Only for project-a",
		Concepts:  []string{"local-concept"},
	}
	_, _, err := obsStore.StoreObservation(ctx, "session-1", "project-a", projectObs, 1, 100)
	require.NoError(t, err)

	// Create global observation from project-a
	globalObs := &models.ParsedObservation{
		Type:      models.ObsTypeDiscovery,
		Title:     "Global security practice",
		Narrative: "Best practice for all",
		Concepts:  []string{"security", "best-practice"},
	}
	_, _, err = obsStore.StoreObservation(ctx, "session-1", "project-a", globalObs, 2, 100)
	require.NoError(t, err)

	// Create observation for project-b
	projectBObs := &models.ParsedObservation{
		Type:      models.ObsTypeDiscovery,
		Title:     "Project B specific",
		Narrative: "Only for project-b",
	}
	_, _, err = obsStore.StoreObservation(ctx, "session-1", "project-b", projectBObs, 1, 100)
	require.NoError(t, err)

	// GetObservationsByProjectStrict for project-a should only return project-a observations
	// This is different from GetRecentObservations which includes globals from other projects
	results, err := obsStore.GetObservationsByProjectStrict(ctx, "project-a", 10)
	require.NoError(t, err)
	assert.Len(t, results, 2) // Only observations created in project-a

	// Verify both are from project-a
	for _, obs := range results {
		assert.Equal(t, "project-a", obs.Project)
	}

	// GetObservationsByProjectStrict for project-b should only return project-b observations
	results, err = obsStore.GetObservationsByProjectStrict(ctx, "project-b", 10)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "Project B specific", results[0].Title.String)
}

func TestObservationStore_SearchObservationsFTS_EmptyQuery(t *testing.T) {
	obsStore, _, cleanup := testObservationStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create an observation
	obs := &models.ParsedObservation{
		Type:      models.ObsTypeDiscovery,
		Title:     "Test observation",
		Narrative: "Some content here",
	}
	_, _, err := obsStore.StoreObservation(ctx, "session-1", "project-a", obs, 1, 100)
	require.NoError(t, err)

	// Search with only stop words (should return nil)
	results, err := obsStore.SearchObservationsFTS(ctx, "the a an is are", "project-a", 10)
	require.NoError(t, err)
	assert.Nil(t, results)

	// Search with empty query
	results, err = obsStore.SearchObservationsFTS(ctx, "", "project-a", 10)
	require.NoError(t, err)
	assert.Nil(t, results)
}

func TestObservationStore_SearchObservationsFTS_DefaultLimit(t *testing.T) {
	obsStore, _, cleanup := testObservationStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create observations
	for i := 0; i < 15; i++ {
		obs := &models.ParsedObservation{
			Type:      models.ObsTypeDiscovery,
			Title:     "Authentication test " + string(rune('A'+i)),
			Narrative: "Auth related content",
		}
		_, _, err := obsStore.StoreObservation(ctx, "session-1", "project-a", obs, i+1, 100)
		require.NoError(t, err)
		time.Sleep(time.Millisecond)
	}

	// Search with limit 0 (should default to 10)
	results, err := obsStore.SearchObservationsFTS(ctx, "authentication", "project-a", 0)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(results), 10)

	// Search with negative limit (should default to 10)
	results, err = obsStore.SearchObservationsFTS(ctx, "authentication", "project-a", -5)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(results), 10)
}

func TestObservationStore_GetAllRecentObservations(t *testing.T) {
	obsStore, _, cleanup := testObservationStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create observations across different projects
	projects := []string{"project-a", "project-b", "project-c"}
	for _, proj := range projects {
		for i := 0; i < 3; i++ {
			obs := &models.ParsedObservation{
				Type:      models.ObsTypeDiscovery,
				Title:     proj + " observation " + string(rune('A'+i)),
				Narrative: "Content for " + proj,
			}
			_, _, err := obsStore.StoreObservation(ctx, "session-1", proj, obs, i+1, 100)
			require.NoError(t, err)
			time.Sleep(time.Millisecond)
		}
	}

	// Get all recent observations
	results, err := obsStore.GetAllRecentObservations(ctx, 100)
	require.NoError(t, err)
	assert.Len(t, results, 9) // 3 projects * 3 observations

	// Verify they are in descending order by epoch
	for i := 1; i < len(results); i++ {
		assert.GreaterOrEqual(t, results[i-1].CreatedAtEpoch, results[i].CreatedAtEpoch)
	}

	// Test with limit
	results, err = obsStore.GetAllRecentObservations(ctx, 5)
	require.NoError(t, err)
	assert.Len(t, results, 5)
}
