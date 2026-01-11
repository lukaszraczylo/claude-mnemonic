package graph

import (
	"context"
	"fmt"
	"math"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
	"github.com/rs/zerolog/log"
)

const (
	// SemanticSimilarityThreshold for creating semantic edges
	SemanticSimilarityThreshold = 0.85

	// MinFileOverlapForEdge minimum file overlap ratio to create edge
	MinFileOverlapForEdge = 0.3

	// MaxEdgesPerNode prevents creating too many edges
	MaxEdgesPerNode = 20
)

// DetectEdges identifies relationships between observations
func DetectEdges(ctx context.Context, observations []*models.Observation) ([]Edge, error) {
	if len(observations) < 2 {
		return nil, nil
	}

	edges := make([]Edge, 0)

	// Build lookup maps for efficient detection
	sessionMap := buildSessionMap(observations)
	conceptMap := buildConceptMap(observations)
	fileMap := buildFileMap(observations)

	log.Info().
		Int("observations", len(observations)).
		Int("sessions", len(sessionMap)).
		Int("concepts", len(conceptMap)).
		Msg("Starting edge detection")

	// Detect temporal edges (same session)
	temporalEdges := detectTemporalEdges(sessionMap)
	edges = append(edges, temporalEdges...)

	// Detect concept edges (shared tags)
	conceptEdges := detectConceptEdges(conceptMap)
	edges = append(edges, conceptEdges...)

	// Detect file overlap edges
	fileEdges := detectFileOverlapEdges(fileMap, observations)
	edges = append(edges, fileEdges...)

	// Prune excessive edges per node
	edges = pruneEdges(edges, MaxEdgesPerNode)

	log.Info().
		Int("temporal_edges", len(temporalEdges)).
		Int("concept_edges", len(conceptEdges)).
		Int("file_edges", len(fileEdges)).
		Int("total_edges", len(edges)).
		Msg("Edge detection complete")

	return edges, nil
}

// buildSessionMap groups observations by SDK session
func buildSessionMap(observations []*models.Observation) map[string][]int64 {
	sessionMap := make(map[string][]int64)

	for _, obs := range observations {
		if obs.SDKSessionID != "" {
			sessionMap[obs.SDKSessionID] = append(sessionMap[obs.SDKSessionID], obs.ID)
		}
	}

	return sessionMap
}

// buildConceptMap groups observations by concept tags
func buildConceptMap(observations []*models.Observation) map[string][]int64 {
	conceptMap := make(map[string][]int64)

	for _, obs := range observations {
		for _, concept := range obs.Concepts {
			conceptMap[concept] = append(conceptMap[concept], obs.ID)
		}
	}

	return conceptMap
}

// buildFileMap maps files to observations (from both FilesRead and FilesModified)
func buildFileMap(observations []*models.Observation) map[string][]int64 {
	fileMap := make(map[string][]int64)

	for _, obs := range observations {
		// Add files from FilesRead
		for _, file := range obs.FilesRead {
			fileMap[file] = append(fileMap[file], obs.ID)
		}
		// Add files from FilesModified
		for _, file := range obs.FilesModified {
			fileMap[file] = append(fileMap[file], obs.ID)
		}
	}

	return fileMap
}

// detectTemporalEdges creates edges between observations in the same session
func detectTemporalEdges(sessionMap map[string][]int64) []Edge {
	edges := make([]Edge, 0)

	for _, obsIDs := range sessionMap {
		if len(obsIDs) < 2 {
			continue
		}

		// Create edges between consecutive observations in session
		for i := 0; i < len(obsIDs)-1; i++ {
			edges = append(edges, Edge{
				FromID:   obsIDs[i],
				ToID:     obsIDs[i+1],
				Relation: RelationTemporal,
				Weight:   0.8, // High weight for temporal proximity
			})
		}
	}

	return edges
}

// detectConceptEdges creates edges between observations sharing concepts
func detectConceptEdges(conceptMap map[string][]int64) []Edge {
	edges := make([]Edge, 0)
	seen := make(map[string]bool)

	for concept, obsIDs := range conceptMap {
		if len(obsIDs) < 2 {
			continue
		}

		// Create edges between all observations sharing this concept
		for i := 0; i < len(obsIDs); i++ {
			for j := i + 1; j < len(obsIDs); j++ {
				// Use sorted pair as key to avoid duplicates
				pairKey := edgeKey(obsIDs[i], obsIDs[j])
				if seen[pairKey] {
					continue
				}
				seen[pairKey] = true

				// Weight based on concept specificity (longer = more specific)
				weight := float32(0.5 + 0.3*math.Min(1.0, float64(len(concept))/20.0))

				edges = append(edges, Edge{
					FromID:   obsIDs[i],
					ToID:     obsIDs[j],
					Relation: RelationConcept,
					Weight:   weight,
				})
			}
		}
	}

	return edges
}

// detectFileOverlapEdges creates edges based on file references
func detectFileOverlapEdges(fileMap map[string][]int64, observations []*models.Observation) []Edge {
	edges := make([]Edge, 0)
	seen := make(map[string]bool)

	// Build observation ID to observation map for quick lookup
	obsMap := make(map[int64]*models.Observation)
	for _, obs := range observations {
		obsMap[obs.ID] = obs
	}

	for _, obsIDs := range fileMap {
		if len(obsIDs) < 2 {
			continue
		}

		// Create edges between observations referencing same files
		for i := 0; i < len(obsIDs); i++ {
			for j := i + 1; j < len(obsIDs); j++ {
				pairKey := edgeKey(obsIDs[i], obsIDs[j])
				if seen[pairKey] {
					continue
				}
				seen[pairKey] = true

				// Calculate file overlap ratio
				obs1, ok1 := obsMap[obsIDs[i]]
				obs2, ok2 := obsMap[obsIDs[j]]

				if !ok1 || !ok2 {
					continue
				}

				// Merge FilesRead and FilesModified for both observations
				files1 := append([]string{}, obs1.FilesRead...)
				files1 = append(files1, obs1.FilesModified...)
				files2 := append([]string{}, obs2.FilesRead...)
				files2 = append(files2, obs2.FilesModified...)

				overlap := calculateFileOverlap(files1, files2)
				if overlap < MinFileOverlapForEdge {
					continue
				}

				edges = append(edges, Edge{
					FromID:   obsIDs[i],
					ToID:     obsIDs[j],
					Relation: RelationFileOverlap,
					Weight:   overlap,
				})
			}
		}
	}

	return edges
}

// calculateFileOverlap computes Jaccard similarity of file sets
func calculateFileOverlap(files1, files2 []string) float32 {
	if len(files1) == 0 || len(files2) == 0 {
		return 0.0
	}

	// Convert to sets
	set1 := make(map[string]bool)
	for _, f := range files1 {
		set1[f] = true
	}

	set2 := make(map[string]bool)
	for _, f := range files2 {
		set2[f] = true
	}

	// Count intersection
	intersection := 0
	for f := range set1 {
		if set2[f] {
			intersection++
		}
	}

	// Jaccard similarity = intersection / union
	union := len(set1) + len(set2) - intersection
	if union == 0 {
		return 0.0
	}

	return float32(intersection) / float32(union)
}

// pruneEdges limits edges per node to prevent graph explosion
func pruneEdges(edges []Edge, maxPerNode int) []Edge {
	if maxPerNode <= 0 {
		return edges
	}

	// Count edges per node
	outEdges := make(map[int64][]Edge)
	inEdges := make(map[int64][]Edge)

	for _, edge := range edges {
		outEdges[edge.FromID] = append(outEdges[edge.FromID], edge)
		inEdges[edge.ToID] = append(inEdges[edge.ToID], edge)
	}

	// Prune low-weight edges if node has too many
	pruned := make([]Edge, 0, len(edges))
	processed := make(map[string]bool)

	for _, edge := range edges {
		pairKey := edgeKey(edge.FromID, edge.ToID)
		if processed[pairKey] {
			continue
		}
		processed[pairKey] = true

		// Check if either node has too many edges
		fromCount := len(outEdges[edge.FromID])
		toCount := len(inEdges[edge.ToID])

		if fromCount <= maxPerNode && toCount <= maxPerNode {
			pruned = append(pruned, edge)
			continue
		}

		// Keep edge if it's high-weight (top edges for this node)
		if shouldKeepEdge(edge, outEdges[edge.FromID], maxPerNode) {
			pruned = append(pruned, edge)
		}
	}

	if len(pruned) < len(edges) {
		log.Debug().
			Int("original", len(edges)).
			Int("pruned", len(pruned)).
			Int("removed", len(edges)-len(pruned)).
			Msg("Pruned excessive edges")
	}

	return pruned
}

// shouldKeepEdge determines if edge should be kept during pruning
func shouldKeepEdge(edge Edge, nodeEdges []Edge, maxPerNode int) bool {
	// Sort node's edges by weight descending
	sortedEdges := make([]Edge, len(nodeEdges))
	copy(sortedEdges, nodeEdges)

	sortEdgesByWeight(sortedEdges)

	// Keep edge if it's in top maxPerNode
	for i := 0; i < maxPerNode && i < len(sortedEdges); i++ {
		if sortedEdges[i].FromID == edge.FromID && sortedEdges[i].ToID == edge.ToID {
			return true
		}
	}

	return false
}

// sortEdgesByWeight sorts edges by weight descending
func sortEdgesByWeight(edges []Edge) {
	// Simple bubble sort (edges are typically small per node)
	n := len(edges)
	for i := 0; i < n-1; i++ {
		for j := 0; j < n-i-1; j++ {
			if edges[j].Weight < edges[j+1].Weight {
				edges[j], edges[j+1] = edges[j+1], edges[j]
			}
		}
	}
}

// edgeKey creates a unique key for an edge pair (sorted)
func edgeKey(id1, id2 int64) string {
	if id1 < id2 {
		return fmt.Sprintf("%d-%d", id1, id2)
	}
	return fmt.Sprintf("%d-%d", id2, id1)
}

// DetectSemanticEdges creates edges based on semantic similarity
// This requires embeddings and is called separately when available
func DetectSemanticEdges(ctx context.Context, observations []*models.Observation, embeddings map[int64][]float32) []Edge {
	edges := make([]Edge, 0)
	seen := make(map[string]bool)

	// Compare all pairs (expensive, but necessary for semantic similarity)
	for i := 0; i < len(observations); i++ {
		emb1, ok1 := embeddings[observations[i].ID]
		if !ok1 {
			continue
		}

		for j := i + 1; j < len(observations); j++ {
			emb2, ok2 := embeddings[observations[j].ID]
			if !ok2 {
				continue
			}

			similarity := cosineSimilarity(emb1, emb2)
			if similarity < SemanticSimilarityThreshold {
				continue
			}

			pairKey := edgeKey(observations[i].ID, observations[j].ID)
			if seen[pairKey] {
				continue
			}
			seen[pairKey] = true

			edges = append(edges, Edge{
				FromID:   observations[i].ID,
				ToID:     observations[j].ID,
				Relation: RelationSemantic,
				Weight:   similarity,
			})
		}
	}

	log.Info().
		Int("semantic_edges", len(edges)).
		Float32("threshold", SemanticSimilarityThreshold).
		Msg("Detected semantic edges")

	return edges
}

// cosineSimilarity computes cosine similarity between two vectors
func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0.0
	}

	var dotProduct, normA, normB float32
	for i := range a {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 0.0
	}

	return dotProduct / float32(math.Sqrt(float64(normA))*math.Sqrt(float64(normB)))
}
