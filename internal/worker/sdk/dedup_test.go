package sdk

import (
	"testing"

	"github.com/lukaszraczylo/claude-mnemonic/internal/vector/sqlitevec"
	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
)

func TestBuildObservationSearchText(t *testing.T) {
	tests := []struct {
		name     string
		obs      *models.ParsedObservation
		expected string
	}{
		{
			name:     "empty observation",
			obs:      &models.ParsedObservation{},
			expected: "",
		},
		{
			name: "title only",
			obs: &models.ParsedObservation{
				Title: "Fix database connection",
			},
			expected: "Fix database connection",
		},
		{
			name: "all fields",
			obs: &models.ParsedObservation{
				Title:     "Fix database connection",
				Subtitle:  "Connection pooling issue",
				Narrative: "The database connection pool was exhausted due to leaked connections.",
			},
			expected: "Fix database connection Connection pooling issue The database connection pool was exhausted due to leaked connections.",
		},
		{
			name: "truncates long text",
			obs: &models.ParsedObservation{
				Narrative: string(make([]byte, 3000)),
			},
			expected: string(make([]byte, 2000)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildObservationSearchText(tt.obs)
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestExtractObservationIDFromVectorDoc(t *testing.T) {
	tests := []struct {
		name     string
		result   sqlitevec.QueryResult
		expected int64
	}{
		{
			name: "from sqlite_id metadata (float64)",
			result: sqlitevec.QueryResult{
				ID:       "obs_42_narrative",
				Metadata: map[string]any{"sqlite_id": float64(42)},
			},
			expected: 42,
		},
		{
			name: "from sqlite_id metadata (int64)",
			result: sqlitevec.QueryResult{
				ID:       "obs_42_narrative",
				Metadata: map[string]any{"sqlite_id": int64(42)},
			},
			expected: 42,
		},
		{
			name: "fallback to doc_id parsing",
			result: sqlitevec.QueryResult{
				ID:       "obs_99_composite",
				Metadata: map[string]any{},
			},
			expected: 99,
		},
		{
			name: "non-observation doc_id",
			result: sqlitevec.QueryResult{
				ID:       "summary_5_text",
				Metadata: map[string]any{},
			},
			expected: 0,
		},
		{
			name: "zero sqlite_id falls back to doc_id",
			result: sqlitevec.QueryResult{
				ID:       "obs_123_narrative",
				Metadata: map[string]any{"sqlite_id": float64(0)},
			},
			expected: 123,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractObservationIDFromVectorDoc(tt.result)
			if result != tt.expected {
				t.Errorf("got %d, want %d", result, tt.expected)
			}
		})
	}
}

func TestCheckVectorDeduplication_NilClient(t *testing.T) {
	p := &Processor{
		// No vectorClient set
	}

	obs := &models.ParsedObservation{
		Title:     "Test observation",
		Narrative: "Some narrative text",
	}

	result := p.checkVectorDeduplication(nil, obs, "test-project")
	if result.Action != "insert" {
		t.Errorf("expected Action='insert' when vectorClient is nil, got %q", result.Action)
	}
}

func TestCheckVectorDeduplication_EmptySearchText(t *testing.T) {
	p := &Processor{
		// vectorClient would be set but obs is empty
	}

	obs := &models.ParsedObservation{
		// All empty fields
	}

	result := p.checkVectorDeduplication(nil, obs, "test-project")
	if result.Action != "insert" {
		t.Errorf("expected Action='insert' for empty observation, got %q", result.Action)
	}
}
