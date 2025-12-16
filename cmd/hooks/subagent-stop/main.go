// Package main provides the subagent-stop hook entry point.
// This hook fires when a Task/subagent completes, capturing observations from subagent work.
package main

import (
	"fmt"
	"os"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/hooks"
)

// Input is the hook input from Claude Code.
type Input struct {
	hooks.BaseInput
	StopHookActive bool `json:"stop_hook_active"`
}

func main() {
	hooks.RunHook("SubagentStop", handleSubagentStop)
}

func handleSubagentStop(ctx *hooks.HookContext, input *Input) (string, error) {
	fmt.Fprintf(os.Stderr, "[subagent-stop] Subagent completed in project %s\n", ctx.Project)

	// Notify worker that a subagent completed
	// This can trigger processing of any queued observations from the subagent
	_, err := hooks.POST(ctx.Port, "/api/sessions/subagent-complete", map[string]interface{}{
		"claudeSessionId": ctx.SessionID,
		"project":         ctx.Project,
	})
	if err != nil {
		// Non-fatal - just log warning
		fmt.Fprintf(os.Stderr, "[subagent-stop] Warning: failed to notify worker: %v\n", err)
	}

	return "", nil
}
