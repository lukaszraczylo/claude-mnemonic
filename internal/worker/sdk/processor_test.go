package sdk

import (
	"testing"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
)

func TestIsSelfReferentialSummary(t *testing.T) {
	tests := []struct {
		name     string
		summary  *models.ParsedSummary
		expected bool
	}{
		{
			name: "meta summary about memory agent role",
			summary: &models.ParsedSummary{
				Request:   "Memory extraction agent role - analyze tool executions and extract meaningful observations for future sessions",
				Completed: "No work has been completed yet. The session has just started with the user providing role definition and operational guidelines.",
				Learned:   "The system expects observations to be created from meaningful learnings during Claude Code sessions, with focus on decisions, bugs fixed, patterns discovered, project structure changes, and code modifications.",
				NextSteps: "Awaiting tool executions or user requests that contain actual work performed in a Claude Code session.",
			},
			expected: true,
		},
		{
			name: "legitimate summary about code changes",
			summary: &models.ParsedSummary{
				Request:   "Fix authentication bug in login handler",
				Completed: "Updated the auth middleware to properly validate JWT tokens and fixed the session expiry check.",
				Learned:   "The JWT library requires explicit algorithm validation to prevent token substitution attacks.",
				NextSteps: "Add unit tests for the authentication flow.",
			},
			expected: false,
		},
		{
			name: "awaiting user summary",
			summary: &models.ParsedSummary{
				Request:   "Session initialization",
				Completed: "No work completed yet.",
				Learned:   "Awaiting user input to begin work.",
				NextSteps: "Waiting for the user to provide instructions.",
			},
			expected: true,
		},
		{
			name: "summary about refactoring",
			summary: &models.ParsedSummary{
				Request:   "Refactor database connection pooling",
				Completed: "Implemented connection pooling using pgxpool with max 10 connections.",
				Learned:   "pgxpool automatically handles connection reuse and health checks.",
				NextSteps: "Run benchmarks to verify performance improvement.",
			},
			expected: false,
		},
		{
			name: "meta summary with extraction agent mention",
			summary: &models.ParsedSummary{
				Request:   "Extraction agent initialization",
				Completed: "No substantive work has been done.",
				Learned:   "The memory extraction agent analyzes tool executions.",
				NextSteps: "Awaiting tool results to extract observations.",
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isSelfReferentialSummary(tt.summary)
			if result != tt.expected {
				t.Errorf("isSelfReferentialSummary() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestHasMeaningfulContent(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected bool
	}{
		{
			name:     "empty content",
			content:  "",
			expected: false,
		},
		{
			name:     "too short content",
			content:  "Hello world",
			expected: false,
		},
		{
			name: "meta content about memory agent",
			content: `This is the memory extraction agent role definition.
The system expects you to analyze tool executions and extract meaningful observations.
No work has been completed yet. Awaiting tool results from the user's session.`,
			expected: false,
		},
		{
			name: "legitimate code discussion",
			content: `I've updated the handler.go file to fix the authentication bug.
The function validateToken() was not checking token expiry correctly.
I've added a check for exp claim and implemented proper error handling.
The changes have been tested and the build passes.`,
			expected: true,
		},
		{
			name: "hook status messages",
			content: `SessionStart:Callback hook success: Success
The memory agent is waiting for user input.
System-reminder about available tools.
No substantive work performed yet.`,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasMeaningfulContent(tt.content)
			if result != tt.expected {
				t.Errorf("hasMeaningfulContent() = %v, want %v", result, tt.expected)
			}
		})
	}
}
