// Package sqlite provides SQLite database operations for claude-mnemonic.
package sqlite

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// testScoringObservationStore creates an ObservationStore with scoring columns for testing.
func testScoringObservationStore(t *testing.T) (*ObservationStore, *Store, func()) {
	t.Helper()

	db, _, cleanup := testDB(t)
	createBaseTables(t, db)
	createConceptWeightsTable(t, db)

	// Add importance index if not exists (columns already in createBaseTables)
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_observations_importance ON observations(importance_score DESC, created_at_epoch DESC)`); err != nil {
		t.Fatalf("create importance index: %v", err)
	}

	store := newStoreFromDB(db)
	obsStore := NewObservationStore(store)

	return obsStore, store, cleanup
}

// createConceptWeightsTable creates the concept_weights table for testing.
func createConceptWeightsTable(t *testing.T, db *sql.DB) {
	t.Helper()

	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS concept_weights (
			concept TEXT PRIMARY KEY,
			weight REAL NOT NULL DEFAULT 0.1,
			updated_at TEXT NOT NULL
		)
	`)
	if err != nil {
		t.Fatalf("create concept_weights: %v", err)
	}
}

// ScoringStoreSuite is a test suite for scoring-related database operations.
type ScoringStoreSuite struct {
	suite.Suite
	obsStore *ObservationStore
	store    *Store
	cleanup  func()
	ctx      context.Context
}

func (s *ScoringStoreSuite) SetupTest() {
	s.obsStore, s.store, s.cleanup = testScoringObservationStore(s.T())
	s.ctx = context.Background()
}

func (s *ScoringStoreSuite) TearDownTest() {
	if s.cleanup != nil {
		s.cleanup()
	}
}

func TestScoringStoreSuite(t *testing.T) {
	suite.Run(t, new(ScoringStoreSuite))
}

// =============================================================================
// FEEDBACK TESTS
// =============================================================================

func (s *ScoringStoreSuite) TestUpdateObservationFeedback_Positive() {
	// Create observation
	obs := &models.ParsedObservation{
		Type:  models.ObsTypeBugfix,
		Title: "Test feedback",
	}
	id, _, err := s.obsStore.StoreObservation(s.ctx, "session-1", "project-a", obs, 1, 100)
	s.NoError(err)

	// Update feedback to positive
	err = s.obsStore.UpdateObservationFeedback(s.ctx, id, 1)
	s.NoError(err)

	// Verify
	retrieved, err := s.obsStore.GetObservationByID(s.ctx, id)
	s.NoError(err)
	s.Equal(1, retrieved.UserFeedback)
	s.True(retrieved.ScoreUpdatedAt.Valid)
}

func (s *ScoringStoreSuite) TestUpdateObservationFeedback_Negative() {
	obs := &models.ParsedObservation{
		Type: models.ObsTypeDiscovery,
	}
	id, _, err := s.obsStore.StoreObservation(s.ctx, "session-1", "project-a", obs, 1, 100)
	s.NoError(err)

	err = s.obsStore.UpdateObservationFeedback(s.ctx, id, -1)
	s.NoError(err)

	retrieved, err := s.obsStore.GetObservationByID(s.ctx, id)
	s.NoError(err)
	s.Equal(-1, retrieved.UserFeedback)
}

func (s *ScoringStoreSuite) TestUpdateObservationFeedback_Neutral() {
	obs := &models.ParsedObservation{
		Type: models.ObsTypeChange,
	}
	id, _, err := s.obsStore.StoreObservation(s.ctx, "session-1", "project-a", obs, 1, 100)
	s.NoError(err)

	// First set to positive
	err = s.obsStore.UpdateObservationFeedback(s.ctx, id, 1)
	s.NoError(err)

	// Then reset to neutral
	err = s.obsStore.UpdateObservationFeedback(s.ctx, id, 0)
	s.NoError(err)

	retrieved, err := s.obsStore.GetObservationByID(s.ctx, id)
	s.NoError(err)
	s.Equal(0, retrieved.UserFeedback)
}

func (s *ScoringStoreSuite) TestUpdateObservationFeedback_NonExistent() {
	// Updating non-existent observation should not fail (just no rows affected)
	err := s.obsStore.UpdateObservationFeedback(s.ctx, 99999, 1)
	s.NoError(err)
}

// =============================================================================
// RETRIEVAL COUNT TESTS
// =============================================================================

func (s *ScoringStoreSuite) TestIncrementRetrievalCount_Single() {
	obs := &models.ParsedObservation{
		Type: models.ObsTypeBugfix,
	}
	id, _, err := s.obsStore.StoreObservation(s.ctx, "session-1", "project-a", obs, 1, 100)
	s.NoError(err)

	err = s.obsStore.IncrementRetrievalCount(s.ctx, []int64{id})
	s.NoError(err)

	retrieved, err := s.obsStore.GetObservationByID(s.ctx, id)
	s.NoError(err)
	s.Equal(1, retrieved.RetrievalCount)
	s.True(retrieved.LastRetrievedAt.Valid)
}

func (s *ScoringStoreSuite) TestIncrementRetrievalCount_Multiple() {
	var ids []int64
	for i := 0; i < 3; i++ {
		obs := &models.ParsedObservation{
			Type: models.ObsTypeDiscovery,
		}
		id, _, err := s.obsStore.StoreObservation(s.ctx, "session-1", "project-a", obs, i, 100)
		s.NoError(err)
		ids = append(ids, id)
	}

	err := s.obsStore.IncrementRetrievalCount(s.ctx, ids)
	s.NoError(err)

	for _, id := range ids {
		retrieved, err := s.obsStore.GetObservationByID(s.ctx, id)
		s.NoError(err)
		s.Equal(1, retrieved.RetrievalCount)
	}
}

func (s *ScoringStoreSuite) TestIncrementRetrievalCount_Cumulative() {
	obs := &models.ParsedObservation{
		Type: models.ObsTypeBugfix,
	}
	id, _, err := s.obsStore.StoreObservation(s.ctx, "session-1", "project-a", obs, 1, 100)
	s.NoError(err)

	// Increment multiple times
	for i := 0; i < 5; i++ {
		err = s.obsStore.IncrementRetrievalCount(s.ctx, []int64{id})
		s.NoError(err)
	}

	retrieved, err := s.obsStore.GetObservationByID(s.ctx, id)
	s.NoError(err)
	s.Equal(5, retrieved.RetrievalCount)
}

func (s *ScoringStoreSuite) TestIncrementRetrievalCount_Empty() {
	err := s.obsStore.IncrementRetrievalCount(s.ctx, []int64{})
	s.NoError(err)
}

// =============================================================================
// IMPORTANCE SCORE TESTS
// =============================================================================

func (s *ScoringStoreSuite) TestUpdateImportanceScore_Single() {
	obs := &models.ParsedObservation{
		Type: models.ObsTypeBugfix,
	}
	id, _, err := s.obsStore.StoreObservation(s.ctx, "session-1", "project-a", obs, 1, 100)
	s.NoError(err)

	err = s.obsStore.UpdateImportanceScore(s.ctx, id, 1.5)
	s.NoError(err)

	retrieved, err := s.obsStore.GetObservationByID(s.ctx, id)
	s.NoError(err)
	s.InDelta(1.5, retrieved.ImportanceScore, 0.001)
}

func (s *ScoringStoreSuite) TestUpdateImportanceScores_Batch() {
	var ids []int64
	for i := 0; i < 5; i++ {
		obs := &models.ParsedObservation{
			Type: models.ObsTypeDiscovery,
		}
		id, _, err := s.obsStore.StoreObservation(s.ctx, "session-1", "project-a", obs, i, 100)
		s.NoError(err)
		ids = append(ids, id)
	}

	scores := map[int64]float64{
		ids[0]: 1.5,
		ids[1]: 0.8,
		ids[2]: 1.2,
		ids[3]: 0.5,
		ids[4]: 2.0,
	}

	err := s.obsStore.UpdateImportanceScores(s.ctx, scores)
	s.NoError(err)

	for id, expectedScore := range scores {
		retrieved, err := s.obsStore.GetObservationByID(s.ctx, id)
		s.NoError(err)
		s.InDelta(expectedScore, retrieved.ImportanceScore, 0.001)
	}
}

func (s *ScoringStoreSuite) TestUpdateImportanceScores_Empty() {
	err := s.obsStore.UpdateImportanceScores(s.ctx, map[int64]float64{})
	s.NoError(err)
}

// =============================================================================
// OBSERVATIONS NEEDING SCORE UPDATE TESTS
// =============================================================================

func (s *ScoringStoreSuite) TestGetObservationsNeedingScoreUpdate_NeverUpdated() {
	// Observations without score_updated_at_epoch should need update
	obs := &models.ParsedObservation{
		Type: models.ObsTypeBugfix,
	}
	id, _, err := s.obsStore.StoreObservation(s.ctx, "session-1", "project-a", obs, 1, 100)
	s.NoError(err)

	observations, err := s.obsStore.GetObservationsNeedingScoreUpdate(s.ctx, 6*time.Hour, 100)
	s.NoError(err)
	s.Len(observations, 1)
	s.Equal(id, observations[0].ID)
}

func (s *ScoringStoreSuite) TestGetObservationsNeedingScoreUpdate_RecentlyUpdated() {
	obs := &models.ParsedObservation{
		Type: models.ObsTypeBugfix,
	}
	id, _, err := s.obsStore.StoreObservation(s.ctx, "session-1", "project-a", obs, 1, 100)
	s.NoError(err)

	// Update score (this sets score_updated_at_epoch)
	err = s.obsStore.UpdateImportanceScore(s.ctx, id, 1.5)
	s.NoError(err)

	// Should not need update (just updated)
	observations, err := s.obsStore.GetObservationsNeedingScoreUpdate(s.ctx, 6*time.Hour, 100)
	s.NoError(err)
	s.Empty(observations)
}

func (s *ScoringStoreSuite) TestGetObservationsNeedingScoreUpdate_Limit() {
	// Create 10 observations
	for i := 0; i < 10; i++ {
		obs := &models.ParsedObservation{
			Type: models.ObsTypeDiscovery,
		}
		_, _, err := s.obsStore.StoreObservation(s.ctx, "session-1", "project-a", obs, i, 100)
		s.NoError(err)
	}

	// Request only 5
	observations, err := s.obsStore.GetObservationsNeedingScoreUpdate(s.ctx, 6*time.Hour, 5)
	s.NoError(err)
	s.Len(observations, 5)
}

// =============================================================================
// CONCEPT WEIGHTS TESTS
// =============================================================================

func (s *ScoringStoreSuite) TestGetConceptWeights_Empty() {
	weights, err := s.obsStore.GetConceptWeights(s.ctx)
	s.NoError(err)
	s.Equal(models.DefaultConceptWeights, weights)
}

func (s *ScoringStoreSuite) TestUpdateConceptWeight_NewConcept() {
	err := s.obsStore.UpdateConceptWeight(s.ctx, "new-concept", 0.42)
	s.NoError(err)

	weights, err := s.obsStore.GetConceptWeights(s.ctx)
	s.NoError(err)
	s.Equal(0.42, weights["new-concept"])
}

func (s *ScoringStoreSuite) TestUpdateConceptWeight_UpdateExisting() {
	// Insert first
	err := s.obsStore.UpdateConceptWeight(s.ctx, "test-concept", 0.1)
	s.NoError(err)

	// Update
	err = s.obsStore.UpdateConceptWeight(s.ctx, "test-concept", 0.9)
	s.NoError(err)

	weights, err := s.obsStore.GetConceptWeights(s.ctx)
	s.NoError(err)
	s.Equal(0.9, weights["test-concept"])
}

func (s *ScoringStoreSuite) TestUpdateConceptWeights_Batch() {
	weightsToSet := map[string]float64{
		"security":    0.5,
		"performance": 0.3,
		"testing":     0.2,
	}

	err := s.obsStore.UpdateConceptWeights(s.ctx, weightsToSet)
	s.NoError(err)

	retrieved, err := s.obsStore.GetConceptWeights(s.ctx)
	s.NoError(err)

	for concept, expected := range weightsToSet {
		s.Equal(expected, retrieved[concept])
	}
}

func (s *ScoringStoreSuite) TestUpdateConceptWeights_Empty() {
	err := s.obsStore.UpdateConceptWeights(s.ctx, map[string]float64{})
	s.NoError(err)
}

// =============================================================================
// FEEDBACK STATS TESTS
// =============================================================================

func (s *ScoringStoreSuite) TestGetObservationFeedbackStats_Empty() {
	stats, err := s.obsStore.GetObservationFeedbackStats(s.ctx, "")
	s.NoError(err)
	s.Equal(0, stats.Total)
	s.Equal(0, stats.Positive)
	s.Equal(0, stats.Negative)
	s.Equal(0, stats.Neutral)
}

func (s *ScoringStoreSuite) TestGetObservationFeedbackStats_WithData() {
	// Create observations with different feedback
	feedbacks := []int{1, 1, 1, -1, -1, 0, 0, 0, 0, 0}
	for i, fb := range feedbacks {
		obs := &models.ParsedObservation{
			Type: models.ObsTypeDiscovery,
		}
		id, _, err := s.obsStore.StoreObservation(s.ctx, "session-1", "project-a", obs, i, 100)
		s.NoError(err)
		if fb != 0 {
			err = s.obsStore.UpdateObservationFeedback(s.ctx, id, fb)
			s.NoError(err)
		}
	}

	stats, err := s.obsStore.GetObservationFeedbackStats(s.ctx, "")
	s.NoError(err)
	s.Equal(10, stats.Total)
	s.Equal(3, stats.Positive)
	s.Equal(2, stats.Negative)
	s.Equal(5, stats.Neutral)
}

func (s *ScoringStoreSuite) TestGetObservationFeedbackStats_ByProject() {
	// Project A observations
	for i := 0; i < 5; i++ {
		obs := &models.ParsedObservation{
			Type: models.ObsTypeBugfix,
		}
		id, _, err := s.obsStore.StoreObservation(s.ctx, "session-1", "project-a", obs, i, 100)
		s.NoError(err)
		_ = s.obsStore.UpdateObservationFeedback(s.ctx, id, 1)
	}

	// Project B observations
	for i := 0; i < 3; i++ {
		obs := &models.ParsedObservation{
			Type: models.ObsTypeFeature,
		}
		id, _, err := s.obsStore.StoreObservation(s.ctx, "session-1", "project-b", obs, i, 100)
		s.NoError(err)
		_ = s.obsStore.UpdateObservationFeedback(s.ctx, id, -1)
	}

	// Check project A stats
	statsA, err := s.obsStore.GetObservationFeedbackStats(s.ctx, "project-a")
	s.NoError(err)
	s.Equal(5, statsA.Total)
	s.Equal(5, statsA.Positive)

	// Check project B stats
	statsB, err := s.obsStore.GetObservationFeedbackStats(s.ctx, "project-b")
	s.NoError(err)
	s.Equal(3, statsB.Total)
	s.Equal(3, statsB.Negative)
}

// =============================================================================
// TOP SCORING OBSERVATIONS TESTS
// =============================================================================

func (s *ScoringStoreSuite) TestGetTopScoringObservations() {
	// Create observations with different scores
	for i := 0; i < 5; i++ {
		obs := &models.ParsedObservation{
			Type: models.ObsTypeDiscovery,
		}
		id, _, err := s.obsStore.StoreObservation(s.ctx, "session-1", "project-a", obs, i, 100)
		s.NoError(err)
		// Set different scores
		err = s.obsStore.UpdateImportanceScore(s.ctx, id, float64(i+1)*0.5)
		s.NoError(err)
	}

	// Get top 3
	top, err := s.obsStore.GetTopScoringObservations(s.ctx, "", 3)
	s.NoError(err)
	s.Len(top, 3)

	// Verify ordered by score descending
	s.GreaterOrEqual(top[0].ImportanceScore, top[1].ImportanceScore)
	s.GreaterOrEqual(top[1].ImportanceScore, top[2].ImportanceScore)
}

func (s *ScoringStoreSuite) TestGetTopScoringObservations_ByProject() {
	// Project A with high scores
	for i := 0; i < 3; i++ {
		obs := &models.ParsedObservation{
			Type: models.ObsTypeBugfix,
		}
		id, _, err := s.obsStore.StoreObservation(s.ctx, "session-1", "project-a", obs, i, 100)
		s.NoError(err)
		_ = s.obsStore.UpdateImportanceScore(s.ctx, id, 2.0)
	}

	// Project B with low scores
	for i := 0; i < 3; i++ {
		obs := &models.ParsedObservation{
			Type: models.ObsTypeChange,
		}
		id, _, err := s.obsStore.StoreObservation(s.ctx, "session-1", "project-b", obs, i, 100)
		s.NoError(err)
		_ = s.obsStore.UpdateImportanceScore(s.ctx, id, 0.5)
	}

	// Get top for project A
	topA, err := s.obsStore.GetTopScoringObservations(s.ctx, "project-a", 10)
	s.NoError(err)
	s.Len(topA, 3)
	for _, obs := range topA {
		s.Equal("project-a", obs.Project)
	}
}

// =============================================================================
// MOST RETRIEVED OBSERVATIONS TESTS
// =============================================================================

func (s *ScoringStoreSuite) TestGetMostRetrievedObservations() {
	// Create observations with different retrieval counts
	var ids []int64
	for i := 0; i < 5; i++ {
		obs := &models.ParsedObservation{
			Type: models.ObsTypeDiscovery,
		}
		id, _, err := s.obsStore.StoreObservation(s.ctx, "session-1", "project-a", obs, i, 100)
		s.NoError(err)
		ids = append(ids, id)
	}

	// Set different retrieval counts
	for i := 0; i < 10; i++ {
		_ = s.obsStore.IncrementRetrievalCount(s.ctx, []int64{ids[0]}) // 10 retrievals
	}
	for i := 0; i < 5; i++ {
		_ = s.obsStore.IncrementRetrievalCount(s.ctx, []int64{ids[1]}) // 5 retrievals
	}
	for i := 0; i < 3; i++ {
		_ = s.obsStore.IncrementRetrievalCount(s.ctx, []int64{ids[2]}) // 3 retrievals
	}
	// ids[3] and ids[4] have 0 retrievals

	// Get top 3
	most, err := s.obsStore.GetMostRetrievedObservations(s.ctx, "", 3)
	s.NoError(err)
	s.Len(most, 3)

	// Verify ordered by retrieval count descending
	s.Equal(10, most[0].RetrievalCount)
	s.Equal(5, most[1].RetrievalCount)
	s.Equal(3, most[2].RetrievalCount)
}

func (s *ScoringStoreSuite) TestGetMostRetrievedObservations_NoRetrievals() {
	// Create observations without any retrievals
	obs := &models.ParsedObservation{
		Type: models.ObsTypeDiscovery,
	}
	_, _, err := s.obsStore.StoreObservation(s.ctx, "session-1", "project-a", obs, 1, 100)
	s.NoError(err)

	most, err := s.obsStore.GetMostRetrievedObservations(s.ctx, "", 10)
	s.NoError(err)
	s.Empty(most) // No observations with retrieval_count > 0
}

// =============================================================================
// RESET OBSERVATION SCORES TESTS
// =============================================================================

func (s *ScoringStoreSuite) TestResetObservationScores() {
	// Create observations with various scores
	for i := 0; i < 5; i++ {
		obs := &models.ParsedObservation{
			Type: models.ObsTypeDiscovery,
		}
		id, _, err := s.obsStore.StoreObservation(s.ctx, "session-1", "project-a", obs, i, 100)
		s.NoError(err)
		_ = s.obsStore.UpdateImportanceScore(s.ctx, id, float64(i+1))
	}

	// Reset all scores
	err := s.obsStore.ResetObservationScores(s.ctx)
	s.NoError(err)

	// Verify all scores are reset to 1.0
	observations, err := s.obsStore.GetAllRecentObservations(s.ctx, 100)
	s.NoError(err)
	for _, obs := range observations {
		s.InDelta(1.0, obs.ImportanceScore, 0.001)
		s.False(obs.ScoreUpdatedAt.Valid)
	}
}

// =============================================================================
// EDGE CASES
// =============================================================================

func (s *ScoringStoreSuite) TestScoring_ZeroScore() {
	obs := &models.ParsedObservation{
		Type: models.ObsTypeBugfix,
	}
	id, _, err := s.obsStore.StoreObservation(s.ctx, "session-1", "project-a", obs, 1, 100)
	s.NoError(err)

	// Set score to 0
	err = s.obsStore.UpdateImportanceScore(s.ctx, id, 0.0)
	s.NoError(err)

	retrieved, err := s.obsStore.GetObservationByID(s.ctx, id)
	s.NoError(err)
	s.InDelta(0.0, retrieved.ImportanceScore, 0.001)
}

func (s *ScoringStoreSuite) TestScoring_NegativeScore() {
	obs := &models.ParsedObservation{
		Type: models.ObsTypeBugfix,
	}
	id, _, err := s.obsStore.StoreObservation(s.ctx, "session-1", "project-a", obs, 1, 100)
	s.NoError(err)

	// Set negative score (calculator shouldn't produce this, but test DB handling)
	err = s.obsStore.UpdateImportanceScore(s.ctx, id, -0.5)
	s.NoError(err)

	retrieved, err := s.obsStore.GetObservationByID(s.ctx, id)
	s.NoError(err)
	s.InDelta(-0.5, retrieved.ImportanceScore, 0.001)
}

func (s *ScoringStoreSuite) TestScoring_LargeScore() {
	obs := &models.ParsedObservation{
		Type: models.ObsTypeBugfix,
	}
	id, _, err := s.obsStore.StoreObservation(s.ctx, "session-1", "project-a", obs, 1, 100)
	s.NoError(err)

	// Set very large score
	err = s.obsStore.UpdateImportanceScore(s.ctx, id, 999.999)
	s.NoError(err)

	retrieved, err := s.obsStore.GetObservationByID(s.ctx, id)
	s.NoError(err)
	s.InDelta(999.999, retrieved.ImportanceScore, 0.001)
}

func (s *ScoringStoreSuite) TestConceptWeight_ZeroWeight() {
	err := s.obsStore.UpdateConceptWeight(s.ctx, "zero-concept", 0.0)
	s.NoError(err)

	weights, err := s.obsStore.GetConceptWeights(s.ctx)
	s.NoError(err)
	s.Equal(0.0, weights["zero-concept"])
}

func (s *ScoringStoreSuite) TestConceptWeight_ExactBoundary() {
	err := s.obsStore.UpdateConceptWeight(s.ctx, "max-concept", 1.0)
	s.NoError(err)

	weights, err := s.obsStore.GetConceptWeights(s.ctx)
	s.NoError(err)
	s.Equal(1.0, weights["max-concept"])
}

// =============================================================================
// STANDALONE TESTS
// =============================================================================

func TestFeedbackStats_Structure(t *testing.T) {
	stats := FeedbackStats{
		Total:        100,
		Positive:     30,
		Negative:     10,
		Neutral:      60,
		AvgScore:     1.5,
		AvgRetrieval: 5.0,
	}

	assert.Equal(t, 100, stats.Total)
	assert.Equal(t, 30, stats.Positive)
	assert.Equal(t, 10, stats.Negative)
	assert.Equal(t, 60, stats.Neutral)
	assert.Equal(t, 1.5, stats.AvgScore)
	assert.Equal(t, 5.0, stats.AvgRetrieval)
}

func TestScoringStore_Integration(t *testing.T) {
	obsStore, _, cleanup := testScoringObservationStore(t)
	defer cleanup()

	ctx := context.Background()

	// Full integration test: store, feedback, retrieval, score update
	obs := &models.ParsedObservation{
		Type:     models.ObsTypeBugfix,
		Title:    "Integration test observation",
		Concepts: []string{"security"},
	}
	id, _, err := obsStore.StoreObservation(ctx, "session-int", "project-int", obs, 1, 100)
	require.NoError(t, err)

	// Add feedback
	err = obsStore.UpdateObservationFeedback(ctx, id, 1)
	require.NoError(t, err)

	// Increment retrieval
	err = obsStore.IncrementRetrievalCount(ctx, []int64{id})
	require.NoError(t, err)

	// Update score
	err = obsStore.UpdateImportanceScore(ctx, id, 1.75)
	require.NoError(t, err)

	// Verify final state
	retrieved, err := obsStore.GetObservationByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, 1, retrieved.UserFeedback)
	assert.Equal(t, 1, retrieved.RetrievalCount)
	assert.InDelta(t, 1.75, retrieved.ImportanceScore, 0.001)
	assert.True(t, retrieved.ScoreUpdatedAt.Valid)
	assert.True(t, retrieved.LastRetrievedAt.Valid)
}
