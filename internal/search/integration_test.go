// Package search provides unified search capabilities for claude-mnemonic.
package search

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/lukaszraczylo/claude-mnemonic/internal/db/sqlite"
	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	// Import sqlite driver
	_ "github.com/mattn/go-sqlite3"
)

// Ensure context is used (for later tests)
var _ = context.Background

// hasFTS5 checks if FTS5 is available in the SQLite build.
func hasFTS5(t *testing.T) bool {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "fts5-check-*")
	if err != nil {
		return false
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	dbPath := tmpDir + "/check.db"
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return false
	}
	defer func() { _ = db.Close() }()

	_, err = db.Exec("CREATE VIRTUAL TABLE IF NOT EXISTS fts5_test USING fts5(content)")
	if err != nil {
		return false
	}
	_, _ = db.Exec("DROP TABLE IF EXISTS fts5_test")
	return true
}

// testStore creates a sqlite.Store with a temporary database for testing.
func testStore(t *testing.T) (*sqlite.Store, func()) {
	t.Helper()

	if !hasFTS5(t) {
		t.Skip("FTS5 not available in this SQLite build")
	}

	tmpDir, err := os.MkdirTemp("", "search-integration-test-*")
	require.NoError(t, err)

	dbPath := tmpDir + "/test.db"

	store, err := sqlite.NewStore(sqlite.StoreConfig{
		Path:     dbPath,
		MaxConns: 1,
		WALMode:  true,
	})
	require.NoError(t, err)

	cleanup := func() {
		_ = store.Close()
		_ = os.RemoveAll(tmpDir)
	}

	return store, cleanup
}

// SearchIntegrationSuite tests search with real SQLite stores.
type SearchIntegrationSuite struct {
	suite.Suite
	store    *sqlite.Store
	cleanup  func()
	manager  *Manager
	obsStore *sqlite.ObservationStore
	sumStore *sqlite.SummaryStore
	prmStore *sqlite.PromptStore
}

func (s *SearchIntegrationSuite) SetupTest() {
	if !hasFTS5(s.T()) {
		s.T().Skip("FTS5 not available in this SQLite build")
	}

	s.store, s.cleanup = testStore(s.T())

	// Create real stores backed by SQLite
	s.obsStore = sqlite.NewObservationStore(s.store)
	s.sumStore = sqlite.NewSummaryStore(s.store)
	s.prmStore = sqlite.NewPromptStore(s.store)

	// Create search manager with real stores (no vector client for now)
	s.manager = NewManager(s.obsStore, s.sumStore, s.prmStore, nil)
}

func (s *SearchIntegrationSuite) TearDownTest() {
	if s.cleanup != nil {
		s.cleanup()
	}
}

func TestSearchIntegrationSuite(t *testing.T) {
	suite.Run(t, new(SearchIntegrationSuite))
}

// seedObservations inserts test observations into the database.
func (s *SearchIntegrationSuite) seedObservations(ctx context.Context) []int64 {
	var ids []int64

	// Observation 1: Authentication bug fix
	obs1 := &models.ParsedObservation{
		Type:      models.ObsTypeBugfix,
		Scope:     models.ScopeProject,
		Title:     "Fixed Authentication Bug",
		Narrative: "Resolved JWT token validation issue that caused intermittent login failures",
		Concepts:  []string{"authentication", "jwt", "security"},
		FilesRead: []string{"auth/handler.go", "auth/jwt.go"},
	}
	id1, _, err := s.obsStore.StoreObservation(ctx, "sdk-sess-1", "test-project", obs1, 1, 100)
	s.Require().NoError(err)
	ids = append(ids, id1)

	// Observation 2: Database optimization decision
	obs2 := &models.ParsedObservation{
		Type:      models.ObsTypeDecision,
		Scope:     models.ScopeProject,
		Title:     "Database Query Optimization Decision",
		Narrative: "Decided to add indexes on user_id and created_at columns for better performance",
		Concepts:  []string{"database", "performance", "decision"},
		FilesRead: []string{"db/migrations/001.sql"},
	}
	id2, _, err := s.obsStore.StoreObservation(ctx, "sdk-sess-1", "test-project", obs2, 2, 150)
	s.Require().NoError(err)
	ids = append(ids, id2)

	// Observation 3: Global best practice
	obs3 := &models.ParsedObservation{
		Type:      models.ObsTypeDiscovery,
		Scope:     models.ScopeGlobal,
		Title:     "Error Handling Best Practice",
		Narrative: "Use wrapped errors with context for better debugging: errors.Wrap(err, context)",
		Concepts:  []string{"best-practice", "errors", "patterns"},
		FilesRead: []string{"pkg/errors/errors.go"},
	}
	id3, _, err := s.obsStore.StoreObservation(ctx, "sdk-sess-2", "other-project", obs3, 1, 80)
	s.Require().NoError(err)
	ids = append(ids, id3)

	// Observation 4: Code change/refactoring
	obs4 := &models.ParsedObservation{
		Type:          models.ObsTypeChange,
		Scope:         models.ScopeProject,
		Title:         "Refactored User Service",
		Narrative:     "Changed user service to use repository pattern, modified interfaces for better testability",
		Concepts:      []string{"refactoring", "architecture"},
		FilesModified: []string{"services/user.go", "services/user_test.go"},
	}
	id4, _, err := s.obsStore.StoreObservation(ctx, "sdk-sess-1", "test-project", obs4, 3, 200)
	s.Require().NoError(err)
	ids = append(ids, id4)

	return ids
}

// seedSummaries inserts test session summaries into the database.
func (s *SearchIntegrationSuite) seedSummaries(ctx context.Context) []int64 {
	var ids []int64

	// Summary 1
	sum1 := &models.ParsedSummary{
		Request:      "Fix authentication bug",
		Investigated: "JWT token validation and session handling",
		Learned:      "JWT validation requires algorithm check to prevent alg:none attacks",
		Completed:    "Fixed JWT validation, added tests",
		NextSteps:    "Review other security endpoints",
	}
	id1, _, err := s.sumStore.StoreSummary(ctx, "sdk-sess-1", "test-project", sum1, 1, 100)
	s.Require().NoError(err)
	ids = append(ids, id1)

	// Summary 2
	sum2 := &models.ParsedSummary{
		Request:      "Optimize database queries",
		Investigated: "Query execution plans and index usage",
		Learned:      "Composite indexes work better for range queries",
		Completed:    "Added indexes, verified performance improvement",
		NextSteps:    "Monitor query times in production",
	}
	id2, _, err := s.sumStore.StoreSummary(ctx, "sdk-sess-1", "test-project", sum2, 2, 150)
	s.Require().NoError(err)
	ids = append(ids, id2)

	return ids
}

// TestFilterSearch_WithRealStores tests filterSearch with seeded data.
func (s *SearchIntegrationSuite) TestFilterSearch_WithRealStores() {
	ctx := context.Background()

	// Seed test data
	obsIDs := s.seedObservations(ctx)
	sumIDs := s.seedSummaries(ctx)
	s.Require().Len(obsIDs, 4)
	s.Require().Len(sumIDs, 2)

	// Test filter search for observations only
	result, err := s.manager.filterSearch(ctx, SearchParams{
		Project: "test-project",
		Type:    "observations",
		Limit:   10,
		Format:  "full",
	})
	s.Require().NoError(err)
	s.NotNil(result)

	// Should return project observations + global observation (4 total: 3 project + 1 global)
	s.GreaterOrEqual(len(result.Results), 3)

	// Verify result types
	for _, r := range result.Results {
		s.Equal("observation", r.Type)
	}
}

// TestFilterSearch_SessionsOnly tests filterSearch for sessions.
func (s *SearchIntegrationSuite) TestFilterSearch_SessionsOnly() {
	ctx := context.Background()

	// Seed test data
	_ = s.seedObservations(ctx)
	sumIDs := s.seedSummaries(ctx)
	s.Require().Len(sumIDs, 2)

	// Test filter search for sessions only
	result, err := s.manager.filterSearch(ctx, SearchParams{
		Project: "test-project",
		Type:    "sessions",
		Limit:   10,
		Format:  "full",
	})
	s.Require().NoError(err)
	s.NotNil(result)

	// Should return 2 summaries
	s.Len(result.Results, 2)

	// Verify result types
	for _, r := range result.Results {
		s.Equal("session", r.Type)
		s.NotEmpty(r.Title) // Title should be populated from Request
	}
}

// TestFilterSearch_AllTypes tests filterSearch for all types.
func (s *SearchIntegrationSuite) TestFilterSearch_AllTypes() {
	ctx := context.Background()

	// Seed test data
	obsIDs := s.seedObservations(ctx)
	sumIDs := s.seedSummaries(ctx)
	s.Require().Len(obsIDs, 4)
	s.Require().Len(sumIDs, 2)

	// Test filter search for all types (Type = "")
	result, err := s.manager.filterSearch(ctx, SearchParams{
		Project: "test-project",
		Type:    "", // All types
		Limit:   20,
		Format:  "full",
	})
	s.Require().NoError(err)
	s.NotNil(result)

	// Should return both observations and sessions
	hasObservations := false
	hasSessions := false
	for _, r := range result.Results {
		if r.Type == "observation" {
			hasObservations = true
		}
		if r.Type == "session" {
			hasSessions = true
		}
	}
	s.True(hasObservations, "Should have observation results")
	s.True(hasSessions, "Should have session results")
}

// TestUnifiedSearch_DefaultLimit tests UnifiedSearch with default limit.
func (s *SearchIntegrationSuite) TestUnifiedSearch_DefaultLimit() {
	ctx := context.Background()

	// Seed test data
	s.seedObservations(ctx)
	s.seedSummaries(ctx)

	// Test with no limit specified (should default to 20)
	result, err := s.manager.UnifiedSearch(ctx, SearchParams{
		Project: "test-project",
	})
	s.Require().NoError(err)
	s.NotNil(result)
	s.LessOrEqual(len(result.Results), 20)
}

// TestUnifiedSearch_LimitCapping tests UnifiedSearch limit capping.
func (s *SearchIntegrationSuite) TestUnifiedSearch_LimitCapping() {
	ctx := context.Background()

	// Seed test data
	s.seedObservations(ctx)
	s.seedSummaries(ctx)

	// Test with limit > 100 (should be capped to 100)
	result, err := s.manager.UnifiedSearch(ctx, SearchParams{
		Project: "test-project",
		Limit:   500,
	})
	s.Require().NoError(err)
	s.NotNil(result)
	s.LessOrEqual(len(result.Results), 100)
}

// TestDecisions_WithRealStores tests the Decisions method falls back to filterSearch.
func (s *SearchIntegrationSuite) TestDecisions_WithRealStores() {
	ctx := context.Background()

	// Seed test data
	s.seedObservations(ctx)

	// Test Decisions search (without vector client, falls back to filterSearch)
	result, err := s.manager.Decisions(ctx, SearchParams{
		Project: "test-project",
		Query:   "database",
		Limit:   10,
	})
	s.Require().NoError(err)
	s.NotNil(result)

	// Without vector client, falls back to filterSearch which returns observations
	// All results should be observations (type is forced to "observations" in Decisions)
	for _, r := range result.Results {
		s.Equal("observation", r.Type)
	}
}

// TestChanges_WithRealStores tests the Changes method falls back to filterSearch.
func (s *SearchIntegrationSuite) TestChanges_WithRealStores() {
	ctx := context.Background()

	// Seed test data
	s.seedObservations(ctx)

	// Test Changes search (without vector client, falls back to filterSearch)
	result, err := s.manager.Changes(ctx, SearchParams{
		Project: "test-project",
		Query:   "user service",
		Limit:   10,
	})
	s.Require().NoError(err)
	s.NotNil(result)

	// Without vector client, falls back to filterSearch which returns observations
	// All results should be observations (type is forced to "observations" in Changes)
	for _, r := range result.Results {
		s.Equal("observation", r.Type)
	}
}

// TestHowItWorks_WithRealStores tests the HowItWorks method falls back to filterSearch.
func (s *SearchIntegrationSuite) TestHowItWorks_WithRealStores() {
	ctx := context.Background()

	// Seed test data
	s.seedObservations(ctx)

	// Test HowItWorks search (without vector client, falls back to filterSearch)
	result, err := s.manager.HowItWorks(ctx, SearchParams{
		Project: "test-project",
		Query:   "authentication",
		Limit:   10,
	})
	s.Require().NoError(err)
	s.NotNil(result)

	// Without vector client, falls back to filterSearch which returns observations
	// All results should be observations (type is forced to "observations" in HowItWorks)
	for _, r := range result.Results {
		s.Equal("observation", r.Type)
	}
}

// TestObservationToResult tests observation to result conversion with full format.
func (s *SearchIntegrationSuite) TestObservationToResult_FullFormat() {
	ctx := context.Background()

	// Insert single observation
	obs := &models.ParsedObservation{
		Type:      models.ObsTypeDiscovery,
		Scope:     models.ScopeProject,
		Title:     "Test Title",
		Narrative: "Detailed narrative content for testing",
		Concepts:  []string{"testing", "content"},
	}
	id, _, err := s.obsStore.StoreObservation(ctx, "sdk-test", "test-project", obs, 1, 50)
	s.Require().NoError(err)

	// Retrieve and convert
	retrieved, err := s.obsStore.GetObservationByID(ctx, id)
	s.Require().NoError(err)
	s.Require().NotNil(retrieved)

	result := s.manager.observationToResult(retrieved, "full")

	s.Equal("observation", result.Type)
	s.Equal(id, result.ID)
	s.Equal("Test Title", result.Title)
	s.Equal("Detailed narrative content for testing", result.Content)
	s.Equal("test-project", result.Project)
	s.Equal("project", result.Scope)
	s.NotNil(result.Metadata)
	s.Equal("discovery", result.Metadata["obs_type"])
}

// TestObservationToResult_IndexFormat tests index format (no content).
func (s *SearchIntegrationSuite) TestObservationToResult_IndexFormat() {
	ctx := context.Background()

	obs := &models.ParsedObservation{
		Type:      models.ObsTypeBugfix,
		Scope:     models.ScopeGlobal,
		Title:     "Bug Fix Title",
		Narrative: "This should not appear in index format",
	}
	id, _, err := s.obsStore.StoreObservation(ctx, "sdk-test", "test-project", obs, 1, 50)
	s.Require().NoError(err)

	retrieved, err := s.obsStore.GetObservationByID(ctx, id)
	s.Require().NoError(err)

	result := s.manager.observationToResult(retrieved, "index")

	s.Equal("observation", result.Type)
	s.Equal("Bug Fix Title", result.Title)
	s.Empty(result.Content, "Index format should not include content")
	s.Equal("global", result.Scope)
}

// TestSummaryToResult_FullFormat tests summary to result conversion.
func (s *SearchIntegrationSuite) TestSummaryToResult_FullFormat() {
	ctx := context.Background()

	sum := &models.ParsedSummary{
		Request: "Implement new feature",
		Learned: "Learned important lessons about testing",
	}
	id, _, err := s.sumStore.StoreSummary(ctx, "sdk-test", "test-project", sum, 1, 50)
	s.Require().NoError(err)

	// Retrieve via GetRecentSummaries since there's no GetByID
	summaries, err := s.sumStore.GetRecentSummaries(ctx, "test-project", 10)
	s.Require().NoError(err)
	s.Require().NotEmpty(summaries)

	var retrieved *models.SessionSummary
	for _, s := range summaries {
		if s.ID == id {
			retrieved = s
			break
		}
	}
	s.Require().NotNil(retrieved)

	result := s.manager.summaryToResult(retrieved, "full")

	s.Equal("session", result.Type)
	s.Equal(id, result.ID)
	s.Contains(result.Title, "Implement new feature")
	s.Equal("Learned important lessons about testing", result.Content)
	s.Equal("test-project", result.Project)
}

// TestPromptToResult_FullFormat tests prompt to result conversion.
func (s *SearchIntegrationSuite) TestPromptToResult_FullFormat() {
	// First create a session
	ctx := context.Background()
	sessionStore := sqlite.NewSessionStore(s.store)
	_, err := sessionStore.CreateSDKSession(ctx, "sdk-prompt-test", "test-project", "initial prompt")
	s.Require().NoError(err)

	// Save a user prompt
	promptID, err := s.prmStore.SaveUserPromptWithMatches(ctx, "sdk-prompt-test", 1, "Help me fix this authentication bug", 3)
	s.Require().NoError(err)

	// Retrieve prompts
	prompts, err := s.prmStore.GetPromptsByIDs(ctx, []int64{promptID}, "date_desc", 10)
	s.Require().NoError(err)
	s.Require().NotEmpty(prompts)

	result := s.manager.promptToResult(prompts[0], "full")

	s.Equal("prompt", result.Type)
	s.Equal(promptID, result.ID)
	s.Contains(result.Title, "Help me fix")
	s.Equal("Help me fix this authentication bug", result.Content)
}

// TestTruncate_TableDriven tests truncation with various inputs.
func TestTruncate_TableDriven(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{"short_string", "hello", 10, "hello"},
		{"exact_length", "hello", 5, "hello"},
		{"long_string", "hello world", 5, "hello..."},
		{"empty_string", "", 10, ""},
		{"whitespace_only", "   ", 10, ""},
		{"with_leading_space", "  hello  ", 10, "hello"},
		{"very_long", "this is a very long string that should be truncated", 20, "this is a very long ..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncate(tt.input, tt.maxLen)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestManagerWithNilStores tests that Manager handles nil stores gracefully.
func TestManagerWithNilStores(t *testing.T) {
	m := NewManager(nil, nil, nil, nil)
	assert.NotNil(t, m)
	assert.Nil(t, m.observationStore)
	assert.Nil(t, m.summaryStore)
	assert.Nil(t, m.promptStore)
	assert.Nil(t, m.vectorClient)
}

// TestSearchResultMetadataFields tests all metadata fields with real data.
func (s *SearchIntegrationSuite) TestSearchResultMetadataFields() {
	ctx := context.Background()

	obs := &models.ParsedObservation{
		Type:      models.ObsTypeDecision,
		Scope:     models.ScopeGlobal,
		Title:     "Architecture Decision",
		Concepts:  []string{"auth", "security"},
		FilesRead: []string{"handler.go", "auth.go"},
	}
	id, _, err := s.obsStore.StoreObservation(ctx, "sdk-meta-test", "test-project", obs, 1, 50)
	s.Require().NoError(err)

	retrieved, err := s.obsStore.GetObservationByID(ctx, id)
	s.Require().NoError(err)

	result := s.manager.observationToResult(retrieved, "full")

	// Check metadata fields
	s.NotNil(result.Metadata)
	s.Equal("decision", result.Metadata["obs_type"])
	s.Equal("global", result.Metadata["scope"])
	s.Equal("global", result.Scope)
}

// TestObservationToResult_AllFormats tests different format options.
func TestObservationToResult_AllFormats(t *testing.T) {
	m := NewManager(nil, nil, nil, nil)

	obs := &models.Observation{
		ID:             1,
		Project:        "test",
		Type:           models.ObsTypeBugfix,
		Scope:          models.ScopeProject,
		Title:          sql.NullString{String: "Bug Fix Title", Valid: true},
		Narrative:      sql.NullString{String: "Detailed bug fix narrative", Valid: true},
		CreatedAtEpoch: 1704067200000,
	}

	tests := []struct {
		name          string
		format        string
		expectContent bool
	}{
		{"full_format", "full", true},
		{"index_format", "index", false},
		{"empty_format", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.observationToResult(obs, tt.format)
			assert.Equal(t, "observation", result.Type)
			assert.Equal(t, int64(1), result.ID)
			if tt.expectContent {
				assert.NotEmpty(t, result.Content)
			}
		})
	}
}

// TestSummaryToResult_AllFormats tests different format options for summaries.
func TestSummaryToResult_AllFormats(t *testing.T) {
	m := NewManager(nil, nil, nil, nil)

	summary := &models.SessionSummary{
		ID:             1,
		Project:        "test",
		Request:        sql.NullString{String: "Test request", Valid: true},
		Learned:        sql.NullString{String: "Test learned", Valid: true},
		Completed:      sql.NullString{String: "Test completed", Valid: true},
		NextSteps:      sql.NullString{String: "Test next steps", Valid: true},
		CreatedAtEpoch: 1704067200000,
	}

	tests := []struct {
		name          string
		format        string
		expectContent bool
	}{
		{"full_format", "full", true},
		{"index_format", "index", false},
		{"empty_format", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.summaryToResult(summary, tt.format)
			assert.Equal(t, "session", result.Type)
			assert.Equal(t, int64(1), result.ID)
			if tt.expectContent {
				assert.NotEmpty(t, result.Content)
			}
		})
	}
}
