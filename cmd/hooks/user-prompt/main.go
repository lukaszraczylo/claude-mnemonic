// Package main provides the user-prompt hook entry point.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/hooks"
)

// Input is the hook input from Claude Code.
type Input struct {
	SessionID      string `json:"session_id"`
	CWD            string `json:"cwd"`
	PermissionMode string `json:"permission_mode"`
	HookEventName  string `json:"hook_event_name"`
	Prompt         string `json:"prompt"`
}

func main() {
	// Skip if this is an internal call (from SDK processor)
	if os.Getenv("CLAUDE_MNEMONIC_INTERNAL") == "1" {
		hooks.WriteResponse("UserPromptSubmit", true)
		return
	}

	// Read input from stdin
	inputData, err := io.ReadAll(os.Stdin)
	if err != nil {
		hooks.WriteError("UserPromptSubmit", err)
		os.Exit(1)
	}

	var input Input
	if err := json.Unmarshal(inputData, &input); err != nil {
		hooks.WriteError("UserPromptSubmit", err)
		os.Exit(1)
	}

	// Ensure worker is running
	port, err := hooks.EnsureWorkerRunning()
	if err != nil {
		hooks.WriteError("UserPromptSubmit", err)
		os.Exit(1)
	}

	// Generate unique project ID from CWD
	project := hooks.ProjectIDWithName(input.CWD)

	// Search for relevant observations based on the prompt
	searchURL := fmt.Sprintf("/api/context/search?project=%s&query=%s&cwd=%s",
		url.QueryEscape(project),
		url.QueryEscape(input.Prompt),
		url.QueryEscape(input.CWD))

	var contextToInject string
	var observationCount int

	searchResult, _ := hooks.GET(port, searchURL)
	if observations, ok := searchResult["observations"].([]interface{}); ok && len(observations) > 0 {
		// Results are already filtered by relevance threshold and capped by max_results
		// from the server-side config (ContextRelevanceThreshold, ContextMaxPromptResults)
		observationCount = len(observations)

		// Build context from search results
		var contextBuilder string
		contextBuilder = "<relevant-memory>\n"
		contextBuilder += "# Relevant Knowledge From Previous Sessions\n"
		contextBuilder += "IMPORTANT: Use this information to answer the question directly. Do NOT explore the codebase if the answer is here.\n\n"

		for i, obs := range observations {
			if obsMap, ok := obs.(map[string]interface{}); ok {
				title := ""
				if t, ok := obsMap["title"].(string); ok {
					title = t
				}
				obsType := ""
				if t, ok := obsMap["type"].(string); ok {
					obsType = t
				}

				// Start observation block
				contextBuilder += fmt.Sprintf("## %d. [%s] %s\n", i+1, obsType, title)

				// Add facts first (most concise answers)
				if facts, ok := obsMap["facts"].([]interface{}); ok && len(facts) > 0 {
					contextBuilder += "Key facts:\n"
					for _, fact := range facts {
						if factStr, ok := fact.(string); ok {
							contextBuilder += fmt.Sprintf("- %s\n", factStr)
						}
					}
					contextBuilder += "\n"
				}

				// Add narrative if present
				if narrative, ok := obsMap["narrative"].(string); ok && narrative != "" {
					contextBuilder += narrative + "\n\n"
				}
			}
		}

		contextBuilder += "</relevant-memory>\n"

		contextToInject = contextBuilder
	}

	// Initialize session with matched observations count
	result, err := hooks.POST(port, "/api/sessions/init", map[string]interface{}{
		"claudeSessionId":     input.SessionID,
		"project":             project,
		"prompt":              input.Prompt,
		"matchedObservations": observationCount,
	})
	if err != nil {
		hooks.WriteError("UserPromptSubmit", err)
		os.Exit(1)
	}

	// Check if skipped due to privacy
	if skipped, ok := result["skipped"].(bool); ok && skipped {
		fmt.Fprintf(os.Stderr, "[user-prompt] Session skipped (private)\n")
		hooks.WriteResponse("UserPromptSubmit", true)
		return
	}

	sessionID := int64(result["sessionDbId"].(float64))
	promptNumber := int(result["promptNumber"].(float64))

	fmt.Fprintf(os.Stderr, "[user-prompt] Session %d, prompt #%d\n", sessionID, promptNumber)

	// Start SDK agent
	_, err = hooks.POST(port, fmt.Sprintf("/sessions/%d/init", sessionID), map[string]interface{}{
		"userPrompt":   input.Prompt,
		"promptNumber": promptNumber,
	})
	if err != nil {
		hooks.WriteError("UserPromptSubmit", err)
		os.Exit(1)
	}

	// Output results - stdout with exit 0 adds context to Claude's prompt
	if observationCount > 0 {
		// Show match count to user via stderr
		fmt.Fprintf(os.Stderr, "[claude-mnemonic] Found %d relevant memories for this prompt\n", observationCount)
		// Output context as JSON with additionalContext field
		response := map[string]interface{}{
			"continue": true,
			"hookSpecificOutput": map[string]interface{}{
				"hookEventName":     "UserPromptSubmit",
				"additionalContext": contextToInject,
			},
		}
		_ = json.NewEncoder(os.Stdout).Encode(response)
		os.Exit(0)
	} else {
		hooks.WriteResponse("UserPromptSubmit", true)
	}
}
