// Package worker provides session-related HTTP handlers.
package worker

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/lukaszraczylo/claude-mnemonic/internal/privacy"
	"github.com/lukaszraczylo/claude-mnemonic/internal/worker/session"
	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
	"github.com/rs/zerolog/log"
)

// SessionInitRequest is the request body for session initialization.
type SessionInitRequest struct {
	ClaudeSessionID     string `json:"claudeSessionId"`
	Project             string `json:"project"`
	Prompt              string `json:"prompt"`
	MatchedObservations int    `json:"matchedObservations"`
}

// SessionInitResponse is the response for session initialization.
type SessionInitResponse struct {
	Reason       string `json:"reason,omitempty"`
	SessionDBID  int64  `json:"sessionDbId"`
	PromptNumber int    `json:"promptNumber"`
	Skipped      bool   `json:"skipped,omitempty"`
}

// DuplicatePromptWindowSeconds is the time window for detecting duplicate prompt submissions.
// If the same prompt text is seen within this window, it's considered a duplicate hook invocation.
const DuplicatePromptWindowSeconds = 10

// handleSessionInit handles session initialization from user-prompt hook.
// This handler is idempotent - duplicate requests within a short time window
// return the existing prompt data without creating duplicates.
func (s *Service) handleSessionInit(w http.ResponseWriter, r *http.Request) {
	var req SessionInitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Privacy check
	if privacy.IsEntirelyPrivate(req.Prompt) {
		// Create session but skip processing
		sessionID, _ := s.sessionStore.CreateSDKSession(r.Context(), req.ClaudeSessionID, req.Project, "")
		promptNum, _ := s.sessionStore.IncrementPromptCounter(r.Context(), sessionID)

		writeJSON(w, SessionInitResponse{
			SessionDBID:  sessionID,
			PromptNumber: promptNum,
			Skipped:      true,
			Reason:       "private",
		})
		return
	}

	// Clean prompt
	cleanedPrompt := privacy.Clean(req.Prompt)

	// DUPLICATE DETECTION: Check if this exact prompt was already saved recently.
	// This prevents the bug where the hook fires multiple times for the same user action,
	// creating many duplicate prompts with incrementing numbers.
	if existingID, existingNum, found := s.promptStore.FindRecentPromptByText(r.Context(), req.ClaudeSessionID, cleanedPrompt, DuplicatePromptWindowSeconds); found {
		// Get or create session (idempotent)
		sessionID, _ := s.sessionStore.CreateSDKSession(r.Context(), req.ClaudeSessionID, req.Project, cleanedPrompt)

		log.Debug().
			Int64("sessionId", sessionID).
			Int("promptNumber", existingNum).
			Int64("promptId", existingID).
			Msg("Duplicate prompt detected - returning existing")

		// Return existing prompt data without incrementing or saving again
		writeJSON(w, SessionInitResponse{
			SessionDBID:  sessionID,
			PromptNumber: existingNum,
		})
		return
	}

	// Create session (idempotent)
	sessionID, err := s.sessionStore.CreateSDKSession(r.Context(), req.ClaudeSessionID, req.Project, cleanedPrompt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Increment prompt counter
	promptNum, err := s.sessionStore.IncrementPromptCounter(r.Context(), sessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Save user prompt with matched observation count
	promptID, err := s.promptStore.SaveUserPromptWithMatches(r.Context(), req.ClaudeSessionID, promptNum, cleanedPrompt, req.MatchedObservations)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to save user prompt")
		// Non-fatal: continue with session initialization
	} else if s.vectorSync != nil {
		// Sync to vector DB asynchronously (non-blocking)
		now := time.Now()
		promptWithSession := &models.UserPromptWithSession{
			UserPrompt: models.UserPrompt{
				ID:                  promptID,
				ClaudeSessionID:     req.ClaudeSessionID,
				PromptNumber:        promptNum,
				PromptText:          cleanedPrompt,
				MatchedObservations: req.MatchedObservations,
				CreatedAt:           now.Format(time.RFC3339),
				CreatedAtEpoch:      now.UnixMilli(),
			},
			Project:      req.Project,
			SDKSessionID: req.ClaudeSessionID,
		}
		s.asyncVectorSync(func() {
			// Use service context as parent to respect shutdown signals
			ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
			defer cancel()
			if err := s.vectorSync.SyncUserPrompt(ctx, promptWithSession); err != nil {
				if s.ctx.Err() == nil { // Don't log during shutdown
					log.Warn().Err(err).Int64("id", promptID).Msg("Failed to sync user prompt to sqlite-vec")
				}
			}
		})
	}

	log.Info().
		Int64("sessionId", sessionID).
		Int("promptNumber", promptNum).
		Str("project", req.Project).
		Msg("Session initialized")

	// Broadcast prompt event for dashboard refresh
	s.sseBroadcaster.Broadcast(map[string]any{
		"type":    "prompt",
		"action":  "created",
		"project": req.Project,
	})

	writeJSON(w, SessionInitResponse{
		SessionDBID:  sessionID,
		PromptNumber: promptNum,
	})
}

// SessionStartRequest is the request body for starting SDK agent.
type SessionStartRequest struct {
	UserPrompt   string `json:"userPrompt"`
	PromptNumber int    `json:"promptNumber"`
}

// handleSessionStart handles SDK agent session start.
func (s *Service) handleSessionStart(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid session id", http.StatusBadRequest)
		return
	}

	var req SessionStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Initialize session in manager
	sess, err := s.sessionManager.InitializeSession(r.Context(), id, req.UserPrompt, req.PromptNumber)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if sess == nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	// Session is now registered. Observations will be processed
	// asynchronously by the background queue processor (processQueue in service.go).
	log.Info().
		Int64("sessionId", id).
		Int("promptNumber", req.PromptNumber).
		Msg("SDK agent session initialized")

	s.broadcastProcessingStatus()
	w.WriteHeader(http.StatusOK)
}

// ObservationRequest is the request body for posting observations.
type ObservationRequest struct {
	ClaudeSessionID string `json:"claudeSessionId"`
	Project         string `json:"project"`
	ToolName        string `json:"tool_name"`
	ToolInput       any    `json:"tool_input"`
	ToolResponse    any    `json:"tool_response"`
	CWD             string `json:"cwd"`
}

// handleObservation handles observation posting from post-tool-use hook.
func (s *Service) handleObservation(w http.ResponseWriter, r *http.Request) {
	var req ObservationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Find session
	sess, err := s.sessionStore.FindAnySDKSession(r.Context(), req.ClaudeSessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if sess == nil {
		// Create session on-the-fly with project from request
		id, err := s.sessionStore.CreateSDKSession(r.Context(), req.ClaudeSessionID, req.Project, "")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		sess, _ = s.sessionStore.GetSessionByID(r.Context(), id)
	}

	// Queue observation
	if err := s.sessionManager.QueueObservation(r.Context(), sess.ID, session.ObservationData{
		ToolName:     req.ToolName,
		ToolInput:    req.ToolInput,
		ToolResponse: req.ToolResponse,
		CWD:          req.CWD,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.broadcastProcessingStatus()
	w.WriteHeader(http.StatusOK)
}

// SubagentCompleteRequest is the request body for subagent completion.
type SubagentCompleteRequest struct {
	ClaudeSessionID string `json:"claudeSessionId"`
	Project         string `json:"project"`
}

// handleSubagentComplete handles subagent/Task completion notifications.
// This triggers immediate processing of any queued observations from the subagent.
func (s *Service) handleSubagentComplete(w http.ResponseWriter, r *http.Request) {
	var req SubagentCompleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Find session
	sess, err := s.sessionStore.FindAnySDKSession(r.Context(), req.ClaudeSessionID)
	if err != nil || sess == nil {
		// Session not found - subagent may have been in a different context
		log.Debug().
			Str("claudeSessionId", req.ClaudeSessionID).
			Msg("Subagent complete - no active session found")
		w.WriteHeader(http.StatusOK)
		return
	}

	// Trigger immediate processing of queued observations
	messages := s.sessionManager.DrainMessages(sess.ID)
	if len(messages) > 0 && s.processor != nil {
		log.Info().
			Int64("sessionId", sess.ID).
			Int("messages", len(messages)).
			Msg("Processing queued observations from subagent")

		for _, msg := range messages {
			if msg.Type == session.MessageTypeObservation && msg.Observation != nil {
				err := s.processor.ProcessObservation(
					r.Context(),
					sess.SDKSessionID.String,
					sess.Project,
					msg.Observation.ToolName,
					msg.Observation.ToolInput,
					msg.Observation.ToolResponse,
					msg.Observation.PromptNumber,
					msg.Observation.CWD,
				)
				if err != nil {
					log.Error().Err(err).
						Str("tool", msg.Observation.ToolName).
						Msg("Failed to process subagent observation")
				}
			}
		}
	}

	s.broadcastProcessingStatus()
	w.WriteHeader(http.StatusOK)
}

// handleGetSessionByClaudeID looks up a session by Claude session ID.
func (s *Service) handleGetSessionByClaudeID(w http.ResponseWriter, r *http.Request) {
	claudeSessionID := r.URL.Query().Get("claudeSessionId")
	if claudeSessionID == "" {
		http.Error(w, "claudeSessionId required", http.StatusBadRequest)
		return
	}

	session, err := s.sessionStore.FindAnySDKSession(r.Context(), claudeSessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if session == nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	writeJSON(w, session)
}

// SummarizeRequest is the request body for summarize requests.
type SummarizeRequest struct {
	LastUserMessage      string `json:"lastUserMessage"`
	LastAssistantMessage string `json:"lastAssistantMessage"`
}

// handleSummarize handles summarize requests from stop hook.
func (s *Service) handleSummarize(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid session id", http.StatusBadRequest)
		return
	}

	var req SummarizeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Queue summarize request
	if err := s.sessionManager.QueueSummarize(r.Context(), id, req.LastUserMessage, req.LastAssistantMessage); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.broadcastProcessingStatus()
	w.WriteHeader(http.StatusOK)
}
