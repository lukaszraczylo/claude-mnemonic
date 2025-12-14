// Package models contains domain models for claude-mnemonic.
package models

import (
	"database/sql"
	"encoding/json"
	"time"
)

// SessionSummary represents a summary of a Claude Code session.
type SessionSummary struct {
	ID              int64          `db:"id" json:"id"`
	SDKSessionID    string         `db:"sdk_session_id" json:"sdk_session_id"`
	Project         string         `db:"project" json:"project"`
	Request         sql.NullString `db:"request" json:"request,omitempty"`
	Investigated    sql.NullString `db:"investigated" json:"investigated,omitempty"`
	Learned         sql.NullString `db:"learned" json:"learned,omitempty"`
	Completed       sql.NullString `db:"completed" json:"completed,omitempty"`
	NextSteps       sql.NullString `db:"next_steps" json:"next_steps,omitempty"`
	Notes           sql.NullString `db:"notes" json:"notes,omitempty"`
	PromptNumber    sql.NullInt64  `db:"prompt_number" json:"prompt_number,omitempty"`
	DiscoveryTokens int64          `db:"discovery_tokens" json:"discovery_tokens"`
	CreatedAt       string         `db:"created_at" json:"created_at"`
	CreatedAtEpoch  int64          `db:"created_at_epoch" json:"created_at_epoch"`
}

// ParsedSummary represents a summary parsed from SDK response XML.
type ParsedSummary struct {
	Request      string
	Investigated string
	Learned      string
	Completed    string
	NextSteps    string
	Notes        string
}

// NewSessionSummary creates a new session summary from parsed data.
func NewSessionSummary(sdkSessionID, project string, parsed *ParsedSummary, promptNumber int, discoveryTokens int64) *SessionSummary {
	now := time.Now()
	return &SessionSummary{
		SDKSessionID:    sdkSessionID,
		Project:         project,
		Request:         sql.NullString{String: parsed.Request, Valid: parsed.Request != ""},
		Investigated:    sql.NullString{String: parsed.Investigated, Valid: parsed.Investigated != ""},
		Learned:         sql.NullString{String: parsed.Learned, Valid: parsed.Learned != ""},
		Completed:       sql.NullString{String: parsed.Completed, Valid: parsed.Completed != ""},
		NextSteps:       sql.NullString{String: parsed.NextSteps, Valid: parsed.NextSteps != ""},
		Notes:           sql.NullString{String: parsed.Notes, Valid: parsed.Notes != ""},
		PromptNumber:    sql.NullInt64{Int64: int64(promptNumber), Valid: promptNumber > 0},
		DiscoveryTokens: discoveryTokens,
		CreatedAt:       now.Format(time.RFC3339),
		CreatedAtEpoch:  now.UnixMilli(),
	}
}

// SessionSummaryJSON is a JSON-friendly representation of SessionSummary.
// It converts sql.NullString to plain strings for clean JSON output.
type SessionSummaryJSON struct {
	ID              int64  `json:"id"`
	SDKSessionID    string `json:"sdk_session_id"`
	Project         string `json:"project"`
	Request         string `json:"request,omitempty"`
	Investigated    string `json:"investigated,omitempty"`
	Learned         string `json:"learned,omitempty"`
	Completed       string `json:"completed,omitempty"`
	NextSteps       string `json:"next_steps,omitempty"`
	Notes           string `json:"notes,omitempty"`
	PromptNumber    int64  `json:"prompt_number,omitempty"`
	DiscoveryTokens int64  `json:"discovery_tokens"`
	CreatedAt       string `json:"created_at"`
	CreatedAtEpoch  int64  `json:"created_at_epoch"`
}

// MarshalJSON implements json.Marshaler for SessionSummary.
// Converts sql.NullString fields to plain strings.
func (s *SessionSummary) MarshalJSON() ([]byte, error) {
	j := SessionSummaryJSON{
		ID:              s.ID,
		SDKSessionID:    s.SDKSessionID,
		Project:         s.Project,
		DiscoveryTokens: s.DiscoveryTokens,
		CreatedAt:       s.CreatedAt,
		CreatedAtEpoch:  s.CreatedAtEpoch,
	}
	if s.Request.Valid {
		j.Request = s.Request.String
	}
	if s.Investigated.Valid {
		j.Investigated = s.Investigated.String
	}
	if s.Learned.Valid {
		j.Learned = s.Learned.String
	}
	if s.Completed.Valid {
		j.Completed = s.Completed.String
	}
	if s.NextSteps.Valid {
		j.NextSteps = s.NextSteps.String
	}
	if s.Notes.Valid {
		j.Notes = s.Notes.String
	}
	if s.PromptNumber.Valid {
		j.PromptNumber = s.PromptNumber.Int64
	}
	return json.Marshal(j)
}
