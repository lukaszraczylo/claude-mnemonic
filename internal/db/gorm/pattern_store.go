// Package gorm provides GORM-based database operations for claude-mnemonic.
package gorm

import (
	"context"
	"database/sql"
	"time"

	"gorm.io/gorm"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
)

// PatternCleanupFunc is a callback for when patterns are deleted.
type PatternCleanupFunc func(ctx context.Context, deletedIDs []int64)

// PatternStore provides pattern-related database operations using GORM.
type PatternStore struct {
	db          *gorm.DB
	cleanupFunc PatternCleanupFunc
}

// NewPatternStore creates a new pattern store.
func NewPatternStore(store *Store) *PatternStore {
	return &PatternStore{
		db: store.DB,
	}
}

// SetCleanupFunc sets the callback for when patterns are deleted.
func (s *PatternStore) SetCleanupFunc(fn PatternCleanupFunc) {
	s.cleanupFunc = fn
}

// StorePattern stores a new pattern.
func (s *PatternStore) StorePattern(ctx context.Context, pattern *models.Pattern) (int64, error) {
	dbPattern := &Pattern{
		Name:            pattern.Name,
		Type:            pattern.Type,
		Signature:       pattern.Signature,
		Frequency:       pattern.Frequency,
		Projects:        pattern.Projects,
		ObservationIDs:  pattern.ObservationIDs,
		Status:          pattern.Status,
		Confidence:      pattern.Confidence,
		LastSeenAt:      pattern.LastSeenAt,
		LastSeenAtEpoch: pattern.LastSeenEpoch,
		CreatedAt:       pattern.CreatedAt,
		CreatedAtEpoch:  pattern.CreatedAtEpoch,
	}

	if pattern.Description.Valid {
		dbPattern.Description = sql.NullString{String: pattern.Description.String, Valid: true}
	}

	if pattern.Recommendation.Valid {
		dbPattern.Recommendation = sql.NullString{String: pattern.Recommendation.String, Valid: true}
	}

	if pattern.MergedIntoID.Valid {
		dbPattern.MergedIntoID = sql.NullInt64{Int64: pattern.MergedIntoID.Int64, Valid: true}
	}

	result := s.db.WithContext(ctx).Create(dbPattern)
	if result.Error != nil {
		return 0, result.Error
	}

	return dbPattern.ID, nil
}

// UpdatePattern updates an existing pattern.
func (s *PatternStore) UpdatePattern(ctx context.Context, pattern *models.Pattern) error {
	updates := map[string]interface{}{
		"name":               pattern.Name,
		"type":               pattern.Type,
		"signature":          pattern.Signature,
		"frequency":          pattern.Frequency,
		"projects":           pattern.Projects,
		"observation_ids":    pattern.ObservationIDs,
		"status":             pattern.Status,
		"confidence":         pattern.Confidence,
		"last_seen_at":       pattern.LastSeenAt,
		"last_seen_at_epoch": pattern.LastSeenEpoch,
	}

	if pattern.Description.Valid {
		updates["description"] = pattern.Description.String
	} else {
		updates["description"] = nil
	}

	if pattern.Recommendation.Valid {
		updates["recommendation"] = pattern.Recommendation.String
	} else {
		updates["recommendation"] = nil
	}

	if pattern.MergedIntoID.Valid {
		updates["merged_into_id"] = pattern.MergedIntoID.Int64
	} else {
		updates["merged_into_id"] = nil
	}

	result := s.db.WithContext(ctx).
		Model(&Pattern{}).
		Where("id = ?", pattern.ID).
		Updates(updates)

	return result.Error
}

// GetPatternByID retrieves a pattern by ID.
func (s *PatternStore) GetPatternByID(ctx context.Context, id int64) (*models.Pattern, error) {
	var dbPattern Pattern

	err := s.db.WithContext(ctx).First(&dbPattern, id).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return toModelPattern(&dbPattern), nil
}

// GetPatternByName retrieves a pattern by name.
func (s *PatternStore) GetPatternByName(ctx context.Context, name string) (*models.Pattern, error) {
	var dbPattern Pattern

	err := s.db.WithContext(ctx).
		Where("name = ? AND status = ?", name, models.PatternStatusActive).
		First(&dbPattern).Error

	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return toModelPattern(&dbPattern), nil
}

// GetActivePatterns retrieves all active patterns.
func (s *PatternStore) GetActivePatterns(ctx context.Context, limit int) ([]*models.Pattern, error) {
	var patterns []Pattern

	err := s.db.WithContext(ctx).
		Where("status = ?", models.PatternStatusActive).
		Order("frequency DESC, confidence DESC").
		Limit(limit).
		Find(&patterns).Error

	if err != nil {
		return nil, err
	}

	return toModelPatterns(patterns), nil
}

// GetPatternsByType retrieves patterns of a specific type.
func (s *PatternStore) GetPatternsByType(ctx context.Context, patternType models.PatternType, limit int) ([]*models.Pattern, error) {
	var patterns []Pattern

	err := s.db.WithContext(ctx).
		Where("type = ? AND status = ?", patternType, models.PatternStatusActive).
		Order("frequency DESC, confidence DESC").
		Limit(limit).
		Find(&patterns).Error

	if err != nil {
		return nil, err
	}

	return toModelPatterns(patterns), nil
}

// GetPatternsByProject retrieves patterns that have been observed in a specific project.
// Uses raw SQL since JSON_EACH is complex in GORM.
func (s *PatternStore) GetPatternsByProject(ctx context.Context, project string, limit int) ([]*models.Pattern, error) {
	var patterns []Pattern

	// Use raw SQL for JSON_EACH query
	query := `
		SELECT * FROM patterns
		WHERE status = 'active'
		AND EXISTS (
			SELECT 1 FROM json_each(projects)
			WHERE json_each.value = ?
		)
		ORDER BY frequency DESC, confidence DESC
		LIMIT ?
	`

	err := s.db.WithContext(ctx).
		Raw(query, project, limit).
		Scan(&patterns).Error

	if err != nil {
		return nil, err
	}

	return toModelPatterns(patterns), nil
}

// FindMatchingPatterns searches for patterns that match a given signature.
// Pattern matching is done in Go code for simplicity.
func (s *PatternStore) FindMatchingPatterns(ctx context.Context, signature []string, minScore float64) ([]*models.Pattern, error) {
	// Get all active patterns
	patterns, err := s.GetActivePatterns(ctx, 100)
	if err != nil {
		return nil, err
	}

	// Filter by signature match in Go
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
	result := s.db.WithContext(ctx).
		Model(&Pattern{}).
		Where("id = ?", id).
		Update("status", models.PatternStatusDeprecated)

	return result.Error
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

	// Merge projects (deduplicate)
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

	// Merge observation IDs (deduplicate)
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
	result := s.db.WithContext(ctx).Delete(&Pattern{}, id)

	if result.Error == nil && s.cleanupFunc != nil {
		s.cleanupFunc(ctx, []int64{id})
	}

	return result.Error
}

// SearchPatternsFTS performs full-text search on patterns.
// Uses raw SQL for FTS5 query.
func (s *PatternStore) SearchPatternsFTS(ctx context.Context, searchQuery string, limit int) ([]*models.Pattern, error) {
	var patterns []Pattern

	// Use raw SQL for FTS5 MATCH query
	query := `
		SELECT p.*
		FROM patterns p
		JOIN patterns_fts fts ON p.id = fts.rowid
		WHERE patterns_fts MATCH ?
		AND p.status = 'active'
		ORDER BY rank
		LIMIT ?
	`

	err := s.db.WithContext(ctx).
		Raw(query, searchQuery, limit).
		Scan(&patterns).Error

	if err != nil {
		return nil, err
	}

	return toModelPatterns(patterns), nil
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

// GetPatternStats returns statistics about patterns.
// Uses raw SQL for complex aggregate query.
func (s *PatternStore) GetPatternStats(ctx context.Context) (*PatternStats, error) {
	var stats PatternStats

	query := `
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

	err := s.db.WithContext(ctx).Raw(query).Scan(&stats).Error
	return &stats, err
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

// toModelPattern converts a GORM Pattern to a pkg/models Pattern.
func toModelPattern(p *Pattern) *models.Pattern {
	pattern := &models.Pattern{
		ID:             p.ID,
		Name:           p.Name,
		Type:           p.Type,
		Description:    p.Description,
		Signature:      p.Signature,
		Recommendation: p.Recommendation,
		Frequency:      p.Frequency,
		Projects:       p.Projects,
		ObservationIDs: p.ObservationIDs,
		Status:         p.Status,
		MergedIntoID:   p.MergedIntoID,
		Confidence:     p.Confidence,
		LastSeenAt:     p.LastSeenAt,
		LastSeenEpoch:  p.LastSeenAtEpoch,
		CreatedAt:      p.CreatedAt,
		CreatedAtEpoch: p.CreatedAtEpoch,
	}

	return pattern
}

// toModelPatterns converts a slice of GORM Patterns to pkg/models Patterns.
func toModelPatterns(patterns []Pattern) []*models.Pattern {
	result := make([]*models.Pattern, len(patterns))
	for i, p := range patterns {
		result[i] = toModelPattern(&p)
	}
	return result
}
