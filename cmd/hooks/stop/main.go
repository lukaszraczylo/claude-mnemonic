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
	StopHookActive bool   `json:"stop_hook_active"`
	TranscriptPath string `json:"transcript_path"`
}

// TranscriptMessage represents a message in the transcript JSONL file.
type TranscriptMessage struct {
	Type    string `json:"type"`
	Message struct {
		Role    string `json:"role"`
		Content any    `json:"content"` // Can be string or array
	} `json:"message"`
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

		if msg.Type == "message" {
			text := extractTextContent(msg.Message.Content)
			if text == "" {
				continue
			}

			switch msg.Message.Role {
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
