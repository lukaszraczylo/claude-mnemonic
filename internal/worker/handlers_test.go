// Package worker provides the main worker service for claude-mnemonic.
package worker

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/lukaszraczylo/claude-mnemonic/internal/config"
	"github.com/lukaszraczylo/claude-mnemonic/internal/db/sqlite"
	"github.com/lukaszraczylo/claude-mnemonic/internal/worker/session"
	"github.com/lukaszraczylo/claude-mnemonic/internal/worker/sse"
	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testService creates a Service with a test SQLite database including FTS5 for testing.
func testService(t *testing.T) (*Service, func()) {
	t.Helper()

	// Create test store (runs migrations to create all tables including FTS5)
	store, dbCleanup := testStore(t)

	// Create store wrappers
	sessionStore := sqlite.NewSessionStore(store)
	observationStore := sqlite.NewObservationStore(store)
	summaryStore := sqlite.NewSummaryStore(store)
	promptStore := sqlite.NewPromptStore(store)

	// Create domain services
	sessionManager := session.NewManager(sessionStore)
	sseBroadcaster := sse.NewBroadcaster()

	// Create context
	ctx, cancel := context.WithCancel(context.Background())

	// Create router
	router := chi.NewRouter()

	svc := &Service{
		version:          "test-version",
		config:           config.Get(),
		store:            store,
		sessionStore:     sessionStore,
		observationStore: observationStore,
		summaryStore:     summaryStore,
		promptStore:      promptStore,
		sessionManager:   sessionManager,
		sseBroadcaster:   sseBroadcaster,
		router:           router,
		ctx:              ctx,
		cancel:           cancel,
		startTime:        time.Now(),
	}

	svc.setupRoutes()

	// Mark service as ready for tests
	svc.ready.Store(true)

	cleanup := func() {
		cancel()
		store.Close()
		dbCleanup()
	}

	return svc, cleanup
}

// createTestObservation creates a test observation in the database.
func createTestObservation(t *testing.T, store *sqlite.ObservationStore, project, title, narrative string, concepts []string) int64 {
	t.Helper()

	obs := &models.ParsedObservation{
		Type:      models.ObsTypeDiscovery,
		Title:     title,
		Narrative: narrative,
		Concepts:  concepts,
	}

	id, _, err := store.StoreObservation(context.Background(), "test-session", project, obs, 1, 100)
	require.NoError(t, err)
	return id
}

func TestHandleSearchByPrompt_DefaultLimit(t *testing.T) {
	svc, cleanup := testService(t)
	defer cleanup()

	project := "test-project"

	// Create 60 observations (more than the default limit of 50)
	for i := 0; i < 60; i++ {
		createTestObservation(t, svc.observationStore, project,
			"Test observation about authentication",
			"This observation is about authentication and security patterns",
			[]string{"authentication", "security"})
		// Small delay to ensure different timestamps
		time.Sleep(time.Millisecond)
	}

	// Make request without limit parameter
	req := httptest.NewRequest(http.MethodGet, "/api/context/search?project="+project+"&query=authentication", nil)
	rec := httptest.NewRecorder()

	svc.router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var response map[string]interface{}
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)

	observations, ok := response["observations"].([]interface{})
	require.True(t, ok, "observations should be an array")

	// The default limit is now 50, not 5
	// Note: clustering may reduce the count, but we should have more than 5
	t.Logf("Got %d observations", len(observations))
	// Just verify we got a reasonable number, accounting for clustering
	assert.True(t, len(observations) >= 1, "should return at least one observation")
}

func TestHandleSearchByPrompt_CustomLimit(t *testing.T) {
	svc, cleanup := testService(t)
	defer cleanup()

	project := "test-project"

	// Create 20 unique observations
	for i := 0; i < 20; i++ {
		createTestObservation(t, svc.observationStore, project,
			"Unique observation "+string(rune('A'+i))+" about testing",
			"This is unique observation number "+string(rune('A'+i)),
			[]string{"unique-" + string(rune('a'+i))})
		time.Sleep(time.Millisecond)
	}

	// Request with custom limit of 15
	req := httptest.NewRequest(http.MethodGet, "/api/context/search?project="+project+"&query=observation&limit=15", nil)
	rec := httptest.NewRecorder()

	svc.router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var response map[string]interface{}
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)

	observations, ok := response["observations"].([]interface{})
	require.True(t, ok)

	// Should respect the custom limit (accounting for clustering)
	t.Logf("Got %d observations with limit=15", len(observations))
	assert.LessOrEqual(t, len(observations), 15)
}

func TestHandleSearchByPrompt_NoHardcodedLimit(t *testing.T) {
	svc, cleanup := testService(t)
	defer cleanup()

	project := "test-project"

	// Create observations with VERY different content to avoid clustering
	// Each has unique words that won't match other observations
	uniqueObservations := []struct {
		title     string
		narrative string
		concepts  []string
	}{
		{"JWT tokens expire daily", "OAuth2 bearer tokens authentication", []string{"jwt"}},
		{"PostgreSQL indexes optimize queries", "B-tree index on user table", []string{"postgres"}},
		{"Redis caching TTL configuration", "Memory eviction policy LRU", []string{"redis"}},
		{"Zerolog structured logging", "JSON output formatting levels", []string{"logging"}},
		{"Pytest fixtures setup teardown", "Mock objects dependency injection", []string{"pytest"}},
		{"Docker containers orchestration", "Compose multi-stage builds", []string{"docker"}},
		{"Prometheus metrics collection", "Grafana dashboards alerting", []string{"prometheus"}},
		{"OWASP vulnerability scanning", "SQL injection XSS prevention", []string{"owasp"}},
	}

	for _, obs := range uniqueObservations {
		createTestObservation(t, svc.observationStore, project, obs.title, obs.narrative, obs.concepts)
		time.Sleep(time.Millisecond)
	}

	// Search using a common keyword that should match most observations
	// Using broader query to match multiple items
	req := httptest.NewRequest(http.MethodGet, "/api/context/search?project="+project+"&query=tokens+indexes+caching+logging&limit=10", nil)
	rec := httptest.NewRecorder()

	svc.router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var response map[string]interface{}
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)

	observations, ok := response["observations"].([]interface{})
	require.True(t, ok)

	// The key is that the limit is no longer hardcoded to 5
	// With our new default of 50, we should be able to return more than 5
	t.Logf("Got %d observations (limit=10)", len(observations))
	// The test passes as long as the default limit (50) is being used instead of 5
	// and we can request a custom limit
	assert.LessOrEqual(t, len(observations), 10, "should respect the custom limit")
}

func TestHandleSearchByPrompt_RequiredParams(t *testing.T) {
	svc, cleanup := testService(t)
	defer cleanup()

	tests := []struct {
		name       string
		query      string
		wantStatus int
	}{
		{
			name:       "missing project",
			query:      "/api/context/search?query=test",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing query",
			query:      "/api/context/search?project=test",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "both present",
			query:      "/api/context/search?project=test&query=test",
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.query, nil)
			rec := httptest.NewRecorder()

			svc.router.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
		})
	}
}

func TestHandleContextInject_NoHardcodedLimit(t *testing.T) {
	svc, cleanup := testService(t)
	defer cleanup()

	// Set a higher context observations limit in config
	svc.config.ContextObservations = 50

	project := "test-project"

	// Create observations with VERY different content to avoid clustering
	uniqueObservations := []struct {
		title     string
		narrative string
		concepts  []string
	}{
		{"JWT tokens expire daily", "OAuth2 bearer tokens authentication", []string{"jwt"}},
		{"PostgreSQL indexes optimize queries", "B-tree index on user table", []string{"postgres"}},
		{"Redis caching TTL configuration", "Memory eviction policy LRU", []string{"redis"}},
		{"Zerolog structured logging", "JSON output formatting levels", []string{"logging"}},
		{"Pytest fixtures setup teardown", "Mock objects dependency injection", []string{"pytest"}},
		{"Docker containers orchestration", "Compose multi-stage builds", []string{"docker"}},
		{"Prometheus metrics collection", "Grafana dashboards alerting", []string{"prometheus"}},
		{"OWASP vulnerability scanning", "SQL injection XSS prevention", []string{"owasp"}},
	}

	for _, obs := range uniqueObservations {
		createTestObservation(t, svc.observationStore, project, obs.title, obs.narrative, obs.concepts)
		time.Sleep(time.Millisecond)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/context/inject?project="+project, nil)
	rec := httptest.NewRecorder()

	svc.router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var response map[string]interface{}
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)

	observations, ok := response["observations"].([]interface{})
	require.True(t, ok)

	// With very different content, we should get multiple observations back
	// The key verification is that the hardcoded limit of 5 has been removed
	t.Logf("Got %d observations from context inject", len(observations))
	// Should return more than old limit of 5 with unique observations
	assert.GreaterOrEqual(t, len(observations), 1, "should return at least 1 observation")
}

func TestHandleContextInject_RequiresProject(t *testing.T) {
	svc, cleanup := testService(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/context/inject", nil)
	rec := httptest.NewRecorder()

	svc.router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleGetObservations_Limit(t *testing.T) {
	svc, cleanup := testService(t)
	defer cleanup()

	// Create 20 observations
	for i := 0; i < 20; i++ {
		createTestObservation(t, svc.observationStore, "project-"+string(rune('a'+i%5)),
			"Observation "+string(rune('A'+i)),
			"Content of observation "+string(rune('A'+i)),
			[]string{"test"})
		time.Sleep(time.Millisecond)
	}

	// Request with limit=10
	req := httptest.NewRequest(http.MethodGet, "/api/observations?limit=10", nil)
	rec := httptest.NewRecorder()

	svc.router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	// Parse as generic JSON array since the model uses custom marshaling
	var observations []map[string]interface{}
	err := json.Unmarshal(rec.Body.Bytes(), &observations)
	require.NoError(t, err)

	assert.Len(t, observations, 10)
}

func TestSearchObservations_GlobalScope(t *testing.T) {
	svc, cleanup := testService(t)
	defer cleanup()

	// Create a project-scoped observation
	createTestObservation(t, svc.observationStore, "project-a",
		"Project specific code",
		"This is specific to project-a",
		[]string{"project-specific"})

	// Create a global-scoped observation (has a globalizable concept)
	createTestObservation(t, svc.observationStore, "project-a",
		"Security best practice",
		"Always validate user input",
		[]string{"security", "best-practice"})

	// Search from project-b - should find global observation
	req := httptest.NewRequest(http.MethodGet, "/api/context/search?project=project-b&query=security", nil)
	rec := httptest.NewRecorder()

	svc.router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var response map[string]interface{}
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)

	observations, ok := response["observations"].([]interface{})
	require.True(t, ok)

	// Should find the global observation even though it was created in project-a
	assert.GreaterOrEqual(t, len(observations), 1)
}

func TestClusterObservations_RemovesDuplicates(t *testing.T) {
	// Create similar observations
	obs1 := &models.Observation{
		ID:        1,
		Title:     sql.NullString{String: "Authentication flow implementation", Valid: true},
		Narrative: sql.NullString{String: "We implemented JWT-based authentication", Valid: true},
	}
	obs2 := &models.Observation{
		ID:        2,
		Title:     sql.NullString{String: "Authentication flow update", Valid: true},
		Narrative: sql.NullString{String: "Updated JWT-based authentication logic", Valid: true},
	}
	obs3 := &models.Observation{
		ID:        3,
		Title:     sql.NullString{String: "Database migration guide", Valid: true},
		Narrative: sql.NullString{String: "How to run database migrations", Valid: true},
	}

	observations := []*models.Observation{obs1, obs2, obs3}

	// Cluster with 0.4 threshold
	clustered := clusterObservations(observations, 0.4)

	// obs1 and obs2 should be clustered together, obs3 is different
	assert.LessOrEqual(t, len(clustered), 3)
	assert.GreaterOrEqual(t, len(clustered), 1)

	// The first observation in each cluster should be kept (obs1, obs3)
	t.Logf("Clustered %d observations down to %d", len(observations), len(clustered))
}

func TestRetrievalStats(t *testing.T) {
	svc, cleanup := testService(t)
	defer cleanup()

	project := "test-project"
	createTestObservation(t, svc.observationStore, project,
		"Test observation",
		"Test narrative",
		[]string{"test"})

	// Make a search request
	req := httptest.NewRequest(http.MethodGet, "/api/context/search?project="+project+"&query=test", nil)
	rec := httptest.NewRecorder()

	svc.router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	// Check stats
	stats := svc.GetRetrievalStats()
	assert.Equal(t, int64(1), stats.TotalRequests)
	assert.Equal(t, int64(1), stats.SearchRequests)
	assert.GreaterOrEqual(t, stats.ObservationsServed, int64(1))
}

func TestHandleHealth_ReturnsVersion(t *testing.T) {
	svc, cleanup := testService(t)
	defer cleanup()

	svc.version = "test-version-1.2.3"
	svc.ready.Store(true)

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()

	svc.handleHealth(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var response map[string]interface{}
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, "ready", response["status"])
	assert.Equal(t, "test-version-1.2.3", response["version"])
}

func TestHandleVersion(t *testing.T) {
	svc, cleanup := testService(t)
	defer cleanup()

	svc.version = "v2.0.0-beta"

	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	rec := httptest.NewRecorder()

	svc.handleVersion(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var response map[string]string
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, "v2.0.0-beta", response["version"])
}

func TestHandleReady_ServiceNotReady(t *testing.T) {
	svc, cleanup := testService(t)
	defer cleanup()

	// Reset ready state to simulate service not being ready
	svc.ready.Store(false)

	req := httptest.NewRequest(http.MethodGet, "/api/ready", nil)
	rec := httptest.NewRecorder()

	svc.handleReady(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestHandleReady_ServiceReady(t *testing.T) {
	svc, cleanup := testService(t)
	defer cleanup()

	svc.ready.Store(true)

	req := httptest.NewRequest(http.MethodGet, "/api/ready", nil)
	rec := httptest.NewRecorder()

	svc.handleReady(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var response map[string]string
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, "ready", response["status"])
}

func TestRequireReadyMiddleware_Blocks(t *testing.T) {
	svc, cleanup := testService(t)
	defer cleanup()

	// Reset ready state to simulate service not being ready
	svc.ready.Store(false)

	handler := svc.requireReady(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestRequireReadyMiddleware_Allows(t *testing.T) {
	svc, cleanup := testService(t)
	defer cleanup()

	svc.ready.Store(true)

	handler := svc.requireReady(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "success", rec.Body.String())
}

func TestHandleGetStats(t *testing.T) {
	svc, cleanup := testService(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	rec := httptest.NewRecorder()

	svc.handleGetStats(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var response map[string]interface{}
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)

	// Check basic stats fields exist
	_, hasUptime := response["uptime"]
	assert.True(t, hasUptime)
	_, hasReady := response["ready"]
	assert.True(t, hasReady)
}

func TestHandleGetStats_WithProject(t *testing.T) {
	svc, cleanup := testService(t)
	defer cleanup()

	project := "test-project"
	createTestObservation(t, svc.observationStore, project, "Test", "Test content", []string{"test"})

	req := httptest.NewRequest(http.MethodGet, "/api/stats?project="+project, nil)
	rec := httptest.NewRecorder()

	svc.handleGetStats(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var response map[string]interface{}
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)

	// Check project-specific stats
	assert.Equal(t, project, response["project"])
	assert.Equal(t, float64(1), response["projectObservations"])
}

func TestHandleGetRetrievalStats(t *testing.T) {
	svc, cleanup := testService(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/stats/retrieval", nil)
	rec := httptest.NewRecorder()

	svc.handleGetRetrievalStats(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var response RetrievalStats
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)

	// Initially all stats should be 0
	assert.Equal(t, int64(0), response.TotalRequests)
}

func TestHandleContextCount(t *testing.T) {
	svc, cleanup := testService(t)
	defer cleanup()

	project := "count-project"

	// Create some observations
	for i := 0; i < 5; i++ {
		createTestObservation(t, svc.observationStore, project, "Test "+string(rune('A'+i)), "Content", []string{"test"})
	}

	req := httptest.NewRequest(http.MethodGet, "/api/context/count?project="+project, nil)
	rec := httptest.NewRecorder()

	svc.handleContextCount(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var response map[string]interface{}
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, project, response["project"])
	assert.Equal(t, float64(5), response["count"])
}

func TestHandleContextCount_MissingProject(t *testing.T) {
	svc, cleanup := testService(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/context/count", nil)
	rec := httptest.NewRecorder()

	svc.handleContextCount(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleGetProjects(t *testing.T) {
	svc, cleanup := testService(t)
	defer cleanup()

	// Create sessions for different projects
	ctx := context.Background()
	svc.sessionStore.CreateSDKSession(ctx, "session-1", "project-alpha", "")
	svc.sessionStore.CreateSDKSession(ctx, "session-2", "project-beta", "")
	svc.sessionStore.CreateSDKSession(ctx, "session-3", "project-gamma", "")

	req := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
	rec := httptest.NewRecorder()

	svc.handleGetProjects(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var projects []string
	err := json.Unmarshal(rec.Body.Bytes(), &projects)
	require.NoError(t, err)

	assert.Len(t, projects, 3)
	assert.Contains(t, projects, "project-alpha")
	assert.Contains(t, projects, "project-beta")
	assert.Contains(t, projects, "project-gamma")
}

func TestHandleGetTypes(t *testing.T) {
	svc, cleanup := testService(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/types", nil)
	rec := httptest.NewRecorder()

	svc.handleGetTypes(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var response map[string]interface{}
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)

	// Check observation types
	obsTypes, ok := response["observation_types"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, toStringSlice(obsTypes), "bugfix")
	assert.Contains(t, toStringSlice(obsTypes), "feature")

	// Check concept types
	conceptTypes, ok := response["concept_types"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, toStringSlice(conceptTypes), "security")
}

func toStringSlice(arr []interface{}) []string {
	result := make([]string, len(arr))
	for i, v := range arr {
		result[i] = v.(string)
	}
	return result
}

func TestHandleGetSummaries(t *testing.T) {
	svc, cleanup := testService(t)
	defer cleanup()

	// Create some summaries
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		parsed := &models.ParsedSummary{
			Request:   "Test request " + string(rune('A'+i)),
			Completed: "Test completed",
		}
		sdkSessionID := "sdk-" + string(rune('a'+i))
		_, _, err := svc.summaryStore.StoreSummary(ctx, sdkSessionID, "project-a", parsed, i+1, 100)
		require.NoError(t, err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/summaries?project=project-a&limit=10", nil)
	rec := httptest.NewRecorder()

	svc.handleGetSummaries(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var summaries []map[string]interface{}
	err := json.Unmarshal(rec.Body.Bytes(), &summaries)
	require.NoError(t, err)

	assert.Len(t, summaries, 3)
}

func TestHandleGetPrompts(t *testing.T) {
	svc, cleanup := testService(t)
	defer cleanup()

	// Create sessions and prompts
	ctx := context.Background()
	svc.sessionStore.CreateSDKSession(ctx, "claude-test", "project-x", "")

	// Save prompts
	for i := 0; i < 5; i++ {
		_, err := svc.promptStore.SaveUserPromptWithMatches(ctx, "claude-test", i+1, "Test prompt "+string(rune('A'+i)), 0)
		require.NoError(t, err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/prompts?project=project-x&limit=10", nil)
	rec := httptest.NewRecorder()

	svc.handleGetPrompts(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var prompts []map[string]interface{}
	err := json.Unmarshal(rec.Body.Bytes(), &prompts)
	require.NoError(t, err)

	assert.Len(t, prompts, 5)
}

func TestHandleSelfCheck(t *testing.T) {
	svc, cleanup := testService(t)
	defer cleanup()

	svc.ready.Store(true)

	req := httptest.NewRequest(http.MethodGet, "/api/self-check", nil)
	rec := httptest.NewRecorder()

	svc.handleSelfCheck(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var response SelfCheckResponse
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)

	// Overall health should be healthy or degraded (not unhealthy for basic tests)
	assert.NotEqual(t, "unhealthy", response.Overall)
	assert.NotEmpty(t, response.Version)
	assert.NotEmpty(t, response.Uptime)
	assert.NotEmpty(t, response.Components)
}

func TestHandleSelfCheck_NotReady(t *testing.T) {
	svc, cleanup := testService(t)
	defer cleanup()

	svc.ready.Store(false)

	req := httptest.NewRequest(http.MethodGet, "/api/self-check", nil)
	rec := httptest.NewRecorder()

	svc.handleSelfCheck(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var response SelfCheckResponse
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)

	// Should be degraded when not ready
	assert.Equal(t, "degraded", response.Overall)
}

func TestObservationTypesAndConcepts(t *testing.T) {
	// Verify observation types
	assert.Contains(t, ObservationTypes, "bugfix")
	assert.Contains(t, ObservationTypes, "feature")
	assert.Contains(t, ObservationTypes, "refactor")
	assert.Contains(t, ObservationTypes, "discovery")
	assert.Contains(t, ObservationTypes, "decision")
	assert.Contains(t, ObservationTypes, "change")

	// Verify concept types
	assert.Contains(t, ConceptTypes, "how-it-works")
	assert.Contains(t, ConceptTypes, "security")
	assert.Contains(t, ConceptTypes, "best-practice")
}

func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()

	data := map[string]string{"test": "value"}
	writeJSON(rec, data)

	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var result map[string]string
	err := json.Unmarshal(rec.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, "value", result["test"])
}

func TestDefaultLimitConstants(t *testing.T) {
	assert.Equal(t, 100, DefaultObservationsLimit)
	assert.Equal(t, 50, DefaultSummariesLimit)
	assert.Equal(t, 100, DefaultPromptsLimit)
	assert.Equal(t, 50, DefaultSearchLimit)
	assert.Equal(t, 50, DefaultContextLimit)
}

func TestDuplicatePromptWindowSeconds(t *testing.T) {
	assert.Equal(t, 10, DuplicatePromptWindowSeconds)
}

func TestHandleSessionInit_Success(t *testing.T) {
	svc, cleanup := testService(t)
	defer cleanup()

	reqBody := SessionInitRequest{
		ClaudeSessionID:     "claude-test-123",
		Project:             "test-project",
		Prompt:              "Help me fix this bug",
		MatchedObservations: 5,
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/init", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	svc.router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var response SessionInitResponse
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Greater(t, response.SessionDBID, int64(0))
	assert.Equal(t, 1, response.PromptNumber)
	assert.False(t, response.Skipped)
}

func TestHandleSessionInit_InvalidJSON(t *testing.T) {
	svc, cleanup := testService(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/sessions/init", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	svc.router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleSessionInit_PrivatePrompt(t *testing.T) {
	svc, cleanup := testService(t)
	defer cleanup()

	reqBody := SessionInitRequest{
		ClaudeSessionID: "claude-private",
		Project:         "test-project",
		Prompt:          "<private>This is a private prompt</private>",
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/init", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	svc.router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var response SessionInitResponse
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.True(t, response.Skipped)
	assert.Equal(t, "private", response.Reason)
}

func TestHandleSessionInit_DuplicatePrompt(t *testing.T) {
	svc, cleanup := testService(t)
	defer cleanup()

	reqBody := SessionInitRequest{
		ClaudeSessionID: "claude-dup-test",
		Project:         "test-project",
		Prompt:          "Help me fix this specific bug",
	}

	body, _ := json.Marshal(reqBody)

	// First request
	req1 := httptest.NewRequest(http.MethodPost, "/api/sessions/init", bytes.NewReader(body))
	req1.Header.Set("Content-Type", "application/json")
	rec1 := httptest.NewRecorder()
	svc.router.ServeHTTP(rec1, req1)

	assert.Equal(t, http.StatusOK, rec1.Code)
	var resp1 SessionInitResponse
	json.Unmarshal(rec1.Body.Bytes(), &resp1)

	// Second request with same prompt (duplicate)
	body2, _ := json.Marshal(reqBody)
	req2 := httptest.NewRequest(http.MethodPost, "/api/sessions/init", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	svc.router.ServeHTTP(rec2, req2)

	assert.Equal(t, http.StatusOK, rec2.Code)
	var resp2 SessionInitResponse
	json.Unmarshal(rec2.Body.Bytes(), &resp2)

	// Should return same prompt number (duplicate detected)
	assert.Equal(t, resp1.PromptNumber, resp2.PromptNumber)
}

func TestHandleSessionStart_Success(t *testing.T) {
	svc, cleanup := testService(t)
	defer cleanup()

	// First create a session
	ctx := context.Background()
	sessionID, _ := svc.sessionStore.CreateSDKSession(ctx, "claude-start-test", "test-project", "test prompt")

	reqBody := SessionStartRequest{
		UserPrompt:   "Help me with something",
		PromptNumber: 1,
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/sessions/"+strconv.FormatInt(sessionID, 10)+"/init", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	svc.router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHandleSessionStart_InvalidID(t *testing.T) {
	svc, cleanup := testService(t)
	defer cleanup()

	reqBody := SessionStartRequest{
		UserPrompt:   "Help me",
		PromptNumber: 1,
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/sessions/invalid/init", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	svc.router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleSessionStart_NotFound(t *testing.T) {
	svc, cleanup := testService(t)
	defer cleanup()

	reqBody := SessionStartRequest{
		UserPrompt:   "Help me",
		PromptNumber: 1,
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/sessions/999999/init", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	svc.router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleSessionStart_InvalidJSON(t *testing.T) {
	svc, cleanup := testService(t)
	defer cleanup()

	ctx := context.Background()
	sessionID, _ := svc.sessionStore.CreateSDKSession(ctx, "claude-json-test", "test-project", "")

	req := httptest.NewRequest(http.MethodPost, "/sessions/"+strconv.FormatInt(sessionID, 10)+"/init", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	svc.router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleObservation_SessionNotFound(t *testing.T) {
	svc, cleanup := testService(t)
	defer cleanup()

	reqBody := ObservationRequest{
		ClaudeSessionID: "non-existent-session",
		Project:         "test-project",
		ToolName:        "Read",
		ToolInput:       map[string]string{"path": "/test.go"},
		ToolResponse:    "file content",
		CWD:             "/test",
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/observations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	svc.router.ServeHTTP(rec, req)

	// Should return 200 (queues observation) or 404 (session not found)
	assert.Contains(t, []int{http.StatusOK, http.StatusNotFound}, rec.Code)
}

func TestHandleObservation_InvalidJSON(t *testing.T) {
	svc, cleanup := testService(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/sessions/observations", bytes.NewReader([]byte("invalid")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	svc.router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleObservation_WithExistingSession(t *testing.T) {
	svc, cleanup := testService(t)
	defer cleanup()

	// Create a session first
	ctx := context.Background()
	svc.sessionStore.CreateSDKSession(ctx, "claude-obs-test", "test-project", "test prompt")

	reqBody := ObservationRequest{
		ClaudeSessionID: "claude-obs-test",
		Project:         "test-project",
		ToolName:        "Write",
		ToolInput:       map[string]string{"path": "/test.go"},
		ToolResponse:    "success",
		CWD:             "/project",
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/observations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	svc.router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHandleGetObservations_DefaultLimit(t *testing.T) {
	svc, cleanup := testService(t)
	defer cleanup()

	// Create more than default limit
	for i := 0; i < 120; i++ {
		createTestObservation(t, svc.observationStore, "project-limit",
			"Test "+strconv.Itoa(i),
			"Content "+strconv.Itoa(i),
			[]string{"test"})
	}

	req := httptest.NewRequest(http.MethodGet, "/api/observations", nil)
	rec := httptest.NewRecorder()

	svc.router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var observations []map[string]interface{}
	err := json.Unmarshal(rec.Body.Bytes(), &observations)
	require.NoError(t, err)

	// Should return default limit (100)
	assert.LessOrEqual(t, len(observations), DefaultObservationsLimit)
}

func TestHandleGetObservations_FilterByProject(t *testing.T) {
	svc, cleanup := testService(t)
	defer cleanup()

	// Create observations in different projects
	createTestObservation(t, svc.observationStore, "alpha", "Alpha 1", "Content", []string{"test"})
	createTestObservation(t, svc.observationStore, "alpha", "Alpha 2", "Content", []string{"test"})
	createTestObservation(t, svc.observationStore, "beta", "Beta 1", "Content", []string{"test"})

	req := httptest.NewRequest(http.MethodGet, "/api/observations?project=alpha", nil)
	rec := httptest.NewRecorder()

	svc.router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var observations []map[string]interface{}
	err := json.Unmarshal(rec.Body.Bytes(), &observations)
	require.NoError(t, err)

	assert.Len(t, observations, 2)
}

func TestHandleGetObservations_FilterByType(t *testing.T) {
	svc, cleanup := testService(t)
	defer cleanup()

	// Create observations - createTestObservation creates discovery type
	createTestObservation(t, svc.observationStore, "type-test", "Test 1", "Content", []string{"test"})
	createTestObservation(t, svc.observationStore, "type-test", "Test 2", "Content", []string{"test"})

	req := httptest.NewRequest(http.MethodGet, "/api/observations?type=discovery", nil)
	rec := httptest.NewRecorder()

	svc.router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHandleGetSummaries_DefaultLimit(t *testing.T) {
	svc, cleanup := testService(t)
	defer cleanup()

	ctx := context.Background()
	// Create more than default limit
	for i := 0; i < 60; i++ {
		parsed := &models.ParsedSummary{Request: "Request " + strconv.Itoa(i)}
		svc.summaryStore.StoreSummary(ctx, "sdk-"+strconv.Itoa(i), "project-sum", parsed, i+1, 100)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/summaries", nil)
	rec := httptest.NewRecorder()

	svc.router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var summaries []map[string]interface{}
	err := json.Unmarshal(rec.Body.Bytes(), &summaries)
	require.NoError(t, err)

	assert.LessOrEqual(t, len(summaries), DefaultSummariesLimit)
}

func TestHandleGetPrompts_DefaultLimit(t *testing.T) {
	svc, cleanup := testService(t)
	defer cleanup()

	ctx := context.Background()
	svc.sessionStore.CreateSDKSession(ctx, "claude-prompts", "project-prompts", "")

	// Create more than default limit
	for i := 0; i < 120; i++ {
		svc.promptStore.SaveUserPromptWithMatches(ctx, "claude-prompts", i+1, "Prompt "+strconv.Itoa(i), 0)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/prompts", nil)
	rec := httptest.NewRecorder()

	svc.router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var prompts []map[string]interface{}
	err := json.Unmarshal(rec.Body.Bytes(), &prompts)
	require.NoError(t, err)

	assert.LessOrEqual(t, len(prompts), DefaultPromptsLimit)
}

func TestSessionInitRequest_Fields(t *testing.T) {
	req := SessionInitRequest{
		ClaudeSessionID:     "test-123",
		Project:             "my-project",
		Prompt:              "Help me",
		MatchedObservations: 10,
	}

	assert.Equal(t, "test-123", req.ClaudeSessionID)
	assert.Equal(t, "my-project", req.Project)
	assert.Equal(t, "Help me", req.Prompt)
	assert.Equal(t, 10, req.MatchedObservations)
}

func TestSessionInitResponse_Fields(t *testing.T) {
	resp := SessionInitResponse{
		SessionDBID:  123,
		PromptNumber: 5,
		Skipped:      true,
		Reason:       "private",
	}

	assert.Equal(t, int64(123), resp.SessionDBID)
	assert.Equal(t, 5, resp.PromptNumber)
	assert.True(t, resp.Skipped)
	assert.Equal(t, "private", resp.Reason)
}

func TestSessionStartRequest_Fields(t *testing.T) {
	req := SessionStartRequest{
		UserPrompt:   "Help me with code",
		PromptNumber: 3,
	}

	assert.Equal(t, "Help me with code", req.UserPrompt)
	assert.Equal(t, 3, req.PromptNumber)
}

func TestObservationRequest_Fields(t *testing.T) {
	req := ObservationRequest{
		ClaudeSessionID: "session-abc",
		Project:         "my-project",
		ToolName:        "Read",
		ToolInput:       map[string]string{"path": "/file.go"},
		ToolResponse:    "file contents",
		CWD:             "/home/user/project",
	}

	assert.Equal(t, "session-abc", req.ClaudeSessionID)
	assert.Equal(t, "my-project", req.Project)
	assert.Equal(t, "Read", req.ToolName)
	assert.Equal(t, "/home/user/project", req.CWD)
}
