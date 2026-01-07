// Package gorm provides GORM-based database operations for claude-mnemonic.
package gorm

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"gorm.io/gorm"
)

// EnsureSessionExists creates a session if it doesn't exist.
// This is shared between stores to avoid duplication.
func EnsureSessionExists(ctx context.Context, db *gorm.DB, sdkSessionID, project string) error {
	// Check if session exists
	var count int64
	err := db.WithContext(ctx).
		Model(&SDKSession{}).
		Where("sdk_session_id = ?", sdkSessionID).
		Count(&count).Error

	if err != nil {
		return err
	}

	if count > 0 {
		return nil // Session exists
	}

	// Auto-create session
	now := time.Now()
	session := &SDKSession{
		ClaudeSessionID: sdkSessionID,
		SDKSessionID:    sqlNullString(sdkSessionID),
		Project:         project,
		Status:          "active",
		StartedAt:       now.Format(time.RFC3339),
		StartedAtEpoch:  now.UnixMilli(),
		PromptCounter:   0,
	}

	return db.WithContext(ctx).Create(session).Error
}

// sqlNullString creates a sql.NullString from a string.
func sqlNullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: s, Valid: true}
}

// ParseLimitParam parses the "limit" query parameter from an HTTP request.
// Returns defaultLimit if the parameter is missing or invalid.
func ParseLimitParam(r *http.Request, defaultLimit int) int {
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			return parsed
		}
	}
	return defaultLimit
}
