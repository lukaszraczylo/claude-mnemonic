// Package session provides session lifecycle management for claude-mnemonic.
package session

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/lukaszraczylo/claude-mnemonic/internal/db/gorm"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	// Import sqlite driver
	_ "github.com/mattn/go-sqlite3"
)

// hasFTS5 checks if FTS5 is available in the SQLite build.
func hasFTS5(t *testing.T) bool {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "fts5-check-*")
	if err != nil {
		return false
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := gorm.NewStore(gorm.Config{
		Path:     tmpDir + "/check.db",
		MaxConns: 1,
	})
	if err != nil {
		return false
	}
	_ = store.Close()
	return true
}

// testStore creates a gorm.Store with a temporary database for testing.
func testStore(t *testing.T) (*gorm.Store, func()) {
	t.Helper()

	if !hasFTS5(t) {
		t.Skip("FTS5 not available in this SQLite build")
	}

	tmpDir, err := os.MkdirTemp("", "session-integration-test-*")
	require.NoError(t, err)

	dbPath := tmpDir + "/test.db"

	store, err := gorm.NewStore(gorm.Config{
		Path:     dbPath,
		MaxConns: 1,
	})
	require.NoError(t, err)

	cleanup := func() {
		_ = store.Close()
		_ = os.RemoveAll(tmpDir)
	}

	return store, cleanup
}

// SessionIntegrationSuite tests session manager with real SQLite stores.
type SessionIntegrationSuite struct {
	suite.Suite
	store        *gorm.Store
	sessionStore *gorm.SessionStore
	cleanup      func()
	manager      *Manager
}

func (s *SessionIntegrationSuite) SetupTest() {
	if !hasFTS5(s.T()) {
		s.T().Skip("FTS5 not available in this SQLite build")
	}

	s.store, s.cleanup = testStore(s.T())
	s.sessionStore = gorm.NewSessionStore(s.store)
	s.manager = NewManager(s.sessionStore)
}

func (s *SessionIntegrationSuite) TearDownTest() {
	if s.manager != nil {
		s.manager.ShutdownAll(context.Background())
	}
	if s.cleanup != nil {
		s.cleanup()
	}
}

func TestSessionIntegrationSuite(t *testing.T) {
	suite.Run(t, new(SessionIntegrationSuite))
}

// TestNewManager_WithRealStore tests manager creation with real store.
func (s *SessionIntegrationSuite) TestNewManager_WithRealStore() {
	s.NotNil(s.manager)
	s.NotNil(s.manager.sessionStore)
	s.NotNil(s.manager.sessions)
	s.NotNil(s.manager.ProcessNotify)
	s.Equal(0, s.manager.GetActiveSessionCount())
}

// TestInitializeSession_WithRealStore tests session initialization.
func (s *SessionIntegrationSuite) TestInitializeSession_WithRealStore() {
	ctx := context.Background()

	// Create a session in the database first
	sessionID, err := s.sessionStore.CreateSDKSession(ctx, "claude-test-123", "test-project", "initial prompt")
	s.Require().NoError(err)
	s.Require().Greater(sessionID, int64(0))

	// Initialize in manager
	session, err := s.manager.InitializeSession(ctx, sessionID, "user prompt", 1)
	s.Require().NoError(err)
	s.Require().NotNil(session)

	// Verify session properties
	s.Equal(sessionID, session.SessionDBID)
	s.Equal("claude-test-123", session.ClaudeSessionID)
	s.Equal("test-project", session.Project)
	s.Equal("user prompt", session.UserPrompt)
	s.Equal(1, session.LastPromptNumber)

	// Verify manager state
	s.Equal(1, s.manager.GetActiveSessionCount())
}

// TestInitializeSession_ReuseExisting tests that existing sessions are reused.
func (s *SessionIntegrationSuite) TestInitializeSession_ReuseExisting() {
	ctx := context.Background()

	// Create session in database
	sessionID, err := s.sessionStore.CreateSDKSession(ctx, "claude-reuse-123", "test-project", "prompt")
	s.Require().NoError(err)

	// Initialize first time
	session1, err := s.manager.InitializeSession(ctx, sessionID, "prompt 1", 1)
	s.Require().NoError(err)
	s.Require().NotNil(session1)

	// Initialize second time - should reuse
	session2, err := s.manager.InitializeSession(ctx, sessionID, "prompt 2", 2)
	s.Require().NoError(err)
	s.Require().NotNil(session2)

	// Should be the same session pointer
	s.Same(session1, session2)

	// Should have updated user prompt
	s.Equal("prompt 2", session2.UserPrompt)
	s.Equal(2, session2.LastPromptNumber)

	// Still only 1 active session
	s.Equal(1, s.manager.GetActiveSessionCount())
}

// TestInitializeSession_NonExistentSession tests initializing non-existent session.
func (s *SessionIntegrationSuite) TestInitializeSession_NonExistentSession() {
	ctx := context.Background()

	// Try to initialize non-existent session
	session, err := s.manager.InitializeSession(ctx, 999999, "prompt", 1)
	s.NoError(err) // No error, just nil session
	s.Nil(session)

	s.Equal(0, s.manager.GetActiveSessionCount())
}

// TestInitializeSession_EmptyUserPrompt tests initialization with empty user prompt.
func (s *SessionIntegrationSuite) TestInitializeSession_EmptyUserPrompt() {
	ctx := context.Background()

	// Create session with initial prompt in database
	sessionID, err := s.sessionStore.CreateSDKSession(ctx, "claude-empty-prompt", "test-project", "db prompt")
	s.Require().NoError(err)

	// Initialize with empty user prompt - should use database prompt
	session, err := s.manager.InitializeSession(ctx, sessionID, "", 0)
	s.Require().NoError(err)
	s.Require().NotNil(session)

	// Should use database prompt
	s.Equal("db prompt", session.UserPrompt)
}

// TestQueueObservation_WithRealStore tests observation queuing.
func (s *SessionIntegrationSuite) TestQueueObservation_WithRealStore() {
	ctx := context.Background()

	// Create and initialize session
	sessionID, err := s.sessionStore.CreateSDKSession(ctx, "claude-queue-obs", "test-project", "prompt")
	s.Require().NoError(err)

	_, err = s.manager.InitializeSession(ctx, sessionID, "prompt", 1)
	s.Require().NoError(err)

	// Queue an observation
	err = s.manager.QueueObservation(ctx, sessionID, ObservationData{
		ToolName:     "Read",
		ToolInput:    map[string]string{"path": "/test.go"},
		ToolResponse: "file content",
		PromptNumber: 1,
		CWD:          "/project",
	})
	s.Require().NoError(err)

	// Check queue depth
	s.Equal(1, s.manager.GetTotalQueueDepth())
	s.True(s.manager.IsAnySessionProcessing())

	// Drain messages
	messages := s.manager.DrainMessages(sessionID)
	s.Len(messages, 1)
	s.Equal(MessageTypeObservation, messages[0].Type)
	s.Equal("Read", messages[0].Observation.ToolName)
}

// TestQueueObservation_AutoInitialize tests auto-initialization on queue.
func (s *SessionIntegrationSuite) TestQueueObservation_AutoInitialize() {
	ctx := context.Background()

	// Create session in database but don't initialize in manager
	sessionID, err := s.sessionStore.CreateSDKSession(ctx, "claude-auto-init", "test-project", "prompt")
	s.Require().NoError(err)

	// Queue observation without explicit initialization
	err = s.manager.QueueObservation(ctx, sessionID, ObservationData{
		ToolName:     "Write",
		ToolInput:    "test input",
		ToolResponse: "success",
		PromptNumber: 1,
	})
	s.Require().NoError(err)

	// Session should be auto-initialized
	s.Equal(1, s.manager.GetActiveSessionCount())
	s.Equal(1, s.manager.GetTotalQueueDepth())
}

// TestQueueObservation_NonExistentSession tests queuing to non-existent session.
func (s *SessionIntegrationSuite) TestQueueObservation_NonExistentSession() {
	ctx := context.Background()

	// Try to queue to non-existent session
	err := s.manager.QueueObservation(ctx, 999999, ObservationData{
		ToolName: "Test",
	})

	// Should not error, but session won't be created
	s.NoError(err)
	s.Equal(0, s.manager.GetActiveSessionCount())
}

// TestQueueSummarize_WithRealStore tests summarize queuing.
func (s *SessionIntegrationSuite) TestQueueSummarize_WithRealStore() {
	ctx := context.Background()

	// Create and initialize session
	sessionID, err := s.sessionStore.CreateSDKSession(ctx, "claude-queue-sum", "test-project", "prompt")
	s.Require().NoError(err)

	_, err = s.manager.InitializeSession(ctx, sessionID, "prompt", 1)
	s.Require().NoError(err)

	// Queue a summarize request
	err = s.manager.QueueSummarize(ctx, sessionID, "What did you do?", "I completed the task.")
	s.Require().NoError(err)

	// Check queue depth
	s.Equal(1, s.manager.GetTotalQueueDepth())
	s.True(s.manager.IsAnySessionProcessing())

	// Drain messages
	messages := s.manager.DrainMessages(sessionID)
	s.Len(messages, 1)
	s.Equal(MessageTypeSummarize, messages[0].Type)
	s.Equal("What did you do?", messages[0].Summarize.LastUserMessage)
	s.Equal("I completed the task.", messages[0].Summarize.LastAssistantMessage)
}

// TestQueueSummarize_AutoInitialize tests auto-initialization on summarize queue.
func (s *SessionIntegrationSuite) TestQueueSummarize_AutoInitialize() {
	ctx := context.Background()

	// Create session in database but don't initialize in manager
	sessionID, err := s.sessionStore.CreateSDKSession(ctx, "claude-sum-auto", "test-project", "prompt")
	s.Require().NoError(err)

	// Queue summarize without explicit initialization
	err = s.manager.QueueSummarize(ctx, sessionID, "user msg", "assistant msg")
	s.Require().NoError(err)

	// Session should be auto-initialized
	s.Equal(1, s.manager.GetActiveSessionCount())
	s.Equal(1, s.manager.GetTotalQueueDepth())
}

// TestQueueSummarize_NonExistentSession tests summarize queuing to non-existent session.
func (s *SessionIntegrationSuite) TestQueueSummarize_NonExistentSession() {
	ctx := context.Background()

	// Try to queue to non-existent session
	err := s.manager.QueueSummarize(ctx, 999999, "user", "assistant")

	// Should not error, but session won't be created
	s.NoError(err)
	s.Equal(0, s.manager.GetActiveSessionCount())
}

// TestMixedQueueOperations tests mixed observation and summarize queuing.
func (s *SessionIntegrationSuite) TestMixedQueueOperations() {
	ctx := context.Background()

	// Create and initialize session
	sessionID, err := s.sessionStore.CreateSDKSession(ctx, "claude-mixed", "test-project", "prompt")
	s.Require().NoError(err)

	_, err = s.manager.InitializeSession(ctx, sessionID, "prompt", 1)
	s.Require().NoError(err)

	// Queue multiple messages of different types
	err = s.manager.QueueObservation(ctx, sessionID, ObservationData{ToolName: "Tool1"})
	s.Require().NoError(err)

	err = s.manager.QueueSummarize(ctx, sessionID, "user1", "assistant1")
	s.Require().NoError(err)

	err = s.manager.QueueObservation(ctx, sessionID, ObservationData{ToolName: "Tool2"})
	s.Require().NoError(err)

	// Check total queue depth
	s.Equal(3, s.manager.GetTotalQueueDepth())

	// Drain and verify order
	messages := s.manager.DrainMessages(sessionID)
	s.Len(messages, 3)
	s.Equal(MessageTypeObservation, messages[0].Type)
	s.Equal("Tool1", messages[0].Observation.ToolName)
	s.Equal(MessageTypeSummarize, messages[1].Type)
	s.Equal(MessageTypeObservation, messages[2].Type)
	s.Equal("Tool2", messages[2].Observation.ToolName)
}

// TestProcessNotifyChannel tests the process notification channel behavior.
func (s *SessionIntegrationSuite) TestProcessNotifyChannel() {
	ctx := context.Background()

	// Create and initialize session
	sessionID, err := s.sessionStore.CreateSDKSession(ctx, "claude-notify", "test-project", "prompt")
	s.Require().NoError(err)

	_, err = s.manager.InitializeSession(ctx, sessionID, "prompt", 1)
	s.Require().NoError(err)

	// Drain any existing notifications
	select {
	case <-s.manager.ProcessNotify:
	default:
	}

	// Queue observation - should trigger notification
	err = s.manager.QueueObservation(ctx, sessionID, ObservationData{ToolName: "Test"})
	s.Require().NoError(err)

	// Should be able to receive notification
	select {
	case <-s.manager.ProcessNotify:
		// Success
	case <-time.After(100 * time.Millisecond):
		s.Fail("Should have received process notification")
	}
}

// TestSessionCallbacks tests session lifecycle callbacks.
func (s *SessionIntegrationSuite) TestSessionCallbacks() {
	ctx := context.Background()

	var createdID, deletedID int64

	s.manager.SetOnSessionCreated(func(id int64) {
		createdID = id
	})
	s.manager.SetOnSessionDeleted(func(id int64) {
		deletedID = id
	})

	// Create session
	sessionID, err := s.sessionStore.CreateSDKSession(ctx, "claude-callbacks", "test-project", "prompt")
	s.Require().NoError(err)

	// Initialize - should trigger created callback
	_, err = s.manager.InitializeSession(ctx, sessionID, "prompt", 1)
	s.Require().NoError(err)

	s.Equal(sessionID, createdID)

	// Delete - should trigger deleted callback
	s.manager.DeleteSession(sessionID)

	s.Equal(sessionID, deletedID)
}

// TestMultipleSessions tests managing multiple sessions.
func (s *SessionIntegrationSuite) TestMultipleSessions() {
	ctx := context.Background()

	// Create multiple sessions
	var sessionIDs []int64
	for i := 0; i < 5; i++ {
		id, err := s.sessionStore.CreateSDKSession(ctx, "claude-multi-"+string(rune('A'+i)), "project-"+string(rune('a'+i)), "prompt")
		s.Require().NoError(err)
		sessionIDs = append(sessionIDs, id)
	}

	// Initialize all
	for _, id := range sessionIDs {
		_, err := s.manager.InitializeSession(ctx, id, "prompt", 1)
		s.Require().NoError(err)
	}

	s.Equal(5, s.manager.GetActiveSessionCount())

	// Queue observations to each
	for i, id := range sessionIDs {
		err := s.manager.QueueObservation(ctx, id, ObservationData{
			ToolName: "Tool" + string(rune('A'+i)),
		})
		s.Require().NoError(err)
	}

	s.Equal(5, s.manager.GetTotalQueueDepth())

	// Get all sessions
	sessions := s.manager.GetAllSessions()
	s.Len(sessions, 5)

	// Delete all
	for _, id := range sessionIDs {
		s.manager.DeleteSession(id)
	}

	s.Equal(0, s.manager.GetActiveSessionCount())
}

// TestCleanupStaleSessions tests the cleanup of stale sessions.
func (s *SessionIntegrationSuite) TestCleanupStaleSessions() {
	ctx := context.Background()

	// Create and initialize a session
	sessionID, err := s.sessionStore.CreateSDKSession(ctx, "claude-stale", "test-project", "prompt")
	s.Require().NoError(err)

	session, err := s.manager.InitializeSession(ctx, sessionID, "prompt", 1)
	s.Require().NoError(err)

	// Manually set start time to past (simulate stale session)
	session.StartTime = time.Now().Add(-SessionTimeout - time.Minute)

	// Run cleanup
	s.manager.cleanupStaleSessions()

	// Session should be deleted
	s.Equal(0, s.manager.GetActiveSessionCount())
}

// TestCleanupStaleSessions_WithPendingMessages tests cleanup doesn't delete sessions with pending messages.
func (s *SessionIntegrationSuite) TestCleanupStaleSessions_WithPendingMessages() {
	ctx := context.Background()

	// Create and initialize a session
	sessionID, err := s.sessionStore.CreateSDKSession(ctx, "claude-stale-pending", "test-project", "prompt")
	s.Require().NoError(err)

	session, err := s.manager.InitializeSession(ctx, sessionID, "prompt", 1)
	s.Require().NoError(err)

	// Make session stale but add pending messages
	session.StartTime = time.Now().Add(-SessionTimeout - time.Minute)
	err = s.manager.QueueObservation(ctx, sessionID, ObservationData{ToolName: "Test"})
	s.Require().NoError(err)

	// Run cleanup
	s.manager.cleanupStaleSessions()

	// Session should NOT be deleted (has pending messages)
	s.Equal(1, s.manager.GetActiveSessionCount())
}

// TestCleanupStaleSessions_WithActiveGenerator tests cleanup doesn't delete sessions with active generator.
func (s *SessionIntegrationSuite) TestCleanupStaleSessions_WithActiveGenerator() {
	ctx := context.Background()

	// Create and initialize a session
	sessionID, err := s.sessionStore.CreateSDKSession(ctx, "claude-stale-gen", "test-project", "prompt")
	s.Require().NoError(err)

	session, err := s.manager.InitializeSession(ctx, sessionID, "prompt", 1)
	s.Require().NoError(err)

	// Make session stale but mark generator as active
	session.StartTime = time.Now().Add(-SessionTimeout - time.Minute)
	session.generatorActive.Store(true)

	// Run cleanup
	s.manager.cleanupStaleSessions()

	// Session should NOT be deleted (generator is active)
	s.Equal(1, s.manager.GetActiveSessionCount())
}

// TestConcurrentQueueOperations tests thread-safe queue operations.
func (s *SessionIntegrationSuite) TestConcurrentQueueOperations() {
	ctx := context.Background()

	// Create and initialize session
	sessionID, err := s.sessionStore.CreateSDKSession(ctx, "claude-concurrent", "test-project", "prompt")
	s.Require().NoError(err)

	_, err = s.manager.InitializeSession(ctx, sessionID, "prompt", 1)
	s.Require().NoError(err)

	// Concurrent queue operations
	done := make(chan bool)
	numGoroutines := 50

	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			if idx%2 == 0 {
				_ = s.manager.QueueObservation(ctx, sessionID, ObservationData{
					ToolName: "Tool",
				})
			} else {
				_ = s.manager.QueueSummarize(ctx, sessionID, "user", "assistant")
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// All messages should be queued
	s.Equal(numGoroutines, s.manager.GetTotalQueueDepth())
}

// TestShutdownAll_WithRealSessions tests shutdown of all real sessions.
func (s *SessionIntegrationSuite) TestShutdownAll_WithRealSessions() {
	ctx := context.Background()

	// Create and initialize multiple sessions
	for i := 0; i < 3; i++ {
		sessionID, err := s.sessionStore.CreateSDKSession(ctx, "claude-shutdown-"+string(rune('A'+i)), "project", "prompt")
		s.Require().NoError(err)

		_, err = s.manager.InitializeSession(ctx, sessionID, "prompt", 1)
		s.Require().NoError(err)
	}

	s.Equal(3, s.manager.GetActiveSessionCount())

	// Shutdown all
	s.manager.ShutdownAll(ctx)

	// All sessions should be deleted
	s.Equal(0, s.manager.GetActiveSessionCount())
}

// TestSessionSDKSessionID tests SDK session ID handling.
func (s *SessionIntegrationSuite) TestSessionSDKSessionID() {
	ctx := context.Background()

	// Create session - SDK session ID is generated
	sessionID, err := s.sessionStore.CreateSDKSession(ctx, "claude-sdk-test", "test-project", "prompt")
	s.Require().NoError(err)

	// Initialize in manager
	session, err := s.manager.InitializeSession(ctx, sessionID, "prompt", 1)
	s.Require().NoError(err)
	s.Require().NotNil(session)

	// SDK session ID should be set
	s.NotEmpty(session.SDKSessionID)
}

// TestPromptNumberTracking tests prompt number tracking across operations.
func (s *SessionIntegrationSuite) TestPromptNumberTracking() {
	ctx := context.Background()

	// Create session
	sessionID, err := s.sessionStore.CreateSDKSession(ctx, "claude-prompt-num", "test-project", "initial")
	s.Require().NoError(err)

	// Initialize with prompt 1
	session, err := s.manager.InitializeSession(ctx, sessionID, "prompt 1", 1)
	s.Require().NoError(err)
	s.Equal(1, session.LastPromptNumber)

	// Re-initialize with prompt 2
	session, err = s.manager.InitializeSession(ctx, sessionID, "prompt 2", 2)
	s.Require().NoError(err)
	s.Equal(2, session.LastPromptNumber)

	// Re-initialize with prompt 5
	session, err = s.manager.InitializeSession(ctx, sessionID, "prompt 5", 5)
	s.Require().NoError(err)
	s.Equal(5, session.LastPromptNumber)
}
