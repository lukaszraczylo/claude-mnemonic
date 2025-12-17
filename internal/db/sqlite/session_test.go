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

func testSessionStore(t *testing.T) (*SessionStore, *Store, func()) {
	t.Helper()

	db, _, cleanup := testDB(t)
	createBaseTables(t, db) // Use base tables without FTS5 for session tests

	store := newStoreFromDB(db)
	sessionStore := NewSessionStore(store)

	return sessionStore, store, cleanup
}

// SessionStoreSuite is a test suite for SessionStore operations.
type SessionStoreSuite struct {
	suite.Suite
	sessionStore *SessionStore
	store        *Store
	cleanup      func()
}

func (s *SessionStoreSuite) SetupTest() {
	s.sessionStore, s.store, s.cleanup = testSessionStore(s.T())
}

func (s *SessionStoreSuite) TearDownTest() {
	if s.cleanup != nil {
		s.cleanup()
	}
}

func TestSessionStoreSuite(t *testing.T) {
	suite.Run(t, new(SessionStoreSuite))
}

// TestCreateSDKSession_TableDriven tests session creation with various scenarios.
func (s *SessionStoreSuite) TestCreateSDKSession_TableDriven() {
	ctx := context.Background()

	tests := []struct {
		name            string
		claudeSessionID string
		project         string
		userPrompt      string
		wantErr         bool
		wantID          bool
	}{
		{
			name:            "basic session creation",
			claudeSessionID: "claude-basic",
			project:         "project-a",
			userPrompt:      "hello world",
			wantErr:         false,
			wantID:          true,
		},
		{
			name:            "empty user prompt",
			claudeSessionID: "claude-noprompt",
			project:         "project-b",
			userPrompt:      "",
			wantErr:         false,
			wantID:          true,
		},
		{
			name:            "long project name",
			claudeSessionID: "claude-longproj",
			project:         "/Users/test/Documents/very/long/path/to/some/project/directory",
			userPrompt:      "test",
			wantErr:         false,
			wantID:          true,
		},
		{
			name:            "unicode project name",
			claudeSessionID: "claude-unicode",
			project:         "项目名称-プロジェクト",
			userPrompt:      "测试 テスト",
			wantErr:         false,
			wantID:          true,
		},
		{
			name:            "special characters in prompt",
			claudeSessionID: "claude-special",
			project:         "project-special",
			userPrompt:      "Fix the bug in file.go:123 with \"quotes\" and 'apostrophes'",
			wantErr:         false,
			wantID:          true,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			id, err := s.sessionStore.CreateSDKSession(ctx, tt.claudeSessionID, tt.project, tt.userPrompt)
			if tt.wantErr {
				s.Error(err)
			} else {
				s.NoError(err)
				if tt.wantID {
					s.Greater(id, int64(0))
				}

				// Verify created session
				sess, err := s.sessionStore.GetSessionByID(ctx, id)
				s.NoError(err)
				s.NotNil(sess)
				s.Equal(tt.claudeSessionID, sess.ClaudeSessionID)
				s.Equal(tt.project, sess.Project)
				s.Equal(models.SessionStatusActive, sess.Status)
			}
		})
	}
}

// TestIdempotentSession tests that session creation is idempotent.
func (s *SessionStoreSuite) TestIdempotentSession() {
	ctx := context.Background()

	// Create initial session
	id1, err := s.sessionStore.CreateSDKSession(ctx, "claude-idem", "project-1", "prompt-1")
	s.NoError(err)
	s.Greater(id1, int64(0))

	// Create with same claude_session_id - should return same ID
	id2, err := s.sessionStore.CreateSDKSession(ctx, "claude-idem", "project-2", "prompt-2")
	s.NoError(err)
	s.Equal(id1, id2)

	// Verify project was updated
	sess, err := s.sessionStore.GetSessionByID(ctx, id1)
	s.NoError(err)
	s.Equal("project-2", sess.Project)
}

// TestPromptCounterOperations tests prompt counter increment and retrieval.
func (s *SessionStoreSuite) TestPromptCounterOperations() {
	ctx := context.Background()

	tests := []struct {
		name          string
		increments    int
		expectedCount int
	}{
		{
			name:          "no increments",
			increments:    0,
			expectedCount: 0,
		},
		{
			name:          "single increment",
			increments:    1,
			expectedCount: 1,
		},
		{
			name:          "multiple increments",
			increments:    5,
			expectedCount: 5,
		},
		{
			name:          "many increments",
			increments:    100,
			expectedCount: 100,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			// Create fresh session for each test
			id, err := s.sessionStore.CreateSDKSession(ctx, "claude-counter-"+tt.name, "project", "")
			s.NoError(err)

			// Increment specified number of times
			var lastCount int
			for i := 0; i < tt.increments; i++ {
				lastCount, err = s.sessionStore.IncrementPromptCounter(ctx, id)
				s.NoError(err)
			}

			// Get final count
			finalCount, err := s.sessionStore.GetPromptCounter(ctx, id)
			s.NoError(err)
			s.Equal(tt.expectedCount, finalCount)

			if tt.increments > 0 {
				s.Equal(tt.expectedCount, lastCount)
			}
		})
	}
}

// TestFindAnySDKSession tests session lookup scenarios.
func (s *SessionStoreSuite) TestFindAnySDKSession_Scenarios() {
	ctx := context.Background()

	// Create test sessions
	_, err := s.sessionStore.CreateSDKSession(ctx, "session-find-1", "project-a", "")
	s.NoError(err)
	_, err = s.sessionStore.CreateSDKSession(ctx, "session-find-2", "project-b", "")
	s.NoError(err)

	tests := []struct {
		name            string
		claudeSessionID string
		wantFound       bool
		wantProject     string
	}{
		{
			name:            "find existing session 1",
			claudeSessionID: "session-find-1",
			wantFound:       true,
			wantProject:     "project-a",
		},
		{
			name:            "find existing session 2",
			claudeSessionID: "session-find-2",
			wantFound:       true,
			wantProject:     "project-b",
		},
		{
			name:            "find non-existent session",
			claudeSessionID: "session-nonexistent",
			wantFound:       false,
		},
		{
			name:            "find with empty string",
			claudeSessionID: "",
			wantFound:       false,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			sess, err := s.sessionStore.FindAnySDKSession(ctx, tt.claudeSessionID)
			s.NoError(err) // FindAnySDKSession returns nil,nil for not found

			if tt.wantFound {
				s.NotNil(sess)
				s.Equal(tt.wantProject, sess.Project)
			} else {
				s.Nil(sess)
			}
		})
	}
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
