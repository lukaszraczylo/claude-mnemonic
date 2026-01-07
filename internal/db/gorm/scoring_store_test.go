//go:build fts5

// Package gorm provides GORM-based database operations for claude-mnemonic.
package gorm

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
)

func TestObservationStore_UpdateImportanceScore(t *testing.T) {
	obsStore, store, cleanup := testObservationStore(t)
	defer cleanup()
	ctx := context.Background()

	// Create observation
	sessionStore := NewSessionStore(store)
	sessionID, _ := sessionStore.CreateSDKSession(ctx, "claude-1", "test-project", "")
	obs := &models.ParsedObservation{Type: models.ObsTypeDiscovery, Title: "Test"}
	obsID, _, _ := obsStore.StoreObservation(ctx, "claude-1", "test-project", obs, int(sessionID), 1)

	// Update score
	err := obsStore.UpdateImportanceScore(ctx, obsID, 5.0)
	require.NoError(t, err)

	// Verify
	var dbObs Observation
	store.DB.First(&dbObs, obsID)
	assert.Equal(t, 5.0, dbObs.ImportanceScore)
}

func TestObservationStore_IncrementRetrievalCount(t *testing.T) {
	obsStore, store, cleanup := testObservationStore(t)
	defer cleanup()
	ctx := context.Background()

	sessionStore := NewSessionStore(store)
	sessionID, _ := sessionStore.CreateSDKSession(ctx, "claude-1", "test-project", "")
	obs := &models.ParsedObservation{Type: models.ObsTypeDiscovery, Title: "Test"}
	obsID, _, _ := obsStore.StoreObservation(ctx, "claude-1", "test-project", obs, int(sessionID), 1)

	err := obsStore.IncrementRetrievalCount(ctx, []int64{obsID})
	require.NoError(t, err)

	var dbObs Observation
	store.DB.First(&dbObs, obsID)
	assert.Equal(t, 1, dbObs.RetrievalCount)
}

func TestObservationStore_IncrementRetrievalCount_Multiple(t *testing.T) {
	obsStore, store, cleanup := testObservationStore(t)
	defer cleanup()
	ctx := context.Background()

	sessionStore := NewSessionStore(store)
	sessionID, _ := sessionStore.CreateSDKSession(ctx, "claude-1", "test-project", "")

	// Create 3 observations
	ids := make([]int64, 3)
	for i := 0; i < 3; i++ {
		obs := &models.ParsedObservation{Type: models.ObsTypeDiscovery, Title: "Test"}
		obsID, _, _ := obsStore.StoreObservation(ctx, "claude-1", "test-project", obs, int(sessionID), 1)
		ids[i] = obsID
	}

	// Increment all
	err := obsStore.IncrementRetrievalCount(ctx, ids)
	require.NoError(t, err)

	// Verify all were incremented
	for _, id := range ids {
		var dbObs Observation
		store.DB.First(&dbObs, id)
		assert.Equal(t, 1, dbObs.RetrievalCount)
	}

	// Increment again
	err = obsStore.IncrementRetrievalCount(ctx, ids)
	require.NoError(t, err)

	// Verify all are now 2
	for _, id := range ids {
		var dbObs Observation
		store.DB.First(&dbObs, id)
		assert.Equal(t, 2, dbObs.RetrievalCount)
	}
}

func TestObservationStore_UpdateImportanceScores_Bulk(t *testing.T) {
	obsStore, store, cleanup := testObservationStore(t)
	defer cleanup()
	ctx := context.Background()

	sessionStore := NewSessionStore(store)
	sessionID, _ := sessionStore.CreateSDKSession(ctx, "claude-1", "test-project", "")

	// Create 3 observations
	ids := make([]int64, 3)
	for i := 0; i < 3; i++ {
		obs := &models.ParsedObservation{Type: models.ObsTypeDiscovery, Title: "Test"}
		obsID, _, _ := obsStore.StoreObservation(ctx, "claude-1", "test-project", obs, int(sessionID), 1)
		ids[i] = obsID
	}

	// Bulk update scores
	scores := map[int64]float64{
		ids[0]: 2.5,
		ids[1]: 3.7,
		ids[2]: 1.2,
	}

	err := obsStore.UpdateImportanceScores(ctx, scores)
	require.NoError(t, err)

	// Verify scores
	for id, expectedScore := range scores {
		var dbObs Observation
		store.DB.First(&dbObs, id)
		assert.Equal(t, expectedScore, dbObs.ImportanceScore)
	}
}

func TestObservationStore_UpdateObservationFeedback(t *testing.T) {
	obsStore, store, cleanup := testObservationStore(t)
	defer cleanup()
	ctx := context.Background()

	sessionStore := NewSessionStore(store)
	sessionID, _ := sessionStore.CreateSDKSession(ctx, "claude-1", "test-project", "")
	obs := &models.ParsedObservation{Type: models.ObsTypeDiscovery, Title: "Test"}
	obsID, _, _ := obsStore.StoreObservation(ctx, "claude-1", "test-project", obs, int(sessionID), 1)

	// Set thumbs up
	err := obsStore.UpdateObservationFeedback(ctx, obsID, 1)
	require.NoError(t, err)

	var dbObs Observation
	store.DB.First(&dbObs, obsID)
	assert.Equal(t, 1, dbObs.UserFeedback)

	// Set thumbs down
	err = obsStore.UpdateObservationFeedback(ctx, obsID, -1)
	require.NoError(t, err)

	store.DB.First(&dbObs, obsID)
	assert.Equal(t, -1, dbObs.UserFeedback)
}

func TestObservationStore_GetObservationFeedbackStats(t *testing.T) {
	obsStore, store, cleanup := testObservationStore(t)
	defer cleanup()
	ctx := context.Background()

	sessionStore := NewSessionStore(store)
	sessionID, _ := sessionStore.CreateSDKSession(ctx, "claude-1", "test-project", "")

	// Create observations with different feedback
	obs1 := &models.ParsedObservation{Type: models.ObsTypeDiscovery, Title: "Test1"}
	obsID1, _, _ := obsStore.StoreObservation(ctx, "claude-1", "test-project", obs1, int(sessionID), 1)
	obsStore.UpdateObservationFeedback(ctx, obsID1, 1) // thumbs up
	obsStore.UpdateImportanceScore(ctx, obsID1, 3.0)

	obs2 := &models.ParsedObservation{Type: models.ObsTypeBugfix, Title: "Test2"}
	obsID2, _, _ := obsStore.StoreObservation(ctx, "claude-1", "test-project", obs2, int(sessionID), 2)
	obsStore.UpdateObservationFeedback(ctx, obsID2, -1) // thumbs down
	obsStore.UpdateImportanceScore(ctx, obsID2, 2.0)

	obs3 := &models.ParsedObservation{Type: models.ObsTypeFeature, Title: "Test3"}
	obsID3, _, _ := obsStore.StoreObservation(ctx, "claude-1", "test-project", obs3, int(sessionID), 3)
	// neutral (0)
	obsStore.UpdateImportanceScore(ctx, obsID3, 1.5)
	obsStore.IncrementRetrievalCount(ctx, []int64{obsID1, obsID2, obsID3})

	// Get stats
	stats, err := obsStore.GetObservationFeedbackStats(ctx, "test-project")
	require.NoError(t, err)

	assert.Equal(t, 3, stats.Total)
	assert.Equal(t, 1, stats.Positive)
	assert.Equal(t, 1, stats.Negative)
	assert.Equal(t, 1, stats.Neutral)
	assert.InDelta(t, 2.166, stats.AvgScore, 0.01) // (3.0 + 2.0 + 1.5) / 3
	assert.InDelta(t, 1.0, stats.AvgRetrieval, 0.01)
}

func TestObservationStore_GetTopScoringObservations(t *testing.T) {
	obsStore, store, cleanup := testObservationStore(t)
	defer cleanup()
	ctx := context.Background()

	sessionStore := NewSessionStore(store)
	sessionID, _ := sessionStore.CreateSDKSession(ctx, "claude-1", "test-project", "")

	// Create observations with different scores
	obs1 := &models.ParsedObservation{Type: models.ObsTypeDiscovery, Title: "High"}
	obsID1, _, _ := obsStore.StoreObservation(ctx, "claude-1", "test-project", obs1, int(sessionID), 1)
	obsStore.UpdateImportanceScore(ctx, obsID1, 5.0)

	obs2 := &models.ParsedObservation{Type: models.ObsTypeBugfix, Title: "Low"}
	obsID2, _, _ := obsStore.StoreObservation(ctx, "claude-1", "test-project", obs2, int(sessionID), 2)
	obsStore.UpdateImportanceScore(ctx, obsID2, 1.0)

	obs3 := &models.ParsedObservation{Type: models.ObsTypeFeature, Title: "Medium"}
	obsID3, _, _ := obsStore.StoreObservation(ctx, "claude-1", "test-project", obs3, int(sessionID), 3)
	obsStore.UpdateImportanceScore(ctx, obsID3, 3.0)

	// Get top 2
	topObs, err := obsStore.GetTopScoringObservations(ctx, "test-project", 2)
	require.NoError(t, err)

	require.Len(t, topObs, 2)
	assert.True(t, topObs[0].Title.Valid)
	assert.Equal(t, "High", topObs[0].Title.String)
	assert.Equal(t, 5.0, topObs[0].ImportanceScore)
	assert.True(t, topObs[1].Title.Valid)
	assert.Equal(t, "Medium", topObs[1].Title.String)
	assert.Equal(t, 3.0, topObs[1].ImportanceScore)
}

func TestObservationStore_GetMostRetrievedObservations(t *testing.T) {
	obsStore, store, cleanup := testObservationStore(t)
	defer cleanup()
	ctx := context.Background()

	sessionStore := NewSessionStore(store)
	sessionID, _ := sessionStore.CreateSDKSession(ctx, "claude-1", "test-project", "")

	// Create observations
	obs1 := &models.ParsedObservation{Type: models.ObsTypeDiscovery, Title: "Popular"}
	obsID1, _, _ := obsStore.StoreObservation(ctx, "claude-1", "test-project", obs1, int(sessionID), 1)

	obs2 := &models.ParsedObservation{Type: models.ObsTypeBugfix, Title: "Unpopular"}
	obsID2, _, _ := obsStore.StoreObservation(ctx, "claude-1", "test-project", obs2, int(sessionID), 2)

	// Increment retrieval counts - each call increments by 1
	obsStore.IncrementRetrievalCount(ctx, []int64{obsID1}) // increment by 1
	obsStore.IncrementRetrievalCount(ctx, []int64{obsID1}) // increment by 1 again (total: 2)
	obsStore.IncrementRetrievalCount(ctx, []int64{obsID1}) // increment by 1 again (total: 3)
	obsStore.IncrementRetrievalCount(ctx, []int64{obsID2}) // increment by 1 (total: 1)

	// Get most retrieved - should return obsID1 (Popular) with count 3
	topObs, err := obsStore.GetMostRetrievedObservations(ctx, "test-project", 2)
	require.NoError(t, err)

	require.Len(t, topObs, 2)
	assert.True(t, topObs[0].Title.Valid)
	assert.Equal(t, "Popular", topObs[0].Title.String)
	assert.Equal(t, 3, topObs[0].RetrievalCount)
	assert.True(t, topObs[1].Title.Valid)
	assert.Equal(t, "Unpopular", topObs[1].Title.String)
	assert.Equal(t, 1, topObs[1].RetrievalCount)
}

func TestObservationStore_SetConceptWeights(t *testing.T) {
	obsStore, _, cleanup := testObservationStore(t)
	defer cleanup()
	ctx := context.Background()

	// Set weights
	weights := map[string]float64{
		"security":      2.0,
		"performance":   1.5,
		"best-practice": 1.8,
	}

	err := obsStore.SetConceptWeights(ctx, weights)
	require.NoError(t, err)

	// Get weights back
	retrieved, err := obsStore.GetConceptWeights(ctx)
	require.NoError(t, err)

	assert.Equal(t, 2.0, retrieved["security"])
	assert.Equal(t, 1.5, retrieved["performance"])
	assert.Equal(t, 1.8, retrieved["best-practice"])

	// Update weights (UPSERT)
	weights["security"] = 2.5
	weights["scalability"] = 1.2

	err = obsStore.SetConceptWeights(ctx, weights)
	require.NoError(t, err)

	retrieved, err = obsStore.GetConceptWeights(ctx)
	require.NoError(t, err)

	assert.Equal(t, 2.5, retrieved["security"])      // updated
	assert.Equal(t, 1.5, retrieved["performance"])   // unchanged
	assert.Equal(t, 1.2, retrieved["scalability"])   // new
	assert.Equal(t, 1.8, retrieved["best-practice"]) // unchanged
}

func TestObservationStore_GetObservationsNeedingScoreUpdate(t *testing.T) {
	obsStore, store, cleanup := testObservationStore(t)
	defer cleanup()
	ctx := context.Background()

	sessionStore := NewSessionStore(store)
	sessionID, _ := sessionStore.CreateSDKSession(ctx, "claude-1", "test-project", "")

	// Create observation with no score update
	obs1 := &models.ParsedObservation{Type: models.ObsTypeDiscovery, Title: "Needs Update"}
	obsID1, _, _ := obsStore.StoreObservation(ctx, "claude-1", "test-project", obs1, int(sessionID), 1)

	// Create observation with recent score update
	obs2 := &models.ParsedObservation{Type: models.ObsTypeBugfix, Title: "Recently Updated"}
	obsID2, _, _ := obsStore.StoreObservation(ctx, "claude-1", "test-project", obs2, int(sessionID), 2)
	obsStore.UpdateImportanceScore(ctx, obsID2, 2.0)

	// Get observations needing update (within 1 hour threshold)
	needsUpdate, err := obsStore.GetObservationsNeedingScoreUpdate(ctx, 1*time.Hour, 10)
	require.NoError(t, err)

	// Only obs1 should need update (obs2 was just updated)
	assert.Len(t, needsUpdate, 1)
	assert.Equal(t, obsID1, needsUpdate[0].ID)
	assert.True(t, needsUpdate[0].Title.Valid)
	assert.Equal(t, "Needs Update", needsUpdate[0].Title.String)
}

func TestObservationStore_ResetObservationScores(t *testing.T) {
	obsStore, store, cleanup := testObservationStore(t)
	defer cleanup()
	ctx := context.Background()

	sessionStore := NewSessionStore(store)
	sessionID, _ := sessionStore.CreateSDKSession(ctx, "claude-1", "test-project", "")

	// Create observations with custom scores
	obs1 := &models.ParsedObservation{Type: models.ObsTypeDiscovery, Title: "Test1"}
	obsID1, _, _ := obsStore.StoreObservation(ctx, "claude-1", "test-project", obs1, int(sessionID), 1)
	obsStore.UpdateImportanceScore(ctx, obsID1, 5.0)

	obs2 := &models.ParsedObservation{Type: models.ObsTypeBugfix, Title: "Test2"}
	obsID2, _, _ := obsStore.StoreObservation(ctx, "claude-1", "test-project", obs2, int(sessionID), 2)
	obsStore.UpdateImportanceScore(ctx, obsID2, 3.0)

	// Reset all scores
	err := obsStore.ResetObservationScores(ctx)
	require.NoError(t, err)

	// Verify all scores are 1.0
	var dbObs1, dbObs2 Observation
	store.DB.First(&dbObs1, obsID1)
	store.DB.First(&dbObs2, obsID2)

	assert.Equal(t, 1.0, dbObs1.ImportanceScore)
	assert.Equal(t, 1.0, dbObs2.ImportanceScore)
}
