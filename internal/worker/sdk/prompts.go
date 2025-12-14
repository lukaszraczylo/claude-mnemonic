// Package sdk provides SDK agent integration for claude-mnemonic.
package sdk

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ObservationTypes defines valid observation types.
var ObservationTypes = []string{"bugfix", "feature", "refactor", "change", "discovery", "decision"}

// ObservationConcepts defines valid observation concepts.
var ObservationConcepts = []string{
	"how-it-works",
	"why-it-exists",
	"what-changed",
	"problem-solution",
	"gotcha",
	"pattern",
	"trade-off",
}

// ToolExecution represents a tool execution for observation.
type ToolExecution struct {
	ID             int64
	ToolName       string
	ToolInput      string
	ToolOutput     string
	CreatedAtEpoch int64
	CWD            string
}

// BuildObservationPrompt builds a prompt for processing a tool observation.
func BuildObservationPrompt(exec ToolExecution) string {
	// Safely parse tool_input and tool_output
	var toolInput interface{}
	var toolOutput interface{}

	if err := json.Unmarshal([]byte(exec.ToolInput), &toolInput); err != nil {
		toolInput = exec.ToolInput
	}

	if err := json.Unmarshal([]byte(exec.ToolOutput), &toolOutput); err != nil {
		toolOutput = exec.ToolOutput
	}

	inputJSON, _ := json.MarshalIndent(toolInput, "  ", "  ")
	outputJSON, _ := json.MarshalIndent(toolOutput, "  ", "  ")

	timestamp := time.UnixMilli(exec.CreatedAtEpoch).Format(time.RFC3339)

	var sb strings.Builder
	sb.WriteString("<observed_from_primary_session>\n")
	sb.WriteString(fmt.Sprintf("  <what_happened>%s</what_happened>\n", exec.ToolName))
	sb.WriteString(fmt.Sprintf("  <occurred_at>%s</occurred_at>\n", timestamp))
	if exec.CWD != "" {
		sb.WriteString(fmt.Sprintf("  <working_directory>%s</working_directory>\n", exec.CWD))
	}
	sb.WriteString(fmt.Sprintf("  <parameters>%s</parameters>\n", truncate(string(inputJSON), 3000)))
	sb.WriteString(fmt.Sprintf("  <outcome>%s</outcome>\n", truncate(string(outputJSON), 5000)))
	sb.WriteString("</observed_from_primary_session>")

	return sb.String()
}

// SummaryRequest contains data for building a summary prompt.
type SummaryRequest struct {
	SessionDBID          int64
	SDKSessionID         string
	Project              string
	UserPrompt           string
	LastUserMessage      string
	LastAssistantMessage string
}

// BuildSummaryPrompt builds a prompt requesting a session summary.
func BuildSummaryPrompt(req SummaryRequest) string {
	var sb strings.Builder

	sb.WriteString("PROGRESS SUMMARY CHECKPOINT\n")
	sb.WriteString("===========================\n")
	sb.WriteString("Write progress notes of what was done, what was learned, and what's next. This is a checkpoint to capture progress so far. The session is ongoing - you may receive more requests and tool executions after this summary. Write \"next_steps\" as the current trajectory of work (what's actively being worked on or coming up next), not as post-session future work. Always write at least a minimal summary explaining current progress, even if work is still in early stages, so that users see a summary output tied to each request.\n\n")

	if req.LastAssistantMessage != "" {
		sb.WriteString("Claude's Full Response to User:\n")
		sb.WriteString(truncate(req.LastAssistantMessage, 4000))
		sb.WriteString("\n\n")
	}

	sb.WriteString(`Respond in this XML format:
<summary>
  <request>[Short title capturing the user's request AND the substance of what was discussed/done]</request>
  <investigated>[What has been explored so far? What was examined?]</investigated>
  <learned>[What have you learned about how things work?]</learned>
  <completed>[What work has been completed so far? What has shipped or changed?]</completed>
  <next_steps>[What are you actively working on or planning to work on next in this session?]</next_steps>
  <notes>[Additional insights or observations about the current progress]</notes>
</summary>

IMPORTANT! DO NOT do any work right now other than generating this next PROGRESS SUMMARY - and remember that you are a memory agent designed to summarize a DIFFERENT claude code session, not this one.

Never reference yourself or your own actions. Do not output anything other than the summary content formatted in the XML structure above. All other output is ignored by the system, and the system has been designed to be smart about token usage. Please spend your tokens wisely on useful summary content.

Thank you, this summary will be very useful for keeping track of our progress!`)

	return sb.String()
}

// truncate truncates a string to the specified length.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "... (truncated)"
}
