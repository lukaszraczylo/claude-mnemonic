// Package worker provides the main worker service for claude-mnemonic.
package worker

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
)

// DefaultPatternsLimit is the default number of patterns to return.
const DefaultPatternsLimit = 100

// handleGetPatterns returns all active patterns, optionally filtered by type or project.
func (s *Service) handleGetPatterns(w http.ResponseWriter, r *http.Request) {
	s.initMu.RLock()
	store := s.patternStore
	s.initMu.RUnlock()

	if store == nil {
		http.Error(w, "pattern store not initialized", http.StatusServiceUnavailable)
		return
	}

	// Parse query parameters
	limit := DefaultPatternsLimit
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	patternType := r.URL.Query().Get("type")
	project := r.URL.Query().Get("project")

	var patterns []*models.Pattern
	var err error

	if patternType != "" {
		// Filter by type
		patterns, err = store.GetPatternsByType(r.Context(), models.PatternType(patternType), limit)
	} else if project != "" {
		// Filter by project
		patterns, err = store.GetPatternsByProject(r.Context(), project, limit)
	} else {
		// Get all active patterns
		patterns, err = store.GetActivePatterns(r.Context(), limit)
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, patterns)
}

// handleGetPatternStats returns aggregate statistics about patterns.
func (s *Service) handleGetPatternStats(w http.ResponseWriter, r *http.Request) {
	s.initMu.RLock()
	store := s.patternStore
	s.initMu.RUnlock()

	if store == nil {
		http.Error(w, "pattern store not initialized", http.StatusServiceUnavailable)
		return
	}

	stats, err := store.GetPatternStats(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, stats)
}

// handleGetPatternByID returns a single pattern by ID.
func (s *Service) handleGetPatternByID(w http.ResponseWriter, r *http.Request) {
	s.initMu.RLock()
	store := s.patternStore
	s.initMu.RUnlock()

	if store == nil {
		http.Error(w, "pattern store not initialized", http.StatusServiceUnavailable)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid pattern ID", http.StatusBadRequest)
		return
	}

	pattern, err := store.GetPatternByID(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if pattern == nil {
		http.Error(w, "pattern not found", http.StatusNotFound)
		return
	}

	writeJSON(w, pattern)
}

// handleGetPatternInsight returns a formatted insight string for a pattern.
func (s *Service) handleGetPatternInsight(w http.ResponseWriter, r *http.Request) {
	s.initMu.RLock()
	detector := s.patternDetector
	s.initMu.RUnlock()

	if detector == nil {
		http.Error(w, "pattern detector not initialized", http.StatusServiceUnavailable)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid pattern ID", http.StatusBadRequest)
		return
	}

	insight, err := detector.GetPatternInsight(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]string{"insight": insight})
}

// handleDeletePattern deletes a pattern by ID.
func (s *Service) handleDeletePattern(w http.ResponseWriter, r *http.Request) {
	s.initMu.RLock()
	store := s.patternStore
	s.initMu.RUnlock()

	if store == nil {
		http.Error(w, "pattern store not initialized", http.StatusServiceUnavailable)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid pattern ID", http.StatusBadRequest)
		return
	}

	if err := store.DeletePattern(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]string{"status": "deleted"})
}

// handleDeprecatePattern marks a pattern as deprecated.
func (s *Service) handleDeprecatePattern(w http.ResponseWriter, r *http.Request) {
	s.initMu.RLock()
	store := s.patternStore
	s.initMu.RUnlock()

	if store == nil {
		http.Error(w, "pattern store not initialized", http.StatusServiceUnavailable)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid pattern ID", http.StatusBadRequest)
		return
	}

	if err := store.MarkPatternDeprecated(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]string{"status": "deprecated"})
}

// MergePatternsRequest is the request body for merging patterns.
type MergePatternsRequest struct {
	SourceID int64 `json:"source_id"`
	TargetID int64 `json:"target_id"`
}

// handleSearchPatterns performs full-text search on patterns.
func (s *Service) handleSearchPatterns(w http.ResponseWriter, r *http.Request) {
	s.initMu.RLock()
	store := s.patternStore
	s.initMu.RUnlock()

	if store == nil {
		http.Error(w, "pattern store not initialized", http.StatusServiceUnavailable)
		return
	}

	query := r.URL.Query().Get("q")
	if query == "" {
		http.Error(w, "query parameter 'q' is required", http.StatusBadRequest)
		return
	}

	limit := DefaultPatternsLimit
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	patterns, err := store.SearchPatternsFTS(r.Context(), query, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, patterns)
}

// handleGetPatternByName returns a pattern by its name.
func (s *Service) handleGetPatternByName(w http.ResponseWriter, r *http.Request) {
	s.initMu.RLock()
	store := s.patternStore
	s.initMu.RUnlock()

	if store == nil {
		http.Error(w, "pattern store not initialized", http.StatusServiceUnavailable)
		return
	}

	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "query parameter 'name' is required", http.StatusBadRequest)
		return
	}

	pattern, err := store.GetPatternByName(r.Context(), name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if pattern == nil {
		http.Error(w, "pattern not found", http.StatusNotFound)
		return
	}

	writeJSON(w, pattern)
}

// handleMergePatterns merges a source pattern into a target pattern.
func (s *Service) handleMergePatterns(w http.ResponseWriter, r *http.Request) {
	s.initMu.RLock()
	store := s.patternStore
	s.initMu.RUnlock()

	if store == nil {
		http.Error(w, "pattern store not initialized", http.StatusServiceUnavailable)
		return
	}

	var req MergePatternsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.SourceID == 0 || req.TargetID == 0 {
		http.Error(w, "source_id and target_id are required", http.StatusBadRequest)
		return
	}

	if req.SourceID == req.TargetID {
		http.Error(w, "source_id and target_id cannot be the same", http.StatusBadRequest)
		return
	}

	if err := store.MergePatterns(r.Context(), req.SourceID, req.TargetID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]string{"status": "merged"})
}
