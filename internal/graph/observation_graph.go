// Package graph provides observation relationship graphs for LEANN Phase 2.
//
// This package implements graph-based selective recomputation where observation
// relationships (file overlap, semantic similarity, temporal proximity) form a
// graph structure. Hub nodes (high-degree observations) store embeddings, while
// leaf nodes recompute on-demand.
package graph

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
	"github.com/rs/zerolog/log"
)

// RelationType defines the type of relationship between observations
type RelationType int

const (
	// RelationFileOverlap indicates observations reference overlapping files
	RelationFileOverlap RelationType = iota
	// RelationSemantic indicates high semantic similarity (cosine > 0.85)
	RelationSemantic
	// RelationTemporal indicates observations from same session
	RelationTemporal
	// RelationConcept indicates shared concept tags
	RelationConcept
)

// Edge represents a relationship between two observations
type Edge struct {
	FromID   int64
	ToID     int64
	Relation RelationType
	Weight   float32 // 0.0-1.0, higher = stronger relationship
}

// Node represents an observation in the graph
type Node struct {
	Metadata    NodeMetadata
	LastAccess  time.Time
	StoredEmb   []float32 // Nil if recomputed on-demand
	ID          int64
	Degree      int // Number of edges (hub detection)
	AccessCount int
}

// NodeMetadata contains observation metadata
type NodeMetadata struct {
	CreatedAt    time.Time
	Project      string
	Type         string
	Title        string
	IsSuperseded bool
}

// CSRGraph represents a graph in Compressed Sparse Row format for memory efficiency
type CSRGraph struct {
	RowPtr  []int32   // Node adjacency list pointers
	ColIdx  []int32   // Edge destination IDs
	Weights []float32 // Edge weights
	mu      sync.RWMutex
}

// ObservationGraph manages the observation relationship graph
type ObservationGraph struct {
	nodes   map[int64]*Node
	csr     *CSRGraph
	edges   []Edge
	nodesMu sync.RWMutex
	edgesMu sync.RWMutex
}

// NewObservationGraph creates a new empty observation graph
func NewObservationGraph() *ObservationGraph {
	return &ObservationGraph{
		nodes: make(map[int64]*Node),
		edges: make([]Edge, 0),
		csr:   &CSRGraph{},
	}
}

// AddNode adds or updates a node in the graph
func (g *ObservationGraph) AddNode(node *Node) {
	g.nodesMu.Lock()
	defer g.nodesMu.Unlock()

	g.nodes[node.ID] = node
}

// AddEdge adds an edge to the graph
func (g *ObservationGraph) AddEdge(edge Edge) {
	g.edgesMu.Lock()
	defer g.edgesMu.Unlock()

	g.edges = append(g.edges, edge)

	// Update degree counts
	g.nodesMu.Lock()
	if fromNode, ok := g.nodes[edge.FromID]; ok {
		fromNode.Degree++
	}
	if toNode, ok := g.nodes[edge.ToID]; ok {
		toNode.Degree++
	}
	g.nodesMu.Unlock()
}

// BuildCSR converts edge list to CSR format for efficient traversal
func (g *ObservationGraph) BuildCSR() error {
	g.edgesMu.RLock()
	g.nodesMu.RLock()
	defer g.edgesMu.RUnlock()
	defer g.nodesMu.RUnlock()

	if len(g.nodes) == 0 {
		return fmt.Errorf("no nodes in graph")
	}

	// Create node ID to index mapping
	nodeIDs := make([]int64, 0, len(g.nodes))
	for id := range g.nodes {
		nodeIDs = append(nodeIDs, id)
	}
	sort.Slice(nodeIDs, func(i, j int) bool {
		return nodeIDs[i] < nodeIDs[j]
	})

	idToIdx := make(map[int64]int32)
	for idx, id := range nodeIDs {
		// #nosec G115 - observation count will never exceed int32 max (2.1B) in practice
		idToIdx[id] = int32(idx)
	}

	// Count edges per node
	edgeCounts := make([]int, len(nodeIDs))
	for _, edge := range g.edges {
		if fromIdx, ok := idToIdx[edge.FromID]; ok {
			edgeCounts[fromIdx]++
		}
	}

	// Build row pointers
	rowPtr := make([]int32, len(nodeIDs)+1)
	rowPtr[0] = 0
	for i := 0; i < len(nodeIDs); i++ {
		// #nosec G115 - edge counts per node will not exceed int32 max
		rowPtr[i+1] = rowPtr[i] + int32(edgeCounts[i])
	}

	// Build column indices and weights
	totalEdges := rowPtr[len(nodeIDs)]
	colIdx := make([]int32, totalEdges)
	weights := make([]float32, totalEdges)

	// Temporary counter for filling CSR
	currentPos := make([]int32, len(nodeIDs))
	copy(currentPos, rowPtr[:len(nodeIDs)])

	for _, edge := range g.edges {
		fromIdx, fromOk := idToIdx[edge.FromID]
		toIdx, toOk := idToIdx[edge.ToID]

		if fromOk && toOk {
			pos := currentPos[fromIdx]
			colIdx[pos] = toIdx
			weights[pos] = edge.Weight
			currentPos[fromIdx]++
		}
	}

	g.csr.mu.Lock()
	g.csr.RowPtr = rowPtr
	g.csr.ColIdx = colIdx
	g.csr.Weights = weights
	g.csr.mu.Unlock()

	log.Info().
		Int("nodes", len(nodeIDs)).
		Int("edges", int(totalEdges)).
		Msg("Built CSR graph representation")

	return nil
}

// GetNeighbors returns neighboring nodes and their edge weights
func (g *ObservationGraph) GetNeighbors(nodeID int64) ([]int64, []float32, error) {
	g.csr.mu.RLock()
	defer g.csr.mu.RUnlock()

	// Find node index in CSR
	g.nodesMu.RLock()
	nodeIDs := make([]int64, 0, len(g.nodes))
	for id := range g.nodes {
		nodeIDs = append(nodeIDs, id)
	}
	g.nodesMu.RUnlock()

	sort.Slice(nodeIDs, func(i, j int) bool {
		return nodeIDs[i] < nodeIDs[j]
	})

	nodeIdx := sort.Search(len(nodeIDs), func(i int) bool {
		return nodeIDs[i] >= nodeID
	})

	if nodeIdx >= len(nodeIDs) || nodeIDs[nodeIdx] != nodeID {
		return nil, nil, fmt.Errorf("node %d not found", nodeID)
	}

	// Extract neighbors from CSR
	startIdx := g.csr.RowPtr[nodeIdx]
	endIdx := g.csr.RowPtr[nodeIdx+1]

	neighborCount := endIdx - startIdx
	neighbors := make([]int64, neighborCount)
	weights := make([]float32, neighborCount)

	for i := int32(0); i < neighborCount; i++ {
		neighborIdx := g.csr.ColIdx[startIdx+i]
		neighbors[i] = nodeIDs[neighborIdx]
		weights[i] = g.csr.Weights[startIdx+i]
	}

	return neighbors, weights, nil
}

// GetNode retrieves a node by ID
func (g *ObservationGraph) GetNode(nodeID int64) (*Node, error) {
	g.nodesMu.RLock()
	defer g.nodesMu.RUnlock()

	node, ok := g.nodes[nodeID]
	if !ok {
		return nil, fmt.Errorf("node %d not found", nodeID)
	}

	return node, nil
}

// FindHubs identifies hub nodes (high degree) in the graph
func (g *ObservationGraph) FindHubs(percentile float64) []int64 {
	g.nodesMu.RLock()
	defer g.nodesMu.RUnlock()

	if len(g.nodes) == 0 {
		return nil
	}

	// Collect all degrees
	degrees := make([]int, 0, len(g.nodes))
	nodeIDs := make([]int64, 0, len(g.nodes))

	for id, node := range g.nodes {
		degrees = append(degrees, node.Degree)
		nodeIDs = append(nodeIDs, id)
	}

	// Sort by degree
	type nodeDegree struct {
		ID     int64
		Degree int
	}

	nodeDegrees := make([]nodeDegree, len(nodeIDs))
	for i := range nodeIDs {
		nodeDegrees[i] = nodeDegree{
			ID:     nodeIDs[i],
			Degree: degrees[i],
		}
	}

	sort.Slice(nodeDegrees, func(i, j int) bool {
		return nodeDegrees[i].Degree > nodeDegrees[j].Degree
	})

	// Return top percentile
	cutoff := int(math.Ceil(float64(len(nodeDegrees)) * (1.0 - percentile)))
	if cutoff > len(nodeDegrees) {
		cutoff = len(nodeDegrees)
	}

	hubs := make([]int64, cutoff)
	for i := 0; i < cutoff; i++ {
		hubs[i] = nodeDegrees[i].ID
	}

	log.Info().
		Int("total_nodes", len(g.nodes)).
		Int("hubs", len(hubs)).
		Float64("percentile", percentile).
		Msg("Identified hub nodes")

	return hubs
}

// Stats returns graph statistics
func (g *ObservationGraph) Stats() GraphStats {
	g.nodesMu.RLock()
	g.edgesMu.RLock()
	defer g.nodesMu.RUnlock()
	defer g.edgesMu.RUnlock()

	stats := GraphStats{
		NodeCount: len(g.nodes),
		EdgeCount: len(g.edges),
	}

	if len(g.nodes) > 0 {
		degrees := make([]int, 0, len(g.nodes))
		for _, node := range g.nodes {
			degrees = append(degrees, node.Degree)
		}

		sort.Ints(degrees)
		stats.AvgDegree = float64(sum(degrees)) / float64(len(degrees))
		stats.MaxDegree = degrees[len(degrees)-1]
		stats.MinDegree = degrees[0]

		// Median
		mid := len(degrees) / 2
		if len(degrees)%2 == 0 {
			stats.MedianDegree = float64(degrees[mid-1]+degrees[mid]) / 2.0
		} else {
			stats.MedianDegree = float64(degrees[mid])
		}
	}

	// Count edge types
	stats.EdgeTypes = make(map[RelationType]int)
	for _, edge := range g.edges {
		stats.EdgeTypes[edge.Relation]++
	}

	return stats
}

// GraphStats contains graph statistics
type GraphStats struct {
	EdgeTypes    map[RelationType]int
	AvgDegree    float64
	MedianDegree float64
	NodeCount    int
	EdgeCount    int
	MaxDegree    int
	MinDegree    int
}

// BuildFromObservations constructs a graph from a list of observations
func BuildFromObservations(ctx context.Context, observations []*models.Observation) (*ObservationGraph, error) {
	graph := NewObservationGraph()

	// Add nodes
	for _, obs := range observations {
		// Extract title from sql.NullString
		title := ""
		if obs.Title.Valid {
			title = obs.Title.String
		}

		node := &Node{
			ID:     obs.ID,
			Degree: 0,
			Metadata: NodeMetadata{
				Project:      obs.Project,
				Type:         string(obs.Type),
				Title:        title,
				CreatedAt:    time.UnixMilli(obs.CreatedAtEpoch),
				IsSuperseded: obs.IsSuperseded,
			},
			LastAccess:  time.Now(),
			AccessCount: 0,
		}
		graph.AddNode(node)
	}

	// Detect edges (will be implemented in edge_detector.go)
	edges, err := DetectEdges(ctx, observations)
	if err != nil {
		return nil, fmt.Errorf("detect edges: %w", err)
	}

	for _, edge := range edges {
		graph.AddEdge(edge)
	}

	// Build CSR representation
	if err := graph.BuildCSR(); err != nil {
		return nil, fmt.Errorf("build CSR: %w", err)
	}

	return graph, nil
}

// Helper function to sum integers
func sum(values []int) int {
	total := 0
	for _, v := range values {
		total += v
	}
	return total
}

// String returns a human-readable representation of RelationType
func (r RelationType) String() string {
	switch r {
	case RelationFileOverlap:
		return "file_overlap"
	case RelationSemantic:
		return "semantic"
	case RelationTemporal:
		return "temporal"
	case RelationConcept:
		return "concept"
	default:
		return "unknown"
	}
}
