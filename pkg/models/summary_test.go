// Package models contains domain models for claude-mnemonic.
package models

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// SummarySuite is a test suite for SessionSummary operations.
type SummarySuite struct {
	suite.Suite
}

func TestSummarySuite(t *testing.T) {
	suite.Run(t, new(SummarySuite))
}

// TestNewSessionSummary tests summary creation.
func (s *SummarySuite) TestNewSessionSummary() {
	parsed := &ParsedSummary{
		Request:      "Fix the bug in handler.go",
		Investigated: "Looked at error logs",
		Learned:      "The issue was a race condition",
		Completed:    "Fixed the race condition",
		NextSteps:    "Add more tests",
		Notes:        "Consider adding mutex",
	}

	summary := NewSessionSummary("sdk-123", "test-project", parsed, 5, 1000)

	s.NotNil(summary)
	s.Equal("sdk-123", summary.SDKSessionID)
	s.Equal("test-project", summary.Project)
	s.True(summary.Request.Valid)
	s.Equal("Fix the bug in handler.go", summary.Request.String)
	s.True(summary.Investigated.Valid)
	s.True(summary.Learned.Valid)
	s.True(summary.Completed.Valid)
	s.True(summary.NextSteps.Valid)
	s.True(summary.Notes.Valid)
	s.True(summary.PromptNumber.Valid)
	s.Equal(int64(5), summary.PromptNumber.Int64)
	s.Equal(int64(1000), summary.DiscoveryTokens)
	s.NotEmpty(summary.CreatedAt)
	s.Greater(summary.CreatedAtEpoch, int64(0))
}

// TestNewSessionSummary_EmptyFields tests summary creation with empty fields.
func (s *SummarySuite) TestNewSessionSummary_EmptyFields() {
	parsed := &ParsedSummary{
		Request: "Test request",
		// All other fields empty
	}

	summary := NewSessionSummary("sdk-123", "project", parsed, 0, 0)

	s.True(summary.Request.Valid)
	s.False(summary.Investigated.Valid)
	s.False(summary.Learned.Valid)
	s.False(summary.Completed.Valid)
	s.False(summary.NextSteps.Valid)
	s.False(summary.Notes.Valid)
	s.False(summary.PromptNumber.Valid) // 0 is not valid
	s.Equal(int64(0), summary.DiscoveryTokens)
}

// TestSessionSummary_MarshalJSON tests JSON marshaling.
func (s *SummarySuite) TestSessionSummary_MarshalJSON() {
	summary := &SessionSummary{
		ID:              1,
		SDKSessionID:    "sdk-123",
		Project:         "test-project",
		Request:         sql.NullString{String: "Test request", Valid: true},
		Investigated:    sql.NullString{String: "Test investigation", Valid: true},
		Learned:         sql.NullString{Valid: false}, // Invalid - should be omitted
		Completed:       sql.NullString{String: "Test completion", Valid: true},
		NextSteps:       sql.NullString{Valid: false},
		Notes:           sql.NullString{String: "Test notes", Valid: true},
		PromptNumber:    sql.NullInt64{Int64: 3, Valid: true},
		DiscoveryTokens: 500,
		CreatedAt:       "2024-01-01T00:00:00Z",
		CreatedAtEpoch:  1704067200000,
	}

	data, err := json.Marshal(summary)
	s.NoError(err)

	// Parse the JSON
	var result map[string]interface{}
	err = json.Unmarshal(data, &result)
	s.NoError(err)

	// Check fields
	s.Equal(float64(1), result["id"])
	s.Equal("sdk-123", result["sdk_session_id"])
	s.Equal("test-project", result["project"])
	s.Equal("Test request", result["request"])
	s.Equal("Test investigation", result["investigated"])
	s.Equal("Test completion", result["completed"])
	s.Equal("Test notes", result["notes"])
	s.Equal(float64(3), result["prompt_number"])
	s.Equal(float64(500), result["discovery_tokens"])

	// Empty fields should be omitted
	_, hasLearned := result["learned"]
	s.False(hasLearned, "Empty learned should be omitted")
	_, hasNextSteps := result["next_steps"]
	s.False(hasNextSteps, "Empty next_steps should be omitted")
}

// TestSessionSummary_MarshalJSON_AllEmpty tests JSON marshaling with all empty optional fields.
func (s *SummarySuite) TestSessionSummary_MarshalJSON_AllEmpty() {
	summary := &SessionSummary{
		ID:              1,
		SDKSessionID:    "sdk-123",
		Project:         "test-project",
		Request:         sql.NullString{Valid: false},
		Investigated:    sql.NullString{Valid: false},
		Learned:         sql.NullString{Valid: false},
		Completed:       sql.NullString{Valid: false},
		NextSteps:       sql.NullString{Valid: false},
		Notes:           sql.NullString{Valid: false},
		PromptNumber:    sql.NullInt64{Valid: false},
		DiscoveryTokens: 0,
		CreatedAt:       "2024-01-01T00:00:00Z",
		CreatedAtEpoch:  1704067200000,
	}

	data, err := json.Marshal(summary)
	s.NoError(err)

	var result map[string]interface{}
	err = json.Unmarshal(data, &result)
	s.NoError(err)

	// Required fields should be present
	s.Equal(float64(1), result["id"])
	s.Equal("sdk-123", result["sdk_session_id"])
	s.Equal("test-project", result["project"])

	// Optional fields should be empty strings or omitted
	request, hasRequest := result["request"]
	if hasRequest {
		s.Equal("", request)
	}
}

// TestParsedSummary tests ParsedSummary structure.
func (s *SummarySuite) TestParsedSummary() {
	parsed := &ParsedSummary{
		Request:      "Request text",
		Investigated: "Investigation text",
		Learned:      "Learned text",
		Completed:    "Completed text",
		NextSteps:    "Next steps text",
		Notes:        "Notes text",
	}

	s.Equal("Request text", parsed.Request)
	s.Equal("Investigation text", parsed.Investigated)
	s.Equal("Learned text", parsed.Learned)
	s.Equal("Completed text", parsed.Completed)
	s.Equal("Next steps text", parsed.NextSteps)
	s.Equal("Notes text", parsed.Notes)
}

// TestSessionSummaryJSON tests the JSON-friendly type.
func (s *SummarySuite) TestSessionSummaryJSON() {
	j := SessionSummaryJSON{
		ID:              1,
		SDKSessionID:    "sdk-123",
		Project:         "test-project",
		Request:         "Request",
		Investigated:    "Investigation",
		Learned:         "Learned",
		Completed:       "Completed",
		NextSteps:       "Next steps",
		Notes:           "Notes",
		PromptNumber:    5,
		DiscoveryTokens: 1000,
		CreatedAt:       "2024-01-01T00:00:00Z",
		CreatedAtEpoch:  1704067200000,
	}

	s.Equal(int64(1), j.ID)
	s.Equal("sdk-123", j.SDKSessionID)
	s.Equal("test-project", j.Project)
	s.Equal("Request", j.Request)
	s.Equal("Investigation", j.Investigated)
	s.Equal("Learned", j.Learned)
	s.Equal("Completed", j.Completed)
	s.Equal("Next steps", j.NextSteps)
	s.Equal("Notes", j.Notes)
	s.Equal(int64(5), j.PromptNumber)
	s.Equal(int64(1000), j.DiscoveryTokens)
}

// TestSessionSummary_TimestampValidity tests that timestamps are set correctly.
func TestSessionSummary_TimestampValidity(t *testing.T) {
	before := time.Now().Add(-time.Second) // Give 1 second buffer

	parsed := &ParsedSummary{Request: "Test"}
	summary := NewSessionSummary("sdk-123", "project", parsed, 1, 100)

	after := time.Now().Add(time.Second) // Give 1 second buffer

	// Parse the timestamp
	createdAt, err := time.Parse(time.RFC3339, summary.CreatedAt)
	require.NoError(t, err)

	// Timestamp should be between before and after (with buffer)
	assert.True(t, createdAt.After(before) || createdAt.Equal(before), "created_at should be >= before")
	assert.True(t, createdAt.Before(after) || createdAt.Equal(after), "created_at should be <= after")

	// Epoch should also be in range (with buffer)
	beforeEpoch := before.UnixMilli()
	afterEpoch := after.UnixMilli()
	assert.GreaterOrEqual(t, summary.CreatedAtEpoch, beforeEpoch, "epoch should be >= before epoch")
	assert.LessOrEqual(t, summary.CreatedAtEpoch, afterEpoch, "epoch should be <= after epoch")
}

// TestSessionSummary_JSONRoundTrip tests that summaries can be marshaled and unmarshaled.
func TestSessionSummary_JSONRoundTrip(t *testing.T) {
	original := &SessionSummary{
		ID:              1,
		SDKSessionID:    "sdk-123",
		Project:         "test-project",
		Request:         sql.NullString{String: "Test request", Valid: true},
		Investigated:    sql.NullString{String: "Test investigation", Valid: true},
		Learned:         sql.NullString{String: "Test learned", Valid: true},
		Completed:       sql.NullString{String: "Test completed", Valid: true},
		NextSteps:       sql.NullString{String: "Test next steps", Valid: true},
		Notes:           sql.NullString{String: "Test notes", Valid: true},
		PromptNumber:    sql.NullInt64{Int64: 5, Valid: true},
		DiscoveryTokens: 1000,
		CreatedAt:       "2024-01-01T00:00:00Z",
		CreatedAtEpoch:  1704067200000,
	}

	// Marshal
	data, err := json.Marshal(original)
	require.NoError(t, err)

	// Unmarshal into JSON type
	var result SessionSummaryJSON
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	// Verify
	assert.Equal(t, original.ID, result.ID)
	assert.Equal(t, original.SDKSessionID, result.SDKSessionID)
	assert.Equal(t, original.Project, result.Project)
	assert.Equal(t, original.Request.String, result.Request)
	assert.Equal(t, original.Investigated.String, result.Investigated)
	assert.Equal(t, original.Learned.String, result.Learned)
	assert.Equal(t, original.Completed.String, result.Completed)
	assert.Equal(t, original.NextSteps.String, result.NextSteps)
	assert.Equal(t, original.Notes.String, result.Notes)
	assert.Equal(t, original.PromptNumber.Int64, result.PromptNumber)
	assert.Equal(t, original.DiscoveryTokens, result.DiscoveryTokens)
}
