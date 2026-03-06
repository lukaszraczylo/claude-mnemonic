// Package main provides the session-start hook entry point.
package main

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

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
	if !hooks.IsWorkerAvailable() {
		hooks.WriteResponse("SessionStart", true)
		return
	}
	hooks.RunHook("SessionStart", handleSessionStart)
}

func handleSessionStart(ctx *hooks.HookContext, input *Input) (string, error) {
	deadline, cancel := hooks.HookDeadline(30 * time.Second)
	defer cancel()

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

	// Token budget for context injection
	maxTokens := 16000 // default; could be made configurable via worker config endpoint
	currentTokens := 0

	// Build context string
	header := fmt.Sprintf("<claude-mnemonic-context>\n# Project Memory (%d observations)\nUse this knowledge to answer questions without re-exploring the codebase.\n\n", len(obsData))
	currentTokens += estimateTokens(header)
	contextBuilder := header

	for i, o := range obsData {
		if deadline.Err() != nil {
			contextBuilder += "\n... (returning early due to time limit)\n"
			break
		}

		obs, ok := o.(map[string]interface{})
		if !ok {
			continue
		}

		title := getString(obs, "title")
		obsType := getString(obs, "type")

		var obsText string

		// First `fullCount` observations get full detail, rest are condensed
		if i < fullCount {
			// Full detail: include narrative and facts
			narrative := getString(obs, "narrative")

			obsText = fmt.Sprintf("## %d. [%s] %s\n", i+1, strings.ToUpper(obsType), title)
			if narrative != "" {
				obsText += narrative + "\n"
			}

			if facts, ok := obs["facts"].([]interface{}); ok && len(facts) > 0 {
				obsText += "Key facts:\n"
				for _, f := range facts {
					if fact, ok := f.(string); ok && fact != "" {
						obsText += fmt.Sprintf("- %s\n", fact)
					}
				}
			}
			obsText += "\n"
		} else {
			// Condensed: just title and subtitle (one line)
			subtitle := getString(obs, "subtitle")
			if subtitle != "" {
				obsText = fmt.Sprintf("- [%s] %s: %s\n", strings.ToUpper(obsType), title, subtitle)
			} else {
				obsText = fmt.Sprintf("- [%s] %s\n", strings.ToUpper(obsType), title)
			}
		}

		obsTokens := estimateTokens(obsText)
		if currentTokens+obsTokens > maxTokens {
			contextBuilder += fmt.Sprintf("\n... (%d more observations omitted due to token budget)\n", len(obsData)-i)
			break
		}

		contextBuilder += obsText
		currentTokens += obsTokens
	}

	contextBuilder += "</claude-mnemonic-context>\n"
	return contextBuilder, nil
}

// estimateTokens provides a more accurate token count estimate.
// Uses word count * 1.3 as base, with adjustments for code and non-ASCII.
func estimateTokens(s string) int {
	if len(s) == 0 {
		return 0
	}

	// Count words (split on whitespace)
	words := len(strings.Fields(s))
	if words == 0 {
		// No whitespace = probably a single token or code blob
		return (len(s) + 3) / 4
	}

	// Base estimate: ~1.3 tokens per word for English text
	estimate := int(float64(words) * 1.3)

	// Detect code-heavy content (high non-alpha ratio)
	nonAlpha := 0
	nonASCII := 0
	for _, r := range s {
		if r > 127 {
			nonASCII++
		} else if !('a' <= r && r <= 'z') && !('A' <= r && r <= 'Z') && !('0' <= r && r <= '9') && r != ' ' {
			nonAlpha++
		}
	}

	totalChars := len(s)

	// Code adjustment: more special chars = more tokens per word
	if totalChars > 0 && float64(nonAlpha)/float64(totalChars) > 0.15 {
		estimate = int(float64(estimate) * 1.3)
	}

	// Non-ASCII adjustment: CJK and other scripts use more tokens
	if totalChars > 0 && float64(nonASCII)/float64(totalChars) > 0.1 {
		estimate += nonASCII // Roughly 1 extra token per non-ASCII char
	}

	return estimate
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
