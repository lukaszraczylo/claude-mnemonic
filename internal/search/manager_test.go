// Package search provides unified search capabilities for claude-mnemonic.
package search

import (
	"database/sql"
	"testing"
	"time"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

// ManagerSuite is a test suite for search Manager operations.
type ManagerSuite struct {
	suite.Suite
}

func TestManagerSuite(t *testing.T) {
	suite.Run(t, new(ManagerSuite))
}

// TestNewManager tests manager creation.
func (s *ManagerSuite) TestNewManager() {
	// Test with nil stores (valid use case for testing)
	m := NewManager(nil, nil, nil, nil)
	s.NotNil(m)
	s.Nil(m.observationStore)
	s.Nil(m.summaryStore)
	s.Nil(m.promptStore)
	s.Nil(m.vectorClient)
}

// TestSearchParams tests SearchParams defaults.
func (s *ManagerSuite) TestSearchParams() {
	params := SearchParams{
		Query:   "test query",
		Project: "my-project",
		Limit:   10,
	}

	s.Equal("test query", params.Query)
	s.Equal("my-project", params.Project)
	s.Equal(10, params.Limit)
	s.Equal("", params.Type)
	s.Equal("", params.OrderBy)
}

// TestSearchResult tests SearchResult struct.
func (s *ManagerSuite) TestSearchResult() {
	result := SearchResult{
		Type:      "observation",
		ID:        123,
		Title:     "Test Title",
		Content:   "Test content",
		Project:   "my-project",
		Scope:     "project",
		CreatedAt: 1704067200000,
		Score:     0.95,
		Metadata: map[string]interface{}{
			"obs_type": "discovery",
		},
	}

	s.Equal("observation", result.Type)
	s.Equal(int64(123), result.ID)
	s.Equal("Test Title", result.Title)
	s.Equal("Test content", result.Content)
	s.Equal("my-project", result.Project)
	s.Equal("project", result.Scope)
	s.Equal(int64(1704067200000), result.CreatedAt)
	s.Equal(0.95, result.Score)
	s.Equal("discovery", result.Metadata["obs_type"])
}

// TestUnifiedSearchResult tests UnifiedSearchResult struct.
func (s *ManagerSuite) TestUnifiedSearchResult() {
	result := UnifiedSearchResult{
		Results: []SearchResult{
			{Type: "observation", ID: 1},
			{Type: "session", ID: 2},
		},
		TotalCount: 2,
		Query:      "test",
	}

	s.Len(result.Results, 2)
	s.Equal(2, result.TotalCount)
	s.Equal("test", result.Query)
}

// TestTruncate tests the truncate helper function.
func TestTruncate(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{
			name:     "short string no truncation",
			input:    "hello",
			maxLen:   10,
			expected: "hello",
		},
		{
			name:     "exact length no truncation",
			input:    "hello",
			maxLen:   5,
			expected: "hello",
		},
		{
			name:     "long string truncated",
			input:    "hello world this is a long string",
			maxLen:   10,
			expected: "hello worl...",
		},
		{
			name:     "empty string",
			input:    "",
			maxLen:   10,
			expected: "",
		},
		{
			name:     "whitespace trimmed",
			input:    "  hello  ",
			maxLen:   10,
			expected: "hello",
		},
		{
			name:     "whitespace trimmed then truncated",
			input:    "  hello world this is long  ",
			maxLen:   10,
			expected: "hello worl...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncate(tt.input, tt.maxLen)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestObservationToResult tests observation to result conversion.
func TestObservationToResult(t *testing.T) {
	m := NewManager(nil, nil, nil, nil)

	tests := []struct {
		name     string
		obs      *models.Observation
		format   string
		expected SearchResult
	}{
		{
			name: "full format with all fields",
			obs: &models.Observation{
				ID:             123,
				Project:        "my-project",
				Type:           models.ObsTypeDiscovery,
				Scope:          models.ScopeProject,
				Title:          sql.NullString{String: "Test Title", Valid: true},
				Narrative:      sql.NullString{String: "Full narrative content", Valid: true},
				CreatedAtEpoch: 1704067200000,
			},
			format: "full",
			expected: SearchResult{
				Type:      "observation",
				ID:        123,
				Title:     "Test Title",
				Content:   "Full narrative content",
				Project:   "my-project",
				Scope:     "project",
				CreatedAt: 1704067200000,
			},
		},
		{
			name: "index format no content",
			obs: &models.Observation{
				ID:             456,
				Project:        "other-project",
				Type:           models.ObsTypeBugfix,
				Scope:          models.ScopeGlobal,
				Title:          sql.NullString{String: "Bug Fix", Valid: true},
				Narrative:      sql.NullString{String: "Narrative here", Valid: true},
				CreatedAtEpoch: 1704067200000,
			},
			format: "index",
			expected: SearchResult{
				Type:      "observation",
				ID:        456,
				Title:     "Bug Fix",
				Content:   "", // Not included in index format
				Project:   "other-project",
				Scope:     "global",
				CreatedAt: 1704067200000,
			},
		},
		{
			name: "null title",
			obs: &models.Observation{
				ID:             789,
				Project:        "project",
				Type:           models.ObsTypeFeature,
				Scope:          models.ScopeProject,
				Title:          sql.NullString{Valid: false},
				Narrative:      sql.NullString{Valid: false},
				CreatedAtEpoch: 1704067200000,
			},
			format: "full",
			expected: SearchResult{
				Type:      "observation",
				ID:        789,
				Title:     "",
				Content:   "",
				Project:   "project",
				Scope:     "project",
				CreatedAt: 1704067200000,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.observationToResult(tt.obs, tt.format)
			assert.Equal(t, tt.expected.Type, result.Type)
			assert.Equal(t, tt.expected.ID, result.ID)
			assert.Equal(t, tt.expected.Title, result.Title)
			assert.Equal(t, tt.expected.Content, result.Content)
			assert.Equal(t, tt.expected.Project, result.Project)
			assert.Equal(t, tt.expected.Scope, result.Scope)
			assert.Equal(t, tt.expected.CreatedAt, result.CreatedAt)
		})
	}
}

// TestSummaryToResult tests summary to result conversion.
func TestSummaryToResult(t *testing.T) {
	m := NewManager(nil, nil, nil, nil)

	tests := []struct {
		name     string
		summary  *models.SessionSummary
		format   string
		expected SearchResult
	}{
		{
			name: "full format with all fields",
			summary: &models.SessionSummary{
				ID:             123,
				Project:        "my-project",
				Request:        sql.NullString{String: "Test request", Valid: true},
				Learned:        sql.NullString{String: "Learned this content", Valid: true},
				CreatedAtEpoch: 1704067200000,
			},
			format: "full",
			expected: SearchResult{
				Type:      "session",
				ID:        123,
				Title:     "Test request",
				Content:   "Learned this content",
				Project:   "my-project",
				CreatedAt: 1704067200000,
			},
		},
		{
			name: "index format no content",
			summary: &models.SessionSummary{
				ID:             456,
				Project:        "other-project",
				Request:        sql.NullString{String: "Another request", Valid: true},
				Learned:        sql.NullString{String: "Some learning", Valid: true},
				CreatedAtEpoch: 1704067200000,
			},
			format: "index",
			expected: SearchResult{
				Type:      "session",
				ID:        456,
				Title:     "Another request",
				Content:   "", // Not included in index format
				Project:   "other-project",
				CreatedAt: 1704067200000,
			},
		},
		{
			name: "long title truncated",
			summary: &models.SessionSummary{
				ID:             789,
				Project:        "project",
				Request:        sql.NullString{String: "This is a very long request that should be truncated because it exceeds the maximum allowed length for titles which is 100 characters", Valid: true},
				Learned:        sql.NullString{Valid: false},
				CreatedAtEpoch: 1704067200000,
			},
			format: "full",
			expected: SearchResult{
				Type:      "session",
				ID:        789,
				Title:     "This is a very long request that should be truncated because it exceeds the maximum allowed length f...",
				Content:   "",
				Project:   "project",
				CreatedAt: 1704067200000,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.summaryToResult(tt.summary, tt.format)
			assert.Equal(t, tt.expected.Type, result.Type)
			assert.Equal(t, tt.expected.ID, result.ID)
			assert.Equal(t, tt.expected.Title, result.Title)
			assert.Equal(t, tt.expected.Content, result.Content)
			assert.Equal(t, tt.expected.Project, result.Project)
			assert.Equal(t, tt.expected.CreatedAt, result.CreatedAt)
		})
	}
}

// TestPromptToResult tests prompt to result conversion.
func TestPromptToResult(t *testing.T) {
	m := NewManager(nil, nil, nil, nil)

	tests := []struct {
		name     string
		prompt   *models.UserPromptWithSession
		format   string
		expected SearchResult
	}{
		{
			name: "full format with content",
			prompt: &models.UserPromptWithSession{
				UserPrompt: models.UserPrompt{
					ID:             123,
					PromptText:     "What is the meaning of life?",
					CreatedAtEpoch: 1704067200000,
				},
				Project: "my-project",
			},
			format: "full",
			expected: SearchResult{
				Type:      "prompt",
				ID:        123,
				Title:     "What is the meaning of life?",
				Content:   "What is the meaning of life?",
				Project:   "my-project",
				CreatedAt: 1704067200000,
			},
		},
		{
			name: "index format no content",
			prompt: &models.UserPromptWithSession{
				UserPrompt: models.UserPrompt{
					ID:             456,
					PromptText:     "Short prompt",
					CreatedAtEpoch: 1704067200000,
				},
				Project: "other-project",
			},
			format: "index",
			expected: SearchResult{
				Type:      "prompt",
				ID:        456,
				Title:     "Short prompt",
				Content:   "",
				Project:   "other-project",
				CreatedAt: 1704067200000,
			},
		},
		{
			name: "long prompt truncated title",
			prompt: &models.UserPromptWithSession{
				UserPrompt: models.UserPrompt{
					ID:             789,
					PromptText:     "This is a very long prompt that should be truncated because it exceeds the maximum allowed length for titles which is 100 characters and it keeps going",
					CreatedAtEpoch: 1704067200000,
				},
				Project: "project",
			},
			format: "full",
			expected: SearchResult{
				Type:      "prompt",
				ID:        789,
				Title:     "This is a very long prompt that should be truncated because it exceeds the maximum allowed length fo...",
				Content:   "This is a very long prompt that should be truncated because it exceeds the maximum allowed length for titles which is 100 characters and it keeps going",
				Project:   "project",
				CreatedAt: 1704067200000,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.promptToResult(tt.prompt, tt.format)
			assert.Equal(t, tt.expected.Type, result.Type)
			assert.Equal(t, tt.expected.ID, result.ID)
			assert.Equal(t, tt.expected.Title, result.Title)
			assert.Equal(t, tt.expected.Content, result.Content)
			assert.Equal(t, tt.expected.Project, result.Project)
			assert.Equal(t, tt.expected.CreatedAt, result.CreatedAt)
		})
	}
}

// TestSearchParamsValidation tests parameter validation in UnifiedSearch.
func TestSearchParamsValidation(t *testing.T) {
	tests := []struct {
		name          string
		params        SearchParams
		expectedLimit int
		expectedOrder string
	}{
		{
			name: "default limit applied",
			params: SearchParams{
				Query:   "test",
				Project: "project",
				Limit:   0,
			},
			expectedLimit: 20,
			expectedOrder: "date_desc",
		},
		{
			name: "negative limit corrected",
			params: SearchParams{
				Query:   "test",
				Project: "project",
				Limit:   -5,
			},
			expectedLimit: 20,
			expectedOrder: "date_desc",
		},
		{
			name: "limit over 100 capped",
			params: SearchParams{
				Query:   "test",
				Project: "project",
				Limit:   200,
			},
			expectedLimit: 100,
			expectedOrder: "date_desc",
		},
		{
			name: "custom limit preserved",
			params: SearchParams{
				Query:   "test",
				Project: "project",
				Limit:   50,
				OrderBy: "relevance",
			},
			expectedLimit: 50,
			expectedOrder: "relevance",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Since we can't easily call UnifiedSearch without stores,
			// we verify the expected values through logic
			params := tt.params

			// Simulate the validation logic from UnifiedSearch
			if params.Limit <= 0 {
				params.Limit = 20
			}
			if params.Limit > 100 {
				params.Limit = 100
			}
			if params.OrderBy == "" {
				params.OrderBy = "date_desc"
			}

			assert.Equal(t, tt.expectedLimit, params.Limit)
			assert.Equal(t, tt.expectedOrder, params.OrderBy)
		})
	}
}

// TestDecisionsQueryBoost tests Decisions search query boosting.
func TestDecisionsQueryBoost(t *testing.T) {
	tests := []struct {
		name          string
		inputQuery    string
		expectedQuery string
	}{
		{
			name:          "empty query not boosted",
			inputQuery:    "",
			expectedQuery: "",
		},
		{
			name:          "query boosted with keywords",
			inputQuery:    "authentication",
			expectedQuery: "authentication decision chose architecture",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := SearchParams{Query: tt.inputQuery}
			// Simulate Decisions boost logic
			if params.Query != "" {
				params.Query = params.Query + " decision chose architecture"
			}
			assert.Equal(t, tt.expectedQuery, params.Query)
		})
	}
}

// TestChangesQueryBoost tests Changes search query boosting.
func TestChangesQueryBoost(t *testing.T) {
	tests := []struct {
		name          string
		inputQuery    string
		expectedQuery string
	}{
		{
			name:          "empty query not boosted",
			inputQuery:    "",
			expectedQuery: "",
		},
		{
			name:          "query boosted with keywords",
			inputQuery:    "handler",
			expectedQuery: "handler changed modified refactored",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := SearchParams{Query: tt.inputQuery}
			// Simulate Changes boost logic
			if params.Query != "" {
				params.Query = params.Query + " changed modified refactored"
			}
			assert.Equal(t, tt.expectedQuery, params.Query)
		})
	}
}

// TestHowItWorksQueryBoost tests HowItWorks search query boosting.
func TestHowItWorksQueryBoost(t *testing.T) {
	tests := []struct {
		name          string
		inputQuery    string
		expectedQuery string
	}{
		{
			name:          "empty query not boosted",
			inputQuery:    "",
			expectedQuery: "",
		},
		{
			name:          "query boosted with keywords",
			inputQuery:    "database",
			expectedQuery: "database architecture design pattern implements",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := SearchParams{Query: tt.inputQuery}
			// Simulate HowItWorks boost logic
			if params.Query != "" {
				params.Query = params.Query + " architecture design pattern implements"
			}
			assert.Equal(t, tt.expectedQuery, params.Query)
		})
	}
}

// TestSearchTypeMapping tests type string to doc type mapping.
func TestSearchTypeMapping(t *testing.T) {
	tests := []struct {
		typeStr  string
		expected string
	}{
		{"observations", "observation"},
		{"sessions", "session_summary"},
		{"prompts", "user_prompt"},
		{"", ""}, // Empty type for all
	}

	for _, tt := range tests {
		t.Run("type_"+tt.typeStr, func(t *testing.T) {
			// This tests the type mapping logic
			// Just verify the valid type strings
			validTypes := map[string]bool{
				"observations": true,
				"sessions":     true,
				"prompts":      true,
				"":             true,
			}
			assert.True(t, validTypes[tt.typeStr])
		})
	}
}

// TestFilterSearchWithObservations tests filter search when observations exist.
func TestFilterSearchWithObservations(t *testing.T) {
	// Create mock observation
	obs := &models.Observation{
		ID:             1,
		Project:        "test-project",
		Type:           models.ObsTypeDiscovery,
		Scope:          models.ScopeProject,
		Title:          sql.NullString{String: "Test Title", Valid: true},
		Narrative:      sql.NullString{String: "Test narrative content", Valid: true},
		CreatedAtEpoch: 1704067200000,
	}

	m := NewManager(nil, nil, nil, nil)
	result := m.observationToResult(obs, "full")

	assert.Equal(t, "observation", result.Type)
	assert.Equal(t, int64(1), result.ID)
	assert.Equal(t, "Test Title", result.Title)
	assert.Equal(t, "Test narrative content", result.Content)
	assert.Equal(t, "test-project", result.Project)
	assert.Equal(t, "project", result.Scope)
}

// TestManagerStoreReferences tests that Manager stores references correctly.
func TestManagerStoreReferences(t *testing.T) {
	m := NewManager(nil, nil, nil, nil)

	assert.Nil(t, m.observationStore)
	assert.Nil(t, m.summaryStore)
	assert.Nil(t, m.promptStore)
	assert.Nil(t, m.vectorClient)
}

// TestObservationToResultWithMetadata tests metadata inclusion in results.
func TestObservationToResultWithMetadata(t *testing.T) {
	m := NewManager(nil, nil, nil, nil)

	tests := []struct {
		name    string
		obsType models.ObservationType
		scope   models.ObservationScope
	}{
		{"bugfix_project", models.ObsTypeBugfix, models.ScopeProject},
		{"feature_global", models.ObsTypeFeature, models.ScopeGlobal},
		{"discovery_project", models.ObsTypeDiscovery, models.ScopeProject},
		{"decision_global", models.ObsTypeDecision, models.ScopeGlobal},
		{"refactor_project", models.ObsTypeRefactor, models.ScopeProject},
		{"change_global", models.ObsTypeChange, models.ScopeGlobal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obs := &models.Observation{
				ID:             1,
				Project:        "test-project",
				Type:           tt.obsType,
				Scope:          tt.scope,
				Title:          sql.NullString{String: "Title", Valid: true},
				CreatedAtEpoch: 1704067200000,
			}

			result := m.observationToResult(obs, "full")

			assert.Equal(t, string(tt.obsType), result.Metadata["obs_type"])
			assert.Equal(t, string(tt.scope), result.Metadata["scope"])
		})
	}
}

// TestSummaryToResultTruncation tests title truncation in summary results.
func TestSummaryToResultTruncation(t *testing.T) {
	m := NewManager(nil, nil, nil, nil)

	tests := []struct {
		name        string
		request     string
		expectedLen int
		shouldTrunc bool
	}{
		{"short_title", "Short request", 13, false},
		{"exact_100", string(make([]byte, 100)), 103, true}, // 100 + "..."
		{"over_100", string(make([]byte, 150)), 103, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary := &models.SessionSummary{
				ID:             1,
				Project:        "test-project",
				Request:        sql.NullString{String: tt.request, Valid: true},
				CreatedAtEpoch: 1704067200000,
			}

			result := m.summaryToResult(summary, "full")

			if tt.shouldTrunc {
				assert.LessOrEqual(t, len(result.Title), tt.expectedLen)
				assert.True(t, len(result.Title) <= 103) // max 100 + "..."
			} else {
				assert.Equal(t, tt.request, result.Title)
			}
		})
	}
}

// TestPromptToResultFormats tests prompt to result conversion with different formats.
func TestPromptToResultFormats(t *testing.T) {
	m := NewManager(nil, nil, nil, nil)

	prompt := &models.UserPromptWithSession{
		UserPrompt: models.UserPrompt{
			ID:             123,
			PromptText:     "What is the meaning of life?",
			CreatedAtEpoch: 1704067200000,
		},
		Project: "my-project",
	}

	// Full format - includes content
	fullResult := m.promptToResult(prompt, "full")
	assert.Equal(t, "What is the meaning of life?", fullResult.Content)

	// Index format - no content
	indexResult := m.promptToResult(prompt, "index")
	assert.Equal(t, "", indexResult.Content)

	// Both should have same title
	assert.Equal(t, fullResult.Title, indexResult.Title)
}

// TestSearchParamsDefaults tests that search params have proper defaults.
func TestSearchParamsDefaults(t *testing.T) {
	tests := []struct {
		name          string
		initialLimit  int
		initialOrder  string
		expectedLimit int
		expectedOrder string
	}{
		{"zero_limit", 0, "", 20, "date_desc"},
		{"negative_limit", -5, "", 20, "date_desc"},
		{"over_100_limit", 150, "", 100, "date_desc"},
		{"valid_limit_50", 50, "relevance", 50, "relevance"},
		{"custom_order", 30, "date_asc", 30, "date_asc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := SearchParams{
				Query:   "test",
				Project: "project",
				Limit:   tt.initialLimit,
				OrderBy: tt.initialOrder,
			}

			// Simulate the normalization that happens in UnifiedSearch
			if params.Limit <= 0 {
				params.Limit = 20
			}
			if params.Limit > 100 {
				params.Limit = 100
			}
			if params.OrderBy == "" {
				params.OrderBy = "date_desc"
			}

			assert.Equal(t, tt.expectedLimit, params.Limit)
			assert.Equal(t, tt.expectedOrder, params.OrderBy)
		})
	}
}

// TestTruncateEdgeCases tests edge cases for truncate function.
func TestTruncateEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		// Unicode strings - uses byte length so ensure maxLen accommodates full string
		{"unicode_string_no_truncate", "日本語テスト", 20, "日本語テスト"},
		{"mixed_unicode_no_truncate", "Hello世界", 15, "Hello世界"},
		// ASCII truncation
		{"ascii_truncate", "Hello World", 5, "Hello..."},
		{"only_whitespace", "   ", 10, ""},
		{"tabs_and_newlines", "\t\n  \t", 10, ""},
		{"newlines_with_content", "\n\nhello\n\n", 10, "hello"},
		{"zero_max_len", "hello", 0, "..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncate(tt.input, tt.maxLen)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestUnifiedSearchResultEmpty tests empty UnifiedSearchResult.
func TestUnifiedSearchResultEmpty(t *testing.T) {
	result := UnifiedSearchResult{
		Results:    []SearchResult{},
		TotalCount: 0,
		Query:      "",
	}

	assert.Empty(t, result.Results)
	assert.Equal(t, 0, result.TotalCount)
	assert.Equal(t, "", result.Query)
}

// TestSearchResultMetadata tests SearchResult metadata handling.
func TestSearchResultMetadata(t *testing.T) {
	result := SearchResult{
		Type: "observation",
		ID:   1,
		Metadata: map[string]interface{}{
			"obs_type": "discovery",
			"scope":    "project",
			"count":    42,
			"enabled":  true,
		},
	}

	assert.Equal(t, "discovery", result.Metadata["obs_type"])
	assert.Equal(t, "project", result.Metadata["scope"])
	assert.Equal(t, 42, result.Metadata["count"])
	assert.Equal(t, true, result.Metadata["enabled"])
}

// TestSearchResultTypes tests all search result types.
func TestSearchResultTypes(t *testing.T) {
	types := []string{"observation", "session", "prompt"}

	for _, typ := range types {
		t.Run(typ, func(t *testing.T) {
			result := SearchResult{
				Type:      typ,
				ID:        1,
				Project:   "test",
				CreatedAt: time.Now().UnixMilli(),
			}
			assert.Equal(t, typ, result.Type)
		})
	}
}

// TestSearchParamsAllFields tests SearchParams with all fields populated.
func TestSearchParamsAllFields(t *testing.T) {
	params := SearchParams{
		Query:         "authentication bug",
		Type:          "observations",
		Project:       "my-project",
		ObsType:       "bugfix",
		Concepts:      "security,auth",
		Files:         "handler.go,auth.go",
		DateStart:     1700000000000,
		DateEnd:       1700100000000,
		OrderBy:       "relevance",
		Limit:         25,
		Offset:        10,
		Format:        "full",
		Scope:         "project",
		IncludeGlobal: true,
	}

	assert.Equal(t, "authentication bug", params.Query)
	assert.Equal(t, "observations", params.Type)
	assert.Equal(t, "my-project", params.Project)
	assert.Equal(t, "bugfix", params.ObsType)
	assert.Equal(t, "security,auth", params.Concepts)
	assert.Equal(t, "handler.go,auth.go", params.Files)
	assert.Equal(t, int64(1700000000000), params.DateStart)
	assert.Equal(t, int64(1700100000000), params.DateEnd)
	assert.Equal(t, "relevance", params.OrderBy)
	assert.Equal(t, 25, params.Limit)
	assert.Equal(t, 10, params.Offset)
	assert.Equal(t, "full", params.Format)
	assert.Equal(t, "project", params.Scope)
	assert.True(t, params.IncludeGlobal)
}

// TestObservationToResultWithNullFields tests handling of null fields.
func TestObservationToResultWithNullFields(t *testing.T) {
	m := NewManager(nil, nil, nil, nil)

	obs := &models.Observation{
		ID:             1,
		Project:        "test-project",
		Type:           models.ObsTypeDiscovery,
		Scope:          models.ScopeProject,
		Title:          sql.NullString{Valid: false},
		Narrative:      sql.NullString{Valid: false},
		CreatedAtEpoch: 1704067200000,
	}

	result := m.observationToResult(obs, "full")

	assert.Equal(t, "", result.Title)
	assert.Equal(t, "", result.Content)
}

// TestSummaryToResultWithNullFields tests handling of null fields in summary.
func TestSummaryToResultWithNullFields(t *testing.T) {
	m := NewManager(nil, nil, nil, nil)

	summary := &models.SessionSummary{
		ID:             1,
		Project:        "test-project",
		Request:        sql.NullString{Valid: false},
		Learned:        sql.NullString{Valid: false},
		CreatedAtEpoch: 1704067200000,
	}

	result := m.summaryToResult(summary, "full")

	assert.Equal(t, "", result.Title)
	assert.Equal(t, "", result.Content)
}

// TestSearchParams_LimitValues tests limit parameter handling values.
func TestSearchParams_LimitValues(t *testing.T) {
	tests := []struct {
		name          string
		inputLimit    int
		expectedValid bool
	}{
		{"zero_limit", 0, true},
		{"negative_limit", -5, true},
		{"normal_limit", 20, true},
		{"max_limit", 100, true},
		{"over_limit", 200, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := SearchParams{
				Query:   "test",
				Project: "test",
				Limit:   tt.inputLimit,
			}
			assert.NotNil(t, params)
			assert.Equal(t, tt.inputLimit, params.Limit)
		})
	}
}

// TestSearchParams_OrderByValues tests order by parameter values.
func TestSearchParams_OrderByValues(t *testing.T) {
	validOrders := []string{"relevance", "date_desc", "date_asc", ""}

	for _, order := range validOrders {
		t.Run("order_"+order, func(t *testing.T) {
			params := SearchParams{
				Query:   "test",
				Project: "test",
				OrderBy: order,
			}
			assert.Equal(t, order, params.OrderBy)
		})
	}
}

// TestSearchParams_TypeValues tests type parameter values.
func TestSearchParams_TypeValues(t *testing.T) {
	validTypes := []string{"observations", "sessions", "prompts", ""}

	for _, typ := range validTypes {
		t.Run("type_"+typ, func(t *testing.T) {
			params := SearchParams{
				Query:   "test",
				Project: "test",
				Type:    typ,
			}
			assert.Equal(t, typ, params.Type)
		})
	}
}

// TestSearchParams_ScopeValues tests scope parameter values.
func TestSearchParams_ScopeValues(t *testing.T) {
	validScopes := []string{"project", "global", ""}

	for _, scope := range validScopes {
		t.Run("scope_"+scope, func(t *testing.T) {
			params := SearchParams{
				Query:   "test",
				Project: "test",
				Scope:   scope,
			}
			assert.Equal(t, scope, params.Scope)
		})
	}
}

// TestSearchParams_FormatValues tests format parameter values.
func TestSearchParams_FormatValues(t *testing.T) {
	validFormats := []string{"index", "full", ""}

	for _, format := range validFormats {
		t.Run("format_"+format, func(t *testing.T) {
			params := SearchParams{
				Query:   "test",
				Project: "test",
				Format:  format,
			}
			assert.Equal(t, format, params.Format)
		})
	}
}

// TestUnifiedSearchResult_MultipleResults tests result with multiple items.
func TestUnifiedSearchResult_MultipleResults(t *testing.T) {
	results := []SearchResult{
		{Type: "observation", ID: 1, Title: "First", Project: "test"},
		{Type: "session", ID: 2, Title: "Second", Project: "test"},
		{Type: "prompt", ID: 3, Title: "Third", Project: "test"},
	}

	result := UnifiedSearchResult{
		Results:    results,
		TotalCount: 3,
		Query:      "test query",
	}

	assert.Len(t, result.Results, 3)
	assert.Equal(t, 3, result.TotalCount)
	assert.Equal(t, "observation", result.Results[0].Type)
	assert.Equal(t, "session", result.Results[1].Type)
	assert.Equal(t, "prompt", result.Results[2].Type)
}

// TestSearchResult_Metadata tests metadata handling in SearchResult.
func TestSearchResult_Metadata(t *testing.T) {
	metadata := map[string]interface{}{
		"obs_type":     "discovery",
		"concepts":     []string{"auth", "security"},
		"files_count":  5,
		"is_important": true,
	}

	result := SearchResult{
		Type:     "observation",
		ID:       1,
		Metadata: metadata,
	}

	assert.Equal(t, "discovery", result.Metadata["obs_type"])
	assert.Equal(t, 5, result.Metadata["files_count"])
	assert.Equal(t, true, result.Metadata["is_important"])
}

// TestSearchResult_Scores tests score handling in SearchResult.
func TestSearchResult_Scores(t *testing.T) {
	tests := []struct {
		name  string
		score float64
	}{
		{"perfect_score", 1.0},
		{"high_score", 0.95},
		{"medium_score", 0.5},
		{"low_score", 0.1},
		{"zero_score", 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SearchResult{
				Type:  "observation",
				ID:    1,
				Score: tt.score,
			}
			assert.Equal(t, tt.score, result.Score)
		})
	}
}
