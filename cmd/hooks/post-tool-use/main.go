// Package main provides the post-tool-use hook entry point.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/hooks"
)

// Input is the hook input from Claude Code.
type Input struct {
	SessionID      string      `json:"session_id"`
	CWD            string      `json:"cwd"`
	PermissionMode string      `json:"permission_mode"`
	HookEventName  string      `json:"hook_event_name"`
	ToolName       string      `json:"tool_name"`
	ToolInput      interface{} `json:"tool_input"`
	ToolResponse   interface{} `json:"tool_response"`
	ToolUseID      string      `json:"tool_use_id"`
}

func main() {
	// Skip if this is an internal call (from SDK processor)
	if os.Getenv("CLAUDE_MNEMONIC_INTERNAL") == "1" {
		hooks.WriteResponse("PostToolUse", true)
		return
	}

	// Read input from stdin
	inputData, err := io.ReadAll(os.Stdin)
	if err != nil {
		hooks.WriteError("PostToolUse", err)
		os.Exit(1)
	}

	var input Input
	if err := json.Unmarshal(inputData, &input); err != nil {
		hooks.WriteError("PostToolUse", err)
		os.Exit(1)
	}

	// Ensure worker is running
	port, err := hooks.EnsureWorkerRunning()
	if err != nil {
		hooks.WriteError("PostToolUse", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "[post-tool-use] %s\n", input.ToolName)

	// Generate project ID from CWD (same logic as user-prompt hook)
	project := hooks.ProjectIDWithName(input.CWD)

	// Send observation to worker
	_, err = hooks.POST(port, "/api/sessions/observations", map[string]interface{}{
		"claudeSessionId": input.SessionID,
		"project":         project,
		"tool_name":       input.ToolName,
		"tool_input":      input.ToolInput,
		"tool_response":   input.ToolResponse,
		"cwd":             input.CWD,
	})
	if err != nil {
		hooks.WriteError("PostToolUse", err)
		os.Exit(1)
	}

	hooks.WriteResponse("PostToolUse", true)
}
