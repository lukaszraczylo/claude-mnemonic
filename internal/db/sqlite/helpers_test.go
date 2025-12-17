package sqlite

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNullString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		valid    bool
	}{
		{"empty_string", "", "", false},
		{"non_empty_string", "hello", "hello", true},
		{"whitespace", " ", " ", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := nullString(tt.input)
			assert.Equal(t, tt.expected, result.String)
			assert.Equal(t, tt.valid, result.Valid)
		})
	}
}

func TestNullInt(t *testing.T) {
	tests := []struct {
		name     string
		input    int
		expected int64
		valid    bool
	}{
		{"zero", 0, 0, false},
		{"positive", 42, 42, true},
		{"negative", -1, -1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := nullInt(tt.input)
			assert.Equal(t, tt.expected, result.Int64)
			assert.Equal(t, tt.valid, result.Valid)
		})
	}
}

func TestRepeatPlaceholders(t *testing.T) {
	tests := []struct {
		name     string
		n        int
		expected string
	}{
		{"zero", 0, ""},
		{"negative", -1, ""},
		{"one", 1, ", ?"},
		{"two", 2, ", ?, ?"},
		{"three", 3, ", ?, ?, ?"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := repeatPlaceholders(tt.n)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestInt64SliceToInterface(t *testing.T) {
	tests := []struct {
		name     string
		input    []int64
		expected []interface{}
	}{
		{"empty", []int64{}, []interface{}{}},
		{"single", []int64{42}, []interface{}{int64(42)}},
		{"multiple", []int64{1, 2, 3}, []interface{}{int64(1), int64(2), int64(3)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := int64SliceToInterface(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseLimitParam(t *testing.T) {
	tests := []struct {
		name         string
		query        string
		defaultLimit int
		expected     int
	}{
		{"no_param_uses_default", "", 10, 10},
		{"valid_limit", "limit=20", 10, 20},
		{"invalid_limit_uses_default", "limit=abc", 10, 10},
		{"zero_limit_uses_default", "limit=0", 10, 10},
		{"negative_limit_uses_default", "limit=-5", 10, 10},
		{"large_limit", "limit=1000", 10, 1000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/?"+tt.query, nil)
			result := ParseLimitParam(req, tt.defaultLimit)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestScanSummary(t *testing.T) {
	db, _, cleanup := testDB(t)
	defer cleanup()
	createBaseTables(t, db)
	seedSession(t, db, "claude-123", "sdk-123", "test-project")

	// Insert a test summary
	_, err := db.Exec(`
		INSERT INTO session_summaries (sdk_session_id, project, request, investigated, learned, completed, next_steps, notes, prompt_number, discovery_tokens, created_at, created_at_epoch)
		VALUES ('sdk-123', 'test-project', 'test request', 'test investigated', 'test learned', 'test completed', 'test next steps', 'test notes', 1, 100, '2025-01-01T00:00:00Z', 1704067200000)
	`)
	require.NoError(t, err)

	// Query and scan
	row := db.QueryRow(`
		SELECT id, sdk_session_id, project, request, investigated, learned, completed, next_steps, notes, prompt_number, discovery_tokens, created_at, created_at_epoch
		FROM session_summaries WHERE sdk_session_id = ?
	`, "sdk-123")

	summary, err := scanSummary(row)
	require.NoError(t, err)
	assert.NotNil(t, summary)
	assert.Equal(t, "sdk-123", summary.SDKSessionID)
	assert.Equal(t, "test-project", summary.Project)
	assert.Equal(t, "test request", summary.Request.String)
	assert.Equal(t, "test investigated", summary.Investigated.String)
	assert.Equal(t, "test learned", summary.Learned.String)
	assert.Equal(t, "test completed", summary.Completed.String)
	assert.Equal(t, "test next steps", summary.NextSteps.String)
	assert.Equal(t, "test notes", summary.Notes.String)
}

func TestScanSummaryRows(t *testing.T) {
	db, _, cleanup := testDB(t)
	defer cleanup()
	createBaseTables(t, db)
	seedSession(t, db, "claude-123", "sdk-123", "test-project")

	// Insert multiple summaries
	_, err := db.Exec(`
		INSERT INTO session_summaries (sdk_session_id, project, request, investigated, learned, completed, next_steps, notes, prompt_number, discovery_tokens, created_at, created_at_epoch)
		VALUES
			('sdk-123', 'test-project', 'request 1', '', '', '', '', '', 1, 0, '2025-01-01T00:00:00Z', 1704067200000),
			('sdk-123', 'test-project', 'request 2', '', '', '', '', '', 2, 0, '2025-01-02T00:00:00Z', 1704153600000)
	`)
	require.NoError(t, err)

	rows, err := db.Query(`
		SELECT id, sdk_session_id, project, request, investigated, learned, completed, next_steps, notes, prompt_number, discovery_tokens, created_at, created_at_epoch
		FROM session_summaries WHERE sdk_session_id = ? ORDER BY id
	`, "sdk-123")
	require.NoError(t, err)
	defer rows.Close()

	summaries, err := scanSummaryRows(rows)
	require.NoError(t, err)
	assert.Len(t, summaries, 2)
	assert.Equal(t, "request 1", summaries[0].Request.String)
	assert.Equal(t, "request 2", summaries[1].Request.String)
}

func TestScanPromptWithSession(t *testing.T) {
	db, _, cleanup := testDB(t)
	defer cleanup()
	createBaseTables(t, db)
	seedSession(t, db, "claude-123", "sdk-123", "test-project")

	// Insert a test prompt
	_, err := db.Exec(`
		INSERT INTO user_prompts (claude_session_id, prompt_number, prompt_text, matched_observations, created_at, created_at_epoch)
		VALUES ('claude-123', 1, 'test prompt', 5, '2025-01-01T00:00:00Z', 1704067200000)
	`)
	require.NoError(t, err)

	// Query with session join
	row := db.QueryRow(`
		SELECT p.id, p.claude_session_id, p.prompt_number, p.prompt_text, p.matched_observations, p.created_at, p.created_at_epoch, s.project, s.sdk_session_id
		FROM user_prompts p
		JOIN sdk_sessions s ON p.claude_session_id = s.claude_session_id
		WHERE p.claude_session_id = ?
	`, "claude-123")

	prompt, err := scanPromptWithSession(row)
	require.NoError(t, err)
	assert.NotNil(t, prompt)
	assert.Equal(t, "claude-123", prompt.ClaudeSessionID)
	assert.Equal(t, 1, prompt.PromptNumber)
	assert.Equal(t, "test prompt", prompt.PromptText)
	assert.Equal(t, 5, prompt.MatchedObservations)
	assert.Equal(t, "test-project", prompt.Project)
	assert.Equal(t, "sdk-123", prompt.SDKSessionID)
}

func TestScanPromptWithSessionRows(t *testing.T) {
	db, _, cleanup := testDB(t)
	defer cleanup()
	createBaseTables(t, db)
	seedSession(t, db, "claude-123", "sdk-123", "test-project")

	// Insert multiple prompts
	_, err := db.Exec(`
		INSERT INTO user_prompts (claude_session_id, prompt_number, prompt_text, matched_observations, created_at, created_at_epoch)
		VALUES
			('claude-123', 1, 'prompt one', 3, '2025-01-01T00:00:00Z', 1704067200000),
			('claude-123', 2, 'prompt two', 5, '2025-01-02T00:00:00Z', 1704153600000)
	`)
	require.NoError(t, err)

	rows, err := db.Query(`
		SELECT p.id, p.claude_session_id, p.prompt_number, p.prompt_text, p.matched_observations, p.created_at, p.created_at_epoch, s.project, s.sdk_session_id
		FROM user_prompts p
		JOIN sdk_sessions s ON p.claude_session_id = s.claude_session_id
		WHERE p.claude_session_id = ? ORDER BY p.id
	`, "claude-123")
	require.NoError(t, err)
	defer rows.Close()

	prompts, err := scanPromptWithSessionRows(rows)
	require.NoError(t, err)
	assert.Len(t, prompts, 2)
	assert.Equal(t, "prompt one", prompts[0].PromptText)
	assert.Equal(t, "prompt two", prompts[1].PromptText)
}

func TestParseLimitParam_HTTPRequest(t *testing.T) {
	// Test with an actual HTTP request
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := ParseLimitParam(r, 25)
		if limit != 50 {
			t.Errorf("Expected limit 50, got %d", limit)
		}
	})

	req := httptest.NewRequest("GET", "http://example.com/api?limit=50", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
}
