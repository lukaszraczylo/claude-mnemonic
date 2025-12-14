// Package worker provides the main worker service for claude-mnemonic.
package worker

import (
	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
	"github.com/lukaszraczylo/claude-mnemonic/pkg/similarity"
)

// clusterObservations groups similar observations and returns only one representative per cluster.
// Uses Jaccard similarity on extracted terms from title, narrative, and facts.
// Delegates to pkg/similarity for the actual clustering logic.
func clusterObservations(observations []*models.Observation, similarityThreshold float64) []*models.Observation {
	return similarity.ClusterObservations(observations, similarityThreshold)
}
