// Package main provides the post-tool-use hook entry point.
package main

import (
	"fmt"
	"os"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/hooks"
)

// Input is the hook input from Claude Code.
type Input struct {
	hooks.BaseInput
	ToolName     string      `json:"tool_name"`
	ToolInput    interface{} `json:"tool_input"`
	ToolResponse interface{} `json:"tool_response"`
	ToolUseID    string      `json:"tool_use_id"`
}

func main() {
	hooks.RunHook("PostToolUse", handlePostToolUse)
}

func handlePostToolUse(ctx *hooks.HookContext, input *Input) (string, error) {
	fmt.Fprintf(os.Stderr, "[post-tool-use] %s\n", input.ToolName)

	// Send observation to worker
	_, err := hooks.POST(ctx.Port, "/api/sessions/observations", map[string]interface{}{
		"claudeSessionId": ctx.SessionID,
		"project":         ctx.Project,
		"tool_name":       input.ToolName,
		"tool_input":      input.ToolInput,
		"tool_response":   input.ToolResponse,
		"cwd":             ctx.CWD,
	})

	return "", err
}
