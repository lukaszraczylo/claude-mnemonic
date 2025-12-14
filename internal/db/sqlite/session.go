// Package sqlite provides SQLite database operations for claude-mnemonic.
package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
)

// SessionStore provides session-related database operations.
type SessionStore struct {
	store *Store
}

// NewSessionStore creates a new session store.
func NewSessionStore(store *Store) *SessionStore {
	return &SessionStore{store: store}
}

// CreateSDKSession creates a new SDK session (idempotent - returns existing ID if exists).
// This is the KEY to how claude-mnemonic stays unified across hooks.
func (s *SessionStore) CreateSDKSession(ctx context.Context, claudeSessionID, project, userPrompt string) (int64, error) {
	now := time.Now()

	// CRITICAL: INSERT OR IGNORE makes this idempotent
	const query = `
		INSERT OR IGNORE INTO sdk_sessions
		(claude_session_id, sdk_session_id, project, user_prompt, started_at, started_at_epoch, status)
		VALUES (?, ?, ?, ?, ?, ?, 'active')
	`

	result, err := s.store.ExecContext(ctx, query,
		claudeSessionID, claudeSessionID, project, userPrompt,
		now.Format(time.RFC3339), now.UnixMilli(),
	)
	if err != nil {
		return 0, err
	}

	// Check if insert happened
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		// Session exists - UPDATE project and user_prompt if we have non-empty values
		if project != "" {
			const updateQuery = `
				UPDATE sdk_sessions
				SET project = ?, user_prompt = ?
				WHERE claude_session_id = ?
			`
			_, _ = s.store.ExecContext(ctx, updateQuery, project, userPrompt, claudeSessionID)
		}

		// Fetch existing ID
		var id int64
		const selectQuery = `SELECT id FROM sdk_sessions WHERE claude_session_id = ? LIMIT 1`
		err := s.store.QueryRowContext(ctx, selectQuery, claudeSessionID).Scan(&id)
		return id, err
	}

	id, _ := result.LastInsertId()
	return id, nil
}

// GetSessionByID retrieves a session by its database ID.
func (s *SessionStore) GetSessionByID(ctx context.Context, id int64) (*models.SDKSession, error) {
	const query = `
		SELECT id, claude_session_id, sdk_session_id, project, user_prompt,
		       worker_port, prompt_counter, status, started_at, started_at_epoch,
		       completed_at, completed_at_epoch
		FROM sdk_sessions
		WHERE id = ?
		LIMIT 1
	`

	var sess models.SDKSession
	err := s.store.QueryRowContext(ctx, query, id).Scan(
		&sess.ID, &sess.ClaudeSessionID, &sess.SDKSessionID, &sess.Project, &sess.UserPrompt,
		&sess.WorkerPort, &sess.PromptCounter, &sess.Status, &sess.StartedAt, &sess.StartedAtEpoch,
		&sess.CompletedAt, &sess.CompletedAtEpoch,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &sess, nil
}

// FindAnySDKSession finds any session by Claude session ID (any status).
func (s *SessionStore) FindAnySDKSession(ctx context.Context, claudeSessionID string) (*models.SDKSession, error) {
	const query = `
		SELECT id, claude_session_id, sdk_session_id, project, user_prompt,
		       worker_port, prompt_counter, status, started_at, started_at_epoch,
		       completed_at, completed_at_epoch
		FROM sdk_sessions
		WHERE claude_session_id = ?
		LIMIT 1
	`

	var sess models.SDKSession
	err := s.store.QueryRowContext(ctx, query, claudeSessionID).Scan(
		&sess.ID, &sess.ClaudeSessionID, &sess.SDKSessionID, &sess.Project, &sess.UserPrompt,
		&sess.WorkerPort, &sess.PromptCounter, &sess.Status, &sess.StartedAt, &sess.StartedAtEpoch,
		&sess.CompletedAt, &sess.CompletedAtEpoch,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &sess, nil
}

// IncrementPromptCounter increments the prompt counter and returns the new value.
func (s *SessionStore) IncrementPromptCounter(ctx context.Context, id int64) (int, error) {
	const updateQuery = `
		UPDATE sdk_sessions
		SET prompt_counter = COALESCE(prompt_counter, 0) + 1
		WHERE id = ?
	`
	if _, err := s.store.ExecContext(ctx, updateQuery, id); err != nil {
		return 0, err
	}

	const selectQuery = `SELECT prompt_counter FROM sdk_sessions WHERE id = ?`
	var counter int
	err := s.store.QueryRowContext(ctx, selectQuery, id).Scan(&counter)
	return counter, err
}

// GetPromptCounter returns the current prompt counter for a session.
func (s *SessionStore) GetPromptCounter(ctx context.Context, id int64) (int, error) {
	const query = `SELECT COALESCE(prompt_counter, 0) FROM sdk_sessions WHERE id = ?`
	var counter int
	err := s.store.QueryRowContext(ctx, query, id).Scan(&counter)
	return counter, err
}

// GetSessionsToday returns the count of sessions started today.
func (s *SessionStore) GetSessionsToday(ctx context.Context) (int, error) {
	// Get start of today in milliseconds
	now := time.Now()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	startEpoch := startOfDay.UnixMilli()

	const query = `SELECT COUNT(*) FROM sdk_sessions WHERE started_at_epoch >= ?`

	var count int
	err := s.store.QueryRowContext(ctx, query, startEpoch).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// GetAllProjects returns all unique project names.
func (s *SessionStore) GetAllProjects(ctx context.Context) ([]string, error) {
	const query = `
		SELECT DISTINCT project
		FROM sdk_sessions
		WHERE project IS NOT NULL AND project != ''
		ORDER BY project ASC
	`

	rows, err := s.store.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var projects []string
	for rows.Next() {
		var project string
		if err := rows.Scan(&project); err != nil {
			return nil, err
		}
		projects = append(projects, project)
	}
	return projects, rows.Err()
}
