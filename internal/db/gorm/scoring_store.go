// Package gorm provides GORM-based database operations for claude-mnemonic.
package gorm

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
)

// UpdateObservationFeedback updates the user feedback for an observation.
// Feedback values: -1 (thumbs down), 0 (neutral), 1 (thumbs up).
func (s *ObservationStore) UpdateObservationFeedback(ctx context.Context, id int64, feedback int) error {
	now := time.Now().UnixMilli()

	result := s.db.WithContext(ctx).
		Model(&Observation{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"user_feedback":          feedback,
			"score_updated_at_epoch": now,
		})

	return result.Error
}

// IncrementRetrievalCount increments the retrieval counter for the given observation IDs.
// This is called when observations are returned in search results.
func (s *ObservationStore) IncrementRetrievalCount(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}

	now := time.Now().UnixMilli()

	// Use raw SQL for increment expression
	result := s.db.WithContext(ctx).
		Exec("UPDATE observations SET retrieval_count = COALESCE(retrieval_count, 0) + 1, last_retrieved_at_epoch = ? WHERE id IN ?",
			now, ids)

	return result.Error
}

// UpdateImportanceScore updates the importance score for a single observation.
func (s *ObservationStore) UpdateImportanceScore(ctx context.Context, id int64, score float64) error {
	now := time.Now().UnixMilli()

	result := s.db.WithContext(ctx).
		Model(&Observation{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"importance_score":       score,
			"score_updated_at_epoch": now,
		})

	return result.Error
}

// UpdateImportanceScores bulk updates importance scores for multiple observations.
// Uses a single SQL statement with CASE/WHEN for efficient batch updates.
func (s *ObservationStore) UpdateImportanceScores(ctx context.Context, scores map[int64]float64) error {
	if len(scores) == 0 {
		return nil
	}

	// For small batches, use simple individual updates
	if len(scores) <= 5 {
		return s.updateScoresIndividually(ctx, scores)
	}

	// For larger batches, use CASE/WHEN SQL for single-query update
	return s.updateScoresBatch(ctx, scores)
}

// updateScoresIndividually updates scores one at a time (efficient for small batches).
func (s *ObservationStore) updateScoresIndividually(ctx context.Context, scores map[int64]float64) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UnixMilli()

		for id, score := range scores {
			err := tx.Model(&Observation{}).
				Where("id = ?", id).
				Updates(map[string]interface{}{
					"importance_score":       score,
					"score_updated_at_epoch": now,
				}).Error

			if err != nil {
				return err
			}
		}

		return nil
	})
}

// updateScoresBatch updates multiple scores in a single SQL statement using CASE/WHEN.
// This is much more efficient for large batches (O(1) queries instead of O(n)).
func (s *ObservationStore) updateScoresBatch(ctx context.Context, scores map[int64]float64) error {
	now := time.Now().UnixMilli()

	// Build CASE/WHEN clause for importance_score
	// UPDATE observations SET
	//   importance_score = CASE id WHEN 1 THEN 0.5 WHEN 2 THEN 0.8 ... END,
	//   score_updated_at_epoch = ?
	// WHERE id IN (1, 2, ...)

	ids := make([]int64, 0, len(scores))
	caseBuilder := strings.Builder{}
	caseBuilder.WriteString("CASE id ")

	for id, score := range scores {
		ids = append(ids, id)
		caseBuilder.WriteString(fmt.Sprintf("WHEN %d THEN %f ", id, score))
	}
	caseBuilder.WriteString("END")

	// Use raw SQL for the batch update
	sql := fmt.Sprintf(
		"UPDATE observations SET importance_score = %s, score_updated_at_epoch = ? WHERE id IN ?",
		caseBuilder.String(),
	)

	return s.db.WithContext(ctx).Exec(sql, now, ids).Error
}

// GetObservationsNeedingScoreUpdate returns observations that need their importance score recalculated.
// Returns observations where score_updated_at_epoch is NULL or older than the threshold.
func (s *ObservationStore) GetObservationsNeedingScoreUpdate(ctx context.Context, threshold time.Duration, limit int) ([]*models.Observation, error) {
	cutoff := time.Now().Add(-threshold).UnixMilli()

	var observations []Observation

	err := s.db.WithContext(ctx).
		Where("score_updated_at_epoch IS NULL OR score_updated_at_epoch < ?", cutoff).
		Order("created_at_epoch DESC").
		Limit(limit).
		Find(&observations).Error

	if err != nil {
		return nil, err
	}

	return toModelObservations(observations), nil
}

// GetConceptWeights returns all concept weights from the database.
func (s *ObservationStore) GetConceptWeights(ctx context.Context) (map[string]float64, error) {
	var weights []struct {
		Concept string
		Weight  float64
	}

	err := s.db.WithContext(ctx).
		Table("concept_weights").
		Select("concept, weight").
		Scan(&weights).Error

	if err != nil {
		return models.DefaultConceptWeights, nil
	}

	if len(weights) == 0 {
		return models.DefaultConceptWeights, nil
	}

	result := make(map[string]float64, len(weights))
	for _, w := range weights {
		result[w.Concept] = w.Weight
	}

	return result, nil
}

// SetConceptWeights stores concept weights in the database using UPSERT.
func (s *ObservationStore) SetConceptWeights(ctx context.Context, weights map[string]float64) error {
	if len(weights) == 0 {
		return nil
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for concept, weight := range weights {
			// UPSERT using raw SQL since GORM's ON CONFLICT is complex for this case
			err := tx.Exec(`
				INSERT INTO concept_weights (concept, weight, updated_at)
				VALUES (?, ?, datetime('now'))
				ON CONFLICT(concept) DO UPDATE SET weight = excluded.weight, updated_at = excluded.updated_at
			`, concept, weight).Error

			if err != nil {
				return err
			}
		}

		return nil
	})
}

// UpdateConceptWeight updates a single concept weight in the database using UPSERT.
func (s *ObservationStore) UpdateConceptWeight(ctx context.Context, concept string, weight float64) error {
	return s.db.WithContext(ctx).Exec(`
		INSERT INTO concept_weights (concept, weight, updated_at)
		VALUES (?, ?, datetime('now'))
		ON CONFLICT(concept) DO UPDATE SET weight = excluded.weight, updated_at = excluded.updated_at
	`, concept, weight).Error
}

// FeedbackStats contains statistics about observation feedback and scoring.
type FeedbackStats struct {
	Total        int     `json:"total"`
	Positive     int     `json:"positive"`
	Negative     int     `json:"negative"`
	Neutral      int     `json:"neutral"`
	AvgScore     float64 `json:"avg_score"`
	AvgRetrieval float64 `json:"avg_retrieval"`
}

// GetObservationFeedbackStats returns statistics about user feedback.
func (s *ObservationStore) GetObservationFeedbackStats(ctx context.Context, project string) (*FeedbackStats, error) {
	var stats FeedbackStats

	query := s.db.WithContext(ctx).
		Model(&Observation{}).
		Select(`
			COUNT(*) as total,
			COALESCE(SUM(CASE WHEN user_feedback = 1 THEN 1 ELSE 0 END), 0) as positive,
			COALESCE(SUM(CASE WHEN user_feedback = -1 THEN 1 ELSE 0 END), 0) as negative,
			COALESCE(SUM(CASE WHEN user_feedback = 0 THEN 1 ELSE 0 END), 0) as neutral,
			COALESCE(AVG(COALESCE(importance_score, 1.0)), 0) as avg_score,
			COALESCE(AVG(COALESCE(retrieval_count, 0)), 0) as avg_retrieval
		`)

	if project != "" {
		query = query.Where("project = ? OR scope = 'global'", project)
	}

	err := query.Scan(&stats).Error
	if err != nil {
		return nil, err
	}

	return &stats, nil
}

// GetTopScoringObservations returns the highest-scoring observations.
func (s *ObservationStore) GetTopScoringObservations(ctx context.Context, project string, limit int) ([]*models.Observation, error) {
	var observations []Observation

	query := s.db.WithContext(ctx).
		Order("COALESCE(importance_score, 1.0) DESC, created_at_epoch DESC").
		Limit(limit)

	if project != "" {
		query = query.Where("project = ? OR scope = 'global'", project)
	}

	err := query.Find(&observations).Error
	if err != nil {
		return nil, err
	}

	return toModelObservations(observations), nil
}

// GetMostRetrievedObservations returns the most frequently retrieved observations.
func (s *ObservationStore) GetMostRetrievedObservations(ctx context.Context, project string, limit int) ([]*models.Observation, error) {
	var observations []Observation

	query := s.db.WithContext(ctx).
		Where("retrieval_count > 0").
		Order("retrieval_count DESC, created_at_epoch DESC").
		Limit(limit)

	if project != "" {
		query = query.Where("project = ? OR scope = 'global'", project)
	}

	err := query.Find(&observations).Error
	if err != nil {
		return nil, err
	}

	return toModelObservations(observations), nil
}

// ResetObservationScores resets all observation scores to their default values.
// This is useful for testing or when changing the scoring algorithm.
func (s *ObservationStore) ResetObservationScores(ctx context.Context) error {
	// Use Where("1 = 1") to explicitly allow bulk update of all rows
	result := s.db.WithContext(ctx).
		Model(&Observation{}).
		Where("1 = 1").
		Updates(map[string]interface{}{
			"importance_score":       1.0,
			"score_updated_at_epoch": nil,
		})

	return result.Error
}
