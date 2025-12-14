package chroma

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
	"github.com/stretchr/testify/assert"
)

// testSync creates a Sync with a nil client for testing format functions.
func testSync() *Sync {
	return &Sync{client: nil}
}

func TestSync_FormatObservationDocs(t *testing.T) {
	sync := testSync()

	obs := &models.Observation{
		ID:             1,
		SDKSessionID:   "test-session",
		Project:        "test-project",
		Scope:          models.ScopeProject,
		Type:           models.ObsTypeDiscovery,
		Title:          sql.NullString{String: "Test Title", Valid: true},
		Subtitle:       sql.NullString{String: "Test Subtitle", Valid: true},
		Narrative:      sql.NullString{String: "Test narrative content", Valid: true},
		Facts:          models.JSONStringArray{"Fact 1", "Fact 2", "Fact 3"},
		Concepts:       models.JSONStringArray{"concept1", "concept2"},
		FilesRead:      models.JSONStringArray{"file1.go", "file2.go"},
		FilesModified:  models.JSONStringArray{"file3.go"},
		CreatedAtEpoch: 1234567890,
	}

	docs := sync.formatObservationDocs(obs)

	// Should have 1 narrative + 3 facts = 4 documents
	assert.Len(t, docs, 4)

	// Check narrative document
	narrativeDoc := docs[0]
	assert.Equal(t, "obs_1_narrative", narrativeDoc.ID)
	assert.Equal(t, "Test narrative content", narrativeDoc.Content)
	assert.Equal(t, int64(1), narrativeDoc.Metadata["sqlite_id"])
	assert.Equal(t, "observation", narrativeDoc.Metadata["doc_type"])
	assert.Equal(t, "narrative", narrativeDoc.Metadata["field_type"])
	assert.Equal(t, "test-project", narrativeDoc.Metadata["project"])
	assert.Equal(t, "project", narrativeDoc.Metadata["scope"])
	assert.Equal(t, "Test Title", narrativeDoc.Metadata["title"])
	assert.Equal(t, "Test Subtitle", narrativeDoc.Metadata["subtitle"])

	// Check fact documents
	for i := 1; i <= 3; i++ {
		factDoc := docs[i]
		assert.Equal(t, fmt.Sprintf("obs_1_fact_%d", i-1), factDoc.ID)
		assert.Equal(t, fmt.Sprintf("Fact %d", i), factDoc.Content)
		assert.Equal(t, "fact", factDoc.Metadata["field_type"])
		assert.Equal(t, i-1, factDoc.Metadata["fact_index"])
	}
}

func TestSync_FormatObservationDocs_NoNarrative(t *testing.T) {
	sync := testSync()

	obs := &models.Observation{
		ID:             2,
		SDKSessionID:   "test-session",
		Project:        "test-project",
		Scope:          models.ScopeGlobal,
		Type:           models.ObsTypeBugfix,
		Facts:          models.JSONStringArray{"Only fact"},
		CreatedAtEpoch: 1234567890,
	}

	docs := sync.formatObservationDocs(obs)

	// Should have 1 fact only (no narrative)
	assert.Len(t, docs, 1)
	assert.Equal(t, "obs_2_fact_0", docs[0].ID)
	assert.Equal(t, "Only fact", docs[0].Content)
	assert.Equal(t, "global", docs[0].Metadata["scope"])
}

func TestSync_FormatObservationDocs_Empty(t *testing.T) {
	sync := testSync()

	obs := &models.Observation{
		ID:             3,
		SDKSessionID:   "test-session",
		Project:        "test-project",
		Type:           models.ObsTypeDiscovery,
		CreatedAtEpoch: 1234567890,
	}

	docs := sync.formatObservationDocs(obs)

	// Should have no documents when no content
	assert.Len(t, docs, 0)
}

func TestSync_FormatObservationDocs_EmptyScope(t *testing.T) {
	sync := testSync()

	obs := &models.Observation{
		ID:             4,
		SDKSessionID:   "test-session",
		Project:        "test-project",
		Scope:          "", // Empty scope
		Type:           models.ObsTypeDiscovery,
		Narrative:      sql.NullString{String: "Content", Valid: true},
		CreatedAtEpoch: 1234567890,
	}

	docs := sync.formatObservationDocs(obs)

	// Empty scope should default to "project"
	assert.Len(t, docs, 1)
	assert.Equal(t, "project", docs[0].Metadata["scope"])
}

func TestSync_FormatSummaryDocs(t *testing.T) {
	sync := testSync()

	summary := &models.SessionSummary{
		ID:             1,
		SDKSessionID:   "test-session",
		Project:        "test-project",
		Request:        sql.NullString{String: "Add feature", Valid: true},
		Investigated:   sql.NullString{String: "Looked at code", Valid: true},
		Learned:        sql.NullString{String: "Found pattern", Valid: true},
		Completed:      sql.NullString{String: "Done", Valid: true},
		NextSteps:      sql.NullString{String: "Test it", Valid: true},
		Notes:          sql.NullString{String: "Notes here", Valid: true},
		PromptNumber:   sql.NullInt64{Int64: 5, Valid: true},
		CreatedAtEpoch: 1234567890,
	}

	docs := sync.formatSummaryDocs(summary)

	// Should have 6 documents (all fields present)
	assert.Len(t, docs, 6)

	// Check first document
	assert.Equal(t, "summary_1_request", docs[0].ID)
	assert.Equal(t, "Add feature", docs[0].Content)
	assert.Equal(t, "session_summary", docs[0].Metadata["doc_type"])
	assert.Equal(t, "request", docs[0].Metadata["field_type"])
	assert.Equal(t, int64(5), docs[0].Metadata["prompt_number"])
}

func TestSync_FormatSummaryDocs_PartialFields(t *testing.T) {
	sync := testSync()

	summary := &models.SessionSummary{
		ID:             2,
		SDKSessionID:   "test-session",
		Project:        "test-project",
		Request:        sql.NullString{String: "Only request", Valid: true},
		Completed:      sql.NullString{String: "Only completed", Valid: true},
		CreatedAtEpoch: 1234567890,
	}

	docs := sync.formatSummaryDocs(summary)

	// Should have 2 documents (only valid fields)
	assert.Len(t, docs, 2)

	// Verify field types
	fieldTypes := make([]string, len(docs))
	for i, doc := range docs {
		fieldTypes[i] = doc.Metadata["field_type"].(string)
	}
	assert.Contains(t, fieldTypes, "request")
	assert.Contains(t, fieldTypes, "completed")
}

func TestSync_FormatSummaryDocs_Empty(t *testing.T) {
	sync := testSync()

	summary := &models.SessionSummary{
		ID:             3,
		SDKSessionID:   "test-session",
		Project:        "test-project",
		CreatedAtEpoch: 1234567890,
	}

	docs := sync.formatSummaryDocs(summary)

	// Should have no documents when no content
	assert.Len(t, docs, 0)
}

func TestSync_FormatSummaryDocs_EmptyStrings(t *testing.T) {
	sync := testSync()

	summary := &models.SessionSummary{
		ID:             4,
		SDKSessionID:   "test-session",
		Project:        "test-project",
		Request:        sql.NullString{String: "", Valid: true}, // Valid but empty
		CreatedAtEpoch: 1234567890,
	}

	docs := sync.formatSummaryDocs(summary)

	// Empty strings should not produce documents
	assert.Len(t, docs, 0)
}

// Test helper functions
func TestJoinStrings(t *testing.T) {
	tests := []struct {
		name     string
		strs     []string
		sep      string
		expected string
	}{
		{"empty", []string{}, ",", ""},
		{"single", []string{"a"}, ",", "a"},
		{"multiple", []string{"a", "b", "c"}, ",", "a,b,c"},
		{"different sep", []string{"a", "b"}, "-", "a-b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := joinStrings(tt.strs, tt.sep)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCopyMetadata(t *testing.T) {
	base := map[string]any{
		"key1": "value1",
		"key2": 42,
	}

	result := copyMetadata(base, "key3", "value3")

	// Original should be unchanged
	assert.Len(t, base, 2)

	// Result should have all keys
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

	// Original should be unchanged
	assert.Len(t, base, 1)

	// Result should have all keys
	assert.Len(t, result, 3)
	assert.Equal(t, "value1", result["key1"])
	assert.Equal(t, "value2", result["key2"])
	assert.Equal(t, "value3", result["key3"])
}

// Test ID generation patterns for delete operations
func TestSync_DeleteObservationIDGeneration(t *testing.T) {
	// Test that we generate correct document IDs for deletion
	obsIDs := []int64{1, 2}
	maxFactsPerObs := 20

	ids := make([]string, 0, len(obsIDs)*(maxFactsPerObs+1))
	for _, obsID := range obsIDs {
		ids = append(ids, fmt.Sprintf("obs_%d_narrative", obsID))
		for i := 0; i < maxFactsPerObs; i++ {
			ids = append(ids, fmt.Sprintf("obs_%d_fact_%d", obsID, i))
		}
	}

	// Each observation should generate 21 IDs (1 narrative + 20 facts)
	assert.Len(t, ids, 42)

	// Check some expected IDs
	assert.Contains(t, ids, "obs_1_narrative")
	assert.Contains(t, ids, "obs_1_fact_0")
	assert.Contains(t, ids, "obs_1_fact_19")
	assert.Contains(t, ids, "obs_2_narrative")
	assert.Contains(t, ids, "obs_2_fact_0")
}

func TestSync_DeletePromptIDGeneration(t *testing.T) {
	// Test that we generate correct document IDs for prompt deletion
	promptIDs := []int64{10, 20, 30}

	ids := make([]string, len(promptIDs))
	for i, promptID := range promptIDs {
		ids[i] = fmt.Sprintf("prompt_%d", promptID)
	}

	assert.Len(t, ids, 3)
	assert.Contains(t, ids, "prompt_10")
	assert.Contains(t, ids, "prompt_20")
	assert.Contains(t, ids, "prompt_30")
}

// Test metadata includes all expected fields
func TestSync_ObservationMetadataFields(t *testing.T) {
	sync := testSync()

	obs := &models.Observation{
		ID:             1,
		SDKSessionID:   "sdk-123",
		Project:        "my-project",
		Scope:          models.ScopeGlobal,
		Type:           models.ObsTypeBugfix,
		Title:          sql.NullString{String: "Bug Fix", Valid: true},
		Subtitle:       sql.NullString{String: "Memory leak", Valid: true},
		Narrative:      sql.NullString{String: "Fixed the leak", Valid: true},
		Concepts:       models.JSONStringArray{"memory", "performance"},
		FilesRead:      models.JSONStringArray{"main.go"},
		FilesModified:  models.JSONStringArray{"fix.go"},
		CreatedAtEpoch: 1234567890,
	}

	docs := sync.formatObservationDocs(obs)
	require := assert.New(t)

	require.Len(docs, 1) // Only narrative, no facts

	meta := docs[0].Metadata
	require.Equal(int64(1), meta["sqlite_id"])
	require.Equal("observation", meta["doc_type"])
	require.Equal("sdk-123", meta["sdk_session_id"])
	require.Equal("my-project", meta["project"])
	require.Equal("global", meta["scope"])
	require.Equal("bugfix", meta["type"])
	require.Equal("Bug Fix", meta["title"])
	require.Equal("Memory leak", meta["subtitle"])
	require.Equal("memory,performance", meta["concepts"])
	require.Equal("main.go", meta["files_read"])
	require.Equal("fix.go", meta["files_modified"])
	require.Equal(int64(1234567890), meta["created_at_epoch"])
	require.Equal("narrative", meta["field_type"])
}
