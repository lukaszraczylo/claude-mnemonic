package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testSummaryStore(t *testing.T) (*SummaryStore, *Store, func()) {
	t.Helper()

	db, _, cleanup := testDB(t)
	createAllTables(t, db)

	store := newStoreFromDB(db)
	summaryStore := NewSummaryStore(store)

	return summaryStore, store, cleanup
}

func TestSummaryStore_StoreSummary(t *testing.T) {
	summaryStore, store, cleanup := testSummaryStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create a session first
	seedSession(t, storeDB(store), "claude-1", "sdk-1", "test-project")

	summary := &models.ParsedSummary{
		Request:      "Add new feature",
		Investigated: "Looked at existing code",
		Learned:      "Found the pattern to follow",
		Completed:    "Implemented the feature",
		NextSteps:    "Add tests",
		Notes:        "Some additional notes",
	}

	id, epoch, err := summaryStore.StoreSummary(ctx, "sdk-1", "test-project", summary, 1, 100)
	require.NoError(t, err)
	assert.Greater(t, id, int64(0))
	assert.Greater(t, epoch, int64(0))

	// Verify it was saved
	var count int
	err = storeDB(store).QueryRow("SELECT COUNT(*) FROM session_summaries WHERE id = ?", id).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestSummaryStore_StoreSummary_AutoCreateSession(t *testing.T) {
	summaryStore, store, cleanup := testSummaryStore(t)
	defer cleanup()

	ctx := context.Background()

	// Don't create session beforehand - should be auto-created
	summary := &models.ParsedSummary{
		Request: "Test request",
	}

	id, _, err := summaryStore.StoreSummary(ctx, "auto-session", "test-project", summary, 1, 0)
	require.NoError(t, err)
	assert.Greater(t, id, int64(0))

	// Verify session was auto-created
	var sessionCount int
	err = storeDB(store).QueryRow("SELECT COUNT(*) FROM sdk_sessions WHERE sdk_session_id = ?", "auto-session").Scan(&sessionCount)
	require.NoError(t, err)
	assert.Equal(t, 1, sessionCount)
}

func TestSummaryStore_GetRecentSummaries(t *testing.T) {
	summaryStore, store, cleanup := testSummaryStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create a session
	seedSession(t, storeDB(store), "claude-1", "sdk-1", "test-project")

	// Store multiple summaries
	for i := 0; i < 5; i++ {
		summary := &models.ParsedSummary{
			Request: "Request " + string(rune('A'+i)),
		}
		_, _, err := summaryStore.StoreSummary(ctx, "sdk-1", "test-project", summary, i+1, 0)
		require.NoError(t, err)
		time.Sleep(time.Millisecond) // Ensure different timestamps
	}

	// Get recent summaries with limit
	summaries, err := summaryStore.GetRecentSummaries(ctx, "test-project", 3)
	require.NoError(t, err)
	assert.Len(t, summaries, 3)

	// Should be in descending order
	assert.Equal(t, int64(5), summaries[0].PromptNumber.Int64)
}

func TestSummaryStore_GetAllRecentSummaries(t *testing.T) {
	summaryStore, store, cleanup := testSummaryStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create sessions for different projects
	seedSession(t, storeDB(store), "claude-1", "sdk-1", "project-a")
	seedSession(t, storeDB(store), "claude-2", "sdk-2", "project-b")

	// Store summaries for both projects
	for i := 0; i < 3; i++ {
		summary := &models.ParsedSummary{Request: "Project A request"}
		_, _, err := summaryStore.StoreSummary(ctx, "sdk-1", "project-a", summary, i+1, 0)
		require.NoError(t, err)
	}
	for i := 0; i < 2; i++ {
		summary := &models.ParsedSummary{Request: "Project B request"}
		_, _, err := summaryStore.StoreSummary(ctx, "sdk-2", "project-b", summary, i+1, 0)
		require.NoError(t, err)
	}

	// Get all summaries (should include both projects)
	summaries, err := summaryStore.GetAllRecentSummaries(ctx, 10)
	require.NoError(t, err)
	assert.Len(t, summaries, 5)
}

func TestSummaryStore_GetSummariesByIDs(t *testing.T) {
	summaryStore, store, cleanup := testSummaryStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create a session
	seedSession(t, storeDB(store), "claude-1", "sdk-1", "test-project")

	// Store summaries and collect IDs
	var ids []int64
	for i := 0; i < 5; i++ {
		summary := &models.ParsedSummary{Request: "Request " + string(rune('A'+i))}
		id, _, err := summaryStore.StoreSummary(ctx, "sdk-1", "test-project", summary, i+1, 0)
		require.NoError(t, err)
		ids = append(ids, id)
		time.Sleep(time.Millisecond)
	}

	// Get specific summaries by ID
	summaries, err := summaryStore.GetSummariesByIDs(ctx, ids[:3], "date_desc", 10)
	require.NoError(t, err)
	assert.Len(t, summaries, 3)

	// Test with ascending order
	summaries, err = summaryStore.GetSummariesByIDs(ctx, ids, "date_asc", 2)
	require.NoError(t, err)
	assert.Len(t, summaries, 2)
	assert.Equal(t, int64(1), summaries[0].PromptNumber.Int64)
}

func TestSummaryStore_GetSummariesByIDs_EmptyInput(t *testing.T) {
	summaryStore, _, cleanup := testSummaryStore(t)
	defer cleanup()

	ctx := context.Background()

	// Empty IDs should return nil
	summaries, err := summaryStore.GetSummariesByIDs(ctx, []int64{}, "date_desc", 10)
	require.NoError(t, err)
	assert.Nil(t, summaries)
}

func TestSummaryStore_SummaryFields(t *testing.T) {
	summaryStore, store, cleanup := testSummaryStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create a session
	seedSession(t, storeDB(store), "claude-1", "sdk-1", "test-project")

	// Store a summary with all fields
	summary := &models.ParsedSummary{
		Request:      "Add authentication",
		Investigated: "Reviewed existing auth code",
		Learned:      "OAuth is preferred",
		Completed:    "Implemented OAuth flow",
		NextSteps:    "Add refresh token support",
		Notes:        "Consider rate limiting",
	}

	id, _, err := summaryStore.StoreSummary(ctx, "sdk-1", "test-project", summary, 5, 1500)
	require.NoError(t, err)

	// Retrieve and verify all fields
	summaries, err := summaryStore.GetSummariesByIDs(ctx, []int64{id}, "date_desc", 1)
	require.NoError(t, err)
	require.Len(t, summaries, 1)

	s := summaries[0]
	assert.Equal(t, id, s.ID)
	assert.Equal(t, "sdk-1", s.SDKSessionID)
	assert.Equal(t, "test-project", s.Project)
	assert.Equal(t, "Add authentication", s.Request.String)
	assert.Equal(t, "Reviewed existing auth code", s.Investigated.String)
	assert.Equal(t, "OAuth is preferred", s.Learned.String)
	assert.Equal(t, "Implemented OAuth flow", s.Completed.String)
	assert.Equal(t, "Add refresh token support", s.NextSteps.String)
	assert.Equal(t, "Consider rate limiting", s.Notes.String)
	assert.Equal(t, int64(5), s.PromptNumber.Int64)
	assert.Equal(t, int64(1500), s.DiscoveryTokens)
}

func TestSummaryStore_EmptySummary(t *testing.T) {
	summaryStore, store, cleanup := testSummaryStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create a session
	seedSession(t, storeDB(store), "claude-1", "sdk-1", "test-project")

	// Store an empty summary
	summary := &models.ParsedSummary{}

	id, _, err := summaryStore.StoreSummary(ctx, "sdk-1", "test-project", summary, 0, 0)
	require.NoError(t, err)
	assert.Greater(t, id, int64(0))

	// Retrieve and verify null fields
	summaries, err := summaryStore.GetSummariesByIDs(ctx, []int64{id}, "date_desc", 1)
	require.NoError(t, err)
	require.Len(t, summaries, 1)

	s := summaries[0]
	assert.False(t, s.Request.Valid || s.Request.String != "")
	assert.False(t, s.Investigated.Valid || s.Investigated.String != "")
	assert.False(t, s.Learned.Valid || s.Learned.String != "")
}
