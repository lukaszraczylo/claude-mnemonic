// Package sdk provides SDK agent integration for claude-mnemonic.
package sdk

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	json "github.com/goccy/go-json"

	"github.com/lukaszraczylo/claude-mnemonic/internal/config"
	"github.com/lukaszraczylo/claude-mnemonic/internal/db/sqlite"
	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
	"github.com/lukaszraczylo/claude-mnemonic/pkg/similarity"
	"github.com/rs/zerolog/log"
)

// BroadcastFunc is a callback for broadcasting events to SSE clients.
type BroadcastFunc func(event map[string]interface{})

// SyncObservationFunc is a callback for syncing observations to vector DB.
type SyncObservationFunc func(obs *models.Observation)

// SyncSummaryFunc is a callback for syncing summaries to vector DB.
type SyncSummaryFunc func(summary *models.SessionSummary)

// Processor handles SDK agent processing of observations and summaries using Claude Code CLI.
type Processor struct {
	claudePath          string
	model               string
	observationStore    *sqlite.ObservationStore
	summaryStore        *sqlite.SummaryStore
	broadcastFunc       BroadcastFunc
	syncObservationFunc SyncObservationFunc
	syncSummaryFunc     SyncSummaryFunc
	mu                  sync.Mutex
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
func (p *Processor) broadcast(event map[string]interface{}) {
	if p.broadcastFunc != nil {
		p.broadcastFunc(event)
	}
}

// NewProcessor creates a new SDK processor.
func NewProcessor(observationStore *sqlite.ObservationStore, summaryStore *sqlite.SummaryStore) (*Processor, error) {
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
	}, nil
}

// IsAvailable checks if the Claude CLI is available for processing.
func (p *Processor) IsAvailable() bool {
	_, err := os.Stat(p.claudePath)
	return err == nil
}

// ProcessObservation processes a single tool observation and extracts insights.
func (p *Processor) ProcessObservation(ctx context.Context, sdkSessionID, project string, toolName string, toolInput, toolResponse interface{}, promptNumber int, cwd string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Skip certain tools that aren't worth processing
	if shouldSkipTool(toolName) {
		log.Info().Str("tool", toolName).Msg("Skipping tool (not interesting for memory)")
		return nil
	}

	log.Info().Str("tool", toolName).Msg("Processing tool execution with Claude CLI")

	// Convert tool data to strings
	inputStr := toJSONString(toolInput)
	outputStr := toJSONString(toolResponse)

	// Check if we already have observations for this file (skip if covered)
	if filePath := extractFilePath(toolName, inputStr); filePath != "" {
		exists, err := p.observationStore.ExistsSimilarObservation(ctx, project, []string{filePath}, nil)
		if err == nil && exists {
			log.Debug().
				Str("tool", toolName).
				Str("file", filePath).
				Msg("Skipping - file already has observations")
			return nil
		}
	}

	// Build the prompt
	exec := ToolExecution{
		ToolName:   toolName,
		ToolInput:  inputStr,
		ToolOutput: outputStr,
		CWD:        cwd,
	}
	prompt := BuildObservationPrompt(exec)

	// Call Claude Code CLI
	response, err := p.callClaudeCLI(ctx, prompt)
	if err != nil {
		log.Error().Err(err).Str("tool", toolName).Msg("Failed to call Claude CLI for observation")
		return err
	}

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

		// Sync to vector DB if callback is set
		if p.syncObservationFunc != nil {
			fullObs := models.NewObservation(sdkSessionID, project, obs, promptNumber, 0)
			fullObs.ID = id
			fullObs.CreatedAtEpoch = createdAtEpoch
			p.syncObservationFunc(fullObs)
		}

		// Broadcast new observation event for dashboard refresh
		p.broadcast(map[string]interface{}{
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
	p.mu.Lock()
	defer p.mu.Unlock()

	// Skip summary generation if there's no meaningful assistant response
	// This prevents generic "initial session setup" summaries
	if !hasMeaningfulContent(lastAssistantMsg) {
		log.Info().
			Int64("sessionId", sessionDBID).
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
	p.broadcast(map[string]interface{}{
		"type":    "summary",
		"action":  "created",
		"id":      id,
		"project": project,
	})

	return nil
}

// callClaudeCLI calls the Claude Code CLI with the given prompt.
func (p *Processor) callClaudeCLI(ctx context.Context, prompt string) (string, error) {
	// Build the full prompt with system instructions
	fullPrompt := systemPrompt + "\n\n" + prompt

	// Create command with timeout
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	// Use claude CLI with --print flag for non-interactive output
	// and -p for prompt input
	cmd := exec.CommandContext(ctx, p.claudePath, "--print", "-p", fullPrompt) // #nosec G204 -- claudePath is from config, fullPrompt is internal

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
	// Only skip truly uninteresting tools
	skipTools := map[string]bool{
		"TodoWrite":  true, // Skip TodoWrite - internal tracking
		"Task":       true, // Skip Task - sub-agent spawning
		"TaskOutput": true, // Skip TaskOutput - sub-agent results
		"Glob":       true, // Skip Glob - just file listing
	}

	skip, found := skipTools[toolName]
	if found {
		return skip
	}
	return false // Process all other tools
}

// extractFilePath extracts the file path from tool input for deduplication.
func extractFilePath(toolName, inputStr string) string {
	if inputStr == "" {
		return ""
	}

	var input map[string]interface{}
	if err := json.Unmarshal([]byte(inputStr), &input); err != nil {
		return ""
	}

	// Handle different tool input formats
	switch toolName {
	case "Read":
		if fp, ok := input["file_path"].(string); ok {
			return fp
		}
	case "Grep", "Search":
		if path, ok := input["path"].(string); ok {
			return path
		}
	case "Edit", "Write":
		if fp, ok := input["file_path"].(string); ok {
			return fp
		}
	}

	return ""
}

// toJSONString converts an interface to a JSON string.
func toJSONString(v interface{}) string {
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

// captureFileMtimes captures current modification times for tracked files.
// Returns a map of absolute file paths to their mtime in epoch milliseconds.
func captureFileMtimes(filesRead, filesModified []string, cwd string) map[string]int64 {
	mtimes := make(map[string]int64)

	// Helper to get mtime for a file path
	getMtime := func(path string) (int64, bool) {
		// Resolve relative paths against cwd
		absPath := path
		if !filepath.IsAbs(path) && cwd != "" {
			absPath = filepath.Join(cwd, path)
		}

		info, err := os.Stat(absPath)
		if err != nil {
			return 0, false
		}
		return info.ModTime().UnixMilli(), true
	}

	// Capture mtimes for all read files
	for _, path := range filesRead {
		if mtime, ok := getMtime(path); ok {
			mtimes[path] = mtime
		}
	}

	// Capture mtimes for all modified files
	for _, path := range filesModified {
		if mtime, ok := getMtime(path); ok {
			mtimes[path] = mtime
		}
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
	absPath := path
	if !filepath.IsAbs(path) && cwd != "" {
		absPath = filepath.Join(cwd, path)
	}

	content, err := os.ReadFile(absPath) // #nosec G304 -- intentional file read for verification
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
	content := strings.ToLower(summary.Request + " " + summary.Completed + " " + summary.Learned + " " + summary.NextSteps)

	// Indicators that the summary is about the memory agent, not user work
	selfReferentialPhrases := []string{
		"memory extraction",
		"memory agent",
		"hook execution",
		"hook mechanism",
		"session initialization",
		"session setup",
		"agent initialization",
		"no technical learnings",
		"no code or project work",
		"waiting for the user",
		"waiting for user",
		"awaiting actual",
		"awaiting claude code",
		"progress checkpoint",
		"checkpoint request",
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

	// Skip messages that are primarily about system/hook status
	skipIndicators := []string{
		"hook success",
		"callback hook",
		"session start",
		"sessionstart",
		"system-reminder",
		"memory extraction agent",
		"memory agent",
		"no technical learnings",
		"waiting for",
		"waiting to",
		"no code or project work",
		"no substantive",
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

const systemPrompt = `You are a memory extraction agent for Claude Code sessions. Your job is to analyze tool executions and extract meaningful observations that would be useful for future sessions.

GUIDELINES:
1. Only create observations for SIGNIFICANT learnings - not every tool call needs one
2. Focus on: decisions made, bugs fixed, patterns discovered, project structure learned
3. Skip trivial operations like simple file reads without insights
4. Be concise but informative in your observations
5. Use appropriate type tags: decision, bugfix, feature, refactor, discovery, change

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

If the tool execution is not noteworthy, simply respond with:
<skip reason="not significant"/>`
