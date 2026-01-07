// Package gorm provides GORM-based database operations for claude-mnemonic.
package gorm

import (
	"context"
	"database/sql"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
)

// PromptCleanupFunc is a callback for when prompts are cleaned up.
// Receives the IDs of deleted prompts for downstream cleanup (e.g., vector DB).
type PromptCleanupFunc func(ctx context.Context, deletedIDs []int64)

// MaxPromptsGlobal is the hard limit of prompts across all projects.
const MaxPromptsGlobal = 500

// PromptStore provides user prompt-related database operations using GORM.
type PromptStore struct {
	db          *gorm.DB
	cleanupFunc PromptCleanupFunc
}

// NewPromptStore creates a new prompt store.
func NewPromptStore(store *Store, cleanupFunc PromptCleanupFunc) *PromptStore {
	return &PromptStore{
		db:          store.DB,
		cleanupFunc: cleanupFunc,
	}
}

// SetCleanupFunc sets the callback for when prompts are deleted during cleanup.
func (s *PromptStore) SetCleanupFunc(fn PromptCleanupFunc) {
	s.cleanupFunc = fn
}

// SaveUserPromptWithMatches saves a user prompt with matched observation count.
// Uses INSERT OR IGNORE to be idempotent - duplicate (session, prompt_number) pairs are silently ignored.
// This prevents duplicate prompts when the user-prompt hook fires multiple times.
func (s *PromptStore) SaveUserPromptWithMatches(ctx context.Context, claudeSessionID string, promptNumber int, promptText string, matchedObservations int) (int64, error) {
	now := time.Now()

	prompt := &UserPrompt{
		ClaudeSessionID:     claudeSessionID,
		PromptNumber:        promptNumber,
		PromptText:          promptText,
		MatchedObservations: matchedObservations,
		CreatedAt:           now.Format(time.RFC3339),
		CreatedAtEpoch:      now.UnixMilli(),
	}

	// INSERT OR IGNORE using OnConflict
	result := s.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "claude_session_id"}, {Name: "prompt_number"}},
			DoNothing: true,
		}).
		Create(prompt)

	if result.Error != nil {
		return 0, result.Error
	}

	// If RowsAffected is 0, the insert was ignored (duplicate) - fetch the existing ID
	if result.RowsAffected == 0 {
		var existing UserPrompt
		err := s.db.Where("claude_session_id = ? AND prompt_number = ?", claudeSessionID, promptNumber).
			First(&existing).Error
		if err != nil {
			return 0, err
		}
		// Return existing ID without triggering cleanup (already handled when first inserted)
		return existing.ID, nil
	}

	// Cleanup old prompts beyond the global limit (async to not block handler)
	go func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		deletedIDs, _ := s.CleanupOldPrompts(cleanupCtx)
		if len(deletedIDs) > 0 && s.cleanupFunc != nil {
			s.cleanupFunc(cleanupCtx, deletedIDs)
		}
	}()

	return prompt.ID, nil
}

// CleanupOldPrompts deletes prompts beyond the global limit.
// Keeps the most recent MaxPromptsGlobal prompts.
// Returns the IDs of deleted prompts for downstream cleanup (e.g., vector DB).
func (s *PromptStore) CleanupOldPrompts(ctx context.Context) ([]int64, error) {
	// Use a transaction to prevent TOCTOU race condition
	var idsToDelete []int64

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Find IDs to keep (most recent MaxPromptsGlobal)
		var idsToKeep []int64
		err := tx.Model(&UserPrompt{}).
			Order("created_at_epoch DESC").
			Limit(MaxPromptsGlobal).
			Pluck("id", &idsToKeep).Error

		if err != nil {
			return err
		}

		if len(idsToKeep) == 0 {
			return nil
		}

		// Find IDs to delete (all IDs not in the keep list)
		// This happens in the same transaction to prevent race conditions
		err = tx.Model(&UserPrompt{}).
			Where("id NOT IN ?", idsToKeep).
			Pluck("id", &idsToDelete).Error

		if err != nil {
			return err
		}

		if len(idsToDelete) == 0 {
			return nil
		}

		// Delete the prompts
		return tx.Delete(&UserPrompt{}, idsToDelete).Error
	})

	if err != nil {
		return nil, err
	}

	return idsToDelete, nil
}

// GetPromptsByIDs retrieves user prompts by a list of IDs.
func (s *PromptStore) GetPromptsByIDs(ctx context.Context, ids []int64, orderBy string, limit int) ([]*models.UserPromptWithSession, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	var results []struct {
		UserPrompt
		Project      sql.NullString `gorm:"column:project"`
		SDKSessionID sql.NullString `gorm:"column:sdk_session_id"`
	}

	query := s.db.WithContext(ctx).
		Table("user_prompts up").
		Select("up.id, up.claude_session_id, up.prompt_number, up.prompt_text, "+
			"COALESCE(up.matched_observations, 0) as matched_observations, "+
			"up.created_at, up.created_at_epoch, "+
			"COALESCE(s.project, '') as project, "+
			"COALESCE(s.sdk_session_id, '') as sdk_session_id").
		Joins("LEFT JOIN sdk_sessions s ON up.claude_session_id = s.claude_session_id").
		Where("up.id IN ?", ids)

	// Apply ordering
	switch orderBy {
	case "date_asc":
		query = query.Order("up.created_at_epoch ASC")
	case "date_desc", "default", "":
		query = query.Order("up.created_at_epoch DESC")
	}

	// Apply limit
	if limit > 0 {
		query = query.Limit(limit)
	}

	err := query.Scan(&results).Error
	if err != nil {
		return nil, err
	}

	return toModelUserPromptsWithSession(results), nil
}

// GetAllRecentUserPrompts retrieves recent user prompts across all projects.
func (s *PromptStore) GetAllRecentUserPrompts(ctx context.Context, limit int) ([]*models.UserPromptWithSession, error) {
	var results []struct {
		UserPrompt
		Project      sql.NullString `gorm:"column:project"`
		SDKSessionID sql.NullString `gorm:"column:sdk_session_id"`
	}

	query := s.db.WithContext(ctx).
		Table("user_prompts up").
		Select("up.id, up.claude_session_id, up.prompt_number, up.prompt_text, " +
			"COALESCE(up.matched_observations, 0) as matched_observations, " +
			"up.created_at, up.created_at_epoch, " +
			"COALESCE(s.project, '') as project, " +
			"COALESCE(s.sdk_session_id, '') as sdk_session_id").
		Joins("LEFT JOIN sdk_sessions s ON up.claude_session_id = s.claude_session_id").
		Order("up.created_at_epoch DESC").
		Limit(limit)

	err := query.Scan(&results).Error
	if err != nil {
		return nil, err
	}

	return toModelUserPromptsWithSession(results), nil
}

// GetAllPrompts retrieves all user prompts (for vector rebuild).
func (s *PromptStore) GetAllPrompts(ctx context.Context) ([]*models.UserPromptWithSession, error) {
	var results []struct {
		UserPrompt
		Project      sql.NullString `gorm:"column:project"`
		SDKSessionID sql.NullString `gorm:"column:sdk_session_id"`
	}

	query := s.db.WithContext(ctx).
		Table("user_prompts up").
		Select("up.id, up.claude_session_id, up.prompt_number, up.prompt_text, " +
			"COALESCE(up.matched_observations, 0) as matched_observations, " +
			"up.created_at, up.created_at_epoch, " +
			"COALESCE(s.project, '') as project, " +
			"COALESCE(s.sdk_session_id, '') as sdk_session_id").
		Joins("LEFT JOIN sdk_sessions s ON up.claude_session_id = s.claude_session_id").
		Order("up.id")

	err := query.Scan(&results).Error
	if err != nil {
		return nil, err
	}

	return toModelUserPromptsWithSession(results), nil
}

// FindRecentPromptByText finds a recent prompt by exact text match within a time window.
// Returns (promptID, promptNumber, found).
func (s *PromptStore) FindRecentPromptByText(ctx context.Context, claudeSessionID, promptText string, withinSeconds int) (int64, int, bool) {
	cutoffEpoch := time.Now().Add(-time.Duration(withinSeconds) * time.Second).UnixMilli()

	var prompt UserPrompt
	err := s.db.WithContext(ctx).
		Where("claude_session_id = ? AND prompt_text = ? AND created_at_epoch >= ?",
			claudeSessionID, promptText, cutoffEpoch).
		Order("created_at_epoch DESC").
		First(&prompt).Error

	if err != nil {
		return 0, 0, false
	}

	return prompt.ID, prompt.PromptNumber, true
}

// GetRecentUserPromptsByProject retrieves recent user prompts for a specific project.
func (s *PromptStore) GetRecentUserPromptsByProject(ctx context.Context, project string, limit int) ([]*models.UserPromptWithSession, error) {
	var results []struct {
		UserPrompt
		Project      sql.NullString `gorm:"column:project"`
		SDKSessionID sql.NullString `gorm:"column:sdk_session_id"`
	}

	query := s.db.WithContext(ctx).
		Table("user_prompts up").
		Select("up.id, up.claude_session_id, up.prompt_number, up.prompt_text, "+
			"COALESCE(up.matched_observations, 0) as matched_observations, "+
			"up.created_at, up.created_at_epoch, "+
			"COALESCE(s.project, '') as project, "+
			"COALESCE(s.sdk_session_id, '') as sdk_session_id").
		Joins("LEFT JOIN sdk_sessions s ON up.claude_session_id = s.claude_session_id").
		Where("s.project = ?", project).
		Order("up.created_at_epoch DESC").
		Limit(limit)

	err := query.Scan(&results).Error
	if err != nil {
		return nil, err
	}

	return toModelUserPromptsWithSession(results), nil
}

// toModelUserPromptsWithSession converts query results to pkg/models.UserPromptWithSession.
func toModelUserPromptsWithSession(results []struct {
	UserPrompt
	Project      sql.NullString `gorm:"column:project"`
	SDKSessionID sql.NullString `gorm:"column:sdk_session_id"`
}) []*models.UserPromptWithSession {
	prompts := make([]*models.UserPromptWithSession, len(results))
	for i, r := range results {
		project := ""
		if r.Project.Valid {
			project = r.Project.String
		}

		sdkSessionID := ""
		if r.SDKSessionID.Valid {
			sdkSessionID = r.SDKSessionID.String
		}

		prompts[i] = &models.UserPromptWithSession{
			UserPrompt: models.UserPrompt{
				ID:                  r.ID,
				ClaudeSessionID:     r.ClaudeSessionID,
				PromptNumber:        r.PromptNumber,
				PromptText:          r.PromptText,
				MatchedObservations: r.MatchedObservations,
				CreatedAt:           r.CreatedAt,
				CreatedAtEpoch:      r.CreatedAtEpoch,
			},
			Project:      project,
			SDKSessionID: sdkSessionID,
		}
	}
	return prompts
}
