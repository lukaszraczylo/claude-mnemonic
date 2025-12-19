// Package scoring provides importance score calculation for observations.
package scoring

import (
	"testing"
	"time"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// CalculatorSuite is a test suite for the Calculator.
type CalculatorSuite struct {
	suite.Suite
	calc   *Calculator
	config *models.ScoringConfig
	now    time.Time
}

func (s *CalculatorSuite) SetupTest() {
	s.config = models.DefaultScoringConfig()
	s.calc = NewCalculator(s.config)
	s.now = time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
}

func TestCalculatorSuite(t *testing.T) {
	suite.Run(t, new(CalculatorSuite))
}

// =============================================================================
// GOOD SCENARIOS - Expected normal operations
// =============================================================================

func (s *CalculatorSuite) TestCalculate_GoodScenarios_NewObservation() {
	// A brand new observation should have score close to type weight
	obs := &models.Observation{
		ID:             1,
		Type:           models.ObsTypeBugfix,
		CreatedAtEpoch: s.now.UnixMilli(),
	}

	score := s.calc.Calculate(obs, s.now)

	// Expected: 1.0 × 1.3 (bugfix weight) × 1.0 (no decay) = 1.3
	s.InDelta(1.3, score, 0.01, "new bugfix should score ~1.3")
}

func (s *CalculatorSuite) TestCalculate_GoodScenarios_OneWeekOld() {
	// One week old observation should have half the recency score
	obs := &models.Observation{
		ID:             1,
		Type:           models.ObsTypeDiscovery,
		CreatedAtEpoch: s.now.Add(-7 * 24 * time.Hour).UnixMilli(),
	}

	score := s.calc.Calculate(obs, s.now)

	// Expected: 1.0 × 1.1 (discovery) × 0.5 (7 days half-life) = 0.55
	s.InDelta(0.55, score, 0.05, "7-day old discovery should score ~0.55")
}

func (s *CalculatorSuite) TestCalculate_GoodScenarios_TwoWeeksOld() {
	// Two weeks old should have 1/4 recency score
	obs := &models.Observation{
		ID:             1,
		Type:           models.ObsTypeFeature,
		CreatedAtEpoch: s.now.Add(-14 * 24 * time.Hour).UnixMilli(),
	}

	score := s.calc.Calculate(obs, s.now)

	// Expected: 1.0 × 1.2 (feature) × 0.25 (14 days = 2 half-lives) = 0.30
	s.InDelta(0.30, score, 0.05, "14-day old feature should score ~0.30")
}

func (s *CalculatorSuite) TestCalculate_GoodScenarios_PositiveFeedback() {
	// Positive feedback should boost score
	obs := &models.Observation{
		ID:             1,
		Type:           models.ObsTypeChange,
		CreatedAtEpoch: s.now.UnixMilli(),
		UserFeedback:   1, // thumbs up
	}

	score := s.calc.Calculate(obs, s.now)

	// Expected: (1.0 × 0.9) + 0.30 (feedback) = 1.20
	s.InDelta(1.20, score, 0.01, "thumbs up should boost score by 0.30")
}

func (s *CalculatorSuite) TestCalculate_GoodScenarios_NegativeFeedback() {
	// Negative feedback should reduce score
	obs := &models.Observation{
		ID:             1,
		Type:           models.ObsTypeChange,
		CreatedAtEpoch: s.now.UnixMilli(),
		UserFeedback:   -1, // thumbs down
	}

	score := s.calc.Calculate(obs, s.now)

	// Expected: (1.0 × 0.9) - 0.30 (feedback) = 0.60
	s.InDelta(0.60, score, 0.01, "thumbs down should reduce score by 0.30")
}

func (s *CalculatorSuite) TestCalculate_GoodScenarios_WithConcepts() {
	// Observation with valuable concepts should get boost
	obs := &models.Observation{
		ID:             1,
		Type:           models.ObsTypeBugfix,
		CreatedAtEpoch: s.now.UnixMilli(),
		Concepts:       []string{"security", "gotcha"},
	}

	score := s.calc.Calculate(obs, s.now)

	// Concept boost: (0.30 + 0.25) × 0.20 = 0.11
	// Expected: 1.3 + 0.11 = 1.41
	s.InDelta(1.41, score, 0.05, "security+gotcha concepts should boost score")
}

func (s *CalculatorSuite) TestCalculate_GoodScenarios_WithRetrievals() {
	// Popular observations should get retrieval boost
	obs := &models.Observation{
		ID:             1,
		Type:           models.ObsTypeDiscovery,
		CreatedAtEpoch: s.now.UnixMilli(),
		RetrievalCount: 7, // log2(8) = 3
	}

	score := s.calc.Calculate(obs, s.now)

	// Retrieval boost: log2(7+1) × 0.1 × 0.15 = 3 × 0.1 × 0.15 = 0.045
	// Expected: 1.1 + 0.045 ≈ 1.145
	s.InDelta(1.145, score, 0.05, "7 retrievals should add small boost")
}

func (s *CalculatorSuite) TestCalculate_GoodScenarios_CombinedFactors() {
	// Test with all factors combined
	obs := &models.Observation{
		ID:             1,
		Type:           models.ObsTypeBugfix,
		CreatedAtEpoch: s.now.Add(-7 * 24 * time.Hour).UnixMilli(), // 7 days old
		UserFeedback:   1,
		Concepts:       []string{"security"},
		RetrievalCount: 3,
	}

	score := s.calc.Calculate(obs, s.now)

	// Core: 1.0 × 1.3 × 0.5 = 0.65
	// Feedback: 0.30
	// Concept: 0.30 × 0.20 = 0.06
	// Retrieval: log2(4) × 0.1 × 0.15 = 2 × 0.1 × 0.15 = 0.03
	// Total ≈ 1.04
	s.InDelta(1.04, score, 0.1, "combined factors should result in ~1.04")
}

// =============================================================================
// WORSE SCENARIOS - Degraded but acceptable operations
// =============================================================================

func (s *CalculatorSuite) TestCalculate_WorseScenarios_VeryOldObservation() {
	// Very old observation should have low but non-zero score
	obs := &models.Observation{
		ID:             1,
		Type:           models.ObsTypeChange,
		CreatedAtEpoch: s.now.Add(-90 * 24 * time.Hour).UnixMilli(), // 90 days old
	}

	score := s.calc.Calculate(obs, s.now)

	// 90 days = ~12.86 half-lives → decay ≈ 0.00014
	// Core: 1.0 × 0.9 × 0.00014 = 0.000126
	// But minimum score is 0.01
	s.GreaterOrEqual(score, 0.01, "very old observation should still meet minimum")
	s.Less(score, 0.1, "very old observation should be low scoring")
}

func (s *CalculatorSuite) TestCalculate_WorseScenarios_NegativeFeedbackOld() {
	// Old observation with negative feedback should still have minimum score
	obs := &models.Observation{
		ID:             1,
		Type:           models.ObsTypeChange,
		CreatedAtEpoch: s.now.Add(-60 * 24 * time.Hour).UnixMilli(),
		UserFeedback:   -1,
	}

	score := s.calc.Calculate(obs, s.now)

	s.GreaterOrEqual(score, s.config.MinScore, "should never go below minimum score")
}

func (s *CalculatorSuite) TestCalculate_WorseScenarios_UnknownConcepts() {
	// Unknown concepts should not affect score negatively
	obs := &models.Observation{
		ID:             1,
		Type:           models.ObsTypeDiscovery,
		CreatedAtEpoch: s.now.UnixMilli(),
		Concepts:       []string{"unknown-concept", "another-unknown"},
	}

	score := s.calc.Calculate(obs, s.now)

	// Should just be the base score without concept boost
	s.InDelta(1.1, score, 0.01, "unknown concepts should not affect score")
}

func (s *CalculatorSuite) TestCalculate_WorseScenarios_MixedConcepts() {
	// Mix of known and unknown concepts
	obs := &models.Observation{
		ID:             1,
		Type:           models.ObsTypeDiscovery,
		CreatedAtEpoch: s.now.UnixMilli(),
		Concepts:       []string{"security", "unknown-concept"},
	}

	score := s.calc.Calculate(obs, s.now)

	// Only security should contribute
	// Expected: 1.1 + (0.30 × 0.20) = 1.16
	s.InDelta(1.16, score, 0.05, "only known concepts should boost score")
}

// =============================================================================
// BAD SCENARIOS - Edge cases and error conditions
// =============================================================================

func (s *CalculatorSuite) TestCalculate_BadScenarios_FutureTimestamp() {
	// Observation created in the future (clock skew)
	obs := &models.Observation{
		ID:             1,
		Type:           models.ObsTypeBugfix,
		CreatedAtEpoch: s.now.Add(24 * time.Hour).UnixMilli(), // 1 day in future
	}

	score := s.calc.Calculate(obs, s.now)

	// Should handle gracefully - age should be 0
	s.InDelta(1.3, score, 0.01, "future timestamp should be treated as now")
}

func (s *CalculatorSuite) TestCalculate_BadScenarios_ZeroEpoch() {
	// Missing creation timestamp
	obs := &models.Observation{
		ID:             1,
		Type:           models.ObsTypeDiscovery,
		CreatedAtEpoch: 0, // Missing timestamp
	}

	score := s.calc.Calculate(obs, s.now)

	// This will be treated as very old (1970)
	s.GreaterOrEqual(score, s.config.MinScore, "should still meet minimum")
}

func (s *CalculatorSuite) TestCalculate_BadScenarios_EmptyObservation() {
	// Minimal observation with defaults
	obs := &models.Observation{
		ID:             1,
		Type:           "", // Empty type
		CreatedAtEpoch: s.now.UnixMilli(),
	}

	score := s.calc.Calculate(obs, s.now)

	// Unknown type should default to 1.0 weight
	s.InDelta(1.0, score, 0.01, "empty type should use default weight 1.0")
}

func (s *CalculatorSuite) TestCalculate_BadScenarios_ExtremeRetrievalCount() {
	// Very high retrieval count
	obs := &models.Observation{
		ID:             1,
		Type:           models.ObsTypeDiscovery,
		CreatedAtEpoch: s.now.UnixMilli(),
		RetrievalCount: 1000000, // Extreme value
	}

	score := s.calc.Calculate(obs, s.now)

	// log2(1000001) ≈ 19.93, so boost = 19.93 × 0.1 × 0.15 ≈ 0.30
	// Score should be reasonable, not exploding
	s.Less(score, 2.0, "extreme retrieval count should not explode score")
}

func (s *CalculatorSuite) TestCalculate_BadScenarios_NegativeRetrievalCount() {
	// Negative retrieval count (should not happen but test defensively)
	obs := &models.Observation{
		ID:             1,
		Type:           models.ObsTypeDiscovery,
		CreatedAtEpoch: s.now.UnixMilli(),
		RetrievalCount: -5,
	}

	score := s.calc.Calculate(obs, s.now)

	// Should not panic and should give base score
	s.InDelta(1.1, score, 0.01, "negative retrieval should be ignored")
}

// =============================================================================
// EDGE CASES - Boundary conditions
// =============================================================================

func (s *CalculatorSuite) TestCalculate_EdgeCases_ExactlyOneHalfLife() {
	obs := &models.Observation{
		ID:             1,
		Type:           models.ObsTypeChange, // 0.9 weight
		CreatedAtEpoch: s.now.Add(-7 * 24 * time.Hour).UnixMilli(),
	}

	score := s.calc.Calculate(obs, s.now)
	s.InDelta(0.45, score, 0.01, "exactly 7 days should give 0.5 decay")
}

func (s *CalculatorSuite) TestCalculate_EdgeCases_ExactlyTwoHalfLives() {
	obs := &models.Observation{
		ID:             1,
		Type:           models.ObsTypeChange,
		CreatedAtEpoch: s.now.Add(-14 * 24 * time.Hour).UnixMilli(),
	}

	score := s.calc.Calculate(obs, s.now)
	s.InDelta(0.225, score, 0.01, "exactly 14 days should give 0.25 decay")
}

func (s *CalculatorSuite) TestCalculate_EdgeCases_AllTypeWeights() {
	types := []struct {
		t      models.ObservationType
		weight float64
	}{
		{models.ObsTypeBugfix, 1.3},
		{models.ObsTypeFeature, 1.2},
		{models.ObsTypeDiscovery, 1.1},
		{models.ObsTypeDecision, 1.1},
		{models.ObsTypeRefactor, 1.0},
		{models.ObsTypeChange, 0.9},
	}

	for _, tt := range types {
		s.Run(string(tt.t), func() {
			obs := &models.Observation{
				ID:             1,
				Type:           tt.t,
				CreatedAtEpoch: s.now.UnixMilli(),
			}
			score := s.calc.Calculate(obs, s.now)
			s.InDelta(tt.weight, score, 0.01)
		})
	}
}

func (s *CalculatorSuite) TestCalculate_EdgeCases_MinimumScoreEnforced() {
	// Create worst case scenario
	obs := &models.Observation{
		ID:             1,
		Type:           models.ObsTypeChange,                                    // Lowest weight 0.9
		CreatedAtEpoch: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli(), // Very old
		UserFeedback:   -1,                                                      // Negative feedback
	}

	score := s.calc.Calculate(obs, s.now)

	s.Equal(s.config.MinScore, score, "should be exactly minimum score")
}

func (s *CalculatorSuite) TestCalculate_EdgeCases_AllConceptsMaxWeight() {
	// Observation with all high-value concepts
	obs := &models.Observation{
		ID:             1,
		Type:           models.ObsTypeBugfix,
		CreatedAtEpoch: s.now.UnixMilli(),
		Concepts:       []string{"security", "gotcha", "best-practice", "anti-pattern"},
	}

	score := s.calc.Calculate(obs, s.now)

	// security=0.30, gotcha=0.25, best-practice=0.20, anti-pattern=0.20 = 0.95
	// Concept contrib: 0.95 × 0.20 = 0.19
	// Total: 1.3 + 0.19 = 1.49
	s.InDelta(1.49, score, 0.05, "all high-value concepts should boost significantly")
}

// =============================================================================
// CALCULATE COMPONENTS TESTS
// =============================================================================

func (s *CalculatorSuite) TestCalculateComponents_ReturnsAllComponents() {
	obs := &models.Observation{
		ID:             1,
		Type:           models.ObsTypeBugfix,
		CreatedAtEpoch: s.now.Add(-7 * 24 * time.Hour).UnixMilli(),
		UserFeedback:   1,
		Concepts:       []string{"security"},
		RetrievalCount: 7,
	}

	components := s.calc.CalculateComponents(obs, s.now)

	s.InDelta(1.3, components.TypeWeight, 0.01)
	s.InDelta(0.5, components.RecencyDecay, 0.01)
	s.InDelta(0.65, components.CoreScore, 0.05)
	s.InDelta(0.30, components.FeedbackContrib, 0.01)
	s.InDelta(0.06, components.ConceptContrib, 0.02)
	s.Greater(components.RetrievalContrib, 0.0)
	s.InDelta(7.0, components.AgeDays, 0.1)
	s.Greater(components.FinalScore, 0.0)
}

func (s *CalculatorSuite) TestCalculateComponents_MatchesCalculate() {
	obs := &models.Observation{
		ID:             1,
		Type:           models.ObsTypeFeature,
		CreatedAtEpoch: s.now.Add(-3 * 24 * time.Hour).UnixMilli(),
		UserFeedback:   -1,
		Concepts:       []string{"performance", "architecture"},
		RetrievalCount: 15,
	}

	score := s.calc.Calculate(obs, s.now)
	components := s.calc.CalculateComponents(obs, s.now)

	s.InDelta(score, components.FinalScore, 0.001, "Calculate and CalculateComponents should match")
}

// =============================================================================
// BATCH CALCULATE TESTS
// =============================================================================

func (s *CalculatorSuite) TestBatchCalculate_Empty() {
	scores := s.calc.BatchCalculate(nil, s.now)
	s.Empty(scores)

	scores = s.calc.BatchCalculate([]*models.Observation{}, s.now)
	s.Empty(scores)
}

func (s *CalculatorSuite) TestBatchCalculate_Multiple() {
	obs := []*models.Observation{
		{ID: 1, Type: models.ObsTypeBugfix, CreatedAtEpoch: s.now.UnixMilli()},
		{ID: 2, Type: models.ObsTypeFeature, CreatedAtEpoch: s.now.Add(-7 * 24 * time.Hour).UnixMilli()},
		{ID: 3, Type: models.ObsTypeChange, CreatedAtEpoch: s.now.Add(-14 * 24 * time.Hour).UnixMilli()},
	}

	scores := s.calc.BatchCalculate(obs, s.now)

	s.Len(scores, 3)
	s.Contains(scores, int64(1))
	s.Contains(scores, int64(2))
	s.Contains(scores, int64(3))

	s.InDelta(1.3, scores[1], 0.01)   // New bugfix
	s.InDelta(0.6, scores[2], 0.1)    // 7-day feature
	s.InDelta(0.225, scores[3], 0.05) // 14-day change
}

// =============================================================================
// CONFIGURATION TESTS
// =============================================================================

func (s *CalculatorSuite) TestNewCalculator_NilConfig() {
	calc := NewCalculator(nil)
	s.NotNil(calc)
	s.NotNil(calc.config)
	s.Equal(7.0, calc.config.RecencyHalfLifeDays)
}

func (s *CalculatorSuite) TestUpdateConfig() {
	newConfig := &models.ScoringConfig{
		RecencyHalfLifeDays: 14.0, // Changed from 7
		FeedbackWeight:      0.50,
		ConceptWeight:       0.10,
		RetrievalWeight:     0.05,
		MinScore:            0.001,
		ConceptWeights:      map[string]float64{"test": 0.5},
	}

	s.calc.UpdateConfig(newConfig)

	obs := &models.Observation{
		ID:             1,
		Type:           models.ObsTypeChange,
		CreatedAtEpoch: s.now.Add(-14 * 24 * time.Hour).UnixMilli(),
	}

	score := s.calc.Calculate(obs, s.now)

	// With 14-day half-life, 14 days = exactly one half-life
	// Expected: 1.0 × 0.9 × 0.5 = 0.45
	s.InDelta(0.45, score, 0.01)
}

func (s *CalculatorSuite) TestUpdateConfig_NilIgnored() {
	originalConfig := s.calc.GetConfig()
	s.calc.UpdateConfig(nil)
	s.Equal(originalConfig, s.calc.GetConfig())
}

func (s *CalculatorSuite) TestGetConfig() {
	config := s.calc.GetConfig()
	s.NotNil(config)
	s.Equal(7.0, config.RecencyHalfLifeDays)
}

func (s *CalculatorSuite) TestRecalculateThreshold() {
	threshold := s.calc.RecalculateThreshold()
	s.Equal(6*time.Hour, threshold)
}

// =============================================================================
// STANDALONE TESTS (non-suite)
// =============================================================================

func TestNewCalculator_DefaultConfig(t *testing.T) {
	calc := NewCalculator(nil)
	require.NotNil(t, calc)
	assert.Equal(t, 7.0, calc.config.RecencyHalfLifeDays)
	assert.Equal(t, 0.30, calc.config.FeedbackWeight)
	assert.Equal(t, 0.01, calc.config.MinScore)
}

func TestCalculator_ConcurrentAccess(t *testing.T) {
	calc := NewCalculator(nil)
	now := time.Now()

	// Test that calculator is safe for concurrent reads
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int64) {
			obs := &models.Observation{
				ID:             id,
				Type:           models.ObsTypeBugfix,
				CreatedAtEpoch: now.UnixMilli(),
			}
			score := calc.Calculate(obs, now)
			assert.Greater(t, score, 0.0)
			done <- true
		}(int64(i))
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestCalculator_DecayPrecision(t *testing.T) {
	calc := NewCalculator(nil)
	now := time.Now()

	// Test that decay is mathematically correct
	testCases := []struct {
		days          int
		expectedDecay float64
	}{
		{0, 1.0},
		{7, 0.5},
		{14, 0.25},
		{21, 0.125},
		{28, 0.0625},
	}

	for _, tc := range testCases {
		t.Run(string(rune('0'+tc.days/7))+"_half_lives", func(t *testing.T) {
			obs := &models.Observation{
				ID:             1,
				Type:           models.ObsTypeRefactor, // 1.0 weight
				CreatedAtEpoch: now.Add(-time.Duration(tc.days) * 24 * time.Hour).UnixMilli(),
			}
			components := calc.CalculateComponents(obs, now)
			assert.InDelta(t, tc.expectedDecay, components.RecencyDecay, 0.001)
		})
	}
}

func TestTypeBaseScore_UnknownType(t *testing.T) {
	score := models.TypeBaseScore("unknown-type")
	assert.Equal(t, 1.0, score, "unknown type should default to 1.0")
}

func TestTypeBaseScore_AllKnownTypes(t *testing.T) {
	expected := map[models.ObservationType]float64{
		models.ObsTypeBugfix:    1.3,
		models.ObsTypeFeature:   1.2,
		models.ObsTypeDiscovery: 1.1,
		models.ObsTypeDecision:  1.1,
		models.ObsTypeRefactor:  1.0,
		models.ObsTypeChange:    0.9,
	}

	for obsType, expectedScore := range expected {
		t.Run(string(obsType), func(t *testing.T) {
			score := models.TypeBaseScore(obsType)
			assert.Equal(t, expectedScore, score)
		})
	}
}

func TestCalculator_RetrievalBoostDiminishingReturns(t *testing.T) {
	calc := NewCalculator(nil)
	now := time.Now()

	// Test that retrieval boost has diminishing returns
	// When retrieval count doubles, the boost should NOT double (log2 gives diminishing returns)

	// Collect boosts for different counts
	boosts := make([]float64, 0)
	retrievalCounts := []int{1, 3, 7, 15, 31, 63, 127}

	for _, count := range retrievalCounts {
		obs := &models.Observation{
			ID:             1,
			Type:           models.ObsTypeRefactor,
			CreatedAtEpoch: now.UnixMilli(),
			RetrievalCount: count,
		}
		components := calc.CalculateComponents(obs, now)
		boosts = append(boosts, components.RetrievalContrib)
	}

	// Verify boost increases but at a decreasing rate
	for i := 1; i < len(boosts); i++ {
		// Each boost should be higher than the previous
		assert.Greater(t, boosts[i], boosts[i-1],
			"boost should increase with more retrievals")

		// But not proportionally - calculate the ratios
		if i >= 2 {
			ratio1 := boosts[i-1] / boosts[i-2]
			ratio2 := boosts[i] / boosts[i-1]
			// The growth ratio should be decreasing (diminishing returns)
			assert.Less(t, ratio2, ratio1+0.01, // Allow small floating point tolerance
				"growth rate should decrease (diminishing returns)")
		}
	}
}
