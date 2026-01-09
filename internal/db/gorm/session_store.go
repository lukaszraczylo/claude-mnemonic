// Package gorm provides GORM-based database operations for claude-mnemonic.
package gorm

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
)

// SessionStore provides session-related database operations using GORM.
type SessionStore struct {
	db *gorm.DB
}

// NewSessionStore creates a new session store.
func NewSessionStore(store *Store) *SessionStore {
	return &SessionStore{db: store.DB}
}

// CreateSDKSession creates a new SDK session (idempotent - returns existing ID if exists).
// This is the KEY to how claude-mnemonic stays unified across hooks.
func (s *SessionStore) CreateSDKSession(ctx context.Context, claudeSessionID, project, userPrompt string) (int64, error) {
	now := time.Now()

	session := &SDKSession{
		ClaudeSessionID: claudeSessionID,
		SDKSessionID: func() sql.NullString {
			return sql.NullString{String: claudeSessionID, Valid: true}
		}(),
		Project: project,
		UserPrompt: func() sql.NullString {
			if userPrompt != "" {
				return sql.NullString{String: userPrompt, Valid: true}
			}
			return sql.NullString{Valid: false}
		}(),
		Status:         "active",
		StartedAt:      now.Format(time.RFC3339),
		StartedAtEpoch: now.UnixMilli(),
	}

	// CRITICAL: INSERT OR IGNORE makes this idempotent
	// Use OnConflict with DoNothing to achieve INSERT OR IGNORE behavior
	result := s.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "claude_session_id"}},
			DoNothing: true,
		}).
		Create(session)

	if result.Error != nil {
		return 0, result.Error
	}

	// Check if insert happened
	if result.RowsAffected == 0 {
		// Session exists - UPDATE project and user_prompt if we have non-empty values
		if project != "" {
			updates := map[string]interface{}{
				"project": project,
			}
			if userPrompt != "" {
				updates["user_prompt"] = userPrompt
			}
			if err := s.db.WithContext(ctx).
				Model(&SDKSession{}).
				Where("claude_session_id = ?", claudeSessionID).
				Updates(updates).Error; err != nil {
				return 0, fmt.Errorf("failed to update session: %w", err)
			}
		}

		// Fetch existing session
		var existing SDKSession
		err := s.db.WithContext(ctx).
			Where("claude_session_id = ?", claudeSessionID).
			First(&existing).Error
		if err != nil {
			return 0, err
		}
		return existing.ID, nil
	}

	return session.ID, nil
}

// GetSessionByID retrieves a session by its database ID.
func (s *SessionStore) GetSessionByID(ctx context.Context, id int64) (*models.SDKSession, error) {
	var sess SDKSession
	err := s.db.WithContext(ctx).First(&sess, id).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return toModelSDKSession(&sess), nil
}

// FindAnySDKSession finds any session by Claude session ID (any status).
func (s *SessionStore) FindAnySDKSession(ctx context.Context, claudeSessionID string) (*models.SDKSession, error) {
	var sess SDKSession
	err := s.db.WithContext(ctx).
		Where("claude_session_id = ?", claudeSessionID).
		First(&sess).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return toModelSDKSession(&sess), nil
}

// IncrementPromptCounter increments the prompt counter and returns the new value.
func (s *SessionStore) IncrementPromptCounter(ctx context.Context, id int64) (int, error) {
	// Atomic increment using GORM expression
	err := s.db.WithContext(ctx).
		Model(&SDKSession{}).
		Where("id = ?", id).
		Update("prompt_counter", gorm.Expr("COALESCE(prompt_counter, 0) + 1")).Error
	if err != nil {
		return 0, err
	}

	// Fetch updated value
	var sess SDKSession
	err = s.db.WithContext(ctx).
		Select("prompt_counter").
		First(&sess, id).Error
	if err != nil {
		return 0, err
	}

	return sess.PromptCounter, nil
}

// GetPromptCounter returns the current prompt counter for a session.
func (s *SessionStore) GetPromptCounter(ctx context.Context, id int64) (int, error) {
	var sess SDKSession
	err := s.db.WithContext(ctx).
		Select("prompt_counter").
		First(&sess, id).Error
	if err != nil {
		return 0, err
	}
	return sess.PromptCounter, nil
}

// GetSessionsToday returns the count of sessions started today.
func (s *SessionStore) GetSessionsToday(ctx context.Context) (int, error) {
	// Get start of today in milliseconds
	now := time.Now()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	startEpoch := startOfDay.UnixMilli()

	var count int64
	err := s.db.WithContext(ctx).
		Model(&SDKSession{}).
		Where("started_at_epoch >= ?", startEpoch).
		Count(&count).Error

	return int(count), err
}

// GetAllProjects returns all unique project names.
func (s *SessionStore) GetAllProjects(ctx context.Context) ([]string, error) {
	var projects []string
	err := s.db.WithContext(ctx).
		Model(&SDKSession{}).
		Distinct("project").
		Where("project IS NOT NULL AND project != ''").
		Order("project ASC").
		Pluck("project", &projects).Error

	return projects, err
}

// toModelSDKSession converts a GORM SDKSession to pkg/models.SDKSession.
func toModelSDKSession(sess *SDKSession) *models.SDKSession {
	return &models.SDKSession{
		ID:               sess.ID,
		ClaudeSessionID:  sess.ClaudeSessionID,
		SDKSessionID:     sess.SDKSessionID,
		Project:          sess.Project,
		UserPrompt:       sess.UserPrompt,
		WorkerPort:       sess.WorkerPort,
		PromptCounter:    int64(sess.PromptCounter),
		Status:           models.SessionStatus(sess.Status),
		StartedAt:        sess.StartedAt,
		StartedAtEpoch:   sess.StartedAtEpoch,
		CompletedAt:      sess.CompletedAt,
		CompletedAtEpoch: sess.CompletedAtEpoch,
	}
}
