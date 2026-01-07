// Package main provides the stop hook entry point.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/hooks"
)

// Input is the hook input from Claude Code.
type Input struct {
	hooks.BaseInput
	TranscriptPath string `json:"transcript_path"`
	StopHookActive bool   `json:"stop_hook_active"`
}

// TranscriptMessage represents a message in the transcript JSONL file.
type TranscriptMessage struct {
	Message struct {
		Content any    `json:"content"`
		Role    string `json:"role"`
	} `json:"message"`
	Type string `json:"type"` // Can be string or array
}

// extractTextContent extracts text content from message content (handles both string and array formats).
func extractTextContent(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		var texts []string
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				if m["type"] == "text" {
					if text, ok := m["text"].(string); ok {
						texts = append(texts, text)
					}
				}
			}
		}
		return strings.Join(texts, "\n")
	}
	return ""
}

// parseTranscript reads the transcript file and extracts the last user and assistant messages.
func parseTranscript(path string) (lastUser, lastAssistant string) {
	// Expand ~ to home directory
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err == nil {
			path = strings.Replace(path, "~", home, 1)
		}
	}

	file, err := os.Open(path) // #nosec G304 -- path is from internal conversation file location
	if err != nil {
		return "", ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	// Increase buffer size for large messages
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		var msg TranscriptMessage
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			continue
		}

		// Transcript entries have type: "user" or "assistant" (not "message")
		// Check if this is a user/assistant message with content
		if msg.Type == "user" || msg.Type == "assistant" {
			text := extractTextContent(msg.Message.Content)
			if text == "" {
				continue
			}

			// Use the outer type field for role (message.role may differ)
			switch msg.Type {
			case "user":
				lastUser = text
			case "assistant":
				lastAssistant = text
			}
		}
	}

	return lastUser, lastAssistant
}

func main() {
	hooks.RunHook("Stop", handleStop)
}

func handleStop(ctx *hooks.HookContext, input *Input) (string, error) {
	// Debug: dump raw input
	fmt.Fprintf(os.Stderr, "[stop] Raw input: %s\n", string(ctx.RawInput))

	// Find session
	result, err := hooks.GET(ctx.Port, fmt.Sprintf("/api/sessions?claudeSessionId=%s", ctx.SessionID))
	if err != nil || result == nil {
		// Session might not exist, that's OK
		return "", nil
	}

	sessionID, ok := result["id"].(float64)
	if !ok {
		return "", nil
	}

	// Parse transcript to get last messages for summary context
	lastUser, lastAssistant := "", ""
	if input.TranscriptPath != "" {
		lastUser, lastAssistant = parseTranscript(input.TranscriptPath)
	}

	// Debug: log what we extracted
	fmt.Fprintf(os.Stderr, "[stop] Transcript path: %s\n", input.TranscriptPath)
	fmt.Fprintf(os.Stderr, "[stop] Last user message length: %d\n", len(lastUser))
	fmt.Fprintf(os.Stderr, "[stop] Last assistant message length: %d\n", len(lastAssistant))
	if len(lastAssistant) > 0 {
		preview := lastAssistant
		if len(preview) > 300 {
			preview = preview[:300] + "..."
		}
		fmt.Fprintf(os.Stderr, "[stop] Last assistant preview: %s\n", preview)
	}
	fmt.Fprintf(os.Stderr, "[stop] Requesting summary for session %d (transcript: %v)\n", int64(sessionID), input.TranscriptPath != "")

	// Request summary with message context from transcript
	_, err = hooks.POST(ctx.Port, fmt.Sprintf("/sessions/%d/summarize", int64(sessionID)), map[string]interface{}{
		"lastUserMessage":      lastUser,
		"lastAssistantMessage": lastAssistant,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "[stop] Warning: summary request failed: %v\n", err)
	}

	return "", nil
}
