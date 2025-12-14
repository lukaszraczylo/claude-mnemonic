// Package chroma provides ChromaDB vector database integration for claude-mnemonic.
package chroma

import (
	"context"
	"fmt"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
	"github.com/rs/zerolog/log"
)

// Sync provides synchronization between SQLite and ChromaDB.
type Sync struct {
	client *Client
}

// NewSync creates a new ChromaDB sync service.
func NewSync(client *Client) *Sync {
	return &Sync{client: client}
}

// SyncObservation syncs a single observation to ChromaDB.
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
		Msg("Synced observation to ChromaDB")

	return nil
}

// formatObservationDocs formats an observation into ChromaDB documents.
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

// SyncSummary syncs a single session summary to ChromaDB.
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
		Msg("Synced summary to ChromaDB")

	return nil
}

// formatSummaryDocs formats a session summary into ChromaDB documents.
func (s *Sync) formatSummaryDocs(summary *models.SessionSummary) []Document {
	docs := make([]Document, 0, 6)

	baseMetadata := map[string]any{
		"sqlite_id":        summary.ID,
		"doc_type":         "session_summary",
		"sdk_session_id":   summary.SDKSessionID,
		"project":          summary.Project,
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

// SyncUserPrompt syncs a single user prompt to ChromaDB.
func (s *Sync) SyncUserPrompt(ctx context.Context, prompt *models.UserPromptWithSession) error {
	doc := Document{
		ID:      fmt.Sprintf("prompt_%d", prompt.ID),
		Content: prompt.PromptText,
		Metadata: map[string]any{
			"sqlite_id":        prompt.ID,
			"doc_type":         "user_prompt",
			"sdk_session_id":   prompt.SDKSessionID,
			"project":          prompt.Project,
			"created_at_epoch": prompt.CreatedAtEpoch,
			"prompt_number":    prompt.PromptNumber,
		},
	}

	if err := s.client.AddDocuments(ctx, []Document{doc}); err != nil {
		return fmt.Errorf("add prompt doc: %w", err)
	}

	log.Debug().
		Int64("promptId", prompt.ID).
		Msg("Synced user prompt to ChromaDB")

	return nil
}

// DeleteObservations removes observation documents from ChromaDB.
// Since each observation may have multiple documents (narrative + facts),
// we delete by the sqlite_id metadata prefix pattern.
func (s *Sync) DeleteObservations(ctx context.Context, observationIDs []int64) error {
	if len(observationIDs) == 0 {
		return nil
	}

	// Generate all possible document IDs for these observations
	// Pattern: obs_{id}_narrative, obs_{id}_fact_{0..n}
	// Since we don't know how many facts each had, we use a reasonable upper bound
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
		Msg("Deleted observations from ChromaDB")

	return nil
}

// DeleteUserPrompts removes user prompt documents from ChromaDB.
func (s *Sync) DeleteUserPrompts(ctx context.Context, promptIDs []int64) error {
	if len(promptIDs) == 0 {
		return nil
	}

	// Each prompt is stored as a single document with ID pattern: prompt_{id}
	ids := make([]string, len(promptIDs))
	for i, promptID := range promptIDs {
		ids[i] = fmt.Sprintf("prompt_%d", promptID)
	}

	if err := s.client.DeleteDocuments(ctx, ids); err != nil {
		return fmt.Errorf("delete prompt docs: %w", err)
	}

	log.Debug().
		Int("promptCount", len(promptIDs)).
		Msg("Deleted user prompts from ChromaDB")

	return nil
}

// Helper functions

func copyMetadata(base map[string]any, key string, value any) map[string]any {
	result := make(map[string]any, len(base)+1)
	for k, v := range base {
		result[k] = v
	}
	result[key] = value
	return result
}

func copyMetadataMulti(base map[string]any, extra map[string]any) map[string]any {
	result := make(map[string]any, len(base)+len(extra))
	for k, v := range base {
		result[k] = v
	}
	for k, v := range extra {
		result[k] = v
	}
	return result
}

func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	result := strs[0]
	for i := 1; i < len(strs); i++ {
		result += sep + strs[i]
	}
	return result
}
