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
	ClaudeSessionID  string         `db:"claude_session_id" json:"claude_session_id"`
	Project          string         `db:"project" json:"project"`
	Status           SessionStatus  `db:"status" json:"status"`
	StartedAt        string         `db:"started_at" json:"started_at"`
	SDKSessionID     sql.NullString `db:"sdk_session_id" json:"sdk_session_id,omitempty"`
	UserPrompt       sql.NullString `db:"user_prompt" json:"user_prompt,omitempty"`
	CompletedAt      sql.NullString `db:"completed_at" json:"completed_at,omitempty"`
	WorkerPort       sql.NullInt64  `db:"worker_port" json:"worker_port,omitempty"`
	CompletedAtEpoch sql.NullInt64  `db:"completed_at_epoch" json:"completed_at_epoch,omitempty"`
	ID               int64          `db:"id" json:"id"`
	PromptCounter    int64          `db:"prompt_counter" json:"prompt_counter"`
	StartedAtEpoch   int64          `db:"started_at_epoch" json:"started_at_epoch"`
}

// ActiveSession represents an in-memory active session being processed.
type ActiveSession struct {
	StartTime              time.Time
	ClaudeSessionID        string
	SDKSessionID           string
	Project                string
	UserPrompt             string
	SessionDBID            int64
	LastPromptNumber       int
	CumulativeInputTokens  int64
	CumulativeOutputTokens int64
}
