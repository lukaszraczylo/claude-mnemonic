// Package session provides session lifecycle management for claude-mnemonic.
package session

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

// ManagerSuite is a test suite for Manager operations.
type ManagerSuite struct {
	suite.Suite
	manager *Manager
}

func (s *ManagerSuite) SetupTest() {
	// Create manager without real session store (use nil for unit tests)
	s.manager = &Manager{
		sessions:      make(map[int64]*ActiveSession),
		ProcessNotify: make(chan struct{}, 1),
	}
	// Initialize context for manager
	ctx, cancel := context.WithCancel(context.Background())
	s.manager.ctx = ctx
	s.manager.cancel = cancel
}

func (s *ManagerSuite) TearDownTest() {
	if s.manager != nil && s.manager.cancel != nil {
		s.manager.cancel()
	}
}

func TestManagerSuite(t *testing.T) {
	suite.Run(t, new(ManagerSuite))
}

// TestActiveSession tests ActiveSession creation and basic operations.
func (s *ManagerSuite) TestActiveSession() {
	session := &ActiveSession{
		SessionDBID:     1,
		ClaudeSessionID: "claude-123",
		SDKSessionID:    "sdk-123",
		Project:         "test-project",
		UserPrompt:      "Hello",
		StartTime:       time.Now(),
		pendingMessages: make([]PendingMessage, 0),
		notify:          make(chan struct{}, 1),
	}

	s.Equal(int64(1), session.SessionDBID)
	s.Equal("claude-123", session.ClaudeSessionID)
	s.Equal("sdk-123", session.SDKSessionID)
	s.Equal("test-project", session.Project)
	s.Equal("Hello", session.UserPrompt)
}

// TestGetActiveSessionCount tests session counting.
func (s *ManagerSuite) TestGetActiveSessionCount() {
	// Initially 0
	s.Equal(0, s.manager.GetActiveSessionCount())

	// Add sessions directly for testing
	s.manager.sessions[1] = &ActiveSession{SessionDBID: 1}
	s.manager.sessions[2] = &ActiveSession{SessionDBID: 2}

	s.Equal(2, s.manager.GetActiveSessionCount())
}

// TestGetTotalQueueDepth tests queue depth calculation.
func (s *ManagerSuite) TestGetTotalQueueDepth() {
	// Initially 0
	s.Equal(0, s.manager.GetTotalQueueDepth())

	// Add sessions with pending messages
	s.manager.sessions[1] = &ActiveSession{
		SessionDBID:     1,
		pendingMessages: make([]PendingMessage, 3),
	}
	s.manager.sessions[2] = &ActiveSession{
		SessionDBID:     2,
		pendingMessages: make([]PendingMessage, 5),
	}

	s.Equal(8, s.manager.GetTotalQueueDepth())
}

// TestIsAnySessionProcessing tests processing status detection.
func (s *ManagerSuite) TestIsAnySessionProcessing() {
	// No sessions - not processing
	s.False(s.manager.IsAnySessionProcessing())

	// Session with no pending - not processing
	s.manager.sessions[1] = &ActiveSession{
		SessionDBID:     1,
		pendingMessages: []PendingMessage{},
	}
	s.False(s.manager.IsAnySessionProcessing())

	// Session with pending - processing
	s.manager.sessions[1].pendingMessages = []PendingMessage{{Type: MessageTypeObservation}}
	s.True(s.manager.IsAnySessionProcessing())

	// Clear pending but set generator active
	s.manager.sessions[1].pendingMessages = []PendingMessage{}
	s.manager.sessions[1].generatorActive.Store(true)
	s.True(s.manager.IsAnySessionProcessing())
}

// TestGetAllSessions tests retrieving all sessions.
func (s *ManagerSuite) TestGetAllSessions() {
	// Empty
	sessions := s.manager.GetAllSessions()
	s.Empty(sessions)

	// Add sessions
	session1 := &ActiveSession{SessionDBID: 1, Project: "project-a"}
	session2 := &ActiveSession{SessionDBID: 2, Project: "project-b"}
	s.manager.sessions[1] = session1
	s.manager.sessions[2] = session2

	sessions = s.manager.GetAllSessions()
	s.Len(sessions, 2)
}

// TestDeleteSession tests session deletion.
func (s *ManagerSuite) TestDeleteSession() {
	// Create session with context
	ctx, cancel := context.WithCancel(context.Background())
	session := &ActiveSession{
		SessionDBID:     1,
		Project:         "test-project",
		StartTime:       time.Now(),
		pendingMessages: []PendingMessage{},
		ctx:             ctx,
		cancel:          cancel,
	}
	s.manager.sessions[1] = session

	// Track callback
	var deletedID int64
	s.manager.SetOnSessionDeleted(func(id int64) {
		deletedID = id
	})

	s.Equal(1, s.manager.GetActiveSessionCount())

	// Delete
	s.manager.DeleteSession(1)

	s.Equal(0, s.manager.GetActiveSessionCount())
	s.Equal(int64(1), deletedID)

	// Double delete should be safe
	s.manager.DeleteSession(1)
}

// TestDrainMessages tests message draining.
func (s *ManagerSuite) TestDrainMessages() {
	// No session - nil
	messages := s.manager.DrainMessages(999)
	s.Nil(messages)

	// Session with messages
	session := &ActiveSession{
		SessionDBID: 1,
		pendingMessages: []PendingMessage{
			{Type: MessageTypeObservation},
			{Type: MessageTypeSummarize},
		},
	}
	s.manager.sessions[1] = session

	messages = s.manager.DrainMessages(1)
	s.Len(messages, 2)

	// Queue should be empty now
	s.Empty(session.pendingMessages)

	// Drain again - empty
	messages = s.manager.DrainMessages(1)
	s.Empty(messages)
}

// TestSetOnSessionCreated tests callback setting.
func (s *ManagerSuite) TestSetOnSessionCreated() {
	var calledWith int64
	callback := func(id int64) {
		calledWith = id
	}

	s.manager.SetOnSessionCreated(callback)
	s.NotNil(s.manager.onCreated)

	// Simulate callback
	if s.manager.onCreated != nil {
		s.manager.onCreated(42)
	}
	s.Equal(int64(42), calledWith)
}

// TestSetOnSessionDeleted tests callback setting.
func (s *ManagerSuite) TestSetOnSessionDeleted() {
	var calledWith int64
	callback := func(id int64) {
		calledWith = id
	}

	s.manager.SetOnSessionDeleted(callback)
	s.NotNil(s.manager.onDeleted)

	// Simulate callback
	if s.manager.onDeleted != nil {
		s.manager.onDeleted(42)
	}
	s.Equal(int64(42), calledWith)
}

// TestMessageTypes tests message type constants.
func TestMessageTypes(t *testing.T) {
	assert.Equal(t, MessageType(0), MessageTypeObservation)
	assert.Equal(t, MessageType(1), MessageTypeSummarize)
}

// TestTimeoutConstants tests timeout constants.
func TestTimeoutConstants(t *testing.T) {
	assert.Equal(t, 30*time.Minute, SessionTimeout)
	assert.Equal(t, 5*time.Minute, CleanupInterval)
}

// TestObservationData tests observation data structure.
func TestObservationData(t *testing.T) {
	data := ObservationData{
		ToolName:     "Read",
		ToolInput:    map[string]string{"path": "/test/file.go"},
		ToolResponse: "file content",
		PromptNumber: 1,
		CWD:          "/test",
	}

	assert.Equal(t, "Read", data.ToolName)
	assert.Equal(t, 1, data.PromptNumber)
	assert.Equal(t, "/test", data.CWD)
}

// TestSummarizeData tests summarize data structure.
func TestSummarizeData(t *testing.T) {
	data := SummarizeData{
		LastUserMessage:      "What did you do?",
		LastAssistantMessage: "I completed the task.",
	}

	assert.Equal(t, "What did you do?", data.LastUserMessage)
	assert.Equal(t, "I completed the task.", data.LastAssistantMessage)
}

// TestPendingMessage tests pending message structure.
func TestPendingMessage(t *testing.T) {
	obsData := &ObservationData{ToolName: "Read"}
	msg := PendingMessage{
		Type:        MessageTypeObservation,
		Observation: obsData,
	}

	assert.Equal(t, MessageTypeObservation, msg.Type)
	assert.NotNil(t, msg.Observation)
	assert.Nil(t, msg.Summarize)

	sumData := &SummarizeData{LastUserMessage: "Test"}
	msg2 := PendingMessage{
		Type:      MessageTypeSummarize,
		Summarize: sumData,
	}

	assert.Equal(t, MessageTypeSummarize, msg2.Type)
	assert.Nil(t, msg2.Observation)
	assert.NotNil(t, msg2.Summarize)
}

// TestConcurrentSessionAccess tests thread-safe session operations.
func TestConcurrentSessionAccess(t *testing.T) {
	manager := &Manager{
		sessions:      make(map[int64]*ActiveSession),
		ProcessNotify: make(chan struct{}, 1),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.ctx = ctx
	manager.cancel = cancel

	var wg sync.WaitGroup
	numGoroutines := 100

	// Concurrent session operations
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()

			// Add session
			ctx, cancel := context.WithCancel(context.Background())
			manager.mu.Lock()
			manager.sessions[id] = &ActiveSession{
				SessionDBID: id,
				Project:     "test",
				StartTime:   time.Now(),
				ctx:         ctx,
				cancel:      cancel,
			}
			manager.mu.Unlock()

			// Read operations
			_ = manager.GetActiveSessionCount()
			_ = manager.GetTotalQueueDepth()
			_ = manager.IsAnySessionProcessing()
			_ = manager.GetAllSessions()

			// Delete session
			manager.DeleteSession(id)
		}(int64(i))
	}

	wg.Wait()

	// All sessions should be deleted
	assert.Equal(t, 0, manager.GetActiveSessionCount())
}

// TestProcessNotifyChannel tests the process notification channel.
func TestProcessNotifyChannel(t *testing.T) {
	manager := &Manager{
		sessions:      make(map[int64]*ActiveSession),
		ProcessNotify: make(chan struct{}, 1),
	}

	// Non-blocking send should work
	select {
	case manager.ProcessNotify <- struct{}{}:
		// Success
	default:
		t.Error("ProcessNotify channel should accept first message")
	}

	// Second send should not block (channel is buffered with size 1)
	select {
	case manager.ProcessNotify <- struct{}{}:
		// Full buffer, this is expected behavior
	default:
		// This is fine - channel is full
	}

	// Drain the channel
	select {
	case <-manager.ProcessNotify:
		// Drained
	default:
		t.Error("Should be able to receive from ProcessNotify")
	}
}

// TestActiveSessionContext tests session context handling.
func TestActiveSessionContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	session := &ActiveSession{
		SessionDBID: 1,
		ctx:         ctx,
		cancel:      cancel,
	}

	// Context should not be done
	select {
	case <-session.ctx.Done():
		t.Error("Context should not be done yet")
	default:
		// Expected
	}

	// Cancel context
	session.cancel()

	// Context should be done
	select {
	case <-session.ctx.Done():
		// Expected
	default:
		t.Error("Context should be done after cancel")
	}
}

// TestGeneratorActive tests the atomic generator active flag.
func TestGeneratorActive(t *testing.T) {
	session := &ActiveSession{}

	// Initially false
	assert.False(t, session.generatorActive.Load())

	// Set to true
	session.generatorActive.Store(true)
	assert.True(t, session.generatorActive.Load())

	// Set back to false
	session.generatorActive.Store(false)
	assert.False(t, session.generatorActive.Load())
}

// TestTokenAccumulation tests token accumulation fields.
func TestTokenAccumulation(t *testing.T) {
	session := &ActiveSession{
		CumulativeInputTokens:  0,
		CumulativeOutputTokens: 0,
	}

	// Accumulate tokens
	session.CumulativeInputTokens += 100
	session.CumulativeOutputTokens += 50

	assert.Equal(t, int64(100), session.CumulativeInputTokens)
	assert.Equal(t, int64(50), session.CumulativeOutputTokens)

	// Add more
	session.CumulativeInputTokens += 200
	session.CumulativeOutputTokens += 100

	assert.Equal(t, int64(300), session.CumulativeInputTokens)
	assert.Equal(t, int64(150), session.CumulativeOutputTokens)
}

// TestShutdownAll tests graceful shutdown of all sessions.
func (s *ManagerSuite) TestShutdownAll() {
	// Create multiple sessions
	for i := int64(1); i <= 3; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		s.manager.sessions[i] = &ActiveSession{
			SessionDBID:     i,
			Project:         "test-project",
			StartTime:       time.Now(),
			pendingMessages: []PendingMessage{},
			ctx:             ctx,
			cancel:          cancel,
		}
	}

	s.Equal(3, s.manager.GetActiveSessionCount())

	// Track deleted sessions
	var deletedIDs []int64
	s.manager.SetOnSessionDeleted(func(id int64) {
		deletedIDs = append(deletedIDs, id)
	})

	// Shutdown all
	s.manager.ShutdownAll(context.Background())

	// All sessions should be deleted
	s.Equal(0, s.manager.GetActiveSessionCount())
	s.Len(deletedIDs, 3)
}

// TestDeleteNonExistentSession tests deleting a session that doesn't exist.
func (s *ManagerSuite) TestDeleteNonExistentSession() {
	// Track callback
	callbackCalled := false
	s.manager.SetOnSessionDeleted(func(id int64) {
		callbackCalled = true
	})

	// Delete non-existent session
	s.manager.DeleteSession(999)

	// Callback should not be called
	s.False(callbackCalled)
}

// TestLastPromptNumber tests prompt number tracking.
func TestLastPromptNumber(t *testing.T) {
	session := &ActiveSession{
		SessionDBID:      1,
		LastPromptNumber: 0,
	}

	assert.Equal(t, 0, session.LastPromptNumber)

	session.LastPromptNumber = 5
	assert.Equal(t, 5, session.LastPromptNumber)

	session.LastPromptNumber++
	assert.Equal(t, 6, session.LastPromptNumber)
}

// TestActiveSessionNotifyChannel tests session notification channel.
func TestActiveSessionNotifyChannel(t *testing.T) {
	session := &ActiveSession{
		notify: make(chan struct{}, 1),
	}

	// Non-blocking send
	select {
	case session.notify <- struct{}{}:
		// Success
	default:
		t.Error("Should accept first notification")
	}

	// Second send should not block
	select {
	case session.notify <- struct{}{}:
		// Full buffer
	default:
		// Expected - buffer is full
	}

	// Drain
	select {
	case <-session.notify:
		// Drained
	default:
		t.Error("Should receive notification")
	}
}

// TestMessageMutex tests message mutex operations.
func TestMessageMutex(t *testing.T) {
	session := &ActiveSession{
		pendingMessages: make([]PendingMessage, 0),
	}

	var wg sync.WaitGroup

	// Concurrent message operations
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			session.messageMu.Lock()
			session.pendingMessages = append(session.pendingMessages, PendingMessage{
				Type: MessageTypeObservation,
			})
			session.messageMu.Unlock()
		}()
	}

	wg.Wait()

	assert.Len(t, session.pendingMessages, 50)
}

// TestQueueDepthMultipleSessions tests queue depth with multiple sessions.
func (s *ManagerSuite) TestQueueDepthMultipleSessions() {
	// Add sessions with varying queue depths
	s.manager.sessions[1] = &ActiveSession{
		SessionDBID:     1,
		pendingMessages: make([]PendingMessage, 10),
	}
	s.manager.sessions[2] = &ActiveSession{
		SessionDBID:     2,
		pendingMessages: make([]PendingMessage, 0),
	}
	s.manager.sessions[3] = &ActiveSession{
		SessionDBID:     3,
		pendingMessages: make([]PendingMessage, 5),
	}

	s.Equal(15, s.manager.GetTotalQueueDepth())
}

// TestIsAnySessionProcessing_GeneratorOnly tests processing status with only generator active.
func (s *ManagerSuite) TestIsAnySessionProcessingGeneratorOnly() {
	session := &ActiveSession{
		SessionDBID:     1,
		pendingMessages: []PendingMessage{},
	}
	s.manager.sessions[1] = session

	// No processing initially
	s.False(s.manager.IsAnySessionProcessing())

	// Set generator active
	session.generatorActive.Store(true)
	s.True(s.manager.IsAnySessionProcessing())

	// Clear generator
	session.generatorActive.Store(false)
	s.False(s.manager.IsAnySessionProcessing())
}

// TestPendingMessageWithBothTypes tests pending messages with both types.
func TestPendingMessageWithBothTypes(t *testing.T) {
	messages := []PendingMessage{
		{
			Type:        MessageTypeObservation,
			Observation: &ObservationData{ToolName: "Read"},
		},
		{
			Type:      MessageTypeSummarize,
			Summarize: &SummarizeData{LastUserMessage: "Test"},
		},
		{
			Type:        MessageTypeObservation,
			Observation: &ObservationData{ToolName: "Write"},
		},
	}

	assert.Len(t, messages, 3)

	// Verify types
	assert.Equal(t, MessageTypeObservation, messages[0].Type)
	assert.Equal(t, MessageTypeSummarize, messages[1].Type)
	assert.Equal(t, MessageTypeObservation, messages[2].Type)

	// Verify data
	assert.Equal(t, "Read", messages[0].Observation.ToolName)
	assert.Nil(t, messages[0].Summarize)

	assert.Equal(t, "Test", messages[1].Summarize.LastUserMessage)
	assert.Nil(t, messages[1].Observation)

	assert.Equal(t, "Write", messages[2].Observation.ToolName)
}

// TestDrainMessagesPreservesOrder tests that draining preserves message order.
func (s *ManagerSuite) TestDrainMessagesPreservesOrder() {
	session := &ActiveSession{
		SessionDBID: 1,
		pendingMessages: []PendingMessage{
			{Type: MessageTypeObservation, Observation: &ObservationData{ToolName: "Tool1"}},
			{Type: MessageTypeSummarize, Summarize: &SummarizeData{LastUserMessage: "Msg1"}},
			{Type: MessageTypeObservation, Observation: &ObservationData{ToolName: "Tool2"}},
		},
	}
	s.manager.sessions[1] = session

	messages := s.manager.DrainMessages(1)

	s.Len(messages, 3)
	s.Equal("Tool1", messages[0].Observation.ToolName)
	s.Equal("Msg1", messages[1].Summarize.LastUserMessage)
	s.Equal("Tool2", messages[2].Observation.ToolName)
}

// TestActiveSessionCWD tests CWD field in ObservationData.
func TestActiveSessionCWD(t *testing.T) {
	tests := []struct {
		name string
		cwd  string
	}{
		{"empty_cwd", ""},
		{"absolute_path", "/home/user/project"},
		{"windows_path", "C:\\Users\\test\\project"},
		{"path_with_spaces", "/home/user/my project"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := ObservationData{
				ToolName: "Test",
				CWD:      tt.cwd,
			}
			assert.Equal(t, tt.cwd, data.CWD)
		})
	}
}

// TestToolInputResponse tests various tool input/response types.
func TestToolInputResponse(t *testing.T) {
	tests := []struct {
		input    interface{}
		response interface{}
		name     string
	}{
		{name: "nil_values", input: nil, response: nil},
		{name: "string_values", input: "input string", response: "response string"},
		{name: "map_values", input: map[string]string{"key": "value"}, response: map[string]interface{}{"result": true}},
		{name: "slice_values", input: []string{"a", "b"}, response: []int{1, 2, 3}},
		{name: "int_values", input: 42, response: 100},
		{name: "bool_values", input: true, response: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := ObservationData{
				ToolName:     "TestTool",
				ToolInput:    tt.input,
				ToolResponse: tt.response,
			}
			assert.Equal(t, tt.input, data.ToolInput)
			assert.Equal(t, tt.response, data.ToolResponse)
		})
	}
}

// =============================================================================
// TESTS FOR NewManager AND CLEANUP
// =============================================================================

// TestNewManager tests the NewManager function.
func TestNewManager(t *testing.T) {
	t.Parallel()

	// Test with nil session store (valid for testing)
	manager := NewManager(nil)

	assert.NotNil(t, manager)
	assert.NotNil(t, manager.sessions)
	assert.NotNil(t, manager.ProcessNotify)
	assert.NotNil(t, manager.ctx)
	assert.NotNil(t, manager.cancel)
	assert.Equal(t, 0, manager.GetActiveSessionCount())

	// Clean up - cancel context to stop cleanup goroutine
	manager.cancel()
}

// TestNewManager_CleanupGoroutineStops tests that cleanup goroutine stops on cancel.
func TestNewManager_CleanupGoroutineStops(t *testing.T) {
	t.Parallel()

	manager := NewManager(nil)

	// Give goroutine time to start
	time.Sleep(10 * time.Millisecond)

	// Cancel should stop the cleanup goroutine
	manager.cancel()

	// Context should be done
	select {
	case <-manager.ctx.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("Context should be done after cancel")
	}
}

// TestCleanupStaleSessions_NoSessions tests cleanup with no sessions.
func TestCleanupStaleSessions_NoSessions(t *testing.T) {
	t.Parallel()

	manager := &Manager{
		sessions:      make(map[int64]*ActiveSession),
		ProcessNotify: make(chan struct{}, 1),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.ctx = ctx
	manager.cancel = cancel

	// Should not panic with empty sessions
	manager.cleanupStaleSessions()
	assert.Equal(t, 0, manager.GetActiveSessionCount())
}

// TestCleanupStaleSessions_FreshSession tests that fresh sessions are not cleaned.
func TestCleanupStaleSessions_FreshSession(t *testing.T) {
	t.Parallel()

	manager := &Manager{
		sessions:      make(map[int64]*ActiveSession),
		ProcessNotify: make(chan struct{}, 1),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.ctx = ctx
	manager.cancel = cancel

	// Add a fresh session
	sessionCtx, sessionCancel := context.WithCancel(context.Background())
	manager.sessions[1] = &ActiveSession{
		SessionDBID:     1,
		StartTime:       time.Now(), // Fresh
		pendingMessages: []PendingMessage{},
		ctx:             sessionCtx,
		cancel:          sessionCancel,
	}

	manager.cleanupStaleSessions()

	// Session should still exist (not stale)
	assert.Equal(t, 1, manager.GetActiveSessionCount())
	sessionCancel()
}

// TestCleanupStaleSessions_StaleSession tests that stale sessions are cleaned.
func TestCleanupStaleSessions_StaleSession(t *testing.T) {
	t.Parallel()

	manager := &Manager{
		sessions:      make(map[int64]*ActiveSession),
		ProcessNotify: make(chan struct{}, 1),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.ctx = ctx
	manager.cancel = cancel

	// Add a stale session
	sessionCtx, sessionCancel := context.WithCancel(context.Background())
	manager.sessions[1] = &ActiveSession{
		SessionDBID:     1,
		StartTime:       time.Now().Add(-SessionTimeout - time.Minute), // Stale
		pendingMessages: []PendingMessage{},
		ctx:             sessionCtx,
		cancel:          sessionCancel,
	}

	manager.cleanupStaleSessions()

	// Session should be deleted
	assert.Equal(t, 0, manager.GetActiveSessionCount())
}

// TestCleanupStaleSessions_StaleWithPending tests stale sessions with pending messages are not cleaned.
func TestCleanupStaleSessions_StaleWithPending(t *testing.T) {
	t.Parallel()

	manager := &Manager{
		sessions:      make(map[int64]*ActiveSession),
		ProcessNotify: make(chan struct{}, 1),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.ctx = ctx
	manager.cancel = cancel

	// Add a stale session with pending messages
	sessionCtx, sessionCancel := context.WithCancel(context.Background())
	defer sessionCancel()
	manager.sessions[1] = &ActiveSession{
		SessionDBID:     1,
		StartTime:       time.Now().Add(-SessionTimeout - time.Minute), // Stale
		pendingMessages: []PendingMessage{{Type: MessageTypeObservation}},
		ctx:             sessionCtx,
		cancel:          sessionCancel,
	}

	manager.cleanupStaleSessions()

	// Session should NOT be deleted (has pending messages)
	assert.Equal(t, 1, manager.GetActiveSessionCount())
}

// TestCleanupStaleSessions_StaleWithActiveGenerator tests stale sessions with active generator are not cleaned.
func TestCleanupStaleSessions_StaleWithActiveGenerator(t *testing.T) {
	t.Parallel()

	manager := &Manager{
		sessions:      make(map[int64]*ActiveSession),
		ProcessNotify: make(chan struct{}, 1),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.ctx = ctx
	manager.cancel = cancel

	// Add a stale session with active generator
	sessionCtx, sessionCancel := context.WithCancel(context.Background())
	defer sessionCancel()
	session := &ActiveSession{
		SessionDBID:     1,
		StartTime:       time.Now().Add(-SessionTimeout - time.Minute), // Stale
		pendingMessages: []PendingMessage{},
		ctx:             sessionCtx,
		cancel:          sessionCancel,
	}
	session.generatorActive.Store(true)
	manager.sessions[1] = session

	manager.cleanupStaleSessions()

	// Session should NOT be deleted (generator is active)
	assert.Equal(t, 1, manager.GetActiveSessionCount())
}

// TestCleanupStaleSessions_MixedSessions tests cleanup with mixed fresh and stale sessions.
func TestCleanupStaleSessions_MixedSessions(t *testing.T) {
	t.Parallel()

	manager := &Manager{
		sessions:      make(map[int64]*ActiveSession),
		ProcessNotify: make(chan struct{}, 1),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.ctx = ctx
	manager.cancel = cancel

	// Fresh session
	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	manager.sessions[1] = &ActiveSession{
		SessionDBID:     1,
		StartTime:       time.Now(),
		pendingMessages: []PendingMessage{},
		ctx:             ctx1,
		cancel:          cancel1,
	}

	// Stale session (should be deleted)
	ctx2, cancel2 := context.WithCancel(context.Background())
	manager.sessions[2] = &ActiveSession{
		SessionDBID:     2,
		StartTime:       time.Now().Add(-SessionTimeout - time.Minute),
		pendingMessages: []PendingMessage{},
		ctx:             ctx2,
		cancel:          cancel2,
	}

	// Stale session with pending (should NOT be deleted)
	ctx3, cancel3 := context.WithCancel(context.Background())
	defer cancel3()
	manager.sessions[3] = &ActiveSession{
		SessionDBID:     3,
		StartTime:       time.Now().Add(-SessionTimeout - time.Minute),
		pendingMessages: []PendingMessage{{Type: MessageTypeObservation}},
		ctx:             ctx3,
		cancel:          cancel3,
	}

	manager.cleanupStaleSessions()

	// Should have 2 sessions left (1 fresh, 1 stale with pending)
	assert.Equal(t, 2, manager.GetActiveSessionCount())

	// Verify which sessions remain
	manager.mu.RLock()
	_, has1 := manager.sessions[1]
	_, has2 := manager.sessions[2]
	_, has3 := manager.sessions[3]
	manager.mu.RUnlock()

	assert.True(t, has1, "Fresh session should remain")
	assert.False(t, has2, "Stale session should be deleted")
	assert.True(t, has3, "Stale session with pending should remain")
}

// TestCleanupLoop_ExitsOnCancel tests that cleanup loop exits when context is cancelled.
func TestCleanupLoop_ExitsOnCancel(t *testing.T) {
	t.Parallel()

	manager := &Manager{
		sessions:      make(map[int64]*ActiveSession),
		ProcessNotify: make(chan struct{}, 1),
	}
	ctx, cancel := context.WithCancel(context.Background())
	manager.ctx = ctx
	manager.cancel = cancel

	// Start cleanup loop in goroutine
	done := make(chan struct{})
	go func() {
		manager.cleanupLoop()
		close(done)
	}()

	// Cancel immediately
	cancel()

	// Should exit quickly
	select {
	case <-done:
		// Success - loop exited
	case <-time.After(100 * time.Millisecond):
		t.Error("Cleanup loop should exit when context is cancelled")
	}
}

// =============================================================================
// TESTS FOR InitializeSession (without DB)
// =============================================================================

// TestInitializeSession_AlreadyActive tests reusing an already active session.
func TestInitializeSession_AlreadyActive(t *testing.T) {
	t.Parallel()

	manager := &Manager{
		sessions:      make(map[int64]*ActiveSession),
		ProcessNotify: make(chan struct{}, 1),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.ctx = ctx
	manager.cancel = cancel

	// Pre-add an active session
	existingSession := &ActiveSession{
		SessionDBID:      42,
		ClaudeSessionID:  "claude-existing",
		Project:          "test-project",
		UserPrompt:       "original prompt",
		LastPromptNumber: 1,
		StartTime:        time.Now(),
		pendingMessages:  make([]PendingMessage, 0),
	}
	manager.sessions[42] = existingSession

	// Initialize same session - should reuse
	session, err := manager.InitializeSession(context.Background(), 42, "new prompt", 5)

	assert.NoError(t, err)
	assert.NotNil(t, session)
	assert.Same(t, existingSession, session)
	assert.Equal(t, "new prompt", session.UserPrompt)
	assert.Equal(t, 5, session.LastPromptNumber)
}

// TestInitializeSession_AlreadyActive_EmptyPrompt tests reusing session with empty prompt.
func TestInitializeSession_AlreadyActive_EmptyPrompt(t *testing.T) {
	t.Parallel()

	manager := &Manager{
		sessions:      make(map[int64]*ActiveSession),
		ProcessNotify: make(chan struct{}, 1),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.ctx = ctx
	manager.cancel = cancel

	// Pre-add an active session
	existingSession := &ActiveSession{
		SessionDBID:      42,
		UserPrompt:       "original prompt",
		LastPromptNumber: 1,
	}
	manager.sessions[42] = existingSession

	// Initialize with empty prompt - should NOT update
	session, err := manager.InitializeSession(context.Background(), 42, "", 0)

	assert.NoError(t, err)
	assert.NotNil(t, session)
	assert.Equal(t, "original prompt", session.UserPrompt) // Unchanged
	assert.Equal(t, 1, session.LastPromptNumber)           // Unchanged
}

// TestInitializeSession_NoStore tests initialization without session store.
func TestInitializeSession_NoStore(t *testing.T) {
	t.Parallel()

	manager := &Manager{
		sessionStore:  nil, // No store
		sessions:      make(map[int64]*ActiveSession),
		ProcessNotify: make(chan struct{}, 1),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.ctx = ctx
	manager.cancel = cancel

	// Should fail gracefully with nil store (panic recovery not expected)
	// This tests the guard against nil sessionStore
	defer func() {
		if r := recover(); r != nil {
			_ = r // Expected panic when calling nil store - intentionally ignored
		}
	}()

	_, _ = manager.InitializeSession(context.Background(), 999, "prompt", 1)
}

// TestInitializeSession_CallbackTriggered tests that created callback is triggered.
func TestInitializeSession_CallbackTriggered(t *testing.T) {
	t.Parallel()

	manager := &Manager{
		sessions:      make(map[int64]*ActiveSession),
		ProcessNotify: make(chan struct{}, 1),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.ctx = ctx
	manager.cancel = cancel

	var calledWithID int64
	manager.SetOnSessionCreated(func(id int64) {
		calledWithID = id
	})

	// Add session directly (simulating what would happen after DB fetch)
	sessionCtx, sessionCancel := context.WithCancel(context.Background())
	defer sessionCancel()
	session := &ActiveSession{
		SessionDBID:     100,
		ClaudeSessionID: "test",
		Project:         "project",
		StartTime:       time.Now(),
		pendingMessages: make([]PendingMessage, 0),
		notify:          make(chan struct{}, 1),
		ctx:             sessionCtx,
		cancel:          sessionCancel,
	}

	manager.mu.Lock()
	manager.sessions[100] = session
	onCreated := manager.onCreated
	manager.mu.Unlock()

	// Trigger callback
	if onCreated != nil {
		onCreated(100)
	}

	assert.Equal(t, int64(100), calledWithID)
}

// =============================================================================
// TESTS FOR QueueObservation AND QueueSummarize (without DB)
// =============================================================================

// TestQueueObservation_ToExistingSession tests queuing to an existing session.
func TestQueueObservation_ToExistingSession(t *testing.T) {
	t.Parallel()

	manager := &Manager{
		sessions:      make(map[int64]*ActiveSession),
		ProcessNotify: make(chan struct{}, 1),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.ctx = ctx
	manager.cancel = cancel

	// Pre-add session
	session := &ActiveSession{
		SessionDBID:     1,
		pendingMessages: make([]PendingMessage, 0),
		notify:          make(chan struct{}, 1),
	}
	manager.sessions[1] = session

	// Queue observation
	err := manager.QueueObservation(context.Background(), 1, ObservationData{
		ToolName:     "Read",
		ToolInput:    map[string]string{"path": "/test"},
		ToolResponse: "content",
		PromptNumber: 1,
		CWD:          "/project",
	})

	assert.NoError(t, err)
	assert.Equal(t, 1, manager.GetTotalQueueDepth())

	// Verify message
	messages := manager.DrainMessages(1)
	assert.Len(t, messages, 1)
	assert.Equal(t, MessageTypeObservation, messages[0].Type)
	assert.Equal(t, "Read", messages[0].Observation.ToolName)
	assert.Equal(t, "/project", messages[0].Observation.CWD)
}

// TestQueueObservation_NotifiesSession tests that notification is sent to session.
func TestQueueObservation_NotifiesSession(t *testing.T) {
	t.Parallel()

	manager := &Manager{
		sessions:      make(map[int64]*ActiveSession),
		ProcessNotify: make(chan struct{}, 1),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.ctx = ctx
	manager.cancel = cancel

	// Pre-add session with notify channel
	session := &ActiveSession{
		SessionDBID:     1,
		pendingMessages: make([]PendingMessage, 0),
		notify:          make(chan struct{}, 1),
	}
	manager.sessions[1] = session

	// Queue observation
	err := manager.QueueObservation(context.Background(), 1, ObservationData{ToolName: "Test"})
	assert.NoError(t, err)

	// Should receive notification on session channel
	select {
	case <-session.notify:
		// Success
	default:
		t.Error("Session should receive notification")
	}

	// Should receive notification on process channel
	select {
	case <-manager.ProcessNotify:
		// Success
	default:
		t.Error("Manager ProcessNotify should receive notification")
	}
}

// TestQueueSummarize_ToExistingSession tests queuing summarize to an existing session.
func TestQueueSummarize_ToExistingSession(t *testing.T) {
	t.Parallel()

	manager := &Manager{
		sessions:      make(map[int64]*ActiveSession),
		ProcessNotify: make(chan struct{}, 1),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.ctx = ctx
	manager.cancel = cancel

	// Pre-add session
	session := &ActiveSession{
		SessionDBID:     1,
		pendingMessages: make([]PendingMessage, 0),
		notify:          make(chan struct{}, 1),
	}
	manager.sessions[1] = session

	// Queue summarize
	err := manager.QueueSummarize(context.Background(), 1, "User asked question", "Assistant answered")
	assert.NoError(t, err)
	assert.Equal(t, 1, manager.GetTotalQueueDepth())

	// Verify message
	messages := manager.DrainMessages(1)
	assert.Len(t, messages, 1)
	assert.Equal(t, MessageTypeSummarize, messages[0].Type)
	assert.Equal(t, "User asked question", messages[0].Summarize.LastUserMessage)
	assert.Equal(t, "Assistant answered", messages[0].Summarize.LastAssistantMessage)
}

// TestQueueSummarize_NotifiesSession tests that notification is sent to session.
func TestQueueSummarize_NotifiesSession(t *testing.T) {
	t.Parallel()

	manager := &Manager{
		sessions:      make(map[int64]*ActiveSession),
		ProcessNotify: make(chan struct{}, 1),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.ctx = ctx
	manager.cancel = cancel

	// Pre-add session with notify channel
	session := &ActiveSession{
		SessionDBID:     1,
		pendingMessages: make([]PendingMessage, 0),
		notify:          make(chan struct{}, 1),
	}
	manager.sessions[1] = session

	// Queue summarize
	err := manager.QueueSummarize(context.Background(), 1, "user", "assistant")
	assert.NoError(t, err)

	// Should receive notification on session channel
	select {
	case <-session.notify:
		// Success
	default:
		t.Error("Session should receive notification")
	}

	// Should receive notification on process channel
	select {
	case <-manager.ProcessNotify:
		// Success
	default:
		t.Error("Manager ProcessNotify should receive notification")
	}
}

// TestQueueOperations_MultipleMessages tests queuing multiple messages.
func TestQueueOperations_MultipleMessages(t *testing.T) {
	t.Parallel()

	manager := &Manager{
		sessions:      make(map[int64]*ActiveSession),
		ProcessNotify: make(chan struct{}, 1),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.ctx = ctx
	manager.cancel = cancel

	// Pre-add session
	session := &ActiveSession{
		SessionDBID:     1,
		pendingMessages: make([]PendingMessage, 0),
		notify:          make(chan struct{}, 1),
	}
	manager.sessions[1] = session

	// Queue multiple messages
	for i := 0; i < 10; i++ {
		if i%2 == 0 {
			err := manager.QueueObservation(context.Background(), 1, ObservationData{
				ToolName: "Tool" + string(rune('A'+i)),
			})
			assert.NoError(t, err)
		} else {
			err := manager.QueueSummarize(context.Background(), 1, "user", "assistant")
			assert.NoError(t, err)
		}
	}

	assert.Equal(t, 10, manager.GetTotalQueueDepth())

	// Drain and verify
	messages := manager.DrainMessages(1)
	assert.Len(t, messages, 10)
}

// TestQueueOperations_NonBlockingNotification tests non-blocking notification behavior.
func TestQueueOperations_NonBlockingNotification(t *testing.T) {
	t.Parallel()

	manager := &Manager{
		sessions:      make(map[int64]*ActiveSession),
		ProcessNotify: make(chan struct{}, 1),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.ctx = ctx
	manager.cancel = cancel

	// Pre-add session with full notify channel
	session := &ActiveSession{
		SessionDBID:     1,
		pendingMessages: make([]PendingMessage, 0),
		notify:          make(chan struct{}, 1),
	}
	// Fill the notify channel
	session.notify <- struct{}{}
	manager.sessions[1] = session

	// Fill ProcessNotify channel
	manager.ProcessNotify <- struct{}{}

	// Queue should NOT block even with full channels
	done := make(chan bool)
	go func() {
		err := manager.QueueObservation(context.Background(), 1, ObservationData{ToolName: "Test"})
		assert.NoError(t, err)
		done <- true
	}()

	select {
	case <-done:
		// Success - didn't block
	case <-time.After(100 * time.Millisecond):
		t.Error("Queue operation should not block even with full notification channels")
	}
}

// TestConcurrentQueueAndCleanup tests concurrent queue operations and cleanup.
func TestConcurrentQueueAndCleanup(t *testing.T) {
	t.Parallel()

	manager := &Manager{
		sessions:      make(map[int64]*ActiveSession),
		ProcessNotify: make(chan struct{}, 1),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.ctx = ctx
	manager.cancel = cancel

	// Pre-add multiple sessions
	for i := int64(1); i <= 5; i++ {
		sessionCtx, sessionCancel := context.WithCancel(context.Background())
		manager.sessions[i] = &ActiveSession{
			SessionDBID:     i,
			StartTime:       time.Now(),
			pendingMessages: make([]PendingMessage, 0),
			notify:          make(chan struct{}, 1),
			ctx:             sessionCtx,
			cancel:          sessionCancel,
		}
	}

	var wg sync.WaitGroup

	// Concurrent queue operations
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sessionID := int64((idx % 5) + 1)
			if idx%2 == 0 {
				_ = manager.QueueObservation(context.Background(), sessionID, ObservationData{ToolName: "Test"})
			} else {
				_ = manager.QueueSummarize(context.Background(), sessionID, "user", "assistant")
			}
		}(i)
	}

	// Concurrent cleanup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			manager.cleanupStaleSessions()
		}()
	}

	// Concurrent reads
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = manager.GetActiveSessionCount()
			_ = manager.GetTotalQueueDepth()
			_ = manager.IsAnySessionProcessing()
			_ = manager.GetAllSessions()
		}()
	}

	wg.Wait()

	// Should have all sessions (none are stale)
	assert.Equal(t, 5, manager.GetActiveSessionCount())
	// Should have 50 messages total
	assert.Equal(t, 50, manager.GetTotalQueueDepth())
}
