// Package sqlite provides SQLite database operations for claude-mnemonic.
package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
)

// UpdateObservationFeedback updates the user feedback for an observation.
// Feedback values: -1 (thumbs down), 0 (neutral), 1 (thumbs up).
func (s *ObservationStore) UpdateObservationFeedback(ctx context.Context, id int64, feedback int) error {
	const query = `
		UPDATE observations
		SET user_feedback = ?, score_updated_at_epoch = ?
		WHERE id = ?
	`
	_, err := s.store.ExecContext(ctx, query, feedback, time.Now().UnixMilli(), id)
	return err
}

// IncrementRetrievalCount increments the retrieval counter for the given observation IDs.
// This is called when observations are returned in search results.
func (s *ObservationStore) IncrementRetrievalCount(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}

	now := time.Now().UnixMilli()

	// Build query with placeholders
	// #nosec G202 -- query uses parameterized placeholders, not user input
	query := `
		UPDATE observations
		SET retrieval_count = COALESCE(retrieval_count, 0) + 1,
		    last_retrieved_at_epoch = ?
		WHERE id IN (?` + repeatPlaceholders(len(ids)-1) + `)
	`

	args := make([]interface{}, 0, len(ids)+1)
	args = append(args, now)
	for _, id := range ids {
		args = append(args, id)
	}

	_, err := s.store.db.ExecContext(ctx, query, args...)
	return err
}

// UpdateImportanceScore updates the importance score for a single observation.
func (s *ObservationStore) UpdateImportanceScore(ctx context.Context, id int64, score float64) error {
	const query = `
		UPDATE observations
		SET importance_score = ?, score_updated_at_epoch = ?
		WHERE id = ?
	`
	_, err := s.store.ExecContext(ctx, query, score, time.Now().UnixMilli(), id)
	return err
}

// UpdateImportanceScores bulk updates importance scores for multiple observations.
// This is more efficient than individual updates for batch recalculation.
func (s *ObservationStore) UpdateImportanceScores(ctx context.Context, scores map[int64]float64) error {
	if len(scores) == 0 {
		return nil
	}

	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now().UnixMilli()
	stmt, err := tx.PrepareContext(ctx, `
		UPDATE observations
		SET importance_score = ?, score_updated_at_epoch = ?
		WHERE id = ?
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for id, score := range scores {
		if _, err := stmt.ExecContext(ctx, score, now, id); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// GetObservationsNeedingScoreUpdate returns observations that need their importance score recalculated.
// Returns observations where score_updated_at_epoch is NULL or older than the threshold.
func (s *ObservationStore) GetObservationsNeedingScoreUpdate(ctx context.Context, threshold time.Duration, limit int) ([]*models.Observation, error) {
	cutoff := time.Now().Add(-threshold).UnixMilli()

	query := `SELECT ` + observationColumns + `
		FROM observations
		WHERE score_updated_at_epoch IS NULL OR score_updated_at_epoch < ?
		ORDER BY created_at_epoch DESC
		LIMIT ?
	`

	rows, err := s.store.QueryContext(ctx, query, cutoff, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanObservationRows(rows)
}

// GetConceptWeights returns all concept weights from the database.
func (s *ObservationStore) GetConceptWeights(ctx context.Context) (map[string]float64, error) {
	const query = `SELECT concept, weight FROM concept_weights`

	rows, err := s.store.QueryContext(ctx, query)
	if err != nil {
		// Table might not exist in older databases
		if err == sql.ErrNoRows {
			return models.DefaultConceptWeights, nil
		}
		return nil, err
	}
	defer rows.Close()

	weights := make(map[string]float64)
	for rows.Next() {
		var concept string
		var weight float64
		if err := rows.Scan(&concept, &weight); err != nil {
			return nil, err
		}
		weights[concept] = weight
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	// If no weights found, use defaults
	if len(weights) == 0 {
		return models.DefaultConceptWeights, nil
	}

	return weights, nil
}

// UpdateConceptWeight updates a single concept weight.
func (s *ObservationStore) UpdateConceptWeight(ctx context.Context, concept string, weight float64) error {
	const query = `
		INSERT INTO concept_weights (concept, weight, updated_at)
		VALUES (?, ?, datetime('now'))
		ON CONFLICT(concept) DO UPDATE SET weight = excluded.weight, updated_at = excluded.updated_at
	`
	_, err := s.store.ExecContext(ctx, query, concept, weight)
	return err
}

// UpdateConceptWeights bulk updates multiple concept weights.
func (s *ObservationStore) UpdateConceptWeights(ctx context.Context, weights map[string]float64) error {
	if len(weights) == 0 {
		return nil
	}

	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO concept_weights (concept, weight, updated_at)
		VALUES (?, ?, datetime('now'))
		ON CONFLICT(concept) DO UPDATE SET weight = excluded.weight, updated_at = excluded.updated_at
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for concept, weight := range weights {
		if _, err := stmt.ExecContext(ctx, concept, weight); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// GetObservationFeedbackStats returns statistics about user feedback.
func (s *ObservationStore) GetObservationFeedbackStats(ctx context.Context, project string) (*FeedbackStats, error) {
	var query string
	var args []interface{}

	if project == "" {
		query = `
			SELECT
				COUNT(*) as total,
				COALESCE(SUM(CASE WHEN user_feedback = 1 THEN 1 ELSE 0 END), 0) as positive,
				COALESCE(SUM(CASE WHEN user_feedback = -1 THEN 1 ELSE 0 END), 0) as negative,
				COALESCE(SUM(CASE WHEN user_feedback = 0 THEN 1 ELSE 0 END), 0) as neutral,
				COALESCE(AVG(COALESCE(importance_score, 1.0)), 0) as avg_score,
				COALESCE(AVG(COALESCE(retrieval_count, 0)), 0) as avg_retrieval
			FROM observations
		`
	} else {
		query = `
			SELECT
				COUNT(*) as total,
				COALESCE(SUM(CASE WHEN user_feedback = 1 THEN 1 ELSE 0 END), 0) as positive,
				COALESCE(SUM(CASE WHEN user_feedback = -1 THEN 1 ELSE 0 END), 0) as negative,
				COALESCE(SUM(CASE WHEN user_feedback = 0 THEN 1 ELSE 0 END), 0) as neutral,
				COALESCE(AVG(COALESCE(importance_score, 1.0)), 0) as avg_score,
				COALESCE(AVG(COALESCE(retrieval_count, 0)), 0) as avg_retrieval
			FROM observations
			WHERE project = ? OR scope = 'global'
		`
		args = append(args, project)
	}

	var stats FeedbackStats
	err := s.store.QueryRowContext(ctx, query, args...).Scan(
		&stats.Total,
		&stats.Positive,
		&stats.Negative,
		&stats.Neutral,
		&stats.AvgScore,
		&stats.AvgRetrieval,
	)
	if err != nil {
		return nil, err
	}

	return &stats, nil
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

// GetTopScoringObservations returns the highest-scoring observations.
func (s *ObservationStore) GetTopScoringObservations(ctx context.Context, project string, limit int) ([]*models.Observation, error) {
	var query string
	var args []interface{}

	if project == "" {
		query = `SELECT ` + observationColumns + `
			FROM observations
			ORDER BY COALESCE(importance_score, 1.0) DESC, created_at_epoch DESC
			LIMIT ?
		`
		args = append(args, limit)
	} else {
		query = `SELECT ` + observationColumns + `
			FROM observations
			WHERE project = ? OR scope = 'global'
			ORDER BY COALESCE(importance_score, 1.0) DESC, created_at_epoch DESC
			LIMIT ?
		`
		args = append(args, project, limit)
	}

	rows, err := s.store.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanObservationRows(rows)
}

// GetMostRetrievedObservations returns the most frequently retrieved observations.
func (s *ObservationStore) GetMostRetrievedObservations(ctx context.Context, project string, limit int) ([]*models.Observation, error) {
	var query string
	var args []interface{}

	if project == "" {
		query = `SELECT ` + observationColumns + `
			FROM observations
			WHERE retrieval_count > 0
			ORDER BY retrieval_count DESC, created_at_epoch DESC
			LIMIT ?
		`
		args = append(args, limit)
	} else {
		query = `SELECT ` + observationColumns + `
			FROM observations
			WHERE (project = ? OR scope = 'global') AND retrieval_count > 0
			ORDER BY retrieval_count DESC, created_at_epoch DESC
			LIMIT ?
		`
		args = append(args, project, limit)
	}

	rows, err := s.store.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanObservationRows(rows)
}

// ResetObservationScores resets all observation scores to their default values.
// This is useful for testing or when changing the scoring algorithm.
func (s *ObservationStore) ResetObservationScores(ctx context.Context) error {
	const query = `
		UPDATE observations
		SET importance_score = 1.0, score_updated_at_epoch = NULL
	`
	_, err := s.store.ExecContext(ctx, query)
	return err
}
