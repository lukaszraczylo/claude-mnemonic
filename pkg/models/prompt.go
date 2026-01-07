// Package models contains domain models for claude-mnemonic.
package models

// UserPrompt represents a user prompt captured during a session.
type UserPrompt struct {
	ClaudeSessionID     string `db:"claude_session_id" json:"claude_session_id"`
	PromptText          string `db:"prompt_text" json:"prompt_text"`
	CreatedAt           string `db:"created_at" json:"created_at"`
	ID                  int64  `db:"id" json:"id"`
	PromptNumber        int    `db:"prompt_number" json:"prompt_number"`
	MatchedObservations int    `db:"matched_observations" json:"matched_observations"`
	CreatedAtEpoch      int64  `db:"created_at_epoch" json:"created_at_epoch"`
}

// UserPromptWithSession includes session context for search results.
type UserPromptWithSession struct {
	Project      string `db:"project" json:"project"`
	SDKSessionID string `db:"sdk_session_id" json:"sdk_session_id"`
	UserPrompt
}
