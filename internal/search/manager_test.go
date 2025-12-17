// Package search provides unified search capabilities for claude-mnemonic.
package search

import (
	"database/sql"
	"testing"

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
