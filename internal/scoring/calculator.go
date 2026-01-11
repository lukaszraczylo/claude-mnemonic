// Package scoring provides importance score calculation for observations.
package scoring

import (
	"math"
	"time"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
)

// Calculator computes importance scores for observations.
type Calculator struct {
	config *models.ScoringConfig
}

// NewCalculator creates a new scoring calculator.
// If config is nil, uses the default configuration.
func NewCalculator(config *models.ScoringConfig) *Calculator {
	if config == nil {
		config = models.DefaultScoringConfig()
	}
	return &Calculator{config: config}
}

// Calculate computes the importance score for an observation at the given time.
//
// The scoring formula:
//
//	FinalScore = (BaseScore × TypeWeight × RecencyDecay) + FeedbackContrib + ConceptContrib + RetrievalContrib
//
// Where:
//   - BaseScore = 1.0
//   - TypeWeight = observation type multiplier (e.g., bugfix=1.3, change=0.9)
//   - RecencyDecay = 0.5^(age_days / half_life_days) - halves every 7 days by default
//   - FeedbackContrib = user_feedback × feedback_weight
//   - ConceptContrib = sum(concept_weights) × concept_weight_factor
//   - RetrievalContrib = log2(retrieval_count + 1) × 0.1 × retrieval_weight
func (c *Calculator) Calculate(obs *models.Observation, now time.Time) float64 {
	return c.CalculateComponents(obs, now).FinalScore
}

// CalculateComponents returns the individual components of the importance score.
// Useful for debugging and explaining scores to users.
// This is the core calculation method - Calculate() delegates to this.
func (c *Calculator) CalculateComponents(obs *models.Observation, now time.Time) ScoreComponents {
	// 1. Get base type weight
	typeWeight := models.TypeBaseScore(obs.Type)

	// 2. Calculate recency decay: 0.5^(age_days / half_life_days)
	ageDays := now.Sub(time.UnixMilli(obs.CreatedAtEpoch)).Hours() / 24.0
	if ageDays < 0 {
		ageDays = 0 // Handle future timestamps gracefully
	}
	recencyDecay := math.Pow(0.5, ageDays/c.config.RecencyHalfLifeDays)

	// Core score = 1.0 × type_weight × recency_decay
	coreScore := 1.0 * typeWeight * recencyDecay

	// 3. User feedback contribution: feedback × weight
	feedbackContrib := float64(obs.UserFeedback) * c.config.FeedbackWeight

	// 4. Concept boost contribution: sum of matching concept weights × factor
	conceptBoost := 0.0
	for _, concept := range obs.Concepts {
		if weight, ok := c.config.ConceptWeights[concept]; ok {
			conceptBoost += weight
		}
	}
	conceptContrib := conceptBoost * c.config.ConceptWeight

	// 5. Retrieval boost: log2(count + 1) × 0.1 × weight (diminishing returns)
	retrievalContrib := 0.0
	if obs.RetrievalCount > 0 {
		// log2(count + 1) gives diminishing returns: 1→1, 3→2, 7→3, 15→4, etc.
		retrievalBoost := math.Log2(float64(obs.RetrievalCount)+1) * 0.1
		retrievalContrib = retrievalBoost * c.config.RetrievalWeight
	}

	// Final score with minimum threshold
	finalScore := coreScore + feedbackContrib + conceptContrib + retrievalContrib
	if finalScore < c.config.MinScore {
		finalScore = c.config.MinScore
	}

	return ScoreComponents{
		TypeWeight:       typeWeight,
		RecencyDecay:     recencyDecay,
		CoreScore:        coreScore,
		FeedbackContrib:  feedbackContrib,
		ConceptContrib:   conceptContrib,
		RetrievalContrib: retrievalContrib,
		FinalScore:       finalScore,
		AgeDays:          ageDays,
	}
}

// ScoreComponents contains the breakdown of an importance score calculation.
type ScoreComponents struct {
	TypeWeight       float64 `json:"type_weight"`
	RecencyDecay     float64 `json:"recency_decay"`
	CoreScore        float64 `json:"core_score"`
	FeedbackContrib  float64 `json:"feedback_contrib"`
	ConceptContrib   float64 `json:"concept_contrib"`
	RetrievalContrib float64 `json:"retrieval_contrib"`
	FinalScore       float64 `json:"final_score"`
	AgeDays          float64 `json:"age_days"`
}

// BatchCalculate computes scores for multiple observations.
// Returns a map of observation ID to calculated score.
func (c *Calculator) BatchCalculate(observations []*models.Observation, now time.Time) map[int64]float64 {
	scores := make(map[int64]float64, len(observations))
	for _, obs := range observations {
		scores[obs.ID] = c.Calculate(obs, now)
	}
	return scores
}

// RecalculateThreshold returns the minimum duration before an observation
// should have its score recalculated. This prevents excessive recalculation
// while ensuring scores stay reasonably fresh.
func (c *Calculator) RecalculateThreshold() time.Duration {
	// Recalculate at most every 6 hours
	// This balances freshness with performance
	return 6 * time.Hour
}

// UpdateConfig updates the calculator's scoring configuration.
// This allows runtime tuning of scoring parameters.
func (c *Calculator) UpdateConfig(config *models.ScoringConfig) {
	if config != nil {
		c.config = config
	}
}

// GetConfig returns the current scoring configuration.
func (c *Calculator) GetConfig() *models.ScoringConfig {
	return c.config
}
