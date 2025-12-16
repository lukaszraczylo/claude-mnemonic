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
type Config struct {
	// Worker settings
	WorkerPort int `json:"worker_port"`

	// Database settings
	DBPath   string `json:"db_path"`
	MaxConns int    `json:"max_conns"`

	// SDK Agent settings
	Model          string `json:"model"`
	ClaudeCodePath string `json:"claude_code_path"`

	// Context injection settings
	ContextObservations    int      `json:"context_observations"`
	ContextFullCount       int      `json:"context_full_count"`
	ContextSessionCount    int      `json:"context_session_count"`
	ContextShowReadTokens  bool     `json:"context_show_read_tokens"`
	ContextShowWorkTokens  bool     `json:"context_show_work_tokens"`
	ContextFullField       string   `json:"context_full_field"`
	ContextShowLastSummary bool     `json:"context_show_last_summary"`
	ContextObsTypes        []string `json:"context_obs_types"`
	ContextObsConcepts     []string `json:"context_obs_concepts"`
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
func EnsureDataDir() error {
	return os.MkdirAll(DataDir(), 0750)
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

// Default returns a Config with default values.
func Default() *Config {
	return &Config{
		WorkerPort:             DefaultWorkerPort,
		DBPath:                 DBPath(),
		MaxConns:               4,
		Model:                  DefaultModel,
		ContextObservations:    100,
		ContextFullCount:       25,
		ContextSessionCount:    10,
		ContextShowReadTokens:  true,
		ContextShowWorkTokens:  true,
		ContextFullField:       "narrative",
		ContextShowLastSummary: true,
		ContextObsTypes:        DefaultObservationTypes,
		ContextObsConcepts:     DefaultObservationConcepts,
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
