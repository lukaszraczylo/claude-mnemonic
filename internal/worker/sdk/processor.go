// Package sdk provides SDK agent integration for claude-mnemonic.
package sdk

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	json "github.com/goccy/go-json"

	"github.com/lukaszraczylo/claude-mnemonic/internal/config"
	"github.com/lukaszraczylo/claude-mnemonic/internal/db/gorm"
	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
	"github.com/lukaszraczylo/claude-mnemonic/pkg/similarity"
	"github.com/rs/zerolog/log"
)

// CircuitBreaker implements a simple circuit breaker pattern for CLI calls.
type CircuitBreaker struct {
	failures     int64 // Current failure count
	lastFailure  int64 // Unix timestamp of last failure
	threshold    int64 // Number of failures before opening
	resetTimeout int64 // Seconds to wait before trying again
	state        int32 // 0=closed, 1=open, 2=half-open
}

const (
	circuitClosed   int32 = 0
	circuitOpen     int32 = 1
	circuitHalfOpen int32 = 2
)

// NewCircuitBreaker creates a new circuit breaker.
func NewCircuitBreaker(threshold int64, resetTimeout int64) *CircuitBreaker {
	return &CircuitBreaker{
		threshold:    threshold,
		resetTimeout: resetTimeout,
	}
}

// Allow checks if a request should be allowed through.
func (cb *CircuitBreaker) Allow() bool {
	state := atomic.LoadInt32(&cb.state)
	if state == circuitClosed {
		return true
	}

	if state == circuitOpen {
		// Check if reset timeout has passed
		lastFail := atomic.LoadInt64(&cb.lastFailure)
		if time.Now().Unix()-lastFail > cb.resetTimeout {
			// Transition to half-open
			atomic.CompareAndSwapInt32(&cb.state, circuitOpen, circuitHalfOpen)
			return true
		}
		return false
	}

	// Half-open: allow one request through
	return true
}

// RecordSuccess records a successful call.
func (cb *CircuitBreaker) RecordSuccess() {
	atomic.StoreInt64(&cb.failures, 0)
	atomic.StoreInt32(&cb.state, circuitClosed)
}

// RecordFailure records a failed call.
func (cb *CircuitBreaker) RecordFailure() {
	failures := atomic.AddInt64(&cb.failures, 1)
	atomic.StoreInt64(&cb.lastFailure, time.Now().Unix())

	if failures >= cb.threshold {
		atomic.StoreInt32(&cb.state, circuitOpen)
		log.Warn().Int64("failures", failures).Msg("Circuit breaker opened - Claude CLI calls temporarily disabled")
	}
}

// State returns the current state as a string.
func (cb *CircuitBreaker) State() string {
	switch atomic.LoadInt32(&cb.state) {
	case circuitOpen:
		return "open"
	case circuitHalfOpen:
		return "half-open"
	default:
		return "closed"
	}
}

// CircuitBreakerMetrics contains metrics about the circuit breaker state.
type CircuitBreakerMetrics struct {
	State             string `json:"state"`
	Failures          int64  `json:"failures"`
	Threshold         int64  `json:"threshold"`
	ResetTimeoutSecs  int64  `json:"reset_timeout_secs"`
	LastFailureUnix   int64  `json:"last_failure_unix,omitempty"`
	SecondsUntilReset int64  `json:"seconds_until_reset,omitempty"`
}

// Metrics returns the current metrics of the circuit breaker.
func (cb *CircuitBreaker) Metrics() CircuitBreakerMetrics {
	failures := atomic.LoadInt64(&cb.failures)
	lastFail := atomic.LoadInt64(&cb.lastFailure)
	state := cb.State()

	metrics := CircuitBreakerMetrics{
		State:            state,
		Failures:         failures,
		Threshold:        cb.threshold,
		ResetTimeoutSecs: cb.resetTimeout,
	}

	if lastFail > 0 {
		metrics.LastFailureUnix = lastFail
		if state == "open" {
			remaining := cb.resetTimeout - (time.Now().Unix() - lastFail)
			if remaining > 0 {
				metrics.SecondsUntilReset = remaining
			}
		}
	}

	return metrics
}

// RequestDeduplicator tracks recent requests to prevent duplicates.
type RequestDeduplicator struct {
	seen    map[string]int64 // hash -> timestamp
	mu      sync.RWMutex
	ttlSecs int64
	maxSize int
}

// NewRequestDeduplicator creates a new deduplicator.
func NewRequestDeduplicator(ttlSecs int64, maxSize int) *RequestDeduplicator {
	return &RequestDeduplicator{
		seen:    make(map[string]int64),
		ttlSecs: ttlSecs,
		maxSize: maxSize,
	}
}

// IsDuplicate checks if a request hash was seen recently.
func (d *RequestDeduplicator) IsDuplicate(hash string) bool {
	now := time.Now().Unix()

	d.mu.RLock()
	ts, exists := d.seen[hash]
	d.mu.RUnlock()

	if exists && now-ts < d.ttlSecs {
		return true
	}
	return false
}

// Record marks a request hash as seen.
func (d *RequestDeduplicator) Record(hash string) {
	now := time.Now().Unix()

	d.mu.Lock()
	defer d.mu.Unlock()

	// Evict old entries if at capacity
	if len(d.seen) >= d.maxSize {
		threshold := now - d.ttlSecs
		for k, ts := range d.seen {
			if ts < threshold {
				delete(d.seen, k)
			}
		}
	}

	d.seen[hash] = now
}

// hashRequest creates a hash of a request for deduplication.
func hashRequest(toolName, input, output string) string {
	h := sha256.New()
	h.Write([]byte(toolName))
	h.Write([]byte(input))
	h.Write([]byte(output[:min(len(output), 1000)])) // Only hash first 1000 chars of output
	return hex.EncodeToString(h.Sum(nil))[:16]       // Short hash is sufficient
}

// BroadcastFunc is a callback for broadcasting events to SSE clients.
type BroadcastFunc func(event map[string]any)

// SyncObservationFunc is a callback for syncing observations to vector DB.
type SyncObservationFunc func(obs *models.Observation)

// SyncSummaryFunc is a callback for syncing summaries to vector DB.
type SyncSummaryFunc func(summary *models.SessionSummary)

// MaxVectorSyncWorkers is the maximum number of concurrent vector sync operations.
// This prevents unbounded goroutine spawning during high-volume observation ingestion.
const MaxVectorSyncWorkers = 8

// Processor handles SDK agent processing of observations and summaries using Claude Code CLI.
// Field order optimized for memory alignment (fieldalignment).
type Processor struct {
	observationStore    *gorm.ObservationStore
	summaryStore        *gorm.SummaryStore
	broadcastFunc       BroadcastFunc
	syncObservationFunc SyncObservationFunc
	syncSummaryFunc     SyncSummaryFunc
	circuitBreaker      *CircuitBreaker
	deduplicator        *RequestDeduplicator
	vectorSyncChan      chan *models.Observation
	vectorSyncDone      chan struct{}
	sem                 chan struct{}
	claudePath          string
	model               string
	vectorSyncWg        sync.WaitGroup
}

// SetBroadcastFunc sets the broadcast callback for SSE events.
func (p *Processor) SetBroadcastFunc(fn BroadcastFunc) {
	p.broadcastFunc = fn
}

// SetSyncObservationFunc sets the callback for syncing observations to vector DB.
func (p *Processor) SetSyncObservationFunc(fn SyncObservationFunc) {
	p.syncObservationFunc = fn
}

// SetSyncSummaryFunc sets the callback for syncing summaries to vector DB.
func (p *Processor) SetSyncSummaryFunc(fn SyncSummaryFunc) {
	p.syncSummaryFunc = fn
}

// broadcast sends an event via the broadcast callback if set.
func (p *Processor) broadcast(event map[string]any) {
	if p.broadcastFunc != nil {
		p.broadcastFunc(event)
	}
}

// MaxConcurrentCLICalls is the maximum number of concurrent Claude CLI calls.
// This prevents overwhelming the API and manages resource usage.
const MaxConcurrentCLICalls = 4

// NewProcessor creates a new SDK processor.
func NewProcessor(observationStore *gorm.ObservationStore, summaryStore *gorm.SummaryStore) (*Processor, error) {
	cfg := config.Get()

	// Find Claude Code CLI
	claudePath := cfg.ClaudeCodePath
	if claudePath == "" {
		// Try to find in PATH
		path, err := exec.LookPath("claude")
		if err != nil {
			return nil, fmt.Errorf("claude CLI not found in PATH and CLAUDE_CODE_PATH not set")
		}
		claudePath = path
	}

	// Verify it exists
	if _, err := os.Stat(claudePath); err != nil {
		return nil, fmt.Errorf("claude CLI not found at %s: %w", claudePath, err)
	}

	return &Processor{
		claudePath:       claudePath,
		model:            cfg.Model,
		observationStore: observationStore,
		summaryStore:     summaryStore,
		sem:              make(chan struct{}, MaxConcurrentCLICalls),
		circuitBreaker:   NewCircuitBreaker(5, 60),                               // Open after 5 failures, reset after 60s
		deduplicator:     NewRequestDeduplicator(300, 1000),                      // 5-minute TTL, 1000 max entries
		vectorSyncChan:   make(chan *models.Observation, MaxVectorSyncWorkers*2), // Buffered channel
		vectorSyncDone:   make(chan struct{}),
	}, nil
}

// StartVectorSyncWorkers starts the bounded worker pool for vector sync operations.
// Call this after setting the sync function via SetSyncObservationFunc.
func (p *Processor) StartVectorSyncWorkers() {
	for i := 0; i < MaxVectorSyncWorkers; i++ {
		p.vectorSyncWg.Add(1)
		go p.vectorSyncWorker()
	}
	log.Info().Int("workers", MaxVectorSyncWorkers).Msg("Vector sync worker pool started")
}

// StopVectorSyncWorkers gracefully stops the worker pool.
func (p *Processor) StopVectorSyncWorkers() {
	close(p.vectorSyncDone)
	p.vectorSyncWg.Wait()
	log.Info().Msg("Vector sync worker pool stopped")
}

// vectorSyncWorker is a worker goroutine that processes vector sync requests.
func (p *Processor) vectorSyncWorker() {
	defer p.vectorSyncWg.Done()
	for {
		select {
		case <-p.vectorSyncDone:
			// Drain remaining items before exiting
			for {
				select {
				case obs := <-p.vectorSyncChan:
					if p.syncObservationFunc != nil {
						p.syncObservationFunc(obs)
					}
				default:
					return
				}
			}
		case obs := <-p.vectorSyncChan:
			if p.syncObservationFunc != nil {
				p.syncObservationFunc(obs)
			}
		}
	}
}

// CircuitBreakerState returns the current state of the circuit breaker.
func (p *Processor) CircuitBreakerState() string {
	return p.circuitBreaker.State()
}

// CircuitBreakerMetrics returns detailed metrics about the circuit breaker.
func (p *Processor) CircuitBreakerMetrics() CircuitBreakerMetrics {
	return p.circuitBreaker.Metrics()
}

// IsAvailable checks if the Claude CLI is available for processing.
func (p *Processor) IsAvailable() bool {
	_, err := os.Stat(p.claudePath)
	return err == nil
}

// ProcessObservation processes a single tool observation and extracts insights.
func (p *Processor) ProcessObservation(ctx context.Context, sdkSessionID, project string, toolName string, toolInput, toolResponse any, promptNumber int, cwd string) error {
	// Skip certain tools that aren't worth processing
	if shouldSkipTool(toolName) {
		log.Info().Str("tool", toolName).Msg("Skipping tool (not interesting for memory)")
		return nil
	}

	// Convert tool data to strings for pre-filtering
	inputStr := toJSONString(toolInput)
	outputStr := toJSONString(toolResponse)

	// Pre-filter trivial operations without calling Haiku
	if shouldSkipTrivialOperation(toolName, inputStr, outputStr) {
		log.Debug().Str("tool", toolName).Msg("Skipping trivial operation (pre-filter)")
		return nil
	}

	// Check for duplicate request within TTL window
	reqHash := hashRequest(toolName, inputStr, outputStr)
	if p.deduplicator.IsDuplicate(reqHash) {
		log.Debug().Str("tool", toolName).Msg("Skipping duplicate request (dedup)")
		return nil
	}

	// Check circuit breaker before making CLI call
	if !p.circuitBreaker.Allow() {
		log.Warn().Str("tool", toolName).Msg("Circuit breaker open - skipping CLI call")
		return fmt.Errorf("circuit breaker open")
	}

	log.Info().Str("tool", toolName).Msg("Processing tool execution with Claude CLI")

	// Record this request to prevent duplicates
	p.deduplicator.Record(reqHash)

	// Build the prompt
	exec := ToolExecution{
		ToolName:   toolName,
		ToolInput:  inputStr,
		ToolOutput: outputStr,
		CWD:        cwd,
	}
	prompt := BuildObservationPrompt(exec)

	// Acquire semaphore slot (limits concurrent CLI calls)
	select {
	case p.sem <- struct{}{}:
		defer func() { <-p.sem }()
	case <-ctx.Done():
		return ctx.Err()
	}

	// Call Claude Code CLI
	response, err := p.callClaudeCLI(ctx, prompt)
	if err != nil {
		p.circuitBreaker.RecordFailure()
		log.Error().Err(err).Str("tool", toolName).Msg("Failed to call Claude CLI for observation")
		return err
	}
	p.circuitBreaker.RecordSuccess()

	// Parse observations from response
	observations := ParseObservations(response, sdkSessionID)
	if len(observations) == 0 {
		log.Info().Str("tool", toolName).Msg("No observations extracted (Claude deemed not significant)")
		return nil
	}

	// Get existing observations for deduplication
	existingObs, err := p.observationStore.GetRecentObservations(ctx, project, 50)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to get existing observations for dedup check")
		existingObs = nil // Continue without dedup
	}

	// Store each observation (with deduplication check)
	const similarityThreshold = 0.4 // Same threshold as retrieval clustering
	var storedCount, skippedCount int

	for _, obs := range observations {
		// Capture file modification times for staleness detection
		obs.FileMtimes = captureFileMtimes(obs.FilesRead, obs.FilesModified, cwd)

		// Convert to stored observation for similarity check
		storedObs := obs.ToStoredObservation()

		// Check if this observation is too similar to existing ones
		if existingObs != nil && similarity.IsSimilarToAny(storedObs, existingObs, similarityThreshold) {
			log.Debug().
				Str("type", string(obs.Type)).
				Str("title", obs.Title).
				Msg("Skipping observation - too similar to existing")
			skippedCount++
			continue
		}

		id, createdAtEpoch, err := p.observationStore.StoreObservation(ctx, sdkSessionID, project, obs, promptNumber, 0)
		if err != nil {
			log.Error().Err(err).Msg("Failed to store observation")
			continue
		}

		storedCount++
		log.Info().
			Int64("id", id).
			Str("type", string(obs.Type)).
			Str("title", obs.Title).
			Int("trackedFiles", len(obs.FileMtimes)).
			Msg("Observation stored")

		// Sync to vector DB via bounded worker pool (non-blocking to reduce latency)
		if p.syncObservationFunc != nil && p.vectorSyncChan != nil {
			fullObs := models.NewObservation(sdkSessionID, project, obs, promptNumber, 0)
			fullObs.ID = id
			fullObs.CreatedAtEpoch = createdAtEpoch
			// Non-blocking send to worker pool - drops if channel is full
			select {
			case p.vectorSyncChan <- fullObs:
				// Sent to worker pool
			default:
				// Channel full, fall back to direct sync in goroutine (bounded by channel buffer)
				log.Debug().Int64("obs_id", id).Msg("Vector sync channel full, using fallback goroutine")
				go func(obsToSync *models.Observation) {
					p.syncObservationFunc(obsToSync)
				}(fullObs)
			}
		}

		// Broadcast new observation event for dashboard refresh
		p.broadcast(map[string]any{
			"type":    "observation",
			"action":  "created",
			"id":      id,
			"project": project,
		})

		// Add to existing for subsequent dedup checks within same batch
		if existingObs != nil {
			existingObs = append(existingObs, storedObs)
		}
	}

	if skippedCount > 0 {
		log.Info().
			Int("stored", storedCount).
			Int("skipped", skippedCount).
			Msg("Observation processing complete (duplicates skipped)")
	}

	return nil
}

// ProcessSummary processes a session summary request.
func (p *Processor) ProcessSummary(ctx context.Context, sessionDBID int64, sdkSessionID, project, userPrompt, lastUserMsg, lastAssistantMsg string) error {
	// Debug: log what we received
	log.Debug().
		Int64("sessionId", sessionDBID).
		Int("lastAssistantMsgLen", len(lastAssistantMsg)).
		Str("lastAssistantMsgPreview", truncateForLog(lastAssistantMsg, 200)).
		Msg("ProcessSummary called")

	// Skip summary generation if there's no meaningful assistant response
	// This prevents generic "initial session setup" summaries
	if !hasMeaningfulContent(lastAssistantMsg) {
		log.Info().
			Int64("sessionId", sessionDBID).
			Int("msgLen", len(lastAssistantMsg)).
			Msg("Skipping summary - no meaningful assistant response")
		return nil
	}

	// Build the summary prompt
	req := SummaryRequest{
		SessionDBID:          sessionDBID,
		SDKSessionID:         sdkSessionID,
		Project:              project,
		UserPrompt:           userPrompt,
		LastUserMessage:      lastUserMsg,
		LastAssistantMessage: lastAssistantMsg,
	}
	prompt := BuildSummaryPrompt(req)

	// Acquire semaphore slot (limits concurrent CLI calls)
	select {
	case p.sem <- struct{}{}:
		defer func() { <-p.sem }()
	case <-ctx.Done():
		return ctx.Err()
	}

	// Call Claude Code CLI
	response, err := p.callClaudeCLI(ctx, prompt)
	if err != nil {
		log.Error().Err(err).Int64("sessionId", sessionDBID).Msg("Failed to call Claude CLI for summary")
		return err
	}

	// Parse summary from response
	summary := ParseSummary(response, sessionDBID)
	if summary == nil {
		log.Info().Int64("sessionId", sessionDBID).Msg("No summary generated (skipped or empty)")
		return nil
	}

	// Filter out summaries that describe the memory agent itself
	if isSelfReferentialSummary(summary) {
		log.Info().Int64("sessionId", sessionDBID).Msg("Skipping self-referential summary (describes agent, not user work)")
		return nil
	}

	// Store the summary (promptNumber=0, discoveryTokens=0 for summaries)
	id, createdAtEpoch, err := p.summaryStore.StoreSummary(ctx, sdkSessionID, project, summary, 0, 0)
	if err != nil {
		log.Error().Err(err).Msg("Failed to store summary")
		return err
	}

	log.Info().
		Int64("id", id).
		Int64("sessionId", sessionDBID).
		Msg("Summary stored")

	// Sync to vector DB if callback is set
	if p.syncSummaryFunc != nil {
		fullSummary := models.NewSessionSummary(sdkSessionID, project, summary, 0, 0)
		fullSummary.ID = id
		fullSummary.CreatedAtEpoch = createdAtEpoch
		p.syncSummaryFunc(fullSummary)
	}

	// Broadcast new summary event for dashboard refresh
	p.broadcast(map[string]any{
		"type":    "summary",
		"action":  "created",
		"id":      id,
		"project": project,
	})

	return nil
}

// MaxPromptSize is the maximum size of a prompt that can be passed to the Claude CLI.
// This prevents resource exhaustion from extremely large prompts.
const MaxPromptSize = 100 * 1024 // 100KB

// sanitizePrompt removes null bytes and control characters from a prompt.
// Keeps newlines, tabs, and carriage returns as they're valid in prompts.
func sanitizePrompt(s string) string {
	return strings.Map(func(r rune) rune {
		// Keep printable ASCII, extended Unicode, and common whitespace
		if r >= 32 || r == '\n' || r == '\t' || r == '\r' {
			return r
		}
		// Remove null bytes and other control characters
		return -1
	}, s)
}

// callClaudeCLI calls the Claude Code CLI with the given prompt.
func (p *Processor) callClaudeCLI(ctx context.Context, prompt string) (string, error) {
	// Validate and sanitize prompt
	if len(prompt) > MaxPromptSize {
		return "", fmt.Errorf("prompt exceeds maximum size of %d bytes", MaxPromptSize)
	}
	prompt = sanitizePrompt(prompt)

	// Build the full prompt with system instructions
	fullPrompt := systemPrompt + "\n\n" + prompt

	// Create command with timeout
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	// Use claude CLI with --print flag for non-interactive output
	// and -p for prompt input
	// Add --tools "" to disable tools (we only need text analysis)
	// Add --strict-mcp-config to skip loading MCP servers
	// Add --disable-slash-commands to skip command loading
	// These flags significantly speed up processing by avoiding plugin/MCP initialization
	cmd := exec.CommandContext(ctx, p.claudePath,
		"--print",
		"--tools", "",
		"--strict-mcp-config",
		"--disable-slash-commands",
		"-p", fullPrompt) // #nosec G204 -- claudePath is from config, fullPrompt is internal

	// Set model if specified (use haiku for cost efficiency)
	if p.model != "" {
		cmd.Args = append([]string{cmd.Args[0], "--model", p.model}, cmd.Args[1:]...)
	} else {
		// Default to haiku for processing (cheap and fast)
		cmd.Args = append([]string{cmd.Args[0], "--model", "haiku"}, cmd.Args[1:]...)
	}

	// Run from /tmp to avoid triggering our own hooks
	// (hooks are triggered based on working directory)
	cmd.Dir = "/tmp"

	// Disable any plugin hooks by setting an env var that our hooks can check
	cmd.Env = append(os.Environ(), "CLAUDE_MNEMONIC_INTERNAL=1")

	// Capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Run command
	err := cmd.Run()
	if err != nil {
		log.Error().
			Err(err).
			Str("stderr", stderr.String()).
			Msg("Claude CLI execution failed")
		return "", fmt.Errorf("claude CLI failed: %w (stderr: %s)", err, stderr.String())
	}

	return stdout.String(), nil
}

// shouldSkipTool returns true for tools that aren't worth processing.
func shouldSkipTool(toolName string) bool {
	// Skip tools that rarely produce meaningful observations
	skipTools := map[string]bool{
		// Internal tracking tools
		"TodoWrite":  true,
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
	}

	skip, found := skipTools[toolName]
	if found {
		return skip
	}
	return false // Process remaining tools: Read, Edit, Write, Grep, Bash, WebFetch, WebSearch, NotebookEdit
}

// shouldSkipTrivialOperation performs local pre-filtering to skip trivial operations
// without making a Haiku API call. Returns true if the operation is too trivial to process.
func shouldSkipTrivialOperation(toolName, inputStr, outputStr string) bool {
	// Skip if output is too small to be meaningful (less than 50 chars)
	// Reduced from 100 to capture more meaningful small operations
	if len(outputStr) < 50 {
		return true
	}

	// Pre-compute lowercase strings once to avoid repeated allocations
	lowerOutput := strings.ToLower(outputStr)
	lowerInput := strings.ToLower(inputStr)

	// Skip if output indicates an error or empty result
	trivialOutputs := []string{
		"no matches found",
		"file not found",
		"directory not found",
		"permission denied",
		"command not found",
		"no such file",
		"is a directory",
		"[]", // Empty array result
		"{}", // Empty object result
	}
	for _, trivial := range trivialOutputs {
		if strings.Contains(lowerOutput, trivial) || outputStr == trivial {
			return true
		}
	}

	// Tool-specific pre-filtering
	switch toolName {
	case "Read":
		// Skip reading config files that rarely contain project-specific insights
		boringFiles := []string{
			"package-lock.json", "yarn.lock", "pnpm-lock.yaml",
			"go.sum", "cargo.lock", "gemfile.lock", "poetry.lock",
			".gitignore", ".dockerignore", ".eslintignore",
			"tsconfig.json", "jsconfig.json", "vite.config",
			"tailwind.config", "postcss.config",
		}
		for _, boring := range boringFiles {
			if strings.Contains(lowerInput, boring) {
				return true
			}
		}

	case "Grep":
		// Skip grep results with too many matches (likely generic search)
		if strings.Count(outputStr, "\n") > 50 {
			return true
		}

	case "Bash":
		// Skip simple status commands (use pre-computed lowerInput)
		boringCommands := []string{
			"git status", "git diff", "git log", "git branch",
			"ls ", "pwd", "echo ", "cat ", "which ", "type ",
			"npm list", "npm outdated", "npm audit",
		}
		for _, boring := range boringCommands {
			if strings.Contains(lowerInput, boring) {
				return true
			}
		}
	}

	return false
}

// toJSONString converts an interface to a JSON string.
func toJSONString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

// safeResolvePath resolves a path relative to cwd and validates it doesn't escape the cwd directory.
// Returns the resolved absolute path and true if valid, or empty string and false if path traversal detected.
// This function is a security sanitizer for path traversal attacks.
func safeResolvePath(path, cwd string) (string, bool) {
	// Clean the input path to normalize any .. or . components
	cleanPath := filepath.Clean(path)

	// Reject paths that explicitly contain parent directory traversal after cleaning
	if strings.Contains(cleanPath, "..") {
		return "", false
	}

	if filepath.IsAbs(cleanPath) {
		// For absolute paths, verify they're within cwd if cwd is specified
		if cwd != "" {
			cleanCwd := filepath.Clean(cwd)
			if !strings.HasPrefix(cleanPath, cleanCwd+string(filepath.Separator)) && cleanPath != cleanCwd {
				return "", false
			}
		}
		return cleanPath, true
	}

	if cwd == "" {
		return cleanPath, true
	}

	// Clean the cwd first
	cleanCwd := filepath.Clean(cwd)

	// Join and clean the path
	absPath := filepath.Join(cleanCwd, cleanPath)

	// Use filepath.Rel to verify the path is actually within cwd
	// If Rel returns a path starting with "..", it escapes the base
	rel, err := filepath.Rel(cleanCwd, absPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", false
	}

	return absPath, true
}

// captureFileMtimes captures current modification times for tracked files.
// Returns a map of absolute file paths to their mtime in epoch milliseconds.
// For large file lists (>10 files), uses parallel stat calls for better performance.
func captureFileMtimes(filesRead, filesModified []string, cwd string) map[string]int64 {
	// Combine all unique file paths
	allPaths := make(map[string]struct{}, len(filesRead)+len(filesModified))
	for _, path := range filesRead {
		allPaths[path] = struct{}{}
	}
	for _, path := range filesModified {
		allPaths[path] = struct{}{}
	}

	// For small lists, use sequential processing (goroutine overhead not worth it)
	if len(allPaths) <= 10 {
		return captureFileMtimesSequential(allPaths, cwd)
	}

	// For larger lists, parallelize with bounded concurrency
	return captureFileMtimesParallel(allPaths, cwd)
}

// captureFileMtimesSequential captures mtimes sequentially (efficient for small lists).
func captureFileMtimesSequential(paths map[string]struct{}, cwd string) map[string]int64 {
	mtimes := make(map[string]int64, len(paths))

	for path := range paths {
		absPath, ok := safeResolvePath(path, cwd)
		if !ok {
			// Skip paths that attempt directory traversal
			continue
		}

		info, err := os.Stat(absPath)
		if err == nil {
			mtimes[path] = info.ModTime().UnixMilli()
		}
	}

	return mtimes
}

// captureFileMtimesParallel captures mtimes in parallel with bounded concurrency.
func captureFileMtimesParallel(paths map[string]struct{}, cwd string) map[string]int64 {
	type mtimeResult struct {
		path  string
		mtime int64
	}

	results := make(chan mtimeResult, len(paths))
	sem := make(chan struct{}, 8) // Limit to 8 concurrent stat calls
	var wg sync.WaitGroup

	for path := range paths {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			sem <- struct{}{}        // Acquire
			defer func() { <-sem }() // Release

			absPath, ok := safeResolvePath(p, cwd)
			if !ok {
				// Skip paths that attempt directory traversal
				return
			}

			info, err := os.Stat(absPath)
			if err == nil {
				results <- mtimeResult{path: p, mtime: info.ModTime().UnixMilli()}
			}
		}(path)
	}

	// Close results channel when all goroutines complete
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	mtimes := make(map[string]int64, len(paths))
	for res := range results {
		mtimes[res.path] = res.mtime
	}

	return mtimes
}

// GetFileMtimes returns current modification times for a list of file paths.
// This is used for staleness checking when injecting context.
func GetFileMtimes(paths []string, cwd string) map[string]int64 {
	return captureFileMtimes(paths, nil, cwd)
}

// GetFileContent reads file content for verification purposes.
// Returns content and ok status.
func GetFileContent(path, cwd string) (string, bool) {
	absPath, ok := safeResolvePath(path, cwd)
	if !ok {
		// Reject paths that attempt directory traversal
		return "", false
	}

	content, err := os.ReadFile(absPath) // #nosec G304 -- path validated by safeResolvePath
	if err != nil {
		return "", false
	}

	// Limit to first 2000 chars for verification (enough context, not too expensive)
	if len(content) > 2000 {
		return string(content[:2000]) + "\n...[truncated]", true
	}
	return string(content), true
}

// VerifyObservation checks if an observation is still valid given the current file contents.
// Returns true if the observation is still accurate, false if it should be deleted.
func (p *Processor) VerifyObservation(ctx context.Context, obs *models.Observation, cwd string) bool {
	// Build file content context
	var fileContents []string
	var paths []string

	// Combine files_read and files_modified
	for _, path := range obs.FilesRead {
		paths = append(paths, path)
	}
	for _, path := range obs.FilesModified {
		paths = append(paths, path)
	}

	// Get current content of tracked files
	for _, path := range paths {
		if content, ok := GetFileContent(path, cwd); ok {
			fileContents = append(fileContents, fmt.Sprintf("=== %s ===\n%s", path, content))
		}
	}

	if len(fileContents) == 0 {
		// No files available to verify against - keep the observation
		return true
	}

	// Build verification prompt
	prompt := fmt.Sprintf(`You are verifying if a previously recorded observation is still accurate.

OBSERVATION:
- Type: %s
- Title: %s
- Subtitle: %s
- Narrative: %s
- Facts: %v

CURRENT FILE CONTENTS:
%s

TASK: Check if the observation is still accurate given the current file contents.
Reply with ONLY one of:
- VALID - if the observation is still accurate
- INVALID - if the observation is no longer accurate (the code/behavior changed)
- UNCERTAIN - if you can't determine validity (files might be incomplete)

Your response:`,
		obs.Type,
		obs.Title.String,
		obs.Subtitle.String,
		obs.Narrative.String,
		obs.Facts,
		strings.Join(fileContents, "\n\n"),
	)

	// Call Claude CLI for quick verification
	response, err := p.callClaudeCLI(ctx, prompt)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to verify observation, keeping it")
		return true // On error, keep the observation
	}

	response = strings.TrimSpace(strings.ToUpper(response))

	// Parse response
	if strings.Contains(response, "INVALID") {
		log.Info().
			Int64("id", obs.ID).
			Str("title", obs.Title.String).
			Msg("Observation verified as INVALID - will delete")
		return false
	}

	// VALID or UNCERTAIN - keep the observation
	log.Debug().
		Int64("id", obs.ID).
		Str("title", obs.Title.String).
		Str("result", response).
		Msg("Observation verified")
	return true
}

// isSelfReferentialSummary checks if a summary describes the memory agent itself
// rather than actual user work. These summaries should be filtered out.
func isSelfReferentialSummary(summary *models.ParsedSummary) bool {
	// Combine all summary fields for checking
	content := strings.ToLower(summary.Request + " " + summary.Completed + " " + summary.Learned + " " + summary.NextSteps + " " + summary.Investigated + " " + summary.Notes)

	// Indicators that the summary is about the memory agent, not user work
	selfReferentialPhrases := []string{
		// Agent references
		"memory extraction",
		"memory agent",
		"extraction agent",
		"hook execution",
		"hook mechanism",
		// Session meta-state
		"session initialization",
		"session setup",
		"session has just started",
		"session just started",
		"agent initialization",
		"no technical learnings",
		"no code or project work",
		// Waiting states
		"waiting for the user",
		"waiting for user",
		"awaiting actual",
		"awaiting claude code",
		"awaiting tool",
		"awaiting user",
		// Meta checkpoint references
		"progress checkpoint",
		"checkpoint request",
		// Common no-work phrases
		"no work has been completed",
		"no work completed",
		"no work done",
		"no actual work",
		"nothing has been completed",
		"nothing completed",
		// Role/guideline parroting
		"role definition",
		"operational guidelines",
		"providing role",
		"providing guidelines",
		// System prompt echoes
		"extract meaningful observations",
		"meaningful learnings",
		"analyze tool executions",
		"observations for future sessions",
		// Empty session indicators
		"empty session",
		"no substantive work",
		"no meaningful work",
		"just beginning",
		"just begun",
	}

	matchCount := 0
	for _, phrase := range selfReferentialPhrases {
		if strings.Contains(content, phrase) {
			matchCount++
		}
	}

	// If the summary mentions 2+ self-referential phrases, it's about the agent
	return matchCount >= 2
}

// hasMeaningfulContent checks if the assistant response contains meaningful content
// worth generating a summary for. This filters out initial greetings, empty sessions,
// and sessions where only system messages were exchanged.
func hasMeaningfulContent(assistantMsg string) bool {
	// Skip if empty or too short (need substantial content)
	if len(strings.TrimSpace(assistantMsg)) < 200 {
		return false
	}

	lowerMsg := strings.ToLower(assistantMsg)

	// Skip messages that are primarily about system/hook status or meta-instructions
	skipIndicators := []string{
		// System/hook markers
		"hook success",
		"callback hook",
		"session start",
		"sessionstart",
		"system-reminder",
		// Agent self-references
		"memory extraction agent",
		"memory agent",
		"extraction agent",
		// No-work indicators
		"no technical learnings",
		"waiting for",
		"waiting to",
		"no code or project work",
		"no substantive",
		"no work has been completed",
		"no work done",
		"awaiting tool",
		"awaiting user",
		// Meta-instruction echoes
		"role definition",
		"operational guidelines",
		"analyze tool executions",
		"extract meaningful observations",
	}

	skipCount := 0
	for _, skip := range skipIndicators {
		if strings.Contains(lowerMsg, skip) {
			skipCount++
		}
	}
	// If multiple skip indicators found, this is likely a system-only session
	if skipCount >= 2 {
		return false
	}

	// Check for indicators of actual work being done
	workIndicators := []string{
		// Concrete file operations (with paths)
		".go", ".ts", ".js", ".py", ".md", ".json", ".yaml", ".yml",
		// Code modifications
		"edited", "modified", "created", "deleted", "updated", "changed",
		"added", "removed", "fixed", "implemented", "refactored",
		// Tool results
		"```", "lines ", "function ", "const ", "var ", "let ",
		"type ", "struct ", "class ", "def ", "func ",
	}

	matchCount := 0
	for _, indicator := range workIndicators {
		if strings.Contains(lowerMsg, strings.ToLower(indicator)) {
			matchCount++
		}
	}

	// Require at least 2 work indicators to generate a summary
	return matchCount >= 2
}

// truncateForLog truncates a string for logging purposes.
func truncateForLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

const systemPrompt = `You are a memory extraction agent for Claude Code sessions. Your job is to analyze tool executions and extract meaningful observations that would be useful for future sessions.

GUIDELINES:
1. Create observations for any meaningful learnings - be generous, not restrictive
2. Focus on: decisions made, bugs fixed, patterns discovered, project structure, code changes, refactoring
3. Even small changes can be worth remembering if they reveal something about the codebase
4. Be concise but informative in your observations
5. Use appropriate type tags: decision, bugfix, feature, refactor, discovery, change

CONCEPT TAGS (use 1-3 of these):
- how-it-works, why-it-exists, what-changed, problem-solution, gotcha
- pattern, trade-off, best-practice, anti-pattern, architecture
- security, performance, testing, debugging, workflow, tooling
- refactoring, api, database, configuration, error-handling

OUTPUT FORMAT:
When you find something worth remembering, output:
<observation>
<type>decision|bugfix|feature|refactor|discovery|change</type>
<title>Short descriptive title</title>
<subtitle>One-line summary</subtitle>
<narrative>Detailed explanation</narrative>
<facts>
<fact>Specific fact 1</fact>
</facts>
<concepts>
<concept>tag1</concept>
</concepts>
<files_read>
<file>/path/to/file</file>
</files_read>
<files_modified>
<file>/path/to/file</file>
</files_modified>
</observation>

If the tool execution is truly trivial (just a directory listing, empty result, etc.), respond with:
<skip reason="trivial"/>

Prefer creating observations over skipping - memories are valuable for future context!`
