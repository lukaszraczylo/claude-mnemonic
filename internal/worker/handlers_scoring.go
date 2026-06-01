// Package worker provides the main worker service for claude-mnemonic.
package worker

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
	"github.com/rs/zerolog/log"
)

// FeedbackRequest represents a user feedback submission.
type FeedbackRequest struct {
	Feedback int `json:"feedback"` // -1 (thumbs down), 0 (neutral), 1 (thumbs up)
}

// handleObservationFeedback handles user feedback (thumbs up/down) for an observation.
// POST /api/observations/{id}/feedback
func (s *Service) handleObservationFeedback(w http.ResponseWriter, r *http.Request) {
	// Parse observation ID
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid observation id", http.StatusBadRequest)
		return
	}

	// Parse request body
	var req FeedbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Validate feedback value
	if req.Feedback < -1 || req.Feedback > 1 {
		http.Error(w, "feedback must be -1, 0, or 1", http.StatusBadRequest)
		return
	}

	// Get required components (initMu.RLock held by requireReady middleware)
	observationStore := s.observationStore
	scoreCalculator := s.scoreCalculator

	if observationStore == nil {
		http.Error(w, "service not ready", http.StatusServiceUnavailable)
		return
	}

	// Update feedback in database
	if err := observationStore.UpdateObservationFeedback(r.Context(), id, req.Feedback); err != nil {
		http.Error(w, "failed to update feedback", http.StatusInternalServerError)
		return
	}

	// Recalculate score immediately if calculator is available
	var newScore float64
	if scoreCalculator != nil {
		obs, err := observationStore.GetObservationByID(r.Context(), id)
		if err == nil && obs != nil {
			obs.UserFeedback = req.Feedback // Apply the new feedback
			newScore = scoreCalculator.Calculate(obs, time.Now())
			if err := observationStore.UpdateImportanceScore(r.Context(), id, newScore); err != nil {
				// Log but don't fail - feedback was recorded
				// Score will be updated on next recalculation cycle
				_ = err // Explicitly ignore - non-critical operation
			}
		}
	}

	// Broadcast update via SSE
	s.sseBroadcaster.Broadcast(map[string]interface{}{
		"type":     "observation_feedback",
		"id":       id,
		"feedback": req.Feedback,
		"score":    newScore,
	})

	writeJSON(w, map[string]interface{}{
		"status":   "ok",
		"id":       id,
		"feedback": req.Feedback,
		"score":    newScore,
	})
}

// handleGetScoringStats returns scoring statistics and configuration.
// GET /api/scoring/stats
func (s *Service) handleGetScoringStats(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")

	// initMu.RLock held by requireReady middleware
	observationStore := s.observationStore
	recalculator := s.recalculator

	if observationStore == nil {
		http.Error(w, "service not ready", http.StatusServiceUnavailable)
		return
	}

	// Get feedback statistics
	feedbackStats, err := observationStore.GetObservationFeedbackStats(r.Context(), project)
	if err != nil {
		http.Error(w, "failed to get feedback stats", http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"feedback": feedbackStats,
	}

	// Add recalculator stats if available
	if recalculator != nil {
		response["recalculator"] = recalculator.GetStats()
	}

	writeJSON(w, response)
}

// handleGetTopObservations returns the highest-scoring observations.
// GET /api/observations/top
func (s *Service) handleGetTopObservations(w http.ResponseWriter, r *http.Request) {
	limit := parseIntParam(r, "limit", 10)
	project := r.URL.Query().Get("project")

	// initMu.RLock held by requireReady middleware
	observationStore := s.observationStore

	if observationStore == nil {
		http.Error(w, "service not ready", http.StatusServiceUnavailable)
		return
	}

	observations, err := observationStore.GetTopScoringObservations(r.Context(), project, limit)
	if err != nil {
		http.Error(w, "failed to get top observations", http.StatusInternalServerError)
		return
	}

	if observations == nil {
		observations = []*models.Observation{}
	}

	writeJSON(w, observations)
}

// handleGetMostRetrieved returns the most frequently retrieved observations.
// GET /api/observations/most-retrieved
func (s *Service) handleGetMostRetrieved(w http.ResponseWriter, r *http.Request) {
	limit := parseIntParam(r, "limit", 10)
	project := r.URL.Query().Get("project")

	// initMu.RLock held by requireReady middleware
	observationStore := s.observationStore

	if observationStore == nil {
		http.Error(w, "service not ready", http.StatusServiceUnavailable)
		return
	}

	observations, err := observationStore.GetMostRetrievedObservations(r.Context(), project, limit)
	if err != nil {
		http.Error(w, "failed to get most retrieved observations", http.StatusInternalServerError)
		return
	}

	if observations == nil {
		observations = []*models.Observation{}
	}

	writeJSON(w, observations)
}

// handleExplainScore returns a breakdown of how an observation's score was calculated.
// GET /api/observations/{id}/score
func (s *Service) handleExplainScore(w http.ResponseWriter, r *http.Request) {
	// Parse observation ID
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid observation id", http.StatusBadRequest)
		return
	}

	// initMu.RLock held by requireReady middleware
	observationStore := s.observationStore
	scoreCalculator := s.scoreCalculator

	if observationStore == nil || scoreCalculator == nil {
		http.Error(w, "service not ready", http.StatusServiceUnavailable)
		return
	}

	// Get observation
	obs, err := observationStore.GetObservationByID(r.Context(), id)
	if err != nil {
		http.Error(w, "failed to get observation", http.StatusInternalServerError)
		return
	}
	if obs == nil {
		http.Error(w, "observation not found", http.StatusNotFound)
		return
	}

	// Calculate score components
	components := scoreCalculator.CalculateComponents(obs, time.Now())

	writeJSON(w, map[string]interface{}{
		"id":         id,
		"components": components,
		"config":     scoreCalculator.GetConfig(),
	})
}

// handleUpdateConceptWeight updates a concept weight.
// PUT /api/scoring/concepts/{concept}
func (s *Service) handleUpdateConceptWeight(w http.ResponseWriter, r *http.Request) {
	concept := chi.URLParam(r, "concept")
	if concept == "" {
		http.Error(w, "concept is required", http.StatusBadRequest)
		return
	}

	var req struct {
		Weight float64 `json:"weight"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Validate weight
	if req.Weight < 0 || req.Weight > 1 {
		http.Error(w, "weight must be between 0 and 1", http.StatusBadRequest)
		return
	}

	// initMu.RLock held by requireReady middleware
	observationStore := s.observationStore
	recalculator := s.recalculator

	if observationStore == nil {
		http.Error(w, "service not ready", http.StatusServiceUnavailable)
		return
	}

	// Update in database
	if err := observationStore.UpdateConceptWeight(r.Context(), concept, req.Weight); err != nil {
		http.Error(w, "failed to update concept weight", http.StatusInternalServerError)
		return
	}

	// Refresh concept weights in recalculator
	if recalculator != nil {
		if err := recalculator.RefreshConceptWeights(r.Context()); err != nil {
			// Log but don't fail - weight was saved
			_ = err // Explicitly ignore - non-critical operation
		}
	}

	writeJSON(w, map[string]interface{}{
		"status":  "ok",
		"concept": concept,
		"weight":  req.Weight,
	})
}

// handleGetConceptWeights returns all concept weights.
// GET /api/scoring/concepts
func (s *Service) handleGetConceptWeights(w http.ResponseWriter, r *http.Request) {
	// initMu.RLock held by requireReady middleware
	observationStore := s.observationStore

	if observationStore == nil {
		http.Error(w, "service not ready", http.StatusServiceUnavailable)
		return
	}

	weights, err := observationStore.GetConceptWeights(r.Context())
	if err != nil {
		http.Error(w, "failed to get concept weights", http.StatusInternalServerError)
		return
	}

	writeJSON(w, weights)
}

// handleTriggerRecalculation triggers an immediate score recalculation.
// POST /api/scoring/recalculate
func (s *Service) handleTriggerRecalculation(w http.ResponseWriter, r *http.Request) {
	// initMu.RLock held by requireReady middleware
	recalculator := s.recalculator

	if recalculator == nil {
		http.Error(w, "recalculator not available", http.StatusServiceUnavailable)
		return
	}

	// Run recalculation in background with independent context
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if err := recalculator.RecalculateNow(ctx); err != nil {
			log.Error().Err(err).Msg("Background recalculation failed")
		}
	}()

	writeJSON(w, map[string]string{"status": "recalculation triggered"})
}

// handleRunMaintenance triggers an immediate, synchronous database maintenance run
// (Optimize/TRUNCATE checkpoint + prompt cleanup + any enabled retention/stale cleanup)
// and also kicks off an importance-score recalculation in the background so the behavior
// of the previous trigger_maintenance tool is preserved (issue #49).
func (s *Service) handleRunMaintenance(w http.ResponseWriter, r *http.Request) {
	// initMu.RLock held by requireReady middleware
	maintSvc := s.maintenanceSvc
	recalculator := s.recalculator

	if maintSvc == nil {
		http.Error(w, "maintenance service not available", http.StatusServiceUnavailable)
		return
	}

	// Run maintenance synchronously with an independent, bounded context so the caller
	// receives a real completion status. Use context.Background so an HTTP client timeout
	// does not abort an in-progress DB maintenance pass.
	mctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	maintSvc.RunNowSync(mctx)

	// Preserve prior trigger_maintenance behavior: also recalculate importance scores.
	recalcTriggered := false
	if recalculator != nil {
		recalcTriggered = true
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			if err := recalculator.RecalculateNow(ctx); err != nil {
				log.Error().Err(err).Msg("Background recalculation during maintenance failed")
			}
		}()
	}

	writeJSON(w, map[string]any{
		"status":            "maintenance completed",
		"recalc_triggered":  recalcTriggered,
		"maintenance_stats": maintSvc.Stats(),
	})
}

// parseIntParam parses an integer query parameter with a default value.
func parseIntParam(r *http.Request, name string, defaultVal int) int {
	if val := r.URL.Query().Get(name); val != "" {
		if parsed, err := strconv.Atoi(val); err == nil && parsed > 0 {
			return parsed
		}
	}
	return defaultVal
}

// incrementRetrievalCounts increments retrieval counts for observations.
// Called after search results are returned to track popularity.
func (s *Service) incrementRetrievalCounts(ids []int64) {
	if len(ids) == 0 {
		return
	}

	// initMu.RLock held by requireReady middleware (caller is always behind requireReady)
	store := s.observationStore

	if store == nil {
		return
	}

	// Increment in background to not block response
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		// Create a new context with timeout for the background operation
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		if err := store.IncrementRetrievalCount(ctx, ids); err != nil {
			// Log but don't fail - this is a background operation
			_ = err // Explicitly ignore - background operation
		}
	}()
}
