// Package models contains domain models for claude-mnemonic.
package models

import (
	"strings"
	"time"
)

// RelationType represents the type of relationship between observations.
type RelationType string

const (
	// RelationCauses means source observation caused target observation.
	// Example: "This architectural decision caused this bug"
	RelationCauses RelationType = "causes"
	// RelationFixes means source observation fixes target observation.
	// Example: "This bugfix addresses that discovered issue"
	RelationFixes RelationType = "fixes"
	// RelationSupersedes means source observation supersedes target observation.
	// Example: "This new approach replaces the old workaround"
	RelationSupersedes RelationType = "supersedes"
	// RelationDependsOn means source observation depends on target observation.
	// Example: "This feature relies on that architectural decision"
	RelationDependsOn RelationType = "depends_on"
	// RelationRelatesTo means observations are related but no causal relationship.
	// Example: "Both deal with authentication"
	RelationRelatesTo RelationType = "relates_to"
	// RelationEvolvesFrom means source observation evolved from target observation.
	// Example: "This refined pattern evolved from that initial discovery"
	RelationEvolvesFrom RelationType = "evolves_from"
)

// AllRelationTypes is the list of all valid relation types.
var AllRelationTypes = []RelationType{
	RelationCauses,
	RelationFixes,
	RelationSupersedes,
	RelationDependsOn,
	RelationRelatesTo,
	RelationEvolvesFrom,
}

// RelationDetectionSource indicates how a relationship was detected.
type RelationDetectionSource string

const (
	// DetectionSourceFileOverlap means relationship was detected via shared file references.
	DetectionSourceFileOverlap RelationDetectionSource = "file_overlap"
	// DetectionSourceEmbeddingSimilarity means relationship was detected via vector similarity.
	DetectionSourceEmbeddingSimilarity RelationDetectionSource = "embedding_similarity"
	// DetectionSourceTemporalProximity means relationship was detected via close timestamps.
	DetectionSourceTemporalProximity RelationDetectionSource = "temporal_proximity"
	// DetectionSourceNarrativeMention means relationship was detected via explicit mentions.
	DetectionSourceNarrativeMention RelationDetectionSource = "narrative_mention"
	// DetectionSourceConceptOverlap means relationship was detected via shared concepts.
	DetectionSourceConceptOverlap RelationDetectionSource = "concept_overlap"
	// DetectionSourceTypeProgression means relationship was detected via type progression pattern.
	DetectionSourceTypeProgression RelationDetectionSource = "type_progression"
)

// ObservationRelation represents a directed relationship between two observations.
type ObservationRelation struct {
	ID              int64                   `db:"id" json:"id"`
	SourceID        int64                   `db:"source_id" json:"source_id"`
	TargetID        int64                   `db:"target_id" json:"target_id"`
	RelationType    RelationType            `db:"relation_type" json:"relation_type"`
	Confidence      float64                 `db:"confidence" json:"confidence"`
	DetectionSource RelationDetectionSource `db:"detection_source" json:"detection_source"`
	Reason          string                  `db:"reason" json:"reason,omitempty"`
	CreatedAt       string                  `db:"created_at" json:"created_at"`
	CreatedAtEpoch  int64                   `db:"created_at_epoch" json:"created_at_epoch"`
}

// NewObservationRelation creates a new observation relation.
func NewObservationRelation(sourceID, targetID int64, relType RelationType, confidence float64, source RelationDetectionSource, reason string) *ObservationRelation {
	now := time.Now()
	return &ObservationRelation{
		SourceID:        sourceID,
		TargetID:        targetID,
		RelationType:    relType,
		Confidence:      confidence,
		DetectionSource: source,
		Reason:          reason,
		CreatedAt:       now.Format(time.RFC3339),
		CreatedAtEpoch:  now.UnixMilli(),
	}
}

// RelationDetectionResult contains the result of relation detection.
type RelationDetectionResult struct {
	SourceID        int64
	TargetID        int64
	RelationType    RelationType
	Confidence      float64
	DetectionSource RelationDetectionSource
	Reason          string
}

// DetectFileOverlapRelation checks if observations share file references and determines relationship type.
func DetectFileOverlapRelation(newer, older *Observation) *RelationDetectionResult {
	// Check for overlapping modified files
	newerModified := make(map[string]bool)
	for _, f := range newer.FilesModified {
		newerModified[f] = true
	}

	olderModified := make(map[string]bool)
	for _, f := range older.FilesModified {
		olderModified[f] = true
	}

	// Files modified by both
	var sharedModified []string
	for f := range newerModified {
		if olderModified[f] {
			sharedModified = append(sharedModified, f)
		}
	}

	// Files that newer reads which older modified
	var newerReadsOlderModified []string
	for _, f := range newer.FilesRead {
		if olderModified[f] {
			newerReadsOlderModified = append(newerReadsOlderModified, f)
		}
	}

	// Calculate overlap score
	overlap := len(sharedModified) + len(newerReadsOlderModified)
	if overlap == 0 {
		return nil
	}

	// Determine relationship type based on observation types and file overlap
	relType := RelationRelatesTo
	confidence := 0.5 + float64(overlap)*0.1 // Base 0.5, +0.1 per overlapping file

	// Type-based relationship inference
	switch {
	case newer.Type == ObsTypeBugfix && (older.Type == ObsTypeDecision || older.Type == ObsTypeFeature):
		relType = RelationFixes
		confidence += 0.2
	case newer.Type == ObsTypeRefactor && older.Type == ObsTypeDiscovery:
		relType = RelationEvolvesFrom
		confidence += 0.15
	case newer.Type == older.Type && len(sharedModified) > 0:
		relType = RelationSupersedes
		confidence += 0.1
	case newer.Type == ObsTypeFeature && older.Type == ObsTypeDecision:
		relType = RelationDependsOn
		confidence += 0.15
	}

	if confidence > 1.0 {
		confidence = 1.0
	}

	reason := buildFileOverlapReason(sharedModified, newerReadsOlderModified)

	return &RelationDetectionResult{
		SourceID:        newer.ID,
		TargetID:        older.ID,
		RelationType:    relType,
		Confidence:      confidence,
		DetectionSource: DetectionSourceFileOverlap,
		Reason:          reason,
	}
}

// buildFileOverlapReason creates a human-readable reason for file overlap relation.
func buildFileOverlapReason(shared, readsModified []string) string {
	parts := []string{}
	if len(shared) > 0 {
		parts = append(parts, "both modified: "+strings.Join(truncateList(shared, 3), ", "))
	}
	if len(readsModified) > 0 {
		parts = append(parts, "reads files modified by older: "+strings.Join(truncateList(readsModified, 3), ", "))
	}
	return strings.Join(parts, "; ")
}

// DetectConceptOverlapRelation checks if observations share concepts.
func DetectConceptOverlapRelation(newer, older *Observation) *RelationDetectionResult {
	newerConcepts := make(map[string]bool)
	for _, c := range newer.Concepts {
		newerConcepts[c] = true
	}

	var shared []string
	for _, c := range older.Concepts {
		if newerConcepts[c] {
			shared = append(shared, c)
		}
	}

	if len(shared) == 0 {
		return nil
	}

	// Calculate confidence based on overlap ratio
	totalUniqueConcepts := len(newerConcepts)
	for _, c := range older.Concepts {
		if !newerConcepts[c] {
			totalUniqueConcepts++
		}
	}

	overlapRatio := float64(len(shared)) / float64(totalUniqueConcepts)
	confidence := 0.3 + overlapRatio*0.5 // Base 0.3, scale with overlap

	// Boost for important concepts
	for _, c := range shared {
		if isHighValueConcept(c) {
			confidence += 0.1
		}
	}
	if confidence > 1.0 {
		confidence = 1.0
	}

	return &RelationDetectionResult{
		SourceID:        newer.ID,
		TargetID:        older.ID,
		RelationType:    RelationRelatesTo,
		Confidence:      confidence,
		DetectionSource: DetectionSourceConceptOverlap,
		Reason:          "shared concepts: " + strings.Join(truncateList(shared, 5), ", "),
	}
}

// isHighValueConcept returns true for concepts that strongly indicate relationships.
func isHighValueConcept(concept string) bool {
	highValue := map[string]bool{
		"security":       true,
		"architecture":   true,
		"gotcha":         true,
		"anti-pattern":   true,
		"best-practice":  true,
		"error-handling": true,
	}
	return highValue[concept]
}

// DetectTypeProgressionRelation checks for natural type progressions.
// Example: discovery -> decision -> feature -> bugfix
func DetectTypeProgressionRelation(newer, older *Observation) *RelationDetectionResult {
	// Define natural type progressions
	progressions := map[ObservationType][]ObservationType{
		ObsTypeBugfix:   {ObsTypeDiscovery, ObsTypeFeature, ObsTypeDecision},
		ObsTypeFeature:  {ObsTypeDiscovery, ObsTypeDecision},
		ObsTypeRefactor: {ObsTypeDiscovery, ObsTypeFeature, ObsTypeBugfix},
		ObsTypeDecision: {ObsTypeDiscovery},
		ObsTypeChange:   {ObsTypeDiscovery, ObsTypeDecision},
	}

	validPredecessors, ok := progressions[newer.Type]
	if !ok {
		return nil
	}

	isValidProgression := false
	for _, pred := range validPredecessors {
		if older.Type == pred {
			isValidProgression = true
			break
		}
	}

	if !isValidProgression {
		return nil
	}

	// Determine relationship type based on progression
	var relType RelationType
	var confidence float64 = 0.4

	switch {
	case newer.Type == ObsTypeBugfix && older.Type == ObsTypeDiscovery:
		relType = RelationFixes
		confidence = 0.6
	case newer.Type == ObsTypeBugfix && older.Type == ObsTypeFeature:
		relType = RelationFixes
		confidence = 0.5
	case newer.Type == ObsTypeFeature && older.Type == ObsTypeDecision:
		relType = RelationDependsOn
		confidence = 0.6
	case newer.Type == ObsTypeRefactor:
		relType = RelationEvolvesFrom
		confidence = 0.5
	default:
		relType = RelationRelatesTo
	}

	return &RelationDetectionResult{
		SourceID:        newer.ID,
		TargetID:        older.ID,
		RelationType:    relType,
		Confidence:      confidence,
		DetectionSource: DetectionSourceTypeProgression,
		Reason:          string(older.Type) + " -> " + string(newer.Type) + " progression",
	}
}

// DetectTemporalProximityRelation checks if observations are temporally close (same session).
func DetectTemporalProximityRelation(newer, older *Observation) *RelationDetectionResult {
	// Only relate observations from the same session
	if newer.SDKSessionID != older.SDKSessionID {
		return nil
	}

	// Check temporal proximity (within 5 minutes)
	timeDiffMs := newer.CreatedAtEpoch - older.CreatedAtEpoch
	if timeDiffMs < 0 {
		timeDiffMs = -timeDiffMs
	}

	fiveMinutesMs := int64(5 * 60 * 1000)
	if timeDiffMs > fiveMinutesMs {
		return nil
	}

	// Calculate confidence based on temporal proximity
	// Closer = higher confidence
	proximityRatio := 1.0 - (float64(timeDiffMs) / float64(fiveMinutesMs))
	confidence := 0.3 + proximityRatio*0.4

	return &RelationDetectionResult{
		SourceID:        newer.ID,
		TargetID:        older.ID,
		RelationType:    RelationRelatesTo,
		Confidence:      confidence,
		DetectionSource: DetectionSourceTemporalProximity,
		Reason:          "same session, close timestamps",
	}
}

// NarrativeMentionPatterns are patterns that indicate explicit relationships in narratives.
var NarrativeMentionPatterns = []struct {
	Pattern      string
	RelationType RelationType
	ConfBoost    float64
}{
	{" caused ", RelationCauses, 0.3},
	{" causes ", RelationCauses, 0.3},
	{" because of ", RelationCauses, 0.25},
	{" due to ", RelationCauses, 0.2},
	{" fixes ", RelationFixes, 0.3},
	{" fixed ", RelationFixes, 0.3},
	{" resolves ", RelationFixes, 0.3},
	{" addresses ", RelationFixes, 0.25},
	{" replaces ", RelationSupersedes, 0.3},
	{" supersedes ", RelationSupersedes, 0.35},
	{" instead of ", RelationSupersedes, 0.25},
	{" depends on ", RelationDependsOn, 0.3},
	{" requires ", RelationDependsOn, 0.25},
	{" builds on ", RelationDependsOn, 0.25},
	{" based on ", RelationDependsOn, 0.2},
	{" related to ", RelationRelatesTo, 0.2},
	{" similar to ", RelationRelatesTo, 0.2},
	{" evolved from ", RelationEvolvesFrom, 0.3},
	{" improved from ", RelationEvolvesFrom, 0.25},
	{" refined from ", RelationEvolvesFrom, 0.25},
}

// DetectNarrativeMentionRelation checks if newer observation's narrative mentions relationship.
func DetectNarrativeMentionRelation(newer, older *Observation) *RelationDetectionResult {
	if !newer.Narrative.Valid || newer.Narrative.String == "" {
		return nil
	}

	narrative := strings.ToLower(newer.Narrative.String)

	// Check for patterns
	for _, p := range NarrativeMentionPatterns {
		if strings.Contains(narrative, p.Pattern) {
			// Found a pattern - this is a potential relationship
			confidence := 0.4 + p.ConfBoost
			if confidence > 1.0 {
				confidence = 1.0
			}

			return &RelationDetectionResult{
				SourceID:        newer.ID,
				TargetID:        older.ID,
				RelationType:    p.RelationType,
				Confidence:      confidence,
				DetectionSource: DetectionSourceNarrativeMention,
				Reason:          "narrative contains '" + strings.TrimSpace(p.Pattern) + "' language",
			}
		}
	}

	return nil
}

// DetectRelationsWithExisting checks a new observation against existing ones and returns detected relations.
// This is the main entry point for relation detection.
func DetectRelationsWithExisting(newer *Observation, existing []*Observation, minConfidence float64) []*RelationDetectionResult {
	var results []*RelationDetectionResult
	seen := make(map[int64]bool)

	for _, older := range existing {
		// Skip self
		if older.ID == newer.ID {
			continue
		}

		// Skip if already superseded
		if older.IsSuperseded {
			continue
		}

		// Only compare within same project (or both global)
		if newer.Project != older.Project && newer.Scope != ScopeGlobal && older.Scope != ScopeGlobal {
			continue
		}

		// Run all detection methods and keep highest confidence result per target
		var bestResult *RelationDetectionResult

		// 1. File overlap detection
		if result := DetectFileOverlapRelation(newer, older); result != nil && result.Confidence >= minConfidence {
			if bestResult == nil || result.Confidence > bestResult.Confidence {
				bestResult = result
			}
		}

		// 2. Concept overlap detection
		if result := DetectConceptOverlapRelation(newer, older); result != nil && result.Confidence >= minConfidence {
			if bestResult == nil || result.Confidence > bestResult.Confidence {
				bestResult = result
			}
		}

		// 3. Type progression detection
		if result := DetectTypeProgressionRelation(newer, older); result != nil && result.Confidence >= minConfidence {
			if bestResult == nil || result.Confidence > bestResult.Confidence {
				bestResult = result
			}
		}

		// 4. Temporal proximity detection
		if result := DetectTemporalProximityRelation(newer, older); result != nil && result.Confidence >= minConfidence {
			// Only use temporal proximity if no better detection found
			if bestResult == nil {
				bestResult = result
			}
		}

		// 5. Narrative mention detection (can upgrade relation type)
		if result := DetectNarrativeMentionRelation(newer, older); result != nil && result.Confidence >= minConfidence {
			if bestResult == nil || result.Confidence > bestResult.Confidence {
				bestResult = result
			}
		}

		// Add best result if found and not already seen
		if bestResult != nil && !seen[older.ID] {
			results = append(results, bestResult)
			seen[older.ID] = true
		}
	}

	return results
}

// truncateList truncates a list to maxLen items.
func truncateList(items []string, maxLen int) []string {
	if len(items) <= maxLen {
		return items
	}
	result := items[:maxLen]
	return append(result, "...")
}

// RelationWithDetails contains a relation with its observation details.
type RelationWithDetails struct {
	Relation    *ObservationRelation `json:"relation"`
	SourceTitle string               `json:"source_title"`
	TargetTitle string               `json:"target_title"`
	SourceType  ObservationType      `json:"source_type"`
	TargetType  ObservationType      `json:"target_type"`
}

// RelationGraph represents a graph of related observations.
type RelationGraph struct {
	CenterID  int64                  `json:"center_id"`
	Relations []*RelationWithDetails `json:"relations"`
}
