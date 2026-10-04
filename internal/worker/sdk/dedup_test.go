package sdk

import (
	"context"
	"testing"

	"github.com/lukaszraczylo/claude-mnemonic/internal/db/gorm"
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

	result := p.checkVectorDeduplication(context.Background(), obs, "test-project")
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

	result := p.checkVectorDeduplication(context.Background(), obs, "test-project")
	if result.Action != "insert" {
		t.Errorf("expected Action='insert' for empty observation, got %q", result.Action)
	}
}

func TestCanMergeInto(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := gorm.NewStore(gorm.Config{Path: dir + "/test.db", MaxConns: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	obsStore := gorm.NewObservationStore(store, nil, nil, nil)
	id, _, err := obsStore.StoreObservation(ctx, "s1", "proj", &models.ParsedObservation{Type: models.ObsTypeDiscovery, Title: "A note"}, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	p := &Processor{observationStore: obsStore}

	if !p.canMergeInto(ctx, id) {
		t.Error("a live observation is a merge target")
	}
	if p.canMergeInto(ctx, id+1000) {
		t.Error("an observation that does not exist is not")
	}
	if err := store.DB.Exec(`UPDATE observations SET is_superseded = 1 WHERE id = ?`, id).Error; err != nil {
		t.Fatal(err)
	}
	if p.canMergeInto(ctx, id) {
		t.Error("a superseded observation is hidden from sessions, so nothing is merged into it")
	}
	if !(&Processor{}).canMergeInto(ctx, id) {
		t.Error("without an observation store there is nothing to check against")
	}
}
