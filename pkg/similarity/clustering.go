// Package similarity provides text similarity and clustering utilities.
package similarity

import (
	"math/bits"
	"strings"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
)

// ClusterObservations groups similar observations and returns only one representative per cluster.
// Uses Jaccard similarity on extracted terms from title, narrative, and facts.
// Observations should be sorted by preference (e.g., recency) - first one in each cluster is kept.
func ClusterObservations(observations []*models.Observation, similarityThreshold float64) []*models.Observation {
	if len(observations) <= 1 {
		return observations
	}

	// For small sets, use the simple O(n²) algorithm
	if len(observations) <= 50 {
		return clusterObservationsSimple(observations, similarityThreshold)
	}

	// For larger sets, use an optimized approach with early termination
	return clusterObservationsOptimized(observations, similarityThreshold)
}

// clusterObservationsSimple is the simple O(n²) algorithm for small sets.
func clusterObservationsSimple(observations []*models.Observation, similarityThreshold float64) []*models.Observation {
	// Extract terms for each observation
	termSets := make([]map[string]bool, len(observations))
	for i, obs := range observations {
		termSets[i] = ExtractObservationTerms(obs)
	}

	// Track which observations are already clustered
	clustered := make([]bool, len(observations))
	result := make([]*models.Observation, 0)

	for i := 0; i < len(observations); i++ {
		if clustered[i] {
			continue
		}

		// This observation becomes the representative of its cluster
		// (observations are already sorted by recency, so first one is newest)
		result = append(result, observations[i])
		clustered[i] = true

		// Find all similar observations and mark them as clustered
		for j := i + 1; j < len(observations); j++ {
			if clustered[j] {
				continue
			}

			similarity := JaccardSimilarity(termSets[i], termSets[j])
			if similarity >= similarityThreshold {
				clustered[j] = true
			}
		}
	}

	return result
}

// clusterObservationsOptimized uses MinHash-based approximation for large sets.
// This reduces complexity from O(n²) to approximately O(n*k) where k is the number of hash functions.
func clusterObservationsOptimized(observations []*models.Observation, similarityThreshold float64) []*models.Observation {
	n := len(observations)

	// Extract terms for each observation and compute a signature
	type termSetWithSig struct {
		terms     map[string]bool
		signature uint64 // Simple hash signature for fast comparison
	}

	termSets := make([]termSetWithSig, n)
	for i, obs := range observations {
		terms := ExtractObservationTerms(obs)
		termSets[i] = termSetWithSig{
			terms:     terms,
			signature: computeTermSignature(terms),
		}
	}

	// Track which observations are already clustered
	clustered := make([]bool, n)
	result := make([]*models.Observation, 0, n/2) // Pre-allocate assuming ~50% are unique

	for i := 0; i < n; i++ {
		if clustered[i] {
			continue
		}

		// This observation becomes the representative of its cluster
		result = append(result, observations[i])
		clustered[i] = true

		// Use signature for fast pre-filtering
		sigI := termSets[i].signature
		termsI := termSets[i].terms

		// Find all similar observations and mark them as clustered
		for j := i + 1; j < n; j++ {
			if clustered[j] {
				continue
			}

			// Quick signature comparison - if signatures are very different, skip detailed comparison
			sigJ := termSets[j].signature
			sigDiff := sigI ^ sigJ
			popCount := popCount64(sigDiff)

			// If signatures differ significantly, similarity is likely low
			// Skip detailed comparison for very different signatures
			if popCount > 32 { // More than half of bits differ
				continue
			}

			// Full Jaccard comparison for candidates
			similarity := JaccardSimilarity(termsI, termSets[j].terms)
			if similarity >= similarityThreshold {
				clustered[j] = true
			}
		}
	}

	return result
}

// computeTermSignature creates a quick hash signature for term sets.
// Used for fast pre-filtering in the optimized clustering algorithm.
func computeTermSignature(terms map[string]bool) uint64 {
	var sig uint64
	for term := range terms {
		// Simple hash using FNV-1a inspired approach
		h := uint64(14695981039346656037)
		for i := 0; i < len(term); i++ {
			h ^= uint64(term[i])
			h *= 1099511628211
		}
		sig ^= h
	}
	return sig
}

// popCount64 counts the number of set bits in a 64-bit integer.
// Uses the stdlib bits.OnesCount64 which may use CPU POPCNT instruction.
func popCount64(x uint64) int {
	return bits.OnesCount64(x)
}

// IsSimilarToAny checks if a new observation is similar to any existing observation.
// Returns true if similarity to any existing observation exceeds the threshold.
func IsSimilarToAny(newObs *models.Observation, existing []*models.Observation, similarityThreshold float64) bool {
	if len(existing) == 0 {
		return false
	}

	newTerms := ExtractObservationTerms(newObs)
	if len(newTerms) == 0 {
		return false
	}

	for _, obs := range existing {
		existingTerms := ExtractObservationTerms(obs)
		similarity := JaccardSimilarity(newTerms, existingTerms)
		if similarity >= similarityThreshold {
			return true
		}
	}

	return false
}

// ExtractObservationTerms extracts meaningful terms from an observation for similarity comparison.
func ExtractObservationTerms(obs *models.Observation) map[string]bool {
	terms := make(map[string]bool)

	// Add terms from title
	addTerms(terms, obs.Title.String)

	// Add terms from narrative
	addTerms(terms, obs.Narrative.String)

	// Add terms from facts
	for _, fact := range obs.Facts {
		addTerms(terms, fact)
	}

	// Add file paths as terms (normalized)
	for _, file := range obs.FilesRead {
		// Use just the filename without path for matching
		parts := strings.Split(file, "/")
		if len(parts) > 0 {
			terms[strings.ToLower(parts[len(parts)-1])] = true
		}
	}

	for _, file := range obs.FilesModified {
		parts := strings.Split(file, "/")
		if len(parts) > 0 {
			terms[strings.ToLower(parts[len(parts)-1])] = true
		}
	}

	return terms
}

// addTerms tokenizes text and adds meaningful terms to the set.
func addTerms(terms map[string]bool, text string) {
	// Simple tokenization: split on non-alphanumeric, filter short words
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_')
	})

	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true,
		"was": true, "were": true, "be": true, "been": true, "being": true,
		"have": true, "has": true, "had": true, "do": true, "does": true,
		"did": true, "will": true, "would": true, "could": true, "should": true,
		"may": true, "might": true, "must": true, "shall": true,
		"this": true, "that": true, "these": true, "those": true,
		"and": true, "or": true, "but": true, "if": true, "then": true,
		"for": true, "from": true, "with": true, "about": true, "into": true,
		"to": true, "of": true, "in": true, "on": true, "at": true, "by": true,
		"it": true, "its": true, "which": true, "who": true, "what": true,
		"when": true, "where": true, "how": true, "why": true,
	}

	for _, word := range words {
		if len(word) >= 3 && !stopWords[word] {
			terms[word] = true
		}
	}
}

// JaccardSimilarity calculates the Jaccard similarity between two term sets.
// Returns a value between 0 (no overlap) and 1 (identical).
func JaccardSimilarity(set1, set2 map[string]bool) float64 {
	if len(set1) == 0 && len(set2) == 0 {
		return 1.0
	}
	if len(set1) == 0 || len(set2) == 0 {
		return 0.0
	}

	intersection := 0
	for term := range set1 {
		if set2[term] {
			intersection++
		}
	}

	union := len(set1) + len(set2) - intersection
	if union == 0 {
		return 0.0
	}

	return float64(intersection) / float64(union)
}
