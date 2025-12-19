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

// observationColumns is the standard list of columns to select for observations.
// This ensures consistency across all observation queries and includes importance scoring fields.
const observationColumns = `id, sdk_session_id, project, COALESCE(scope, 'project') as scope, type,
       title, subtitle, facts, narrative, concepts, files_read, files_modified, file_mtimes,
       prompt_number, discovery_tokens, created_at, created_at_epoch,
       COALESCE(importance_score, 1.0) as importance_score,
       COALESCE(user_feedback, 0) as user_feedback,
       COALESCE(retrieval_count, 0) as retrieval_count,
       last_retrieved_at_epoch, score_updated_at_epoch,
       COALESCE(is_superseded, 0) as is_superseded`

// CleanupFunc is a callback for when observations are cleaned up.
// Receives the IDs of deleted observations for downstream cleanup (e.g., vector DB).
type CleanupFunc func(ctx context.Context, deletedIDs []int64)

// ObservationStore provides observation-related database operations.
type ObservationStore struct {
	store         *Store
	cleanupFunc   CleanupFunc
	conflictStore *ConflictStore
	relationStore *RelationStore
}

// NewObservationStore creates a new observation store.
func NewObservationStore(store *Store) *ObservationStore {
	return &ObservationStore{store: store}
}

// SetCleanupFunc sets the callback for when observations are deleted during cleanup.
func (s *ObservationStore) SetCleanupFunc(fn CleanupFunc) {
	s.cleanupFunc = fn
}

// SetConflictStore sets the conflict store for conflict detection.
func (s *ObservationStore) SetConflictStore(conflictStore *ConflictStore) {
	s.conflictStore = conflictStore
}

// SetRelationStore sets the relation store for relationship detection.
func (s *ObservationStore) SetRelationStore(relationStore *RelationStore) {
	s.relationStore = relationStore
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

	// Detect conflicts with existing observations (async to not block handler)
	if s.conflictStore != nil && project != "" {
		go func(newObsID int64, proj string, parsedObs *models.ParsedObservation) {
			conflictCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			s.detectAndStoreConflicts(conflictCtx, newObsID, proj, parsedObs)
		}(id, project, obs)
	}

	// Detect relationships with existing observations (async to not block handler)
	if s.relationStore != nil && project != "" {
		go func(newObsID int64, proj string) {
			relationCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			s.detectAndStoreRelations(relationCtx, newObsID, proj)
		}(id, project)
	}

	return id, nowEpoch, nil
}

// detectAndStoreConflicts detects conflicts between a new observation and existing ones.
func (s *ObservationStore) detectAndStoreConflicts(ctx context.Context, newObsID int64, project string, parsedObs *models.ParsedObservation) {
	// Fetch the newly stored observation
	newObs, err := s.GetObservationByID(ctx, newObsID)
	if err != nil || newObs == nil {
		return
	}

	// Fetch recent observations from the same project to check for conflicts
	existing, err := s.GetRecentObservations(ctx, project, 50)
	if err != nil {
		return
	}

	// Detect conflicts
	conflicts := models.DetectConflictsWithExisting(newObs, existing)

	// Store conflicts and mark superseded observations
	var supersededIDs []int64
	for _, result := range conflicts {
		for _, olderID := range result.OlderObsIDs {
			conflict := models.NewObservationConflict(
				newObsID, olderID,
				result.Type, result.Resolution, result.Reason,
			)
			if _, err := s.conflictStore.StoreConflict(ctx, conflict); err != nil {
				continue
			}

			// If resolution is to prefer newer, mark older as superseded
			if result.Resolution == models.ResolutionPreferNewer {
				supersededIDs = append(supersededIDs, olderID)
			}
		}
	}

	// Mark superseded observations
	if len(supersededIDs) > 0 {
		_ = s.conflictStore.MarkObservationsSuperseded(ctx, supersededIDs)
	}

	// Cleanup old superseded observations (older than 3 days)
	deletedIDs, _ := s.conflictStore.CleanupSupersededObservations(ctx, project)
	if len(deletedIDs) > 0 && s.cleanupFunc != nil {
		s.cleanupFunc(ctx, deletedIDs)
	}
}

// MinRelationConfidence is the minimum confidence threshold for storing relations.
const MinRelationConfidence = 0.4

// detectAndStoreRelations detects relationships between a new observation and existing ones.
func (s *ObservationStore) detectAndStoreRelations(ctx context.Context, newObsID int64, project string) {
	// Fetch the newly stored observation
	newObs, err := s.GetObservationByID(ctx, newObsID)
	if err != nil || newObs == nil {
		return
	}

	// Fetch recent observations from the same project to check for relations
	existing, err := s.GetRecentObservations(ctx, project, 50)
	if err != nil {
		return
	}

	// Detect relationships using the models package detection logic
	results := models.DetectRelationsWithExisting(newObs, existing, MinRelationConfidence)
	if len(results) == 0 {
		return
	}

	// Convert detection results to relation objects
	relations := make([]*models.ObservationRelation, len(results))
	for i, r := range results {
		relations[i] = models.NewObservationRelation(
			r.SourceID, r.TargetID,
			r.RelationType, r.Confidence,
			r.DetectionSource, r.Reason,
		)
	}

	// Store all relations
	_ = s.relationStore.StoreRelations(ctx, relations)
}

// ensureSessionExists creates a session if it doesn't exist.
func (s *ObservationStore) ensureSessionExists(ctx context.Context, sdkSessionID, project string) error {
	return EnsureSessionExists(ctx, s.store, sdkSessionID, project)
}

// GetObservationByID retrieves an observation by ID.
func (s *ObservationStore) GetObservationByID(ctx context.Context, id int64) (*models.Observation, error) {
	query := `SELECT ` + observationColumns + ` FROM observations WHERE id = ?`

	obs, err := scanObservation(s.store.QueryRowContext(ctx, query, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return obs, err
}

// GetObservationsByIDs retrieves observations by a list of IDs.
// Results are ordered by importance_score DESC by default, with created_at_epoch as secondary sort.
func (s *ObservationStore) GetObservationsByIDs(ctx context.Context, ids []int64, orderBy string, limit int) ([]*models.Observation, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	// Build query with placeholders
	// #nosec G202 -- query uses parameterized placeholders, not user input
	query := `SELECT ` + observationColumns + `
		FROM observations
		WHERE id IN (?` + repeatPlaceholders(len(ids)-1) + `)
		ORDER BY `

	// Default to importance-based ordering
	switch orderBy {
	case "date_asc":
		query += "created_at_epoch ASC"
	case "date_desc":
		query += "created_at_epoch DESC"
	case "importance":
		query += "importance_score DESC, created_at_epoch DESC"
	default:
		// Default: importance first, then recency
		query += "COALESCE(importance_score, 1.0) DESC, created_at_epoch DESC"
	}

	if limit > 0 {
		query += " LIMIT ?"
	}

	// Convert []int64 to []interface{}
	args := int64SliceToInterface(ids)
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
// Results are ordered by importance_score DESC, then created_at_epoch DESC.
func (s *ObservationStore) GetRecentObservations(ctx context.Context, project string, limit int) ([]*models.Observation, error) {
	query := `SELECT ` + observationColumns + `
		FROM observations
		WHERE (project = ? AND (scope IS NULL OR scope = 'project'))
		   OR scope = 'global'
		ORDER BY COALESCE(importance_score, 1.0) DESC, created_at_epoch DESC
		LIMIT ?
	`

	rows, err := s.store.QueryContext(ctx, query, project, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanObservationRows(rows)
}

// GetActiveObservations retrieves recent non-superseded observations for a project.
// This excludes observations that have been marked as superseded by newer ones.
// Use this for context injection where you want to avoid outdated/contradicted advice.
// Results are ordered by importance_score DESC, then created_at_epoch DESC.
func (s *ObservationStore) GetActiveObservations(ctx context.Context, project string, limit int) ([]*models.Observation, error) {
	query := `SELECT ` + observationColumns + `
		FROM observations
		WHERE ((project = ? AND (scope IS NULL OR scope = 'project'))
		   OR scope = 'global')
		  AND COALESCE(is_superseded, 0) = 0
		ORDER BY COALESCE(importance_score, 1.0) DESC, created_at_epoch DESC
		LIMIT ?
	`

	rows, err := s.store.QueryContext(ctx, query, project, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanObservationRows(rows)
}

// GetSupersededObservations retrieves observations that have been superseded by newer ones.
// Use this for verification/debugging to see which observations were marked as outdated.
// Results are ordered by created_at_epoch DESC.
func (s *ObservationStore) GetSupersededObservations(ctx context.Context, project string, limit int) ([]*models.Observation, error) {
	query := `SELECT ` + observationColumns + `
		FROM observations
		WHERE project = ?
		  AND COALESCE(is_superseded, 0) = 1
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

// GetObservationsByProjectStrict retrieves observations strictly for a specific project.
// Unlike GetRecentObservations, this does NOT include global observations from other projects.
// Use this for dashboard filtering where the user expects to see only that project's data.
// Results are ordered by importance_score DESC, then created_at_epoch DESC.
func (s *ObservationStore) GetObservationsByProjectStrict(ctx context.Context, project string, limit int) ([]*models.Observation, error) {
	query := `SELECT ` + observationColumns + `
		FROM observations
		WHERE project = ?
		ORDER BY COALESCE(importance_score, 1.0) DESC, created_at_epoch DESC
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
// Results are ordered by importance_score DESC, then created_at_epoch DESC.
func (s *ObservationStore) GetAllRecentObservations(ctx context.Context, limit int) ([]*models.Observation, error) {
	query := `SELECT ` + observationColumns + `
		FROM observations
		ORDER BY COALESCE(importance_score, 1.0) DESC, created_at_epoch DESC
		LIMIT ?
	`

	rows, err := s.store.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanObservationRows(rows)
}

// GetAllObservations retrieves all observations (for vector rebuild).
func (s *ObservationStore) GetAllObservations(ctx context.Context) ([]*models.Observation, error) {
	query := `SELECT ` + observationColumns + `
		FROM observations
		ORDER BY id
	`

	rows, err := s.store.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanObservationRows(rows)
}

// SearchObservationsFTS performs full-text search on observations.
// Results are ordered by FTS rank (relevance), then by importance_score.
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
	// Include importance scoring columns and order by rank then importance
	ftsQuery := `
		SELECT o.id, o.sdk_session_id, o.project, COALESCE(o.scope, 'project') as scope, o.type,
		       o.title, o.subtitle, o.facts, o.narrative, o.concepts, o.files_read, o.files_modified,
		       o.file_mtimes, o.prompt_number, o.discovery_tokens, o.created_at, o.created_at_epoch,
		       COALESCE(o.importance_score, 1.0) as importance_score,
		       COALESCE(o.user_feedback, 0) as user_feedback,
		       COALESCE(o.retrieval_count, 0) as retrieval_count,
		       o.last_retrieved_at_epoch, o.score_updated_at_epoch,
		       COALESCE(o.is_superseded, 0) as is_superseded
		FROM observations o
		JOIN observations_fts fts ON o.id = fts.rowid
		WHERE observations_fts MATCH ?
		  AND (o.project = ? OR o.scope = 'global')
		ORDER BY rank, COALESCE(o.importance_score, 1.0) DESC
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
// Results are ordered by importance_score DESC, then created_at_epoch DESC.
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
	query := `SELECT ` + observationColumns + `
		FROM observations
		WHERE (` + strings.Join(conditions, " OR ") + `)
		  AND (project = ? OR scope = 'global')
		ORDER BY COALESCE(importance_score, 1.0) DESC, created_at_epoch DESC
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
		// Importance scoring fields
		&obs.ImportanceScore, &obs.UserFeedback, &obs.RetrievalCount,
		&obs.LastRetrievedAt, &obs.ScoreUpdatedAt,
		// Conflict detection fields
		&obs.IsSuperseded,
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

// Note: nullString, nullInt, and repeatPlaceholders are in helpers.go
