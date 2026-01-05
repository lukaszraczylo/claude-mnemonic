package sqlite

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupConflictTables creates necessary tables for conflict testing.
func setupConflictTables(t *testing.T, db *sql.DB) {
	t.Helper()

	// Create observations table
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS observations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id INTEGER NOT NULL,
			project TEXT NOT NULL,
			title TEXT,
			is_superseded INTEGER DEFAULT 0,
			scope TEXT DEFAULT 'project'
		)
	`)
	require.NoError(t, err)

	// Create observation_conflicts table
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS observation_conflicts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			newer_obs_id INTEGER NOT NULL,
			older_obs_id INTEGER NOT NULL,
			conflict_type TEXT NOT NULL,
			resolution TEXT NOT NULL,
			reason TEXT,
			detected_at TEXT NOT NULL,
			detected_at_epoch INTEGER NOT NULL,
			resolved INTEGER DEFAULT 0,
			resolved_at TEXT,
			FOREIGN KEY (newer_obs_id) REFERENCES observations(id),
			FOREIGN KEY (older_obs_id) REFERENCES observations(id)
		)
	`)
	require.NoError(t, err)
}

func TestNewConflictStore(t *testing.T) {
	db, _, cleanup := testDB(t)
	defer cleanup()

	store := newStoreFromDB(db)
	conflictStore := NewConflictStore(store)

	assert.NotNil(t, conflictStore)
	assert.Equal(t, store, conflictStore.store)
}

func TestConflictStore_StoreConflict(t *testing.T) {
	db, _, cleanup := testDB(t)
	defer cleanup()
	setupConflictTables(t, db)

	store := newStoreFromDB(db)
	conflictStore := NewConflictStore(store)
	ctx := context.Background()

	// Insert test observations
	_, err := db.Exec("INSERT INTO observations (id, session_id, project, title) VALUES (1, 1, 'test', 'obs1')")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO observations (id, session_id, project, title) VALUES (2, 1, 'test', 'obs2')")
	require.NoError(t, err)

	now := time.Now()
	nowStr := now.Format(time.RFC3339)
	conflict := &models.ObservationConflict{
		NewerObsID:      1,
		OlderObsID:      2,
		ConflictType:    models.ConflictContradicts,
		Resolution:      models.ResolutionPreferNewer,
		Reason:          "Newer observation contradicts older one",
		DetectedAt:      nowStr,
		DetectedAtEpoch: now.Unix(),
		Resolved:        false,
		ResolvedAt:      nil,
	}

	id, err := conflictStore.StoreConflict(ctx, conflict)
	require.NoError(t, err)
	assert.Greater(t, id, int64(0))

	// Verify conflict was stored
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM observation_conflicts WHERE id = ?", id).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestConflictStore_MarkObservationSuperseded(t *testing.T) {
	db, _, cleanup := testDB(t)
	defer cleanup()
	setupConflictTables(t, db)

	store := newStoreFromDB(db)
	conflictStore := NewConflictStore(store)
	ctx := context.Background()

	// Insert test observation
	result, err := db.Exec("INSERT INTO observations (session_id, project, title, is_superseded) VALUES (1, 'test', 'obs1', 0)")
	require.NoError(t, err)
	obsID, err := result.LastInsertId()
	require.NoError(t, err)

	// Mark as superseded
	err = conflictStore.MarkObservationSuperseded(ctx, obsID)
	require.NoError(t, err)

	// Verify it's marked
	var isSuperseded bool
	err = db.QueryRow("SELECT is_superseded FROM observations WHERE id = ?", obsID).Scan(&isSuperseded)
	require.NoError(t, err)
	assert.True(t, isSuperseded)
}

func TestConflictStore_MarkObservationsSuperseded(t *testing.T) {
	db, _, cleanup := testDB(t)
	defer cleanup()
	setupConflictTables(t, db)

	store := newStoreFromDB(db)
	conflictStore := NewConflictStore(store)
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
				result, err := db.Exec("INSERT INTO observations (session_id, project, title) VALUES (1, 'test', 'obs1')")
				require.NoError(t, err)
				id, err := result.LastInsertId()
				require.NoError(t, err)
				return []int64{id}
			},
		},
		{
			name: "multiple observations",
			setup: func() []int64 {
				var ids []int64
				for i := 0; i < 3; i++ {
					result, err := db.Exec("INSERT INTO observations (session_id, project, title) VALUES (1, 'test', 'obs')")
					require.NoError(t, err)
					id, err := result.LastInsertId()
					require.NoError(t, err)
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
					var isSuperseded bool
					err = db.QueryRow("SELECT is_superseded FROM observations WHERE id = ?", id).Scan(&isSuperseded)
					require.NoError(t, err)
					assert.True(t, isSuperseded)
				}
			}
		})
	}
}

func TestConflictStore_GetConflictsByObservationID(t *testing.T) {
	db, _, cleanup := testDB(t)
	defer cleanup()
	setupConflictTables(t, db)

	store := newStoreFromDB(db)
	conflictStore := NewConflictStore(store)
	ctx := context.Background()

	// Insert test observations
	_, err := db.Exec("INSERT INTO observations (id, session_id, project, title) VALUES (1, 1, 'test', 'obs1')")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO observations (id, session_id, project, title) VALUES (2, 1, 'test', 'obs2')")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO observations (id, session_id, project, title) VALUES (3, 1, 'test', 'obs3')")
	require.NoError(t, err)

	// Insert conflicts
	now := time.Now()
	_, err = db.Exec(`INSERT INTO observation_conflicts
		(newer_obs_id, older_obs_id, conflict_type, resolution, reason, detected_at, detected_at_epoch, resolved)
		VALUES (1, 2, 'contradiction', 'supersede', 'reason1', ?, ?, 0)`, now.Format(time.RFC3339), now.Unix())
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO observation_conflicts
		(newer_obs_id, older_obs_id, conflict_type, resolution, reason, detected_at, detected_at_epoch, resolved)
		VALUES (2, 3, 'update', 'supersede', 'reason2', ?, ?, 0)`, now.Format(time.RFC3339), now.Unix())
	require.NoError(t, err)

	// Get conflicts for observation 2 (involved in 2 conflicts)
	conflicts, err := conflictStore.GetConflictsByObservationID(ctx, 2)
	require.NoError(t, err)
	assert.Len(t, conflicts, 2)
}

func TestConflictStore_GetUnresolvedConflicts(t *testing.T) {
	db, _, cleanup := testDB(t)
	defer cleanup()
	setupConflictTables(t, db)

	store := newStoreFromDB(db)
	conflictStore := NewConflictStore(store)
	ctx := context.Background()

	// Insert test observations
	_, err := db.Exec("INSERT INTO observations (id, session_id, project, title) VALUES (1, 1, 'test', 'obs1')")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO observations (id, session_id, project, title) VALUES (2, 1, 'test', 'obs2')")
	require.NoError(t, err)

	// Insert unresolved conflicts
	now := time.Now()
	for i := 0; i < 5; i++ {
		_, err = db.Exec(`INSERT INTO observation_conflicts
			(newer_obs_id, older_obs_id, conflict_type, resolution, reason, detected_at, detected_at_epoch, resolved)
			VALUES (1, 2, 'contradiction', 'supersede', 'reason', ?, ?, 0)`, now.Format(time.RFC3339), now.Unix())
		require.NoError(t, err)
	}

	// Insert resolved conflict
	_, err = db.Exec(`INSERT INTO observation_conflicts
		(newer_obs_id, older_obs_id, conflict_type, resolution, reason, detected_at, detected_at_epoch, resolved, resolved_at)
		VALUES (1, 2, 'contradiction', 'supersede', 'reason', ?, ?, 1, ?)`,
		now.Format(time.RFC3339), now.Unix(), now.Format(time.RFC3339))
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
	db, _, cleanup := testDB(t)
	defer cleanup()
	setupConflictTables(t, db)

	store := newStoreFromDB(db)
	conflictStore := NewConflictStore(store)
	ctx := context.Background()

	// Insert test observations (newer ones that supersede older ones)
	_, err := db.Exec("INSERT INTO observations (id, session_id, project, title, is_superseded) VALUES (1, 1, 'test', 'newer1', 0)")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO observations (id, session_id, project, title, is_superseded) VALUES (2, 1, 'test', 'older1', 1)")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO observations (id, session_id, project, title, is_superseded) VALUES (3, 1, 'test', 'newer2', 0)")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO observations (id, session_id, project, title, is_superseded) VALUES (4, 1, 'test', 'older2', 1)")
	require.NoError(t, err)

	// Insert conflicts with prefer_newer resolution
	now := time.Now()
	_, err = db.Exec(`INSERT INTO observation_conflicts
		(newer_obs_id, older_obs_id, conflict_type, resolution, reason, detected_at, detected_at_epoch, resolved)
		VALUES (1, 2, 'superseded', 'prefer_newer', 'reason1', ?, ?, 0)`, now.Format(time.RFC3339), now.Unix())
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO observation_conflicts
		(newer_obs_id, older_obs_id, conflict_type, resolution, reason, detected_at, detected_at_epoch, resolved)
		VALUES (3, 4, 'superseded', 'prefer_newer', 'reason2', ?, ?, 0)`, now.Format(time.RFC3339), now.Unix())
	require.NoError(t, err)

	// Get superseded IDs (should return IDs 2 and 4 - the older observations)
	ids, err := conflictStore.GetSupersededObservationIDs(ctx, "test")
	require.NoError(t, err)
	assert.Len(t, ids, 2)
	assert.Contains(t, ids, int64(2))
	assert.Contains(t, ids, int64(4))
}

func TestConflictStore_ResolveConflict(t *testing.T) {
	db, _, cleanup := testDB(t)
	defer cleanup()
	setupConflictTables(t, db)

	store := newStoreFromDB(db)
	conflictStore := NewConflictStore(store)
	ctx := context.Background()

	// Insert test observations and conflict
	_, err := db.Exec("INSERT INTO observations (id, session_id, project, title) VALUES (1, 1, 'test', 'obs1')")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO observations (id, session_id, project, title) VALUES (2, 1, 'test', 'obs2')")
	require.NoError(t, err)

	now := time.Now()
	result, err := db.Exec(`INSERT INTO observation_conflicts
		(newer_obs_id, older_obs_id, conflict_type, resolution, reason, detected_at, detected_at_epoch, resolved)
		VALUES (1, 2, 'contradiction', 'supersede', 'reason', ?, ?, 0)`, now.Format(time.RFC3339), now.Unix())
	require.NoError(t, err)
	conflictID, err := result.LastInsertId()
	require.NoError(t, err)

	// Resolve conflict
	err = conflictStore.ResolveConflict(ctx, conflictID, models.ResolutionPreferNewer)
	require.NoError(t, err)

	// Verify resolved
	var resolved bool
	var resolvedAt sql.NullString
	err = db.QueryRow("SELECT resolved, resolved_at FROM observation_conflicts WHERE id = ?", conflictID).Scan(&resolved, &resolvedAt)
	require.NoError(t, err)
	assert.True(t, resolved)
	assert.True(t, resolvedAt.Valid)
}

func TestConflictStore_DeleteConflictsByObservationID(t *testing.T) {
	db, _, cleanup := testDB(t)
	defer cleanup()
	setupConflictTables(t, db)

	store := newStoreFromDB(db)
	conflictStore := NewConflictStore(store)
	ctx := context.Background()

	// Insert test observations and conflicts
	_, err := db.Exec("INSERT INTO observations (id, session_id, project, title) VALUES (1, 1, 'test', 'obs1')")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO observations (id, session_id, project, title) VALUES (2, 1, 'test', 'obs2')")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO observations (id, session_id, project, title) VALUES (3, 1, 'test', 'obs3')")
	require.NoError(t, err)

	now := time.Now()
	// Conflicts involving observation 1
	_, err = db.Exec(`INSERT INTO observation_conflicts
		(newer_obs_id, older_obs_id, conflict_type, resolution, reason, detected_at, detected_at_epoch, resolved)
		VALUES (1, 2, 'contradiction', 'supersede', 'reason', ?, ?, 0)`, now.Format(time.RFC3339), now.Unix())
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO observation_conflicts
		(newer_obs_id, older_obs_id, conflict_type, resolution, reason, detected_at, detected_at_epoch, resolved)
		VALUES (3, 1, 'contradiction', 'supersede', 'reason', ?, ?, 0)`, now.Format(time.RFC3339), now.Unix())
	require.NoError(t, err)

	// Conflict not involving observation 1
	_, err = db.Exec(`INSERT INTO observation_conflicts
		(newer_obs_id, older_obs_id, conflict_type, resolution, reason, detected_at, detected_at_epoch, resolved)
		VALUES (2, 3, 'contradiction', 'supersede', 'reason', ?, ?, 0)`, now.Format(time.RFC3339), now.Unix())
	require.NoError(t, err)

	// Delete conflicts for observation 1
	err = conflictStore.DeleteConflictsByObservationID(ctx, 1)
	require.NoError(t, err)

	// Verify only conflicts involving 1 are deleted
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM observation_conflicts WHERE newer_obs_id = 1 OR older_obs_id = 1").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	// Other conflict should still exist
	err = db.QueryRow("SELECT COUNT(*) FROM observation_conflicts WHERE newer_obs_id = 2 AND older_obs_id = 3").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestConflictStore_CleanupSupersededObservations(t *testing.T) {
	db, _, cleanup := testDB(t)
	defer cleanup()
	setupConflictTables(t, db)

	store := newStoreFromDB(db)
	conflictStore := NewConflictStore(store)
	ctx := context.Background()

	// Old conflict time (more than 3 days ago)
	oldTime := time.Now().AddDate(0, 0, -SupersededRetentionDays-1)
	recentTime := time.Now().AddDate(0, 0, -1)

	// Insert observations
	// Newer observations
	_, err := db.Exec("INSERT INTO observations (id, session_id, project, title, is_superseded) VALUES (1, 1, 'test', 'newer1', 0)")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO observations (id, session_id, project, title, is_superseded) VALUES (3, 1, 'test', 'newer2', 0)")
	require.NoError(t, err)

	// Old superseded observation with old conflict (should be deleted)
	_, err = db.Exec("INSERT INTO observations (id, session_id, project, title, is_superseded) VALUES (2, 1, 'test', 'old_superseded', 1)")
	require.NoError(t, err)

	// Recent superseded observation with recent conflict (should be kept)
	_, err = db.Exec("INSERT INTO observations (id, session_id, project, title, is_superseded) VALUES (4, 1, 'test', 'recent_superseded', 1)")
	require.NoError(t, err)

	// Insert conflicts
	// Old conflict (detected > 3 days ago) - observation 2 should be deleted
	_, err = db.Exec(`INSERT INTO observation_conflicts
		(newer_obs_id, older_obs_id, conflict_type, resolution, reason, detected_at, detected_at_epoch, resolved)
		VALUES (1, 2, 'superseded', 'prefer_newer', 'reason', ?, ?, 0)`,
		oldTime.Format(time.RFC3339), oldTime.UnixMilli())
	require.NoError(t, err)

	// Recent conflict (detected < 3 days ago) - observation 4 should be kept
	_, err = db.Exec(`INSERT INTO observation_conflicts
		(newer_obs_id, older_obs_id, conflict_type, resolution, reason, detected_at, detected_at_epoch, resolved)
		VALUES (3, 4, 'superseded', 'prefer_newer', 'reason', ?, ?, 0)`,
		recentTime.Format(time.RFC3339), recentTime.UnixMilli())
	require.NoError(t, err)

	// Clean up old superseded observations
	deletedIDs, err := conflictStore.CleanupSupersededObservations(ctx, "test")
	require.NoError(t, err)
	assert.Len(t, deletedIDs, 1)
	assert.Contains(t, deletedIDs, int64(2))

	// Verify only old superseded observation was deleted
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM observations").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 3, count) // 1, 3, 4 remain

	// Verify observation 2 was deleted
	err = db.QueryRow("SELECT COUNT(*) FROM observations WHERE id = 2").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestConflictStore_GetConflictsWithDetails(t *testing.T) {
	db, _, cleanup := testDB(t)
	defer cleanup()
	setupConflictTables(t, db)

	store := newStoreFromDB(db)
	conflictStore := NewConflictStore(store)
	ctx := context.Background()

	// Insert test observations
	_, err := db.Exec("INSERT INTO observations (id, session_id, project, title) VALUES (1, 1, 'test', 'Newer observation')")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO observations (id, session_id, project, title) VALUES (2, 1, 'test', 'Older observation')")
	require.NoError(t, err)

	// Insert conflict
	now := time.Now()
	_, err = db.Exec(`INSERT INTO observation_conflicts
		(newer_obs_id, older_obs_id, conflict_type, resolution, reason, detected_at, detected_at_epoch, resolved)
		VALUES (1, 2, 'contradicts', 'prefer_newer', 'Test conflict', ?, ?, 0)`, now.Format(time.RFC3339), now.Unix())
	require.NoError(t, err)

	// Get conflicts with details
	conflicts, err := conflictStore.GetConflictsWithDetails(ctx, "test", 10)
	require.NoError(t, err)
	assert.Len(t, conflicts, 1)

	// Verify conflict details
	assert.Equal(t, int64(1), conflicts[0].Conflict.NewerObsID)
	assert.Equal(t, int64(2), conflicts[0].Conflict.OlderObsID)
	assert.Equal(t, models.ConflictContradicts, conflicts[0].Conflict.ConflictType)
	assert.Equal(t, "Test conflict", conflicts[0].Conflict.Reason)
	assert.Equal(t, "Newer observation", conflicts[0].NewerObsTitle)
	assert.Equal(t, "Older observation", conflicts[0].OlderObsTitle)
}
