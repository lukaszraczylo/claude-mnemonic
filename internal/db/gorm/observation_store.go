// Package gorm provides GORM-based database operations for claude-mnemonic.
package gorm

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gorm.io/gorm"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
	"github.com/rs/zerolog/log"
)

// MaxObservationsPerProject is the maximum number of observations to keep per project.
const MaxObservationsPerProject = 100

// cleanupQueueSize is the buffer size for the cleanup queue.
const cleanupQueueSize = 100

// commonWords is a package-level set for O(1) lookup of stop words.
// Created once at init time to avoid repeated map allocations.
var commonWords = map[string]struct{}{
	"the": {}, "and": {}, "or": {}, "but": {}, "in": {},
	"on": {}, "at": {}, "to": {}, "for": {}, "of": {},
	"with": {}, "by": {}, "from": {}, "as": {}, "is": {},
	"was": {}, "are": {}, "were": {}, "be": {}, "been": {},
	"being": {}, "have": {}, "has": {}, "had": {}, "do": {},
	"does": {}, "did": {}, "will": {}, "would": {}, "should": {},
	"could": {}, "may": {}, "might": {}, "must": {}, "can": {},
}

// CleanupFunc is a callback for when observations are cleaned up.
// Receives the IDs of deleted observations for downstream cleanup (e.g., vector DB).
type CleanupFunc func(ctx context.Context, deletedIDs []int64)

// ObservationStore provides observation-related database operations using GORM.
type ObservationStore struct {
	conflictStore  any
	relationStore  any
	db             *gorm.DB
	rawDB          *sql.DB
	cleanupFunc    CleanupFunc
	cleanupQueue   chan string
	stopCleanup    chan struct{}
	cleanupWg      sync.WaitGroup
	cleanupOnce    sync.Once
	cleanupStarted atomic.Bool
}

// NewObservationStore creates a new observation store.
// The conflictStore and relationStore parameters are optional (can be nil) and will be used in Phase 4.
func NewObservationStore(store *Store, cleanupFunc CleanupFunc, conflictStore, relationStore any) *ObservationStore {
	s := &ObservationStore{
		db:            store.DB,
		rawDB:         store.GetRawDB(),
		cleanupFunc:   cleanupFunc,
		conflictStore: conflictStore,
		relationStore: relationStore,
		cleanupQueue:  make(chan string, cleanupQueueSize),
		stopCleanup:   make(chan struct{}),
	}
	// Start the cleanup worker
	s.startCleanupWorker()
	return s
}

// startCleanupWorker starts the background cleanup worker.
func (s *ObservationStore) startCleanupWorker() {
	s.cleanupOnce.Do(func() {
		s.cleanupStarted.Store(true)
		s.cleanupWg.Add(1)
		go s.cleanupWorker()
	})
}

// cleanupWorker processes cleanup requests from the queue.
func (s *ObservationStore) cleanupWorker() {
	defer s.cleanupWg.Done()

	for {
		select {
		case <-s.stopCleanup:
			// Drain remaining items in queue before exiting
			for {
				select {
				case project := <-s.cleanupQueue:
					s.processCleanup(project)
				default:
					return
				}
			}
		case project := <-s.cleanupQueue:
			s.processCleanup(project)
		}
	}
}

// processCleanup performs the actual cleanup for a project.
func (s *ObservationStore) processCleanup(project string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	deletedIDs, err := s.CleanupOldObservations(ctx, project)
	if err != nil {
		log.Warn().Err(err).Str("project", project).Msg("Failed to cleanup old observations")
		return
	}

	if len(deletedIDs) > 0 && s.cleanupFunc != nil {
		s.cleanupFunc(ctx, deletedIDs)
		log.Debug().Str("project", project).Int("count", len(deletedIDs)).Msg("Cleaned up old observations")
	}
}

// Close stops the cleanup worker and waits for it to finish.
// Safe to call even if the worker was never started.
func (s *ObservationStore) Close() {
	// Only stop if worker was actually started to avoid deadlock
	if s.cleanupStarted.Load() {
		close(s.stopCleanup)
		s.cleanupWg.Wait()
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

	// Queue cleanup of old observations beyond the limit for this project (async to not block handler)
	if project != "" {
		select {
		case s.cleanupQueue <- project:
			// Successfully queued for cleanup
		default:
			// Queue is full, log a warning instead of silently dropping
			log.Warn().Str("project", project).Msg("Cleanup queue full, skipping cleanup for this observation")
		}
	}

	// Note: Conflict and relation detection intentionally omitted for now
	// Will be added in Phase 4 when ConflictStore and RelationStore are implemented

	return dbObs.ID, nowEpoch, nil
}

// ObservationUpdate contains fields that can be updated on an observation.
// Only non-nil fields will be updated.
type ObservationUpdate struct {
	Title         *string   // New title
	Subtitle      *string   // New subtitle
	Narrative     *string   // New narrative
	Facts         *[]string // New facts (replaces existing)
	Concepts      *[]string // New concepts (replaces existing)
	FilesRead     *[]string // New files read (replaces existing)
	FilesModified *[]string // New files modified (replaces existing)
	Scope         *string   // New scope (project or global)
}

// UpdateObservation updates an existing observation with the provided fields.
// Only non-nil fields in the update struct will be modified.
// Returns the updated observation or an error.
func (s *ObservationStore) UpdateObservation(ctx context.Context, id int64, update *ObservationUpdate) (*models.Observation, error) {
	if update == nil {
		return nil, fmt.Errorf("update cannot be nil")
	}

	// First, verify the observation exists
	var dbObs Observation
	if err := s.db.WithContext(ctx).First(&dbObs, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("observation not found: %d", id)
		}
		return nil, err
	}

	// Build update map with only provided fields
	updates := make(map[string]any)

	if update.Title != nil {
		updates["title"] = sql.NullString{String: *update.Title, Valid: true}
	}
	if update.Subtitle != nil {
		updates["subtitle"] = sql.NullString{String: *update.Subtitle, Valid: true}
	}
	if update.Narrative != nil {
		updates["narrative"] = sql.NullString{String: *update.Narrative, Valid: true}
	}
	if update.Facts != nil {
		factsJSON, _ := json.Marshal(*update.Facts)
		updates["facts"] = string(factsJSON)
	}
	if update.Concepts != nil {
		conceptsJSON, _ := json.Marshal(*update.Concepts)
		updates["concepts"] = string(conceptsJSON)
	}
	if update.FilesRead != nil {
		filesReadJSON, _ := json.Marshal(*update.FilesRead)
		updates["files_read"] = string(filesReadJSON)
	}
	if update.FilesModified != nil {
		filesModifiedJSON, _ := json.Marshal(*update.FilesModified)
		updates["files_modified"] = string(filesModifiedJSON)
	}
	if update.Scope != nil {
		updates["scope"] = sql.NullString{String: *update.Scope, Valid: true}
	}

	// Add updated_at timestamp
	updates["updated_at_epoch"] = sql.NullInt64{Int64: time.Now().Unix(), Valid: true}

	if len(updates) == 0 {
		// Nothing to update, just return existing observation
		return toModelObservation(&dbObs), nil
	}

	// Perform the update
	if err := s.db.WithContext(ctx).Model(&Observation{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("failed to update observation: %w", err)
	}

	// Fetch the updated observation
	if err := s.db.WithContext(ctx).First(&dbObs, id).Error; err != nil {
		return nil, err
	}

	return toModelObservation(&dbObs), nil
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
	case "score_desc":
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

// GetObservationsByIDsPreserveOrder retrieves observations by IDs, preserving the input order.
// This is useful when the caller has already sorted/ranked the IDs (e.g., by vector similarity).
func (s *ObservationStore) GetObservationsByIDsPreserveOrder(ctx context.Context, ids []int64) ([]*models.Observation, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	// Fetch all observations in a single query
	var dbObservations []Observation
	err := s.db.WithContext(ctx).Where("id IN ?", ids).Find(&dbObservations).Error
	if err != nil {
		return nil, err
	}

	// Build ID -> observation map for O(1) lookups
	obsMap := make(map[int64]*Observation, len(dbObservations))
	for i := range dbObservations {
		obsMap[int64(dbObservations[i].ID)] = &dbObservations[i]
	}

	// Reconstruct in original order
	result := make([]*models.Observation, 0, len(ids))
	for _, id := range ids {
		if obs, ok := obsMap[id]; ok {
			result = append(result, toModelObservation(obs))
		}
	}

	return result, nil
}

// BatchGetObservationsWithScores retrieves observations with associated scores.
// Returns a map of ID -> observation for efficient lookup.
func (s *ObservationStore) BatchGetObservationsWithScores(ctx context.Context, ids []int64) (map[int64]*models.Observation, error) {
	if len(ids) == 0 {
		return make(map[int64]*models.Observation), nil
	}

	// Fetch all observations in a single query
	var dbObservations []Observation
	err := s.db.WithContext(ctx).Where("id IN ?", ids).Find(&dbObservations).Error
	if err != nil {
		return nil, err
	}

	// Build result map
	result := make(map[int64]*models.Observation, len(dbObservations))
	for i := range dbObservations {
		result[int64(dbObservations[i].ID)] = toModelObservation(&dbObservations[i])
	}

	return result, nil
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

// GetActiveObservations retrieves recent non-superseded, non-archived observations for a project.
// This excludes observations that have been marked as superseded or archived.
// Results are ordered by importance_score DESC, then created_at_epoch DESC.
func (s *ObservationStore) GetActiveObservations(ctx context.Context, project string, limit int) ([]*models.Observation, error) {
	var dbObservations []Observation
	err := s.db.WithContext(ctx).
		Scopes(projectScopeFilter(project), activeObservationFilter(), importanceOrdering()).
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

// GetAllRecentObservationsPaginated retrieves recent observations with pagination.
func (s *ObservationStore) GetAllRecentObservationsPaginated(ctx context.Context, limit, offset int) ([]*models.Observation, int64, error) {
	var dbObservations []Observation
	var total int64

	// Get total count
	if err := s.db.WithContext(ctx).Model(&Observation{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Get paginated results
	err := s.db.WithContext(ctx).
		Scopes(importanceOrdering()).
		Limit(limit).
		Offset(offset).
		Find(&dbObservations).Error

	if err != nil {
		return nil, 0, err
	}

	return toModelObservations(dbObservations), total, nil
}

// GetObservationsByProjectStrictPaginated retrieves observations strictly from a project with pagination.
func (s *ObservationStore) GetObservationsByProjectStrictPaginated(ctx context.Context, project string, limit, offset int) ([]*models.Observation, int64, error) {
	var dbObservations []Observation
	var total int64

	// Get total count for project
	if err := s.db.WithContext(ctx).Model(&Observation{}).Where("project = ?", project).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Get paginated results
	err := s.db.WithContext(ctx).
		Where("project = ?", project).
		Scopes(importanceOrdering()).
		Limit(limit).
		Offset(offset).
		Find(&dbObservations).Error

	if err != nil {
		return nil, 0, err
	}

	return toModelObservations(dbObservations), total, nil
}

// GetAllObservations retrieves all observations (for vector rebuild).
// Note: For large datasets, prefer GetAllObservationsIterator to avoid memory issues.
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

// GetAllObservationsIterator returns observations in batches to avoid loading all into memory.
// The callback is called for each batch. Return false from callback to stop iteration.
// batchSize controls how many observations are loaded at once (default 500 if <= 0).
func (s *ObservationStore) GetAllObservationsIterator(ctx context.Context, batchSize int, callback func([]*models.Observation) bool) error {
	if batchSize <= 0 {
		batchSize = 500
	}

	var lastID int64 = 0
	for {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var dbObservations []Observation
		err := s.db.WithContext(ctx).
			Where("id > ?", lastID).
			Order("id ASC").
			Limit(batchSize).
			Find(&dbObservations).Error

		if err != nil {
			return err
		}

		if len(dbObservations) == 0 {
			break // No more observations
		}

		// Update cursor for next batch
		lastID = dbObservations[len(dbObservations)-1].ID

		// Convert and call callback
		observations := toModelObservations(dbObservations)
		if !callback(observations) {
			break // Callback requested stop
		}
	}

	return nil
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
// Limits to 2 keywords to prevent expensive OR queries that SQLite optimizes poorly.
// This is a fallback path when FTS returns no results, so we prioritize performance.
func (s *ObservationStore) searchObservationsLike(ctx context.Context, keywords []string, project string, limit int) ([]*models.Observation, error) {
	if len(keywords) == 0 {
		return nil, nil
	}

	// Limit keywords to prevent excessive OR conditions that hurt query planning.
	// SQLite performs significantly better with fewer LIKE conditions.
	// Using 2 instead of 5 reduces query complexity from O(15) to O(6) conditions
	// (each keyword creates 3 LIKE conditions for title, subtitle, narrative).
	const maxKeywords = 2
	if len(keywords) > maxKeywords {
		keywords = keywords[:maxKeywords]
	}

	// Build LIKE conditions for each keyword
	// Pre-allocate for efficiency: maxKeywords conditions × 3 args each + 1 project arg
	conditions := make([]string, 0, len(keywords))
	args := make([]any, 0, len(keywords)*3+1)

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

// DeleteObservation deletes a single observation by ID.
func (s *ObservationStore) DeleteObservation(ctx context.Context, id int64) error {
	result := s.db.WithContext(ctx).Delete(&Observation{}, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("observation %d not found", id)
	}
	return nil
}

// MarkAsSuperseded marks an observation as superseded (stale).
func (s *ObservationStore) MarkAsSuperseded(ctx context.Context, id int64) error {
	result := s.db.WithContext(ctx).
		Model(&Observation{}).
		Where("id = ?", id).
		Update("is_superseded", 1)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("observation %d not found", id)
	}
	return nil
}

// MarkAsSupersededBatch marks multiple observations as superseded in a single query.
// Returns the number of observations updated and any error.
func (s *ObservationStore) MarkAsSupersededBatch(ctx context.Context, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}

	result := s.db.WithContext(ctx).
		Model(&Observation{}).
		Where("id IN ?", ids).
		Update("is_superseded", 1)

	return result.RowsAffected, result.Error
}

// ArchiveObservation archives a single observation with an optional reason.
func (s *ObservationStore) ArchiveObservation(ctx context.Context, id int64, reason string) error {
	updates := map[string]any{
		"is_archived":       1,
		"archived_at_epoch": time.Now().UnixMilli(),
	}
	if reason != "" {
		updates["archived_reason"] = reason
	}

	result := s.db.WithContext(ctx).
		Model(&Observation{}).
		Where("id = ?", id).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("observation %d not found", id)
	}
	return nil
}

// UnarchiveObservation restores an archived observation.
func (s *ObservationStore) UnarchiveObservation(ctx context.Context, id int64) error {
	result := s.db.WithContext(ctx).
		Model(&Observation{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"is_archived":       0,
			"archived_at_epoch": nil,
			"archived_reason":   nil,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("observation %d not found", id)
	}
	return nil
}

// ArchiveOldObservations archives observations older than the specified age.
// Returns the count of archived observations and their IDs.
func (s *ObservationStore) ArchiveOldObservations(ctx context.Context, project string, maxAgeDays int, reason string) ([]int64, error) {
	if maxAgeDays <= 0 {
		maxAgeDays = 90 // Default: archive observations older than 90 days
	}

	cutoffEpoch := time.Now().AddDate(0, 0, -maxAgeDays).UnixMilli()
	if reason == "" {
		reason = fmt.Sprintf("auto-archived: older than %d days", maxAgeDays)
	}

	var idsToArchive []int64

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Find observations to archive (not already archived, older than cutoff)
		query := tx.Model(&Observation{}).
			Where("created_at_epoch < ?", cutoffEpoch).
			Where("COALESCE(is_archived, 0) = 0")

		if project != "" {
			query = query.Where("project = ?", project)
		}

		if err := query.Pluck("id", &idsToArchive).Error; err != nil {
			return err
		}

		if len(idsToArchive) == 0 {
			return nil
		}

		// Archive the observations
		now := time.Now().UnixMilli()
		return tx.Model(&Observation{}).
			Where("id IN ?", idsToArchive).
			Updates(map[string]any{
				"is_archived":       1,
				"archived_at_epoch": now,
				"archived_reason":   reason,
			}).Error
	})

	if err != nil {
		return nil, err
	}

	return idsToArchive, nil
}

// GetArchivedObservations retrieves archived observations for a project.
func (s *ObservationStore) GetArchivedObservations(ctx context.Context, project string, limit int) ([]*models.Observation, error) {
	var dbObservations []Observation
	query := s.db.WithContext(ctx).
		Where("COALESCE(is_archived, 0) = 1")

	if project != "" {
		query = query.Where("project = ?", project)
	}

	err := query.
		Order("archived_at_epoch DESC").
		Limit(limit).
		Find(&dbObservations).Error

	if err != nil {
		return nil, err
	}

	return toModelObservations(dbObservations), nil
}

// GetArchivalStats returns statistics about archived observations.
// Optimized to use a single query instead of 4 separate queries.
func (s *ObservationStore) GetArchivalStats(ctx context.Context, project string) (*ArchivalStats, error) {
	// Use a single query with conditional aggregation to get all stats at once.
	// This is much faster than 4 separate queries (saves 3 round trips).
	type statsResult struct {
		OldestEpoch   *int64
		NewestEpoch   *int64
		TotalCount    int64
		ArchivedCount int64
	}

	var result statsResult

	query := s.db.WithContext(ctx).Model(&Observation{}).
		Select(`
			COUNT(*) as total_count,
			SUM(CASE WHEN COALESCE(is_archived, 0) = 1 THEN 1 ELSE 0 END) as archived_count,
			MIN(CASE WHEN COALESCE(is_archived, 0) = 1 THEN archived_at_epoch END) as oldest_epoch,
			MAX(CASE WHEN COALESCE(is_archived, 0) = 1 THEN archived_at_epoch END) as newest_epoch
		`)

	if project != "" {
		query = query.Where("project = ?", project)
	}

	if err := query.Scan(&result).Error; err != nil {
		return nil, err
	}

	stats := &ArchivalStats{
		TotalCount:    result.TotalCount,
		ArchivedCount: result.ArchivedCount,
		ActiveCount:   result.TotalCount - result.ArchivedCount,
	}

	if result.OldestEpoch != nil {
		stats.OldestArchivedEpoch = *result.OldestEpoch
	}
	if result.NewestEpoch != nil {
		stats.NewestArchivedEpoch = *result.NewestEpoch
	}

	return stats, nil
}

// ArchivalStats contains statistics about archived observations.
type ArchivalStats struct {
	TotalCount          int64 `json:"total_count"`
	ActiveCount         int64 `json:"active_count"`
	ArchivedCount       int64 `json:"archived_count"`
	OldestArchivedEpoch int64 `json:"oldest_archived_epoch,omitempty"`
	NewestArchivedEpoch int64 `json:"newest_archived_epoch,omitempty"`
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

// activeObservationFilter filters for active (non-archived, non-superseded) observations.
// This is more efficient than chaining notSupersededFilter + notArchivedFilter
// as it produces a single WHERE clause for the query optimizer.
func activeObservationFilter() func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		return db.Where("COALESCE(is_archived, 0) = 0 AND COALESCE(is_superseded, 0) = 0")
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
// Uses package-level commonWords map for O(1) stop word filtering.
func extractKeywords(query string) []string {
	words := strings.Fields(strings.ToLower(query))
	keywords := make([]string, 0, len(words)) // Pre-allocate for typical case

	for _, word := range words {
		// Skip short words and common stop words
		if len(word) <= 3 {
			continue
		}
		if _, isCommon := commonWords[word]; isCommon {
			continue
		}
		keywords = append(keywords, word)
	}

	return keywords
}

// scanObservationRows scans multiple observations from raw SQL rows.
func scanObservationRows(rows *sql.Rows) ([]*models.Observation, error) {
	// Pre-allocate with reasonable initial capacity to avoid repeated slice growth
	observations := make([]*models.Observation, 0, 64)
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
func scanObservation(scanner interface{ Scan(...any) error }) (*models.Observation, error) {
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
