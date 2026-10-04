package gorm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
)

func storeForUpdate(t *testing.T) (*ObservationStore, int64, func()) {
	t.Helper()
	obsStore, store, cleanup := testObservationStore(t)
	ctx := context.Background()
	sessionID, err := NewSessionStore(store).CreateSDKSession(ctx, "claude-1", "test-project", "")
	require.NoError(t, err)
	id, _, err := obsStore.StoreObservation(ctx, "claude-1", "test-project", &models.ParsedObservation{
		Type: models.ObsTypeDiscovery, Title: "Original title", Subtitle: "Original subtitle", Narrative: "Original narrative",
		Facts: []string{"fact one"}, Concepts: []string{"cache"}, FilesRead: []string{"a.go"},
	}, int(sessionID), 1)
	require.NoError(t, err)
	return obsStore, id, cleanup
}

func ptr[T any](v T) *T { return &v }

func TestUpdateObservation_ChangesOnlyTheGivenFields(t *testing.T) {
	obsStore, id, cleanup := storeForUpdate(t)
	defer cleanup()
	ctx := context.Background()

	got, err := obsStore.UpdateObservation(ctx, id, &ObservationUpdate{Title: ptr("New title"), Narrative: ptr("New narrative")})
	require.NoError(t, err)
	assert.Equal(t, "New title", got.Title.String)
	assert.Equal(t, "New narrative", got.Narrative.String)
	assert.Equal(t, "Original subtitle", got.Subtitle.String, "a field that was not given stays")
	assert.Equal(t, []string{"fact one"}, []string(got.Facts))

	stored, err := obsStore.GetObservationByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "New title", stored.Title.String, "and it is stored, not only returned")
}

func TestUpdateObservation_ReplacesListsAndScope(t *testing.T) {
	obsStore, id, cleanup := storeForUpdate(t)
	defer cleanup()
	ctx := context.Background()

	got, err := obsStore.UpdateObservation(ctx, id, &ObservationUpdate{
		Facts: &[]string{"a", "b"}, Concepts: &[]string{}, FilesRead: &[]string{"x.go"}, FilesModified: &[]string{"y.go"}, Scope: ptr("global"),
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, []string(got.Facts))
	assert.Empty(t, got.Concepts, "an empty list clears the field")
	assert.Equal(t, []string{"x.go"}, []string(got.FilesRead))
	assert.Equal(t, []string{"y.go"}, []string(got.FilesModified))
	assert.Equal(t, models.ScopeGlobal, got.Scope)
}

func TestUpdateObservation_NothingToChangeReturnsTheObservation(t *testing.T) {
	obsStore, id, cleanup := storeForUpdate(t)
	defer cleanup()

	got, err := obsStore.UpdateObservation(context.Background(), id, &ObservationUpdate{})
	require.NoError(t, err)
	assert.Equal(t, "Original title", got.Title.String)
}

func TestUpdateObservation_Refusals(t *testing.T) {
	obsStore, id, cleanup := storeForUpdate(t)
	defer cleanup()
	ctx := context.Background()

	_, err := obsStore.UpdateObservation(ctx, id, nil)
	assert.Error(t, err, "a nil update")
	_, err = obsStore.UpdateObservation(ctx, 999999, &ObservationUpdate{Title: ptr("x")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestUpdateObservation_KeepsTheSearchIndexInStep(t *testing.T) {
	obsStore, id, cleanup := storeForUpdate(t)
	defer cleanup()
	ctx := context.Background()

	_, err := obsStore.UpdateObservation(ctx, id, &ObservationUpdate{Title: ptr("Zebrafish enclosure")})
	require.NoError(t, err)
	var n int64
	require.NoError(t, obsStore.db.WithContext(ctx).Raw(
		`SELECT COUNT(*) FROM observations_fts WHERE observations_fts MATCH 'zebrafish'`).Scan(&n).Error)
	assert.EqualValues(t, 1, n, "the full-text index sees the new title")
}
