// Package worker provides import, export, and archive HTTP handlers.
package worker

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/lukaszraczylo/claude-mnemonic/internal/db/gorm"
	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
	"github.com/lukaszraczylo/claude-mnemonic/pkg/similarity"
	"github.com/rs/zerolog/log"
)

// BulkImportRequest is the request body for bulk observation import.
type BulkImportRequest struct {
	Project      string                 `json:"project"`
	Observations []BulkObservationInput `json:"observations"`
}

// BulkObservationInput represents a single observation in bulk import.
type BulkObservationInput struct {
	Type          string   `json:"type"`
	Title         string   `json:"title"`
	Subtitle      string   `json:"subtitle,omitempty"`
	Narrative     string   `json:"narrative,omitempty"`
	Scope         string   `json:"scope,omitempty"`
	Facts         []string `json:"facts,omitempty"`
	Concepts      []string `json:"concepts,omitempty"`
	FilesRead     []string `json:"files_read,omitempty"`
	FilesModified []string `json:"files_modified,omitempty"`
}

// BulkImportResponse contains the result of a bulk import operation.
type BulkImportResponse struct {
	Errors            []string `json:"errors,omitempty"`
	Imported          int      `json:"imported"`
	Failed            int      `json:"failed"`
	SkippedDuplicates int      `json:"skipped_duplicates,omitempty"`
}

// handleBulkImport handles bulk import of observations.
// This is useful for migrating data or importing observations from external sources.
func (s *Service) handleBulkImport(w http.ResponseWriter, r *http.Request) {
	// Rate limit bulk operations to prevent DoS
	if s.bulkOpLimiter != nil && !s.bulkOpLimiter.CanExecute() {
		remaining := s.bulkOpLimiter.CooldownRemaining()
		http.Error(w, fmt.Sprintf("bulk import rate limited, retry in %d seconds", remaining), http.StatusTooManyRequests)
		return
	}

	var req BulkImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.Project == "" {
		http.Error(w, "project is required", http.StatusBadRequest)
		return
	}

	// Validate project name to prevent path traversal
	if err := ValidateProjectName(req.Project); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if len(req.Observations) == 0 {
		http.Error(w, "at least one observation is required", http.StatusBadRequest)
		return
	}

	// Limit batch size to prevent overwhelming the system
	maxBatchSize := 100
	if len(req.Observations) > maxBatchSize {
		http.Error(w, fmt.Sprintf("batch size exceeds maximum of %d", maxBatchSize), http.StatusBadRequest)
		return
	}

	// Create a synthetic session for bulk import
	sessionID, err := s.sessionStore.CreateSDKSession(r.Context(), fmt.Sprintf("bulk-import-%d", time.Now().UnixMilli()), req.Project, "bulk import")
	if err != nil {
		http.Error(w, "failed to create import session: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var imported, failed, skippedDupes int
	var errors []string

	// Track imported observations for deduplication within the batch
	importedObs := make([]*models.Observation, 0, len(req.Observations))

	// Deduplication threshold - observations more similar than this are considered duplicates
	const dedupThreshold = 0.7

	for i, obsInput := range req.Observations {
		// Validate observation type using O(1) map lookup
		if !IsValidObservationType(obsInput.Type) {
			failed++
			errors = append(errors, fmt.Sprintf("observation %d: invalid type '%s'", i, obsInput.Type))
			continue
		}

		// Build parsed observation
		parsedObs := &models.ParsedObservation{
			Type:          models.ObservationType(obsInput.Type),
			Title:         obsInput.Title,
			Subtitle:      obsInput.Subtitle,
			Facts:         obsInput.Facts,
			Narrative:     obsInput.Narrative,
			Concepts:      obsInput.Concepts,
			FilesRead:     obsInput.FilesRead,
			FilesModified: obsInput.FilesModified,
			Scope:         models.ObservationScope(obsInput.Scope),
		}

		// Convert to temporary observation for similarity check
		tempObs := &models.Observation{
			Title:     sql.NullString{String: parsedObs.Title, Valid: parsedObs.Title != ""},
			Subtitle:  sql.NullString{String: parsedObs.Subtitle, Valid: parsedObs.Subtitle != ""},
			Narrative: sql.NullString{String: parsedObs.Narrative, Valid: parsedObs.Narrative != ""},
		}

		// Check for duplicates within this import batch
		if similarity.IsSimilarToAny(tempObs, importedObs, dedupThreshold) {
			skippedDupes++
			continue
		}

		// Store observation
		obsID, _, err := s.observationStore.StoreObservation(
			r.Context(),
			fmt.Sprintf("bulk-import-%d", sessionID),
			req.Project,
			parsedObs,
			0, // prompt number
			0, // discovery tokens
		)
		if err != nil {
			failed++
			errors = append(errors, fmt.Sprintf("observation %d: %v", i, err))
			continue
		}

		// Sync to vector DB asynchronously with rate limiting
		if s.vectorSync != nil {
			s.asyncVectorSync(func() {
				// Use service context as parent to respect shutdown signals
				ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
				defer cancel()
				obs, err := s.observationStore.GetObservationByID(ctx, obsID)
				if err == nil && obs != nil {
					if syncErr := s.vectorSync.SyncObservation(ctx, obs); syncErr != nil {
						if s.ctx.Err() == nil { // Don't log during shutdown
							log.Debug().Err(syncErr).Int64("id", obsID).Msg("Failed to sync observation during bulk import")
						}
					}
				}
			})
		}

		// Track for deduplication of subsequent observations in this batch
		importedObs = append(importedObs, tempObs)
		imported++
	}

	log.Info().
		Str("project", req.Project).
		Int("imported", imported).
		Int("failed", failed).
		Int("skipped_duplicates", skippedDupes).
		Msg("Bulk import completed")

	// Invalidate observation count cache after import
	if imported > 0 {
		if req.Project != "" {
			s.invalidateObsCountCache(req.Project)
		} else {
			s.invalidateAllObsCountCache()
		}
	}

	// Broadcast observation event for dashboard refresh
	s.sseBroadcaster.Broadcast(map[string]any{
		"type":    "observation",
		"action":  "bulk_import",
		"project": req.Project,
		"count":   imported,
	})

	writeJSON(w, BulkImportResponse{
		Imported:          imported,
		Failed:            failed,
		SkippedDuplicates: skippedDupes,
		Errors:            errors,
	})
}

// ArchiveRequest is the request body for archiving observations.
type ArchiveRequest struct {
	Project    string  `json:"project,omitempty"`
	Reason     string  `json:"reason,omitempty"`
	IDs        []int64 `json:"ids,omitempty"`
	MaxAgeDays int     `json:"max_age_days,omitempty"`
}

// handleArchiveObservations archives observations by ID or by age.
// Supports batch archival with error tracking per observation.
func (s *Service) handleArchiveObservations(w http.ResponseWriter, r *http.Request) {
	var req ArchiveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	var archivedIDs []int64
	var failedIDs []int64
	var errors []string
	var err error

	if len(req.IDs) > 0 {
		// Archive specific observations with parallel processing for large batches
		if len(req.IDs) > 5 {
			// Use parallel archival for batches larger than 5
			type archiveResult struct {
				err error
				id  int64
			}
			results := make(chan archiveResult, len(req.IDs))

			// Limit concurrency to avoid overwhelming the database
			sem := make(chan struct{}, 5)
			var wg sync.WaitGroup

			for _, id := range req.IDs {
				wg.Add(1)
				go func(obsID int64) {
					defer wg.Done()
					sem <- struct{}{}        // Acquire
					defer func() { <-sem }() // Release

					archErr := s.observationStore.ArchiveObservation(r.Context(), obsID, req.Reason)
					results <- archiveResult{id: obsID, err: archErr}
				}(id)
			}

			// Close results channel when all goroutines complete
			go func() {
				wg.Wait()
				close(results)
			}()

			// Collect results
			for res := range results {
				if res.err != nil {
					log.Warn().Err(res.err).Int64("id", res.id).Msg("Failed to archive observation")
					failedIDs = append(failedIDs, res.id)
					errors = append(errors, fmt.Sprintf("id %d: %v", res.id, res.err))
				} else {
					archivedIDs = append(archivedIDs, res.id)
				}
			}
		} else {
			// Sequential for small batches
			for _, id := range req.IDs {
				if archErr := s.observationStore.ArchiveObservation(r.Context(), id, req.Reason); archErr != nil {
					log.Warn().Err(archErr).Int64("id", id).Msg("Failed to archive observation")
					failedIDs = append(failedIDs, id)
					errors = append(errors, fmt.Sprintf("id %d: %v", id, archErr))
				} else {
					archivedIDs = append(archivedIDs, id)
				}
			}
		}
	} else if req.Project != "" || req.MaxAgeDays > 0 {
		// Archive by age
		archivedIDs, err = s.observationStore.ArchiveOldObservations(r.Context(), req.Project, req.MaxAgeDays, req.Reason)
		if err != nil {
			http.Error(w, "failed to archive: "+err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		http.Error(w, "either 'ids' or 'project'/'max_age_days' is required", http.StatusBadRequest)
		return
	}

	log.Info().
		Str("project", req.Project).
		Int("archived", len(archivedIDs)).
		Int("failed", len(failedIDs)).
		Msg("Observations archived")

	// Invalidate cache if any observations were archived
	if len(archivedIDs) > 0 {
		if req.Project != "" {
			s.invalidateObsCountCache(req.Project)
		} else {
			s.invalidateAllObsCountCache()
		}
	}

	response := map[string]any{
		"archived_count": len(archivedIDs),
		"archived_ids":   archivedIDs,
	}
	if len(failedIDs) > 0 {
		response["failed_count"] = len(failedIDs)
		response["failed_ids"] = failedIDs
		response["errors"] = errors
	}

	writeJSON(w, response)
}

// handleUnarchiveObservation restores an archived observation.
func (s *Service) handleUnarchiveObservation(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid observation id", http.StatusBadRequest)
		return
	}

	if err := s.observationStore.UnarchiveObservation(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Invalidate all caches since we don't know the project
	s.invalidateAllObsCountCache()

	writeJSON(w, map[string]any{
		"success": true,
		"id":      id,
	})
}

// handleGetArchivedObservations returns archived observations.
func (s *Service) handleGetArchivedObservations(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	limit := gorm.ParseLimitParam(r, DefaultObservationsLimit)

	observations, err := s.observationStore.GetArchivedObservations(r.Context(), project, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if observations == nil {
		observations = []*models.Observation{}
	}

	writeJSON(w, observations)
}

// handleGetArchivalStats returns archival statistics.
func (s *Service) handleGetArchivalStats(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")

	stats, err := s.observationStore.GetArchivalStats(r.Context(), project)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, stats)
}

// handleExportObservations exports observations in JSON or CSV format.
// Supports query parameters: project, format (json/csv), scope, type, limit.
func (s *Service) handleExportObservations(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}
	scope := r.URL.Query().Get("scope")                 // project, global, or empty for all
	obsType := r.URL.Query().Get("type")                // bugfix, feature, etc.
	limit := gorm.ParseLimitParamWithMax(r, 1000, 5000) // Higher limit for exports, capped at 5000

	// Validate format
	if format != "json" && format != "csv" {
		http.Error(w, "format must be 'json' or 'csv'", http.StatusBadRequest)
		return
	}

	// Get observations with filters
	ctx := r.Context()
	var observations []*models.Observation
	var err error

	if project != "" {
		observations, _, err = s.observationStore.GetObservationsByProjectStrictPaginated(ctx, project, limit, 0)
	} else {
		observations, _, err = s.observationStore.GetAllRecentObservationsPaginated(ctx, limit, 0)
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Apply additional filters
	if scope != "" || obsType != "" {
		filtered := make([]*models.Observation, 0, len(observations))
		for _, obs := range observations {
			if scope != "" && string(obs.Scope) != scope {
				continue
			}
			if obsType != "" && string(obs.Type) != obsType {
				continue
			}
			filtered = append(filtered, obs)
		}
		observations = filtered
	}

	// Generate filename
	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("observations-%s.%s", timestamp, format)
	if project != "" {
		// Sanitize project name for filename
		sanitized := strings.ReplaceAll(project, "/", "_")
		sanitized = strings.ReplaceAll(sanitized, "\\", "_")
		if len(sanitized) > 50 {
			sanitized = sanitized[:50]
		}
		filename = fmt.Sprintf("observations-%s-%s.%s", sanitized, timestamp, format)
	}

	switch format {
	case "csv":
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
		s.writeObservationsCSV(w, observations)
	default: // json
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
		writeJSON(w, map[string]any{
			"exported_at":  time.Now().Format(time.RFC3339),
			"project":      project,
			"count":        len(observations),
			"observations": observations,
		})
	}
}

// writeObservationsCSV writes observations in CSV format.
// Uses fmt.Fprintf directly to avoid intermediate string allocations.
func (s *Service) writeObservationsCSV(w http.ResponseWriter, observations []*models.Observation) {
	// Write CSV header
	_, _ = io.WriteString(w, "id,type,scope,project,title,subtitle,narrative,concepts,facts,created_at,importance_score\n")

	for _, obs := range observations {
		// Write directly to avoid string allocation per row
		_, _ = fmt.Fprintf(w, "%d,%s,%s,%s,%s,%s,%s,%s,%s,%s,%.2f\n",
			obs.ID,
			obs.Type,
			obs.Scope,
			escapeCsvField(obs.Project),
			escapeCsvField(obs.Title.String),
			escapeCsvField(obs.Subtitle.String),
			escapeCsvField(obs.Narrative.String),
			escapeCsvField(strings.Join(obs.Concepts, ";")),
			escapeCsvField(strings.Join(obs.Facts, ";")),
			obs.CreatedAt,
			obs.ImportanceScore,
		)
	}
}

// escapeCsvField escapes a field for CSV output.
func escapeCsvField(s string) string {
	// If field contains comma, quote, or newline, wrap in quotes and escape quotes
	if strings.ContainsAny(s, ",\"\n\r") {
		s = strings.ReplaceAll(s, "\"", "\"\"")
		return "\"" + s + "\""
	}
	return s
}

// BulkStatusRequest represents a request to update status for multiple observations.
type BulkStatusRequest struct {
	Action   string  `json:"action"`
	Reason   string  `json:"reason,omitempty"`
	IDs      []int64 `json:"ids"`
	Feedback int     `json:"feedback,omitempty"`
}

// handleBulkStatusUpdate updates status for multiple observations in one request.
func (s *Service) handleBulkStatusUpdate(w http.ResponseWriter, r *http.Request) {
	var req BulkStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if len(req.IDs) == 0 {
		http.Error(w, "ids is required", http.StatusBadRequest)
		return
	}

	if len(req.IDs) > 500 {
		http.Error(w, "maximum 500 ids per request", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	var updated, failed int
	var errors []string

	switch req.Action {
	case "supersede":
		for _, id := range req.IDs {
			if err := s.observationStore.MarkAsSuperseded(ctx, id); err != nil {
				failed++
				errors = append(errors, fmt.Sprintf("id %d: %v", id, err))
			} else {
				updated++
			}
		}

	case "archive":
		for _, id := range req.IDs {
			if err := s.observationStore.ArchiveObservation(ctx, id, req.Reason); err != nil {
				failed++
				errors = append(errors, fmt.Sprintf("id %d: %v", id, err))
			} else {
				updated++
			}
		}

	case "set_feedback":
		if req.Feedback < -1 || req.Feedback > 1 {
			http.Error(w, "feedback must be -1, 0, or 1", http.StatusBadRequest)
			return
		}
		for _, id := range req.IDs {
			if err := s.observationStore.UpdateObservationFeedback(ctx, id, req.Feedback); err != nil {
				failed++
				errors = append(errors, fmt.Sprintf("id %d: %v", id, err))
			} else {
				updated++
			}
		}

	default:
		http.Error(w, "action must be 'supersede', 'archive', or 'set_feedback'", http.StatusBadRequest)
		return
	}

	// Invalidate cache for archive action (affects observation counts)
	if req.Action == "archive" && updated > 0 {
		// No project info available, invalidate all caches
		s.invalidateAllObsCountCache()
	}

	response := map[string]any{
		"action":  req.Action,
		"updated": updated,
		"failed":  failed,
	}
	if len(errors) > 0 {
		response["errors"] = errors
	}

	writeJSON(w, response)
}

// handleFindDuplicates finds potential duplicate observations using similarity clustering.
// Returns groups of similar observations that may be candidates for merging or archival.
func (s *Service) handleFindDuplicates(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	thresholdStr := r.URL.Query().Get("threshold")
	limit := gorm.ParseLimitParam(r, 100)

	// Parse threshold (default 0.6 = 60% similarity)
	threshold := 0.6
	if thresholdStr != "" {
		if t, err := strconv.ParseFloat(thresholdStr, 64); err == nil && t > 0 && t < 1 {
			threshold = t
		}
	}

	// Get recent observations
	ctx := r.Context()
	var observations []*models.Observation
	var err error

	if project != "" {
		observations, _, err = s.observationStore.GetObservationsByProjectStrictPaginated(ctx, project, limit, 0)
	} else {
		observations, _, err = s.observationStore.GetAllRecentObservationsPaginated(ctx, limit, 0)
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if len(observations) < 2 {
		writeJSON(w, map[string]any{
			"duplicate_groups": []any{},
			"total_checked":    len(observations),
			"threshold":        threshold,
		})
		return
	}

	// Find duplicates using similarity comparison
	type duplicateGroup struct {
		Observations []map[string]any `json:"observations"`
		Similarity   float64          `json:"similarity"`
	}

	groups := []duplicateGroup{}
	processed := make(map[int64]bool)

	for i, obs1 := range observations {
		if processed[obs1.ID] {
			continue
		}

		terms1 := similarity.ExtractObservationTerms(obs1)
		if len(terms1) == 0 {
			continue
		}

		group := duplicateGroup{
			Observations: []map[string]any{obs1.ToMap()},
			Similarity:   1.0,
		}

		for j := i + 1; j < len(observations); j++ {
			obs2 := observations[j]
			if processed[obs2.ID] {
				continue
			}

			terms2 := similarity.ExtractObservationTerms(obs2)
			sim := similarity.JaccardSimilarity(terms1, terms2)

			if sim >= threshold {
				obsMap := obs2.ToMap()
				obsMap["similarity_to_first"] = sim
				group.Observations = append(group.Observations, obsMap)
				group.Similarity = min(group.Similarity, sim)
				processed[obs2.ID] = true
			}
		}

		if len(group.Observations) > 1 {
			processed[obs1.ID] = true
			groups = append(groups, group)
		}
	}

	// Sort groups by size (largest first)
	sort.Slice(groups, func(i, j int) bool {
		return len(groups[i].Observations) > len(groups[j].Observations)
	})

	writeJSON(w, map[string]any{
		"duplicate_groups": groups,
		"total_checked":    len(observations),
		"groups_found":     len(groups),
		"threshold":        threshold,
		"project":          project,
	})
}
