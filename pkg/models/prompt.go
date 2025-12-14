// Package models contains domain models for claude-mnemonic.
package models

// UserPrompt represents a user prompt captured during a session.
type UserPrompt struct {
	ID                  int64  `db:"id" json:"id"`
	ClaudeSessionID     string `db:"claude_session_id" json:"claude_session_id"`
	PromptNumber        int    `db:"prompt_number" json:"prompt_number"`
	PromptText          string `db:"prompt_text" json:"prompt_text"`
	MatchedObservations int    `db:"matched_observations" json:"matched_observations"`
	CreatedAt           string `db:"created_at" json:"created_at"`
	CreatedAtEpoch      int64  `db:"created_at_epoch" json:"created_at_epoch"`
}

// UserPromptWithSession includes session context for search results.
type UserPromptWithSession struct {
	UserPrompt
	Project      string `db:"project" json:"project"`
	SDKSessionID string `db:"sdk_session_id" json:"sdk_session_id"`
}
