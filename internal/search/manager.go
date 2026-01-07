// Package search provides unified search capabilities for claude-mnemonic.
package search

import (
	"context"
	"strings"

	"github.com/lukaszraczylo/claude-mnemonic/internal/db/gorm"
	"github.com/lukaszraczylo/claude-mnemonic/internal/vector/sqlitevec"
	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
)

// Manager provides unified search across SQLite and sqlite-vec.
type Manager struct {
	observationStore *gorm.ObservationStore
	summaryStore     *gorm.SummaryStore
	promptStore      *gorm.PromptStore
	vectorClient     *sqlitevec.Client
}

// NewManager creates a new search manager.
func NewManager(
	observationStore *gorm.ObservationStore,
	summaryStore *gorm.SummaryStore,
	promptStore *gorm.PromptStore,
	vectorClient *sqlitevec.Client,
) *Manager {
	return &Manager{
		observationStore: observationStore,
		summaryStore:     summaryStore,
		promptStore:      promptStore,
		vectorClient:     vectorClient,
	}
}

// SearchParams contains parameters for unified search.
type SearchParams struct {
	Format            string
	Type              string
	Project           string
	ObsType           string
	Concepts          string
	Files             string
	Query             string
	Scope             string
	OrderBy           string
	DateStart         int64
	Offset            int
	Limit             int
	DateEnd           int64
	IncludeGlobal     bool
	ExcludeSuperseded bool
}

// SearchResult represents a unified search result.
type SearchResult struct {
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	Type      string                 `json:"type"`
	Title     string                 `json:"title,omitempty"`
	Content   string                 `json:"content,omitempty"`
	Project   string                 `json:"project"`
	Scope     string                 `json:"scope,omitempty"`
	ID        int64                  `json:"id"`
	CreatedAt int64                  `json:"created_at_epoch"`
	Score     float64                `json:"score,omitempty"`
}

// UnifiedSearchResult contains the combined search results.
type UnifiedSearchResult struct {
	Query      string         `json:"query,omitempty"`
	Results    []SearchResult `json:"results"`
	TotalCount int            `json:"total_count"`
}

// UnifiedSearch performs a unified search across all document types.
func (m *Manager) UnifiedSearch(ctx context.Context, params SearchParams) (*UnifiedSearchResult, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	if params.Limit > 100 {
		params.Limit = 100
	}
	if params.OrderBy == "" {
		params.OrderBy = "date_desc"
	}

	// If query is provided and vector client is available, use vector search
	if params.Query != "" && m.vectorClient != nil && m.vectorClient.IsConnected() {
		return m.vectorSearch(ctx, params)
	}

	// Otherwise fall back to structured filter search
	return m.filterSearch(ctx, params)
}

// vectorSearch performs semantic search via sqlite-vec.
func (m *Manager) vectorSearch(ctx context.Context, params SearchParams) (*UnifiedSearchResult, error) {
	// Build where filter based on search type
	var docType sqlitevec.DocType
	switch params.Type {
	case "observations":
		docType = sqlitevec.DocTypeObservation
	case "sessions":
		docType = sqlitevec.DocTypeSessionSummary
	case "prompts":
		docType = sqlitevec.DocTypeUserPrompt
	}
	where := sqlitevec.BuildWhereFilter(docType, params.Project)

	// Query sqlite-vec
	vectorResults, err := m.vectorClient.Query(ctx, params.Query, params.Limit*2, where)
	if err != nil {
		// Fall back to filter search on error
		return m.filterSearch(ctx, params)
	}

	// Extract IDs grouped by document type using shared helper
	extracted := sqlitevec.ExtractIDsByDocType(vectorResults)
	obsIDs := extracted.ObservationIDs
	summaryIDs := extracted.SummaryIDs
	promptIDs := extracted.PromptIDs

	// Fetch full records from SQLite
	var results []SearchResult

	if len(obsIDs) > 0 && (params.Type == "" || params.Type == "observations") {
		obs, err := m.observationStore.GetObservationsByIDs(ctx, obsIDs, params.OrderBy, 0)
		if err == nil {
			for _, o := range obs {
				// Skip superseded observations when requested
				if params.ExcludeSuperseded && o.IsSuperseded {
					continue
				}
				results = append(results, m.observationToResult(o, params.Format))
			}
		}
	}

	if len(summaryIDs) > 0 && (params.Type == "" || params.Type == "sessions") {
		summaries, err := m.summaryStore.GetSummariesByIDs(ctx, summaryIDs, params.OrderBy, 0)
		if err == nil {
			for _, s := range summaries {
				results = append(results, m.summaryToResult(s, params.Format))
			}
		}
	}

	if len(promptIDs) > 0 && (params.Type == "" || params.Type == "prompts") {
		prompts, err := m.promptStore.GetPromptsByIDs(ctx, promptIDs, params.OrderBy, 0)
		if err == nil {
			for _, p := range prompts {
				results = append(results, m.promptToResult(p, params.Format))
			}
		}
	}

	// Apply limit
	if len(results) > params.Limit {
		results = results[:params.Limit]
	}

	return &UnifiedSearchResult{
		Results:    results,
		TotalCount: len(results),
		Query:      params.Query,
	}, nil
}

// filterSearch performs structured filter search via SQLite.
func (m *Manager) filterSearch(ctx context.Context, params SearchParams) (*UnifiedSearchResult, error) {
	var results []SearchResult

	// Search observations
	if params.Type == "" || params.Type == "observations" {
		var obs []*models.Observation
		var err error

		// Use active observations (excluding superseded) when requested
		if params.ExcludeSuperseded {
			obs, err = m.observationStore.GetActiveObservations(ctx, params.Project, params.Limit)
		} else {
			obs, err = m.observationStore.GetRecentObservations(ctx, params.Project, params.Limit)
		}

		if err == nil {
			for _, o := range obs {
				results = append(results, m.observationToResult(o, params.Format))
			}
		}
	}

	// Search summaries
	if params.Type == "" || params.Type == "sessions" {
		summaries, err := m.summaryStore.GetRecentSummaries(ctx, params.Project, params.Limit)
		if err == nil {
			for _, s := range summaries {
				results = append(results, m.summaryToResult(s, params.Format))
			}
		}
	}

	// Apply limit
	if len(results) > params.Limit {
		results = results[:params.Limit]
	}

	return &UnifiedSearchResult{
		Results:    results,
		TotalCount: len(results),
	}, nil
}

// Decisions performs a semantic search optimized for finding decisions.
func (m *Manager) Decisions(ctx context.Context, params SearchParams) (*UnifiedSearchResult, error) {
	// Boost query with decision-related keywords
	if params.Query != "" {
		params.Query = params.Query + " decision chose architecture"
	}
	params.Type = "observations"
	return m.UnifiedSearch(ctx, params)
}

// Changes performs a semantic search optimized for finding code changes.
func (m *Manager) Changes(ctx context.Context, params SearchParams) (*UnifiedSearchResult, error) {
	// Boost query with change-related keywords
	if params.Query != "" {
		params.Query = params.Query + " changed modified refactored"
	}
	params.Type = "observations"
	return m.UnifiedSearch(ctx, params)
}

// HowItWorks performs a semantic search optimized for understanding architecture.
func (m *Manager) HowItWorks(ctx context.Context, params SearchParams) (*UnifiedSearchResult, error) {
	// Boost query with architecture-related keywords
	if params.Query != "" {
		params.Query = params.Query + " architecture design pattern implements"
	}
	params.Type = "observations"
	return m.UnifiedSearch(ctx, params)
}

// Helper methods

func (m *Manager) observationToResult(obs *models.Observation, format string) SearchResult {
	result := SearchResult{
		Type:      "observation",
		ID:        obs.ID,
		Project:   obs.Project,
		Scope:     string(obs.Scope),
		CreatedAt: obs.CreatedAtEpoch,
		Metadata: map[string]interface{}{
			"obs_type": string(obs.Type),
			"scope":    string(obs.Scope),
		},
	}

	if obs.Title.Valid {
		result.Title = obs.Title.String
	}

	if format == "full" && obs.Narrative.Valid {
		result.Content = obs.Narrative.String
	}

	return result
}

func (m *Manager) summaryToResult(summary *models.SessionSummary, format string) SearchResult {
	result := SearchResult{
		Type:      "session",
		ID:        summary.ID,
		Project:   summary.Project,
		CreatedAt: summary.CreatedAtEpoch,
	}

	if summary.Request.Valid {
		result.Title = truncate(summary.Request.String, 100)
	}

	if format == "full" && summary.Learned.Valid {
		result.Content = summary.Learned.String
	}

	return result
}

func (m *Manager) promptToResult(prompt *models.UserPromptWithSession, format string) SearchResult {
	result := SearchResult{
		Type:      "prompt",
		ID:        prompt.ID,
		Project:   prompt.Project,
		CreatedAt: prompt.CreatedAtEpoch,
	}

	result.Title = truncate(prompt.PromptText, 100)

	if format == "full" {
		result.Content = prompt.PromptText
	}

	return result
}

func truncate(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
