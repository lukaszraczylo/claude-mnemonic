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

// skipTools lists tools that never produce useful observations.
// Skip the HTTP call entirely for these to reduce overhead during heavy tool usage.
var skipTools = map[string]bool{
	// Internal tracking tools (but NOT TodoWrite - it captures planned work)
	"Task":       true,
	"TaskOutput": true,

	// File discovery tools (just listings, no insights)
	"Glob":      true,
	"ListDir":   true,
	"LS":        true,
	"KillShell": true,

	// Question/interaction tools (no code insights)
	"AskUserQuestion": true,

	// Plan mode tools (planning, not execution)
	"EnterPlanMode": true,
	"ExitPlanMode":  true,

	// Skill/command execution (meta-operations)
	"Skill":        true,
	"SlashCommand": true,

	// Read is high-volume and rarely produces insights worth the overhead
	// The processor would skip most reads anyway after filtering
	"Read": true,

	// Search tools are for finding, not modifying
	"Grep":      true,
	"WebSearch": true,
}

func main() {
	hooks.RunHook("PostToolUse", handlePostToolUse)
}

func handlePostToolUse(ctx *hooks.HookContext, input *Input) (string, error) {
	// Skip HTTP call entirely for tools that never produce useful observations.
	// This significantly reduces overhead during heavy tool usage.
	if skipTools[input.ToolName] {
		return "", nil
	}

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
