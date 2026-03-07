// Package main provides the user-prompt hook entry point.
package main

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/hooks"
	"github.com/lukaszraczylo/claude-mnemonic/pkg/sanitize"
)

// Input is the hook input from Claude Code.
type Input struct {
	hooks.BaseInput
	Prompt string `json:"prompt"`
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

func main() {
	if !hooks.IsWorkerAvailable() {
		hooks.WriteResponse("UserPromptSubmit", true)
		return
	}
	hooks.RunHook("UserPromptSubmit", handleUserPrompt)
}

func handleUserPrompt(ctx *hooks.HookContext, input *Input) (string, error) {
	deadline, cancel := hooks.HookDeadline(10 * time.Second)
	defer cancel()

	searchURL := fmt.Sprintf("/api/context/search?project=%s&query=%s&cwd=%s",
		url.QueryEscape(ctx.Project),
		url.QueryEscape(input.Prompt),
		url.QueryEscape(ctx.CWD))

	// Run search and session init concurrently.
	// Session init doesn't strictly depend on search results -- the observation
	// count passed is approximate (0) and acceptable.
	var (
		wg               sync.WaitGroup
		searchResult     map[string]interface{}
		initResult       map[string]interface{}
		initErr          error
		contextToInject  string
		observationCount int
	)

	// Start search in background
	wg.Add(1)
	go func() {
		defer wg.Done()
		searchResult, _ = hooks.GET(ctx.Port, searchURL)
	}()

	// Start session init in parallel (with observationCount=0; approximate is fine)
	wg.Add(1)
	go func() {
		defer wg.Done()
		initResult, initErr = hooks.POST(ctx.Port, "/api/sessions/init", map[string]interface{}{
			"claudeSessionId":     ctx.SessionID,
			"project":             ctx.Project,
			"prompt":              input.Prompt,
			"matchedObservations": 0,
		})
	}()

	// Wait for both to complete
	wg.Wait()

	// Check deadline after network calls
	if deadline.Err() != nil {
		return "", nil
	}

	// Process search results
	if observations, ok := searchResult["observations"].([]interface{}); ok && len(observations) > 0 {
		observationCount = len(observations)

		// Token budget for prompt context injection
		maxTokens := 8000
		currentTokens := 0

		header := "<relevant-memory>\n# Relevant Knowledge From Previous Sessions\nIMPORTANT: Use this information to answer the question directly. Do NOT explore the codebase if the answer is here.\n\n"
		currentTokens += estimateTokens(header)
		var contextBuilder string
		contextBuilder = header

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

				var obsText string
				obsText = fmt.Sprintf("## %d. [%s] %s\n", i+1, obsType, title)

				if facts, ok := obsMap["facts"].([]interface{}); ok && len(facts) > 0 {
					obsText += "Key facts:\n"
					for _, fact := range facts {
						if factStr, ok := fact.(string); ok {
							obsText += fmt.Sprintf("- %s\n", sanitize.StripSystemXML(factStr))
						}
					}
					obsText += "\n"
				}

				if narrative, ok := obsMap["narrative"].(string); ok && narrative != "" {
					obsText += sanitize.StripSystemXML(narrative) + "\n\n"
				}

				obsTokens := estimateTokens(obsText)
				if currentTokens+obsTokens > maxTokens {
					break
				}

				contextBuilder += obsText
				currentTokens += obsTokens
			}
		}

		contextBuilder += "</relevant-memory>\n"
		contextToInject = contextBuilder
	}

	// Check session init result
	if initErr != nil {
		return "", initErr
	}

	// Check if skipped due to privacy
	if skipped, ok := initResult["skipped"].(bool); ok && skipped {
		fmt.Fprintf(os.Stderr, "[user-prompt] Session skipped (private)\n")
		return "", nil
	}

	sessionID := int64(initResult["sessionDbId"].(float64))
	promptNumber := int(initResult["promptNumber"].(float64))

	fmt.Fprintf(os.Stderr, "[user-prompt] Session %d, prompt #%d\n", sessionID, promptNumber)

	// Start SDK agent (depends on session init result, so kept sequential)
	_, err := hooks.POST(ctx.Port, fmt.Sprintf("/sessions/%d/init", sessionID), map[string]interface{}{
		"userPrompt":   input.Prompt,
		"promptNumber": promptNumber,
	})
	if err != nil {
		return "", err
	}

	// Return context if we found relevant observations
	if observationCount > 0 {
		fmt.Fprintf(os.Stderr, "[claude-mnemonic] Found %d relevant memories for this prompt\n", observationCount)
		return contextToInject, nil
	}

	return "", nil
}
