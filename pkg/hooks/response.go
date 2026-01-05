// Package hooks provides hook utilities for claude-mnemonic.
package hooks

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// HookResponse is the response sent back to Claude Code.
type HookResponse struct {
	Continue bool `json:"continue"`
}

// ProjectIDWithName returns both the hash ID and the directory name for display.
// Format: "dirname_abc123" (name + truncated hash for human-readability)
func ProjectIDWithName(cwd string) string {
	absPath, err := filepath.Abs(cwd)
	if err != nil {
		absPath = cwd
	}

	dirName := filepath.Base(absPath)
	hash := sha256.Sum256([]byte(absPath))
	shortHash := hex.EncodeToString(hash[:3]) // 6 chars

	return fmt.Sprintf("%s_%s", dirName, shortHash)
}

// Exit codes for Claude Code hooks
const (
	ExitSuccess         = 0
	ExitFailure         = 1
	ExitUserMessageOnly = 3 // Display stderr as user message
)

// WriteResponse writes a hook response to stdout.
func WriteResponse(hookName string, success bool) {
	response := HookResponse{Continue: success}
	data, _ := json.Marshal(response)
	fmt.Println(string(data))
}

// WriteError writes an error message to stderr and exits.
func WriteError(hookName string, err error) {
	fmt.Fprintf(os.Stderr, "[%s] Error: %v\n", hookName, err)
	WriteResponse(hookName, false)
}

// BaseInput contains common fields shared by all hook inputs.
type BaseInput struct {
	SessionID      string `json:"session_id"`
	CWD            string `json:"cwd"`
	PermissionMode string `json:"permission_mode"`
	HookEventName  string `json:"hook_event_name"`
}

// HookContext provides common context for hook handlers.
type HookContext struct {
	HookName  string
	Port      int
	Project   string
	SessionID string
	CWD       string
	RawInput  []byte
}

// HookHandler is a function that handles hook-specific logic.
// It receives the context and returns an optional context string and error.
type HookHandler[T any] func(ctx *HookContext, input *T) (additionalContext string, err error)

// RunHook executes a hook with common boilerplate handling.
// It handles: internal call skip, stdin reading, JSON unmarshaling,
// worker startup, and project ID generation.
func RunHook[T any](hookName string, handler HookHandler[T]) {
	// Skip if this is an internal call (from SDK processor)
	if os.Getenv("CLAUDE_MNEMONIC_INTERNAL") == "1" {
		WriteResponse(hookName, true)
		return
	}

	// Read input from stdin
	inputData, err := io.ReadAll(os.Stdin)
	if err != nil {
		WriteError(hookName, err)
		os.Exit(1)
	}

	// Parse input
	var input T
	if err := json.Unmarshal(inputData, &input); err != nil {
		WriteError(hookName, err)
		os.Exit(1)
	}

	// Ensure worker is running
	port, err := EnsureWorkerRunning()
	if err != nil {
		WriteError(hookName, err)
		os.Exit(1)
	}

	// Extract base fields using interface assertion or reflection
	var base BaseInput
	_ = json.Unmarshal(inputData, &base)

	// Generate project ID from CWD
	project := ProjectIDWithName(base.CWD)

	// Create context
	ctx := &HookContext{
		HookName:  hookName,
		Port:      port,
		Project:   project,
		SessionID: base.SessionID,
		CWD:       base.CWD,
		RawInput:  inputData,
	}

	// Run hook-specific handler
	additionalContext, err := handler(ctx, &input)
	if err != nil {
		WriteError(hookName, err)
		os.Exit(1)
	}

	// Output response
	if additionalContext != "" {
		response := map[string]interface{}{
			"continue": true,
			"hookSpecificOutput": map[string]interface{}{
				"hookEventName":     hookName,
				"additionalContext": additionalContext,
			},
		}
		_ = json.NewEncoder(os.Stdout).Encode(response)
		os.Exit(0)
	}

	WriteResponse(hookName, true)
}

// StatuslineHandler is a function that handles statusline-specific logic.
// It receives input and port, returns formatted status string.
// No context injection or worker startup - just display.
type StatuslineHandler[T any] func(input *T, port int) string

// RunStatuslineHook executes a statusline hook with minimal overhead.
// Unlike RunHook, this:
// - Does NOT check CLAUDE_MNEMONIC_INTERNAL (statuslines always run)
// - Uses GetWorkerPort() instead of EnsureWorkerRunning() (no startup)
// - Prints output directly to stdout (no JSON wrapping)
// This keeps statusline fast (<100ms requirement).
func RunStatuslineHook[T any](handler StatuslineHandler[T]) {
	// Read input from stdin
	inputData, err := io.ReadAll(os.Stdin)
	if err != nil {
		// On error, handler receives nil and should return offline status
		fmt.Println(handler(nil, 0))
		return
	}

	// Parse input
	var input T
	if err := json.Unmarshal(inputData, &input); err != nil {
		// On parse error, handler receives nil and should return offline status
		fmt.Println(handler(nil, 0))
		return
	}

	// Get worker port (does NOT start worker)
	port := GetWorkerPort()

	// Run handler and print result
	fmt.Println(handler(&input, port))
}
