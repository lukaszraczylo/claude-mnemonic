// Package sqlitevec provides sqlite-vec based vector database integration for claude-mnemonic.
package sqlitevec

import (
	"context"
	"fmt"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
	"github.com/rs/zerolog/log"
)

// Sync provides synchronization between SQLite data and vector embeddings.
type Sync struct {
	client *Client
}

// NewSync creates a new sync service.
func NewSync(client *Client) *Sync {
	return &Sync{client: client}
}

// SyncObservation syncs a single observation to the vector store.
func (s *Sync) SyncObservation(ctx context.Context, obs *models.Observation) error {
	docs := s.formatObservationDocs(obs)
	if len(docs) == 0 {
		return nil
	}

	if err := s.client.AddDocuments(ctx, docs); err != nil {
		return fmt.Errorf("add observation docs: %w", err)
	}

	log.Debug().
		Int64("observationId", obs.ID).
		Int("docCount", len(docs)).
		Msg("Synced observation to sqlite-vec")

	return nil
}

// formatObservationDocs formats an observation into vector documents.
// Each semantic field becomes a separate vector document (granular approach).
func (s *Sync) formatObservationDocs(obs *models.Observation) []Document {
	docs := make([]Document, 0, len(obs.Facts)+2)

	// Determine scope for metadata
	scope := string(obs.Scope)
	if scope == "" {
		scope = "project"
	}

	baseMetadata := map[string]any{
		"sqlite_id":        obs.ID,
		"doc_type":         "observation",
		"sdk_session_id":   obs.SDKSessionID,
		"project":          obs.Project,
		"scope":            scope,
		"created_at_epoch": obs.CreatedAtEpoch,
		"type":             string(obs.Type),
	}

	if obs.Title.Valid {
		baseMetadata["title"] = obs.Title.String
	}
	if obs.Subtitle.Valid {
		baseMetadata["subtitle"] = obs.Subtitle.String
	}
	if len(obs.Concepts) > 0 {
		baseMetadata["concepts"] = joinStrings(obs.Concepts, ",")
	}
	if len(obs.FilesRead) > 0 {
		baseMetadata["files_read"] = joinStrings(obs.FilesRead, ",")
	}
	if len(obs.FilesModified) > 0 {
		baseMetadata["files_modified"] = joinStrings(obs.FilesModified, ",")
	}

	// Narrative as separate document
	if obs.Narrative.Valid && obs.Narrative.String != "" {
		docs = append(docs, Document{
			ID:       fmt.Sprintf("obs_%d_narrative", obs.ID),
			Content:  obs.Narrative.String,
			Metadata: copyMetadata(baseMetadata, "field_type", "narrative"),
		})
	}

	// Each fact as separate document
	for i, fact := range obs.Facts {
		docs = append(docs, Document{
			ID:      fmt.Sprintf("obs_%d_fact_%d", obs.ID, i),
			Content: fact,
			Metadata: copyMetadataMulti(baseMetadata, map[string]any{
				"field_type": "fact",
				"fact_index": i,
			}),
		})
	}

	return docs
}

// SyncSummary syncs a single session summary to the vector store.
func (s *Sync) SyncSummary(ctx context.Context, summary *models.SessionSummary) error {
	docs := s.formatSummaryDocs(summary)
	if len(docs) == 0 {
		return nil
	}

	if err := s.client.AddDocuments(ctx, docs); err != nil {
		return fmt.Errorf("add summary docs: %w", err)
	}

	log.Debug().
		Int64("summaryId", summary.ID).
		Int("docCount", len(docs)).
		Msg("Synced summary to sqlite-vec")

	return nil
}

// formatSummaryDocs formats a session summary into vector documents.
func (s *Sync) formatSummaryDocs(summary *models.SessionSummary) []Document {
	docs := make([]Document, 0, 6)

	baseMetadata := map[string]any{
		"sqlite_id":        summary.ID,
		"doc_type":         "session_summary",
		"sdk_session_id":   summary.SDKSessionID,
		"project":          summary.Project,
		"scope":            "", // Summaries don't have scope
		"created_at_epoch": summary.CreatedAtEpoch,
	}

	if summary.PromptNumber.Valid {
		baseMetadata["prompt_number"] = summary.PromptNumber.Int64
	}

	// Each field as separate document
	fields := []struct {
		name  string
		value string
		valid bool
	}{
		{"request", summary.Request.String, summary.Request.Valid},
		{"investigated", summary.Investigated.String, summary.Investigated.Valid},
		{"learned", summary.Learned.String, summary.Learned.Valid},
		{"completed", summary.Completed.String, summary.Completed.Valid},
		{"next_steps", summary.NextSteps.String, summary.NextSteps.Valid},
		{"notes", summary.Notes.String, summary.Notes.Valid},
	}

	for _, field := range fields {
		if field.valid && field.value != "" {
			docs = append(docs, Document{
				ID:       fmt.Sprintf("summary_%d_%s", summary.ID, field.name),
				Content:  field.value,
				Metadata: copyMetadata(baseMetadata, "field_type", field.name),
			})
		}
	}

	return docs
}

// SyncUserPrompt syncs a single user prompt to the vector store.
func (s *Sync) SyncUserPrompt(ctx context.Context, prompt *models.UserPromptWithSession) error {
	doc := Document{
		ID:      fmt.Sprintf("prompt_%d", prompt.ID),
		Content: prompt.PromptText,
		Metadata: map[string]any{
			"sqlite_id":        prompt.ID,
			"doc_type":         "user_prompt",
			"sdk_session_id":   prompt.SDKSessionID,
			"project":          prompt.Project,
			"scope":            "", // Prompts don't have scope
			"created_at_epoch": prompt.CreatedAtEpoch,
			"prompt_number":    prompt.PromptNumber,
			"field_type":       "prompt",
		},
	}

	if err := s.client.AddDocuments(ctx, []Document{doc}); err != nil {
		return fmt.Errorf("add prompt doc: %w", err)
	}

	log.Debug().
		Int64("promptId", prompt.ID).
		Msg("Synced user prompt to sqlite-vec")

	return nil
}

// DeleteObservations removes observation documents from the vector store.
func (s *Sync) DeleteObservations(ctx context.Context, observationIDs []int64) error {
	if len(observationIDs) == 0 {
		return nil
	}

	// Generate all possible document IDs for these observations
	// Pattern: obs_{id}_narrative, obs_{id}_fact_{0..n}
	const maxFactsPerObs = 20
	ids := make([]string, 0, len(observationIDs)*(maxFactsPerObs+1))

	for _, obsID := range observationIDs {
		ids = append(ids, fmt.Sprintf("obs_%d_narrative", obsID))
		for i := 0; i < maxFactsPerObs; i++ {
			ids = append(ids, fmt.Sprintf("obs_%d_fact_%d", obsID, i))
		}
	}

	if err := s.client.DeleteDocuments(ctx, ids); err != nil {
		return fmt.Errorf("delete observation docs: %w", err)
	}

	log.Debug().
		Int("observationCount", len(observationIDs)).
		Msg("Deleted observations from sqlite-vec")

	return nil
}

// DeleteUserPrompts removes user prompt documents from the vector store.
func (s *Sync) DeleteUserPrompts(ctx context.Context, promptIDs []int64) error {
	if len(promptIDs) == 0 {
		return nil
	}

	ids := make([]string, len(promptIDs))
	for i, promptID := range promptIDs {
		ids[i] = fmt.Sprintf("prompt_%d", promptID)
	}

	if err := s.client.DeleteDocuments(ctx, ids); err != nil {
		return fmt.Errorf("delete prompt docs: %w", err)
	}

	log.Debug().
		Int("promptCount", len(promptIDs)).
		Msg("Deleted user prompts from sqlite-vec")

	return nil
}

// SyncPattern syncs a single pattern to the vector store.
func (s *Sync) SyncPattern(ctx context.Context, pattern *models.Pattern) error {
	docs := s.formatPatternDocs(pattern)
	if len(docs) == 0 {
		return nil
	}

	if err := s.client.AddDocuments(ctx, docs); err != nil {
		return fmt.Errorf("add pattern docs: %w", err)
	}

	log.Debug().
		Int64("patternId", pattern.ID).
		Int("docCount", len(docs)).
		Msg("Synced pattern to sqlite-vec")

	return nil
}

// formatPatternDocs formats a pattern into vector documents.
func (s *Sync) formatPatternDocs(pattern *models.Pattern) []Document {
	docs := make([]Document, 0, 3)

	baseMetadata := map[string]any{
		"sqlite_id":        pattern.ID,
		"doc_type":         "pattern",
		"pattern_type":     string(pattern.Type),
		"status":           string(pattern.Status),
		"scope":            "global", // Patterns are always global
		"frequency":        pattern.Frequency,
		"confidence":       pattern.Confidence,
		"created_at_epoch": pattern.CreatedAtEpoch,
	}

	if len(pattern.Signature) > 0 {
		baseMetadata["signature"] = joinStrings(pattern.Signature, ",")
	}
	if len(pattern.Projects) > 0 {
		baseMetadata["projects"] = joinStrings(pattern.Projects, ",")
	}

	// Pattern name as document
	if pattern.Name != "" {
		docs = append(docs, Document{
			ID:       fmt.Sprintf("pattern_%d_name", pattern.ID),
			Content:  pattern.Name,
			Metadata: copyMetadata(baseMetadata, "field_type", "name"),
		})
	}

	// Pattern description as document
	if pattern.Description.Valid && pattern.Description.String != "" {
		docs = append(docs, Document{
			ID:       fmt.Sprintf("pattern_%d_description", pattern.ID),
			Content:  pattern.Description.String,
			Metadata: copyMetadata(baseMetadata, "field_type", "description"),
		})
	}

	// Pattern recommendation as document
	if pattern.Recommendation.Valid && pattern.Recommendation.String != "" {
		docs = append(docs, Document{
			ID:       fmt.Sprintf("pattern_%d_recommendation", pattern.ID),
			Content:  pattern.Recommendation.String,
			Metadata: copyMetadata(baseMetadata, "field_type", "recommendation"),
		})
	}

	return docs
}

// DeletePatterns removes pattern documents from the vector store.
func (s *Sync) DeletePatterns(ctx context.Context, patternIDs []int64) error {
	if len(patternIDs) == 0 {
		return nil
	}

	// Generate all possible document IDs for these patterns
	// Pattern: pattern_{id}_name, pattern_{id}_description, pattern_{id}_recommendation
	ids := make([]string, 0, len(patternIDs)*3)

	for _, patternID := range patternIDs {
		ids = append(ids, fmt.Sprintf("pattern_%d_name", patternID))
		ids = append(ids, fmt.Sprintf("pattern_%d_description", patternID))
		ids = append(ids, fmt.Sprintf("pattern_%d_recommendation", patternID))
	}

	if err := s.client.DeleteDocuments(ctx, ids); err != nil {
		return fmt.Errorf("delete pattern docs: %w", err)
	}

	log.Debug().
		Int("patternCount", len(patternIDs)).
		Msg("Deleted patterns from sqlite-vec")

	return nil
}

// BatchSyncConfig configures batch synchronization behavior.
type BatchSyncConfig struct {
	BatchSize       int // Number of items per batch (default: 50)
	ProgressLogFreq int // Log progress every N items (default: 100)
}

// DefaultBatchSyncConfig returns sensible defaults for batch sync.
func DefaultBatchSyncConfig() BatchSyncConfig {
	return BatchSyncConfig{
		BatchSize:       50,
		ProgressLogFreq: 100,
	}
}

// BatchSyncObservations syncs multiple observations efficiently in batches.
// This reduces memory pressure during large rebuilds by processing in chunks.
func (s *Sync) BatchSyncObservations(ctx context.Context, observations []*models.Observation, cfg BatchSyncConfig) (synced int, errors int) {
	if len(observations) == 0 {
		return 0, 0
	}

	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 50
	}
	if cfg.ProgressLogFreq <= 0 {
		cfg.ProgressLogFreq = 100
	}

	for i := 0; i < len(observations); i += cfg.BatchSize {
		// Check context cancellation
		select {
		case <-ctx.Done():
			log.Warn().Int("synced", synced).Int("remaining", len(observations)-i).Msg("Batch sync cancelled")
			return synced, errors
		default:
		}

		end := min(i+cfg.BatchSize, len(observations))

		batch := observations[i:end]
		var docs []Document

		// Collect all documents for this batch
		for _, obs := range batch {
			docs = append(docs, s.formatObservationDocs(obs)...)
		}

		// Add all documents in one call
		if len(docs) > 0 {
			if err := s.client.AddDocuments(ctx, docs); err != nil {
				log.Warn().Err(err).Int("batchStart", i).Int("batchSize", len(batch)).Msg("Failed to sync observation batch")
				errors += len(batch)
				continue
			}
		}

		synced += len(batch)

		// Log progress periodically
		if synced%cfg.ProgressLogFreq == 0 || synced == len(observations) {
			log.Debug().Int("synced", synced).Int("total", len(observations)).Msg("Observation batch sync progress")
		}
	}

	return synced, errors
}

// BatchSyncSummaries syncs multiple summaries efficiently in batches.
func (s *Sync) BatchSyncSummaries(ctx context.Context, summaries []*models.SessionSummary, cfg BatchSyncConfig) (synced int, errors int) {
	if len(summaries) == 0 {
		return 0, 0
	}

	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 50
	}
	if cfg.ProgressLogFreq <= 0 {
		cfg.ProgressLogFreq = 100
	}

	for i := 0; i < len(summaries); i += cfg.BatchSize {
		// Check context cancellation
		select {
		case <-ctx.Done():
			log.Warn().Int("synced", synced).Int("remaining", len(summaries)-i).Msg("Batch sync cancelled")
			return synced, errors
		default:
		}

		end := min(i+cfg.BatchSize, len(summaries))

		batch := summaries[i:end]
		var docs []Document

		// Collect all documents for this batch
		for _, summary := range batch {
			docs = append(docs, s.formatSummaryDocs(summary)...)
		}

		// Add all documents in one call
		if len(docs) > 0 {
			if err := s.client.AddDocuments(ctx, docs); err != nil {
				log.Warn().Err(err).Int("batchStart", i).Int("batchSize", len(batch)).Msg("Failed to sync summary batch")
				errors += len(batch)
				continue
			}
		}

		synced += len(batch)

		// Log progress periodically
		if synced%cfg.ProgressLogFreq == 0 || synced == len(summaries) {
			log.Debug().Int("synced", synced).Int("total", len(summaries)).Msg("Summary batch sync progress")
		}
	}

	return synced, errors
}

// BatchSyncPrompts syncs multiple user prompts efficiently in batches.
func (s *Sync) BatchSyncPrompts(ctx context.Context, prompts []*models.UserPromptWithSession, cfg BatchSyncConfig) (synced int, errors int) {
	if len(prompts) == 0 {
		return 0, 0
	}

	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 50
	}
	if cfg.ProgressLogFreq <= 0 {
		cfg.ProgressLogFreq = 100
	}

	for i := 0; i < len(prompts); i += cfg.BatchSize {
		// Check context cancellation
		select {
		case <-ctx.Done():
			log.Warn().Int("synced", synced).Int("remaining", len(prompts)-i).Msg("Batch sync cancelled")
			return synced, errors
		default:
		}

		end := min(i+cfg.BatchSize, len(prompts))

		batch := prompts[i:end]
		docs := make([]Document, 0, len(batch))

		// Collect all documents for this batch
		for _, prompt := range batch {
			docs = append(docs, Document{
				ID:      fmt.Sprintf("prompt_%d", prompt.ID),
				Content: prompt.PromptText,
				Metadata: map[string]any{
					"sqlite_id":        prompt.ID,
					"doc_type":         "user_prompt",
					"sdk_session_id":   prompt.SDKSessionID,
					"project":          prompt.Project,
					"scope":            "",
					"created_at_epoch": prompt.CreatedAtEpoch,
					"prompt_number":    prompt.PromptNumber,
					"field_type":       "prompt",
				},
			})
		}

		// Add all documents in one call
		if len(docs) > 0 {
			if err := s.client.AddDocuments(ctx, docs); err != nil {
				log.Warn().Err(err).Int("batchStart", i).Int("batchSize", len(batch)).Msg("Failed to sync prompt batch")
				errors += len(batch)
				continue
			}
		}

		synced += len(batch)

		// Log progress periodically
		if synced%cfg.ProgressLogFreq == 0 || synced == len(prompts) {
			log.Debug().Int("synced", synced).Int("total", len(prompts)).Msg("Prompt batch sync progress")
		}
	}

	return synced, errors
}
