// Package sqlite provides SQLite database operations for claude-mnemonic.
package sqlite

import (
	"context"
	"time"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
)

// PromptCleanupFunc is a callback for when prompts are cleaned up.
// Receives the IDs of deleted prompts for downstream cleanup (e.g., vector DB).
type PromptCleanupFunc func(ctx context.Context, deletedIDs []int64)

// MaxPromptsGlobal is the hard limit of prompts across all projects.
const MaxPromptsGlobal = 500

// PromptStore provides user prompt-related database operations.
type PromptStore struct {
	store       *Store
	cleanupFunc PromptCleanupFunc
}

// NewPromptStore creates a new prompt store.
func NewPromptStore(store *Store) *PromptStore {
	return &PromptStore{store: store}
}

// SetCleanupFunc sets the callback for when prompts are deleted during cleanup.
func (s *PromptStore) SetCleanupFunc(fn PromptCleanupFunc) {
	s.cleanupFunc = fn
}

// SaveUserPromptWithMatches saves a user prompt with matched observation count.
func (s *PromptStore) SaveUserPromptWithMatches(ctx context.Context, claudeSessionID string, promptNumber int, promptText string, matchedObservations int) (int64, error) {
	now := time.Now()

	const query = `
		INSERT INTO user_prompts
		(claude_session_id, prompt_number, prompt_text, matched_observations, created_at, created_at_epoch)
		VALUES (?, ?, ?, ?, ?, ?)
	`

	result, err := s.store.ExecContext(ctx, query,
		claudeSessionID, promptNumber, promptText, matchedObservations,
		now.Format(time.RFC3339), now.UnixMilli(),
	)
	if err != nil {
		return 0, err
	}

	id, _ := result.LastInsertId()

	// Cleanup old prompts beyond the global limit
	deletedIDs, _ := s.CleanupOldPrompts(ctx)
	if len(deletedIDs) > 0 && s.cleanupFunc != nil {
		s.cleanupFunc(ctx, deletedIDs)
	}

	return id, nil
}

// CleanupOldPrompts deletes prompts beyond the global limit.
// Keeps the most recent MaxPromptsGlobal prompts.
// Returns the IDs of deleted prompts for downstream cleanup (e.g., vector DB).
func (s *PromptStore) CleanupOldPrompts(ctx context.Context) ([]int64, error) {
	// First, find IDs that will be deleted
	const selectQuery = `
		SELECT id FROM user_prompts
		WHERE id NOT IN (
			SELECT id FROM user_prompts
			ORDER BY created_at_epoch DESC
			LIMIT ?
		)
	`

	rows, err := s.store.QueryContext(ctx, selectQuery, MaxPromptsGlobal)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var toDelete []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		toDelete = append(toDelete, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(toDelete) == 0 {
		return nil, nil
	}

	// Delete the prompts
	const deleteQuery = `
		DELETE FROM user_prompts
		WHERE id NOT IN (
			SELECT id FROM user_prompts
			ORDER BY created_at_epoch DESC
			LIMIT ?
		)
	`

	_, err = s.store.ExecContext(ctx, deleteQuery, MaxPromptsGlobal)
	if err != nil {
		return nil, err
	}

	return toDelete, nil
}

// GetPromptsByIDs retrieves user prompts by a list of IDs.
func (s *PromptStore) GetPromptsByIDs(ctx context.Context, ids []int64, orderBy string, limit int) ([]*models.UserPromptWithSession, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	// Build query with placeholders
	// #nosec G202 -- query uses parameterized placeholders, not user input
	query := `
		SELECT up.id, up.claude_session_id, up.prompt_number, up.prompt_text,
		       up.created_at, up.created_at_epoch, s.project, s.sdk_session_id
		FROM user_prompts up
		JOIN sdk_sessions s ON up.claude_session_id = s.claude_session_id
		WHERE up.id IN (?` + repeatPlaceholders(len(ids)-1) + `)
		ORDER BY up.created_at_epoch `

	if orderBy == "date_asc" {
		query += "ASC"
	} else {
		query += "DESC"
	}

	if limit > 0 {
		query += " LIMIT ?"
	}

	// Convert []int64 to []interface{}
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	if limit > 0 {
		args = append(args, limit)
	}

	rows, err := s.store.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var prompts []*models.UserPromptWithSession
	for rows.Next() {
		var prompt models.UserPromptWithSession
		if err := rows.Scan(
			&prompt.ID, &prompt.ClaudeSessionID, &prompt.PromptNumber, &prompt.PromptText,
			&prompt.CreatedAt, &prompt.CreatedAtEpoch, &prompt.Project, &prompt.SDKSessionID,
		); err != nil {
			return nil, err
		}
		prompts = append(prompts, &prompt)
	}
	return prompts, rows.Err()
}

// GetAllRecentUserPrompts retrieves recent user prompts across all sessions.
func (s *PromptStore) GetAllRecentUserPrompts(ctx context.Context, limit int) ([]*models.UserPromptWithSession, error) {
	const query = `
		SELECT up.id, up.claude_session_id, up.prompt_number, up.prompt_text,
		       COALESCE(up.matched_observations, 0) as matched_observations,
		       up.created_at, up.created_at_epoch,
		       COALESCE(s.project, '') as project,
		       COALESCE(s.sdk_session_id, '') as sdk_session_id
		FROM user_prompts up
		LEFT JOIN sdk_sessions s ON up.claude_session_id = s.claude_session_id
		ORDER BY up.created_at_epoch DESC
		LIMIT ?
	`

	rows, err := s.store.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var prompts []*models.UserPromptWithSession
	for rows.Next() {
		var prompt models.UserPromptWithSession
		if err := rows.Scan(
			&prompt.ID, &prompt.ClaudeSessionID, &prompt.PromptNumber, &prompt.PromptText,
			&prompt.MatchedObservations, &prompt.CreatedAt, &prompt.CreatedAtEpoch,
			&prompt.Project, &prompt.SDKSessionID,
		); err != nil {
			return nil, err
		}
		prompts = append(prompts, &prompt)
	}
	return prompts, rows.Err()
}

// GetRecentUserPromptsByProject retrieves recent user prompts for a specific project.
func (s *PromptStore) GetRecentUserPromptsByProject(ctx context.Context, project string, limit int) ([]*models.UserPromptWithSession, error) {
	const query = `
		SELECT up.id, up.claude_session_id, up.prompt_number, up.prompt_text,
		       COALESCE(up.matched_observations, 0) as matched_observations,
		       up.created_at, up.created_at_epoch,
		       COALESCE(s.project, '') as project,
		       COALESCE(s.sdk_session_id, '') as sdk_session_id
		FROM user_prompts up
		LEFT JOIN sdk_sessions s ON up.claude_session_id = s.claude_session_id
		WHERE s.project = ?
		ORDER BY up.created_at_epoch DESC
		LIMIT ?
	`

	rows, err := s.store.QueryContext(ctx, query, project, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var prompts []*models.UserPromptWithSession
	for rows.Next() {
		var prompt models.UserPromptWithSession
		if err := rows.Scan(
			&prompt.ID, &prompt.ClaudeSessionID, &prompt.PromptNumber, &prompt.PromptText,
			&prompt.MatchedObservations, &prompt.CreatedAt, &prompt.CreatedAtEpoch,
			&prompt.Project, &prompt.SDKSessionID,
		); err != nil {
			return nil, err
		}
		prompts = append(prompts, &prompt)
	}
	return prompts, rows.Err()
}
