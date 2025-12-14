// Package models contains domain models for claude-mnemonic.
package models

import (
	"database/sql"
	"time"
)

// SessionStatus represents the status of an SDK session.
type SessionStatus string

const (
	SessionStatusActive    SessionStatus = "active"
	SessionStatusCompleted SessionStatus = "completed"
	SessionStatusFailed    SessionStatus = "failed"
)

// SDKSession represents a Claude Code session tracked by the memory system.
type SDKSession struct {
	ID               int64          `db:"id" json:"id"`
	ClaudeSessionID  string         `db:"claude_session_id" json:"claude_session_id"`
	SDKSessionID     sql.NullString `db:"sdk_session_id" json:"sdk_session_id,omitempty"`
	Project          string         `db:"project" json:"project"`
	UserPrompt       sql.NullString `db:"user_prompt" json:"user_prompt,omitempty"`
	WorkerPort       sql.NullInt64  `db:"worker_port" json:"worker_port,omitempty"`
	PromptCounter    int64          `db:"prompt_counter" json:"prompt_counter"`
	Status           SessionStatus  `db:"status" json:"status"`
	StartedAt        string         `db:"started_at" json:"started_at"`
	StartedAtEpoch   int64          `db:"started_at_epoch" json:"started_at_epoch"`
	CompletedAt      sql.NullString `db:"completed_at" json:"completed_at,omitempty"`
	CompletedAtEpoch sql.NullInt64  `db:"completed_at_epoch" json:"completed_at_epoch,omitempty"`
}

// ActiveSession represents an in-memory active session being processed.
type ActiveSession struct {
	SessionDBID            int64
	ClaudeSessionID        string
	SDKSessionID           string
	Project                string
	UserPrompt             string
	LastPromptNumber       int
	StartTime              time.Time
	CumulativeInputTokens  int64
	CumulativeOutputTokens int64
}
