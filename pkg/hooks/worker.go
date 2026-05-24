// Package hooks provides hook utilities for claude-mnemonic.
package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Version is set at build time via ldflags
var Version = "dev"

const (
	// DefaultWorkerPort is the default worker port.
	DefaultWorkerPort = 37777

	// HealthCheckTimeout is the timeout for health checks.
	HealthCheckTimeout = 2 * time.Second

	// StartupTimeout is the timeout for worker startup.
	StartupTimeout = 30 * time.Second

	// workerCacheMaxAge is how long the worker cache is considered fresh.
	workerCacheMaxAge = 10 * time.Second

	// circuitBreakerCooldown is how long to wait after a startup failure before retrying.
	circuitBreakerCooldown = 30 * time.Second

	// healthCheckRetries is the number of health check attempts before declaring dead.
	healthCheckRetries = 3

	// healthCheckRetryDelay is the delay between health check retries.
	healthCheckRetryDelay = 200 * time.Millisecond
)

var (
	// circuitBreakerMu protects lastStartupFailure.
	circuitBreakerMu   sync.Mutex
	lastStartupFailure time.Time
)

// IsWorkerAvailable performs a fast check without network calls.
// Returns true if the worker is likely available, false if definitely down.
func IsWorkerAvailable() bool {
	// Check circuit breaker first
	circuitBreakerMu.Lock()
	if !lastStartupFailure.IsZero() && time.Since(lastStartupFailure) < circuitBreakerCooldown {
		circuitBreakerMu.Unlock()
		return false
	}
	circuitBreakerMu.Unlock()

	// Check PID cache
	entry := readWorkerCache()
	if entry == nil {
		return true // No cache = unknown, don't block
	}

	// Cache exists and is fresh (readWorkerCache already checks staleness)
	// Check if cached process is alive
	return isProcessAlive(entry.PID)
}

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
// Parses the JSON health response to check the "ready" field when available.
// Falls back to HTTP status code check for backwards compatibility.
func IsWorkerRunning(port int) bool {
	client := &http.Client{Timeout: HealthCheckTimeout}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/health", port))
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()

	// Try to parse JSON response for structured health check
	var health struct {
		Ready bool `json:"ready"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&health); err == nil {
		return health.Ready
	}

	// Fallback: treat HTTP 200 as healthy (backwards compatibility)
	return resp.StatusCode == http.StatusOK
}

// workerCachePath returns the path to the worker cache file.
func workerCachePath() string {
	home := os.Getenv("HOME")
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".claude-mnemonic", ".worker-cache")
}

// workerCacheEntry holds cached worker state: "port:pid:timestamp".
type workerCacheEntry struct {
	Timestamp time.Time
	Port      int
	PID       int
}

// readWorkerCache reads the worker cache file and returns the entry if fresh.
func readWorkerCache() *workerCacheEntry {
	path := workerCachePath()
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	parts := strings.SplitN(strings.TrimSpace(string(data)), ":", 3)
	if len(parts) != 3 {
		return nil
	}
	port, err := strconv.Atoi(parts[0])
	if err != nil || port <= 0 {
		return nil
	}
	pid, err := strconv.Atoi(parts[1])
	if err != nil || pid <= 0 {
		return nil
	}
	ts, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return nil
	}
	entry := &workerCacheEntry{
		Port:      port,
		PID:       pid,
		Timestamp: time.Unix(ts, 0),
	}
	// Check freshness
	if time.Since(entry.Timestamp) > workerCacheMaxAge {
		return nil
	}
	return entry
}

// writeWorkerCache writes the worker cache file.
func writeWorkerCache(port, pid int) {
	path := workerCachePath()
	if path == "" {
		return
	}
	// Ensure directory exists
	dir := filepath.Dir(path)
	_ = os.MkdirAll(dir, 0o700)
	data := fmt.Sprintf("%d:%d:%d", port, pid, time.Now().Unix())
	_ = os.WriteFile(path, []byte(data), 0o600)
}

// isProcessAlive checks if a process with the given PID exists and is alive.
func isProcessAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0 checks if process exists without actually sending a signal.
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}

// isWorkerRunningWithRetries checks if the worker is running, retrying on timeout.
// Returns true only if health check succeeds. Returns false if all retries fail.
func isWorkerRunningWithRetries(port int) bool {
	for i := 0; i < healthCheckRetries; i++ {
		if IsWorkerRunning(port) {
			return true
		}
		if i < healthCheckRetries-1 {
			time.Sleep(healthCheckRetryDelay)
		}
	}
	return false
}

// EnsureWorkerRunning ensures the worker is running, starting it if necessary.
// If a worker is already running and healthy with matching version, it reuses it.
// If version mismatch or unhealthy, it kills the old worker and starts fresh.
func EnsureWorkerRunning() (int, error) {
	port := GetWorkerPort()

	// Fast path: check PID cache before making any HTTP calls.
	if entry := readWorkerCache(); entry != nil && entry.Port == port {
		if isProcessAlive(entry.PID) {
			return port, nil
		}
	}

	// Circuit breaker: if we failed to start recently, don't retry immediately.
	circuitBreakerMu.Lock()
	if !lastStartupFailure.IsZero() && time.Since(lastStartupFailure) < circuitBreakerCooldown {
		circuitBreakerMu.Unlock()
		return 0, fmt.Errorf("worker startup failed recently (circuit breaker open, retry after %v)", circuitBreakerCooldown-time.Since(lastStartupFailure))
	}
	circuitBreakerMu.Unlock()

	// Check if already running and healthy (with retries to avoid false negatives under load)
	if isWorkerRunningWithRetries(port) {
		// Check version - if mismatch, restart (unless both are dev builds)
		if runningVersion := GetWorkerVersion(port); runningVersion != "" {
			if runningVersion != Version {
				// For dev/dirty builds, don't restart if base versions match
				if versionsCompatible(runningVersion, Version) {
					updateCacheFromPort(port)
					return port, nil
				}
				fmt.Fprintf(os.Stderr, "[claude-mnemonic] Worker version mismatch (running: %s, expected: %s), restarting...\n", runningVersion, Version)
				if err := KillProcessOnPort(port); err != nil {
					fmt.Fprintf(os.Stderr, "[claude-mnemonic] Warning: failed to kill old worker: %v\n", err)
				}
				time.Sleep(500 * time.Millisecond)
			} else {
				// Version matches, reuse existing worker
				updateCacheFromPort(port)
				return port, nil
			}
		} else {
			// Couldn't get version, assume it's fine
			updateCacheFromPort(port)
			return port, nil
		}
	}

	// Port is in use but health check failed -- worker may be slow, not dead.
	if IsPortInUse(port) {
		// The port is responding to TCP but health check timed out.
		// Don't kill it -- it's likely just under load. Give it more time.
		fmt.Fprintf(os.Stderr, "[claude-mnemonic] Worker on port %d is slow to respond, waiting...\n", port)
		// Try a few more times with longer delays before giving up
		for i := 0; i < 3; i++ {
			time.Sleep(500 * time.Millisecond)
			if IsWorkerRunning(port) {
				updateCacheFromPort(port)
				return port, nil
			}
		}
		// Still not healthy after extended wait -- kill and restart
		fmt.Fprintf(os.Stderr, "[claude-mnemonic] Worker unresponsive after extended wait, restarting...\n")
		if err := KillProcessOnPort(port); err != nil {
			fmt.Fprintf(os.Stderr, "[claude-mnemonic] Warning: failed to kill unhealthy process on port %d: %v\n", port, err)
		}
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
		circuitBreakerMu.Lock()
		lastStartupFailure = time.Now()
		circuitBreakerMu.Unlock()
		return 0, fmt.Errorf("failed to start worker: %w", err)
	}

	pid := cmd.Process.Pid

	// Wait for worker to be ready with exponential backoff
	deadline := time.Now().Add(StartupTimeout)
	backoff := 50 * time.Millisecond
	maxBackoff := 500 * time.Millisecond

	for time.Now().Before(deadline) {
		if IsWorkerRunning(port) {
			writeWorkerCache(port, pid)
			return port, nil
		}
		time.Sleep(backoff)
		// Exponential backoff with cap
		backoff = backoff * 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}

	circuitBreakerMu.Lock()
	lastStartupFailure = time.Now()
	circuitBreakerMu.Unlock()
	return 0, fmt.Errorf("worker failed to start within timeout")
}

// updateCacheFromPort finds the PID of the process on the port and updates the cache.
func updateCacheFromPort(port int) {
	cmd := exec.Command("lsof", "-t", "-i", fmt.Sprintf(":%d", port)) // #nosec G204 -- port is from internal config
	output, err := cmd.Output()
	if err != nil {
		return
	}
	pidStr := strings.TrimSpace(string(output))
	// Take first PID if multiple
	if idx := strings.Index(pidStr, "\n"); idx > 0 {
		pidStr = pidStr[:idx]
	}
	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid <= 0 {
		return
	}
	writeWorkerCache(port, pid)
}

// GetWorkerVersion gets the version of the running worker.
func GetWorkerVersion(port int) string {
	client := &http.Client{Timeout: HealthCheckTimeout}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/version", port))
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()

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
	home := os.Getenv("HOME")

	// Stable binary location (primary, survives Claude Code updates)
	stablePath := filepath.Join(home, ".claude-mnemonic", "bin", "worker")
	if _, err := os.Stat(stablePath); err == nil {
		return stablePath
	}

	// Check CLAUDE_PLUGIN_ROOT (set by Claude Code when running hooks)
	if pluginRoot := os.Getenv("CLAUDE_PLUGIN_ROOT"); pluginRoot != "" {
		workerPath := filepath.Join(pluginRoot, "worker")
		if _, err := os.Stat(workerPath); err == nil {
			return workerPath
		}
	}

	// Check common locations
	locations := []string{
		"./worker",
		"./bin/worker",
	}

	for _, loc := range locations {
		if _, err := os.Stat(loc); err == nil {
			return loc
		}
	}

	// Try cache directory with any version
	matches, _ := filepath.Glob(filepath.Join(home, ".claude/plugins/cache/claude-mnemonic/claude-mnemonic/*/worker"))
	if len(matches) > 0 {
		return matches[len(matches)-1]
	}

	// Try marketplaces directory
	marketplacePath := filepath.Join(home, ".claude/plugins/marketplaces/claude-mnemonic/worker")
	if _, err := os.Stat(marketplacePath); err == nil {
		return marketplacePath
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
	defer func() { _ = resp.Body.Close() }()

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

// POSTWithContext sends a POST request using the provided context.
// Used for fire-and-forget calls where we want to control the timeout externally.
func POSTWithContext(ctx context.Context, port int, path string, body interface{}) error {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("http://127.0.0.1:%d%s", port, path),
		bytes.NewReader(jsonBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	return nil
}

// GET sends a GET request to the worker.
func GET(port int, path string) (map[string]interface{}, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d%s", port, path))
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

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
