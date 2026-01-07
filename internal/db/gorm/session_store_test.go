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

// testSessionStore creates a SessionStore with a temporary database for testing.
func testSessionStore(t *testing.T) (*SessionStore, *Store, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "gorm_session_test_*")
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

	sessionStore := NewSessionStore(store)

	cleanup := func() {
		store.Close()
		os.RemoveAll(tmpDir)
	}

	return sessionStore, store, cleanup
}

func TestSessionStore_CreateSDKSession(t *testing.T) {
	sessionStore, _, cleanup := testSessionStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create a new session
	id, err := sessionStore.CreateSDKSession(ctx, "claude-1", "test-project", "initial prompt")
	require.NoError(t, err)
	assert.Greater(t, id, int64(0))

	// Retrieve and verify
	sess, err := sessionStore.GetSessionByID(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, sess)
	assert.Equal(t, "claude-1", sess.ClaudeSessionID)
	assert.Equal(t, "test-project", sess.Project)
	assert.Equal(t, models.SessionStatusActive, sess.Status)
	assert.True(t, sess.UserPrompt.Valid)
	assert.Equal(t, "initial prompt", sess.UserPrompt.String)
}

func TestSessionStore_CreateSDKSession_Idempotent(t *testing.T) {
	sessionStore, _, cleanup := testSessionStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create first session
	id1, err := sessionStore.CreateSDKSession(ctx, "claude-1", "project-a", "prompt 1")
	require.NoError(t, err)

	// Create again with same claude_session_id but different project
	id2, err := sessionStore.CreateSDKSession(ctx, "claude-1", "project-b", "prompt 2")
	require.NoError(t, err)

	// Should return same ID (idempotent)
	assert.Equal(t, id1, id2)

	// Should have updated project to project-b
	sess, err := sessionStore.GetSessionByID(ctx, id1)
	require.NoError(t, err)
	assert.Equal(t, "project-b", sess.Project)
	assert.Equal(t, "prompt 2", sess.UserPrompt.String)
}

func TestSessionStore_CreateSDKSession_EmptyPrompt(t *testing.T) {
	sessionStore, _, cleanup := testSessionStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create session with empty prompt
	id, err := sessionStore.CreateSDKSession(ctx, "claude-2", "test-project", "")
	require.NoError(t, err)
	assert.Greater(t, id, int64(0))

	// Verify prompt is NULL
	sess, err := sessionStore.GetSessionByID(ctx, id)
	require.NoError(t, err)
	assert.False(t, sess.UserPrompt.Valid)
}

func TestSessionStore_FindAnySDKSession(t *testing.T) {
	sessionStore, _, cleanup := testSessionStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create a session
	_, err := sessionStore.CreateSDKSession(ctx, "claude-1", "test-project", "")
	require.NoError(t, err)

	// Find it
	sess, err := sessionStore.FindAnySDKSession(ctx, "claude-1")
	require.NoError(t, err)
	require.NotNil(t, sess)
	assert.Equal(t, "claude-1", sess.ClaudeSessionID)

	// Try to find non-existent
	sess, err = sessionStore.FindAnySDKSession(ctx, "claude-nonexistent")
	require.NoError(t, err)
	assert.Nil(t, sess)
}

func TestSessionStore_GetSessionByID_NotFound(t *testing.T) {
	sessionStore, _, cleanup := testSessionStore(t)
	defer cleanup()

	ctx := context.Background()

	// Try to get non-existent session
	sess, err := sessionStore.GetSessionByID(ctx, 99999)
	require.NoError(t, err)
	assert.Nil(t, sess)
}

func TestSessionStore_IncrementPromptCounter(t *testing.T) {
	sessionStore, _, cleanup := testSessionStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create a session
	id, err := sessionStore.CreateSDKSession(ctx, "claude-1", "test-project", "")
	require.NoError(t, err)

	// Initial counter should be 0
	counter, err := sessionStore.GetPromptCounter(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, 0, counter)

	// Increment
	counter, err = sessionStore.IncrementPromptCounter(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, 1, counter)

	// Increment again
	counter, err = sessionStore.IncrementPromptCounter(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, 2, counter)

	// Verify via GetPromptCounter
	counter, err = sessionStore.GetPromptCounter(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, 2, counter)
}

func TestSessionStore_GetSessionsToday(t *testing.T) {
	sessionStore, _, cleanup := testSessionStore(t)
	defer cleanup()

	ctx := context.Background()

	// Initially should be 0
	count, err := sessionStore.GetSessionsToday(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	// Create some sessions
	_, err = sessionStore.CreateSDKSession(ctx, "claude-1", "project-1", "")
	require.NoError(t, err)

	_, err = sessionStore.CreateSDKSession(ctx, "claude-2", "project-2", "")
	require.NoError(t, err)

	// Should now have 2 sessions today
	count, err = sessionStore.GetSessionsToday(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestSessionStore_GetAllProjects(t *testing.T) {
	sessionStore, _, cleanup := testSessionStore(t)
	defer cleanup()

	ctx := context.Background()

	// Initially should be empty
	projects, err := sessionStore.GetAllProjects(ctx)
	require.NoError(t, err)
	assert.Empty(t, projects)

	// Create sessions with different projects
	_, err = sessionStore.CreateSDKSession(ctx, "claude-1", "project-a", "")
	require.NoError(t, err)

	_, err = sessionStore.CreateSDKSession(ctx, "claude-2", "project-b", "")
	require.NoError(t, err)

	_, err = sessionStore.CreateSDKSession(ctx, "claude-3", "project-a", "") // Duplicate project
	require.NoError(t, err)

	// Should get distinct projects in alphabetical order
	projects, err = sessionStore.GetAllProjects(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"project-a", "project-b"}, projects)
}

func TestSessionStore_SessionFields(t *testing.T) {
	sessionStore, _, cleanup := testSessionStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create a session
	id, err := sessionStore.CreateSDKSession(ctx, "claude-1", "test-project", "test prompt")
	require.NoError(t, err)

	// Retrieve and verify all fields
	sess, err := sessionStore.GetSessionByID(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, sess)

	// Verify all fields
	assert.Equal(t, id, sess.ID)
	assert.Equal(t, "claude-1", sess.ClaudeSessionID)
	assert.True(t, sess.SDKSessionID.Valid)
	assert.Equal(t, "claude-1", sess.SDKSessionID.String) // Should be same as ClaudeSessionID
	assert.Equal(t, "test-project", sess.Project)
	assert.True(t, sess.UserPrompt.Valid)
	assert.Equal(t, "test prompt", sess.UserPrompt.String)
	assert.Equal(t, int64(0), sess.PromptCounter)
	assert.Equal(t, models.SessionStatusActive, sess.Status)
	assert.NotEmpty(t, sess.StartedAt)
	assert.Greater(t, sess.StartedAtEpoch, int64(0))
	assert.False(t, sess.CompletedAt.Valid)      // Should be NULL
	assert.False(t, sess.CompletedAtEpoch.Valid) // Should be NULL
}
