package sdk

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTruncate(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{
			name:     "shorter_than_max",
			input:    "hello",
			maxLen:   10,
			expected: "hello",
		},
		{
			name:     "equal_to_max",
			input:    "hello",
			maxLen:   5,
			expected: "hello",
		},
		{
			name:     "longer_than_max",
			input:    "hello world",
			maxLen:   5,
			expected: "hello... (truncated)",
		},
		{
			name:     "empty_string",
			input:    "",
			maxLen:   5,
			expected: "",
		},
		{
			name:     "zero_max_length",
			input:    "hello",
			maxLen:   0,
			expected: "... (truncated)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncate(tt.input, tt.maxLen)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuildObservationPrompt(t *testing.T) {
	now := time.Now().UnixMilli()

	tests := []struct {
		name     string
		exec     ToolExecution
		contains []string
	}{
		{
			name: "basic_read_tool",
			exec: ToolExecution{
				ID:             1,
				ToolName:       "Read",
				ToolInput:      `{"file_path": "/path/to/file.go"}`,
				ToolOutput:     `package main\nfunc main() {}`,
				CreatedAtEpoch: now,
				CWD:            "/project",
			},
			contains: []string{
				"<observed_from_primary_session>",
				"<what_happened>Read</what_happened>",
				"<working_directory>/project</working_directory>",
				"<parameters>",
				"file_path",
				"<outcome>",
				"</observed_from_primary_session>",
			},
		},
		{
			name: "edit_tool_with_json_input",
			exec: ToolExecution{
				ID:             2,
				ToolName:       "Edit",
				ToolInput:      `{"file_path": "/file.go", "old_string": "foo", "new_string": "bar"}`,
				ToolOutput:     "Edit applied successfully",
				CreatedAtEpoch: now,
				CWD:            "",
			},
			contains: []string{
				"<what_happened>Edit</what_happened>",
				"file_path",
				"old_string",
				"new_string",
			},
		},
		{
			name: "no_cwd",
			exec: ToolExecution{
				ID:             3,
				ToolName:       "Bash",
				ToolInput:      `{"command": "go test"}`,
				ToolOutput:     "ok",
				CreatedAtEpoch: now,
				CWD:            "",
			},
			contains: []string{
				"<what_happened>Bash</what_happened>",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BuildObservationPrompt(tt.exec)

			for _, s := range tt.contains {
				assert.Contains(t, result, s, "Expected result to contain: %s", s)
			}

			// Check CWD only appears when set
			if tt.exec.CWD == "" {
				assert.NotContains(t, result, "<working_directory>")
			}
		})
	}
}

func TestBuildObservationPrompt_TruncatesLongContent(t *testing.T) {
	longInput := strings.Repeat("x", 5000)
	longOutput := strings.Repeat("y", 7000)

	exec := ToolExecution{
		ID:             1,
		ToolName:       "Read",
		ToolInput:      longInput,
		ToolOutput:     longOutput,
		CreatedAtEpoch: time.Now().UnixMilli(),
		CWD:            "/project",
	}

	result := BuildObservationPrompt(exec)

	// Input should be truncated to ~3000
	assert.Contains(t, result, "truncated")
	// The result should not be excessively long
	assert.Less(t, len(result), 10000)
}

func TestBuildSummaryPrompt(t *testing.T) {
	tests := []struct {
		name     string
		req      SummaryRequest
		contains []string
	}{
		{
			name: "basic_request",
			req: SummaryRequest{
				SessionDBID:  1,
				SDKSessionID: "sdk-123",
				Project:      "test-project",
			},
			contains: []string{
				"PROGRESS SUMMARY CHECKPOINT",
				"<summary>",
				"<request>",
				"<investigated>",
				"<learned>",
				"<completed>",
				"<next_steps>",
				"<notes>",
				"</summary>",
			},
		},
		{
			name: "with_assistant_message",
			req: SummaryRequest{
				SessionDBID:          2,
				SDKSessionID:         "sdk-456",
				Project:              "project-b",
				LastAssistantMessage: "I fixed the authentication bug by updating the JWT validation.",
			},
			contains: []string{
				"Claude's Full Response to User:",
				"fixed the authentication",
			},
		},
		{
			name: "empty_assistant_message",
			req: SummaryRequest{
				SessionDBID:          3,
				SDKSessionID:         "sdk-789",
				Project:              "project-c",
				LastAssistantMessage: "",
			},
			contains: []string{
				"PROGRESS SUMMARY CHECKPOINT",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BuildSummaryPrompt(tt.req)

			for _, s := range tt.contains {
				assert.Contains(t, result, s, "Expected result to contain: %s", s)
			}

			// Check assistant message only appears when set
			if tt.req.LastAssistantMessage == "" {
				assert.NotContains(t, result, "Claude's Full Response")
			}
		})
	}
}

func TestBuildSummaryPrompt_TruncatesLongAssistantMessage(t *testing.T) {
	longMessage := strings.Repeat("a", 5000)

	req := SummaryRequest{
		SessionDBID:          1,
		SDKSessionID:         "sdk-123",
		Project:              "test",
		LastAssistantMessage: longMessage,
	}

	result := BuildSummaryPrompt(req)

	// Should contain truncation indicator
	assert.Contains(t, result, "truncated")
	// Result should be reasonable length (less than full 5000 + overhead)
	assert.Less(t, len(result), 6000)
}

func TestToolExecution_Struct(t *testing.T) {
	exec := ToolExecution{
		ID:             42,
		ToolName:       "Write",
		ToolInput:      `{"file_path": "/test.go"}`,
		ToolOutput:     "File written",
		CreatedAtEpoch: 1234567890000,
		CWD:            "/workspace",
	}

	assert.Equal(t, int64(42), exec.ID)
	assert.Equal(t, "Write", exec.ToolName)
	assert.Equal(t, `{"file_path": "/test.go"}`, exec.ToolInput)
	assert.Equal(t, "File written", exec.ToolOutput)
	assert.Equal(t, int64(1234567890000), exec.CreatedAtEpoch)
	assert.Equal(t, "/workspace", exec.CWD)
}

func TestSummaryRequest_Struct(t *testing.T) {
	req := SummaryRequest{
		SessionDBID:          100,
		SDKSessionID:         "sdk-abc",
		Project:              "my-project",
		UserPrompt:           "Fix the bug",
		LastUserMessage:      "Please fix the auth bug",
		LastAssistantMessage: "I've fixed the authentication issue",
	}

	assert.Equal(t, int64(100), req.SessionDBID)
	assert.Equal(t, "sdk-abc", req.SDKSessionID)
	assert.Equal(t, "my-project", req.Project)
	assert.Equal(t, "Fix the bug", req.UserPrompt)
	assert.Equal(t, "Please fix the auth bug", req.LastUserMessage)
	assert.Equal(t, "I've fixed the authentication issue", req.LastAssistantMessage)
}
