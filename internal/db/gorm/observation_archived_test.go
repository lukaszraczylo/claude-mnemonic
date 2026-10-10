//go:build fts5

package gorm

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
)

// An archived note is kept, but it must stay out of search, context, lists, counts and the vector rebuild, and an
// export (which asks for it) still has it.
func TestArchivedObservationsAreKeptButHiddenEverywhereExceptAnExport(t *testing.T) {
	s, store, cleanup := testObservationStore(t)
	defer cleanup()
	ctx := context.Background()

	var ids []int64
	for _, title := range []string{"Zebra ledger reconciliation", "Quokka deployment checklist", "Ocelot retry budget"} {
		id, _, err := s.StoreObservation(ctx, "claude-1", "proj",
			&models.ParsedObservation{Type: models.ObsTypeDiscovery, Title: title, Narrative: title + " notes"}, 1, 10)
		require.NoError(t, err)
		ids = append(ids, id)
		time.Sleep(2 * time.Millisecond)
	}
	archivedID := ids[0] // the zebra note
	require.NoError(t, s.ArchiveObservation(ctx, archivedID, "test"))

	has := func(obs []*models.Observation) bool {
		for _, o := range obs {
			if o.ID == archivedID {
				return true
			}
		}
		return false
	}

	recent, err := s.GetRecentObservations(ctx, "proj", 10)
	require.NoError(t, err)
	assert.False(t, has(recent), "context injection does not see it")

	strict, err := s.GetObservationsByProjectStrict(ctx, "proj", 10)
	require.NoError(t, err)
	assert.False(t, has(strict))

	all, err := s.GetAllRecentObservations(ctx, 10)
	require.NoError(t, err)
	assert.False(t, has(all))

	byID, err := s.GetObservationsByIDs(ctx, ids, "default", 0)
	require.NoError(t, err)
	assert.False(t, has(byID), "a vector hit for it is dropped when it is loaded")
	assert.Len(t, byID, 2)

	ordered, err := s.GetObservationsByIDsPreserveOrder(ctx, ids)
	require.NoError(t, err)
	assert.False(t, has(ordered))

	fts, err := s.SearchObservationsFTS(ctx, "zebra ledger", "proj", 10)
	require.NoError(t, err)
	assert.False(t, has(fts), "full-text search does not find it")
	other, err := s.SearchObservationsFTS(ctx, "quokka deployment", "proj", 10)
	require.NoError(t, err)
	assert.Len(t, other, 1, "and still finds the others")

	n, err := s.GetObservationCount(ctx, "proj")
	require.NoError(t, err)
	assert.Equal(t, 2, n, "the count is of notes that can be used")

	rebuild, err := s.GetAllObservations(ctx)
	require.NoError(t, err)
	assert.False(t, has(rebuild), "a vector rebuild does not put it back into the index")

	listed, total, err := s.GetAllRecentObservationsPaginated(ctx, 10, 0, false)
	require.NoError(t, err)
	assert.False(t, has(listed))
	assert.EqualValues(t, 2, total)
	projListed, projTotal, err := s.GetObservationsByProjectStrictPaginated(ctx, "proj", 10, 0, false)
	require.NoError(t, err)
	assert.False(t, has(projListed))
	assert.EqualValues(t, 2, projTotal)

	exported, total, err := s.GetAllRecentObservationsPaginated(ctx, 10, 0, true)
	require.NoError(t, err)
	assert.True(t, has(exported), "an export asks for them")
	assert.EqualValues(t, 3, total)
	projExported, _, err := s.GetObservationsByProjectStrictPaginated(ctx, "proj", 10, 0, true)
	require.NoError(t, err)
	assert.True(t, has(projExported))

	// Kept: the row is still there, and restoring it makes it visible again.
	one, err := s.GetObservationByID(ctx, archivedID)
	require.NoError(t, err)
	require.NotNil(t, one)
	require.NoError(t, s.UnarchiveObservation(ctx, archivedID))
	back, err := s.SearchObservationsFTS(ctx, "zebra ledger", "proj", 10)
	require.NoError(t, err)
	assert.True(t, has(back))
	var rows int64
	require.NoError(t, store.DB.Model(&Observation{}).Where("project = ?", "proj").Count(&rows).Error)
	assert.EqualValues(t, 3, rows)
}
