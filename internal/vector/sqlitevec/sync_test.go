package sqlitevec

import (
	"context"
	"database/sql"
	"testing"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testClient creates a test client for sync tests.
func testClient(t *testing.T) (*Client, func()) {
	t.Helper()

	db, dbCleanup := testDB(t)
	embedSvc, embedCleanup := testEmbeddingService(t)

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	cleanup := func() {
		embedCleanup()
		dbCleanup()
	}

	return client, cleanup
}

func TestNewSync(t *testing.T) {
	client, cleanup := testClient(t)
	defer cleanup()

	sync := NewSync(client)
	assert.NotNil(t, sync)
}

func TestSync_SyncObservation_Empty(t *testing.T) {
	client, cleanup := testClient(t)
	defer cleanup()

	sync := NewSync(client)

	// Observation with no content should be handled gracefully
	obs := &models.Observation{
		ID:           1,
		SDKSessionID: "test-session",
		Project:      "test-project",
		Type:         models.ObsTypeDiscovery,
	}

	err := sync.SyncObservation(context.Background(), obs)
	require.NoError(t, err)
}

func TestSync_SyncObservation_WithContent(t *testing.T) {
	client, cleanup := testClient(t)
	defer cleanup()

	sync := NewSync(client)

	obs := &models.Observation{
		ID:            1,
		SDKSessionID:  "test-session",
		Project:       "test-project",
		Type:          models.ObsTypeDiscovery,
		Scope:         models.ScopeProject,
		Title:         sql.NullString{String: "Authentication bug fix", Valid: true},
		Subtitle:      sql.NullString{String: "Fixed JWT validation", Valid: true},
		Narrative:     sql.NullString{String: "Fixed the JWT token validation to handle expired tokens correctly.", Valid: true},
		Facts:         []string{"JWT tokens expire after 24 hours", "Refresh tokens are used for renewal"},
		Concepts:      []string{"authentication", "security"},
		FilesRead:     []string{"auth.go"},
		FilesModified: []string{"handler.go"},
	}

	err := sync.SyncObservation(context.Background(), obs)
	require.NoError(t, err)

	// Verify documents were added
	results, err := client.Query(context.Background(), "authentication", 10, nil)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 1)
}

func TestSync_SyncObservation_DefaultScope(t *testing.T) {
	client, cleanup := testClient(t)
	defer cleanup()

	sync := NewSync(client)

	// Observation without explicit scope
	obs := &models.Observation{
		ID:           2,
		SDKSessionID: "test-session",
		Project:      "test-project",
		Type:         models.ObsTypeBugfix,
		Narrative:    sql.NullString{String: "Fixed a null pointer exception.", Valid: true},
	}

	err := sync.SyncObservation(context.Background(), obs)
	require.NoError(t, err)
}

func TestSync_SyncSummary_Empty(t *testing.T) {
	client, cleanup := testClient(t)
	defer cleanup()

	sync := NewSync(client)

	// Summary with no content
	summary := &models.SessionSummary{
		ID:           1,
		SDKSessionID: "test-session",
		Project:      "test-project",
	}

	err := sync.SyncSummary(context.Background(), summary)
	require.NoError(t, err)
}

func TestSync_SyncSummary_WithContent(t *testing.T) {
	client, cleanup := testClient(t)
	defer cleanup()

	sync := NewSync(client)

	summary := &models.SessionSummary{
		ID:           1,
		SDKSessionID: "test-session",
		Project:      "test-project",
		Request:      sql.NullString{String: "Help me fix the authentication bug", Valid: true},
		Investigated: sql.NullString{String: "Looked at auth.go and handler.go", Valid: true},
		Learned:      sql.NullString{String: "JWT tokens were not being validated properly", Valid: true},
		Completed:    sql.NullString{String: "Fixed the JWT validation logic", Valid: true},
		NextSteps:    sql.NullString{String: "Add tests for edge cases", Valid: true},
		Notes:        sql.NullString{String: "Consider using a library for JWT handling", Valid: true},
		PromptNumber: sql.NullInt64{Int64: 1, Valid: true},
	}

	err := sync.SyncSummary(context.Background(), summary)
	require.NoError(t, err)

	// Verify documents were added
	results, err := client.Query(context.Background(), "authentication", 10, nil)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 1)
}

func TestSync_SyncUserPrompt(t *testing.T) {
	client, cleanup := testClient(t)
	defer cleanup()

	sync := NewSync(client)

	prompt := &models.UserPromptWithSession{
		UserPrompt: models.UserPrompt{
			ID:             1,
			PromptNumber:   1,
			PromptText:     "Help me fix the authentication bug in the login handler",
			CreatedAtEpoch: 1234567890,
		},
		SDKSessionID: "test-session",
		Project:      "test-project",
	}

	err := sync.SyncUserPrompt(context.Background(), prompt)
	require.NoError(t, err)

	// Verify document was added
	results, err := client.Query(context.Background(), "authentication", 10, nil)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 1)
}

func TestSync_DeleteObservations_Empty(t *testing.T) {
	client, cleanup := testClient(t)
	defer cleanup()

	sync := NewSync(client)

	// Should handle empty list
	err := sync.DeleteObservations(context.Background(), []int64{})
	require.NoError(t, err)
}

func TestSync_DeleteObservations_WithData(t *testing.T) {
	client, cleanup := testClient(t)
	defer cleanup()

	sync := NewSync(client)

	// First add an observation
	obs := &models.Observation{
		ID:           10,
		SDKSessionID: "test-session",
		Project:      "test-project",
		Type:         models.ObsTypeDiscovery,
		Narrative:    sql.NullString{String: "This observation should be deleted.", Valid: true},
		Facts:        []string{"Fact 1", "Fact 2"},
	}

	err := sync.SyncObservation(context.Background(), obs)
	require.NoError(t, err)

	// Then delete it
	err = sync.DeleteObservations(context.Background(), []int64{10})
	require.NoError(t, err)
}

func TestSync_DeleteUserPrompts_Empty(t *testing.T) {
	client, cleanup := testClient(t)
	defer cleanup()

	sync := NewSync(client)

	// Should handle empty list
	err := sync.DeleteUserPrompts(context.Background(), []int64{})
	require.NoError(t, err)
}

func TestSync_DeleteUserPrompts_WithData(t *testing.T) {
	client, cleanup := testClient(t)
	defer cleanup()

	sync := NewSync(client)

	// First add a prompt
	prompt := &models.UserPromptWithSession{
		UserPrompt: models.UserPrompt{
			ID:             20,
			PromptNumber:   1,
			PromptText:     "This prompt should be deleted.",
			CreatedAtEpoch: 1234567890,
		},
		SDKSessionID: "test-session",
		Project:      "test-project",
	}

	err := sync.SyncUserPrompt(context.Background(), prompt)
	require.NoError(t, err)

	// Then delete it
	err = sync.DeleteUserPrompts(context.Background(), []int64{20})
	require.NoError(t, err)
}

func TestSync_FormatObservationDocs_AllFields(t *testing.T) {
	client, cleanup := testClient(t)
	defer cleanup()

	sync := NewSync(client)

	obs := &models.Observation{
		ID:            100,
		SDKSessionID:  "sdk-123",
		Project:       "my-project",
		Type:          models.ObsTypeFeature,
		Scope:         models.ScopeGlobal,
		Title:         sql.NullString{String: "Feature Title", Valid: true},
		Subtitle:      sql.NullString{String: "Feature Subtitle", Valid: true},
		Narrative:     sql.NullString{String: "Feature narrative content", Valid: true},
		Facts:         []string{"Fact A", "Fact B", "Fact C"},
		Concepts:      []string{"api", "performance"},
		FilesRead:     []string{"file1.go", "file2.go"},
		FilesModified: []string{"file3.go"},
	}

	docs := sync.formatObservationDocs(obs)

	// Should have 1 narrative + 3 facts = 4 docs
	assert.Len(t, docs, 4)

	// Check narrative doc
	var narrativeDoc *Document
	for i := range docs {
		if docs[i].ID == "obs_100_narrative" {
			narrativeDoc = &docs[i]
			break
		}
	}
	require.NotNil(t, narrativeDoc)
	assert.Equal(t, "Feature narrative content", narrativeDoc.Content)
	assert.Equal(t, int64(100), narrativeDoc.Metadata["sqlite_id"])
	assert.Equal(t, "observation", narrativeDoc.Metadata["doc_type"])
	assert.Equal(t, "global", narrativeDoc.Metadata["scope"])
	assert.Equal(t, "narrative", narrativeDoc.Metadata["field_type"])
}

func TestSync_FormatSummaryDocs_AllFields(t *testing.T) {
	client, cleanup := testClient(t)
	defer cleanup()

	sync := NewSync(client)

	summary := &models.SessionSummary{
		ID:           200,
		SDKSessionID: "sdk-456",
		Project:      "summary-project",
		Request:      sql.NullString{String: "Request content", Valid: true},
		Investigated: sql.NullString{String: "Investigated content", Valid: true},
		Learned:      sql.NullString{String: "Learned content", Valid: true},
		Completed:    sql.NullString{String: "Completed content", Valid: true},
		NextSteps:    sql.NullString{String: "Next steps content", Valid: true},
		Notes:        sql.NullString{String: "Notes content", Valid: true},
		PromptNumber: sql.NullInt64{Int64: 5, Valid: true},
	}

	docs := sync.formatSummaryDocs(summary)

	// Should have 6 docs (one for each field)
	assert.Len(t, docs, 6)

	// Check request doc
	var requestDoc *Document
	for i := range docs {
		if docs[i].ID == "summary_200_request" {
			requestDoc = &docs[i]
			break
		}
	}
	require.NotNil(t, requestDoc)
	assert.Equal(t, "Request content", requestDoc.Content)
	assert.Equal(t, int64(200), requestDoc.Metadata["sqlite_id"])
	assert.Equal(t, "session_summary", requestDoc.Metadata["doc_type"])
	assert.Equal(t, int64(5), requestDoc.Metadata["prompt_number"])
}

func TestSync_FormatObservationDocs_EmptyScope(t *testing.T) {
	client, cleanup := testClient(t)
	defer cleanup()

	sync := NewSync(client)

	obs := &models.Observation{
		ID:           300,
		SDKSessionID: "sdk-789",
		Project:      "scope-test",
		Type:         models.ObsTypeDecision,
		Narrative:    sql.NullString{String: "Test narrative", Valid: true},
		// Scope intentionally left empty
	}

	docs := sync.formatObservationDocs(obs)
	assert.Len(t, docs, 1)
	assert.Equal(t, "project", docs[0].Metadata["scope"])
}
