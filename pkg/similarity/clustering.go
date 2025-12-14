// Package similarity provides text similarity and clustering utilities.
package similarity

import (
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
