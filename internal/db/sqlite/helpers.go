// Package sqlite provides SQLite database operations for claude-mnemonic.
package sqlite

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
)

// EnsureSessionExists creates a session if it doesn't exist.
// This is shared between ObservationStore and SummaryStore to avoid duplication.
func EnsureSessionExists(ctx context.Context, store *Store, sdkSessionID, project string) error {
	const checkQuery = `SELECT id FROM sdk_sessions WHERE sdk_session_id = ?`
	var id int64
	err := store.QueryRowContext(ctx, checkQuery, sdkSessionID).Scan(&id)
	if err == nil {
		return nil // Session exists
	}
	if err != sql.ErrNoRows {
		return err
	}

	// Auto-create session
	now := time.Now()
	const insertQuery = `
		INSERT INTO sdk_sessions
		(claude_session_id, sdk_session_id, project, started_at, started_at_epoch, status)
		VALUES (?, ?, ?, ?, ?, 'active')
	`
	_, err = store.ExecContext(ctx, insertQuery,
		sdkSessionID, sdkSessionID, project,
		now.Format(time.RFC3339), now.UnixMilli(),
	)
	return err
}

// nullString converts a string to sql.NullString.
func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

// nullInt converts an int to sql.NullInt64.
func nullInt(i int) sql.NullInt64 {
	return sql.NullInt64{Int64: int64(i), Valid: i > 0}
}

// repeatPlaceholders generates n comma-prefixed placeholders for SQL IN clauses.
// e.g., repeatPlaceholders(2) returns ", ?, ?"
func repeatPlaceholders(n int) string {
	if n <= 0 {
		return ""
	}
	result := ""
	for i := 0; i < n; i++ {
		result += ", ?"
	}
	return result
}

// int64SliceToInterface converts []int64 to []interface{} for SQL queries.
func int64SliceToInterface(ids []int64) []interface{} {
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return args
}

// ParseLimitParam parses a limit query parameter with a default value.
func ParseLimitParam(r *http.Request, defaultLimit int) int {
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			return parsed
		}
	}
	return defaultLimit
}

// scanSummary scans a single summary from a row scanner.
func scanSummary(scanner interface{ Scan(...interface{}) error }) (*models.SessionSummary, error) {
	var summary models.SessionSummary
	if err := scanner.Scan(
		&summary.ID, &summary.SDKSessionID, &summary.Project,
		&summary.Request, &summary.Investigated, &summary.Learned, &summary.Completed,
		&summary.NextSteps, &summary.Notes, &summary.PromptNumber, &summary.DiscoveryTokens,
		&summary.CreatedAt, &summary.CreatedAtEpoch,
	); err != nil {
		return nil, err
	}
	return &summary, nil
}

// scanSummaryRows scans multiple summaries from rows.
func scanSummaryRows(rows *sql.Rows) ([]*models.SessionSummary, error) {
	var summaries []*models.SessionSummary
	for rows.Next() {
		summary, err := scanSummary(rows)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, summary)
	}
	return summaries, rows.Err()
}

// scanPromptWithSession scans a single prompt with session info from a row scanner.
func scanPromptWithSession(scanner interface{ Scan(...interface{}) error }) (*models.UserPromptWithSession, error) {
	var prompt models.UserPromptWithSession
	if err := scanner.Scan(
		&prompt.ID, &prompt.ClaudeSessionID, &prompt.PromptNumber, &prompt.PromptText,
		&prompt.MatchedObservations, &prompt.CreatedAt, &prompt.CreatedAtEpoch,
		&prompt.Project, &prompt.SDKSessionID,
	); err != nil {
		return nil, err
	}
	return &prompt, nil
}

// scanPromptWithSessionRows scans multiple prompts with session info from rows.
func scanPromptWithSessionRows(rows *sql.Rows) ([]*models.UserPromptWithSession, error) {
	var prompts []*models.UserPromptWithSession
	for rows.Next() {
		prompt, err := scanPromptWithSession(rows)
		if err != nil {
			return nil, err
		}
		prompts = append(prompts, prompt)
	}
	return prompts, rows.Err()
}

// BuildGetByIDsQuery builds a query for fetching records by IDs with optional ordering and limit.
// Returns the query string and args slice.
func BuildGetByIDsQuery(baseQuery string, ids []int64, orderBy string, limit int) (string, []interface{}) {
	// Build query with placeholders
	// #nosec G202 -- query uses parameterized placeholders, not user input
	query := baseQuery + ` WHERE id IN (?` + repeatPlaceholders(len(ids)-1) + `)
		ORDER BY created_at_epoch `

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

	return query, args
}
