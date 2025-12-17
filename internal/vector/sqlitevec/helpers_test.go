package sqlitevec

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDocTypes(t *testing.T) {
	assert.Equal(t, DocType("observation"), DocTypeObservation)
	assert.Equal(t, DocType("session_summary"), DocTypeSessionSummary)
	assert.Equal(t, DocType("user_prompt"), DocTypeUserPrompt)
}

func TestDocument_Fields(t *testing.T) {
	doc := Document{
		ID:      "doc-123",
		Content: "test content",
		Metadata: map[string]any{
			"key": "value",
		},
	}

	assert.Equal(t, "doc-123", doc.ID)
	assert.Equal(t, "test content", doc.Content)
	assert.Equal(t, "value", doc.Metadata["key"])
}

func TestQueryResult_Fields(t *testing.T) {
	result := QueryResult{
		ID:       "result-123",
		Distance: 0.5,
		Metadata: map[string]any{
			"sqlite_id": float64(42),
		},
	}

	assert.Equal(t, "result-123", result.ID)
	assert.Equal(t, 0.5, result.Distance)
	assert.Equal(t, float64(42), result.Metadata["sqlite_id"])
}

func TestBuildWhereFilter(t *testing.T) {
	tests := []struct {
		name     string
		docType  DocType
		project  string
		expected map[string]interface{}
	}{
		{
			name:     "empty_filters",
			docType:  "",
			project:  "",
			expected: map[string]interface{}{},
		},
		{
			name:    "doc_type_only",
			docType: DocTypeObservation,
			project: "",
			expected: map[string]interface{}{
				"doc_type": "observation",
			},
		},
		{
			name:    "project_only",
			docType: "",
			project: "my-project",
			expected: map[string]interface{}{
				"project": "my-project",
			},
		},
		{
			name:    "both_filters",
			docType: DocTypeSessionSummary,
			project: "test-project",
			expected: map[string]interface{}{
				"doc_type": "session_summary",
				"project":  "test-project",
			},
		},
		{
			name:    "user_prompt_type",
			docType: DocTypeUserPrompt,
			project: "prompt-project",
			expected: map[string]interface{}{
				"doc_type": "user_prompt",
				"project":  "prompt-project",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BuildWhereFilter(tt.docType, tt.project)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractIDsByDocType_Empty(t *testing.T) {
	results := []QueryResult{}
	ids := ExtractIDsByDocType(results)

	assert.Empty(t, ids.ObservationIDs)
	assert.Empty(t, ids.SummaryIDs)
	assert.Empty(t, ids.PromptIDs)
}

func TestExtractIDsByDocType_AllTypes(t *testing.T) {
	results := []QueryResult{
		{
			ID:       "obs-1",
			Distance: 0.1,
			Metadata: map[string]any{
				"sqlite_id": float64(1),
				"doc_type":  "observation",
			},
		},
		{
			ID:       "obs-2",
			Distance: 0.2,
			Metadata: map[string]any{
				"sqlite_id": float64(2),
				"doc_type":  "observation",
			},
		},
		{
			ID:       "summary-1",
			Distance: 0.3,
			Metadata: map[string]any{
				"sqlite_id": float64(10),
				"doc_type":  "session_summary",
			},
		},
		{
			ID:       "prompt-1",
			Distance: 0.4,
			Metadata: map[string]any{
				"sqlite_id": float64(20),
				"doc_type":  "user_prompt",
			},
		},
	}

	ids := ExtractIDsByDocType(results)

	assert.Equal(t, []int64{1, 2}, ids.ObservationIDs)
	assert.Equal(t, []int64{10}, ids.SummaryIDs)
	assert.Equal(t, []int64{20}, ids.PromptIDs)
}

func TestExtractIDsByDocType_Deduplication(t *testing.T) {
	results := []QueryResult{
		{
			ID:       "obs-1-field1",
			Distance: 0.1,
			Metadata: map[string]any{
				"sqlite_id": float64(1),
				"doc_type":  "observation",
			},
		},
		{
			ID:       "obs-1-field2",
			Distance: 0.2,
			Metadata: map[string]any{
				"sqlite_id": float64(1), // Same ID, different field
				"doc_type":  "observation",
			},
		},
		{
			ID:       "obs-2",
			Distance: 0.3,
			Metadata: map[string]any{
				"sqlite_id": float64(2),
				"doc_type":  "observation",
			},
		},
	}

	ids := ExtractIDsByDocType(results)

	assert.Equal(t, []int64{1, 2}, ids.ObservationIDs) // Should be deduplicated
}

func TestExtractIDsByDocType_Int64Fallback(t *testing.T) {
	results := []QueryResult{
		{
			ID:       "obs-1",
			Distance: 0.1,
			Metadata: map[string]any{
				"sqlite_id": int64(42), // int64 instead of float64
				"doc_type":  "observation",
			},
		},
	}

	ids := ExtractIDsByDocType(results)

	assert.Equal(t, []int64{42}, ids.ObservationIDs)
}

func TestExtractIDsByDocType_MissingSqliteID(t *testing.T) {
	results := []QueryResult{
		{
			ID:       "obs-1",
			Distance: 0.1,
			Metadata: map[string]any{
				"doc_type": "observation",
				// Missing sqlite_id
			},
		},
	}

	ids := ExtractIDsByDocType(results)

	assert.Empty(t, ids.ObservationIDs)
}

func TestExtractIDsByDocType_UnknownType(t *testing.T) {
	results := []QueryResult{
		{
			ID:       "unknown-1",
			Distance: 0.1,
			Metadata: map[string]any{
				"sqlite_id": float64(1),
				"doc_type":  "unknown_type",
			},
		},
	}

	ids := ExtractIDsByDocType(results)

	// Should not be added to any category
	assert.Empty(t, ids.ObservationIDs)
	assert.Empty(t, ids.SummaryIDs)
	assert.Empty(t, ids.PromptIDs)
}

func TestExtractObservationIDs_NoFilter(t *testing.T) {
	results := []QueryResult{
		{
			ID: "obs-1",
			Metadata: map[string]any{
				"sqlite_id": float64(1),
				"doc_type":  "observation",
				"project":   "proj-a",
			},
		},
		{
			ID: "obs-2",
			Metadata: map[string]any{
				"sqlite_id": float64(2),
				"doc_type":  "observation",
				"project":   "proj-b",
			},
		},
		{
			ID: "summary-1",
			Metadata: map[string]any{
				"sqlite_id": float64(10),
				"doc_type":  "session_summary",
				"project":   "proj-a",
			},
		},
	}

	ids := ExtractObservationIDs(results, "")

	assert.Equal(t, []int64{1, 2}, ids)
}

func TestExtractObservationIDs_WithProjectFilter(t *testing.T) {
	results := []QueryResult{
		{
			ID: "obs-1",
			Metadata: map[string]any{
				"sqlite_id": float64(1),
				"doc_type":  "observation",
				"project":   "proj-a",
				"scope":     "project",
			},
		},
		{
			ID: "obs-2",
			Metadata: map[string]any{
				"sqlite_id": float64(2),
				"doc_type":  "observation",
				"project":   "proj-b",
				"scope":     "project",
			},
		},
		{
			ID: "obs-global",
			Metadata: map[string]any{
				"sqlite_id": float64(3),
				"doc_type":  "observation",
				"project":   "proj-b",
				"scope":     "global",
			},
		},
	}

	ids := ExtractObservationIDs(results, "proj-a")

	// Should include proj-a and global scope observations
	assert.Equal(t, []int64{1, 3}, ids)
}

func TestExtractObservationIDs_Deduplication(t *testing.T) {
	results := []QueryResult{
		{
			ID: "obs-1-field1",
			Metadata: map[string]any{
				"sqlite_id": float64(1),
				"doc_type":  "observation",
			},
		},
		{
			ID: "obs-1-field2",
			Metadata: map[string]any{
				"sqlite_id": float64(1), // Same ID
				"doc_type":  "observation",
			},
		},
	}

	ids := ExtractObservationIDs(results, "")

	assert.Equal(t, []int64{1}, ids)
}

func TestExtractSummaryIDs_NoFilter(t *testing.T) {
	results := []QueryResult{
		{
			ID: "summary-1",
			Metadata: map[string]any{
				"sqlite_id": float64(10),
				"doc_type":  "session_summary",
				"project":   "proj-a",
			},
		},
		{
			ID: "summary-2",
			Metadata: map[string]any{
				"sqlite_id": float64(20),
				"doc_type":  "session_summary",
				"project":   "proj-b",
			},
		},
		{
			ID: "obs-1",
			Metadata: map[string]any{
				"sqlite_id": float64(1),
				"doc_type":  "observation",
			},
		},
	}

	ids := ExtractSummaryIDs(results, "")

	assert.Equal(t, []int64{10, 20}, ids)
}

func TestExtractSummaryIDs_WithProjectFilter(t *testing.T) {
	results := []QueryResult{
		{
			ID: "summary-1",
			Metadata: map[string]any{
				"sqlite_id": float64(10),
				"doc_type":  "session_summary",
				"project":   "proj-a",
			},
		},
		{
			ID: "summary-2",
			Metadata: map[string]any{
				"sqlite_id": float64(20),
				"doc_type":  "session_summary",
				"project":   "proj-b",
			},
		},
	}

	ids := ExtractSummaryIDs(results, "proj-a")

	assert.Equal(t, []int64{10}, ids)
}

func TestExtractPromptIDs_NoFilter(t *testing.T) {
	results := []QueryResult{
		{
			ID: "prompt-1",
			Metadata: map[string]any{
				"sqlite_id": float64(100),
				"doc_type":  "user_prompt",
				"project":   "proj-a",
			},
		},
		{
			ID: "prompt-2",
			Metadata: map[string]any{
				"sqlite_id": float64(200),
				"doc_type":  "user_prompt",
				"project":   "proj-b",
			},
		},
	}

	ids := ExtractPromptIDs(results, "")

	assert.Equal(t, []int64{100, 200}, ids)
}

func TestExtractPromptIDs_WithProjectFilter(t *testing.T) {
	results := []QueryResult{
		{
			ID: "prompt-1",
			Metadata: map[string]any{
				"sqlite_id": float64(100),
				"doc_type":  "user_prompt",
				"project":   "proj-a",
			},
		},
		{
			ID: "prompt-2",
			Metadata: map[string]any{
				"sqlite_id": float64(200),
				"doc_type":  "user_prompt",
				"project":   "proj-b",
			},
		},
	}

	ids := ExtractPromptIDs(results, "proj-b")

	assert.Equal(t, []int64{200}, ids)
}

func TestCopyMetadata(t *testing.T) {
	base := map[string]any{
		"key1": "value1",
		"key2": 42,
	}

	result := copyMetadata(base, "key3", "value3")

	// Original should be unchanged
	assert.Len(t, base, 2)

	// Result should have new key
	assert.Len(t, result, 3)
	assert.Equal(t, "value1", result["key1"])
	assert.Equal(t, 42, result["key2"])
	assert.Equal(t, "value3", result["key3"])
}

func TestCopyMetadataMulti(t *testing.T) {
	base := map[string]any{
		"key1": "value1",
	}
	extra := map[string]any{
		"key2": "value2",
		"key3": "value3",
	}

	result := copyMetadataMulti(base, extra)

	assert.Len(t, result, 3)
	assert.Equal(t, "value1", result["key1"])
	assert.Equal(t, "value2", result["key2"])
	assert.Equal(t, "value3", result["key3"])
}

func TestJoinStrings(t *testing.T) {
	tests := []struct {
		name     string
		strs     []string
		sep      string
		expected string
	}{
		{
			name:     "empty_slice",
			strs:     []string{},
			sep:      ", ",
			expected: "",
		},
		{
			name:     "single_element",
			strs:     []string{"one"},
			sep:      ", ",
			expected: "one",
		},
		{
			name:     "multiple_elements",
			strs:     []string{"one", "two", "three"},
			sep:      ", ",
			expected: "one, two, three",
		},
		{
			name:     "different_separator",
			strs:     []string{"a", "b", "c"},
			sep:      "-",
			expected: "a-b-c",
		},
		{
			name:     "empty_separator",
			strs:     []string{"a", "b", "c"},
			sep:      "",
			expected: "abc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := joinStrings(tt.strs, tt.sep)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{
			name:     "shorter_than_max",
			input:    "hello",
			maxLen:   10,
			expected: "hello",
		},
		{
			name:     "equal_to_max",
			input:    "hello",
			maxLen:   5,
			expected: "hello",
		},
		{
			name:     "longer_than_max",
			input:    "hello world",
			maxLen:   5,
			expected: "hello...",
		},
		{
			name:     "empty_string",
			input:    "",
			maxLen:   5,
			expected: "",
		},
		{
			name:     "zero_max_length",
			input:    "hello",
			maxLen:   0,
			expected: "...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateString(tt.input, tt.maxLen)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractedIDs_Empty(t *testing.T) {
	ids := &ExtractedIDs{}

	assert.Nil(t, ids.ObservationIDs)
	assert.Nil(t, ids.SummaryIDs)
	assert.Nil(t, ids.PromptIDs)
}
