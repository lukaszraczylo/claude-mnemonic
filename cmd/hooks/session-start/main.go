// Package main provides the session-start hook entry point.
package main

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/hooks"
)

// Input is the hook input from Claude Code.
type Input struct {
	hooks.BaseInput
	Source string `json:"source"` // "startup", "resume", "clear", "compact"
}

// Observation represents an observation from the API.
type Observation struct {
	Type      string   `json:"type"`
	Title     string   `json:"title"`
	Subtitle  string   `json:"subtitle"`
	Narrative string   `json:"narrative"`
	Facts     []string `json:"facts"`
	ID        int64    `json:"id"`
}

func main() {
	hooks.RunHook("SessionStart", handleSessionStart)
}

func handleSessionStart(ctx *hooks.HookContext, input *Input) (string, error) {
	// Fetch observations for context injection
	endpoint := fmt.Sprintf("/api/context/inject?project=%s&cwd=%s",
		url.QueryEscape(ctx.Project),
		url.QueryEscape(ctx.CWD))

	result, err := hooks.GET(ctx.Port, endpoint)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[claude-mnemonic] Warning: context fetch failed: %v\n", err)
		return "", nil
	}

	// Parse observations from response
	obsData, ok := result["observations"].([]interface{})
	if !ok || len(obsData) == 0 {
		// No observations - just continue normally
		return "", nil
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
	return contextBuilder, nil
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
