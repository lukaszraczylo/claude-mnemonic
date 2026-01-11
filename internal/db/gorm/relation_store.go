// Package gorm provides GORM-based database operations for claude-mnemonic.
package gorm

import (
	"context"
	"database/sql"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
)

// RelationStore provides relation-related database operations using GORM.
type RelationStore struct {
	db *gorm.DB
}

// NewRelationStore creates a new relation store.
func NewRelationStore(store *Store) *RelationStore {
	return &RelationStore{
		db: store.DB,
	}
}

// StoreRelation stores a new observation relation.
// Uses INSERT OR IGNORE to handle duplicate (source_id, target_id, relation_type) combinations.
func (s *RelationStore) StoreRelation(ctx context.Context, relation *models.ObservationRelation) (int64, error) {
	dbRelation := &ObservationRelation{
		SourceID:        relation.SourceID,
		TargetID:        relation.TargetID,
		RelationType:    relation.RelationType,
		Confidence:      relation.Confidence,
		DetectionSource: relation.DetectionSource,
		CreatedAt:       relation.CreatedAt,
		CreatedAtEpoch:  relation.CreatedAtEpoch,
	}

	// Handle nullable fields
	if relation.Reason != "" {
		dbRelation.Reason = sql.NullString{String: relation.Reason, Valid: true}
	}

	// INSERT OR IGNORE using OnConflict
	result := s.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "source_id"}, {Name: "target_id"}, {Name: "relation_type"}},
			DoNothing: true,
		}).
		Create(dbRelation)

	if result.Error != nil {
		return 0, result.Error
	}

	// If RowsAffected is 0, the insert was ignored (duplicate)
	if result.RowsAffected == 0 {
		var existing ObservationRelation
		err := s.db.Where("source_id = ? AND target_id = ? AND relation_type = ?",
			relation.SourceID, relation.TargetID, relation.RelationType).
			First(&existing).Error
		if err != nil {
			return 0, err
		}
		return existing.ID, nil
	}

	return dbRelation.ID, nil
}

// StoreRelations stores multiple relations in a single transaction.
func (s *RelationStore) StoreRelations(ctx context.Context, relations []*models.ObservationRelation) error {
	if len(relations) == 0 {
		return nil
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, rel := range relations {
			dbRelation := &ObservationRelation{
				SourceID:        rel.SourceID,
				TargetID:        rel.TargetID,
				RelationType:    rel.RelationType,
				Confidence:      rel.Confidence,
				DetectionSource: rel.DetectionSource,
				CreatedAt:       rel.CreatedAt,
				CreatedAtEpoch:  rel.CreatedAtEpoch,
			}

			if rel.Reason != "" {
				dbRelation.Reason = sql.NullString{String: rel.Reason, Valid: true}
			}

			result := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "source_id"}, {Name: "target_id"}, {Name: "relation_type"}},
				DoNothing: true,
			}).Create(dbRelation)

			if result.Error != nil {
				return result.Error
			}
		}
		return nil
	})
}

// GetRelationsByObservationID retrieves all relations involving an observation (as source or target).
func (s *RelationStore) GetRelationsByObservationID(ctx context.Context, obsID int64) ([]*models.ObservationRelation, error) {
	var relations []ObservationRelation

	err := s.db.WithContext(ctx).
		Where("source_id = ? OR target_id = ?", obsID, obsID).
		Order("confidence DESC, created_at_epoch DESC").
		Find(&relations).Error

	if err != nil {
		return nil, err
	}

	return toModelRelations(relations), nil
}

// GetOutgoingRelations retrieves relations where the observation is the source.
func (s *RelationStore) GetOutgoingRelations(ctx context.Context, obsID int64) ([]*models.ObservationRelation, error) {
	var relations []ObservationRelation

	err := s.db.WithContext(ctx).
		Where("source_id = ?", obsID).
		Order("confidence DESC, created_at_epoch DESC").
		Find(&relations).Error

	if err != nil {
		return nil, err
	}

	return toModelRelations(relations), nil
}

// GetIncomingRelations retrieves relations where the observation is the target.
func (s *RelationStore) GetIncomingRelations(ctx context.Context, obsID int64) ([]*models.ObservationRelation, error) {
	var relations []ObservationRelation

	err := s.db.WithContext(ctx).
		Where("target_id = ?", obsID).
		Order("confidence DESC, created_at_epoch DESC").
		Find(&relations).Error

	if err != nil {
		return nil, err
	}

	return toModelRelations(relations), nil
}

// GetRelationsByType retrieves all relations of a specific type.
func (s *RelationStore) GetRelationsByType(ctx context.Context, relationType models.RelationType, limit int) ([]*models.ObservationRelation, error) {
	var relations []ObservationRelation

	err := s.db.WithContext(ctx).
		Where("relation_type = ?", relationType).
		Order("confidence DESC, created_at_epoch DESC").
		Limit(limit).
		Find(&relations).Error

	if err != nil {
		return nil, err
	}

	return toModelRelations(relations), nil
}

// GetRelationsWithDetails retrieves relations with observation titles for display.
func (s *RelationStore) GetRelationsWithDetails(ctx context.Context, obsID int64) ([]*models.RelationWithDetails, error) {
	var results []struct {
		SourceType  string         `gorm:"column:source_type"`
		TargetType  string         `gorm:"column:target_type"`
		SourceTitle sql.NullString `gorm:"column:source_title"`
		TargetTitle sql.NullString `gorm:"column:target_title"`
		ObservationRelation
	}

	err := s.db.WithContext(ctx).
		Table("observation_relations r").
		Select("r.*, "+
			"COALESCE(src.title, '') as source_title, "+
			"COALESCE(tgt.title, '') as target_title, "+
			"src.type as source_type, "+
			"tgt.type as target_type").
		Joins("JOIN observations src ON src.id = r.source_id").
		Joins("JOIN observations tgt ON tgt.id = r.target_id").
		Where("r.source_id = ? OR r.target_id = ?", obsID, obsID).
		Order("r.confidence DESC, r.created_at_epoch DESC").
		Scan(&results).Error

	if err != nil {
		return nil, err
	}

	relations := make([]*models.RelationWithDetails, len(results))
	for i, r := range results {
		relations[i] = &models.RelationWithDetails{
			Relation:    toModelRelation(&r.ObservationRelation),
			SourceTitle: r.SourceTitle.String,
			TargetTitle: r.TargetTitle.String,
			SourceType:  models.ObservationType(r.SourceType),
			TargetType:  models.ObservationType(r.TargetType),
		}
	}

	return relations, nil
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
	result := s.db.WithContext(ctx).
		Where("source_id = ? OR target_id = ?", obsID, obsID).
		Delete(&ObservationRelation{})

	return result.Error
}

// GetRelationCount returns the count of relations for an observation.
func (s *RelationStore) GetRelationCount(ctx context.Context, obsID int64) (int, error) {
	var count int64
	err := s.db.WithContext(ctx).
		Model(&ObservationRelation{}).
		Where("source_id = ? OR target_id = ?", obsID, obsID).
		Count(&count).Error

	return int(count), err
}

// GetTotalRelationCount returns the total count of all relations.
func (s *RelationStore) GetTotalRelationCount(ctx context.Context) (int, error) {
	var count int64
	err := s.db.WithContext(ctx).
		Model(&ObservationRelation{}).
		Count(&count).Error

	return int(count), err
}

// GetHighConfidenceRelations retrieves relations with confidence above threshold.
func (s *RelationStore) GetHighConfidenceRelations(ctx context.Context, minConfidence float64, limit int) ([]*models.ObservationRelation, error) {
	var relations []ObservationRelation

	err := s.db.WithContext(ctx).
		Where("confidence >= ?", minConfidence).
		Order("confidence DESC, created_at_epoch DESC").
		Limit(limit).
		Find(&relations).Error

	if err != nil {
		return nil, err
	}

	return toModelRelations(relations), nil
}

// UpdateRelationConfidence updates the confidence of a relation.
func (s *RelationStore) UpdateRelationConfidence(ctx context.Context, relationID int64, newConfidence float64) error {
	result := s.db.WithContext(ctx).
		Model(&ObservationRelation{}).
		Where("id = ?", relationID).
		Update("confidence", newConfidence)

	return result.Error
}

// GetRelatedObservationIDs returns IDs of observations related to the given one.
// This is useful for expanding search results.
// Uses CASE expression for bidirectional ID lookup (GORM doesn't support this well, so we use raw SQL).
func (s *RelationStore) GetRelatedObservationIDs(ctx context.Context, obsID int64, minConfidence float64) ([]int64, error) {
	var ids []int64

	err := s.db.WithContext(ctx).
		Raw("SELECT DISTINCT CASE WHEN source_id = ? THEN target_id ELSE source_id END as related_id "+
			"FROM observation_relations "+
			"WHERE (source_id = ? OR target_id = ?) AND confidence >= ?",
			obsID, obsID, obsID, minConfidence).
		Pluck("related_id", &ids).Error

	return ids, err
}

// toModelRelation converts a GORM ObservationRelation to a pkg/models ObservationRelation.
func toModelRelation(r *ObservationRelation) *models.ObservationRelation {
	relation := &models.ObservationRelation{
		ID:              r.ID,
		SourceID:        r.SourceID,
		TargetID:        r.TargetID,
		RelationType:    r.RelationType,
		Confidence:      r.Confidence,
		DetectionSource: r.DetectionSource,
		CreatedAt:       r.CreatedAt,
		CreatedAtEpoch:  r.CreatedAtEpoch,
	}

	if r.Reason.Valid {
		relation.Reason = r.Reason.String
	}

	return relation
}

// toModelRelations converts a slice of GORM ObservationRelations to pkg/models ObservationRelations.
func toModelRelations(relations []ObservationRelation) []*models.ObservationRelation {
	result := make([]*models.ObservationRelation, len(relations))
	for i, r := range relations {
		result[i] = toModelRelation(&r)
	}
	return result
}
