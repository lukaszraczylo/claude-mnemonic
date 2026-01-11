// Package pattern provides pattern detection and recognition functionality.
package pattern

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lukaszraczylo/claude-mnemonic/internal/db/gorm"
	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
	"github.com/rs/zerolog/log"
)

// DetectorConfig contains configuration for the pattern detector.
type DetectorConfig struct {
	// MinMatchScore is the minimum similarity score to consider a match (0.0-1.0).
	MinMatchScore float64
	// MinFrequencyForPattern is the minimum occurrences before creating a pattern.
	MinFrequencyForPattern int
	// AnalysisInterval is how often to run background pattern analysis.
	AnalysisInterval time.Duration
	// MaxPatternsToTrack is the maximum number of active patterns.
	MaxPatternsToTrack int
	// MaxCandidates is the maximum number of candidates to track (LRU eviction).
	MaxCandidates int
}

// DefaultConfig returns the default detector configuration.
func DefaultConfig() DetectorConfig {
	return DetectorConfig{
		MinMatchScore:          0.3, // 30% similarity threshold
		MinFrequencyForPattern: 2,   // At least 2 occurrences to form a pattern
		AnalysisInterval:       5 * time.Minute,
		MaxPatternsToTrack:     1000,
		MaxCandidates:          500, // Prevent unbounded growth
	}
}

// PatternSyncFunc is a callback for syncing patterns to vector store.
type PatternSyncFunc func(pattern *models.Pattern)

// Detector detects and tracks recurring patterns across observations.
type Detector struct {
	ctx              context.Context
	patternStore     *gorm.PatternStore
	observationStore *gorm.ObservationStore
	syncFunc         PatternSyncFunc
	candidates       map[string]*candidatePattern
	cancel           context.CancelFunc
	config           DetectorConfig
	wg               sync.WaitGroup
	candidatesMu     sync.RWMutex
}

// SetSyncFunc sets the callback for syncing patterns to vector store.
func (d *Detector) SetSyncFunc(fn PatternSyncFunc) {
	d.syncFunc = fn
}

// candidatePattern tracks a potential pattern before it reaches frequency threshold.
type candidatePattern struct {
	patternType    models.PatternType
	title          string
	signature      []string
	observationIDs []int64
	projects       []string
	lastSeenEpoch  int64
}

// NewDetector creates a new pattern detector.
func NewDetector(patternStore *gorm.PatternStore, observationStore *gorm.ObservationStore, config DetectorConfig) *Detector {
	ctx, cancel := context.WithCancel(context.Background())
	return &Detector{
		config:           config,
		patternStore:     patternStore,
		observationStore: observationStore,
		candidates:       make(map[string]*candidatePattern),
		ctx:              ctx,
		cancel:           cancel,
	}
}

// Start begins background pattern analysis.
func (d *Detector) Start() {
	d.wg.Add(1)
	go d.backgroundAnalysis()
	log.Info().Dur("interval", d.config.AnalysisInterval).Msg("Pattern detector started")
}

// Stop stops background pattern analysis.
func (d *Detector) Stop() {
	d.cancel()
	d.wg.Wait()
	log.Info().Msg("Pattern detector stopped")
}

// backgroundAnalysis runs periodic pattern analysis.
func (d *Detector) backgroundAnalysis() {
	defer d.wg.Done()

	ticker := time.NewTicker(d.config.AnalysisInterval)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			if err := d.AnalyzeRecentObservations(d.ctx); err != nil {
				log.Warn().Err(err).Msg("Background pattern analysis failed")
			}
		}
	}
}

// AnalyzeObservation processes a new observation for pattern detection.
// This is called synchronously when a new observation is stored.
func (d *Detector) AnalyzeObservation(ctx context.Context, obs *models.Observation) (*DetectionResult, error) {
	result := &DetectionResult{}

	// Extract signature from observation
	signature := models.ExtractSignature(
		obs.Concepts,
		obs.Title.String,
		obs.Narrative.String,
	)
	if len(signature) == 0 {
		return result, nil // Nothing to detect
	}

	// Check against existing patterns
	matches, err := d.patternStore.FindMatchingPatterns(ctx, signature, d.config.MinMatchScore)
	if err != nil {
		return nil, err
	}

	if len(matches) > 0 {
		// Found existing pattern match
		bestMatch := matches[0]
		for _, m := range matches[1:] {
			if models.CalculateMatchScore(signature, m.Signature) > models.CalculateMatchScore(signature, bestMatch.Signature) {
				bestMatch = m
			}
		}

		// Update the pattern with new occurrence
		if err := d.patternStore.IncrementPatternFrequency(ctx, bestMatch.ID, obs.Project, obs.ID); err != nil {
			log.Warn().Err(err).Int64("pattern_id", bestMatch.ID).Msg("Failed to update pattern frequency")
		}

		result.MatchedPattern = bestMatch
		result.MatchScore = models.CalculateMatchScore(signature, bestMatch.Signature)
		result.IsNewPattern = false

		log.Debug().
			Int64("pattern_id", bestMatch.ID).
			Str("pattern_name", bestMatch.Name).
			Float64("score", result.MatchScore).
			Msg("Observation matched existing pattern")

		return result, nil
	}

	// No existing pattern match - check candidates
	candidateKey := generateCandidateKey(signature)
	d.candidatesMu.Lock()
	defer d.candidatesMu.Unlock()

	if candidate, exists := d.candidates[candidateKey]; exists {
		// Update existing candidate
		candidate.observationIDs = append(candidate.observationIDs, obs.ID)

		// Add project if not already present
		found := false
		for _, p := range candidate.projects {
			if p == obs.Project {
				found = true
				break
			}
		}
		if !found {
			candidate.projects = append(candidate.projects, obs.Project)
		}
		candidate.lastSeenEpoch = time.Now().UnixMilli()

		// Check if candidate should be promoted to pattern
		if len(candidate.observationIDs) >= d.config.MinFrequencyForPattern {
			pattern, err := d.promoteCandidate(ctx, candidateKey, candidate)
			if err != nil {
				log.Warn().Err(err).Msg("Failed to promote candidate to pattern")
			} else {
				result.MatchedPattern = pattern
				result.IsNewPattern = true
				log.Info().
					Str("pattern_name", pattern.Name).
					Int("frequency", pattern.Frequency).
					Msg("New pattern detected and stored")
			}
		}
	} else {
		// Create new candidate - with immediate size check to prevent unbounded growth
		// between periodic cleanups (which run every 5 minutes)
		if d.config.MaxCandidates > 0 && len(d.candidates) >= d.config.MaxCandidates {
			// Evict oldest candidate immediately rather than waiting for periodic cleanup
			var oldestKey string
			var oldestTime int64 = time.Now().UnixMilli()
			for k, c := range d.candidates {
				if c.lastSeenEpoch < oldestTime {
					oldestTime = c.lastSeenEpoch
					oldestKey = k
				}
			}
			if oldestKey != "" {
				delete(d.candidates, oldestKey)
				log.Debug().Str("evicted_key", oldestKey).Msg("Evicted oldest candidate to make room")
			}
		}

		patternType := models.DetectPatternType(obs.Concepts, obs.Title.String, obs.Narrative.String)
		d.candidates[candidateKey] = &candidatePattern{
			signature:      signature,
			observationIDs: []int64{obs.ID},
			projects:       []string{obs.Project},
			patternType:    patternType,
			title:          obs.Title.String,
			lastSeenEpoch:  time.Now().UnixMilli(),
		}
		log.Debug().
			Str("candidate_key", candidateKey).
			Strs("signature", signature).
			Msg("New pattern candidate created")
	}

	return result, nil
}

// promoteCandidate converts a candidate to a stored pattern.
func (d *Detector) promoteCandidate(ctx context.Context, key string, candidate *candidatePattern) (*models.Pattern, error) {
	// Generate pattern name from signature
	name := generatePatternName(candidate.patternType, candidate.signature, candidate.title)

	// Create base pattern using NewPattern with first observation
	firstProject := ""
	if len(candidate.projects) > 0 {
		firstProject = candidate.projects[0]
	}
	var firstObsID int64
	if len(candidate.observationIDs) > 0 {
		firstObsID = candidate.observationIDs[0]
	}
	pattern := models.NewPattern(
		name,
		candidate.patternType,
		"Automatically detected pattern from recurring observations",
		candidate.signature,
		firstProject,
		firstObsID,
	)

	// Add remaining projects and observations
	for i := 1; i < len(candidate.projects); i++ {
		pattern.Projects = append(pattern.Projects, candidate.projects[i])
	}
	for i := 1; i < len(candidate.observationIDs); i++ {
		pattern.ObservationIDs = append(pattern.ObservationIDs, candidate.observationIDs[i])
	}
	pattern.Frequency = len(candidate.observationIDs)

	id, err := d.patternStore.StorePattern(ctx, pattern)
	if err != nil {
		return nil, err
	}
	pattern.ID = id

	// Sync to vector store if callback is set
	if d.syncFunc != nil {
		d.syncFunc(pattern)
	}

	// Remove from candidates
	delete(d.candidates, key)

	return pattern, nil
}

// AnalyzeRecentObservations analyzes recent observations for pattern detection.
// This is used for background batch analysis.
func (d *Detector) AnalyzeRecentObservations(ctx context.Context) error {
	// Get observations from the last 24 hours that haven't been analyzed
	observations, err := d.observationStore.GetRecentObservations(ctx, "", 100)
	if err != nil {
		return err
	}

	analyzed := 0
	patternsFound := 0
	for _, obs := range observations {
		result, err := d.AnalyzeObservation(ctx, obs)
		if err != nil {
			log.Warn().Err(err).Int64("obs_id", obs.ID).Msg("Failed to analyze observation")
			continue
		}
		analyzed++
		if result.MatchedPattern != nil {
			patternsFound++
		}
	}

	if analyzed > 0 {
		log.Info().
			Int("analyzed", analyzed).
			Int("patterns_found", patternsFound).
			Msg("Background pattern analysis completed")
	}

	// Clean up old candidates (older than 7 days)
	d.cleanupOldCandidates()

	return nil
}

// cleanupOldCandidates removes candidates that haven't been seen recently
// and enforces the max candidates limit using LRU eviction.
func (d *Detector) cleanupOldCandidates() {
	d.candidatesMu.Lock()
	defer d.candidatesMu.Unlock()

	threshold := time.Now().Add(-7 * 24 * time.Hour).UnixMilli()

	// First pass: remove expired candidates
	for key, candidate := range d.candidates {
		if candidate.lastSeenEpoch < threshold {
			delete(d.candidates, key)
		}
	}

	// Second pass: enforce max candidates limit using LRU eviction
	if d.config.MaxCandidates > 0 && len(d.candidates) > d.config.MaxCandidates {
		// Find oldest candidates to evict using O(n log n) sort instead of O(n²) selection sort
		type keyAge struct {
			key string
			age int64
		}
		candidates := make([]keyAge, 0, len(d.candidates))
		for k, c := range d.candidates {
			candidates = append(candidates, keyAge{k, c.lastSeenEpoch})
		}

		// Sort by age ascending (oldest first) - O(n log n)
		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].age < candidates[j].age
		})

		// Delete oldest entries
		toEvict := len(d.candidates) - d.config.MaxCandidates
		for i := 0; i < toEvict; i++ {
			delete(d.candidates, candidates[i].key)
		}

		log.Debug().Int("evicted", toEvict).Int("remaining", len(d.candidates)).Msg("Evicted old pattern candidates (LRU)")
	}
}

// CandidateCount returns the current number of pattern candidates.
func (d *Detector) CandidateCount() int {
	d.candidatesMu.RLock()
	defer d.candidatesMu.RUnlock()
	return len(d.candidates)
}

// GetPatternInsight returns a formatted insight string for a pattern.
func (d *Detector) GetPatternInsight(ctx context.Context, patternID int64) (string, error) {
	pattern, err := d.patternStore.GetPatternByID(ctx, patternID)
	if err != nil {
		return "", err
	}

	return formatPatternInsight(pattern), nil
}

// DetectionResult contains the result of pattern detection.
type DetectionResult struct {
	MatchedPattern *models.Pattern
	MatchScore     float64
	IsNewPattern   bool
}

// generateCandidateKey creates a unique key for a signature.
func generateCandidateKey(signature []string) string {
	if len(signature) == 0 {
		return ""
	}
	// Use strings.Builder to avoid O(n²) string concatenation
	var b strings.Builder
	// Pre-allocate: estimate average signature element is 10 chars + separator
	b.Grow(len(signature) * 11)
	for _, s := range signature {
		b.WriteString(s)
		b.WriteByte('|')
	}
	return b.String()
}

// generatePatternName creates a human-readable name for a pattern.
func generatePatternName(patternType models.PatternType, signature []string, title string) string {
	// Use title if available and meaningful
	if title != "" && len(title) < 60 {
		return title
	}

	// Otherwise generate from type and signature
	prefix := ""
	switch patternType {
	case models.PatternTypeBug:
		prefix = "Bug Pattern: "
	case models.PatternTypeRefactor:
		prefix = "Refactor Pattern: "
	case models.PatternTypeArchitecture:
		prefix = "Architecture Pattern: "
	case models.PatternTypeAntiPattern:
		prefix = "Anti-Pattern: "
	case models.PatternTypeBestPractice:
		prefix = "Best Practice: "
	}

	// Use first few signature elements
	if len(signature) > 0 {
		name := prefix
		for i, s := range signature {
			if i >= 3 {
				break
			}
			if i > 0 {
				name += ", "
			}
			name += s
		}
		return name
	}

	return prefix + "Unnamed"
}

// formatPatternInsight creates a human-readable insight from a pattern.
func formatPatternInsight(pattern *models.Pattern) string {
	insight := "I've encountered this pattern " +
		itoa(pattern.Frequency) + " times"

	if len(pattern.Projects) > 1 {
		insight += " across " + itoa(len(pattern.Projects)) + " projects"
	}

	insight += ". "

	if pattern.Recommendation.Valid && pattern.Recommendation.String != "" {
		insight += "What works: " + pattern.Recommendation.String
	} else {
		switch pattern.Type {
		case models.PatternTypeBug:
			insight += "This appears to be a recurring bug pattern."
		case models.PatternTypeAntiPattern:
			insight += "This is an identified anti-pattern to avoid."
		case models.PatternTypeBestPractice:
			insight += "This is a validated best practice."
		default:
			insight += "This is a recognized pattern in the codebase."
		}
	}

	return insight
}

// itoa converts int to string without importing strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	negative := false
	if n < 0 {
		negative = true
		n = -n
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if negative {
		digits = append([]byte{'-'}, digits...)
	}
	return string(digits)
}
