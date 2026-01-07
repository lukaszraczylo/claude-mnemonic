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
