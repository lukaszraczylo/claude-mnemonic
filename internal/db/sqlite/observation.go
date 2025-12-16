// Package sqlite provides SQLite database operations for claude-mnemonic.
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
)

// CleanupFunc is a callback for when observations are cleaned up.
// Receives the IDs of deleted observations for downstream cleanup (e.g., vector DB).
type CleanupFunc func(ctx context.Context, deletedIDs []int64)

// ObservationStore provides observation-related database operations.
type ObservationStore struct {
	store       *Store
	cleanupFunc CleanupFunc
}

// NewObservationStore creates a new observation store.
func NewObservationStore(store *Store) *ObservationStore {
	return &ObservationStore{store: store}
}

// SetCleanupFunc sets the callback for when observations are deleted during cleanup.
func (s *ObservationStore) SetCleanupFunc(fn CleanupFunc) {
	s.cleanupFunc = fn
}

// StoreObservation stores a new observation.
func (s *ObservationStore) StoreObservation(ctx context.Context, sdkSessionID, project string, obs *models.ParsedObservation, promptNumber int, discoveryTokens int64) (int64, int64, error) {
	now := time.Now()
	nowEpoch := now.UnixMilli()

	// Ensure session exists (auto-create if missing)
	if err := s.ensureSessionExists(ctx, sdkSessionID, project); err != nil {
		return 0, 0, err
	}

	// Determine scope: use parsed scope if set, otherwise auto-determine from concepts
	scope := obs.Scope
	if scope == "" {
		scope = models.DetermineScope(obs.Concepts)
	}

	factsJSON, _ := json.Marshal(obs.Facts)
	conceptsJSON, _ := json.Marshal(obs.Concepts)
	filesReadJSON, _ := json.Marshal(obs.FilesRead)
	filesModifiedJSON, _ := json.Marshal(obs.FilesModified)
	fileMtimesJSON, _ := json.Marshal(obs.FileMtimes)

	const query = `
		INSERT INTO observations
		(sdk_session_id, project, scope, type, title, subtitle, facts, narrative, concepts,
		 files_read, files_modified, file_mtimes, prompt_number, discovery_tokens, created_at, created_at_epoch)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	result, err := s.store.ExecContext(ctx, query,
		sdkSessionID, project, string(scope), string(obs.Type),
		nullString(obs.Title), nullString(obs.Subtitle),
		string(factsJSON), nullString(obs.Narrative), string(conceptsJSON),
		string(filesReadJSON), string(filesModifiedJSON), string(fileMtimesJSON),
		nullInt(promptNumber), discoveryTokens,
		now.Format(time.RFC3339), nowEpoch,
	)
	if err != nil {
		return 0, 0, err
	}

	id, _ := result.LastInsertId()

	// Cleanup old observations beyond the limit for this project (async to not block handler)
	if project != "" {
		go func(proj string) {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			deletedIDs, _ := s.CleanupOldObservations(cleanupCtx, proj)
			if len(deletedIDs) > 0 && s.cleanupFunc != nil {
				s.cleanupFunc(cleanupCtx, deletedIDs)
			}
		}(project)
	}

	return id, nowEpoch, nil
}

// ensureSessionExists creates a session if it doesn't exist.
func (s *ObservationStore) ensureSessionExists(ctx context.Context, sdkSessionID, project string) error {
	const checkQuery = `SELECT id FROM sdk_sessions WHERE sdk_session_id = ?`
	var id int64
	err := s.store.QueryRowContext(ctx, checkQuery, sdkSessionID).Scan(&id)
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
	_, err = s.store.ExecContext(ctx, insertQuery,
		sdkSessionID, sdkSessionID, project,
		now.Format(time.RFC3339), now.UnixMilli(),
	)
	return err
}

// GetObservationByID retrieves an observation by ID.
func (s *ObservationStore) GetObservationByID(ctx context.Context, id int64) (*models.Observation, error) {
	const query = `
		SELECT id, sdk_session_id, project, COALESCE(scope, 'project') as scope, type, title, subtitle, facts, narrative,
		       concepts, files_read, files_modified, file_mtimes, prompt_number, discovery_tokens,
		       created_at, created_at_epoch
		FROM observations
		WHERE id = ?
	`

	obs, err := scanObservation(s.store.QueryRowContext(ctx, query, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return obs, err
}

// GetObservationsByIDs retrieves observations by a list of IDs.
func (s *ObservationStore) GetObservationsByIDs(ctx context.Context, ids []int64, orderBy string, limit int) ([]*models.Observation, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	// Build query with placeholders
	// #nosec G202 -- query uses parameterized placeholders, not user input
	query := `
		SELECT id, sdk_session_id, project, COALESCE(scope, 'project') as scope, type, title, subtitle, facts, narrative,
		       concepts, files_read, files_modified, file_mtimes, prompt_number, discovery_tokens,
		       created_at, created_at_epoch
		FROM observations
		WHERE id IN (?` + repeatPlaceholders(len(ids)-1) + `)
		ORDER BY created_at_epoch `

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

	return scanObservationRows(rows)
}

// GetRecentObservations retrieves recent observations for a project.
// This includes project-scoped observations for the specified project AND global observations.
func (s *ObservationStore) GetRecentObservations(ctx context.Context, project string, limit int) ([]*models.Observation, error) {
	const query = `
		SELECT id, sdk_session_id, project, COALESCE(scope, 'project') as scope, type, title, subtitle, facts, narrative,
		       concepts, files_read, files_modified, file_mtimes, prompt_number, discovery_tokens,
		       created_at, created_at_epoch
		FROM observations
		WHERE (project = ? AND (scope IS NULL OR scope = 'project'))
		   OR scope = 'global'
		ORDER BY created_at_epoch DESC
		LIMIT ?
	`

	rows, err := s.store.QueryContext(ctx, query, project, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanObservationRows(rows)
}

// GetObservationCount returns the count of observations for a project (including global).
func (s *ObservationStore) GetObservationCount(ctx context.Context, project string) (int, error) {
	const query = `
		SELECT COUNT(*) FROM observations
		WHERE project = ? OR scope = 'global'
	`
	var count int
	err := s.store.QueryRowContext(ctx, query, project).Scan(&count)
	return count, err
}

// GetAllRecentObservations retrieves recent observations across all projects.
func (s *ObservationStore) GetAllRecentObservations(ctx context.Context, limit int) ([]*models.Observation, error) {
	const query = `
		SELECT id, sdk_session_id, project, COALESCE(scope, 'project') as scope, type, title, subtitle, facts, narrative,
		       concepts, files_read, files_modified, file_mtimes, prompt_number, discovery_tokens,
		       created_at, created_at_epoch
		FROM observations
		ORDER BY created_at_epoch DESC
		LIMIT ?
	`

	rows, err := s.store.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanObservationRows(rows)
}

// SearchObservationsFTS performs full-text search on observations.
func (s *ObservationStore) SearchObservationsFTS(ctx context.Context, query, project string, limit int) ([]*models.Observation, error) {
	if limit <= 0 {
		limit = 10
	}

	// Extract keywords from the query (words > 3 chars, not common)
	keywords := extractKeywords(query)
	if len(keywords) == 0 {
		return nil, nil
	}

	// Build FTS5 query: keyword1 OR keyword2 OR keyword3
	ftsTerms := strings.Join(keywords, " OR ")

	// Use FTS5 to search title, subtitle, and narrative
	const ftsQuery = `
		SELECT o.id, o.sdk_session_id, o.project, COALESCE(o.scope, 'project') as scope, o.type,
		       o.title, o.subtitle, o.facts, o.narrative, o.concepts, o.files_read, o.files_modified,
		       o.file_mtimes, o.prompt_number, o.discovery_tokens, o.created_at, o.created_at_epoch
		FROM observations o
		JOIN observations_fts fts ON o.id = fts.rowid
		WHERE observations_fts MATCH ?
		  AND (o.project = ? OR o.scope = 'global')
		ORDER BY rank
		LIMIT ?
	`

	rows, err := s.store.QueryContext(ctx, ftsQuery, ftsTerms, project, limit)
	if err != nil {
		// FTS failed, try LIKE fallback
		return s.searchObservationsLike(ctx, keywords, project, limit)
	}
	defer rows.Close()

	observations, err := scanObservationRows(rows)
	if err != nil {
		return nil, err
	}

	// If FTS returned nothing, try LIKE search
	if len(observations) == 0 {
		return s.searchObservationsLike(ctx, keywords, project, limit)
	}

	return observations, nil
}

// searchObservationsLike performs fallback LIKE search on observations.
func (s *ObservationStore) searchObservationsLike(ctx context.Context, keywords []string, project string, limit int) ([]*models.Observation, error) {
	if len(keywords) == 0 {
		return nil, nil
	}

	// Build LIKE conditions for each keyword
	var conditions []string
	var args []interface{}

	for _, kw := range keywords {
		pattern := "%" + kw + "%"
		conditions = append(conditions, "(title LIKE ? OR subtitle LIKE ? OR narrative LIKE ?)")
		args = append(args, pattern, pattern, pattern)
	}

	// #nosec G202 -- query uses parameterized placeholders, not user input
	query := `
		SELECT id, sdk_session_id, project, COALESCE(scope, 'project') as scope, type,
		       title, subtitle, facts, narrative, concepts, files_read, files_modified,
		       file_mtimes, prompt_number, discovery_tokens, created_at, created_at_epoch
		FROM observations
		WHERE (` + strings.Join(conditions, " OR ") + `)
		  AND (project = ? OR scope = 'global')
		ORDER BY created_at_epoch DESC
		LIMIT ?
	`
	args = append(args, project, limit)

	rows, err := s.store.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanObservationRows(rows)
}

// extractKeywords extracts significant words from a query.
func extractKeywords(query string) []string {
	// Common words to skip
	stopWords := map[string]bool{
		"what": true, "is": true, "the": true, "a": true, "an": true,
		"how": true, "does": true, "do": true, "can": true, "could": true,
		"would": true, "should": true, "where": true, "when": true, "why": true,
		"which": true, "who": true, "this": true, "that": true, "these": true,
		"those": true, "it": true, "its": true, "for": true, "from": true,
		"with": true, "about": true, "into": true, "through": true, "during": true,
		"before": true, "after": true, "above": true, "below": true, "to": true,
		"of": true, "in": true, "on": true, "at": true, "by": true, "and": true,
		"or": true, "but": true, "if": true, "then": true, "else": true,
		"function": true, "method": true, "class": true, "file": true,
		"code": true, "work": true, "works": true, "working": true,
		"please": true, "help": true, "me": true, "my": true, "i": true,
		"tell": true, "show": true, "explain": true, "describe": true,
	}

	// Split and filter
	words := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_')
	})

	var keywords []string
	seen := make(map[string]bool)

	for _, word := range words {
		// Skip short words, stop words, and duplicates
		if len(word) < 4 || stopWords[word] || seen[word] {
			continue
		}
		seen[word] = true
		keywords = append(keywords, word)
	}

	return keywords
}

// ExistsSimilarObservation checks if an observation about the same files exists for a project.
// Used to prevent duplicate observations when re-reading the same files.
func (s *ObservationStore) ExistsSimilarObservation(ctx context.Context, project string, filesRead, filesModified []string) (bool, error) {
	// If no files tracked, can't deduplicate
	if len(filesRead) == 0 && len(filesModified) == 0 {
		return false, nil
	}

	// Check if any observation exists with the same primary file
	// Use the first file as the key identifier
	var primaryFile string
	if len(filesRead) > 0 {
		primaryFile = filesRead[0]
	} else if len(filesModified) > 0 {
		primaryFile = filesModified[0]
	}

	const query = `
		SELECT COUNT(*) FROM observations
		WHERE project = ? AND (files_read LIKE ? OR files_modified LIKE ?)
	`
	pattern := "%" + primaryFile + "%"

	var count int
	err := s.store.QueryRowContext(ctx, query, project, pattern, pattern).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// DeleteObservations deletes multiple observations by ID.
func (s *ObservationStore) DeleteObservations(ctx context.Context, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}

	query := `DELETE FROM observations WHERE id IN (?` + repeatPlaceholders(len(ids)-1) + `)` // #nosec G202 -- uses parameterized placeholders

	args := make([]interface{}, len(ids))
	for i, id := range ids {
		args[i] = id
	}

	result, err := s.store.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// MaxObservationsPerProject is the hard limit of observations per project.
const MaxObservationsPerProject = 100

// CleanupOldObservations deletes observations beyond the limit for a project.
// Keeps the most recent MaxObservationsPerProject observations per project.
// Returns the IDs of deleted observations for downstream cleanup (e.g., vector DB).
func (s *ObservationStore) CleanupOldObservations(ctx context.Context, project string) ([]int64, error) {
	// First, find IDs that will be deleted
	const selectQuery = `
		SELECT id FROM observations
		WHERE project = ? AND id NOT IN (
			SELECT id FROM observations
			WHERE project = ?
			ORDER BY created_at_epoch DESC
			LIMIT ?
		)
	`

	rows, err := s.store.QueryContext(ctx, selectQuery, project, project, MaxObservationsPerProject)
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

	// Delete the observations
	const deleteQuery = `
		DELETE FROM observations
		WHERE project = ? AND id NOT IN (
			SELECT id FROM observations
			WHERE project = ?
			ORDER BY created_at_epoch DESC
			LIMIT ?
		)
	`

	_, err = s.store.ExecContext(ctx, deleteQuery, project, project, MaxObservationsPerProject)
	if err != nil {
		return nil, err
	}

	return toDelete, nil
}

// Helper functions

// scanObservation scans a single observation from a row scanner.
// This reduces code duplication across all observation query methods.
func scanObservation(scanner interface{ Scan(...interface{}) error }) (*models.Observation, error) {
	var obs models.Observation
	if err := scanner.Scan(
		&obs.ID, &obs.SDKSessionID, &obs.Project, &obs.Scope, &obs.Type,
		&obs.Title, &obs.Subtitle, &obs.Facts, &obs.Narrative,
		&obs.Concepts, &obs.FilesRead, &obs.FilesModified, &obs.FileMtimes,
		&obs.PromptNumber, &obs.DiscoveryTokens,
		&obs.CreatedAt, &obs.CreatedAtEpoch,
	); err != nil {
		return nil, err
	}
	return &obs, nil
}

// scanObservationRows scans multiple observations from rows.
// Caller must close rows after calling this function.
func scanObservationRows(rows *sql.Rows) ([]*models.Observation, error) {
	var observations []*models.Observation
	for rows.Next() {
		obs, err := scanObservation(rows)
		if err != nil {
			return nil, err
		}
		observations = append(observations, obs)
	}
	return observations, rows.Err()
}

func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

func nullInt(i int) sql.NullInt64 {
	return sql.NullInt64{Int64: int64(i), Valid: i > 0}
}

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
