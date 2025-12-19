// Package sqlite provides SQLite database operations for claude-mnemonic.
package sqlite

import (
	"context"
	"time"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
)

// SupersededRetentionDays is the number of days to keep superseded observations before deletion.
const SupersededRetentionDays = 3

// ConflictStore provides conflict-related database operations.
type ConflictStore struct {
	store *Store
}

// NewConflictStore creates a new conflict store.
func NewConflictStore(store *Store) *ConflictStore {
	return &ConflictStore{store: store}
}

// StoreConflict stores a new observation conflict.
func (s *ConflictStore) StoreConflict(ctx context.Context, conflict *models.ObservationConflict) (int64, error) {
	const query = `
		INSERT INTO observation_conflicts
		(newer_obs_id, older_obs_id, conflict_type, resolution, reason, detected_at, detected_at_epoch, resolved, resolved_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	result, err := s.store.ExecContext(ctx, query,
		conflict.NewerObsID, conflict.OlderObsID,
		string(conflict.ConflictType), string(conflict.Resolution),
		conflict.Reason, conflict.DetectedAt, conflict.DetectedAtEpoch,
		conflict.Resolved, conflict.ResolvedAt,
	)
	if err != nil {
		return 0, err
	}

	return result.LastInsertId()
}

// MarkObservationSuperseded marks an observation as superseded.
func (s *ConflictStore) MarkObservationSuperseded(ctx context.Context, obsID int64) error {
	const query = `UPDATE observations SET is_superseded = 1 WHERE id = ?`
	_, err := s.store.ExecContext(ctx, query, obsID)
	return err
}

// MarkObservationsSuperseded marks multiple observations as superseded.
func (s *ConflictStore) MarkObservationsSuperseded(ctx context.Context, obsIDs []int64) error {
	if len(obsIDs) == 0 {
		return nil
	}

	query := `UPDATE observations SET is_superseded = 1 WHERE id IN (?` + repeatPlaceholders(len(obsIDs)-1) + `)` // #nosec G202 -- uses parameterized placeholders
	args := int64SliceToInterface(obsIDs)
	_, err := s.store.db.ExecContext(ctx, query, args...)
	return err
}

// GetConflictsByObservationID retrieves all conflicts involving an observation.
func (s *ConflictStore) GetConflictsByObservationID(ctx context.Context, obsID int64) ([]*models.ObservationConflict, error) {
	const query = `
		SELECT id, newer_obs_id, older_obs_id, conflict_type, resolution, reason,
		       detected_at, detected_at_epoch, resolved, resolved_at
		FROM observation_conflicts
		WHERE newer_obs_id = ? OR older_obs_id = ?
		ORDER BY detected_at_epoch DESC
	`

	rows, err := s.store.QueryContext(ctx, query, obsID, obsID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanConflictRows(rows)
}

// GetUnresolvedConflicts retrieves all unresolved conflicts.
func (s *ConflictStore) GetUnresolvedConflicts(ctx context.Context, limit int) ([]*models.ObservationConflict, error) {
	const query = `
		SELECT id, newer_obs_id, older_obs_id, conflict_type, resolution, reason,
		       detected_at, detected_at_epoch, resolved, resolved_at
		FROM observation_conflicts
		WHERE resolved = 0
		ORDER BY detected_at_epoch DESC
		LIMIT ?
	`

	rows, err := s.store.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanConflictRows(rows)
}

// GetSupersededObservationIDs returns IDs of all observations that have been superseded.
func (s *ConflictStore) GetSupersededObservationIDs(ctx context.Context, project string) ([]int64, error) {
	const query = `
		SELECT DISTINCT older_obs_id
		FROM observation_conflicts oc
		JOIN observations o ON o.id = oc.older_obs_id
		WHERE oc.resolution = 'prefer_newer'
		  AND (o.project = ? OR o.scope = 'global')
	`

	rows, err := s.store.QueryContext(ctx, query, project)
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

// ResolveConflict marks a conflict as resolved.
func (s *ConflictStore) ResolveConflict(ctx context.Context, conflictID int64, resolution models.ConflictResolution) error {
	now := time.Now().Format(time.RFC3339)
	const query = `
		UPDATE observation_conflicts
		SET resolved = 1, resolved_at = ?, resolution = ?
		WHERE id = ?
	`
	_, err := s.store.ExecContext(ctx, query, now, string(resolution), conflictID)
	return err
}

// DeleteConflictsByObservationID deletes all conflicts involving an observation.
// Called when an observation is deleted.
func (s *ConflictStore) DeleteConflictsByObservationID(ctx context.Context, obsID int64) error {
	const query = `DELETE FROM observation_conflicts WHERE newer_obs_id = ? OR older_obs_id = ?`
	_, err := s.store.ExecContext(ctx, query, obsID, obsID)
	return err
}

// ConflictWithDetails contains a conflict with its observation details.
type ConflictWithDetails struct {
	Conflict      *models.ObservationConflict
	NewerObsTitle string
	OlderObsTitle string
}

// CleanupSupersededObservations deletes observations that have been superseded for longer than
// SupersededRetentionDays. Returns the IDs of deleted observations for downstream cleanup (e.g., vector DB).
func (s *ConflictStore) CleanupSupersededObservations(ctx context.Context, project string) ([]int64, error) {
	// Calculate cutoff time (3 days ago in milliseconds)
	cutoffEpoch := time.Now().AddDate(0, 0, -SupersededRetentionDays).UnixMilli()

	// First, find the IDs that will be deleted
	// We delete observations that:
	// 1. Are marked as superseded
	// 2. Have a conflict record where they are the older observation
	// 3. The conflict was detected more than 3 days ago
	const selectQuery = `
		SELECT DISTINCT o.id FROM observations o
		JOIN observation_conflicts oc ON o.id = oc.older_obs_id
		WHERE o.is_superseded = 1
		  AND o.project = ?
		  AND oc.detected_at_epoch < ?
	`

	rows, err := s.store.QueryContext(ctx, selectQuery, project, cutoffEpoch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var toDelete []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		toDelete = append(toDelete, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(toDelete) == 0 {
		return nil, nil
	}

	// Delete the conflict records first (due to foreign key constraints)
	for _, obsID := range toDelete {
		if err := s.DeleteConflictsByObservationID(ctx, obsID); err != nil {
			return nil, err
		}
	}

	// Delete the observations
	deleteQuery := `DELETE FROM observations WHERE id IN (?` + repeatPlaceholders(len(toDelete)-1) + `)` // #nosec G202 -- uses parameterized placeholders
	args := int64SliceToInterface(toDelete)
	_, err = s.store.db.ExecContext(ctx, deleteQuery, args...)
	if err != nil {
		return nil, err
	}

	return toDelete, nil
}

// GetConflictsWithDetails retrieves all conflicts with observation titles for display.
func (s *ConflictStore) GetConflictsWithDetails(ctx context.Context, project string, limit int) ([]*ConflictWithDetails, error) {
	const query = `
		SELECT oc.id, oc.newer_obs_id, oc.older_obs_id, oc.conflict_type, oc.resolution, oc.reason,
		       oc.detected_at, oc.detected_at_epoch, oc.resolved, oc.resolved_at,
		       COALESCE(newer.title, '') as newer_title,
		       COALESCE(older.title, '') as older_title
		FROM observation_conflicts oc
		JOIN observations newer ON newer.id = oc.newer_obs_id
		JOIN observations older ON older.id = oc.older_obs_id
		WHERE newer.project = ? OR older.project = ?
		ORDER BY oc.detected_at_epoch DESC
		LIMIT ?
	`

	rows, err := s.store.QueryContext(ctx, query, project, project, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []*ConflictWithDetails
	for rows.Next() {
		var c models.ObservationConflict
		var cwd ConflictWithDetails
		if err := rows.Scan(
			&c.ID, &c.NewerObsID, &c.OlderObsID,
			&c.ConflictType, &c.Resolution, &c.Reason,
			&c.DetectedAt, &c.DetectedAtEpoch,
			&c.Resolved, &c.ResolvedAt,
			&cwd.NewerObsTitle, &cwd.OlderObsTitle,
		); err != nil {
			return nil, err
		}
		cwd.Conflict = &c
		results = append(results, &cwd)
	}
	return results, rows.Err()
}

// scanConflictRows scans multiple conflicts from rows.
func (s *ConflictStore) scanConflictRows(rows interface {
	Next() bool
	Scan(...interface{}) error
	Err() error
}) ([]*models.ObservationConflict, error) {
	var conflicts []*models.ObservationConflict
	for rows.Next() {
		var c models.ObservationConflict
		if err := rows.Scan(
			&c.ID, &c.NewerObsID, &c.OlderObsID,
			&c.ConflictType, &c.Resolution, &c.Reason,
			&c.DetectedAt, &c.DetectedAtEpoch,
			&c.Resolved, &c.ResolvedAt,
		); err != nil {
			return nil, err
		}
		conflicts = append(conflicts, &c)
	}
	return conflicts, rows.Err()
}
