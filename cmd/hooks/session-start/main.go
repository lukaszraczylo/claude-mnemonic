// Package main provides the session-start hook entry point.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/hooks"
)

// Input is the hook input from Claude Code.
type Input struct {
	SessionID      string `json:"session_id"`
	CWD            string `json:"cwd"`
	PermissionMode string `json:"permission_mode"`
	HookEventName  string `json:"hook_event_name"`
	Source         string `json:"source"` // "startup", "resume", "clear", "compact"
}

// Observation represents an observation from the API.
type Observation struct {
	ID        int64    `json:"id"`
	Type      string   `json:"type"`
	Title     string   `json:"title"`
	Subtitle  string   `json:"subtitle"`
	Narrative string   `json:"narrative"`
	Facts     []string `json:"facts"`
}

func main() {
	// Skip if this is an internal call (from SDK processor)
	if os.Getenv("CLAUDE_MNEMONIC_INTERNAL") == "1" {
		hooks.WriteResponse("SessionStart", true)
		return
	}

	// Read input from stdin
	inputData, err := io.ReadAll(os.Stdin)
	if err != nil {
		hooks.WriteError("SessionStart", err)
		os.Exit(1)
	}

	var input Input
	if err := json.Unmarshal(inputData, &input); err != nil {
		hooks.WriteError("SessionStart", err)
		os.Exit(1)
	}

	// Ensure worker is running
	port, err := hooks.EnsureWorkerRunning()
	if err != nil {
		hooks.WriteError("SessionStart", err)
		os.Exit(1)
	}

	// Generate unique project ID from CWD (dirname_hash format)
	project := hooks.ProjectIDWithName(input.CWD)

	// Fetch observations for context injection
	endpoint := fmt.Sprintf("/api/context/inject?project=%s&cwd=%s",
		url.QueryEscape(project),
		url.QueryEscape(input.CWD))

	result, err := hooks.GET(port, endpoint)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[claude-mnemonic] Warning: context fetch failed: %v\n", err)
		hooks.WriteResponse("SessionStart", true)
		return
	}

	// Parse observations from response
	obsData, ok := result["observations"].([]interface{})
	if !ok || len(obsData) == 0 {
		// No observations - just continue normally
		hooks.WriteResponse("SessionStart", true)
		return
	}

	// Get full_count from response (how many observations get full detail)
	fullCount := 25 // default
	if fc, ok := result["full_count"].(float64); ok && fc > 0 {
		fullCount = int(fc)
	}

	// Show count to user via stderr
	fmt.Fprintf(os.Stderr, "[claude-mnemonic] Injecting %d observations from project memory (%d detailed, %d condensed)\n",
		len(obsData), min(fullCount, len(obsData)), max(0, len(obsData)-fullCount))

	// Build context string
	contextBuilder := "<claude-mnemonic-context>\n"
	contextBuilder += fmt.Sprintf("# Project Memory (%d observations)\n", len(obsData))
	contextBuilder += "Use this knowledge to answer questions without re-exploring the codebase.\n\n"

	for i, o := range obsData {
		obs, ok := o.(map[string]interface{})
		if !ok {
			continue
		}

		title := getString(obs, "title")
		obsType := getString(obs, "type")

		// First `fullCount` observations get full detail, rest are condensed
		if i < fullCount {
			// Full detail: include narrative and facts
			narrative := getString(obs, "narrative")

			contextBuilder += fmt.Sprintf("## %d. [%s] %s\n", i+1, strings.ToUpper(obsType), title)
			if narrative != "" {
				contextBuilder += narrative + "\n"
			}

			if facts, ok := obs["facts"].([]interface{}); ok && len(facts) > 0 {
				contextBuilder += "Key facts:\n"
				for _, f := range facts {
					if fact, ok := f.(string); ok && fact != "" {
						contextBuilder += fmt.Sprintf("- %s\n", fact)
					}
				}
			}
			contextBuilder += "\n"
		} else {
			// Condensed: just title and subtitle (one line)
			subtitle := getString(obs, "subtitle")
			if subtitle != "" {
				contextBuilder += fmt.Sprintf("- [%s] %s: %s\n", strings.ToUpper(obsType), title, subtitle)
			} else {
				contextBuilder += fmt.Sprintf("- [%s] %s\n", strings.ToUpper(obsType), title)
			}
		}
	}

	contextBuilder += "</claude-mnemonic-context>\n"

	// Output context as JSON with additionalContext field
	response := map[string]interface{}{
		"continue": true,
		"hookSpecificOutput": map[string]interface{}{
			"hookEventName":     "SessionStart",
			"additionalContext": contextBuilder,
		},
	}
	_ = json.NewEncoder(os.Stdout).Encode(response)
	os.Exit(0)
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
