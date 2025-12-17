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
// Uses INSERT OR IGNORE to be idempotent - duplicate (session, prompt_number) pairs are silently ignored.
// This prevents duplicate prompts when the user-prompt hook fires multiple times.
func (s *PromptStore) SaveUserPromptWithMatches(ctx context.Context, claudeSessionID string, promptNumber int, promptText string, matchedObservations int) (int64, error) {
	now := time.Now()

	// Use INSERT OR IGNORE for idempotency - if (claude_session_id, prompt_number) already exists,
	// the insert is silently ignored. This handles concurrent/duplicate hook invocations.
	const query = `
		INSERT OR IGNORE INTO user_prompts
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

	// If id is 0, the insert was ignored (duplicate) - fetch the existing ID
	if id == 0 {
		const selectQuery = `SELECT id FROM user_prompts WHERE claude_session_id = ? AND prompt_number = ?`
		row := s.store.QueryRowContext(ctx, selectQuery, claudeSessionID, promptNumber)
		if err := row.Scan(&id); err != nil {
			return 0, err
		}
		// Return existing ID without triggering cleanup (already handled when first inserted)
		return id, nil
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
		       COALESCE(up.matched_observations, 0) as matched_observations,
		       up.created_at, up.created_at_epoch,
		       COALESCE(s.project, '') as project,
		       COALESCE(s.sdk_session_id, '') as sdk_session_id
		FROM user_prompts up
		LEFT JOIN sdk_sessions s ON up.claude_session_id = s.claude_session_id
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

	args := int64SliceToInterface(ids)
	if limit > 0 {
		args = append(args, limit)
	}

	rows, err := s.store.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanPromptWithSessionRows(rows)
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

	return scanPromptWithSessionRows(rows)
}

// FindRecentPromptByText finds a prompt with the same text for a session within the last few seconds.
// This is used to detect duplicate hook invocations.
// Returns (promptID, promptNumber, found).
func (s *PromptStore) FindRecentPromptByText(ctx context.Context, claudeSessionID, promptText string, withinSeconds int) (int64, int, bool) {
	// Look for an existing prompt with the same text within the time window
	// This catches duplicate hook invocations that happen in quick succession
	const query = `
		SELECT id, prompt_number FROM user_prompts
		WHERE claude_session_id = ? AND prompt_text = ?
		AND created_at_epoch > ?
		ORDER BY created_at_epoch DESC
		LIMIT 1
	`

	cutoff := time.Now().Add(-time.Duration(withinSeconds) * time.Second).UnixMilli()

	var id int64
	var promptNumber int
	err := s.store.QueryRowContext(ctx, query, claudeSessionID, promptText, cutoff).Scan(&id, &promptNumber)
	if err != nil {
		return 0, 0, false
	}
	return id, promptNumber, true
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

	return scanPromptWithSessionRows(rows)
}
