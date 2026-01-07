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

// testConflictStore creates a ConflictStore with a temporary database for testing.
func testConflictStore(t *testing.T) (*ConflictStore, *Store, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "gorm_conflict_test_*")
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

	conflictStore := NewConflictStore(store)

	cleanup := func() {
		store.Close()
		os.RemoveAll(tmpDir)
	}

	return conflictStore, store, cleanup
}

func TestConflictStore_StoreConflict(t *testing.T) {
	conflictStore, store, cleanup := testConflictStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create session for observations
	sessionStore := NewSessionStore(store)
	sessionID, err := sessionStore.CreateSDKSession(ctx, "claude-1", "test-project", "")
	require.NoError(t, err)

	// Create test observations
	obsStore := NewObservationStore(store, nil, nil, nil)
	obs1 := &models.ParsedObservation{
		Type:  models.ObsTypeDiscovery,
		Title: "Newer observation",
	}
	obsID1, _, err := obsStore.StoreObservation(ctx, "claude-1", "test-project", obs1, int(sessionID), 1)
	require.NoError(t, err)

	obs2 := &models.ParsedObservation{
		Type:  models.ObsTypeDiscovery,
		Title: "Older observation",
	}
	obsID2, _, err := obsStore.StoreObservation(ctx, "claude-1", "test-project", obs2, int(sessionID), 2)
	require.NoError(t, err)

	// Create conflict
	now := time.Now()
	conflict := &models.ObservationConflict{
		NewerObsID:      obsID1,
		OlderObsID:      obsID2,
		ConflictType:    models.ConflictContradicts,
		Resolution:      models.ResolutionPreferNewer,
		Reason:          "Newer observation contradicts older one",
		DetectedAt:      now.Format(time.RFC3339),
		DetectedAtEpoch: now.UnixMilli(),
		Resolved:        false,
	}

	id, err := conflictStore.StoreConflict(ctx, conflict)
	require.NoError(t, err)
	assert.Greater(t, id, int64(0))

	// Verify conflict was stored
	var count int64
	store.DB.Model(&ObservationConflict{}).Where("id = ?", id).Count(&count)
	assert.Equal(t, int64(1), count)
}

func TestConflictStore_MarkObservationSuperseded(t *testing.T) {
	conflictStore, store, cleanup := testConflictStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create observation
	sessionStore := NewSessionStore(store)
	sessionID, err := sessionStore.CreateSDKSession(ctx, "claude-1", "test-project", "")
	require.NoError(t, err)

	obsStore := NewObservationStore(store, nil, nil, nil)
	obs := &models.ParsedObservation{
		Type:  models.ObsTypeDiscovery,
		Title: "Test observation",
	}
	obsID, _, err := obsStore.StoreObservation(ctx, "claude-1", "test-project", obs, int(sessionID), 1)
	require.NoError(t, err)

	// Mark as superseded
	err = conflictStore.MarkObservationSuperseded(ctx, obsID)
	require.NoError(t, err)

	// Verify it's marked
	var dbObs Observation
	store.DB.First(&dbObs, obsID)
	assert.Equal(t, 1, dbObs.IsSuperseded)
}

func TestConflictStore_MarkObservationsSuperseded(t *testing.T) {
	conflictStore, store, cleanup := testConflictStore(t)
	defer cleanup()

	ctx := context.Background()

	tests := []struct {
		name   string
		obsIDs []int64
		setup  func() []int64
	}{
		{
			name:   "empty list",
			obsIDs: []int64{},
			setup:  func() []int64 { return []int64{} },
		},
		{
			name: "single observation",
			setup: func() []int64 {
				sessionStore := NewSessionStore(store)
				sessionID, _ := sessionStore.CreateSDKSession(ctx, "claude-1", "test-project", "")
				obsStore := NewObservationStore(store, nil, nil, nil)
				obs := &models.ParsedObservation{
					Type:  models.ObsTypeDiscovery,
					Title: "Test",
				}
				id, _, _ := obsStore.StoreObservation(ctx, "claude-1", "test-project", obs, int(sessionID), 1)
				return []int64{id}
			},
		},
		{
			name: "multiple observations",
			setup: func() []int64 {
				sessionStore := NewSessionStore(store)
				sessionID, _ := sessionStore.CreateSDKSession(ctx, "claude-1", "test-project", "")
				obsStore := NewObservationStore(store, nil, nil, nil)
				var ids []int64
				for i := 0; i < 3; i++ {
					obs := &models.ParsedObservation{
						Type:  models.ObsTypeDiscovery,
						Title: "Test",
					}
					id, _, _ := obsStore.StoreObservation(ctx, "claude-1", "test-project", obs, int(sessionID), int64(i+1))
					ids = append(ids, id)
				}
				return ids
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obsIDs := tt.setup()
			err := conflictStore.MarkObservationsSuperseded(ctx, obsIDs)
			require.NoError(t, err)

			if len(obsIDs) > 0 {
				// Verify all are marked
				for _, id := range obsIDs {
					var dbObs Observation
					store.DB.First(&dbObs, id)
					assert.Equal(t, 1, dbObs.IsSuperseded)
				}
			}
		})
	}
}

func TestConflictStore_GetConflictsByObservationID(t *testing.T) {
	conflictStore, store, cleanup := testConflictStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create observations
	sessionStore := NewSessionStore(store)
	sessionID, err := sessionStore.CreateSDKSession(ctx, "claude-1", "test-project", "")
	require.NoError(t, err)

	obsStore := NewObservationStore(store, nil, nil, nil)
	var obsIDs []int64
	for i := 0; i < 3; i++ {
		obs := &models.ParsedObservation{
			Type:  models.ObsTypeDiscovery,
			Title: "Test",
		}
		id, _, err := obsStore.StoreObservation(ctx, "claude-1", "test-project", obs, int(sessionID), int64(i+1))
		require.NoError(t, err)
		obsIDs = append(obsIDs, id)
	}

	// Create conflicts involving observation 2 (index 1)
	now := time.Now()
	conflict1 := &models.ObservationConflict{
		NewerObsID:      obsIDs[0],
		OlderObsID:      obsIDs[1],
		ConflictType:    models.ConflictContradicts,
		Resolution:      models.ResolutionPreferNewer,
		Reason:          "reason1",
		DetectedAt:      now.Format(time.RFC3339),
		DetectedAtEpoch: now.UnixMilli(),
	}
	_, err = conflictStore.StoreConflict(ctx, conflict1)
	require.NoError(t, err)

	conflict2 := &models.ObservationConflict{
		NewerObsID:      obsIDs[1],
		OlderObsID:      obsIDs[2],
		ConflictType:    models.ConflictSuperseded,
		Resolution:      models.ResolutionPreferNewer,
		Reason:          "reason2",
		DetectedAt:      now.Format(time.RFC3339),
		DetectedAtEpoch: now.UnixMilli(),
	}
	_, err = conflictStore.StoreConflict(ctx, conflict2)
	require.NoError(t, err)

	// Get conflicts for observation 2 (involved in 2 conflicts)
	conflicts, err := conflictStore.GetConflictsByObservationID(ctx, obsIDs[1])
	require.NoError(t, err)
	assert.Len(t, conflicts, 2)
}

func TestConflictStore_GetUnresolvedConflicts(t *testing.T) {
	conflictStore, store, cleanup := testConflictStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create observations
	sessionStore := NewSessionStore(store)
	sessionID, err := sessionStore.CreateSDKSession(ctx, "claude-1", "test-project", "")
	require.NoError(t, err)

	obsStore := NewObservationStore(store, nil, nil, nil)
	obs1 := &models.ParsedObservation{
		Type:  models.ObsTypeDiscovery,
		Title: "Test1",
	}
	obsID1, _, err := obsStore.StoreObservation(ctx, "claude-1", "test-project", obs1, int(sessionID), 1)
	require.NoError(t, err)

	obs2 := &models.ParsedObservation{
		Type:  models.ObsTypeDiscovery,
		Title: "Test2",
	}
	obsID2, _, err := obsStore.StoreObservation(ctx, "claude-1", "test-project", obs2, int(sessionID), 2)
	require.NoError(t, err)

	// Create unresolved conflicts
	now := time.Now()
	for i := 0; i < 5; i++ {
		conflict := &models.ObservationConflict{
			NewerObsID:      obsID1,
			OlderObsID:      obsID2,
			ConflictType:    models.ConflictContradicts,
			Resolution:      models.ResolutionPreferNewer,
			Reason:          "reason",
			DetectedAt:      now.Format(time.RFC3339),
			DetectedAtEpoch: now.UnixMilli(),
			Resolved:        false,
		}
		_, err = conflictStore.StoreConflict(ctx, conflict)
		require.NoError(t, err)
	}

	// Create resolved conflict
	resolvedAt := now.Format(time.RFC3339)
	resolvedConflict := &models.ObservationConflict{
		NewerObsID:      obsID1,
		OlderObsID:      obsID2,
		ConflictType:    models.ConflictContradicts,
		Resolution:      models.ResolutionPreferNewer,
		Reason:          "reason",
		DetectedAt:      now.Format(time.RFC3339),
		DetectedAtEpoch: now.UnixMilli(),
		Resolved:        true,
		ResolvedAt:      &resolvedAt,
	}
	_, err = conflictStore.StoreConflict(ctx, resolvedConflict)
	require.NoError(t, err)

	// Get unresolved conflicts with limit
	conflicts, err := conflictStore.GetUnresolvedConflicts(ctx, 3)
	require.NoError(t, err)
	assert.Len(t, conflicts, 3)

	// Verify all are unresolved
	for _, c := range conflicts {
		assert.False(t, c.Resolved)
	}
}

func TestConflictStore_GetSupersededObservationIDs(t *testing.T) {
	conflictStore, store, cleanup := testConflictStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create observations
	sessionStore := NewSessionStore(store)
	sessionID, err := sessionStore.CreateSDKSession(ctx, "claude-1", "test-project", "")
	require.NoError(t, err)

	obsStore := NewObservationStore(store, nil, nil, nil)

	// Create newer observations
	newer1 := &models.ParsedObservation{
		Type:  models.ObsTypeDiscovery,
		Title: "Newer1",
	}
	newerID1, _, err := obsStore.StoreObservation(ctx, "claude-1", "test-project", newer1, int(sessionID), 1)
	require.NoError(t, err)

	newer2 := &models.ParsedObservation{
		Type:  models.ObsTypeDiscovery,
		Title: "Newer2",
	}
	newerID2, _, err := obsStore.StoreObservation(ctx, "claude-1", "test-project", newer2, int(sessionID), 2)
	require.NoError(t, err)

	// Create older observations
	older1 := &models.ParsedObservation{
		Type:  models.ObsTypeDiscovery,
		Title: "Older1",
	}
	olderID1, _, err := obsStore.StoreObservation(ctx, "claude-1", "test-project", older1, int(sessionID), 3)
	require.NoError(t, err)

	older2 := &models.ParsedObservation{
		Type:  models.ObsTypeDiscovery,
		Title: "Older2",
	}
	olderID2, _, err := obsStore.StoreObservation(ctx, "claude-1", "test-project", older2, int(sessionID), 4)
	require.NoError(t, err)

	// Mark older observations as superseded
	err = conflictStore.MarkObservationsSuperseded(ctx, []int64{olderID1, olderID2})
	require.NoError(t, err)

	// Create conflicts with prefer_newer resolution
	now := time.Now()
	conflict1 := &models.ObservationConflict{
		NewerObsID:      newerID1,
		OlderObsID:      olderID1,
		ConflictType:    models.ConflictSuperseded,
		Resolution:      models.ResolutionPreferNewer,
		Reason:          "reason1",
		DetectedAt:      now.Format(time.RFC3339),
		DetectedAtEpoch: now.UnixMilli(),
	}
	_, err = conflictStore.StoreConflict(ctx, conflict1)
	require.NoError(t, err)

	conflict2 := &models.ObservationConflict{
		NewerObsID:      newerID2,
		OlderObsID:      olderID2,
		ConflictType:    models.ConflictSuperseded,
		Resolution:      models.ResolutionPreferNewer,
		Reason:          "reason2",
		DetectedAt:      now.Format(time.RFC3339),
		DetectedAtEpoch: now.UnixMilli(),
	}
	_, err = conflictStore.StoreConflict(ctx, conflict2)
	require.NoError(t, err)

	// Get superseded IDs (should return older observation IDs)
	ids, err := conflictStore.GetSupersededObservationIDs(ctx, "test-project")
	require.NoError(t, err)
	assert.Len(t, ids, 2)
	assert.Contains(t, ids, olderID1)
	assert.Contains(t, ids, olderID2)
}

func TestConflictStore_ResolveConflict(t *testing.T) {
	conflictStore, _, cleanup := testConflictStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create a simple conflict by inserting directly to DB
	conflict := &ObservationConflict{
		NewerObsID:      1,
		OlderObsID:      2,
		ConflictType:    models.ConflictContradicts,
		Resolution:      models.ResolutionManual,
		DetectedAt:      time.Now().Format(time.RFC3339),
		DetectedAtEpoch: time.Now().UnixMilli(),
		Resolved:        0,
	}
	conflictStore.db.Create(conflict)

	// Resolve conflict
	err := conflictStore.ResolveConflict(ctx, conflict.ID, models.ResolutionPreferNewer)
	require.NoError(t, err)

	// Verify resolved
	var resolved ObservationConflict
	conflictStore.db.First(&resolved, conflict.ID)
	assert.Equal(t, 1, resolved.Resolved)
	assert.True(t, resolved.ResolvedAt.Valid)
	assert.Equal(t, models.ResolutionPreferNewer, resolved.Resolution)
}

func TestConflictStore_DeleteConflictsByObservationID(t *testing.T) {
	conflictStore, _, cleanup := testConflictStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create conflicts directly in DB
	now := time.Now()
	conflicts := []ObservationConflict{
		{
			NewerObsID:      1,
			OlderObsID:      2,
			ConflictType:    models.ConflictContradicts,
			Resolution:      models.ResolutionPreferNewer,
			DetectedAt:      now.Format(time.RFC3339),
			DetectedAtEpoch: now.UnixMilli(),
		},
		{
			NewerObsID:      3,
			OlderObsID:      1,
			ConflictType:    models.ConflictContradicts,
			Resolution:      models.ResolutionPreferNewer,
			DetectedAt:      now.Format(time.RFC3339),
			DetectedAtEpoch: now.UnixMilli(),
		},
		{
			NewerObsID:      2,
			OlderObsID:      3,
			ConflictType:    models.ConflictContradicts,
			Resolution:      models.ResolutionPreferNewer,
			DetectedAt:      now.Format(time.RFC3339),
			DetectedAtEpoch: now.UnixMilli(),
		},
	}
	for _, c := range conflicts {
		conflictStore.db.Create(&c)
	}

	// Delete conflicts for observation 1
	err := conflictStore.DeleteConflictsByObservationID(ctx, 1)
	require.NoError(t, err)

	// Verify only conflicts involving 1 are deleted
	var count int64
	conflictStore.db.Model(&ObservationConflict{}).
		Where("newer_obs_id = 1 OR older_obs_id = 1").
		Count(&count)
	assert.Equal(t, int64(0), count)

	// Other conflict should still exist
	conflictStore.db.Model(&ObservationConflict{}).
		Where("newer_obs_id = 2 AND older_obs_id = 3").
		Count(&count)
	assert.Equal(t, int64(1), count)
}

func TestConflictStore_CleanupSupersededObservations(t *testing.T) {
	conflictStore, store, cleanup := testConflictStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create observations
	sessionStore := NewSessionStore(store)
	sessionID, err := sessionStore.CreateSDKSession(ctx, "claude-1", "test-project", "")
	require.NoError(t, err)

	obsStore := NewObservationStore(store, nil, nil, nil)

	// Create newer observations
	newer1 := &models.ParsedObservation{
		Type:  models.ObsTypeDiscovery,
		Title: "Newer1",
	}
	newerID1, _, err := obsStore.StoreObservation(ctx, "claude-1", "test-project", newer1, int(sessionID), 1)
	require.NoError(t, err)

	newer2 := &models.ParsedObservation{
		Type:  models.ObsTypeDiscovery,
		Title: "Newer2",
	}
	newerID2, _, err := obsStore.StoreObservation(ctx, "claude-1", "test-project", newer2, int(sessionID), 2)
	require.NoError(t, err)

	// Create older observations
	older1 := &models.ParsedObservation{
		Type:  models.ObsTypeDiscovery,
		Title: "OldSuperseded",
	}
	oldSupersededID, _, err := obsStore.StoreObservation(ctx, "claude-1", "test-project", older1, int(sessionID), 3)
	require.NoError(t, err)

	older2 := &models.ParsedObservation{
		Type:  models.ObsTypeDiscovery,
		Title: "RecentSuperseded",
	}
	recentSupersededID, _, err := obsStore.StoreObservation(ctx, "claude-1", "test-project", older2, int(sessionID), 4)
	require.NoError(t, err)

	// Mark as superseded
	err = conflictStore.MarkObservationsSuperseded(ctx, []int64{oldSupersededID, recentSupersededID})
	require.NoError(t, err)

	// Create conflicts
	oldTime := time.Now().AddDate(0, 0, -SupersededRetentionDays-1)
	recentTime := time.Now().AddDate(0, 0, -1)

	// Old conflict (should be deleted)
	oldConflict := &models.ObservationConflict{
		NewerObsID:      newerID1,
		OlderObsID:      oldSupersededID,
		ConflictType:    models.ConflictSuperseded,
		Resolution:      models.ResolutionPreferNewer,
		Reason:          "old",
		DetectedAt:      oldTime.Format(time.RFC3339),
		DetectedAtEpoch: oldTime.UnixMilli(),
	}
	_, err = conflictStore.StoreConflict(ctx, oldConflict)
	require.NoError(t, err)

	// Recent conflict (should be kept)
	recentConflict := &models.ObservationConflict{
		NewerObsID:      newerID2,
		OlderObsID:      recentSupersededID,
		ConflictType:    models.ConflictSuperseded,
		Resolution:      models.ResolutionPreferNewer,
		Reason:          "recent",
		DetectedAt:      recentTime.Format(time.RFC3339),
		DetectedAtEpoch: recentTime.UnixMilli(),
	}
	_, err = conflictStore.StoreConflict(ctx, recentConflict)
	require.NoError(t, err)

	// Cleanup old superseded observations
	deletedIDs, err := conflictStore.CleanupSupersededObservations(ctx, "test-project")
	require.NoError(t, err)
	assert.Len(t, deletedIDs, 1)
	assert.Contains(t, deletedIDs, oldSupersededID)

	// Verify only old superseded observation was deleted
	var count int64
	store.DB.Model(&Observation{}).Count(&count)
	assert.Equal(t, int64(3), count) // newer1, newer2, recentSuperseded remain

	// Verify old observation was deleted
	store.DB.Model(&Observation{}).Where("id = ?", oldSupersededID).Count(&count)
	assert.Equal(t, int64(0), count)
}

func TestConflictStore_GetConflictsWithDetails(t *testing.T) {
	conflictStore, store, cleanup := testConflictStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create observations
	sessionStore := NewSessionStore(store)
	sessionID, err := sessionStore.CreateSDKSession(ctx, "claude-1", "test-project", "")
	require.NoError(t, err)

	obsStore := NewObservationStore(store, nil, nil, nil)

	newer := &models.ParsedObservation{
		Type:  models.ObsTypeDiscovery,
		Title: "Newer observation",
	}
	newerID, _, err := obsStore.StoreObservation(ctx, "claude-1", "test-project", newer, int(sessionID), 1)
	require.NoError(t, err)

	older := &models.ParsedObservation{
		Type:  models.ObsTypeDiscovery,
		Title: "Older observation",
	}
	olderID, _, err := obsStore.StoreObservation(ctx, "claude-1", "test-project", older, int(sessionID), 2)
	require.NoError(t, err)

	// Create conflict
	now := time.Now()
	conflict := &models.ObservationConflict{
		NewerObsID:      newerID,
		OlderObsID:      olderID,
		ConflictType:    models.ConflictContradicts,
		Resolution:      models.ResolutionPreferNewer,
		Reason:          "Test conflict",
		DetectedAt:      now.Format(time.RFC3339),
		DetectedAtEpoch: now.UnixMilli(),
	}
	_, err = conflictStore.StoreConflict(ctx, conflict)
	require.NoError(t, err)

	// Get conflicts with details
	conflicts, err := conflictStore.GetConflictsWithDetails(ctx, "test-project", 10)
	require.NoError(t, err)
	assert.Len(t, conflicts, 1)

	// Verify conflict details
	assert.Equal(t, newerID, conflicts[0].Conflict.NewerObsID)
	assert.Equal(t, olderID, conflicts[0].Conflict.OlderObsID)
	assert.Equal(t, models.ConflictContradicts, conflicts[0].Conflict.ConflictType)
	assert.Equal(t, "Test conflict", conflicts[0].Conflict.Reason)
	assert.Equal(t, "Newer observation", conflicts[0].NewerObsTitle)
	assert.Equal(t, "Older observation", conflicts[0].OlderObsTitle)
}
