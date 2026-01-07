package hybrid

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/lukaszraczylo/claude-mnemonic/internal/graph"
	"github.com/lukaszraczylo/claude-mnemonic/internal/vector/sqlitevec"
	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
	"github.com/rs/zerolog/log"
)

// GraphConfig configures graph-aware search
type GraphConfig struct {
	Enabled      bool
	MaxHops      int     // Maximum graph traversal depth (default: 2)
	BranchFactor int     // Number of neighbors to expand per node (default: 5)
	EdgeWeight   float64 // Minimum edge weight to follow (default: 0.3)
}

// DefaultGraphConfig returns sensible defaults for graph search
func DefaultGraphConfig() GraphConfig {
	return GraphConfig{
		Enabled:      true,
		MaxHops:      2,
		BranchFactor: 5,
		EdgeWeight:   0.3,
	}
}

// GraphSearchClient wraps hybrid.Client with graph-aware search
type GraphSearchClient struct {
	*Client
	graph       *graph.ObservationGraph
	graphConfig GraphConfig
}

// NewGraphSearchClient creates a graph-enhanced hybrid client
func NewGraphSearchClient(baseClient *Client, observationGraph *graph.ObservationGraph, cfg GraphConfig) *GraphSearchClient {
	return &GraphSearchClient{
		Client:      baseClient,
		graph:       observationGraph,
		graphConfig: cfg,
	}
}

// Query performs graph-aware vector search with two-level traversal
func (g *GraphSearchClient) Query(ctx context.Context, query string, limit int, where map[string]any) ([]sqlitevec.QueryResult, error) {
	if !g.graphConfig.Enabled || g.graph == nil {
		// Fall back to standard hybrid search
		return g.Client.Query(ctx, query, limit, where)
	}

	startTime := time.Now()

	// 1. Generate query embedding
	queryEmb, err := g.embedSvc.Embed(query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	// 2. Search hub nodes (stored embeddings)
	hubResults, err := g.base.Query(ctx, query, limit*2, where)
	if err != nil {
		// Fall back to standard search on error
		log.Warn().Err(err).Msg("Hub search failed, falling back to hybrid search")
		return g.Client.Query(ctx, query, limit, where)
	}

	// 3. Track hub access
	g.trackAccess(hubResults)

	// 4. Expand via graph traversal
	expandedIDs := g.expandFromHubs(hubResults, limit*4)

	// 5. Filter to non-hubs that need recomputation
	nonHubIDs := make([]string, 0)
	for _, id := range expandedIDs {
		if !g.isHub(id) {
			nonHubIDs = append(nonHubIDs, id)
		}
	}

	// 6. Batch recompute non-hub embeddings
	recomputedResults, err := g.recomputeAndScore(ctx, query, nonHubIDs)
	if err != nil {
		log.Warn().Err(err).Msg("Recomputation failed, using hub results only")
		recomputedResults = nil
	}

	// 7. Apply graph-based ranking boost
	allResults := g.mergeAndRankWithGraph(hubResults, recomputedResults, queryEmb)

	// 8. Return top K
	if len(allResults) > limit {
		allResults = allResults[:limit]
	}

	duration := time.Since(startTime)
	log.Debug().
		Dur("duration_ms", duration).
		Int("hubs", len(hubResults)).
		Int("expanded", len(expandedIDs)).
		Int("recomputed", len(recomputedResults)).
		Int("results", len(allResults)).
		Msg("Graph search completed")

	return allResults, nil
}

// expandFromHubs traverses graph from hub nodes to find promising candidates
func (g *GraphSearchClient) expandFromHubs(hubResults []sqlitevec.QueryResult, maxCandidates int) []string {
	if g.graph == nil {
		return nil
	}

	expanded := make(map[string]float64) // doc_id -> relevance score
	visited := make(map[int64]bool)

	// Start from top hub results
	for i, result := range hubResults {
		if i >= g.graphConfig.BranchFactor*2 {
			break // Limit starting points
		}

		// Parse observation ID from doc_id
		obsID := parseObservationID(result.ID)
		if obsID == 0 {
			continue
		}

		// Mark as visited with high relevance (direct match)
		visited[obsID] = true
		expanded[result.ID] = result.Similarity

		// Traverse graph from this hub
		g.traverseGraph(obsID, result.Similarity, 0, expanded, visited)
	}

	// Convert to sorted list
	type candidate struct {
		ID        string
		Relevance float64
	}

	candidates := make([]candidate, 0, len(expanded))
	for id, rel := range expanded {
		candidates = append(candidates, candidate{ID: id, Relevance: rel})
	}

	// Sort by relevance descending
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Relevance > candidates[j].Relevance
	})

	// Return top candidates
	if len(candidates) > maxCandidates {
		candidates = candidates[:maxCandidates]
	}

	result := make([]string, len(candidates))
	for i, c := range candidates {
		result[i] = c.ID
	}

	return result
}

// traverseGraph performs depth-limited graph traversal
func (g *GraphSearchClient) traverseGraph(nodeID int64, baseRelevance float64, depth int, expanded map[string]float64, visited map[int64]bool) {
	if depth >= g.graphConfig.MaxHops {
		return // Max depth reached
	}

	// Get neighbors from graph
	neighbors, weights, err := g.graph.GetNeighbors(nodeID)
	if err != nil {
		return // No neighbors or error
	}

	// Traverse top neighbors by weight
	type neighborWeight struct {
		ID     int64
		Weight float32
	}

	neighborList := make([]neighborWeight, len(neighbors))
	for i := range neighbors {
		neighborList[i] = neighborWeight{
			ID:     neighbors[i],
			Weight: weights[i],
		}
	}

	// Sort by weight descending
	sort.Slice(neighborList, func(i, j int) bool {
		return neighborList[i].Weight > neighborList[j].Weight
	})

	// Expand top branch_factor neighbors
	expanded_count := 0
	for _, nw := range neighborList {
		if expanded_count >= g.graphConfig.BranchFactor {
			break
		}

		// Skip if edge weight too low
		if float64(nw.Weight) < g.graphConfig.EdgeWeight {
			continue
		}

		// Skip if already visited
		if visited[nw.ID] {
			continue
		}
		visited[nw.ID] = true

		// Calculate propagated relevance (decays with distance)
		decay := 0.7 // 30% decay per hop
		propagatedRelevance := baseRelevance * float64(nw.Weight) * decay

		// Add to expanded set
		docID := formatObservationDocID(nw.ID)
		if existing, ok := expanded[docID]; !ok || propagatedRelevance > existing {
			expanded[docID] = propagatedRelevance
		}

		// Recursively traverse
		g.traverseGraph(nw.ID, propagatedRelevance, depth+1, expanded, visited)
		expanded_count++
	}
}

// mergeAndRankWithGraph combines hub and recomputed results with graph-based ranking
func (g *GraphSearchClient) mergeAndRankWithGraph(hubResults, recomputedResults []sqlitevec.QueryResult, queryEmb []float32) []sqlitevec.QueryResult {
	// Merge results
	allResults := append(hubResults, recomputedResults...)

	// Apply graph-based re-ranking
	if g.graph != nil {
		for i := range allResults {
			obsID := parseObservationID(allResults[i].ID)
			if obsID == 0 {
				continue
			}

			// Boost score based on node degree (hubs are more important)
			node, err := g.graph.GetNode(obsID)
			if err == nil && node.Degree > 0 {
				// Degree boost: up to 10% increase for high-degree nodes
				degreeBoost := 1.0 + (0.1 * float64(node.Degree) / 20.0)
				if degreeBoost > 1.1 {
					degreeBoost = 1.1
				}
				allResults[i].Similarity *= degreeBoost
			}
		}
	}

	// Sort by adjusted similarity
	sortBySimilarity(allResults)

	return allResults
}

// parseObservationID extracts observation ID from doc_id
// Format: "obs-{id}-{field}"
func parseObservationID(docID string) int64 {
	var obsID int64
	// Ignore error - returns 0 on parse failure, which callers handle
	_, _ = fmt.Sscanf(docID, "obs-%d-", &obsID)
	return obsID
}

// formatObservationDocID creates a doc_id for an observation
func formatObservationDocID(obsID int64) string {
	return fmt.Sprintf("obs-%d-combined", obsID)
}

// GetGraphStats returns statistics about the observation graph
func (g *GraphSearchClient) GetGraphStats() graph.GraphStats {
	if g.graph == nil {
		return graph.GraphStats{}
	}
	return g.graph.Stats()
}

// RebuildGraph rebuilds the observation graph from current observations
// This should be called periodically or when observations change significantly
func (g *GraphSearchClient) RebuildGraph(ctx context.Context, observations []*models.Observation) error {
	log.Info().Int("observations", len(observations)).Msg("Rebuilding observation graph")

	newGraph, err := graph.BuildFromObservations(ctx, observations)
	if err != nil {
		return fmt.Errorf("build graph: %w", err)
	}

	g.graph = newGraph

	log.Info().
		Int("nodes", newGraph.Stats().NodeCount).
		Int("edges", newGraph.Stats().EdgeCount).
		Msg("Graph rebuilt successfully")

	return nil
}
