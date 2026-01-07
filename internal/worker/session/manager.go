// Package session provides session lifecycle management for claude-mnemonic.
package session

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lukaszraczylo/claude-mnemonic/internal/db/gorm"
	"github.com/rs/zerolog/log"
)

// MessageType represents the type of pending message.
type MessageType int

const (
	MessageTypeObservation MessageType = iota
	MessageTypeSummarize
)

// ObservationData contains data for a tool observation.
type ObservationData struct {
	ToolInput    interface{}
	ToolResponse interface{}
	ToolName     string
	CWD          string
	PromptNumber int
}

// SummarizeData contains data for a summarize request.
type SummarizeData struct {
	LastUserMessage      string
	LastAssistantMessage string
}

// PendingMessage represents a message queued for SDK processing.
type PendingMessage struct {
	Observation *ObservationData
	Summarize   *SummarizeData
	Type        MessageType
}

// ActiveSession represents an in-memory active session being processed.
type ActiveSession struct {
	StartTime              time.Time
	ctx                    context.Context
	cancel                 context.CancelFunc
	notify                 chan struct{}
	Project                string
	UserPrompt             string
	SDKSessionID           string
	ClaudeSessionID        string
	pendingMessages        []PendingMessage
	LastPromptNumber       int
	CumulativeInputTokens  int64
	CumulativeOutputTokens int64
	SessionDBID            int64
	messageMu              sync.Mutex
	generatorActive        atomic.Bool
}

// SessionTimeout is how long an inactive session can exist before cleanup.
const SessionTimeout = 30 * time.Minute

// CleanupInterval is how often to check for stale sessions.
const CleanupInterval = 5 * time.Minute

// Manager manages active session lifecycles.
type Manager struct {
	ctx           context.Context
	sessionStore  *gorm.SessionStore
	sessions      map[int64]*ActiveSession
	onCreated     func(int64)
	onDeleted     func(int64)
	cancel        context.CancelFunc
	ProcessNotify chan struct{}
	mu            sync.RWMutex
}

// NewManager creates a new session manager.
func NewManager(sessionStore *gorm.SessionStore) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{
		sessionStore:  sessionStore,
		sessions:      make(map[int64]*ActiveSession),
		ctx:           ctx,
		cancel:        cancel,
		ProcessNotify: make(chan struct{}, 1),
	}
	// Start background cleanup goroutine
	go m.cleanupLoop()
	return m
}

// cleanupLoop periodically removes stale sessions.
func (m *Manager) cleanupLoop() {
	ticker := time.NewTicker(CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.cleanupStaleSessions()
		}
	}
}

// cleanupStaleSessions removes sessions that have been inactive too long.
func (m *Manager) cleanupStaleSessions() {
	m.mu.RLock()
	var staleIDs []int64
	now := time.Now()
	for id, session := range m.sessions {
		// Check if session has been inactive for too long
		session.messageMu.Lock()
		hasPending := len(session.pendingMessages) > 0
		session.messageMu.Unlock()

		// Don't delete sessions with pending messages or active processing
		if hasPending || session.generatorActive.Load() {
			continue
		}

		// Delete if session is older than timeout
		if now.Sub(session.StartTime) > SessionTimeout {
			staleIDs = append(staleIDs, id)
		}
	}
	m.mu.RUnlock()

	// Delete stale sessions
	for _, id := range staleIDs {
		log.Info().Int64("sessionId", id).Dur("age", SessionTimeout).Msg("Cleaning up stale session")
		m.DeleteSession(id)
	}
}

// SetOnSessionCreated sets a callback for when a session is created.
func (m *Manager) SetOnSessionCreated(callback func(int64)) {
	m.onCreated = callback
}

// SetOnSessionDeleted sets a callback for when a session is deleted.
func (m *Manager) SetOnSessionDeleted(callback func(int64)) {
	m.onDeleted = callback
}

// InitializeSession initializes a session, creating it if needed.
func (m *Manager) InitializeSession(ctx context.Context, sessionDBID int64, userPrompt string, promptNumber int) (*ActiveSession, error) {
	m.mu.Lock()

	// Check if already active
	if session, ok := m.sessions[sessionDBID]; ok {
		// Update user prompt for continuation
		if userPrompt != "" {
			session.UserPrompt = userPrompt
			session.LastPromptNumber = promptNumber
		}
		m.mu.Unlock()
		return session, nil
	}

	// Fetch from database (unlock during DB call to avoid blocking)
	m.mu.Unlock()
	dbSession, err := m.sessionStore.GetSessionByID(ctx, sessionDBID)
	if err != nil {
		return nil, err
	}
	if dbSession == nil {
		return nil, nil
	}

	// Use provided userPrompt or fall back to database
	prompt := userPrompt
	if prompt == "" && dbSession.UserPrompt.Valid {
		prompt = dbSession.UserPrompt.String
	}

	// Get prompt counter if not provided
	if promptNumber <= 0 {
		promptNumber, _ = m.sessionStore.GetPromptCounter(ctx, sessionDBID)
	}

	// Create session context
	sessionCtx, cancel := context.WithCancel(context.Background())

	session := &ActiveSession{
		SessionDBID:      sessionDBID,
		ClaudeSessionID:  dbSession.ClaudeSessionID,
		SDKSessionID:     dbSession.SDKSessionID.String,
		Project:          dbSession.Project,
		UserPrompt:       prompt,
		LastPromptNumber: promptNumber,
		StartTime:        time.Now(),
		pendingMessages:  make([]PendingMessage, 0, 32),
		notify:           make(chan struct{}, 1),
		ctx:              sessionCtx,
		cancel:           cancel,
	}

	// Re-acquire lock to add session
	m.mu.Lock()
	// Double-check another goroutine didn't create it
	if existing, ok := m.sessions[sessionDBID]; ok {
		m.mu.Unlock()
		cancel() // Clean up unused context
		return existing, nil
	}
	m.sessions[sessionDBID] = session
	onCreated := m.onCreated
	m.mu.Unlock()

	log.Info().
		Int64("sessionId", sessionDBID).
		Str("project", session.Project).
		Str("claudeSessionId", session.ClaudeSessionID).
		Msg("Session initialized")

	// Notify callback (outside lock)
	if onCreated != nil {
		onCreated(sessionDBID)
	}

	return session, nil
}

// QueueObservation queues an observation for SDK processing.
func (m *Manager) QueueObservation(ctx context.Context, sessionDBID int64, data ObservationData) error {
	m.mu.Lock()
	session, ok := m.sessions[sessionDBID]
	if !ok {
		// Auto-initialize from database
		m.mu.Unlock()
		var err error
		session, err = m.InitializeSession(ctx, sessionDBID, "", 0)
		if err != nil || session == nil {
			return err
		}
	} else {
		m.mu.Unlock()
	}

	session.messageMu.Lock()
	session.pendingMessages = append(session.pendingMessages, PendingMessage{
		Type:        MessageTypeObservation,
		Observation: &data,
	})
	queueDepth := len(session.pendingMessages)
	session.messageMu.Unlock()

	// Non-blocking notification to session
	select {
	case session.notify <- struct{}{}:
	default:
	}

	// Non-blocking notification to global processor
	select {
	case m.ProcessNotify <- struct{}{}:
	default:
	}

	log.Info().
		Int64("sessionId", sessionDBID).
		Str("tool", data.ToolName).
		Int("queueDepth", queueDepth).
		Msg("Observation queued")

	return nil
}

// QueueSummarize queues a summarize request for SDK processing.
func (m *Manager) QueueSummarize(ctx context.Context, sessionDBID int64, lastUserMessage, lastAssistantMessage string) error {
	m.mu.Lock()
	session, ok := m.sessions[sessionDBID]
	if !ok {
		// Auto-initialize from database
		m.mu.Unlock()
		var err error
		session, err = m.InitializeSession(ctx, sessionDBID, "", 0)
		if err != nil || session == nil {
			return err
		}
	} else {
		m.mu.Unlock()
	}

	session.messageMu.Lock()
	session.pendingMessages = append(session.pendingMessages, PendingMessage{
		Type: MessageTypeSummarize,
		Summarize: &SummarizeData{
			LastUserMessage:      lastUserMessage,
			LastAssistantMessage: lastAssistantMessage,
		},
	})
	queueDepth := len(session.pendingMessages)
	session.messageMu.Unlock()

	// Non-blocking notification to session
	select {
	case session.notify <- struct{}{}:
	default:
	}

	// Non-blocking notification to global processor
	select {
	case m.ProcessNotify <- struct{}{}:
	default:
	}

	log.Info().
		Int64("sessionId", sessionDBID).
		Int("queueDepth", queueDepth).
		Msg("Summarize request queued")

	return nil
}

// DeleteSession removes a session and cleans up resources.
func (m *Manager) DeleteSession(sessionDBID int64) {
	m.mu.Lock()
	session, ok := m.sessions[sessionDBID]
	if !ok {
		m.mu.Unlock()
		return
	}
	delete(m.sessions, sessionDBID)
	m.mu.Unlock()

	// Cancel context to stop generator
	session.cancel()

	duration := time.Since(session.StartTime)
	log.Info().
		Int64("sessionId", sessionDBID).
		Str("project", session.Project).
		Dur("duration", duration).
		Msg("Session deleted")

	// Trigger callback
	if m.onDeleted != nil {
		m.onDeleted(sessionDBID)
	}
}

// ShutdownAll shuts down all active sessions.
func (m *Manager) ShutdownAll(ctx context.Context) {
	// Stop cleanup goroutine
	m.cancel()

	m.mu.Lock()
	sessionIDs := make([]int64, 0, len(m.sessions))
	for id := range m.sessions {
		sessionIDs = append(sessionIDs, id)
	}
	m.mu.Unlock()

	for _, id := range sessionIDs {
		m.DeleteSession(id)
	}

	log.Info().
		Int("count", len(sessionIDs)).
		Msg("All sessions shut down")
}

// GetActiveSessionCount returns the number of active sessions.
func (m *Manager) GetActiveSessionCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// GetTotalQueueDepth returns the total queue depth across all sessions.
func (m *Manager) GetTotalQueueDepth() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	total := 0
	for _, session := range m.sessions {
		session.messageMu.Lock()
		total += len(session.pendingMessages)
		session.messageMu.Unlock()
	}
	return total
}

// IsAnySessionProcessing returns true if any session is actively processing.
func (m *Manager) IsAnySessionProcessing() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, session := range m.sessions {
		// Check for pending messages
		session.messageMu.Lock()
		hasPending := len(session.pendingMessages) > 0
		session.messageMu.Unlock()
		if hasPending {
			return true
		}

		// Check for active generator
		if session.generatorActive.Load() {
			return true
		}
	}
	return false
}

// GetAllSessions returns a copy of all active sessions.
func (m *Manager) GetAllSessions() []*ActiveSession {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sessions := make([]*ActiveSession, 0, len(m.sessions))
	for _, session := range m.sessions {
		sessions = append(sessions, session)
	}
	return sessions
}

// DrainMessages drains and returns all pending messages for a session.
func (m *Manager) DrainMessages(sessionDBID int64) []PendingMessage {
	m.mu.RLock()
	session, ok := m.sessions[sessionDBID]
	m.mu.RUnlock()

	if !ok {
		return nil
	}

	session.messageMu.Lock()
	messages := make([]PendingMessage, len(session.pendingMessages))
	copy(messages, session.pendingMessages)
	session.pendingMessages = session.pendingMessages[:0]
	session.messageMu.Unlock()

	return messages
}
