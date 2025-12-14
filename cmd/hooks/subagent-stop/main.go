// Package main provides the subagent-stop hook entry point.
// This hook fires when a Task/subagent completes, capturing observations from subagent work.
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
	SessionID      string `json:"session_id"`
	CWD            string `json:"cwd"`
	PermissionMode string `json:"permission_mode"`
	HookEventName  string `json:"hook_event_name"`
	StopHookActive bool   `json:"stop_hook_active"`
}

func main() {
	// Skip if this is an internal call (from SDK processor)
	if os.Getenv("CLAUDE_MNEMONIC_INTERNAL") == "1" {
		hooks.WriteResponse("SubagentStop", true)
		return
	}

	// Read input from stdin
	inputData, err := io.ReadAll(os.Stdin)
	if err != nil {
		hooks.WriteError("SubagentStop", err)
		os.Exit(1)
	}

	var input Input
	if err := json.Unmarshal(inputData, &input); err != nil {
		hooks.WriteError("SubagentStop", err)
		os.Exit(1)
	}

	// Ensure worker is running
	port, err := hooks.EnsureWorkerRunning()
	if err != nil {
		hooks.WriteError("SubagentStop", err)
		os.Exit(1)
	}

	// Generate unique project ID from CWD
	project := hooks.ProjectIDWithName(input.CWD)

	fmt.Fprintf(os.Stderr, "[subagent-stop] Subagent completed in project %s\n", project)

	// Notify worker that a subagent completed
	// This can trigger processing of any queued observations from the subagent
	_, err = hooks.POST(port, "/api/sessions/subagent-complete", map[string]interface{}{
		"claudeSessionId": input.SessionID,
		"project":         project,
	})
	if err != nil {
		// Non-fatal - just log warning
		fmt.Fprintf(os.Stderr, "[subagent-stop] Warning: failed to notify worker: %v\n", err)
	}

	hooks.WriteResponse("SubagentStop", true)
}
