// Package config provides configuration management for claude-mnemonic.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	// DefaultWorkerPort is the default HTTP port for the worker service.
	DefaultWorkerPort = 37777

	// DefaultModel for SDK agent (use "haiku" for cost-efficient processing).
	// Claude Code CLI accepts aliases: haiku, sonnet, opus (always latest versions)
	DefaultModel = "haiku"
)

// DefaultObservationTypes are the observation types to include in context.
var DefaultObservationTypes = []string{
	"bugfix", "feature", "refactor", "change", "discovery", "decision",
}

// DefaultObservationConcepts are the concept tags to include in context.
var DefaultObservationConcepts = []string{
	"how-it-works", "why-it-exists", "what-changed",
	"problem-solution", "gotcha", "pattern", "trade-off",
}

// CriticalConcepts are concepts that indicate "must know" information.
// Observations with these concepts are prioritized in context injection.
var CriticalConcepts = []string{
	"gotcha", "pattern", "problem-solution", "trade-off",
}

// Config holds the application configuration.
// Field order optimized for memory alignment (fieldalignment).
type Config struct {
	ContextFullField          string   `json:"context_full_field"`
	DBPath                    string   `json:"db_path"`
	Model                     string   `json:"model"`
	ClaudeCodePath            string   `json:"claude_code_path"`
	EmbeddingModel            string   `json:"embedding_model"`
	VectorStorageStrategy     string   `json:"vector_storage_strategy"`
	ContextObsConcepts        []string `json:"context_obs_concepts"`
	ContextObsTypes           []string `json:"context_obs_types"`
	ContextFullCount          int      `json:"context_full_count"`
	GraphBranchFactor         int      `json:"graph_branch_factor"`
	GraphEdgeWeight           float64  `json:"graph_edge_weight"`
	ContextRelevanceThreshold float64  `json:"context_relevance_threshold"`
	RerankingCandidates       int      `json:"reranking_candidates"`
	WorkerPort                int      `json:"worker_port"`
	RerankingMinImprovement   float64  `json:"reranking_min_improvement"`
	ContextObservations       int      `json:"context_observations"`
	ContextMaxPromptResults   int      `json:"context_max_prompt_results"`
	ContextSessionCount       int      `json:"context_session_count"`
	MaxConns                  int      `json:"max_conns"`
	RerankingAlpha            float64  `json:"reranking_alpha"`
	GraphMaxHops              int      `json:"graph_max_hops"`
	RerankingResults          int      `json:"reranking_results"`
	GraphRebuildIntervalMin   int      `json:"graph_rebuild_interval_min"`
	HubThreshold              int      `json:"hub_threshold"`
	ObservationRetentionDays  int      `json:"observation_retention_days"`
	MaintenanceIntervalHours  int      `json:"maintenance_interval_hours"`
	ContextShowWorkTokens     bool     `json:"context_show_work_tokens"`
	ContextShowReadTokens     bool     `json:"context_show_read_tokens"`
	RerankingPureMode         bool     `json:"reranking_pure_mode"`
	GraphEnabled              bool     `json:"graph_enabled"`
	MaintenanceEnabled        bool     `json:"maintenance_enabled"`
	RerankingEnabled          bool     `json:"reranking_enabled"`
	ContextShowLastSummary    bool     `json:"context_show_last_summary"`
	CleanupStaleObservations  bool     `json:"cleanup_stale_observations"`
}

var (
	globalConfig *Config
	configOnce   sync.Once
	configMu     sync.RWMutex
)

// DataDir returns the data directory path (~/.claude-mnemonic).
func DataDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude-mnemonic")
}

// DBPath returns the database file path.
func DBPath() string {
	return filepath.Join(DataDir(), "claude-mnemonic.db")
}

// SettingsPath returns the settings file path.
func SettingsPath() string {
	return filepath.Join(DataDir(), "settings.json")
}

// EnsureDataDir creates the data directory if it doesn't exist.
// Uses 0700 permissions (owner-only) for security.
func EnsureDataDir() error {
	return os.MkdirAll(DataDir(), 0700)
}

// EnsureSettings creates a default settings file if it doesn't exist.
func EnsureSettings() error {
	path := SettingsPath()

	// Check if file exists
	if _, err := os.Stat(path); err == nil {
		return nil // File exists
	}

	// Create default settings file with comments
	defaultSettings := `{
  "CLAUDE_MNEMONIC_WORKER_PORT": 37777,
  "CLAUDE_MNEMONIC_MODEL": "haiku",
  "CLAUDE_MNEMONIC_CONTEXT_OBSERVATIONS": 100,
  "CLAUDE_MNEMONIC_CONTEXT_FULL_COUNT": 25,
  "CLAUDE_MNEMONIC_CONTEXT_SESSION_COUNT": 10
}
`
	return os.WriteFile(path, []byte(defaultSettings), 0600)
}

// EnsureAll ensures all required directories and files exist.
func EnsureAll() error {
	if err := EnsureDataDir(); err != nil {
		return err
	}
	if err := EnsureSettings(); err != nil {
		return err
	}
	return nil
}

// DefaultEmbeddingModel is the default embedding model to use.
const DefaultEmbeddingModel = "bge-v1.5"

// Default returns a Config with default values.
func Default() *Config {
	return &Config{
		WorkerPort:                DefaultWorkerPort,
		DBPath:                    DBPath(),
		MaxConns:                  4,
		Model:                     DefaultModel,
		EmbeddingModel:            DefaultEmbeddingModel,
		RerankingEnabled:          true,  // Enable by default for improved relevance
		RerankingCandidates:       100,   // Retrieve top 100 candidates
		RerankingResults:          10,    // Return top 10 after reranking
		RerankingAlpha:            0.7,   // Favor cross-encoder score
		RerankingMinImprovement:   0,     // Always apply reranking
		GraphEnabled:              true,  // Enable graph-aware search by default
		GraphMaxHops:              2,     // Two-hop traversal
		GraphBranchFactor:         5,     // Expand top 5 neighbors per node
		GraphEdgeWeight:           0.3,   // Minimum edge weight to follow
		GraphRebuildIntervalMin:   60,    // Rebuild graph every 60 minutes
		VectorStorageStrategy:     "hub", // Hub storage strategy (LEANN-inspired)
		HubThreshold:              5,     // Require 5+ accesses to store embedding
		ContextObservations:       100,
		ContextFullCount:          25,
		ContextSessionCount:       10,
		ContextShowReadTokens:     true,
		ContextShowWorkTokens:     true,
		ContextFullField:          "narrative",
		ContextShowLastSummary:    true,
		ContextObsTypes:           DefaultObservationTypes,
		ContextObsConcepts:        DefaultObservationConcepts,
		ContextRelevanceThreshold: 0.3,   // Minimum 30% similarity to include
		ContextMaxPromptResults:   10,    // Cap at 10 results max (0 = no cap, threshold only)
		MaintenanceEnabled:        true,  // Enable scheduled maintenance
		MaintenanceIntervalHours:  6,     // Run every 6 hours
		ObservationRetentionDays:  0,     // 0 = no age-based deletion (keep all)
		CleanupStaleObservations:  false, // Don't auto-cleanup stale observations
	}
}

// Load loads configuration from the settings file, merging with defaults.
func Load() (*Config, error) {
	cfg := Default()

	data, err := os.ReadFile(SettingsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}

	// Load settings into a map to preserve unknown fields
	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		return cfg, nil // Return defaults on parse error
	}

	// Map settings to config
	if v, ok := settings["CLAUDE_MNEMONIC_WORKER_PORT"].(float64); ok {
		cfg.WorkerPort = int(v)
	}
	if v, ok := settings["CLAUDE_MNEMONIC_MODEL"].(string); ok {
		cfg.Model = v
	}
	if v, ok := settings["CLAUDE_CODE_PATH"].(string); ok {
		cfg.ClaudeCodePath = v
	}
	if v, ok := settings["CLAUDE_MNEMONIC_EMBEDDING_MODEL"].(string); ok && v != "" {
		cfg.EmbeddingModel = v
	}
	// Reranking settings
	if v, ok := settings["CLAUDE_MNEMONIC_RERANKING_ENABLED"].(bool); ok {
		cfg.RerankingEnabled = v
	}
	if v, ok := settings["CLAUDE_MNEMONIC_RERANKING_CANDIDATES"].(float64); ok && v > 0 {
		cfg.RerankingCandidates = int(v)
	}
	if v, ok := settings["CLAUDE_MNEMONIC_RERANKING_RESULTS"].(float64); ok && v > 0 {
		cfg.RerankingResults = int(v)
	}
	if v, ok := settings["CLAUDE_MNEMONIC_RERANKING_ALPHA"].(float64); ok && v >= 0 && v <= 1 {
		cfg.RerankingAlpha = v
	}
	if v, ok := settings["CLAUDE_MNEMONIC_RERANKING_MIN_IMPROVEMENT"].(float64); ok && v >= 0 {
		cfg.RerankingMinImprovement = v
	}
	if v, ok := settings["CLAUDE_MNEMONIC_RERANKING_PURE_MODE"].(bool); ok {
		cfg.RerankingPureMode = v
	}
	if v, ok := settings["CLAUDE_MNEMONIC_CONTEXT_OBSERVATIONS"].(float64); ok {
		cfg.ContextObservations = int(v)
	}
	if v, ok := settings["CLAUDE_MNEMONIC_CONTEXT_FULL_COUNT"].(float64); ok {
		cfg.ContextFullCount = int(v)
	}
	if v, ok := settings["CLAUDE_MNEMONIC_CONTEXT_SESSION_COUNT"].(float64); ok {
		cfg.ContextSessionCount = int(v)
	}
	if v, ok := settings["CLAUDE_MNEMONIC_CONTEXT_OBS_TYPES"].(string); ok && v != "" {
		cfg.ContextObsTypes = splitTrim(v)
	}
	if v, ok := settings["CLAUDE_MNEMONIC_CONTEXT_OBS_CONCEPTS"].(string); ok && v != "" {
		cfg.ContextObsConcepts = splitTrim(v)
	}
	if v, ok := settings["CLAUDE_MNEMONIC_CONTEXT_RELEVANCE_THRESHOLD"].(float64); ok && v >= 0 && v <= 1 {
		cfg.ContextRelevanceThreshold = v
	}
	if v, ok := settings["CLAUDE_MNEMONIC_CONTEXT_MAX_PROMPT_RESULTS"].(float64); ok && v >= 0 {
		cfg.ContextMaxPromptResults = int(v)
	}
	// Graph settings
	if v, ok := settings["CLAUDE_MNEMONIC_GRAPH_ENABLED"].(bool); ok {
		cfg.GraphEnabled = v
	}
	if v, ok := settings["CLAUDE_MNEMONIC_GRAPH_MAX_HOPS"].(float64); ok && v > 0 {
		cfg.GraphMaxHops = int(v)
	}
	if v, ok := settings["CLAUDE_MNEMONIC_GRAPH_BRANCH_FACTOR"].(float64); ok && v > 0 {
		cfg.GraphBranchFactor = int(v)
	}
	if v, ok := settings["CLAUDE_MNEMONIC_GRAPH_EDGE_WEIGHT"].(float64); ok && v >= 0 && v <= 1 {
		cfg.GraphEdgeWeight = v
	}
	if v, ok := settings["CLAUDE_MNEMONIC_GRAPH_REBUILD_INTERVAL_MIN"].(float64); ok && v > 0 {
		cfg.GraphRebuildIntervalMin = int(v)
	}
	// Vector storage settings (LEANN Phase 2)
	if v, ok := settings["CLAUDE_MNEMONIC_VECTOR_STORAGE_STRATEGY"].(string); ok && v != "" {
		cfg.VectorStorageStrategy = v
	}
	if v, ok := settings["CLAUDE_MNEMONIC_HUB_THRESHOLD"].(float64); ok && v > 0 {
		cfg.HubThreshold = int(v)
	}

	return cfg, nil
}

// splitTrim splits a comma-separated string and trims whitespace.
func splitTrim(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// Get returns the global configuration, loading it if necessary.
func Get() *Config {
	configOnce.Do(func() {
		var err error
		globalConfig, err = Load()
		if err != nil {
			globalConfig = Default()
		}
	})

	configMu.RLock()
	defer configMu.RUnlock()
	return globalConfig
}

// GetWorkerPort returns the worker port from environment or config.
func GetWorkerPort() int {
	if port := os.Getenv("CLAUDE_MNEMONIC_WORKER_PORT"); port != "" {
		var p int
		if err := json.Unmarshal([]byte(port), &p); err == nil && p > 0 {
			return p
		}
	}
	return Get().WorkerPort
}
