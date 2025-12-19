// Package sqlite provides SQLite database operations for claude-mnemonic.
package sqlite

import (
	"context"
	"database/sql"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
)

// RelationStore provides relation-related database operations.
type RelationStore struct {
	store *Store
}

// NewRelationStore creates a new relation store.
func NewRelationStore(store *Store) *RelationStore {
	return &RelationStore{store: store}
}

// StoreRelation stores a new observation relation.
// Uses INSERT OR IGNORE to handle duplicate (source_id, target_id, relation_type) combinations.
func (s *RelationStore) StoreRelation(ctx context.Context, relation *models.ObservationRelation) (int64, error) {
	const query = `
		INSERT OR IGNORE INTO observation_relations
		(source_id, target_id, relation_type, confidence, detection_source, reason, created_at, created_at_epoch)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`

	result, err := s.store.ExecContext(ctx, query,
		relation.SourceID, relation.TargetID,
		string(relation.RelationType), relation.Confidence,
		string(relation.DetectionSource), relation.Reason,
		relation.CreatedAt, relation.CreatedAtEpoch,
	)
	if err != nil {
		return 0, err
	}

	return result.LastInsertId()
}

// StoreRelations stores multiple relations in a single transaction.
func (s *RelationStore) StoreRelations(ctx context.Context, relations []*models.ObservationRelation) error {
	if len(relations) == 0 {
		return nil
	}

	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	const query = `
		INSERT OR IGNORE INTO observation_relations
		(source_id, target_id, relation_type, confidence, detection_source, reason, created_at, created_at_epoch)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`

	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, rel := range relations {
		_, err = stmt.ExecContext(ctx,
			rel.SourceID, rel.TargetID,
			string(rel.RelationType), rel.Confidence,
			string(rel.DetectionSource), rel.Reason,
			rel.CreatedAt, rel.CreatedAtEpoch,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// GetRelationsByObservationID retrieves all relations involving an observation (as source or target).
func (s *RelationStore) GetRelationsByObservationID(ctx context.Context, obsID int64) ([]*models.ObservationRelation, error) {
	const query = `
		SELECT id, source_id, target_id, relation_type, confidence, detection_source, reason,
		       created_at, created_at_epoch
		FROM observation_relations
		WHERE source_id = ? OR target_id = ?
		ORDER BY confidence DESC, created_at_epoch DESC
	`

	rows, err := s.store.QueryContext(ctx, query, obsID, obsID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanRelationRows(rows)
}

// GetOutgoingRelations retrieves relations where the observation is the source.
func (s *RelationStore) GetOutgoingRelations(ctx context.Context, obsID int64) ([]*models.ObservationRelation, error) {
	const query = `
		SELECT id, source_id, target_id, relation_type, confidence, detection_source, reason,
		       created_at, created_at_epoch
		FROM observation_relations
		WHERE source_id = ?
		ORDER BY confidence DESC, created_at_epoch DESC
	`

	rows, err := s.store.QueryContext(ctx, query, obsID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanRelationRows(rows)
}

// GetIncomingRelations retrieves relations where the observation is the target.
func (s *RelationStore) GetIncomingRelations(ctx context.Context, obsID int64) ([]*models.ObservationRelation, error) {
	const query = `
		SELECT id, source_id, target_id, relation_type, confidence, detection_source, reason,
		       created_at, created_at_epoch
		FROM observation_relations
		WHERE target_id = ?
		ORDER BY confidence DESC, created_at_epoch DESC
	`

	rows, err := s.store.QueryContext(ctx, query, obsID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanRelationRows(rows)
}

// GetRelationsByType retrieves all relations of a specific type.
func (s *RelationStore) GetRelationsByType(ctx context.Context, relationType models.RelationType, limit int) ([]*models.ObservationRelation, error) {
	const query = `
		SELECT id, source_id, target_id, relation_type, confidence, detection_source, reason,
		       created_at, created_at_epoch
		FROM observation_relations
		WHERE relation_type = ?
		ORDER BY confidence DESC, created_at_epoch DESC
		LIMIT ?
	`

	rows, err := s.store.QueryContext(ctx, query, string(relationType), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanRelationRows(rows)
}

// GetRelationsWithDetails retrieves relations with observation titles for display.
func (s *RelationStore) GetRelationsWithDetails(ctx context.Context, obsID int64) ([]*models.RelationWithDetails, error) {
	const query = `
		SELECT r.id, r.source_id, r.target_id, r.relation_type, r.confidence, r.detection_source, r.reason,
		       r.created_at, r.created_at_epoch,
		       COALESCE(src.title, '') as source_title,
		       COALESCE(tgt.title, '') as target_title,
		       src.type as source_type,
		       tgt.type as target_type
		FROM observation_relations r
		JOIN observations src ON src.id = r.source_id
		JOIN observations tgt ON tgt.id = r.target_id
		WHERE r.source_id = ? OR r.target_id = ?
		ORDER BY r.confidence DESC, r.created_at_epoch DESC
	`

	rows, err := s.store.QueryContext(ctx, query, obsID, obsID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []*models.RelationWithDetails
	for rows.Next() {
		var r models.ObservationRelation
		var rwd models.RelationWithDetails
		var reason sql.NullString
		if err := rows.Scan(
			&r.ID, &r.SourceID, &r.TargetID,
			&r.RelationType, &r.Confidence, &r.DetectionSource, &reason,
			&r.CreatedAt, &r.CreatedAtEpoch,
			&rwd.SourceTitle, &rwd.TargetTitle,
			&rwd.SourceType, &rwd.TargetType,
		); err != nil {
			return nil, err
		}
		if reason.Valid {
			r.Reason = reason.String
		}
		rwd.Relation = &r
		results = append(results, &rwd)
	}
	return results, rows.Err()
}

// GetRelationGraph retrieves a relation graph centered on an observation.
// This returns all observations within N hops from the center.
func (s *RelationStore) GetRelationGraph(ctx context.Context, centerID int64, maxDepth int) (*models.RelationGraph, error) {
	// Get all relations involving the center observation
	relations, err := s.GetRelationsWithDetails(ctx, centerID)
	if err != nil {
		return nil, err
	}

	graph := &models.RelationGraph{
		CenterID:  centerID,
		Relations: relations,
	}

	// If depth > 1, recursively get relations for connected observations
	if maxDepth > 1 {
		visited := map[int64]bool{centerID: true}
		toVisit := make([]int64, 0)

		// Collect IDs of directly connected observations
		for _, r := range relations {
			if !visited[r.Relation.SourceID] {
				toVisit = append(toVisit, r.Relation.SourceID)
				visited[r.Relation.SourceID] = true
			}
			if !visited[r.Relation.TargetID] {
				toVisit = append(toVisit, r.Relation.TargetID)
				visited[r.Relation.TargetID] = true
			}
		}

		// Get relations for connected observations (depth - 1)
		for depth := 1; depth < maxDepth && len(toVisit) > 0; depth++ {
			nextLevel := make([]int64, 0)
			for _, obsID := range toVisit {
				moreRelations, err := s.GetRelationsWithDetails(ctx, obsID)
				if err != nil {
					continue
				}
				for _, r := range moreRelations {
					// Avoid duplicates
					exists := false
					for _, existing := range graph.Relations {
						if existing.Relation.ID == r.Relation.ID {
							exists = true
							break
						}
					}
					if !exists {
						graph.Relations = append(graph.Relations, r)
					}

					// Queue next level
					if !visited[r.Relation.SourceID] {
						nextLevel = append(nextLevel, r.Relation.SourceID)
						visited[r.Relation.SourceID] = true
					}
					if !visited[r.Relation.TargetID] {
						nextLevel = append(nextLevel, r.Relation.TargetID)
						visited[r.Relation.TargetID] = true
					}
				}
			}
			toVisit = nextLevel
		}
	}

	return graph, nil
}

// DeleteRelationsByObservationID deletes all relations involving an observation.
// Called when an observation is deleted.
func (s *RelationStore) DeleteRelationsByObservationID(ctx context.Context, obsID int64) error {
	const query = `DELETE FROM observation_relations WHERE source_id = ? OR target_id = ?`
	_, err := s.store.ExecContext(ctx, query, obsID, obsID)
	return err
}

// GetRelationCount returns the count of relations for an observation.
func (s *RelationStore) GetRelationCount(ctx context.Context, obsID int64) (int, error) {
	const query = `
		SELECT COUNT(*) FROM observation_relations
		WHERE source_id = ? OR target_id = ?
	`
	var count int
	err := s.store.QueryRowContext(ctx, query, obsID, obsID).Scan(&count)
	return count, err
}

// GetTotalRelationCount returns the total count of all relations.
func (s *RelationStore) GetTotalRelationCount(ctx context.Context) (int, error) {
	const query = `SELECT COUNT(*) FROM observation_relations`
	var count int
	err := s.store.QueryRowContext(ctx, query).Scan(&count)
	return count, err
}

// GetHighConfidenceRelations retrieves relations with confidence above threshold.
func (s *RelationStore) GetHighConfidenceRelations(ctx context.Context, minConfidence float64, limit int) ([]*models.ObservationRelation, error) {
	const query = `
		SELECT id, source_id, target_id, relation_type, confidence, detection_source, reason,
		       created_at, created_at_epoch
		FROM observation_relations
		WHERE confidence >= ?
		ORDER BY confidence DESC, created_at_epoch DESC
		LIMIT ?
	`

	rows, err := s.store.QueryContext(ctx, query, minConfidence, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanRelationRows(rows)
}

// UpdateRelationConfidence updates the confidence of a relation.
func (s *RelationStore) UpdateRelationConfidence(ctx context.Context, relationID int64, newConfidence float64) error {
	const query = `UPDATE observation_relations SET confidence = ? WHERE id = ?`
	_, err := s.store.ExecContext(ctx, query, newConfidence, relationID)
	return err
}

// GetRelatedObservationIDs returns IDs of observations related to the given one.
// This is useful for expanding search results.
func (s *RelationStore) GetRelatedObservationIDs(ctx context.Context, obsID int64, minConfidence float64) ([]int64, error) {
	const query = `
		SELECT DISTINCT CASE WHEN source_id = ? THEN target_id ELSE source_id END as related_id
		FROM observation_relations
		WHERE (source_id = ? OR target_id = ?) AND confidence >= ?
	`

	rows, err := s.store.QueryContext(ctx, query, obsID, obsID, obsID, minConfidence)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// scanRelationRows scans multiple relations from rows.
func (s *RelationStore) scanRelationRows(rows *sql.Rows) ([]*models.ObservationRelation, error) {
	var relations []*models.ObservationRelation
	for rows.Next() {
		var r models.ObservationRelation
		var reason sql.NullString
		if err := rows.Scan(
			&r.ID, &r.SourceID, &r.TargetID,
			&r.RelationType, &r.Confidence, &r.DetectionSource, &reason,
			&r.CreatedAt, &r.CreatedAtEpoch,
		); err != nil {
			return nil, err
		}
		if reason.Valid {
			r.Reason = reason.String
		}
		relations = append(relations, &r)
	}
	return relations, rows.Err()
}
