// Package main provides the user-prompt hook entry point.
package main

import (
	"fmt"
	"net/url"
	"os"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/hooks"
)

// Input is the hook input from Claude Code.
type Input struct {
	hooks.BaseInput
	Prompt string `json:"prompt"`
}

func main() {
	hooks.RunHook("UserPromptSubmit", handleUserPrompt)
}

func handleUserPrompt(ctx *hooks.HookContext, input *Input) (string, error) {
	// Search for relevant observations based on the prompt
	searchURL := fmt.Sprintf("/api/context/search?project=%s&query=%s&cwd=%s",
		url.QueryEscape(ctx.Project),
		url.QueryEscape(input.Prompt),
		url.QueryEscape(ctx.CWD))

	var contextToInject string
	var observationCount int

	searchResult, _ := hooks.GET(ctx.Port, searchURL)
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
	result, err := hooks.POST(ctx.Port, "/api/sessions/init", map[string]interface{}{
		"claudeSessionId":     ctx.SessionID,
		"project":             ctx.Project,
		"prompt":              input.Prompt,
		"matchedObservations": observationCount,
	})
	if err != nil {
		return "", err
	}

	// Check if skipped due to privacy
	if skipped, ok := result["skipped"].(bool); ok && skipped {
		fmt.Fprintf(os.Stderr, "[user-prompt] Session skipped (private)\n")
		return "", nil
	}

	// Safely extract session ID and prompt number with type checking
	sessionDbIdRaw, ok := result["sessionDbId"].(float64)
	if !ok {
		return "", fmt.Errorf("invalid or missing sessionDbId in response")
	}
	sessionID := int64(sessionDbIdRaw)

	promptNumberRaw, ok := result["promptNumber"].(float64)
	if !ok {
		return "", fmt.Errorf("invalid or missing promptNumber in response")
	}
	promptNumber := int(promptNumberRaw)

	fmt.Fprintf(os.Stderr, "[user-prompt] Session %d, prompt #%d\n", sessionID, promptNumber)

	// Start SDK agent
	_, err = hooks.POST(ctx.Port, fmt.Sprintf("/sessions/%d/init", sessionID), map[string]interface{}{
		"userPrompt":   input.Prompt,
		"promptNumber": promptNumber,
	})
	if err != nil {
		return "", err
	}

	// Return context if we found relevant observations
	if observationCount > 0 {
		// Show match count to user via stderr
		fmt.Fprintf(os.Stderr, "[claude-mnemonic] Found %d relevant memories for this prompt\n", observationCount)
		return contextToInject, nil
	}

	return "", nil
}
