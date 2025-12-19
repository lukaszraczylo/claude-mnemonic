// Package models contains domain models for claude-mnemonic.
package models

// ConceptWeight represents a configurable weight for a concept.
type ConceptWeight struct {
	Concept   string  `db:"concept" json:"concept"`
	Weight    float64 `db:"weight" json:"weight"`
	UpdatedAt string  `db:"updated_at" json:"updated_at"`
}

// UserFeedbackType represents the type of user feedback.
type UserFeedbackType int

const (
	// FeedbackNegative represents a thumbs down.
	FeedbackNegative UserFeedbackType = -1
	// FeedbackNeutral represents no feedback.
	FeedbackNeutral UserFeedbackType = 0
	// FeedbackPositive represents a thumbs up.
	FeedbackPositive UserFeedbackType = 1
)

// DefaultConceptWeights contains the default weights for concepts.
// Higher weights indicate more important concepts.
var DefaultConceptWeights = map[string]float64{
	// Critical concepts (0.25-0.30)
	"security": 0.30, // Security issues are most critical

	// High importance (0.20-0.25)
	"gotcha":        0.25, // Gotchas prevent future mistakes
	"best-practice": 0.20, // Best practices guide development
	"anti-pattern":  0.20, // Anti-patterns prevent bad code

	// Medium importance (0.10-0.15)
	"architecture":     0.15, // Architectural decisions have lasting impact
	"performance":      0.15, // Performance patterns matter for scale
	"error-handling":   0.15, // Error handling prevents failures
	"pattern":          0.10, // General patterns are useful
	"testing":          0.10, // Testing knowledge helps quality
	"debugging":        0.10, // Debugging tips save time
	"problem-solution": 0.10, // Problem-solution pairs are actionable
	"trade-off":        0.10, // Trade-offs inform decisions

	// Lower importance (0.05)
	"workflow":      0.05, // Workflow optimizations are nice-to-have
	"tooling":       0.05, // Tooling preferences are subjective
	"how-it-works":  0.05, // Understanding is foundational but less urgent
	"why-it-exists": 0.05, // Context is helpful but less actionable
	"what-changed":  0.05, // Changes are informational
}

// TypeBaseScores contains the base importance multipliers for each observation type.
// These are multiplied with the core score to weight different observation types.
var TypeBaseScores = map[ObservationType]float64{
	ObsTypeBugfix:    1.3, // Bugfixes are valuable - prevent regressions
	ObsTypeFeature:   1.2, // New features expand capabilities
	ObsTypeDiscovery: 1.1, // Discoveries inform future work
	ObsTypeDecision:  1.1, // Architectural decisions guide development
	ObsTypeRefactor:  1.0, // Refactoring is neutral
	ObsTypeChange:    0.9, // Minor changes are slightly less important
}

// ScoringConfig contains all scoring weights and parameters.
type ScoringConfig struct {
	// RecencyHalfLifeDays is the number of days for the importance score to halve.
	// With 7 days, a 7-day old observation has 50% of a new observation's recency score.
	RecencyHalfLifeDays float64 `json:"recency_half_life_days"`

	// FeedbackWeight scales the user feedback contribution to final score.
	// With 0.30, a thumbs up adds 0.30 to the score, thumbs down subtracts 0.30.
	FeedbackWeight float64 `json:"feedback_weight"`

	// ConceptWeight scales the concept boost contribution.
	// The sum of matching concept weights is multiplied by this.
	ConceptWeight float64 `json:"concept_weight"`

	// RetrievalWeight scales the retrieval boost contribution.
	// Popular observations get a logarithmic bonus.
	RetrievalWeight float64 `json:"retrieval_weight"`

	// ConceptWeights maps concept names to their importance weights.
	ConceptWeights map[string]float64 `json:"concept_weights"`

	// MinScore is the minimum allowed importance score.
	// Prevents observations from completely disappearing.
	MinScore float64 `json:"min_score"`
}

// DefaultScoringConfig returns the default scoring configuration.
func DefaultScoringConfig() *ScoringConfig {
	conceptWeights := make(map[string]float64, len(DefaultConceptWeights))
	for k, v := range DefaultConceptWeights {
		conceptWeights[k] = v
	}

	return &ScoringConfig{
		RecencyHalfLifeDays: 7.0,  // Score halves every 7 days
		FeedbackWeight:      0.30, // Feedback has moderate impact
		ConceptWeight:       0.20, // Concept weights have smaller impact
		RetrievalWeight:     0.15, // Retrieval has smallest impact
		ConceptWeights:      conceptWeights,
		MinScore:            0.01, // Never completely disappear
	}
}

// TypeBaseScore returns the base weight for an observation type.
func TypeBaseScore(t ObservationType) float64 {
	if score, ok := TypeBaseScores[t]; ok {
		return score
	}
	return 1.0 // Default for unknown types
}
