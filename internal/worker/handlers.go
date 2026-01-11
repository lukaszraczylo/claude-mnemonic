// Package worker provides the main worker service for claude-mnemonic.
// This file contains shared handler utilities and health/status endpoints.
// Domain-specific handlers are split into:
//   - handlers_sessions.go: Session lifecycle (init, start, observation, summarize)
//   - handlers_context.go: Context/search (search by prompt, file context, inject)
//   - handlers_data.go: Data retrieval (observations, summaries, prompts, stats)
//   - handlers_update.go: Updates and self-check (update check/apply, self-check)
//   - handlers_import_export.go: Import/export/archive operations
package worker

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/rs/zerolog/log"
)

// Handler configuration constants
const (
	// DefaultObservationsLimit is the default number of observations to return.
	DefaultObservationsLimit = 100

	// DefaultSummariesLimit is the default number of summaries to return.
	DefaultSummariesLimit = 50

	// DefaultPromptsLimit is the default number of prompts to return.
	DefaultPromptsLimit = 100

	// DefaultSearchLimit is the default number of search results to return.
	DefaultSearchLimit = 50

	// DefaultContextLimit is the default number of context observations to return.
	DefaultContextLimit = 50
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

// observationTypeSet is a pre-computed map for O(1) type validation.
// Initialized at package load time.
var observationTypeSet = func() map[string]struct{} {
	m := make(map[string]struct{}, len(ObservationTypes))
	for _, t := range ObservationTypes {
		m[t] = struct{}{}
	}
	return m
}()

// IsValidObservationType returns true if the type is valid (O(1) lookup).
func IsValidObservationType(t string) bool {
	_, ok := observationTypeSet[t]
	return ok
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

// conceptTypeSet is a pre-computed map for O(1) concept validation.
var conceptTypeSet = func() map[string]struct{} {
	m := make(map[string]struct{}, len(ConceptTypes))
	for _, t := range ConceptTypes {
		m[t] = struct{}{}
	}
	return m
}()

// IsValidConceptType returns true if the concept type is valid (O(1) lookup).
func IsValidConceptType(t string) bool {
	_, ok := conceptTypeSet[t]
	return ok
}

// writeJSON writes a JSON response with proper error handling.
func writeJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Error().Err(err).Msg("Failed to encode JSON response")
	}
}

// parseIDParam parses an ID parameter from a string.
// Returns the parsed ID and true on success, or writes an error response and returns false.
// The entityName is used in error messages (e.g., "observation", "session", "pattern").
func parseIDParam(w http.ResponseWriter, idStr, entityName string) (int64, bool) {
	if idStr == "" {
		http.Error(w, entityName+" id required", http.StatusBadRequest)
		return 0, false
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid "+entityName+" id", http.StatusBadRequest)
		return 0, false
	}

	return id, true
}

// formatWarning formats a warning message for use in health responses.
func formatWarning(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
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
	writeJSON(w, map[string]any{
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

// handleRebuildStatus returns the current status of vector rebuild operations.
// This provides visibility into long-running rebuild operations.
func (s *Service) handleRebuildStatus(w http.ResponseWriter, _ *http.Request) {
	s.rebuildStatusMu.RLock()
	status := s.rebuildStatus
	s.rebuildStatusMu.RUnlock()

	if status == nil {
		writeJSON(w, map[string]any{
			"in_progress": false,
			"message":     "No rebuild operation has been started",
		})
		return
	}

	writeJSON(w, status)
}

// handleTriggerVectorRebuild triggers a full vector rebuild operation.
// This rebuilds all vectors from observations, summaries, and prompts.
// Returns 409 Conflict if a rebuild is already in progress.
// Returns 429 Too Many Requests if called too frequently (5 minute cooldown).
func (s *Service) handleTriggerVectorRebuild(w http.ResponseWriter, _ *http.Request) {
	// Check rate limiting for expensive operations
	if s.expensiveOpLimiter != nil && !s.expensiveOpLimiter.CanRebuild() {
		http.Error(w, "rebuild requested too recently, please wait 5 minutes", http.StatusTooManyRequests)
		return
	}

	// Check if rebuild is already in progress
	s.rebuildStatusMu.RLock()
	if s.rebuildStatus != nil && s.rebuildStatus.InProgress {
		s.rebuildStatusMu.RUnlock()
		http.Error(w, "rebuild already in progress", http.StatusConflict)
		return
	}
	s.rebuildStatusMu.RUnlock()

	// Verify we have the necessary components
	if s.vectorSync == nil || s.observationStore == nil || s.summaryStore == nil || s.promptStore == nil {
		http.Error(w, "vector sync not initialized", http.StatusServiceUnavailable)
		return
	}

	// Start rebuild in background
	s.wg.Add(1)
	go s.rebuildAllVectors(s.observationStore, s.summaryStore, s.promptStore, s.vectorSync)

	writeJSON(w, map[string]any{
		"status":  "started",
		"message": "Vector rebuild started. Check /api/rebuild-status for progress.",
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
