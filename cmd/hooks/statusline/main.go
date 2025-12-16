// Package main provides the statusline hook for Claude Code.
// This binary outputs a status line showing claude-mnemonic metrics.
package main

import (
	"encoding/json"
	"fmt"
	"io"
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
	// Read input from stdin
	inputData, err := io.ReadAll(os.Stdin)
	if err != nil {
		// On error, output minimal status
		fmt.Println(formatOffline())
		return
	}

	var input StatusInput
	if err := json.Unmarshal(inputData, &input); err != nil {
		fmt.Println(formatOffline())
		return
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
	stats := getWorkerStats(project)

	// Format and output statusline
	fmt.Println(formatStatusLine(stats, input))
}

// getWorkerStats fetches stats from the worker service.
func getWorkerStats(project string) *WorkerStats {
	port := hooks.GetWorkerPort()

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
	parts := []string{}

	// Total memories served to Claude this session
	parts = append(parts, fmt.Sprintf("served:%d", stats.Retrieval.ObservationsServed))

	// Context injections (memories auto-loaded at session start)
	if stats.Retrieval.ContextInjections > 0 {
		parts = append(parts, fmt.Sprintf("injected:%d", stats.Retrieval.ContextInjections))
	}

	// Semantic searches performed
	if stats.Retrieval.SearchRequests > 0 {
		parts = append(parts, fmt.Sprintf("searches:%d", stats.Retrieval.SearchRequests))
	}

	// Project-specific memory count
	if stats.ProjectObservations > 0 {
		if useColors {
			parts = append(parts, fmt.Sprintf("%sproject:%d memories%s", colorYellow, stats.ProjectObservations, reset))
		} else {
			parts = append(parts, fmt.Sprintf("project:%d memories", stats.ProjectObservations))
		}
	}

	// Processing indicator
	if stats.IsProcessing || stats.QueueDepth > 0 {
		if useColors {
			parts = append(parts, colorYellow+"processing..."+colorReset)
		} else {
			parts = append(parts, "processing...")
		}
	}

	result := prefix + " " + indicator
	for i, part := range parts {
		if i == 0 {
			result += " " + part
		} else {
			result += " | " + part
		}
	}

	return result
}

// formatCompact returns a compact status line.
func formatCompact(stats *WorkerStats, useColors bool) string {
	// [m] ● 42/5/3 (28)
	var prefix, indicator string
	if useColors {
		prefix = colorCyan + "[m]" + colorReset
		indicator = colorGreen + "●" + colorReset
	} else {
		prefix = "[m]"
		indicator = "●"
	}

	result := fmt.Sprintf("%s %s %d/%d/%d",
		prefix, indicator,
		stats.Retrieval.ObservationsServed,
		stats.Retrieval.ContextInjections,
		stats.Retrieval.SearchRequests,
	)

	if stats.ProjectObservations > 0 {
		result += fmt.Sprintf(" (%d)", stats.ProjectObservations)
	}

	if stats.IsProcessing || stats.QueueDepth > 0 {
		if useColors {
			result += " " + colorYellow + "⚙" + colorReset
		} else {
			result += " ⚙"
		}
	}

	return result
}

// formatMinimal returns a minimal status line.
func formatMinimal(stats *WorkerStats, useColors bool) string {
	// ● 42 obs
	var indicator string
	if useColors {
		indicator = colorGreen + "●" + colorReset
	} else {
		indicator = "●"
	}

	result := fmt.Sprintf("%s %d", indicator, stats.Retrieval.ObservationsServed)

	if stats.ProjectObservations > 0 {
		result += fmt.Sprintf("/%d", stats.ProjectObservations)
	}

	return result
}

// formatOffline returns the offline status.
func formatOffline() string {
	return formatOfflineColored(true)
}

// formatOfflineColored returns the offline status with optional colors.
func formatOfflineColored(useColors bool) string {
	if useColors {
		return colorCyan + "[mnemonic]" + colorReset + " " + colorGray + "○" + colorReset
	}
	return "[mnemonic] ○"
}

// formatStartingColored returns the starting status with optional colors.
func formatStartingColored(useColors bool) string {
	if useColors {
		return colorCyan + "[mnemonic]" + colorReset + " " + colorYellow + "◐" + colorReset + " starting"
	}
	return "[mnemonic] ◐ starting"
}
