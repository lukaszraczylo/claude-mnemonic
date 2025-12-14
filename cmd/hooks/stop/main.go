// Package main provides the stop hook entry point.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
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
	// Skip if this is an internal call (from SDK processor)
	if os.Getenv("CLAUDE_MNEMONIC_INTERNAL") == "1" {
		hooks.WriteResponse("Stop", true)
		return
	}

	// Read input from stdin
	inputData, err := io.ReadAll(os.Stdin)
	if err != nil {
		hooks.WriteError("Stop", err)
		os.Exit(1)
	}

	var input Input
	if err := json.Unmarshal(inputData, &input); err != nil {
		hooks.WriteError("Stop", err)
		os.Exit(1)
	}

	// Ensure worker is running
	port, err := hooks.EnsureWorkerRunning()
	if err != nil {
		hooks.WriteError("Stop", err)
		os.Exit(1)
	}

	// Find session
	result, err := hooks.GET(port, fmt.Sprintf("/api/sessions?claudeSessionId=%s", input.SessionID))
	if err != nil || result == nil {
		// Session might not exist, that's OK
		hooks.WriteResponse("Stop", true)
		return
	}

	sessionID, ok := result["id"].(float64)
	if !ok {
		hooks.WriteResponse("Stop", true)
		return
	}

	// Parse transcript to get last messages for summary context
	lastUser, lastAssistant := "", ""
	if input.TranscriptPath != "" {
		lastUser, lastAssistant = parseTranscript(input.TranscriptPath)
	}

	fmt.Fprintf(os.Stderr, "[stop] Requesting summary for session %d (transcript: %v)\n", int64(sessionID), input.TranscriptPath != "")

	// Request summary with message context from transcript
	_, err = hooks.POST(port, fmt.Sprintf("/sessions/%d/summarize", int64(sessionID)), map[string]interface{}{
		"lastUserMessage":      lastUser,
		"lastAssistantMessage": lastAssistant,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "[stop] Warning: summary request failed: %v\n", err)
	}

	hooks.WriteResponse("Stop", true)
}
