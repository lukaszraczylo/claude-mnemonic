// Package hooks provides hook utilities for claude-mnemonic.
package hooks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Version is set at build time via ldflags
var Version = "dev"

const (
	// DefaultWorkerPort is the default worker port.
	DefaultWorkerPort = 37777

	// HealthCheckTimeout is the timeout for health checks (reduced from 5s for faster startup).
	HealthCheckTimeout = 1 * time.Second

	// StartupTimeout is the timeout for worker startup.
	StartupTimeout = 30 * time.Second
)

// GetWorkerPort returns the worker port from environment or default.
func GetWorkerPort() int {
	if port := os.Getenv("CLAUDE_MNEMONIC_WORKER_PORT"); port != "" {
		if p, err := strconv.Atoi(port); err == nil && p > 0 {
			return p
		}
	}
	return DefaultWorkerPort
}

// IsWorkerRunning checks if the worker is running and healthy.
func IsWorkerRunning(port int) bool {
	client := &http.Client{Timeout: HealthCheckTimeout}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/health", port))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// EnsureWorkerRunning ensures the worker is running, starting it if necessary.
// If a worker is already running and healthy with matching version, it reuses it.
// If version mismatch or unhealthy, it kills the old worker and starts fresh.
func EnsureWorkerRunning() (int, error) {
	port := GetWorkerPort()

	// Check if already running and healthy
	if IsWorkerRunning(port) {
		// Check version - if mismatch, restart (unless both are dev builds)
		if runningVersion := GetWorkerVersion(port); runningVersion != "" {
			if runningVersion != Version {
				// For dev/dirty builds, don't restart if base versions match
				if versionsCompatible(runningVersion, Version) {
					return port, nil
				}
				fmt.Fprintf(os.Stderr, "[claude-mnemonic] Worker version mismatch (running: %s, expected: %s), restarting...\n", runningVersion, Version)
				if err := KillProcessOnPort(port); err != nil {
					fmt.Fprintf(os.Stderr, "[claude-mnemonic] Warning: failed to kill old worker: %v\n", err)
				}
				time.Sleep(500 * time.Millisecond)
			} else {
				// Version matches, reuse existing worker
				return port, nil
			}
		} else {
			// Couldn't get version, assume it's fine
			return port, nil
		}
	}

	// Check if port is in use but worker is unhealthy
	if IsPortInUse(port) {
		// Something is using the port but not responding to health checks
		// Try to kill it
		if err := KillProcessOnPort(port); err != nil {
			// Log but continue - maybe it will die on its own
			fmt.Fprintf(os.Stderr, "[claude-mnemonic] Warning: failed to kill unhealthy process on port %d: %v\n", port, err)
		}
		// Wait a moment for port to be released
		time.Sleep(500 * time.Millisecond)
	}

	// Find worker binary
	workerPath := findWorkerBinary()
	if workerPath == "" {
		return 0, fmt.Errorf("worker binary not found")
	}

	// Start worker
	cmd := exec.Command(workerPath) // #nosec G204 -- workerPath is from internal findWorkerBinary
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("failed to start worker: %w", err)
	}

	// Wait for worker to be ready with exponential backoff
	deadline := time.Now().Add(StartupTimeout)
	backoff := 50 * time.Millisecond
	maxBackoff := 500 * time.Millisecond

	for time.Now().Before(deadline) {
		if IsWorkerRunning(port) {
			return port, nil
		}
		time.Sleep(backoff)
		// Exponential backoff with cap
		backoff = backoff * 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}

	return 0, fmt.Errorf("worker failed to start within timeout")
}

// GetWorkerVersion gets the version of the running worker.
func GetWorkerVersion(port int) string {
	client := &http.Client{Timeout: HealthCheckTimeout}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/version", port))
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ""
	}

	return result["version"]
}

// IsPortInUse checks if the port is in use (regardless of health).
func IsPortInUse(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// KillProcessOnPort finds and kills the process using the given port.
func KillProcessOnPort(port int) error {
	// Use lsof to find the process (works on macOS and Linux)
	cmd := exec.Command("lsof", "-t", "-i", fmt.Sprintf(":%d", port)) // #nosec G204 -- port is from internal config
	output, err := cmd.Output()
	if err != nil {
		// lsof returns exit code 1 when no process is found - that's fine
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil // No process found
		}
		return fmt.Errorf("failed to find process on port: %w", err)
	}

	pidStr := strings.TrimSpace(string(output))
	if pidStr == "" {
		return nil // No process found
	}

	// Handle multiple PIDs (one per line)
	pids := strings.Split(pidStr, "\n")
	for _, pid := range pids {
		pid = strings.TrimSpace(pid)
		if pid == "" {
			continue
		}

		// Kill the process
		killCmd := exec.Command("kill", "-9", pid) // #nosec G204 -- pid is from lsof output
		if err := killCmd.Run(); err != nil {
			return fmt.Errorf("failed to kill process %s: %w", pid, err)
		}
	}

	return nil
}

// findWorkerBinary finds the worker binary path.
func findWorkerBinary() string {
	// Check CLAUDE_PLUGIN_ROOT first (set by Claude Code when running hooks)
	if pluginRoot := os.Getenv("CLAUDE_PLUGIN_ROOT"); pluginRoot != "" {
		workerPath := filepath.Join(pluginRoot, "worker")
		if _, err := os.Stat(workerPath); err == nil {
			return workerPath
		}
	}

	// Check common locations
	home := os.Getenv("HOME")
	locations := []string{
		"./worker",
		"./bin/worker",
		filepath.Join(home, ".claude/plugins/cache/claude-mnemonic/claude-mnemonic/1.0.0/worker"),
		filepath.Join(home, ".claude/plugins/marketplaces/claude-mnemonic/worker"),
	}

	for _, loc := range locations {
		if _, err := os.Stat(loc); err == nil {
			return loc
		}
	}

	// Try PATH
	if path, err := exec.LookPath("claude-mnemonic-worker"); err == nil {
		return path
	}

	return ""
}

// POST sends a POST request to the worker.
func POST(port int, path string, body interface{}) (map[string]interface{}, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	resp, err := client.Post(
		fmt.Sprintf("http://127.0.0.1:%d%s", port, path),
		"application/json",
		bytes.NewReader(jsonBody),
	)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("request failed: %s", resp.Status)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		// Not all endpoints return JSON
		return nil, nil
	}

	return result, nil
}

// GET sends a GET request to the worker.
func GET(port int, path string) (map[string]interface{}, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d%s", port, path))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("request failed: %s", resp.Status)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result, nil
}

// versionsCompatible checks if two versions are compatible for dev builds.
// Returns true if both versions share the same base version (ignoring -dirty, -dev, commit suffixes).
// This prevents unnecessary restarts during development.
func versionsCompatible(v1, v2 string) bool {
	// If either is a plain "dev" version, consider it compatible with anything
	if v1 == "dev" || v2 == "dev" {
		return true
	}

	// Extract base versions (e.g., "v0.3.5" from "v0.3.5-2-gca711a8-dirty")
	base1 := extractBaseVersion(v1)
	base2 := extractBaseVersion(v2)

	// If base versions match, they're compatible
	return base1 == base2
}

// extractBaseVersion extracts the semver base from a version string.
// e.g., "v0.3.5-2-gca711a8-dirty" -> "0.3.5"
func extractBaseVersion(version string) string {
	// Remove leading 'v' if present
	v := strings.TrimPrefix(version, "v")

	// Find first hyphen (start of suffix like -2-gcommit-dirty)
	if idx := strings.Index(v, "-"); idx > 0 {
		v = v[:idx]
	}

	return v
}
