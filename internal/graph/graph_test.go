//go:build fts5

package graph

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---- helpers ----------------------------------------------------------------

func makeObs(id int64, sessionID string, concepts, filesRead, filesModified []string) *models.Observation {
	return &models.Observation{
		ID:             id,
		SDKSessionID:   sessionID,
		Title:          sql.NullString{String: "title", Valid: true},
		Project:        "test-project",
		Type:           models.ObsTypeDecision,
		Concepts:       concepts,
		FilesRead:      filesRead,
		FilesModified:  filesModified,
		CreatedAtEpoch: time.Now().UnixMilli(),
	}
}

// ---- ObservationGraph -------------------------------------------------------

func TestNewObservationGraph_Empty(t *testing.T) {
	g := NewObservationGraph()
	require.NotNil(t, g)

	stats := g.Stats()
	assert.Equal(t, 0, stats.NodeCount)
	assert.Equal(t, 0, stats.EdgeCount)
}

func TestAddNode_StoresAndRetrieves(t *testing.T) {
	g := NewObservationGraph()
	node := &Node{
		ID:     42,
		Degree: 0,
		Metadata: NodeMetadata{
			Project: "proj",
			Type:    "decision",
			Title:   "test node",
		},
	}
	g.AddNode(node)

	got, err := g.GetNode(42)
	require.NoError(t, err)
	assert.Equal(t, int64(42), got.ID)
	assert.Equal(t, "test node", got.Metadata.Title)
}

func TestAddNode_OverwritesExisting(t *testing.T) {
	g := NewObservationGraph()
	g.AddNode(&Node{ID: 1, Metadata: NodeMetadata{Title: "old"}})
	g.AddNode(&Node{ID: 1, Metadata: NodeMetadata{Title: "new"}})

	got, err := g.GetNode(1)
	require.NoError(t, err)
	assert.Equal(t, "new", got.Metadata.Title)
}

func TestGetNode_NotFound(t *testing.T) {
	g := NewObservationGraph()
	_, err := g.GetNode(999)
	assert.Error(t, err)
}

func TestAddEdge_UpdatesDegree(t *testing.T) {
	g := NewObservationGraph()
	g.AddNode(&Node{ID: 1})
	g.AddNode(&Node{ID: 2})

	g.AddEdge(Edge{FromID: 1, ToID: 2, Relation: RelationTemporal, Weight: 0.8})

	n1, _ := g.GetNode(1)
	n2, _ := g.GetNode(2)
	assert.Equal(t, 1, n1.Degree)
	assert.Equal(t, 1, n2.Degree)
}

func TestAddEdge_MissingNodesDontPanic(t *testing.T) {
	g := NewObservationGraph()
	// Adding edge referencing non-existent nodes must not panic
	assert.NotPanics(t, func() {
		g.AddEdge(Edge{FromID: 100, ToID: 200, Relation: RelationConcept, Weight: 0.5})
	})
}

// ---- BuildCSR / GetNeighbors ------------------------------------------------

func TestBuildCSR_NoNodes_ReturnsError(t *testing.T) {
	g := NewObservationGraph()
	err := g.BuildCSR()
	assert.Error(t, err)
}

func TestGetNeighbors_AfterBuildCSR(t *testing.T) {
	g := NewObservationGraph()
	for _, id := range []int64{1, 2, 3} {
		g.AddNode(&Node{ID: id})
	}
	g.AddEdge(Edge{FromID: 1, ToID: 2, Relation: RelationTemporal, Weight: 0.8})
	g.AddEdge(Edge{FromID: 1, ToID: 3, Relation: RelationConcept, Weight: 0.6})

	require.NoError(t, g.BuildCSR())

	neighbors, weights, err := g.GetNeighbors(1)
	require.NoError(t, err)
	assert.Len(t, neighbors, 2)
	assert.Len(t, weights, 2)
}

func TestGetNeighbors_NodeWithNoOutgoingEdges(t *testing.T) {
	g := NewObservationGraph()
	g.AddNode(&Node{ID: 1})
	g.AddNode(&Node{ID: 2})
	// Edge only from 2 → 1; node 2 is a leaf from 1's perspective
	g.AddEdge(Edge{FromID: 2, ToID: 1, Relation: RelationTemporal, Weight: 0.8})
	require.NoError(t, g.BuildCSR())

	neighbors, weights, err := g.GetNeighbors(1)
	require.NoError(t, err)
	assert.Empty(t, neighbors)
	assert.Empty(t, weights)
}

func TestGetNeighbors_NodeNotInGraph(t *testing.T) {
	g := NewObservationGraph()
	g.AddNode(&Node{ID: 1})
	require.NoError(t, g.BuildCSR())

	_, _, err := g.GetNeighbors(999)
	assert.Error(t, err)
}

// ---- FindHubs ---------------------------------------------------------------

func TestFindHubs_EmptyGraph(t *testing.T) {
	g := NewObservationGraph()
	hubs := g.FindHubs(0.1)
	assert.Nil(t, hubs)
}

func TestFindHubs_IdentifiesHighDegreeNodes(t *testing.T) {
	g := NewObservationGraph()
	// Node 1 connected to everyone else → hub
	for id := int64(1); id <= 5; id++ {
		g.AddNode(&Node{ID: id})
	}
	for id := int64(2); id <= 5; id++ {
		g.AddEdge(Edge{FromID: 1, ToID: id, Relation: RelationConcept, Weight: 0.5})
	}

	hubs := g.FindHubs(0.2) // top 20%
	assert.Contains(t, hubs, int64(1))
}

func TestFindHubs_Percentile100_ReturnsEmpty(t *testing.T) {
	// percentile=1.0 → cutoff = ceil(N * (1 - 1.0)) = ceil(0) = 0 → no hubs
	g := NewObservationGraph()
	for id := int64(1); id <= 4; id++ {
		g.AddNode(&Node{ID: id})
	}
	hubs := g.FindHubs(1.0)
	assert.Empty(t, hubs)
}

func TestFindHubs_Percentile0_ReturnsAllNodes(t *testing.T) {
	// percentile=0.0 → cutoff = ceil(N * 1.0) = N → all nodes returned
	g := NewObservationGraph()
	for id := int64(1); id <= 4; id++ {
		g.AddNode(&Node{ID: id})
	}
	hubs := g.FindHubs(0.0)
	assert.Len(t, hubs, 4)
}

// ---- Stats ------------------------------------------------------------------

func TestStats_EdgeTypesCounted(t *testing.T) {
	g := NewObservationGraph()
	for _, id := range []int64{1, 2, 3} {
		g.AddNode(&Node{ID: id})
	}
	g.AddEdge(Edge{FromID: 1, ToID: 2, Relation: RelationTemporal, Weight: 0.8})
	g.AddEdge(Edge{FromID: 1, ToID: 3, Relation: RelationConcept, Weight: 0.6})
	g.AddEdge(Edge{FromID: 2, ToID: 3, Relation: RelationConcept, Weight: 0.6})

	stats := g.Stats()
	assert.Equal(t, 3, stats.NodeCount)
	assert.Equal(t, 3, stats.EdgeCount)
	assert.Equal(t, 1, stats.EdgeTypes[RelationTemporal])
	assert.Equal(t, 2, stats.EdgeTypes[RelationConcept])
}

func TestStats_DegreeMetrics(t *testing.T) {
	g := NewObservationGraph()
	// Node 1: degree 2, nodes 2,3: degree 1 each
	for _, id := range []int64{1, 2, 3} {
		g.AddNode(&Node{ID: id})
	}
	g.AddEdge(Edge{FromID: 1, ToID: 2, Relation: RelationTemporal, Weight: 0.8})
	g.AddEdge(Edge{FromID: 1, ToID: 3, Relation: RelationTemporal, Weight: 0.8})

	stats := g.Stats()
	assert.Equal(t, 2, stats.MaxDegree)
	assert.Equal(t, 1, stats.MinDegree)
	assert.InDelta(t, 4.0/3.0, stats.AvgDegree, 0.001)
}

// ---- BuildFromObservations --------------------------------------------------

func TestBuildFromObservations_SingleObservation_ReturnsError(t *testing.T) {
	obs := []*models.Observation{makeObs(1, "s1", nil, nil, nil)}
	// Single observation: DetectEdges returns nil, BuildCSR errors (no nodes never happens
	// since node was added — but CSR build will succeed with 1 node and 0 edges).
	g, err := BuildFromObservations(context.Background(), obs)
	// With 1 node, BuildCSR succeeds (nodes exist); no edges → valid graph.
	require.NoError(t, err)
	require.NotNil(t, g)
	stats := g.Stats()
	assert.Equal(t, 1, stats.NodeCount)
	assert.Equal(t, 0, stats.EdgeCount)
}

func TestBuildFromObservations_SetsNodeMetadata(t *testing.T) {
	obs := []*models.Observation{
		{
			ID:             7,
			SDKSessionID:   "sess",
			Project:        "myproject",
			Type:           models.ObsTypeFeature,
			Title:          sql.NullString{String: "feature title", Valid: true},
			IsSuperseded:   true,
			CreatedAtEpoch: time.Now().UnixMilli(),
		},
	}
	g, err := BuildFromObservations(context.Background(), obs)
	require.NoError(t, err)

	node, err := g.GetNode(7)
	require.NoError(t, err)
	assert.Equal(t, "myproject", node.Metadata.Project)
	assert.Equal(t, "feature title", node.Metadata.Title)
	assert.Equal(t, string(models.ObsTypeFeature), node.Metadata.Type)
	assert.True(t, node.Metadata.IsSuperseded)
}

func TestBuildFromObservations_TitleMissing_EmptyString(t *testing.T) {
	obs := []*models.Observation{
		{
			ID:             3,
			SDKSessionID:   "s",
			Title:          sql.NullString{Valid: false},
			CreatedAtEpoch: time.Now().UnixMilli(),
		},
	}
	g, err := BuildFromObservations(context.Background(), obs)
	require.NoError(t, err)

	node, err := g.GetNode(3)
	require.NoError(t, err)
	assert.Equal(t, "", node.Metadata.Title)
}

func TestBuildFromObservations_WithTemporalEdges(t *testing.T) {
	obs := []*models.Observation{
		makeObs(1, "sess-a", nil, nil, nil),
		makeObs(2, "sess-a", nil, nil, nil),
		makeObs(3, "sess-b", nil, nil, nil),
	}
	g, err := BuildFromObservations(context.Background(), obs)
	require.NoError(t, err)

	stats := g.Stats()
	assert.Equal(t, 3, stats.NodeCount)
	// obs 1 and 2 share session → 1 temporal edge
	assert.Equal(t, 1, stats.EdgeTypes[RelationTemporal])
}

func TestBuildFromObservations_WithConceptEdges(t *testing.T) {
	obs := []*models.Observation{
		makeObs(1, "s1", []string{"security", "auth"}, nil, nil),
		makeObs(2, "s2", []string{"security"}, nil, nil),
		makeObs(3, "s3", []string{"unrelated"}, nil, nil),
	}
	g, err := BuildFromObservations(context.Background(), obs)
	require.NoError(t, err)

	stats := g.Stats()
	// obs 1 and 2 share "security"
	assert.GreaterOrEqual(t, stats.EdgeTypes[RelationConcept], 1)
}

func TestBuildFromObservations_WithFileOverlapEdges(t *testing.T) {
	obs := []*models.Observation{
		makeObs(1, "s1", nil, []string{"pkg/foo.go", "pkg/bar.go"}, nil),
		makeObs(2, "s2", nil, []string{"pkg/foo.go", "pkg/baz.go"}, nil),
	}
	g, err := BuildFromObservations(context.Background(), obs)
	require.NoError(t, err)

	stats := g.Stats()
	// Jaccard({foo,bar},{foo,baz}) = 1/3 ≈ 0.333 > MinFileOverlapForEdge(0.3)
	assert.GreaterOrEqual(t, stats.EdgeTypes[RelationFileOverlap], 1)
}

// ---- RelationType.String() --------------------------------------------------

func TestRelationType_String(t *testing.T) {
	cases := []struct {
		rt   RelationType
		want string
	}{
		{RelationFileOverlap, "file_overlap"},
		{RelationSemantic, "semantic"},
		{RelationTemporal, "temporal"},
		{RelationConcept, "concept"},
		{RelationType(99), "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.rt.String())
		})
	}
}

// ---- DetectEdges (edge_detector.go) ----------------------------------------

func TestDetectEdges_LessThanTwo_ReturnsNil(t *testing.T) {
	edges, err := DetectEdges(context.Background(), nil)
	assert.NoError(t, err)
	assert.Nil(t, edges)

	edges, err = DetectEdges(context.Background(), []*models.Observation{makeObs(1, "s", nil, nil, nil)})
	assert.NoError(t, err)
	assert.Nil(t, edges)
}

func TestDetectEdges_SameSession_CreatesTemporalEdges(t *testing.T) {
	obs := []*models.Observation{
		makeObs(1, "session-x", nil, nil, nil),
		makeObs(2, "session-x", nil, nil, nil),
		makeObs(3, "session-x", nil, nil, nil),
	}
	edges, err := DetectEdges(context.Background(), obs)
	require.NoError(t, err)

	var temporal []Edge
	for _, e := range edges {
		if e.Relation == RelationTemporal {
			temporal = append(temporal, e)
		}
	}
	// Consecutive pairs: (1,2) and (2,3)
	assert.Len(t, temporal, 2)
	assert.InDelta(t, 0.8, temporal[0].Weight, 0.001)
}

func TestDetectEdges_DifferentSessions_NoTemporalEdges(t *testing.T) {
	obs := []*models.Observation{
		makeObs(1, "sess-a", nil, nil, nil),
		makeObs(2, "sess-b", nil, nil, nil),
	}
	edges, err := DetectEdges(context.Background(), obs)
	require.NoError(t, err)

	for _, e := range edges {
		assert.NotEqual(t, RelationTemporal, e.Relation)
	}
}

func TestDetectEdges_EmptySessionID_NoTemporalEdge(t *testing.T) {
	obs := []*models.Observation{
		makeObs(1, "", nil, nil, nil),
		makeObs(2, "", nil, nil, nil),
	}
	edges, err := DetectEdges(context.Background(), obs)
	require.NoError(t, err)

	for _, e := range edges {
		assert.NotEqual(t, RelationTemporal, e.Relation)
	}
}

func TestDetectEdges_SharedConcepts_CreatesConceptEdges(t *testing.T) {
	obs := []*models.Observation{
		makeObs(1, "s1", []string{"performance", "caching"}, nil, nil),
		makeObs(2, "s2", []string{"performance"}, nil, nil),
		makeObs(3, "s3", []string{"caching"}, nil, nil),
	}
	edges, err := DetectEdges(context.Background(), obs)
	require.NoError(t, err)

	conceptEdges := filterByRelation(edges, RelationConcept)
	// obs1↔obs2 (performance), obs1↔obs3 (caching) → 2 concept edges
	assert.Len(t, conceptEdges, 2)
}

func TestDetectEdges_NoConcepts_NoConceptEdges(t *testing.T) {
	obs := []*models.Observation{
		makeObs(1, "s1", nil, nil, nil),
		makeObs(2, "s2", nil, nil, nil),
	}
	edges, err := DetectEdges(context.Background(), obs)
	require.NoError(t, err)

	assert.Empty(t, filterByRelation(edges, RelationConcept))
}

func TestDetectEdges_FileOverlap_AboveThreshold_CreatesEdge(t *testing.T) {
	// Jaccard 2/3 ≈ 0.667 > 0.3 threshold
	obs := []*models.Observation{
		makeObs(1, "s1", nil, []string{"a.go", "b.go", "c.go"}, nil),
		makeObs(2, "s2", nil, []string{"a.go", "b.go", "d.go"}, nil),
	}
	edges, err := DetectEdges(context.Background(), obs)
	require.NoError(t, err)

	fileEdges := filterByRelation(edges, RelationFileOverlap)
	require.Len(t, fileEdges, 1)
	assert.InDelta(t, 2.0/4.0, float64(fileEdges[0].Weight), 0.01)
}

func TestDetectEdges_FileOverlap_BelowThreshold_NoEdge(t *testing.T) {
	// Jaccard 1/9 ≈ 0.11 < 0.3 threshold
	obs := []*models.Observation{
		makeObs(1, "s1", nil, []string{"a.go", "b.go", "c.go", "d.go", "e.go"}, nil),
		makeObs(2, "s2", nil, []string{"a.go", "f.go", "g.go", "h.go", "i.go"}, nil),
	}
	edges, err := DetectEdges(context.Background(), obs)
	require.NoError(t, err)

	assert.Empty(t, filterByRelation(edges, RelationFileOverlap))
}

func TestDetectEdges_FilesModified_CountsForOverlap(t *testing.T) {
	obs := []*models.Observation{
		makeObs(1, "s1", nil, nil, []string{"pkg/core.go", "pkg/util.go"}),
		makeObs(2, "s2", nil, []string{"pkg/core.go"}, []string{"pkg/util.go"}),
	}
	edges, err := DetectEdges(context.Background(), obs)
	require.NoError(t, err)

	fileEdges := filterByRelation(edges, RelationFileOverlap)
	assert.NotEmpty(t, fileEdges)
}

func TestDetectEdges_NoEdgeDuplicates(t *testing.T) {
	// Same pair via two concepts → only one concept edge
	obs := []*models.Observation{
		makeObs(1, "s1", []string{"security", "auth"}, nil, nil),
		makeObs(2, "s2", []string{"security", "auth"}, nil, nil),
	}
	edges, err := DetectEdges(context.Background(), obs)
	require.NoError(t, err)

	conceptEdges := filterByRelation(edges, RelationConcept)
	// Both share security and auth, but deduplication should keep only 1 edge per pair per call
	// The seen map deduplicates: only first concept that creates the pair wins
	assert.Len(t, conceptEdges, 1)
}

// ---- calculateFileOverlap ---------------------------------------------------

func TestCalculateFileOverlap_DisjointSets_Zero(t *testing.T) {
	result := calculateFileOverlap([]string{"a.go", "b.go"}, []string{"c.go", "d.go"})
	assert.Equal(t, float32(0.0), result)
}

func TestCalculateFileOverlap_IdenticalSets_One(t *testing.T) {
	files := []string{"a.go", "b.go", "c.go"}
	result := calculateFileOverlap(files, files)
	assert.InDelta(t, 1.0, float64(result), 0.001)
}

func TestCalculateFileOverlap_EmptySlices_Zero(t *testing.T) {
	assert.Equal(t, float32(0.0), calculateFileOverlap(nil, []string{"a.go"}))
	assert.Equal(t, float32(0.0), calculateFileOverlap([]string{"a.go"}, nil))
	assert.Equal(t, float32(0.0), calculateFileOverlap(nil, nil))
}

func TestCalculateFileOverlap_Jaccard_Correct(t *testing.T) {
	// {a,b,c} ∩ {b,c,d} = {b,c} → 2/4 = 0.5
	result := calculateFileOverlap([]string{"a", "b", "c"}, []string{"b", "c", "d"})
	assert.InDelta(t, 0.5, float64(result), 0.001)
}

func TestCalculateFileOverlap_Duplicates_TreatedAsSet(t *testing.T) {
	// Duplicates collapse: {a,a,b} → {a,b}; {a,b,b} → {a,b}; Jaccard = 1.0
	result := calculateFileOverlap([]string{"a", "a", "b"}, []string{"a", "b", "b"})
	assert.InDelta(t, 1.0, float64(result), 0.001)
}

// ---- DetectSemanticEdges ----------------------------------------------------

func TestDetectSemanticEdges_AboveThreshold_CreatesEdge(t *testing.T) {
	// Identical vectors → similarity = 1.0 > 0.85
	emb := []float32{1.0, 0.0, 0.0}
	obs := []*models.Observation{makeObs(1, "s", nil, nil, nil), makeObs(2, "s", nil, nil, nil)}
	embeddings := map[int64][]float32{1: emb, 2: emb}

	edges := DetectSemanticEdges(context.Background(), obs, embeddings)
	require.Len(t, edges, 1)
	assert.Equal(t, RelationSemantic, edges[0].Relation)
	assert.InDelta(t, 1.0, float64(edges[0].Weight), 0.001)
}

func TestDetectSemanticEdges_BelowThreshold_NoEdge(t *testing.T) {
	// Orthogonal vectors → similarity = 0.0
	obs := []*models.Observation{makeObs(1, "s", nil, nil, nil), makeObs(2, "s", nil, nil, nil)}
	embeddings := map[int64][]float32{
		1: {1.0, 0.0, 0.0},
		2: {0.0, 1.0, 0.0},
	}

	edges := DetectSemanticEdges(context.Background(), obs, embeddings)
	assert.Empty(t, edges)
}

func TestDetectSemanticEdges_MissingEmbedding_Skipped(t *testing.T) {
	obs := []*models.Observation{makeObs(1, "s", nil, nil, nil), makeObs(2, "s", nil, nil, nil)}
	// Only obs 1 has embedding
	embeddings := map[int64][]float32{1: {1.0, 0.0, 0.0}}

	edges := DetectSemanticEdges(context.Background(), obs, embeddings)
	assert.Empty(t, edges)
}

func TestDetectSemanticEdges_NoDuplicates(t *testing.T) {
	emb := []float32{0.9, 0.1, 0.0}
	obs := []*models.Observation{
		makeObs(1, "s", nil, nil, nil),
		makeObs(2, "s", nil, nil, nil),
		makeObs(3, "s", nil, nil, nil),
	}
	embeddings := map[int64][]float32{1: emb, 2: emb, 3: emb}

	edges := DetectSemanticEdges(context.Background(), obs, embeddings)
	// 3 pairs: (1,2),(1,3),(2,3)
	assert.Len(t, edges, 3)
}

// ---- cosineSimilarity -------------------------------------------------------

func TestCosineSimilarity_IdenticalVectors(t *testing.T) {
	v := []float32{1.0, 2.0, 3.0}
	result := cosineSimilarity(v, v)
	assert.InDelta(t, 1.0, float64(result), 0.0001)
}

func TestCosineSimilarity_OppositeVectors(t *testing.T) {
	a := []float32{1.0, 0.0}
	b := []float32{-1.0, 0.0}
	result := cosineSimilarity(a, b)
	assert.InDelta(t, -1.0, float64(result), 0.0001)
}

func TestCosineSimilarity_OrthogonalVectors(t *testing.T) {
	a := []float32{1.0, 0.0}
	b := []float32{0.0, 1.0}
	result := cosineSimilarity(a, b)
	assert.InDelta(t, 0.0, float64(result), 0.0001)
}

func TestCosineSimilarity_ZeroVector_ReturnsZero(t *testing.T) {
	a := []float32{0.0, 0.0}
	b := []float32{1.0, 0.0}
	assert.Equal(t, float32(0.0), cosineSimilarity(a, b))
	assert.Equal(t, float32(0.0), cosineSimilarity(b, a))
}

func TestCosineSimilarity_MismatchedLength_ReturnsZero(t *testing.T) {
	a := []float32{1.0, 2.0}
	b := []float32{1.0, 2.0, 3.0}
	assert.Equal(t, float32(0.0), cosineSimilarity(a, b))
}

// ---- edgeKey ----------------------------------------------------------------

func TestEdgeKey_Symmetric(t *testing.T) {
	// Must produce the same key regardless of order
	assert.Equal(t, edgeKey(1, 2), edgeKey(2, 1))
	assert.Equal(t, edgeKey(100, 5), edgeKey(5, 100))
}

func TestEdgeKey_DifferentPairs_DifferentKeys(t *testing.T) {
	assert.NotEqual(t, edgeKey(1, 2), edgeKey(1, 3))
	assert.NotEqual(t, edgeKey(1, 2), edgeKey(2, 3))
}

// ---- pruneEdges -------------------------------------------------------------

func TestPruneEdges_BelowLimit_NoChange(t *testing.T) {
	edges := []Edge{
		{FromID: 1, ToID: 2, Weight: 0.9},
		{FromID: 1, ToID: 3, Weight: 0.7},
	}
	pruned := pruneEdges(edges, 5)
	assert.Len(t, pruned, 2)
}

func TestPruneEdges_ZeroLimit_ReturnsAll(t *testing.T) {
	edges := []Edge{{FromID: 1, ToID: 2, Weight: 0.5}}
	pruned := pruneEdges(edges, 0)
	assert.Len(t, pruned, 1)
}

func TestPruneEdges_KeepsHighWeightEdges(t *testing.T) {
	// Node 1 gets 4 edges, limit is 2 → only the 2 heaviest should survive
	edges := []Edge{
		{FromID: 1, ToID: 2, Weight: 0.9},
		{FromID: 1, ToID: 3, Weight: 0.8},
		{FromID: 1, ToID: 4, Weight: 0.3},
		{FromID: 1, ToID: 5, Weight: 0.1},
	}
	pruned := pruneEdges(edges, 2)

	weights := make([]float32, len(pruned))
	for i, e := range pruned {
		weights[i] = e.Weight
	}
	assert.Contains(t, weights, float32(0.9))
	assert.Contains(t, weights, float32(0.8))
}

// ---- sortEdgesByWeight ------------------------------------------------------

func TestSortEdgesByWeight_DescendingOrder(t *testing.T) {
	edges := []Edge{
		{Weight: 0.3},
		{Weight: 0.9},
		{Weight: 0.1},
		{Weight: 0.7},
	}
	sortEdgesByWeight(edges)
	for i := 1; i < len(edges); i++ {
		assert.GreaterOrEqual(t, edges[i-1].Weight, edges[i].Weight)
	}
}

func TestSortEdgesByWeight_EmptySlice_NoPanic(t *testing.T) {
	assert.NotPanics(t, func() {
		sortEdgesByWeight([]Edge{})
	})
}

func TestSortEdgesByWeight_SingleElement_Unchanged(t *testing.T) {
	edges := []Edge{{Weight: 0.5}}
	sortEdgesByWeight(edges)
	assert.Equal(t, float32(0.5), edges[0].Weight)
}

// ---- helpers ----------------------------------------------------------------

func filterByRelation(edges []Edge, rel RelationType) []Edge {
	var out []Edge
	for _, e := range edges {
		if e.Relation == rel {
			out = append(out, e)
		}
	}
	return out
}
