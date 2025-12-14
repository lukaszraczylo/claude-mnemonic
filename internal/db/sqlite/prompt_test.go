package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testPromptStore(t *testing.T) (*PromptStore, *Store, func()) {
	t.Helper()

	db, _, cleanup := testDB(t)
	createAllTables(t, db)

	store := newStoreFromDB(db)
	promptStore := NewPromptStore(store)

	return promptStore, store, cleanup
}

func TestPromptStore_SaveUserPromptWithMatches(t *testing.T) {
	promptStore, store, cleanup := testPromptStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create a session first
	seedSession(t, storeDB(store), "claude-1", "sdk-1", "test-project")

	// Save a prompt
	id, err := promptStore.SaveUserPromptWithMatches(ctx, "claude-1", 1, "Help me fix this bug", 5)
	require.NoError(t, err)
	assert.Greater(t, id, int64(0))

	// Verify it was saved
	var count int
	err = storeDB(store).QueryRow("SELECT COUNT(*) FROM user_prompts WHERE id = ?", id).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestPromptStore_GetAllRecentUserPrompts(t *testing.T) {
	promptStore, store, cleanup := testPromptStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create a session
	seedSession(t, storeDB(store), "claude-1", "sdk-1", "test-project")

	// Save multiple prompts
	for i := 1; i <= 5; i++ {
		_, err := promptStore.SaveUserPromptWithMatches(ctx, "claude-1", i, "Prompt "+string(rune('A'+i-1)), i)
		require.NoError(t, err)
		time.Sleep(time.Millisecond) // Ensure different timestamps
	}

	// Get recent prompts
	prompts, err := promptStore.GetAllRecentUserPrompts(ctx, 3)
	require.NoError(t, err)
	assert.Len(t, prompts, 3)

	// Should be in descending order (most recent first)
	assert.Equal(t, 5, prompts[0].PromptNumber)
}

func TestPromptStore_GetRecentUserPromptsByProject(t *testing.T) {
	promptStore, store, cleanup := testPromptStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create sessions for different projects
	seedSession(t, storeDB(store), "claude-1", "sdk-1", "project-a")
	seedSession(t, storeDB(store), "claude-2", "sdk-2", "project-b")

	// Save prompts for both projects
	for i := 1; i <= 3; i++ {
		_, err := promptStore.SaveUserPromptWithMatches(ctx, "claude-1", i, "Project A prompt", 0)
		require.NoError(t, err)
	}
	for i := 1; i <= 2; i++ {
		_, err := promptStore.SaveUserPromptWithMatches(ctx, "claude-2", i, "Project B prompt", 0)
		require.NoError(t, err)
	}

	// Get prompts for project-a
	prompts, err := promptStore.GetRecentUserPromptsByProject(ctx, "project-a", 10)
	require.NoError(t, err)
	assert.Len(t, prompts, 3)

	// Get prompts for project-b
	prompts, err = promptStore.GetRecentUserPromptsByProject(ctx, "project-b", 10)
	require.NoError(t, err)
	assert.Len(t, prompts, 2)
}

func TestPromptStore_CleanupOldPrompts(t *testing.T) {
	promptStore, store, cleanup := testPromptStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create a session
	seedSession(t, storeDB(store), "claude-1", "sdk-1", "test-project")

	// Save more prompts than the limit
	// Note: MaxPromptsGlobal is 500, but we'll test with a smaller number
	// by directly calling CleanupOldPrompts
	for i := 1; i <= 10; i++ {
		_, err := storeDB(store).Exec(`
			INSERT INTO user_prompts (claude_session_id, prompt_number, prompt_text, created_at, created_at_epoch)
			VALUES (?, ?, ?, datetime('now'), ?)
		`, "claude-1", i, "Prompt "+string(rune('A'+i-1)), time.Now().UnixMilli()+int64(i))
		require.NoError(t, err)
	}

	// Verify we have 10 prompts
	var count int
	err := storeDB(store).QueryRow("SELECT COUNT(*) FROM user_prompts").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 10, count)

	// Cleanup should return empty since we're under the limit
	deletedIDs, err := promptStore.CleanupOldPrompts(ctx)
	require.NoError(t, err)
	assert.Empty(t, deletedIDs)
}

func TestPromptStore_SetCleanupFunc(t *testing.T) {
	promptStore, store, cleanup := testPromptStore(t)
	defer cleanup()

	ctx := context.Background()

	// Track cleanup calls
	var cleanupCalledWith []int64
	promptStore.SetCleanupFunc(func(ctx context.Context, deletedIDs []int64) {
		cleanupCalledWith = deletedIDs
	})

	// Create a session
	seedSession(t, storeDB(store), "claude-1", "sdk-1", "test-project")

	// Save a prompt (should trigger cleanup, but won't delete anything under limit)
	_, err := promptStore.SaveUserPromptWithMatches(ctx, "claude-1", 1, "Test prompt", 0)
	require.NoError(t, err)

	// Cleanup func should not have been called since nothing was deleted
	assert.Empty(t, cleanupCalledWith)
}

func TestPromptStore_GetPromptsByIDs(t *testing.T) {
	promptStore, store, cleanup := testPromptStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create a session
	seedSession(t, storeDB(store), "claude-1", "sdk-1", "test-project")

	// Save some prompts and collect their IDs
	var ids []int64
	for i := 1; i <= 5; i++ {
		id, err := promptStore.SaveUserPromptWithMatches(ctx, "claude-1", i, "Prompt "+string(rune('A'+i-1)), 0)
		require.NoError(t, err)
		ids = append(ids, id)
		time.Sleep(time.Millisecond)
	}

	// Get specific prompts by ID
	prompts, err := promptStore.GetPromptsByIDs(ctx, ids[:3], "date_desc", 10)
	require.NoError(t, err)
	assert.Len(t, prompts, 3)

	// Test with ascending order
	prompts, err = promptStore.GetPromptsByIDs(ctx, ids, "date_asc", 2)
	require.NoError(t, err)
	assert.Len(t, prompts, 2)
	assert.Equal(t, 1, prompts[0].PromptNumber)
}

func TestPromptStore_GetPromptsByIDs_EmptyInput(t *testing.T) {
	promptStore, _, cleanup := testPromptStore(t)
	defer cleanup()

	ctx := context.Background()

	// Empty IDs should return nil
	prompts, err := promptStore.GetPromptsByIDs(ctx, []int64{}, "date_desc", 10)
	require.NoError(t, err)
	assert.Nil(t, prompts)
}
