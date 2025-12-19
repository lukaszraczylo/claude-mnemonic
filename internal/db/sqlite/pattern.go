// Package sqlite provides SQLite database operations for claude-mnemonic.
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
)

// patternColumns is the standard list of columns to select for patterns.
const patternColumns = `id, name, type, description, signature, recommendation,
       frequency, projects, observation_ids, status, merged_into_id, confidence,
       last_seen_at, last_seen_at_epoch, created_at, created_at_epoch`

// PatternCleanupFunc is a callback for when patterns are deleted.
type PatternCleanupFunc func(ctx context.Context, deletedIDs []int64)

// PatternStore provides pattern-related database operations.
type PatternStore struct {
	store       *Store
	cleanupFunc PatternCleanupFunc
}

// NewPatternStore creates a new pattern store.
func NewPatternStore(store *Store) *PatternStore {
	return &PatternStore{store: store}
}

// SetCleanupFunc sets the callback for when patterns are deleted.
func (s *PatternStore) SetCleanupFunc(fn PatternCleanupFunc) {
	s.cleanupFunc = fn
}

// StorePattern stores a new pattern.
func (s *PatternStore) StorePattern(ctx context.Context, pattern *models.Pattern) (int64, error) {
	signatureJSON, _ := json.Marshal(pattern.Signature)
	projectsJSON, _ := json.Marshal(pattern.Projects)
	obsIDsJSON, _ := json.Marshal(pattern.ObservationIDs)

	const query = `
		INSERT INTO patterns
		(name, type, description, signature, recommendation, frequency, projects,
		 observation_ids, status, merged_into_id, confidence,
		 last_seen_at, last_seen_at_epoch, created_at, created_at_epoch)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	result, err := s.store.ExecContext(ctx, query,
		pattern.Name, string(pattern.Type),
		nullString(pattern.Description.String), string(signatureJSON),
		nullString(pattern.Recommendation.String),
		pattern.Frequency, string(projectsJSON), string(obsIDsJSON),
		string(pattern.Status), nullInt64(pattern.MergedIntoID),
		pattern.Confidence, pattern.LastSeenAt, pattern.LastSeenEpoch,
		pattern.CreatedAt, pattern.CreatedAtEpoch,
	)
	if err != nil {
		return 0, err
	}

	return result.LastInsertId()
}

// UpdatePattern updates an existing pattern.
func (s *PatternStore) UpdatePattern(ctx context.Context, pattern *models.Pattern) error {
	signatureJSON, _ := json.Marshal(pattern.Signature)
	projectsJSON, _ := json.Marshal(pattern.Projects)
	obsIDsJSON, _ := json.Marshal(pattern.ObservationIDs)

	const query = `
		UPDATE patterns SET
			name = ?, type = ?, description = ?, signature = ?, recommendation = ?,
			frequency = ?, projects = ?, observation_ids = ?, status = ?,
			merged_into_id = ?, confidence = ?, last_seen_at = ?, last_seen_at_epoch = ?
		WHERE id = ?
	`

	_, err := s.store.ExecContext(ctx, query,
		pattern.Name, string(pattern.Type),
		nullString(pattern.Description.String), string(signatureJSON),
		nullString(pattern.Recommendation.String),
		pattern.Frequency, string(projectsJSON), string(obsIDsJSON),
		string(pattern.Status), nullInt64(pattern.MergedIntoID),
		pattern.Confidence, pattern.LastSeenAt, pattern.LastSeenEpoch,
		pattern.ID,
	)
	return err
}

// GetPatternByID retrieves a pattern by ID.
func (s *PatternStore) GetPatternByID(ctx context.Context, id int64) (*models.Pattern, error) {
	query := `SELECT ` + patternColumns + ` FROM patterns WHERE id = ?`

	row := s.store.QueryRowContext(ctx, query, id)
	return scanPattern(row)
}

// GetPatternByName retrieves a pattern by name.
func (s *PatternStore) GetPatternByName(ctx context.Context, name string) (*models.Pattern, error) {
	query := `SELECT ` + patternColumns + ` FROM patterns WHERE name = ? AND status = 'active'`

	row := s.store.QueryRowContext(ctx, query, name)
	pattern, err := scanPattern(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return pattern, err
}

// GetActivePatterns retrieves all active patterns.
func (s *PatternStore) GetActivePatterns(ctx context.Context, limit int) ([]*models.Pattern, error) {
	query := `SELECT ` + patternColumns + `
		FROM patterns
		WHERE status = 'active'
		ORDER BY frequency DESC, confidence DESC
		LIMIT ?`

	rows, err := s.store.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanPatternRows(rows)
}

// GetPatternsByType retrieves patterns of a specific type.
func (s *PatternStore) GetPatternsByType(ctx context.Context, patternType models.PatternType, limit int) ([]*models.Pattern, error) {
	query := `SELECT ` + patternColumns + `
		FROM patterns
		WHERE type = ? AND status = 'active'
		ORDER BY frequency DESC, confidence DESC
		LIMIT ?`

	rows, err := s.store.QueryContext(ctx, query, string(patternType), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanPatternRows(rows)
}

// GetPatternsByProject retrieves patterns that have been observed in a specific project.
func (s *PatternStore) GetPatternsByProject(ctx context.Context, project string, limit int) ([]*models.Pattern, error) {
	// Use JSON path to search within the projects array
	query := `SELECT ` + patternColumns + `
		FROM patterns
		WHERE status = 'active'
		AND EXISTS (
			SELECT 1 FROM json_each(projects)
			WHERE json_each.value = ?
		)
		ORDER BY frequency DESC, confidence DESC
		LIMIT ?`

	rows, err := s.store.QueryContext(ctx, query, project, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanPatternRows(rows)
}

// FindMatchingPatterns searches for patterns that match a given signature.
func (s *PatternStore) FindMatchingPatterns(ctx context.Context, signature []string, minScore float64) ([]*models.Pattern, error) {
	// Get all active patterns and filter by signature match in Go
	// This is simpler than complex SQL for JSON array matching
	patterns, err := s.GetActivePatterns(ctx, 100)
	if err != nil {
		return nil, err
	}

	var matches []*models.Pattern
	for _, pattern := range patterns {
		score := models.CalculateMatchScore(signature, pattern.Signature)
		if score >= minScore {
			matches = append(matches, pattern)
		}
	}
	return matches, nil
}

// MarkPatternDeprecated marks a pattern as deprecated.
func (s *PatternStore) MarkPatternDeprecated(ctx context.Context, id int64) error {
	const query = `UPDATE patterns SET status = 'deprecated' WHERE id = ?`
	_, err := s.store.ExecContext(ctx, query, id)
	return err
}

// MergePatterns merges a source pattern into a target pattern.
func (s *PatternStore) MergePatterns(ctx context.Context, sourceID, targetID int64) error {
	// Get both patterns
	source, err := s.GetPatternByID(ctx, sourceID)
	if err != nil {
		return err
	}
	target, err := s.GetPatternByID(ctx, targetID)
	if err != nil {
		return err
	}

	// Merge source into target
	target.Frequency += source.Frequency
	for _, proj := range source.Projects {
		found := false
		for _, existing := range target.Projects {
			if existing == proj {
				found = true
				break
			}
		}
		if !found {
			target.Projects = append(target.Projects, proj)
		}
	}
	for _, obsID := range source.ObservationIDs {
		found := false
		for _, existing := range target.ObservationIDs {
			if existing == obsID {
				found = true
				break
			}
		}
		if !found {
			target.ObservationIDs = append(target.ObservationIDs, obsID)
		}
	}

	// Update target
	if err := s.UpdatePattern(ctx, target); err != nil {
		return err
	}

	// Mark source as merged
	source.Status = models.PatternStatusMerged
	source.MergedIntoID = sql.NullInt64{Int64: targetID, Valid: true}
	return s.UpdatePattern(ctx, source)
}

// DeletePattern deletes a pattern by ID.
func (s *PatternStore) DeletePattern(ctx context.Context, id int64) error {
	const query = `DELETE FROM patterns WHERE id = ?`
	_, err := s.store.ExecContext(ctx, query, id)
	if err == nil && s.cleanupFunc != nil {
		s.cleanupFunc(ctx, []int64{id})
	}
	return err
}

// SearchPatternsFTS performs full-text search on patterns.
func (s *PatternStore) SearchPatternsFTS(ctx context.Context, searchQuery string, limit int) ([]*models.Pattern, error) {
	query := `SELECT p.` + patternColumns + `
		FROM patterns p
		JOIN patterns_fts fts ON p.id = fts.rowid
		WHERE patterns_fts MATCH ?
		AND p.status = 'active'
		ORDER BY rank
		LIMIT ?`

	rows, err := s.store.QueryContext(ctx, query, searchQuery, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanPatternRows(rows)
}

// GetPatternStats returns statistics about patterns.
func (s *PatternStore) GetPatternStats(ctx context.Context) (*PatternStats, error) {
	const query = `
		SELECT
			COUNT(*) as total,
			COUNT(CASE WHEN status = 'active' THEN 1 END) as active,
			COUNT(CASE WHEN status = 'deprecated' THEN 1 END) as deprecated,
			COUNT(CASE WHEN status = 'merged' THEN 1 END) as merged,
			COALESCE(SUM(frequency), 0) as total_occurrences,
			COALESCE(AVG(confidence), 0) as avg_confidence,
			COUNT(CASE WHEN type = 'bug' THEN 1 END) as bugs,
			COUNT(CASE WHEN type = 'refactor' THEN 1 END) as refactors,
			COUNT(CASE WHEN type = 'architecture' THEN 1 END) as architectures,
			COUNT(CASE WHEN type = 'anti-pattern' THEN 1 END) as anti_patterns,
			COUNT(CASE WHEN type = 'best-practice' THEN 1 END) as best_practices
		FROM patterns
	`

	var stats PatternStats
	err := s.store.QueryRowContext(ctx, query).Scan(
		&stats.Total, &stats.Active, &stats.Deprecated, &stats.Merged,
		&stats.TotalOccurrences, &stats.AvgConfidence,
		&stats.Bugs, &stats.Refactors, &stats.Architectures,
		&stats.AntiPatterns, &stats.BestPractices,
	)
	return &stats, err
}

// PatternStats contains aggregate statistics about patterns.
type PatternStats struct {
	Total            int     `json:"total"`
	Active           int     `json:"active"`
	Deprecated       int     `json:"deprecated"`
	Merged           int     `json:"merged"`
	TotalOccurrences int     `json:"total_occurrences"`
	AvgConfidence    float64 `json:"avg_confidence"`
	Bugs             int     `json:"bugs"`
	Refactors        int     `json:"refactors"`
	Architectures    int     `json:"architectures"`
	AntiPatterns     int     `json:"anti_patterns"`
	BestPractices    int     `json:"best_practices"`
}

// scanPattern scans a single pattern from a row scanner.
func scanPattern(scanner interface{ Scan(...interface{}) error }) (*models.Pattern, error) {
	var pattern models.Pattern
	if err := scanner.Scan(
		&pattern.ID, &pattern.Name, &pattern.Type,
		&pattern.Description, &pattern.Signature, &pattern.Recommendation,
		&pattern.Frequency, &pattern.Projects, &pattern.ObservationIDs,
		&pattern.Status, &pattern.MergedIntoID, &pattern.Confidence,
		&pattern.LastSeenAt, &pattern.LastSeenEpoch,
		&pattern.CreatedAt, &pattern.CreatedAtEpoch,
	); err != nil {
		return nil, err
	}
	return &pattern, nil
}

// scanPatternRows scans multiple patterns from rows.
func scanPatternRows(rows *sql.Rows) ([]*models.Pattern, error) {
	var patterns []*models.Pattern
	for rows.Next() {
		pattern, err := scanPattern(rows)
		if err != nil {
			return nil, err
		}
		patterns = append(patterns, pattern)
	}
	return patterns, rows.Err()
}

// nullInt64 converts sql.NullInt64 to the value needed for database insertion.
func nullInt64(n sql.NullInt64) interface{} {
	if n.Valid {
		return n.Int64
	}
	return nil
}

// IncrementPatternFrequency atomically increments a pattern's frequency and updates last_seen.
func (s *PatternStore) IncrementPatternFrequency(ctx context.Context, id int64, project string, observationID int64) error {
	now := time.Now()

	// Get current pattern
	pattern, err := s.GetPatternByID(ctx, id)
	if err != nil {
		return err
	}

	// Add occurrence
	pattern.AddOccurrence(project, observationID)
	pattern.LastSeenAt = now.Format(time.RFC3339)
	pattern.LastSeenEpoch = now.UnixMilli()

	return s.UpdatePattern(ctx, pattern)
}
