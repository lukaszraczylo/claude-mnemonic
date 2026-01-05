// Package gorm provides GORM-based database operations for claude-mnemonic.
package gorm

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
)

// MaxObservationsPerProject is the maximum number of observations to keep per project.
const MaxObservationsPerProject = 100

// CleanupFunc is a callback for when observations are cleaned up.
// Receives the IDs of deleted observations for downstream cleanup (e.g., vector DB).
type CleanupFunc func(ctx context.Context, deletedIDs []int64)

// ObservationStore provides observation-related database operations using GORM.
type ObservationStore struct {
	db            *gorm.DB
	rawDB         *sql.DB
	cleanupFunc   CleanupFunc
	conflictStore interface{} // Placeholder for ConflictStore (Phase 4)
	relationStore interface{} // Placeholder for RelationStore (Phase 4)
}

// NewObservationStore creates a new observation store.
// The conflictStore and relationStore parameters are optional (can be nil) and will be used in Phase 4.
func NewObservationStore(store *Store, cleanupFunc CleanupFunc, conflictStore, relationStore interface{}) *ObservationStore {
	return &ObservationStore{
		db:            store.DB,
		rawDB:         store.GetRawDB(),
		cleanupFunc:   cleanupFunc,
		conflictStore: conflictStore,
		relationStore: relationStore,
	}
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
	if err := EnsureSessionExists(ctx, s.db, sdkSessionID, project); err != nil {
		return 0, 0, err
	}

	// Determine scope: use parsed scope if set, otherwise auto-determine from concepts
	scope := obs.Scope
	if scope == "" {
		scope = models.DetermineScope(obs.Concepts)
	}

	dbObs := &Observation{
		SDKSessionID:    sdkSessionID,
		Project:         project,
		Scope:           scope,
		Type:            obs.Type,
		Title:           nullString(obs.Title),
		Subtitle:        nullString(obs.Subtitle),
		Facts:           models.JSONStringArray(obs.Facts),
		Narrative:       nullString(obs.Narrative),
		Concepts:        models.JSONStringArray(obs.Concepts),
		FilesRead:       models.JSONStringArray(obs.FilesRead),
		FilesModified:   models.JSONStringArray(obs.FilesModified),
		FileMtimes:      models.JSONInt64Map(obs.FileMtimes),
		PromptNumber:    nullInt64(promptNumber),
		DiscoveryTokens: discoveryTokens,
		CreatedAt:       now.Format(time.RFC3339),
		CreatedAtEpoch:  nowEpoch,
	}

	err := s.db.WithContext(ctx).Create(dbObs).Error
	if err != nil {
		return 0, 0, err
	}

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

	// Note: Conflict and relation detection intentionally omitted for now
	// Will be added in Phase 4 when ConflictStore and RelationStore are implemented

	return dbObs.ID, nowEpoch, nil
}

// GetObservationByID retrieves an observation by its ID.
func (s *ObservationStore) GetObservationByID(ctx context.Context, id int64) (*models.Observation, error) {
	var dbObs Observation
	err := s.db.WithContext(ctx).First(&dbObs, id).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return toModelObservation(&dbObs), nil
}

// GetObservationsByIDs retrieves observations by a list of IDs.
func (s *ObservationStore) GetObservationsByIDs(ctx context.Context, ids []int64, orderBy string, limit int) ([]*models.Observation, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	var dbObservations []Observation
	query := s.db.WithContext(ctx).Where("id IN ?", ids)

	// Apply ordering
	switch orderBy {
	case "date_asc":
		query = query.Order("created_at_epoch ASC")
	case "date_desc":
		query = query.Order("created_at_epoch DESC")
	case "importance":
		query = query.Order("importance_score DESC, created_at_epoch DESC")
	default:
		// Default: importance first, then recency
		query = query.Order("COALESCE(importance_score, 1.0) DESC, created_at_epoch DESC")
	}

	// Apply limit
	if limit > 0 {
		query = query.Limit(limit)
	}

	err := query.Find(&dbObservations).Error
	if err != nil {
		return nil, err
	}

	return toModelObservations(dbObservations), nil
}

// GetRecentObservations retrieves recent observations for a project.
// This includes project-scoped observations for the specified project AND global observations.
// Results are ordered by importance_score DESC, then created_at_epoch DESC.
func (s *ObservationStore) GetRecentObservations(ctx context.Context, project string, limit int) ([]*models.Observation, error) {
	var dbObservations []Observation
	err := s.db.WithContext(ctx).
		Scopes(projectScopeFilter(project), importanceOrdering()).
		Limit(limit).
		Find(&dbObservations).Error

	if err != nil {
		return nil, err
	}

	return toModelObservations(dbObservations), nil
}

// GetActiveObservations retrieves recent non-superseded observations for a project.
// This excludes observations that have been marked as superseded by newer ones.
// Results are ordered by importance_score DESC, then created_at_epoch DESC.
func (s *ObservationStore) GetActiveObservations(ctx context.Context, project string, limit int) ([]*models.Observation, error) {
	var dbObservations []Observation
	err := s.db.WithContext(ctx).
		Scopes(projectScopeFilter(project), notSupersededFilter(), importanceOrdering()).
		Limit(limit).
		Find(&dbObservations).Error

	if err != nil {
		return nil, err
	}

	return toModelObservations(dbObservations), nil
}

// GetSupersededObservations retrieves observations that have been superseded by newer ones.
// Results are ordered by created_at_epoch DESC.
func (s *ObservationStore) GetSupersededObservations(ctx context.Context, project string, limit int) ([]*models.Observation, error) {
	var dbObservations []Observation
	err := s.db.WithContext(ctx).
		Where("project = ? AND COALESCE(is_superseded, 0) = 1", project).
		Order("created_at_epoch DESC").
		Limit(limit).
		Find(&dbObservations).Error

	if err != nil {
		return nil, err
	}

	return toModelObservations(dbObservations), nil
}

// GetObservationsByProjectStrict retrieves observations for a project (strict - no global observations).
func (s *ObservationStore) GetObservationsByProjectStrict(ctx context.Context, project string, limit int) ([]*models.Observation, error) {
	var dbObservations []Observation
	err := s.db.WithContext(ctx).
		Where("project = ?", project).
		Scopes(importanceOrdering()).
		Limit(limit).
		Find(&dbObservations).Error

	if err != nil {
		return nil, err
	}

	return toModelObservations(dbObservations), nil
}

// GetObservationCount returns the count of observations for a project.
func (s *ObservationStore) GetObservationCount(ctx context.Context, project string) (int, error) {
	var count int64
	err := s.db.WithContext(ctx).
		Model(&Observation{}).
		Where("project = ?", project).
		Count(&count).Error

	return int(count), err
}

// GetAllRecentObservations retrieves recent observations across all projects.
func (s *ObservationStore) GetAllRecentObservations(ctx context.Context, limit int) ([]*models.Observation, error) {
	var dbObservations []Observation
	err := s.db.WithContext(ctx).
		Scopes(importanceOrdering()).
		Limit(limit).
		Find(&dbObservations).Error

	if err != nil {
		return nil, err
	}

	return toModelObservations(dbObservations), nil
}

// GetAllObservations retrieves all observations (for vector rebuild).
func (s *ObservationStore) GetAllObservations(ctx context.Context) ([]*models.Observation, error) {
	var dbObservations []Observation
	err := s.db.WithContext(ctx).
		Order("id").
		Find(&dbObservations).Error

	if err != nil {
		return nil, err
	}

	return toModelObservations(dbObservations), nil
}

// SearchObservationsFTS performs full-text search on observations using FTS5.
// Falls back to LIKE search if FTS5 fails.
func (s *ObservationStore) SearchObservationsFTS(ctx context.Context, query, project string, limit int) ([]*models.Observation, error) {
	if limit <= 0 {
		limit = 10
	}

	// Extract keywords from the query
	keywords := extractKeywords(query)
	if len(keywords) == 0 {
		return nil, nil
	}

	// Build FTS5 query: keyword1 OR keyword2 OR keyword3
	ftsTerms := strings.Join(keywords, " OR ")

	// Use FTS5 via raw SQL (GORM can't handle FTS5 MATCH operator)
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

	rows, err := s.rawDB.QueryContext(ctx, ftsQuery, ftsTerms, project, limit)
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

// searchObservationsLike performs fallback LIKE search on observations using GORM.
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

	// Build WHERE clause
	whereClause := strings.Join(conditions, " OR ")
	fullWhere := "(" + whereClause + ") AND (project = ? OR scope = 'global')"
	args = append(args, project)

	var dbObservations []Observation
	err := s.db.WithContext(ctx).
		Where(fullWhere, args...).
		Scopes(importanceOrdering()).
		Limit(limit).
		Find(&dbObservations).Error

	if err != nil {
		return nil, err
	}

	return toModelObservations(dbObservations), nil
}

// DeleteObservations deletes observations by IDs.
func (s *ObservationStore) DeleteObservations(ctx context.Context, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}

	result := s.db.WithContext(ctx).Delete(&Observation{}, ids)
	return result.RowsAffected, result.Error
}

// CleanupOldObservations removes observations beyond the limit for a project.
// Returns the IDs of deleted observations.
func (s *ObservationStore) CleanupOldObservations(ctx context.Context, project string) ([]int64, error) {
	// Use a transaction to prevent TOCTOU race condition
	var idsToDelete []int64

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Find IDs to keep (most recent MaxObservationsPerProject)
		var idsToKeep []int64
		err := tx.Model(&Observation{}).
			Where("project = ?", project).
			Order("created_at_epoch DESC").
			Limit(MaxObservationsPerProject).
			Pluck("id", &idsToKeep).Error

		if err != nil {
			return err
		}

		if len(idsToKeep) == 0 {
			return nil
		}

		// Find IDs to delete (all IDs not in the keep list)
		// This happens in the same transaction to prevent race conditions
		err = tx.Model(&Observation{}).
			Where("project = ? AND id NOT IN ?", project, idsToKeep).
			Pluck("id", &idsToDelete).Error

		if err != nil {
			return err
		}

		if len(idsToDelete) == 0 {
			return nil
		}

		// Delete the observations
		return tx.Delete(&Observation{}, idsToDelete).Error
	})

	if err != nil {
		return nil, err
	}

	return idsToDelete, nil
}

// ====================
// GORM Scopes (Reusable Query Filters)
// ====================

// projectScopeFilter filters observations by project scope.
// Includes project-scoped observations for the specified project AND global observations.
func projectScopeFilter(project string) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		return db.Where("(project = ? AND (scope IS NULL OR scope = 'project')) OR scope = 'global'", project)
	}
}

// notSupersededFilter filters out superseded observations.
func notSupersededFilter() func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		return db.Where("COALESCE(is_superseded, 0) = 0")
	}
}

// importanceOrdering orders by importance score DESC, then created_at_epoch DESC.
func importanceOrdering() func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		return db.Order("COALESCE(importance_score, 1.0) DESC, created_at_epoch DESC")
	}
}

// ====================
// Helper Functions
// ====================

// extractKeywords extracts keywords from a search query.
func extractKeywords(query string) []string {
	words := strings.Fields(strings.ToLower(query))
	var keywords []string

	commonWords := map[string]bool{
		"the": true, "and": true, "or": true, "but": true, "in": true,
		"on": true, "at": true, "to": true, "for": true, "of": true,
		"with": true, "by": true, "from": true, "as": true, "is": true,
		"was": true, "are": true, "were": true, "be": true, "been": true,
		"being": true, "have": true, "has": true, "had": true, "do": true,
		"does": true, "did": true, "will": true, "would": true, "should": true,
		"could": true, "may": true, "might": true, "must": true, "can": true,
	}

	for _, word := range words {
		// Skip short words and common words
		if len(word) <= 3 || commonWords[word] {
			continue
		}
		keywords = append(keywords, word)
	}

	return keywords
}

// scanObservationRows scans multiple observations from raw SQL rows.
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

// scanObservation scans a single observation from a row scanner.
func scanObservation(scanner interface{ Scan(...interface{}) error }) (*models.Observation, error) {
	var obs models.Observation
	var factsJSON, conceptsJSON, filesReadJSON, filesModifiedJSON, fileMtimesJSON []byte
	var isSuperseded int

	err := scanner.Scan(
		&obs.ID, &obs.SDKSessionID, &obs.Project, &obs.Scope, &obs.Type,
		&obs.Title, &obs.Subtitle, &factsJSON, &obs.Narrative, &conceptsJSON,
		&filesReadJSON, &filesModifiedJSON, &fileMtimesJSON,
		&obs.PromptNumber, &obs.DiscoveryTokens, &obs.CreatedAt, &obs.CreatedAtEpoch,
		&obs.ImportanceScore, &obs.UserFeedback, &obs.RetrievalCount,
		&obs.LastRetrievedAt, &obs.ScoreUpdatedAt, &isSuperseded,
	)
	if err != nil {
		return nil, err
	}

	// Unmarshal JSON fields (data comes from DB, should always be valid)
	if len(factsJSON) > 0 {
		_ = json.Unmarshal(factsJSON, &obs.Facts)
	}
	if len(conceptsJSON) > 0 {
		_ = json.Unmarshal(conceptsJSON, &obs.Concepts)
	}
	if len(filesReadJSON) > 0 {
		_ = json.Unmarshal(filesReadJSON, &obs.FilesRead)
	}
	if len(filesModifiedJSON) > 0 {
		_ = json.Unmarshal(filesModifiedJSON, &obs.FilesModified)
	}
	if len(fileMtimesJSON) > 0 {
		_ = json.Unmarshal(fileMtimesJSON, &obs.FileMtimes)
	}

	// Convert int to bool for IsSuperseded
	obs.IsSuperseded = isSuperseded != 0

	return &obs, nil
}

// toModelObservation converts a GORM Observation to pkg/models.Observation.
func toModelObservation(o *Observation) *models.Observation {
	return &models.Observation{
		ID:              o.ID,
		SDKSessionID:    o.SDKSessionID,
		Project:         o.Project,
		Scope:           o.Scope,
		Type:            o.Type,
		Title:           o.Title,
		Subtitle:        o.Subtitle,
		Facts:           o.Facts,
		Narrative:       o.Narrative,
		Concepts:        o.Concepts,
		FilesRead:       o.FilesRead,
		FilesModified:   o.FilesModified,
		FileMtimes:      o.FileMtimes,
		PromptNumber:    o.PromptNumber,
		DiscoveryTokens: o.DiscoveryTokens,
		CreatedAt:       o.CreatedAt,
		CreatedAtEpoch:  o.CreatedAtEpoch,
		ImportanceScore: o.ImportanceScore,
		UserFeedback:    o.UserFeedback,
		RetrievalCount:  o.RetrievalCount,
		LastRetrievedAt: o.LastRetrievedAt,
		ScoreUpdatedAt:  o.ScoreUpdatedAt,
		IsSuperseded:    o.IsSuperseded != 0, // Convert int to bool
	}
}

// toModelObservations converts a slice of GORM Observation to pkg/models.Observation.
func toModelObservations(observations []Observation) []*models.Observation {
	result := make([]*models.Observation, len(observations))
	for i := range observations {
		result[i] = toModelObservation(&observations[i])
	}
	return result
}

// nullInt64 converts an int to sql.NullInt64.
func nullInt64(val int) sql.NullInt64 {
	if val == 0 {
		return sql.NullInt64{Valid: false}
	}
	return sql.NullInt64{Int64: int64(val), Valid: true}
}
