package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testSessionStore(t *testing.T) (*SessionStore, *Store, func()) {
	t.Helper()

	db, _, cleanup := testDB(t)
	createAllTables(t, db)

	store := newStoreFromDB(db)
	sessionStore := NewSessionStore(store)

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

	// Initially no sessions today
	count, err := sessionStore.GetSessionsToday(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	// Create some sessions
	_, err = sessionStore.CreateSDKSession(ctx, "claude-1", "project-a", "")
	require.NoError(t, err)
	_, err = sessionStore.CreateSDKSession(ctx, "claude-2", "project-b", "")
	require.NoError(t, err)
	_, err = sessionStore.CreateSDKSession(ctx, "claude-3", "project-c", "")
	require.NoError(t, err)

	// Should have 3 sessions today
	count, err = sessionStore.GetSessionsToday(ctx)
	require.NoError(t, err)
	assert.Equal(t, 3, count)
}

func TestSessionStore_GetAllProjects(t *testing.T) {
	sessionStore, _, cleanup := testSessionStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create sessions for different projects
	_, err := sessionStore.CreateSDKSession(ctx, "claude-1", "alpha-project", "")
	require.NoError(t, err)
	_, err = sessionStore.CreateSDKSession(ctx, "claude-2", "beta-project", "")
	require.NoError(t, err)
	_, err = sessionStore.CreateSDKSession(ctx, "claude-3", "alpha-project", "") // duplicate
	require.NoError(t, err)
	_, err = sessionStore.CreateSDKSession(ctx, "claude-4", "gamma-project", "")
	require.NoError(t, err)

	// Get all projects
	projects, err := sessionStore.GetAllProjects(ctx)
	require.NoError(t, err)
	assert.Len(t, projects, 3)
	assert.Contains(t, projects, "alpha-project")
	assert.Contains(t, projects, "beta-project")
	assert.Contains(t, projects, "gamma-project")

	// Should be sorted alphabetically
	assert.Equal(t, "alpha-project", projects[0])
}

func TestSessionStore_GetSessionByID_NotFound(t *testing.T) {
	sessionStore, _, cleanup := testSessionStore(t)
	defer cleanup()

	ctx := context.Background()

	// Non-existent ID should return nil, nil (not an error)
	sess, err := sessionStore.GetSessionByID(ctx, 999)
	require.NoError(t, err)
	assert.Nil(t, sess)
}

func TestSessionStore_SessionFields(t *testing.T) {
	sessionStore, store, cleanup := testSessionStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create a session with full details
	id, err := sessionStore.CreateSDKSession(ctx, "claude-full", "full-project", "full user prompt")
	require.NoError(t, err)

	// Manually update additional fields for testing
	now := time.Now()
	_, err = storeDB(store).Exec(`
		UPDATE sdk_sessions
		SET worker_port = ?, completed_at = ?, completed_at_epoch = ?, status = 'completed'
		WHERE id = ?
	`, 37777, now.Format(time.RFC3339), now.UnixMilli(), id)
	require.NoError(t, err)

	// Retrieve and verify all fields
	sess, err := sessionStore.GetSessionByID(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, sess)

	assert.Equal(t, id, sess.ID)
	assert.Equal(t, "claude-full", sess.ClaudeSessionID)
	assert.Equal(t, "full-project", sess.Project)
	assert.Equal(t, models.SessionStatusCompleted, sess.Status)
	assert.True(t, sess.WorkerPort.Valid)
	assert.Equal(t, int64(37777), sess.WorkerPort.Int64)
	assert.True(t, sess.CompletedAt.Valid)
	assert.True(t, sess.CompletedAtEpoch.Valid)
}
