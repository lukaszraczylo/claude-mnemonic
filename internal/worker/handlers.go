// Package worker provides the main worker service for claude-mnemonic.
package worker

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/lukaszraczylo/claude-mnemonic/internal/db/sqlite"
	"github.com/lukaszraczylo/claude-mnemonic/internal/privacy"
	"github.com/lukaszraczylo/claude-mnemonic/internal/vector/sqlitevec"
	"github.com/lukaszraczylo/claude-mnemonic/internal/worker/sdk"
	"github.com/lukaszraczylo/claude-mnemonic/internal/worker/session"
	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
	"github.com/rs/zerolog/log"
)

// Handler configuration constants
const (
	// DefaultObservationsLimit is the default number of observations to return.
	DefaultObservationsLimit = 100

	// DefaultSummariesLimit is the default number of summaries to return.
)

// ObservationTypes is the canonical list of observation types.
// Used by both Go backend and served to frontend.
var ObservationTypes = []string{
	"bugfix",
	"feature",
	"refactor",
	"discovery",
	"decision",
	"change",
}

// ConceptTypes is the canonical list of valid concept types.
// Used by both Go backend and served to frontend.
var ConceptTypes = []string{
	// Semantic concepts
	"how-it-works",
	"why-it-exists",
	"what-changed",
	"problem-solution",
	"gotcha",
	"pattern",
	"trade-off",
	// Globalizable concepts (from models.GlobalizableConcepts)
	"best-practice",
	"anti-pattern",
	"architecture",
	"security",
	"performance",
	"testing",
	"debugging",
	"workflow",
	"tooling",
	// Additional useful concepts
	"refactoring",
	"api",
	"database",
	"configuration",
	"error-handling",
	"caching",
	"logging",
	"auth",
	"validation",
}

const (
	DefaultSummariesLimit = 50

	// DefaultPromptsLimit is the default number of prompts to return.
	DefaultPromptsLimit = 100

	// DefaultSearchLimit is the default number of search results to return.
	DefaultSearchLimit = 50

	// DefaultContextLimit is the default number of context observations to return.
	DefaultContextLimit = 50
)

// writeJSON writes a JSON response with proper error handling.
func writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Error().Err(err).Msg("Failed to encode JSON response")
	}
}

// handleHealth handles health check requests.
// Returns 200 OK immediately (even during init) so hooks can connect quickly.
// Use /api/ready for full readiness check.
func (s *Service) handleHealth(w http.ResponseWriter, r *http.Request) {
	status := "starting"
	if s.ready.Load() {
		status = "ready"
	} else if err := s.GetInitError(); err != nil {
		status = "error"
	}
	writeJSON(w, map[string]interface{}{
		"status":  status,
		"version": s.version,
	})
}

// handleVersion returns the worker version for version checking.
func (s *Service) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{
		"version": s.version,
	})
}

// handleReady handles readiness check requests.
// Returns 200 only when fully initialized, 503 otherwise.
func (s *Service) handleReady(w http.ResponseWriter, r *http.Request) {
	if !s.ready.Load() {
		if err := s.GetInitError(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Error(w, "service initializing", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, map[string]string{"status": "ready"})
}

// requireReady is middleware that returns 503 if service isn't ready.
func (s *Service) requireReady(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.ready.Load() {
			if err := s.GetInitError(); err != nil {
				http.Error(w, "service initialization failed: "+err.Error(), http.StatusInternalServerError)
				return
			}
			http.Error(w, "service initializing", http.StatusServiceUnavailable)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// SessionInitRequest is the request body for session initialization.
type SessionInitRequest struct {
	ClaudeSessionID     string `json:"claudeSessionId"`
	Project             string `json:"project"`
	Prompt              string `json:"prompt"`
	MatchedObservations int    `json:"matchedObservations"`
}

// SessionInitResponse is the response for session initialization.
type SessionInitResponse struct {
	SessionDBID  int64  `json:"sessionDbId"`
	PromptNumber int    `json:"promptNumber"`
	Skipped      bool   `json:"skipped,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

// handleSessionInit handles session initialization from user-prompt hook.
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

	// Clean prompt and create session
	cleanedPrompt := privacy.Clean(req.Prompt)
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
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := s.vectorSync.SyncUserPrompt(ctx, promptWithSession); err != nil {
				log.Warn().Err(err).Int64("id", promptID).Msg("Failed to sync user prompt to sqlite-vec")
			}
		}()
	}

	log.Info().
		Int64("sessionId", sessionID).
		Int("promptNumber", promptNum).
		Str("project", req.Project).
		Msg("Session initialized")

	// Broadcast prompt event for dashboard refresh
	s.sseBroadcaster.Broadcast(map[string]interface{}{
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
	ClaudeSessionID string      `json:"claudeSessionId"`
	Project         string      `json:"project"`
	ToolName        string      `json:"tool_name"`
	ToolInput       interface{} `json:"tool_input"`
	ToolResponse    interface{} `json:"tool_response"`
	CWD             string      `json:"cwd"`
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

// handleGetObservations returns recent observations.
// Supports optional query parameter for semantic search via sqlite-vec.
func (s *Service) handleGetObservations(w http.ResponseWriter, r *http.Request) {
	limit := sqlite.ParseLimitParam(r, DefaultObservationsLimit)
	project := r.URL.Query().Get("project")
	query := r.URL.Query().Get("query")

	var observations []*models.Observation
	var err error
	var usedVector bool

	// Use vector search if query is provided and vector client is available
	if query != "" && s.vectorClient != nil && s.vectorClient.IsConnected() {
		where := sqlitevec.BuildWhereFilter(sqlitevec.DocTypeObservation, "")
		vectorResults, vecErr := s.vectorClient.Query(r.Context(), query, limit*2, where)
		if vecErr == nil && len(vectorResults) > 0 {
			obsIDs := sqlitevec.ExtractObservationIDs(vectorResults, project)
			if len(obsIDs) > 0 {
				observations, err = s.observationStore.GetObservationsByIDs(r.Context(), obsIDs, "date_desc", limit)
				if err == nil {
					usedVector = true
				}
			}
		}
	}

	// Fall back to SQLite if vector search not used
	if !usedVector {
		if project != "" {
			// Filter by project - includes project-scoped and global observations
			observations, err = s.observationStore.GetRecentObservations(r.Context(), project, limit)
		} else {
			// All projects
			observations, err = s.observationStore.GetAllRecentObservations(r.Context(), limit)
		}
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Ensure we return empty array, not null
	if observations == nil {
		observations = []*models.Observation{}
	}
	writeJSON(w, observations)
}

// handleGetSummaries returns recent summaries.
// Supports optional query parameter for semantic search via sqlite-vec.
func (s *Service) handleGetSummaries(w http.ResponseWriter, r *http.Request) {
	limit := sqlite.ParseLimitParam(r, DefaultSummariesLimit)
	project := r.URL.Query().Get("project")
	query := r.URL.Query().Get("query")

	var summaries []*models.SessionSummary
	var err error
	var usedVector bool

	// Use vector search if query is provided and vector client is available
	if query != "" && s.vectorClient != nil && s.vectorClient.IsConnected() {
		where := sqlitevec.BuildWhereFilter(sqlitevec.DocTypeSessionSummary, "")
		vectorResults, vecErr := s.vectorClient.Query(r.Context(), query, limit*2, where)
		if vecErr == nil && len(vectorResults) > 0 {
			summaryIDs := sqlitevec.ExtractSummaryIDs(vectorResults, project)
			if len(summaryIDs) > 0 {
				summaries, err = s.summaryStore.GetSummariesByIDs(r.Context(), summaryIDs, "date_desc", limit)
				if err == nil {
					usedVector = true
				}
			}
		}
	}

	// Fall back to SQLite if vector search not used
	if !usedVector {
		if project != "" {
			summaries, err = s.summaryStore.GetRecentSummaries(r.Context(), project, limit)
		} else {
			summaries, err = s.summaryStore.GetAllRecentSummaries(r.Context(), limit)
		}
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Ensure we return empty array, not null
	if summaries == nil {
		summaries = []*models.SessionSummary{}
	}
	writeJSON(w, summaries)
}

// handleGetPrompts returns recent user prompts.
// Supports optional query parameter for semantic search via sqlite-vec.
func (s *Service) handleGetPrompts(w http.ResponseWriter, r *http.Request) {
	limit := sqlite.ParseLimitParam(r, DefaultPromptsLimit)
	project := r.URL.Query().Get("project")
	query := r.URL.Query().Get("query")

	var prompts []*models.UserPromptWithSession
	var err error
	var usedVector bool

	// Use vector search if query is provided and vector client is available
	if query != "" && s.vectorClient != nil && s.vectorClient.IsConnected() {
		where := sqlitevec.BuildWhereFilter(sqlitevec.DocTypeUserPrompt, "")
		vectorResults, vecErr := s.vectorClient.Query(r.Context(), query, limit*2, where)
		if vecErr == nil && len(vectorResults) > 0 {
			promptIDs := sqlitevec.ExtractPromptIDs(vectorResults, project)
			if len(promptIDs) > 0 {
				prompts, err = s.promptStore.GetPromptsByIDs(r.Context(), promptIDs, "date_desc", limit)
				if err == nil {
					usedVector = true
				}
			}
		}
	}

	// Fall back to SQLite if vector search not used
	if !usedVector {
		if project != "" {
			prompts, err = s.promptStore.GetRecentUserPromptsByProject(r.Context(), project, limit)
		} else {
			prompts, err = s.promptStore.GetAllRecentUserPrompts(r.Context(), limit)
		}
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Ensure we return empty array, not null
	if prompts == nil {
		prompts = []*models.UserPromptWithSession{}
	}
	writeJSON(w, prompts)
}

// handleGetProjects returns all projects.
func (s *Service) handleGetProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := s.sessionStore.GetAllProjects(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, projects)
}

// handleGetTypes returns the canonical list of observation and concept types.
// This provides a single source of truth for both backend and frontend.
func (s *Service) handleGetTypes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{
		"observation_types": ObservationTypes,
		"concept_types":     ConceptTypes,
	})
}

// handleGetStats returns worker statistics.
func (s *Service) handleGetStats(w http.ResponseWriter, r *http.Request) {
	retrievalStats := s.GetRetrievalStats()
	sessionsToday, _ := s.sessionStore.GetSessionsToday(r.Context())

	response := map[string]interface{}{
		"uptime":           time.Since(s.startTime).String(),
		"activeSessions":   s.sessionManager.GetActiveSessionCount(),
		"queueDepth":       s.sessionManager.GetTotalQueueDepth(),
		"isProcessing":     s.sessionManager.IsAnySessionProcessing(),
		"connectedClients": s.sseBroadcaster.ClientCount(),
		"sessionsToday":    sessionsToday,
		"retrieval":        retrievalStats,
		"ready":            s.ready.Load(),
	}

	// Include project-specific observation count if project is specified
	if project := r.URL.Query().Get("project"); project != "" {
		count, err := s.observationStore.GetObservationCount(r.Context(), project)
		if err == nil {
			response["projectObservations"] = count
			response["project"] = project
		}
	}

	writeJSON(w, response)
}

// handleGetRetrievalStats returns detailed retrieval statistics.
func (s *Service) handleGetRetrievalStats(w http.ResponseWriter, r *http.Request) {
	stats := s.GetRetrievalStats()
	writeJSON(w, stats)
}

// handleContextCount returns the count of observations for a project.
func (s *Service) handleContextCount(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	if project == "" {
		http.Error(w, "project required", http.StatusBadRequest)
		return
	}

	count, err := s.observationStore.GetObservationCount(r.Context(), project)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]interface{}{
		"project": project,
		"count":   count,
	})
}

// handleSearchByPrompt searches observations relevant to a user prompt.
// IMPORTANT: This is on the critical startup path - must be fast!
// No synchronous verification - just filter by staleness and return.
func (s *Service) handleSearchByPrompt(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	query := r.URL.Query().Get("query")
	cwd := r.URL.Query().Get("cwd")

	if project == "" || query == "" {
		http.Error(w, "project and query required", http.StatusBadRequest)
		return
	}

	limit := sqlite.ParseLimitParam(r, DefaultSearchLimit)

	var observations []*models.Observation
	var err error
	var usedVector bool

	// Try vector search first if available
	if s.vectorClient != nil && s.vectorClient.IsConnected() {
		where := sqlitevec.BuildWhereFilter(sqlitevec.DocTypeObservation, "")

		vectorResults, vecErr := s.vectorClient.Query(r.Context(), query, limit*2, where)
		if vecErr == nil && len(vectorResults) > 0 {
			// Extract observation IDs with project/scope filtering using shared helper
			obsIDs := sqlitevec.ExtractObservationIDs(vectorResults, project)

			if len(obsIDs) > 0 {
				// Fetch full observations from SQLite
				observations, err = s.observationStore.GetObservationsByIDs(r.Context(), obsIDs, "date_desc", limit)
				if err == nil {
					usedVector = true
				}
			}
		}
	}

	// Fall back to FTS if vector search not available or returned no results
	if !usedVector || len(observations) == 0 {
		observations, err = s.observationStore.SearchObservationsFTS(r.Context(), query, project, limit)
		if err != nil {
			// FTS might fail if query has special chars, try without
			log.Warn().Err(err).Str("query", query).Msg("FTS search failed, falling back to recent")
			observations, err = s.observationStore.GetRecentObservations(r.Context(), project, limit)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
	}

	// Fast staleness filter - NO verification (that's too slow for interactive use)
	// Just check mtimes and exclude obviously stale observations
	var staleCount int
	freshObservations := make([]*models.Observation, 0, len(observations))

	for _, obs := range observations {
		if len(obs.FileMtimes) > 0 && cwd != "" {
			var paths []string
			for path := range obs.FileMtimes {
				paths = append(paths, path)
			}
			currentMtimes := sdk.GetFileMtimes(paths, cwd)

			if obs.CheckStaleness(currentMtimes) {
				// Stale - exclude but don't verify (too slow)
				// Queue for background verification instead
				staleCount++
				s.queueStaleVerification(obs.ID, cwd)
				continue
			}
		}
		freshObservations = append(freshObservations, obs)
	}

	// Cluster similar observations to remove duplicates
	clusteredObservations := clusterObservations(freshObservations, 0.4)

	// Record retrieval stats (no verification done, so verified=0, deleted=0)
	s.recordRetrievalStats(int64(len(clusteredObservations)), 0, 0, true)

	log.Info().
		Str("project", project).
		Str("query", query).
		Int("found", len(clusteredObservations)).
		Int("stale_excluded", staleCount).
		Msg("Prompt-based observation search")

	writeJSON(w, map[string]interface{}{
		"project":      project,
		"query":        query,
		"observations": clusteredObservations,
	})
}

// handleContextInject returns context for injection at session start.
// IMPORTANT: This is on the critical startup path - must be fast!
// No synchronous verification - just filter by staleness and return.
func (s *Service) handleContextInject(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	if project == "" {
		http.Error(w, "project required", http.StatusBadRequest)
		return
	}

	cwd := r.URL.Query().Get("cwd")
	if cwd == "" {
		cwd = "/"
	}

	// Limit observations for fast startup (configurable, default 100)
	limit := s.config.ContextObservations
	if limit <= 0 {
		limit = DefaultContextLimit
	}

	// Full count determines how many observations get full detail (configurable, default 25)
	fullCount := s.config.ContextFullCount
	if fullCount <= 0 {
		fullCount = 25
	}

	// Get recent observations
	observations, err := s.observationStore.GetRecentObservations(r.Context(), project, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Fast staleness filter - NO verification (that's too slow for startup)
	var staleCount int
	freshObservations := make([]*models.Observation, 0, len(observations))

	for _, obs := range observations {
		if len(obs.FileMtimes) > 0 {
			var paths []string
			for path := range obs.FileMtimes {
				paths = append(paths, path)
			}
			currentMtimes := sdk.GetFileMtimes(paths, cwd)

			if obs.CheckStaleness(currentMtimes) {
				// Stale - exclude but don't verify (too slow)
				// Queue for background verification instead
				staleCount++
				s.queueStaleVerification(obs.ID, cwd)
				continue
			}
		}
		freshObservations = append(freshObservations, obs)
	}

	// Cluster similar observations to remove duplicates
	clusteredObservations := clusterObservations(freshObservations, 0.4)
	duplicatesRemoved := len(freshObservations) - len(clusteredObservations)

	// Record retrieval stats (no verification done)
	s.recordRetrievalStats(int64(len(clusteredObservations)), 0, 0, false)

	log.Info().
		Str("project", project).
		Int("total", len(observations)).
		Int("fresh", len(freshObservations)).
		Int("clustered", len(clusteredObservations)).
		Int("duplicates", duplicatesRemoved).
		Int("stale_excluded", staleCount).
		Msg("Context injection with clustering")

	writeJSON(w, map[string]interface{}{
		"project":            project,
		"observations":       clusteredObservations,
		"full_count":         fullCount,
		"stale_excluded":     staleCount,
		"duplicates_removed": duplicatesRemoved,
	})
}

// handleUpdateCheck checks for available updates.
func (s *Service) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	info, err := s.updater.CheckForUpdate(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, info)
}

// handleUpdateApply downloads and applies an available update.
func (s *Service) handleUpdateApply(w http.ResponseWriter, r *http.Request) {
	// First check for update
	info, err := s.updater.CheckForUpdate(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if !info.Available {
		writeJSON(w, map[string]interface{}{
			"success": false,
			"message": "No update available",
		})
		return
	}

	// Apply update in background
	go func() {
		if err := s.updater.ApplyUpdate(s.ctx, info); err != nil {
			log.Error().Err(err).Msg("Update failed")
		}
	}()

	writeJSON(w, map[string]interface{}{
		"success": true,
		"message": "Update started",
		"version": info.LatestVersion,
	})
}

// handleUpdateStatus returns the current update status.
func (s *Service) handleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	status := s.updater.GetStatus()
	writeJSON(w, status)
}

// ComponentHealth represents the health status of a single component.
type ComponentHealth struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // "healthy", "degraded", "unhealthy"
	Message string `json:"message,omitempty"`
}

// SelfCheckResponse contains the health status of all components.
type SelfCheckResponse struct {
	Overall    string            `json:"overall"` // "healthy", "degraded", "unhealthy"
	Version    string            `json:"version"`
	Uptime     string            `json:"uptime"`
	Components []ComponentHealth `json:"components"`
}

// handleSelfCheck returns the health status of all components.
func (s *Service) handleSelfCheck(w http.ResponseWriter, r *http.Request) {
	components := []ComponentHealth{}
	overall := "healthy"

	// Check Worker Service
	workerStatus := ComponentHealth{Name: "Worker Service", Status: "healthy"}
	if !s.ready.Load() {
		if err := s.GetInitError(); err != nil {
			workerStatus.Status = "unhealthy"
			workerStatus.Message = err.Error()
			overall = "unhealthy"
		} else {
			workerStatus.Status = "degraded"
			workerStatus.Message = "Initializing"
			if overall == "healthy" {
				overall = "degraded"
			}
		}
	}
	components = append(components, workerStatus)

	// Check SQLite Database
	dbStatus := ComponentHealth{Name: "SQLite Database", Status: "healthy"}
	if s.store == nil {
		dbStatus.Status = "unhealthy"
		dbStatus.Message = "Not initialized"
		overall = "unhealthy"
	} else if err := s.store.Ping(); err != nil {
		dbStatus.Status = "unhealthy"
		dbStatus.Message = err.Error()
		overall = "unhealthy"
	}
	components = append(components, dbStatus)

	// Check Vector DB (sqlite-vec)
	vectorStatus := ComponentHealth{Name: "Vector DB", Status: "healthy"}
	if s.vectorClient == nil {
		vectorStatus.Status = "degraded"
		vectorStatus.Message = "Not configured"
		if overall == "healthy" {
			overall = "degraded"
		}
	} else if !s.vectorClient.IsConnected() {
		vectorStatus.Status = "degraded"
		vectorStatus.Message = "Not connected"
		if overall == "healthy" {
			overall = "degraded"
		}
	}
	components = append(components, vectorStatus)

	// Check SDK Processor
	sdkStatus := ComponentHealth{Name: "SDK Processor", Status: "healthy"}
	if s.processor == nil {
		sdkStatus.Status = "degraded"
		sdkStatus.Message = "Not initialized"
		if overall == "healthy" {
			overall = "degraded"
		}
	} else if !s.processor.IsAvailable() {
		sdkStatus.Status = "degraded"
		sdkStatus.Message = "Claude CLI not available"
		if overall == "healthy" {
			overall = "degraded"
		}
	}
	components = append(components, sdkStatus)

	// Check SSE Broadcaster
	sseStatus := ComponentHealth{Name: "SSE Broadcaster", Status: "healthy"}
	if s.sseBroadcaster == nil {
		sseStatus.Status = "unhealthy"
		sseStatus.Message = "Not initialized"
		overall = "unhealthy"
	}
	components = append(components, sseStatus)

	// Calculate uptime
	uptime := time.Since(s.startTime).Round(time.Second).String()

	writeJSON(w, SelfCheckResponse{
		Overall:    overall,
		Version:    s.version,
		Uptime:     uptime,
		Components: components,
	})
}

// handleUpdateRestart restarts the worker with the new binary.
func (s *Service) handleUpdateRestart(w http.ResponseWriter, r *http.Request) {
	status := s.updater.GetStatus()
	if status.State != "done" {
		http.Error(w, "no update has been applied", http.StatusBadRequest)
		return
	}

	// Send response before restarting
	writeJSON(w, map[string]interface{}{
		"success": true,
		"message": "Restarting worker...",
	})

	// Flush the response
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	// Restart in background after response is sent
	go func() {
		if err := s.updater.Restart(); err != nil {
			log.Error().Err(err).Msg("Failed to restart worker")
		}
	}()
}
