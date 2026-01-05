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
)

// testPromptStore creates a PromptStore with a temporary database for testing.
func testPromptStore(t *testing.T) (*PromptStore, *Store, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "gorm_prompt_test_*")
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

	promptStore := NewPromptStore(store, nil)

	cleanup := func() {
		store.Close()
		os.RemoveAll(tmpDir)
	}

	return promptStore, store, cleanup
}

func TestPromptStore_SaveUserPromptWithMatches(t *testing.T) {
	promptStore, store, cleanup := testPromptStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create a session first
	sessionStore := NewSessionStore(store)
	_, err := sessionStore.CreateSDKSession(ctx, "claude-1", "test-project", "")
	require.NoError(t, err)

	// Save a prompt
	id, err := promptStore.SaveUserPromptWithMatches(ctx, "claude-1", 1, "What is the codebase structure?", 5)
	require.NoError(t, err)
	assert.Greater(t, id, int64(0))
}

func TestPromptStore_SaveUserPromptWithMatches_Idempotency(t *testing.T) {
	promptStore, _, cleanup := testPromptStore(t)
	defer cleanup()

	ctx := context.Background()

	// Save the same prompt twice (same claudeSessionID + promptNumber)
	id1, err := promptStore.SaveUserPromptWithMatches(ctx, "claude-1", 1, "Test prompt", 3)
	require.NoError(t, err)

	id2, err := promptStore.SaveUserPromptWithMatches(ctx, "claude-1", 1, "Different text", 5)
	require.NoError(t, err)

	// Should return the same ID (INSERT OR IGNORE)
	assert.Equal(t, id1, id2, "Duplicate prompts should return same ID")
}

func TestPromptStore_SaveUserPromptWithMatches_AsyncCleanup(t *testing.T) {
	promptStore, _, cleanup := testPromptStore(t)
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

	promptStore.cleanupFunc = cleanupFunc

	// Save prompts beyond the global limit (MaxPromptsGlobal = 500)
	// Insert with slower pacing to avoid database lock contention
	for i := 0; i < 505; i++ {
		_, err := promptStore.SaveUserPromptWithMatches(ctx, "claude-1", i+1, "Prompt", 1)
		require.NoError(t, err)
		if i > 500 {
			time.Sleep(5 * time.Millisecond) // Slow down after hitting limit
		}
	}

	// Wait longer for async cleanup to complete
	time.Sleep(500 * time.Millisecond)

	// Verify cleanup was called
	cleanupMutex.Lock()
	defer cleanupMutex.Unlock()
	assert.True(t, cleanupCalled, "Cleanup function should have been called")
	assert.NotEmpty(t, cleanupIDs, "Cleanup should have deleted some prompts")
}

func TestPromptStore_CleanupOldPrompts(t *testing.T) {
	promptStore, _, cleanup := testPromptStore(t)
	defer cleanup()

	ctx := context.Background()

	// Save prompts beyond the limit
	// Async cleanup should fire after each insert beyond 500
	for i := 0; i < 505; i++ {
		_, err := promptStore.SaveUserPromptWithMatches(ctx, "claude-1", i+1, "Prompt", 1)
		require.NoError(t, err)
	}

	// Wait for all async cleanups to complete
	time.Sleep(1 * time.Second)

	// After async cleanup, we should have at most 500 prompts
	remaining, err := promptStore.GetAllPrompts(ctx)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(remaining), MaxPromptsGlobal, "Should have at most %d prompts after async cleanup", MaxPromptsGlobal)
}

func TestPromptStore_GetPromptsByIDs(t *testing.T) {
	promptStore, _, cleanup := testPromptStore(t)
	defer cleanup()

	ctx := context.Background()

	// Save multiple prompts
	var ids []int64
	for i := 1; i <= 3; i++ {
		id, err := promptStore.SaveUserPromptWithMatches(ctx, "claude-1", i, "Prompt", i)
		require.NoError(t, err)
		ids = append(ids, id)
		time.Sleep(10 * time.Millisecond) // Ensure different timestamps
	}

	tests := []struct {
		name     string
		orderBy  string
		expected []int64
	}{
		{
			name:     "Default ordering - date desc",
			orderBy:  "default",
			expected: []int64{ids[2], ids[1], ids[0]}, // Newest to oldest
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
			prompts, err := promptStore.GetPromptsByIDs(ctx, ids, tt.orderBy, 10)
			require.NoError(t, err)
			require.Len(t, prompts, 3)

			// Verify ordering
			for i, prompt := range prompts {
				assert.Equal(t, tt.expected[i], prompt.ID, "Position %d should have ID %d", i, tt.expected[i])
			}
		})
	}
}

func TestPromptStore_GetPromptsByIDs_Limit(t *testing.T) {
	promptStore, _, cleanup := testPromptStore(t)
	defer cleanup()

	ctx := context.Background()

	// Save multiple prompts
	var ids []int64
	for i := 1; i <= 5; i++ {
		id, err := promptStore.SaveUserPromptWithMatches(ctx, "claude-1", i, "Prompt", i)
		require.NoError(t, err)
		ids = append(ids, id)
	}

	// Get with limit
	prompts, err := promptStore.GetPromptsByIDs(ctx, ids, "default", 3)
	require.NoError(t, err)
	assert.Len(t, prompts, 3)
}

func TestPromptStore_GetPromptsByIDs_EmptyInput(t *testing.T) {
	promptStore, _, cleanup := testPromptStore(t)
	defer cleanup()

	ctx := context.Background()

	// Get with empty IDs
	prompts, err := promptStore.GetPromptsByIDs(ctx, []int64{}, "default", 10)
	require.NoError(t, err)
	assert.Nil(t, prompts)
}

func TestPromptStore_GetPromptsByIDs_WithSession(t *testing.T) {
	promptStore, store, cleanup := testPromptStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create a session
	sessionStore := NewSessionStore(store)
	_, err := sessionStore.CreateSDKSession(ctx, "claude-1", "test-project", "")
	require.NoError(t, err)

	// Save a prompt
	id, err := promptStore.SaveUserPromptWithMatches(ctx, "claude-1", 1, "Test prompt", 5)
	require.NoError(t, err)

	// Get with session join
	prompts, err := promptStore.GetPromptsByIDs(ctx, []int64{id}, "default", 10)
	require.NoError(t, err)
	require.Len(t, prompts, 1)

	// Verify session data is populated
	assert.Equal(t, "test-project", prompts[0].Project)
	assert.NotEmpty(t, prompts[0].SDKSessionID)
}

func TestPromptStore_GetAllRecentUserPrompts(t *testing.T) {
	promptStore, _, cleanup := testPromptStore(t)
	defer cleanup()

	ctx := context.Background()

	// Save prompts across multiple sessions with timestamps
	for i := 1; i <= 3; i++ {
		_, err := promptStore.SaveUserPromptWithMatches(ctx, "claude-1", i, "Prompt A", i)
		require.NoError(t, err)
		time.Sleep(10 * time.Millisecond) // Ensure different timestamps
	}

	for i := 1; i <= 2; i++ {
		_, err := promptStore.SaveUserPromptWithMatches(ctx, "claude-2", i, "Prompt B", i)
		require.NoError(t, err)
		time.Sleep(10 * time.Millisecond) // Ensure different timestamps
	}

	// Get all recent prompts
	prompts, err := promptStore.GetAllRecentUserPrompts(ctx, 10)
	require.NoError(t, err)
	assert.Len(t, prompts, 5)

	// Verify ordering (most recent first) - last inserted should be first
	assert.Equal(t, "claude-2", prompts[0].ClaudeSessionID)
	assert.Equal(t, 2, prompts[0].PromptNumber)
}

func TestPromptStore_GetAllPrompts(t *testing.T) {
	promptStore, _, cleanup := testPromptStore(t)
	defer cleanup()

	ctx := context.Background()

	// Save prompts
	for i := 1; i <= 5; i++ {
		_, err := promptStore.SaveUserPromptWithMatches(ctx, "claude-1", i, "Prompt", i)
		require.NoError(t, err)
	}

	// Wait for any async cleanup to complete (longer wait for race detector)
	time.Sleep(500 * time.Millisecond)

	// Get all prompts (for vector rebuild)
	prompts, err := promptStore.GetAllPrompts(ctx)
	require.NoError(t, err)
	assert.Len(t, prompts, 5)

	// Verify ordering by ID
	for i := 0; i < len(prompts)-1; i++ {
		assert.Less(t, prompts[i].ID, prompts[i+1].ID)
	}
}

func TestPromptStore_FindRecentPromptByText(t *testing.T) {
	promptStore, _, cleanup := testPromptStore(t)
	defer cleanup()

	ctx := context.Background()

	// Save a prompt
	id, err := promptStore.SaveUserPromptWithMatches(ctx, "claude-1", 1, "What is the architecture?", 3)
	require.NoError(t, err)

	// Find by exact text match within time window
	foundID, foundNumber, found := promptStore.FindRecentPromptByText(ctx, "claude-1", "What is the architecture?", 60)
	assert.True(t, found, "Should find the prompt")
	assert.Equal(t, id, foundID)
	assert.Equal(t, 1, foundNumber)

	// Try to find with different text
	_, _, notFound := promptStore.FindRecentPromptByText(ctx, "claude-1", "Different text", 60)
	assert.False(t, notFound, "Should not find a different prompt")

	// Try to find outside time window
	time.Sleep(100 * time.Millisecond)
	_, _, notFound = promptStore.FindRecentPromptByText(ctx, "claude-1", "What is the architecture?", 0)
	assert.False(t, notFound, "Should not find prompt outside time window")
}

func TestPromptStore_GetRecentUserPromptsByProject(t *testing.T) {
	promptStore, store, cleanup := testPromptStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create sessions for different projects
	sessionStore := NewSessionStore(store)
	_, err := sessionStore.CreateSDKSession(ctx, "claude-1", "project-a", "")
	require.NoError(t, err)
	_, err = sessionStore.CreateSDKSession(ctx, "claude-2", "project-b", "")
	require.NoError(t, err)

	// Save prompts for project-a
	for i := 1; i <= 3; i++ {
		_, err := promptStore.SaveUserPromptWithMatches(ctx, "claude-1", i, "Prompt A", i)
		require.NoError(t, err)
	}

	// Save prompts for project-b
	for i := 1; i <= 2; i++ {
		_, err := promptStore.SaveUserPromptWithMatches(ctx, "claude-2", i, "Prompt B", i)
		require.NoError(t, err)
	}

	// Get prompts for project-a
	prompts, err := promptStore.GetRecentUserPromptsByProject(ctx, "project-a", 10)
	require.NoError(t, err)
	assert.Len(t, prompts, 3)

	// Verify all prompts are from project-a
	for _, prompt := range prompts {
		assert.Equal(t, "project-a", prompt.Project)
	}
}

func TestPromptStore_GetRecentUserPromptsByProject_Limit(t *testing.T) {
	promptStore, store, cleanup := testPromptStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create session
	sessionStore := NewSessionStore(store)
	_, err := sessionStore.CreateSDKSession(ctx, "claude-1", "test-project", "")
	require.NoError(t, err)

	// Save multiple prompts
	for i := 1; i <= 10; i++ {
		_, err := promptStore.SaveUserPromptWithMatches(ctx, "claude-1", i, "Prompt", i)
		require.NoError(t, err)
	}

	// Wait for any async cleanup to complete
	time.Sleep(100 * time.Millisecond)

	// Get with limit
	prompts, err := promptStore.GetRecentUserPromptsByProject(ctx, "test-project", 5)
	require.NoError(t, err)
	assert.Len(t, prompts, 5)
}
