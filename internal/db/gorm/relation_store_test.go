//go:build fts5

// Package gorm provides GORM-based database operations for claude-mnemonic.
package gorm

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm/logger"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
)

func testRelationStore(t *testing.T) (*RelationStore, *Store, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "gorm_relation_test_*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "test.db")
	cfg := Config{
		Path:     dbPath,
		MaxConns: 4,
		LogLevel: logger.Silent,
	}

	store, err := NewStore(cfg)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("NewStore failed: %v", err)
	}

	relationStore := NewRelationStore(store)

	cleanup := func() {
		store.Close()
		os.RemoveAll(tmpDir)
	}

	return relationStore, store, cleanup
}

func TestRelationStore_StoreRelation(t *testing.T) {
	relationStore, _, cleanup := testRelationStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	relation := &models.ObservationRelation{
		SourceID:        1,
		TargetID:        2,
		RelationType:    models.RelationCauses,
		Confidence:      0.8,
		DetectionSource: models.DetectionSourceFileOverlap,
		Reason:          "Test relation",
		CreatedAt:       now.Format(time.RFC3339),
		CreatedAtEpoch:  now.UnixMilli(),
	}

	id, err := relationStore.StoreRelation(ctx, relation)
	require.NoError(t, err)
	assert.Greater(t, id, int64(0))
}

func TestRelationStore_StoreRelation_Idempotency(t *testing.T) {
	relationStore, _, cleanup := testRelationStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	relation := &models.ObservationRelation{
		SourceID:        1,
		TargetID:        2,
		RelationType:    models.RelationCauses,
		Confidence:      0.8,
		DetectionSource: models.DetectionSourceFileOverlap,
		CreatedAt:       now.Format(time.RFC3339),
		CreatedAtEpoch:  now.UnixMilli(),
	}

	id1, err := relationStore.StoreRelation(ctx, relation)
	require.NoError(t, err)

	// Store again with same source/target/type - should return same ID
	id2, err := relationStore.StoreRelation(ctx, relation)
	require.NoError(t, err)
	assert.Equal(t, id1, id2)
}

func TestRelationStore_StoreRelations(t *testing.T) {
	relationStore, _, cleanup := testRelationStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	relations := []*models.ObservationRelation{
		{
			SourceID:        1,
			TargetID:        2,
			RelationType:    models.RelationCauses,
			Confidence:      0.8,
			DetectionSource: models.DetectionSourceFileOverlap,
			CreatedAt:       now.Format(time.RFC3339),
			CreatedAtEpoch:  now.UnixMilli(),
		},
		{
			SourceID:        2,
			TargetID:        3,
			RelationType:    models.RelationFixes,
			Confidence:      0.9,
			DetectionSource: models.DetectionSourceTemporalProximity,
			CreatedAt:       now.Format(time.RFC3339),
			CreatedAtEpoch:  now.UnixMilli(),
		},
	}

	err := relationStore.StoreRelations(ctx, relations)
	require.NoError(t, err)

	// Verify both were stored
	count, err := relationStore.GetTotalRelationCount(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestRelationStore_GetRelationsByObservationID(t *testing.T) {
	relationStore, _, cleanup := testRelationStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	// Create relations involving observation 2
	relations := []*models.ObservationRelation{
		{
			SourceID:        1,
			TargetID:        2,
			RelationType:    models.RelationCauses,
			Confidence:      0.8,
			DetectionSource: models.DetectionSourceFileOverlap,
			CreatedAt:       now.Format(time.RFC3339),
			CreatedAtEpoch:  now.UnixMilli(),
		},
		{
			SourceID:        2,
			TargetID:        3,
			RelationType:    models.RelationFixes,
			Confidence:      0.9,
			DetectionSource: models.DetectionSourceTemporalProximity,
			CreatedAt:       now.Format(time.RFC3339),
			CreatedAtEpoch:  now.UnixMilli(),
		},
	}

	err := relationStore.StoreRelations(ctx, relations)
	require.NoError(t, err)

	// Get relations for observation 2 (involved in both)
	result, err := relationStore.GetRelationsByObservationID(ctx, 2)
	require.NoError(t, err)
	assert.Len(t, result, 2)
}

func TestRelationStore_GetOutgoingAndIncomingRelations(t *testing.T) {
	relationStore, _, cleanup := testRelationStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	relations := []*models.ObservationRelation{
		{
			SourceID:        2,
			TargetID:        1,
			RelationType:    models.RelationCauses,
			Confidence:      0.8,
			DetectionSource: models.DetectionSourceFileOverlap,
			CreatedAt:       now.Format(time.RFC3339),
			CreatedAtEpoch:  now.UnixMilli(),
		},
		{
			SourceID:        3,
			TargetID:        2,
			RelationType:    models.RelationFixes,
			Confidence:      0.9,
			DetectionSource: models.DetectionSourceTemporalProximity,
			CreatedAt:       now.Format(time.RFC3339),
			CreatedAtEpoch:  now.UnixMilli(),
		},
	}

	err := relationStore.StoreRelations(ctx, relations)
	require.NoError(t, err)

	// Observation 2 has 1 outgoing (to 1) and 1 incoming (from 3)
	outgoing, err := relationStore.GetOutgoingRelations(ctx, 2)
	require.NoError(t, err)
	assert.Len(t, outgoing, 1)
	assert.Equal(t, int64(1), outgoing[0].TargetID)

	incoming, err := relationStore.GetIncomingRelations(ctx, 2)
	require.NoError(t, err)
	assert.Len(t, incoming, 1)
	assert.Equal(t, int64(3), incoming[0].SourceID)
}

func TestRelationStore_GetRelationCount(t *testing.T) {
	relationStore, _, cleanup := testRelationStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	relations := []*models.ObservationRelation{
		{
			SourceID:        1,
			TargetID:        2,
			RelationType:    models.RelationCauses,
			Confidence:      0.8,
			DetectionSource: models.DetectionSourceFileOverlap,
			CreatedAt:       now.Format(time.RFC3339),
			CreatedAtEpoch:  now.UnixMilli(),
		},
		{
			SourceID:        2,
			TargetID:        3,
			RelationType:    models.RelationFixes,
			Confidence:      0.9,
			DetectionSource: models.DetectionSourceTemporalProximity,
			CreatedAt:       now.Format(time.RFC3339),
			CreatedAtEpoch:  now.UnixMilli(),
		},
	}

	err := relationStore.StoreRelations(ctx, relations)
	require.NoError(t, err)

	count, err := relationStore.GetRelationCount(ctx, 2)
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	count, err = relationStore.GetRelationCount(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestRelationStore_DeleteRelationsByObservationID(t *testing.T) {
	relationStore, _, cleanup := testRelationStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	relations := []*models.ObservationRelation{
		{
			SourceID:        1,
			TargetID:        2,
			RelationType:    models.RelationCauses,
			Confidence:      0.8,
			DetectionSource: models.DetectionSourceFileOverlap,
			CreatedAt:       now.Format(time.RFC3339),
			CreatedAtEpoch:  now.UnixMilli(),
		},
		{
			SourceID:        2,
			TargetID:        3,
			RelationType:    models.RelationFixes,
			Confidence:      0.9,
			DetectionSource: models.DetectionSourceTemporalProximity,
			CreatedAt:       now.Format(time.RFC3339),
			CreatedAtEpoch:  now.UnixMilli(),
		},
		{
			SourceID:        4,
			TargetID:        5,
			RelationType:    models.RelationRelatesTo,
			Confidence:      0.7,
			DetectionSource: models.DetectionSourceConceptOverlap,
			CreatedAt:       now.Format(time.RFC3339),
			CreatedAtEpoch:  now.UnixMilli(),
		},
	}

	err := relationStore.StoreRelations(ctx, relations)
	require.NoError(t, err)

	// Delete relations involving observation 2
	err = relationStore.DeleteRelationsByObservationID(ctx, 2)
	require.NoError(t, err)

	// Verify only 1 relation remains (4->5)
	total, err := relationStore.GetTotalRelationCount(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, total)
}
