// Package hooks provides hook utilities for claude-mnemonic.
package hooks

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
