// Package main provides the statusline hook for Claude Code.
// This binary outputs a status line showing claude-mnemonic metrics.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/hooks"
)

// StatusInput is the JSON input from Claude Code's statusline feature.
type StatusInput struct {
	HookEventName string `json:"hook_event_name"`
	SessionID     string `json:"session_id"`
	CWD           string `json:"cwd"`
	Model         struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	} `json:"model"`
	Workspace struct {
		CurrentDir string `json:"current_dir"`
		ProjectDir string `json:"project_dir"`
	} `json:"workspace"`
	Version string `json:"version"`
	Cost    struct {
		TotalCostUSD       float64 `json:"total_cost_usd"`
		TotalDurationMS    int64   `json:"total_duration_ms"`
		TotalAPIDurationMS int64   `json:"total_api_duration_ms"`
		TotalLinesAdded    int     `json:"total_lines_added"`
		TotalLinesRemoved  int     `json:"total_lines_removed"`
	} `json:"cost"`
	ContextWindow struct {
		TotalInputTokens  int `json:"total_input_tokens"`
		TotalOutputTokens int `json:"total_output_tokens"`
		ContextWindowSize int `json:"context_window_size"`
	} `json:"context_window"`
}

// WorkerStats is the response from the worker's /api/stats endpoint.
type WorkerStats struct {
	Uptime              string `json:"uptime"`
	ActiveSessions      int    `json:"activeSessions"`
	QueueDepth          int    `json:"queueDepth"`
	IsProcessing        bool   `json:"isProcessing"`
	ConnectedClients    int    `json:"connectedClients"`
	SessionsToday       int    `json:"sessionsToday"`
	Ready               bool   `json:"ready"`
	Project             string `json:"project,omitempty"`
	ProjectObservations int    `json:"projectObservations,omitempty"`
	Retrieval           struct {
		TotalRequests      int64 `json:"TotalRequests"`
		ObservationsServed int64 `json:"ObservationsServed"`
		SearchRequests     int64 `json:"SearchRequests"`
		ContextInjections  int64 `json:"ContextInjections"`
	} `json:"retrieval"`
}

// ANSI color codes
const (
	colorReset  = "\033[0m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorCyan   = "\033[36m"
	colorGray   = "\033[90m"
	colorRed    = "\033[31m"
)

func main() {
	hooks.RunStatuslineHook(handleStatusline)
}

func handleStatusline(input *StatusInput, port int) string {
	// Handle error cases (nil input)
	if input == nil {
		return formatOffline()
	}

	// Determine project directory
	projectDir := input.Workspace.ProjectDir
	if projectDir == "" {
		projectDir = input.Workspace.CurrentDir
	}
	if projectDir == "" {
		projectDir = input.CWD
	}

	// Generate project ID
	project := ""
	if projectDir != "" {
		project = hooks.ProjectIDWithName(projectDir)
	}

	// Get worker stats
	stats := getWorkerStats(port, project)

	// Format and return statusline
	return formatStatusLine(stats, *input)
}

// getWorkerStats fetches stats from the worker service.
func getWorkerStats(port int, project string) *WorkerStats {
	// Build URL with optional project parameter
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/api/stats", port)
	if project != "" {
		endpoint += "?project=" + url.QueryEscape(project)
	}

	// Create HTTP client with short timeout (statusline must be fast)
	client := &http.Client{Timeout: 100 * time.Millisecond}

	resp, err := client.Get(endpoint)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	var stats WorkerStats
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return nil
	}

	return &stats
}

// formatStatusLine formats the status line output.
func formatStatusLine(stats *WorkerStats, input StatusInput) string {
	// Check if colors are enabled (default: yes, unless TERM is dumb or NO_COLOR is set)
	useColors := os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
	if os.Getenv("CLAUDE_MNEMONIC_STATUSLINE_COLORS") == "false" {
		useColors = false
	} else if os.Getenv("CLAUDE_MNEMONIC_STATUSLINE_COLORS") == "true" {
		useColors = true
	}

	// Check format preference
	format := os.Getenv("CLAUDE_MNEMONIC_STATUSLINE_FORMAT")
	if format == "" {
		format = "default"
	}

	if stats == nil {
		return formatOfflineColored(useColors)
	}

	if !stats.Ready {
		return formatStartingColored(useColors)
	}

	switch format {
	case "compact":
		return formatCompact(stats, useColors)
	case "minimal":
		return formatMinimal(stats, useColors)
	default:
		return formatDefault(stats, useColors)
	}
}

// formatDefault returns the default status line format.
func formatDefault(stats *WorkerStats, useColors bool) string {
	// [mnemonic] ● served:42 | injected:5 | searches:3 | project:28 memories
	var prefix, indicator, reset string
	if useColors {
		prefix = colorCyan + "[mnemonic]" + colorReset
		indicator = colorGreen + "●" + colorReset
		reset = colorReset
	} else {
		prefix = "[mnemonic]"
		indicator = "●"
	}

	// Build status parts with clear labels
	parts := []string{
		prefix,
		indicator,
	}

	// Add retrieval stats if available
	if stats.Retrieval.ObservationsServed > 0 {
		parts = append(parts, fmt.Sprintf("served:%d", stats.Retrieval.ObservationsServed))
	}
	if stats.Retrieval.ContextInjections > 0 {
		parts = append(parts, fmt.Sprintf("injected:%d", stats.Retrieval.ContextInjections))
	}
	if stats.Retrieval.SearchRequests > 0 {
		parts = append(parts, fmt.Sprintf("searches:%d", stats.Retrieval.SearchRequests))
	}

	// Add project-specific observation count if available
	if stats.ProjectObservations > 0 {
		parts = append(parts, fmt.Sprintf("project:%d memories", stats.ProjectObservations))
	}

	// Join with separators
	result := parts[0] + " " + parts[1]
	if len(parts) > 2 {
		for i := 2; i < len(parts); i++ {
			if useColors {
				result += colorGray + " | " + reset + parts[i]
			} else {
				result += " | " + parts[i]
			}
		}
	}

	return result
}

// formatCompact returns a compact status line format.
func formatCompact(stats *WorkerStats, useColors bool) string {
	// [m] ● 42/5/3
	var prefix, indicator string
	if useColors {
		prefix = colorCyan + "[m]" + colorReset
		indicator = colorGreen + "●" + colorReset
	} else {
		prefix = "[m]"
		indicator = "●"
	}

	return fmt.Sprintf("%s %s %d/%d/%d",
		prefix, indicator,
		stats.Retrieval.ObservationsServed,
		stats.Retrieval.ContextInjections,
		stats.Retrieval.SearchRequests)
}

// formatMinimal returns a minimal status line format.
func formatMinimal(stats *WorkerStats, useColors bool) string {
	// ● 28 memories
	var indicator string
	if useColors {
		indicator = colorGreen + "●" + colorReset
	} else {
		indicator = "●"
	}

	if stats.ProjectObservations > 0 {
		return fmt.Sprintf("%s %d memories", indicator, stats.ProjectObservations)
	}

	return fmt.Sprintf("%s mnemonic ready", indicator)
}

// formatOffline returns status for when worker is offline.
func formatOffline() string {
	useColors := os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
	return formatOfflineColored(useColors)
}

// formatOfflineColored returns colored offline status.
func formatOfflineColored(useColors bool) string {
	if useColors {
		return colorGray + "[mnemonic]" + colorReset + " " + colorGray + "○" + colorReset + " offline"
	}
	return "[mnemonic] ○ offline"
}

// formatStartingColored returns colored starting status.
func formatStartingColored(useColors bool) string {
	if useColors {
		return colorYellow + "[mnemonic]" + colorReset + " " + colorYellow + "○" + colorReset + " starting..."
	}
	return "[mnemonic] ○ starting..."
}
