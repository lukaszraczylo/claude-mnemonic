//go:build fts5

// Package gorm provides GORM-based database operations for claude-mnemonic.
package gorm

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm/logger"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
)

// testSummaryStore creates a SummaryStore with a temporary database for testing.
func testSummaryStore(t *testing.T) (*SummaryStore, *Store, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "gorm_summary_test_*")
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

	summaryStore := NewSummaryStore(store)

	cleanup := func() {
		store.Close()
		os.RemoveAll(tmpDir)
	}

	return summaryStore, store, cleanup
}

func TestSummaryStore_StoreSummary(t *testing.T) {
	summaryStore, store, cleanup := testSummaryStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create a session first
	sessionStore := NewSessionStore(store)
	_, err := sessionStore.CreateSDKSession(ctx, "claude-1", "test-project", "")
	require.NoError(t, err)

	// Store a summary
	summary := &models.ParsedSummary{
		Request:      "Build a feature",
		Investigated: "Examined the codebase",
		Learned:      "Discovered patterns",
		Completed:    "Implemented solution",
		NextSteps:    "Write tests",
		Notes:        "Additional notes",
	}

	id, epoch, err := summaryStore.StoreSummary(ctx, "claude-1", "test-project", summary, 1, 100)
	require.NoError(t, err)
	assert.Greater(t, id, int64(0))
	assert.Greater(t, epoch, int64(0))
}

func TestSummaryStore_StoreSummary_AutoCreateSession(t *testing.T) {
	summaryStore, _, cleanup := testSummaryStore(t)
	defer cleanup()

	ctx := context.Background()

	// Store summary without pre-creating session
	summary := &models.ParsedSummary{
		Request: "Test auto-create",
	}

	id, _, err := summaryStore.StoreSummary(ctx, "claude-auto", "auto-project", summary, 1, 50)
	require.NoError(t, err)
	assert.Greater(t, id, int64(0))
}

func TestSummaryStore_GetRecentSummaries(t *testing.T) {
	summaryStore, _, cleanup := testSummaryStore(t)
	defer cleanup()

	ctx := context.Background()

	// Store multiple summaries
	for i := 1; i <= 5; i++ {
		summary := &models.ParsedSummary{
			Request: "Request " + string(rune('0'+i)),
		}
		_, _, err := summaryStore.StoreSummary(ctx, "claude-1", "project-a", summary, i, 10)
		require.NoError(t, err)
	}

	// Store summary for different project
	summary := &models.ParsedSummary{Request: "Other project"}
	_, _, err := summaryStore.StoreSummary(ctx, "claude-2", "project-b", summary, 1, 10)
	require.NoError(t, err)

	// Get recent summaries for project-a
	summaries, err := summaryStore.GetRecentSummaries(ctx, "project-a", 10)
	require.NoError(t, err)
	assert.Len(t, summaries, 5)

	// Verify ordering (most recent first)
	assert.Equal(t, "project-a", summaries[0].Project)
}

func TestSummaryStore_GetAllRecentSummaries(t *testing.T) {
	summaryStore, _, cleanup := testSummaryStore(t)
	defer cleanup()

	ctx := context.Background()

	// Store summaries across projects
	_, _, err := summaryStore.StoreSummary(ctx, "claude-1", "project-a", &models.ParsedSummary{Request: "A1"}, 1, 10)
	require.NoError(t, err)

	_, _, err = summaryStore.StoreSummary(ctx, "claude-2", "project-b", &models.ParsedSummary{Request: "B1"}, 1, 10)
	require.NoError(t, err)

	_, _, err = summaryStore.StoreSummary(ctx, "claude-3", "project-c", &models.ParsedSummary{Request: "C1"}, 1, 10)
	require.NoError(t, err)

	// Get all recent summaries
	summaries, err := summaryStore.GetAllRecentSummaries(ctx, 10)
	require.NoError(t, err)
	assert.Len(t, summaries, 3)
}

func TestSummaryStore_GetSummariesByIDs(t *testing.T) {
	summaryStore, _, cleanup := testSummaryStore(t)
	defer cleanup()

	ctx := context.Background()

	// Store multiple summaries
	var ids []int64
	for i := 1; i <= 3; i++ {
		id, _, err := summaryStore.StoreSummary(ctx, "claude-1", "project-a", &models.ParsedSummary{Request: "Test"}, i, 10)
		require.NoError(t, err)
		ids = append(ids, id)
	}

	// Get by IDs
	summaries, err := summaryStore.GetSummariesByIDs(ctx, ids, "date_desc", 10)
	require.NoError(t, err)
	assert.Len(t, summaries, 3)

	// Get with limit
	summaries, err = summaryStore.GetSummariesByIDs(ctx, ids, "date_desc", 2)
	require.NoError(t, err)
	assert.Len(t, summaries, 2)
}

func TestSummaryStore_GetSummariesByIDs_EmptyInput(t *testing.T) {
	summaryStore, _, cleanup := testSummaryStore(t)
	defer cleanup()

	ctx := context.Background()

	// Get with empty IDs
	summaries, err := summaryStore.GetSummariesByIDs(ctx, []int64{}, "date_desc", 10)
	require.NoError(t, err)
	assert.Nil(t, summaries)
}

func TestSummaryStore_SummaryFields(t *testing.T) {
	summaryStore, _, cleanup := testSummaryStore(t)
	defer cleanup()

	ctx := context.Background()

	// Store a summary with all fields
	summary := &models.ParsedSummary{
		Request:      "Full request",
		Investigated: "Full investigation",
		Learned:      "Full learning",
		Completed:    "Full completion",
		NextSteps:    "Full next steps",
		Notes:        "Full notes",
	}

	id, epoch, err := summaryStore.StoreSummary(ctx, "claude-1", "test-project", summary, 5, 200)
	require.NoError(t, err)

	// Retrieve and verify all fields
	summaries, err := summaryStore.GetSummariesByIDs(ctx, []int64{id}, "date_desc", 1)
	require.NoError(t, err)
	require.Len(t, summaries, 1)

	s := summaries[0]
	assert.Equal(t, id, s.ID)
	assert.Equal(t, "claude-1", s.SDKSessionID)
	assert.Equal(t, "test-project", s.Project)
	assert.True(t, s.Request.Valid)
	assert.Equal(t, "Full request", s.Request.String)
	assert.True(t, s.Investigated.Valid)
	assert.Equal(t, "Full investigation", s.Investigated.String)
	assert.True(t, s.Learned.Valid)
	assert.Equal(t, "Full learning", s.Learned.String)
	assert.True(t, s.Completed.Valid)
	assert.Equal(t, "Full completion", s.Completed.String)
	assert.True(t, s.NextSteps.Valid)
	assert.Equal(t, "Full next steps", s.NextSteps.String)
	assert.True(t, s.Notes.Valid)
	assert.Equal(t, "Full notes", s.Notes.String)
	assert.True(t, s.PromptNumber.Valid)
	assert.Equal(t, int64(5), s.PromptNumber.Int64)
	assert.Equal(t, int64(200), s.DiscoveryTokens)
	assert.NotEmpty(t, s.CreatedAt)
	assert.Equal(t, epoch, s.CreatedAtEpoch)
}

func TestSummaryStore_EmptySummary(t *testing.T) {
	summaryStore, _, cleanup := testSummaryStore(t)
	defer cleanup()

	ctx := context.Background()

	// Store a summary with empty fields
	summary := &models.ParsedSummary{}

	id, _, err := summaryStore.StoreSummary(ctx, "claude-1", "test-project", summary, 0, 0)
	require.NoError(t, err)

	// Retrieve and verify NULL fields
	summaries, err := summaryStore.GetSummariesByIDs(ctx, []int64{id}, "date_desc", 1)
	require.NoError(t, err)
	require.Len(t, summaries, 1)

	s := summaries[0]
	assert.False(t, s.Request.Valid)
	assert.False(t, s.Investigated.Valid)
	assert.False(t, s.Learned.Valid)
	assert.False(t, s.Completed.Valid)
	assert.False(t, s.NextSteps.Valid)
	assert.False(t, s.Notes.Valid)
	assert.False(t, s.PromptNumber.Valid)
	assert.Equal(t, int64(0), s.DiscoveryTokens)
}

func TestSummaryStore_GetAllSummaries(t *testing.T) {
	summaryStore, _, cleanup := testSummaryStore(t)
	defer cleanup()

	ctx := context.Background()

	// Store multiple summaries
	for i := 1; i <= 5; i++ {
		_, _, err := summaryStore.StoreSummary(ctx, "claude-1", "project-a", &models.ParsedSummary{Request: "Test"}, i, 10)
		require.NoError(t, err)
	}

	// Get all summaries
	summaries, err := summaryStore.GetAllSummaries(ctx)
	require.NoError(t, err)
	assert.Len(t, summaries, 5)

	// Verify ordering by ID
	for i := 0; i < len(summaries)-1; i++ {
		assert.Less(t, summaries[i].ID, summaries[i+1].ID)
	}
}
