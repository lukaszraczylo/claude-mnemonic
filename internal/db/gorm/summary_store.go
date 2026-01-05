// Package gorm provides GORM-based database operations for claude-mnemonic.
package gorm

import (
	"context"
	"database/sql"
	"time"

	"gorm.io/gorm"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
)

// SummaryStore provides summary-related database operations using GORM.
type SummaryStore struct {
	db *gorm.DB
}

// NewSummaryStore creates a new summary store.
func NewSummaryStore(store *Store) *SummaryStore {
	return &SummaryStore{db: store.DB}
}

// StoreSummary stores a new session summary.
func (s *SummaryStore) StoreSummary(ctx context.Context, sdkSessionID, project string, summary *models.ParsedSummary, promptNumber int, discoveryTokens int64) (int64, int64, error) {
	now := time.Now()
	nowEpoch := now.UnixMilli()

	// Ensure session exists (auto-create if missing)
	if err := EnsureSessionExists(ctx, s.db, sdkSessionID, project); err != nil {
		return 0, 0, err
	}

	dbSummary := &SessionSummary{
		SDKSessionID: sdkSessionID,
		Project:      project,
		Request:      nullString(summary.Request),
		Investigated: nullString(summary.Investigated),
		Learned:      nullString(summary.Learned),
		Completed:    nullString(summary.Completed),
		NextSteps:    nullString(summary.NextSteps),
		Notes:        nullString(summary.Notes),
		PromptNumber: func() sql.NullInt64 {
			if promptNumber > 0 {
				return sql.NullInt64{Int64: int64(promptNumber), Valid: true}
			}
			return sql.NullInt64{Valid: false}
		}(),
		DiscoveryTokens: discoveryTokens,
		CreatedAt:       now.Format(time.RFC3339),
		CreatedAtEpoch:  nowEpoch,
	}

	err := s.db.WithContext(ctx).Create(dbSummary).Error
	if err != nil {
		return 0, 0, err
	}

	return dbSummary.ID, nowEpoch, nil
}

// GetSummariesByIDs retrieves summaries by a list of IDs.
func (s *SummaryStore) GetSummariesByIDs(ctx context.Context, ids []int64, orderBy string, limit int) ([]*models.SessionSummary, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	var dbSummaries []SessionSummary
	query := s.db.WithContext(ctx).Where("id IN ?", ids)

	// Apply ordering
	switch orderBy {
	case "date_asc":
		query = query.Order("created_at_epoch ASC")
	case "date_desc", "default", "":
		query = query.Order("created_at_epoch DESC")
	}

	// Apply limit
	if limit > 0 {
		query = query.Limit(limit)
	}

	err := query.Find(&dbSummaries).Error
	if err != nil {
		return nil, err
	}

	return toModelSessionSummaries(dbSummaries), nil
}

// GetRecentSummaries retrieves recent summaries for a project.
func (s *SummaryStore) GetRecentSummaries(ctx context.Context, project string, limit int) ([]*models.SessionSummary, error) {
	var dbSummaries []SessionSummary
	err := s.db.WithContext(ctx).
		Where("project = ?", project).
		Order("created_at_epoch DESC").
		Limit(limit).
		Find(&dbSummaries).Error

	if err != nil {
		return nil, err
	}

	return toModelSessionSummaries(dbSummaries), nil
}

// GetAllRecentSummaries retrieves recent summaries across all projects.
func (s *SummaryStore) GetAllRecentSummaries(ctx context.Context, limit int) ([]*models.SessionSummary, error) {
	var dbSummaries []SessionSummary
	err := s.db.WithContext(ctx).
		Order("created_at_epoch DESC").
		Limit(limit).
		Find(&dbSummaries).Error

	if err != nil {
		return nil, err
	}

	return toModelSessionSummaries(dbSummaries), nil
}

// GetAllSummaries retrieves all summaries (for vector rebuild).
func (s *SummaryStore) GetAllSummaries(ctx context.Context) ([]*models.SessionSummary, error) {
	var dbSummaries []SessionSummary
	err := s.db.WithContext(ctx).
		Order("id").
		Find(&dbSummaries).Error

	if err != nil {
		return nil, err
	}

	return toModelSessionSummaries(dbSummaries), nil
}

// toModelSessionSummary converts a GORM SessionSummary to pkg/models.SessionSummary.
func toModelSessionSummary(s *SessionSummary) *models.SessionSummary {
	return &models.SessionSummary{
		ID:              s.ID,
		SDKSessionID:    s.SDKSessionID,
		Project:         s.Project,
		Request:         s.Request,
		Investigated:    s.Investigated,
		Learned:         s.Learned,
		Completed:       s.Completed,
		NextSteps:       s.NextSteps,
		Notes:           s.Notes,
		PromptNumber:    s.PromptNumber,
		DiscoveryTokens: s.DiscoveryTokens,
		CreatedAt:       s.CreatedAt,
		CreatedAtEpoch:  s.CreatedAtEpoch,
	}
}

// toModelSessionSummaries converts a slice of GORM SessionSummary to pkg/models.SessionSummary.
func toModelSessionSummaries(summaries []SessionSummary) []*models.SessionSummary {
	result := make([]*models.SessionSummary, len(summaries))
	for i := range summaries {
		result[i] = toModelSessionSummary(&summaries[i])
	}
	return result
}

// nullString converts a string to sql.NullString.
func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: s, Valid: true}
}
