// Package sqlite provides SQLite database operations for claude-mnemonic.
package sqlite

import (
	"context"
	"time"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
)

// SummaryStore provides summary-related database operations.
type SummaryStore struct {
	store *Store
}

// NewSummaryStore creates a new summary store.
func NewSummaryStore(store *Store) *SummaryStore {
	return &SummaryStore{store: store}
}

// StoreSummary stores a new session summary.
func (s *SummaryStore) StoreSummary(ctx context.Context, sdkSessionID, project string, summary *models.ParsedSummary, promptNumber int, discoveryTokens int64) (int64, int64, error) {
	now := time.Now()
	nowEpoch := now.UnixMilli()

	// Ensure session exists (auto-create if missing)
	if err := s.ensureSessionExists(ctx, sdkSessionID, project); err != nil {
		return 0, 0, err
	}

	const query = `
		INSERT INTO session_summaries
		(sdk_session_id, project, request, investigated, learned, completed,
		 next_steps, notes, prompt_number, discovery_tokens, created_at, created_at_epoch)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	result, err := s.store.ExecContext(ctx, query,
		sdkSessionID, project,
		nullString(summary.Request), nullString(summary.Investigated),
		nullString(summary.Learned), nullString(summary.Completed),
		nullString(summary.NextSteps), nullString(summary.Notes),
		nullInt(promptNumber), discoveryTokens,
		now.Format(time.RFC3339), nowEpoch,
	)
	if err != nil {
		return 0, 0, err
	}

	id, _ := result.LastInsertId()
	return id, nowEpoch, nil
}

// ensureSessionExists creates a session if it doesn't exist.
func (s *SummaryStore) ensureSessionExists(ctx context.Context, sdkSessionID, project string) error {
	return EnsureSessionExists(ctx, s.store, sdkSessionID, project)
}

// GetSummariesByIDs retrieves summaries by a list of IDs.
func (s *SummaryStore) GetSummariesByIDs(ctx context.Context, ids []int64, orderBy string, limit int) ([]*models.SessionSummary, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	const baseQuery = `
		SELECT id, sdk_session_id, project, request, investigated, learned, completed,
		       next_steps, notes, prompt_number, discovery_tokens, created_at, created_at_epoch
		FROM session_summaries`

	query, args := BuildGetByIDsQuery(baseQuery, ids, orderBy, limit)

	rows, err := s.store.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanSummaryRows(rows)
}

// GetRecentSummaries retrieves recent summaries for a project.
func (s *SummaryStore) GetRecentSummaries(ctx context.Context, project string, limit int) ([]*models.SessionSummary, error) {
	const query = `
		SELECT id, sdk_session_id, project, request, investigated, learned, completed,
		       next_steps, notes, prompt_number, discovery_tokens, created_at, created_at_epoch
		FROM session_summaries
		WHERE project = ?
		ORDER BY created_at_epoch DESC
		LIMIT ?
	`

	rows, err := s.store.QueryContext(ctx, query, project, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanSummaryRows(rows)
}

// GetAllRecentSummaries retrieves recent summaries across all projects.
func (s *SummaryStore) GetAllRecentSummaries(ctx context.Context, limit int) ([]*models.SessionSummary, error) {
	const query = `
		SELECT id, sdk_session_id, project, request, investigated, learned, completed,
		       next_steps, notes, prompt_number, discovery_tokens, created_at, created_at_epoch
		FROM session_summaries
		ORDER BY created_at_epoch DESC
		LIMIT ?
	`

	rows, err := s.store.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanSummaryRows(rows)
}

// GetAllSummaries retrieves all summaries (for vector rebuild).
func (s *SummaryStore) GetAllSummaries(ctx context.Context) ([]*models.SessionSummary, error) {
	const query = `
		SELECT id, sdk_session_id, project, request, investigated, learned, completed,
		       next_steps, notes, prompt_number, discovery_tokens, created_at, created_at_epoch
		FROM session_summaries
		ORDER BY id
	`

	rows, err := s.store.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanSummaryRows(rows)
}
