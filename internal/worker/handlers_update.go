// Package worker provides update and restart HTTP handlers.
package worker

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// handleUpdateCheck checks for available updates.
func (s *Service) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	info, err := s.updater.CheckForUpdate(r.Context())
	if err != nil {
		// Return a proper JSON response for errors instead of 500
		// This allows the frontend to handle it gracefully
		writeJSON(w, map[string]any{
			"available":       false,
			"current_version": s.version,
			"error":           err.Error(),
			"rate_limited":    strings.Contains(err.Error(), "403"),
		})
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
		writeJSON(w, map[string]any{
			"success": false,
			"message": "No update available",
		})
		return
	}

	// Apply update in background with tracking for graceful shutdown
	s.wg.Go(func() {
		if err := s.updater.ApplyUpdate(s.ctx, info); err != nil {
			log.Error().Err(err).Msg("Update failed")
		}
	})

	writeJSON(w, map[string]any{
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

	// Check Cross-Encoder Reranker
	rerankerStatus := ComponentHealth{Name: "Cross-Encoder Reranker", Status: "healthy"}
	if !s.config.RerankingEnabled {
		rerankerStatus.Status = "degraded"
		rerankerStatus.Message = "Disabled in config"
		if overall == "healthy" {
			overall = "degraded"
		}
	} else if s.reranker == nil {
		rerankerStatus.Status = "degraded"
		rerankerStatus.Message = "Not initialized"
		if overall == "healthy" {
			overall = "degraded"
		}
	} else {
		// Verify reranker is functional using Score
		_, normalizedScore, err := s.reranker.Score("test query", "test document")
		if err != nil {
			rerankerStatus.Status = "unhealthy"
			rerankerStatus.Message = fmt.Sprintf("Score check failed: %v", err)
			if overall == "healthy" {
				overall = "degraded"
			}
		} else {
			rerankerStatus.Message = fmt.Sprintf("Score check passed (%.4f)", normalizedScore)
		}
	}
	components = append(components, rerankerStatus)

	// Calculate uptime
	uptime := time.Since(s.startTime).Round(time.Second).String()

	writeJSON(w, SelfCheckResponse{
		Overall:    overall,
		Version:    s.version,
		Uptime:     uptime,
		Components: components,
	})
}

// handleUpdateRestart restarts the worker with the new binary (after update).
func (s *Service) handleUpdateRestart(w http.ResponseWriter, r *http.Request) {
	status := s.updater.GetStatus()
	if status.State != "done" {
		http.Error(w, "no update has been applied", http.StatusBadRequest)
		return
	}

	// Send response before restarting
	writeJSON(w, map[string]any{
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

// handleRestart restarts the worker process (general restart, not tied to update).
func (s *Service) handleRestart(w http.ResponseWriter, r *http.Request) {
	log.Info().Msg("Manual restart requested via API")

	// Send response before restarting
	writeJSON(w, map[string]any{
		"success": true,
		"message": "Restarting worker...",
		"version": s.version,
	})

	// Flush the response
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	// Restart in background after response is sent
	go func() {
		// Small delay to ensure response is sent
		time.Sleep(100 * time.Millisecond)
		if err := s.updater.Restart(); err != nil {
			log.Error().Err(err).Msg("Failed to restart worker")
		}
	}()
}
