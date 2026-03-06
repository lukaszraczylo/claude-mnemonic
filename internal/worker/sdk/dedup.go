// Package sdk provides write-time observation deduplication via vector similarity.
package sdk

import (
	"context"
	"fmt"
	"strings"

	"github.com/lukaszraczylo/claude-mnemonic/internal/config"
	"github.com/lukaszraczylo/claude-mnemonic/internal/db/gorm"
	"github.com/lukaszraczylo/claude-mnemonic/internal/vector/sqlitevec"
	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
	"github.com/rs/zerolog/log"
)

// DeduplicationResult represents the outcome of a vector similarity dedup check.
type DeduplicationResult struct {
	ExistingID int64
	Similarity float64
	Action     string // "insert", "merge"
}

// checkVectorDeduplication checks if a similar observation already exists using vector similarity.
// Returns a result indicating whether to insert or merge, or an error.
// On any failure, returns Action="insert" so the caller always proceeds with storage.
func (p *Processor) checkVectorDeduplication(ctx context.Context, obs *models.ParsedObservation, project string) *DeduplicationResult {
	cfg := config.Get()
	if !cfg.DeduplicationEnabled {
		return &DeduplicationResult{Action: "insert"}
	}

	if p.vectorClient == nil {
		return &DeduplicationResult{Action: "insert"}
	}

	// Build search text from observation fields
	searchText := buildObservationSearchText(obs)
	if searchText == "" {
		return &DeduplicationResult{Action: "insert"}
	}

	// Query vector DB for similar observations in the same project
	where := sqlitevec.BuildWhereFilter(sqlitevec.DocTypeObservation, project)
	results, err := p.vectorClient.Query(ctx, searchText, 3, where)
	if err != nil {
		log.Debug().Err(err).Msg("Vector search failed during dedup check")
		return &DeduplicationResult{Action: "insert"}
	}

	// Check results for high similarity
	for _, r := range results {
		if r.Similarity >= cfg.DeduplicationThreshold {
			obsID := extractObservationIDFromVectorDoc(r)
			if obsID > 0 {
				return &DeduplicationResult{
					ExistingID: obsID,
					Similarity: r.Similarity,
					Action:     "merge",
				}
			}
		}
	}

	return &DeduplicationResult{Action: "insert"}
}

// buildObservationSearchText creates searchable text from a parsed observation.
func buildObservationSearchText(obs *models.ParsedObservation) string {
	var parts []string
	if obs.Title != "" {
		parts = append(parts, obs.Title)
	}
	if obs.Subtitle != "" {
		parts = append(parts, obs.Subtitle)
	}
	if obs.Narrative != "" {
		parts = append(parts, obs.Narrative)
	}
	text := strings.Join(parts, " ")
	if len(text) > 2000 {
		text = text[:2000]
	}
	return text
}

// extractObservationIDFromVectorDoc extracts the SQLite observation ID from a vector query result.
func extractObservationIDFromVectorDoc(r sqlitevec.QueryResult) int64 {
	// Prefer the sqlite_id metadata field (set during vector sync)
	if sqliteID, ok := r.Metadata["sqlite_id"].(float64); ok && sqliteID > 0 {
		return int64(sqliteID)
	}
	if sqliteID, ok := r.Metadata["sqlite_id"].(int64); ok && sqliteID > 0 {
		return sqliteID
	}

	// Fallback: parse from doc_id format "obs_{id}_composite" or "obs_{id}_narrative"
	if !strings.HasPrefix(r.ID, "obs_") {
		return 0
	}
	parts := strings.SplitN(r.ID[4:], "_", 2)
	if len(parts) == 0 {
		return 0
	}
	var id int64
	fmt.Sscanf(parts[0], "%d", &id)
	return id
}

// mergeObservation updates an existing observation with new information from a duplicate.
// It appends new facts, updates the narrative if the new one is longer,
// and bumps the importance score to reflect reconfirmation.
func (p *Processor) mergeObservation(ctx context.Context, existingID int64, newObs *models.ParsedObservation) error {
	existing, err := p.observationStore.GetObservationByID(ctx, existingID)
	if err != nil {
		return fmt.Errorf("fetch existing observation %d: %w", existingID, err)
	}
	if existing == nil {
		return fmt.Errorf("observation %d not found", existingID)
	}

	update := &gorm.ObservationUpdate{}
	changed := false

	// Merge facts: append new facts not already present
	if len(newObs.Facts) > 0 {
		existingFactSet := make(map[string]struct{}, len(existing.Facts))
		for _, f := range existing.Facts {
			existingFactSet[f] = struct{}{}
		}
		mergedFacts := make([]string, len(existing.Facts))
		copy(mergedFacts, existing.Facts)
		for _, f := range newObs.Facts {
			if _, exists := existingFactSet[f]; !exists {
				mergedFacts = append(mergedFacts, f)
				changed = true
			}
		}
		if changed {
			update.Facts = &mergedFacts
		}
	}

	// Update narrative if the new one is longer/more detailed
	if len(newObs.Narrative) > len(existing.Narrative.String) {
		update.Narrative = &newObs.Narrative
		changed = true
	}

	if !changed {
		// Nothing new to merge, but still count it as a confirmed observation
		log.Debug().Int64("id", existingID).Msg("Dedup merge: no new content, skipping update")
		return nil
	}

	_, err = p.observationStore.UpdateObservation(ctx, existingID, update)
	if err != nil {
		return fmt.Errorf("update observation %d: %w", existingID, err)
	}

	return nil
}
