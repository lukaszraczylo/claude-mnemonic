package hybrid

import (
	"testing"

	"github.com/lukaszraczylo/claude-mnemonic/internal/vector/sqlitevec"
	_ "github.com/mattn/go-sqlite3" // Import SQLite driver for CGO linking
	"github.com/stretchr/testify/assert"
)

func TestParseStrategy(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected VectorStorageStrategy
	}{
		{"hub_strategy", "hub", StorageHub},
		{"on_demand_strategy", "on_demand", StorageOnDemand},
		{"always_strategy", "always", StorageAlways},
		{"invalid_defaults_to_hub", "invalid", StorageHub},
		{"empty_defaults_to_hub", "", StorageHub},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseStrategy(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestStrategyToString(t *testing.T) {
	tests := []struct {
		name     string
		expected string
		input    VectorStorageStrategy
	}{
		{"hub_to_string", "hub", StorageHub},
		{"on_demand_to_string", "on_demand", StorageOnDemand},
		{"always_to_string", "always", StorageAlways},
		{"invalid_to_unknown", "unknown", VectorStorageStrategy(99)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := strategyToString(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name     string
		a        []float32
		b        []float32
		expected float32
	}{
		{
			name:     "identical_vectors",
			a:        []float32{1, 0, 0},
			b:        []float32{1, 0, 0},
			expected: 1.0,
		},
		{
			name:     "orthogonal_vectors",
			a:        []float32{1, 0, 0},
			b:        []float32{0, 1, 0},
			expected: 0.0,
		},
		{
			name:     "opposite_vectors",
			a:        []float32{1, 0, 0},
			b:        []float32{-1, 0, 0},
			expected: -1.0,
		},
		{
			name:     "zero_vector",
			a:        []float32{0, 0, 0},
			b:        []float32{1, 1, 1},
			expected: 0.0,
		},
		{
			name:     "parallel_vectors",
			a:        []float32{2, 0, 0},
			b:        []float32{4, 0, 0},
			expected: 1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cosineSimilarity(tt.a, tt.b)
			assert.InDelta(t, tt.expected, result, 0.001)
		})
	}
}

func TestSortBySimilarity(t *testing.T) {
	tests := []struct {
		name     string
		input    []sqlitevec.QueryResult
		expected []string // Expected order of IDs
	}{
		{
			name: "already_sorted",
			input: []sqlitevec.QueryResult{
				{ID: "doc1", Similarity: 0.9},
				{ID: "doc2", Similarity: 0.7},
				{ID: "doc3", Similarity: 0.5},
			},
			expected: []string{"doc1", "doc2", "doc3"},
		},
		{
			name: "reverse_sorted",
			input: []sqlitevec.QueryResult{
				{ID: "doc1", Similarity: 0.3},
				{ID: "doc2", Similarity: 0.7},
				{ID: "doc3", Similarity: 0.9},
			},
			expected: []string{"doc3", "doc2", "doc1"},
		},
		{
			name: "random_order",
			input: []sqlitevec.QueryResult{
				{ID: "doc1", Similarity: 0.5},
				{ID: "doc2", Similarity: 0.9},
				{ID: "doc3", Similarity: 0.3},
				{ID: "doc4", Similarity: 0.7},
			},
			expected: []string{"doc2", "doc4", "doc1", "doc3"},
		},
		{
			name: "identical_similarities",
			input: []sqlitevec.QueryResult{
				{ID: "doc1", Similarity: 0.5},
				{ID: "doc2", Similarity: 0.5},
				{ID: "doc3", Similarity: 0.5},
			},
			expected: []string{"doc1", "doc2", "doc3"},
		},
		{
			name:     "empty_list",
			input:    []sqlitevec.QueryResult{},
			expected: []string{},
		},
		{
			name: "single_element",
			input: []sqlitevec.QueryResult{
				{ID: "doc1", Similarity: 0.5},
			},
			expected: []string{"doc1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sortBySimilarity(tt.input)

			actual := make([]string, len(tt.input))
			for i, r := range tt.input {
				actual[i] = r.ID
			}

			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestSortBySimilarity_PreserveOtherFields(t *testing.T) {
	input := []sqlitevec.QueryResult{
		{ID: "doc1", Similarity: 0.3, Distance: 0.7, Metadata: map[string]any{"key": "val1"}},
		{ID: "doc2", Similarity: 0.9, Distance: 0.1, Metadata: map[string]any{"key": "val2"}},
	}

	sortBySimilarity(input)

	assert.Equal(t, "doc2", input[0].ID)
	assert.InDelta(t, 0.9, input[0].Similarity, 0.001)
	assert.InDelta(t, 0.1, input[0].Distance, 0.001)
	assert.Equal(t, "val2", input[0].Metadata["key"])

	assert.Equal(t, "doc1", input[1].ID)
	assert.InDelta(t, 0.3, input[1].Similarity, 0.001)
	assert.InDelta(t, 0.7, input[1].Distance, 0.001)
	assert.Equal(t, "val1", input[1].Metadata["key"])
}
