// Package gorm provides GORM-based database operations for claude-mnemonic.
package gorm

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// EnsureSessionExists creates a session if it doesn't exist.
// Uses INSERT OR IGNORE pattern for atomic idempotent creation (single query instead of COUNT + INSERT).
// This is shared between stores to avoid duplication.
func EnsureSessionExists(ctx context.Context, db *gorm.DB, sdkSessionID, project string) error {
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

	// Use INSERT OR IGNORE - single query, atomic operation
	// If session already exists (conflict on sdk_session_id), do nothing
	return db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "sdk_session_id"}},
			DoNothing: true,
		}).
		Create(session).Error
}

// sqlNullString creates a sql.NullString from a string.
func sqlNullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: s, Valid: true}
}

// MaxPaginationLimit is the maximum allowed limit for pagination queries.
// This protects against resource exhaustion from excessively large requests.
const MaxPaginationLimit = 1000

// ParseLimitParam parses the "limit" query parameter from an HTTP request.
// Returns defaultLimit if the parameter is missing or invalid.
// Note: This does NOT enforce a maximum limit. Use ParseLimitParamWithMax for that.
func ParseLimitParam(r *http.Request, defaultLimit int) int {
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			return parsed
		}
	}
	return defaultLimit
}

// ParseLimitParamWithMax parses the "limit" query parameter with a maximum cap.
// Returns min(parsed, maxLimit) or defaultLimit if missing/invalid.
// If maxLimit is 0, uses MaxPaginationLimit (1000).
func ParseLimitParamWithMax(r *http.Request, defaultLimit, maxLimit int) int {
	if maxLimit <= 0 {
		maxLimit = MaxPaginationLimit
	}
	limit := ParseLimitParam(r, defaultLimit)
	if limit > maxLimit {
		return maxLimit
	}
	return limit
}

// ParseOffsetParam parses the "offset" query parameter from an HTTP request.
// Returns 0 if the parameter is missing or invalid.
func ParseOffsetParam(r *http.Request) int {
	if o := r.URL.Query().Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			return parsed
		}
	}
	return 0
}

// PaginationParams holds pagination parameters.
type PaginationParams struct {
	Limit  int
	Offset int
}

// ParsePaginationParams parses both limit and offset from an HTTP request.
func ParsePaginationParams(r *http.Request, defaultLimit int) PaginationParams {
	return PaginationParams{
		Limit:  ParseLimitParam(r, defaultLimit),
		Offset: ParseOffsetParam(r),
	}
}
