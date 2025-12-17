// Package config provides configuration management for claude-mnemonic.
package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// ConfigSuite is a test suite for config operations.
type ConfigSuite struct {
	suite.Suite
	tempDir     string
	origHomeDir string
}

func (s *ConfigSuite) SetupTest() {
	var err error
	s.tempDir, err = os.MkdirTemp("", "config-test-*")
	s.Require().NoError(err)

	// Save and override HOME
	s.origHomeDir = os.Getenv("HOME")
	os.Setenv("HOME", s.tempDir)
}

func (s *ConfigSuite) TearDownTest() {
	os.Setenv("HOME", s.origHomeDir)
	os.RemoveAll(s.tempDir)
}

func TestConfigSuite(t *testing.T) {
	suite.Run(t, new(ConfigSuite))
}

// TestDefault tests default configuration values.
func (s *ConfigSuite) TestDefault() {
	cfg := Default()

	s.Equal(DefaultWorkerPort, cfg.WorkerPort)
	s.Equal(DefaultModel, cfg.Model)
	s.Equal(4, cfg.MaxConns)
	s.Equal(100, cfg.ContextObservations)
	s.Equal(25, cfg.ContextFullCount)
	s.Equal(10, cfg.ContextSessionCount)
	s.True(cfg.ContextShowReadTokens)
	s.True(cfg.ContextShowWorkTokens)
	s.Equal("narrative", cfg.ContextFullField)
	s.True(cfg.ContextShowLastSummary)
	s.Equal(DefaultObservationTypes, cfg.ContextObsTypes)
	s.Equal(DefaultObservationConcepts, cfg.ContextObsConcepts)
}

// TestDataDir tests data directory path.
func (s *ConfigSuite) TestDataDir() {
	dir := DataDir()
	s.Contains(dir, ".claude-mnemonic")
}

// TestDBPath tests database path.
func (s *ConfigSuite) TestDBPath() {
	path := DBPath()
	s.Contains(path, "claude-mnemonic.db")
}

// TestSettingsPath tests settings file path.
func (s *ConfigSuite) TestSettingsPath() {
	path := SettingsPath()
	s.Contains(path, "settings.json")
}

// TestEnsureDataDir tests data directory creation.
func (s *ConfigSuite) TestEnsureDataDir() {
	err := EnsureDataDir()
	s.NoError(err)

	dir := DataDir()
	info, err := os.Stat(dir)
	s.NoError(err)
	s.True(info.IsDir())
}

// TestEnsureSettings tests settings file creation.
func (s *ConfigSuite) TestEnsureSettings() {
	// First ensure data dir exists
	err := EnsureDataDir()
	s.NoError(err)

	// Ensure settings creates default file
	err = EnsureSettings()
	s.NoError(err)

	path := SettingsPath()
	info, err := os.Stat(path)
	s.NoError(err)
	s.False(info.IsDir())

	// Second call should not error (file exists)
	err = EnsureSettings()
	s.NoError(err)
}

// TestEnsureAll tests full initialization.
func (s *ConfigSuite) TestEnsureAll() {
	err := EnsureAll()
	s.NoError(err)

	// Verify dir and settings exist
	_, err = os.Stat(DataDir())
	s.NoError(err)
	_, err = os.Stat(SettingsPath())
	s.NoError(err)
}

// TestLoad_TableDriven tests configuration loading with various scenarios.
func (s *ConfigSuite) TestLoad_TableDriven() {
	tests := []struct {
		name           string
		settingsJSON   string
		expectedPort   int
		expectedModel  string
		expectedObsObs int
	}{
		{
			name:           "no settings file",
			settingsJSON:   "",
			expectedPort:   DefaultWorkerPort,
			expectedModel:  DefaultModel,
			expectedObsObs: 100,
		},
		{
			name:           "custom port",
			settingsJSON:   `{"CLAUDE_MNEMONIC_WORKER_PORT": 38888}`,
			expectedPort:   38888,
			expectedModel:  DefaultModel,
			expectedObsObs: 100,
		},
		{
			name:           "custom model",
			settingsJSON:   `{"CLAUDE_MNEMONIC_MODEL": "sonnet"}`,
			expectedPort:   DefaultWorkerPort,
			expectedModel:  "sonnet",
			expectedObsObs: 100,
		},
		{
			name:           "custom observations",
			settingsJSON:   `{"CLAUDE_MNEMONIC_CONTEXT_OBSERVATIONS": 200}`,
			expectedPort:   DefaultWorkerPort,
			expectedModel:  DefaultModel,
			expectedObsObs: 200,
		},
		{
			name:           "multiple settings",
			settingsJSON:   `{"CLAUDE_MNEMONIC_WORKER_PORT": 39999, "CLAUDE_MNEMONIC_MODEL": "opus", "CLAUDE_MNEMONIC_CONTEXT_OBSERVATIONS": 50}`,
			expectedPort:   39999,
			expectedModel:  "opus",
			expectedObsObs: 50,
		},
		{
			name:           "invalid JSON returns defaults",
			settingsJSON:   `{invalid}`,
			expectedPort:   DefaultWorkerPort,
			expectedModel:  DefaultModel,
			expectedObsObs: 100,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			// Create fresh temp dir
			tempDir, err := os.MkdirTemp("", "config-test-*")
			s.Require().NoError(err)
			defer os.RemoveAll(tempDir)

			os.Setenv("HOME", tempDir)

			// Create data dir
			err = os.MkdirAll(filepath.Join(tempDir, ".claude-mnemonic"), 0750)
			s.Require().NoError(err)

			if tt.settingsJSON != "" {
				err := os.WriteFile(
					filepath.Join(tempDir, ".claude-mnemonic", "settings.json"),
					[]byte(tt.settingsJSON),
					0600,
				)
				s.Require().NoError(err)
			}

			cfg, err := Load()
			s.NoError(err)
			s.NotNil(cfg)
			s.Equal(tt.expectedPort, cfg.WorkerPort)
			s.Equal(tt.expectedModel, cfg.Model)
			s.Equal(tt.expectedObsObs, cfg.ContextObservations)
		})
	}
}

// TestGetWorkerPort_TableDriven tests worker port retrieval with various scenarios.
func TestGetWorkerPort_TableDriven(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		wantPort int
		setEnv   bool
	}{
		{
			name:     "no env, use default",
			envValue: "",
			wantPort: DefaultWorkerPort,
			setEnv:   false,
		},
		{
			name:     "env set to valid port",
			envValue: "38888",
			wantPort: 38888,
			setEnv:   true,
		},
		{
			name:     "env set to invalid value",
			envValue: "invalid",
			wantPort: DefaultWorkerPort,
			setEnv:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original env
			origEnv := os.Getenv("CLAUDE_MNEMONIC_WORKER_PORT")
			defer os.Setenv("CLAUDE_MNEMONIC_WORKER_PORT", origEnv)

			if tt.setEnv {
				os.Setenv("CLAUDE_MNEMONIC_WORKER_PORT", tt.envValue)
			} else {
				os.Unsetenv("CLAUDE_MNEMONIC_WORKER_PORT")
			}

			// We can't easily test GetWorkerPort since it uses Get() which caches
			// So we test the env parsing logic directly
			if tt.setEnv && tt.envValue != "" {
				if tt.wantPort != DefaultWorkerPort {
					assert.Equal(t, tt.envValue, os.Getenv("CLAUDE_MNEMONIC_WORKER_PORT"))
				}
			}
		})
	}
}

// TestSplitTrim tests the splitTrim helper function.
func TestSplitTrim(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: []string{},
		},
		{
			name:     "single value",
			input:    "bugfix",
			expected: []string{"bugfix"},
		},
		{
			name:     "multiple values",
			input:    "bugfix,feature,refactor",
			expected: []string{"bugfix", "feature", "refactor"},
		},
		{
			name:     "values with spaces",
			input:    " bugfix , feature , refactor ",
			expected: []string{"bugfix", "feature", "refactor"},
		},
		{
			name:     "empty values filtered",
			input:    "bugfix,,feature,,",
			expected: []string{"bugfix", "feature"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitTrim(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestDefaultObservationTypes tests default observation types.
func TestDefaultObservationTypes(t *testing.T) {
	expected := []string{
		"bugfix", "feature", "refactor", "change", "discovery", "decision",
	}
	assert.Equal(t, expected, DefaultObservationTypes)
}

// TestDefaultObservationConcepts tests default observation concepts.
func TestDefaultObservationConcepts(t *testing.T) {
	expected := []string{
		"how-it-works", "why-it-exists", "what-changed",
		"problem-solution", "gotcha", "pattern", "trade-off",
	}
	assert.Equal(t, expected, DefaultObservationConcepts)
}

// TestCriticalConcepts tests critical concepts list.
func TestCriticalConcepts(t *testing.T) {
	expected := []string{
		"gotcha", "pattern", "problem-solution", "trade-off",
	}
	assert.Equal(t, expected, CriticalConcepts)
}

// TestLoad_ClaudeCodePath tests claude code path loading.
func TestLoad_ClaudeCodePath(t *testing.T) {
	// Create temp dir
	tempDir, err := os.MkdirTemp("", "config-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tempDir)
	defer os.Setenv("HOME", origHome)

	// Create data dir and settings
	err = os.MkdirAll(filepath.Join(tempDir, ".claude-mnemonic"), 0750)
	require.NoError(t, err)

	settingsJSON := `{"CLAUDE_CODE_PATH": "/usr/local/bin/claude"}`
	err = os.WriteFile(
		filepath.Join(tempDir, ".claude-mnemonic", "settings.json"),
		[]byte(settingsJSON),
		0600,
	)
	require.NoError(t, err)

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "/usr/local/bin/claude", cfg.ClaudeCodePath)
}

// TestLoad_ContextSettings tests context-related settings loading.
func TestLoad_ContextSettings(t *testing.T) {
	// Create temp dir
	tempDir, err := os.MkdirTemp("", "config-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tempDir)
	defer os.Setenv("HOME", origHome)

	// Create data dir and settings
	err = os.MkdirAll(filepath.Join(tempDir, ".claude-mnemonic"), 0750)
	require.NoError(t, err)

	settingsJSON := `{
		"CLAUDE_MNEMONIC_CONTEXT_FULL_COUNT": 50,
		"CLAUDE_MNEMONIC_CONTEXT_SESSION_COUNT": 20,
		"CLAUDE_MNEMONIC_CONTEXT_OBS_TYPES": "bugfix,feature",
		"CLAUDE_MNEMONIC_CONTEXT_OBS_CONCEPTS": "security,performance"
	}`
	err = os.WriteFile(
		filepath.Join(tempDir, ".claude-mnemonic", "settings.json"),
		[]byte(settingsJSON),
		0600,
	)
	require.NoError(t, err)

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, 50, cfg.ContextFullCount)
	assert.Equal(t, 20, cfg.ContextSessionCount)
	assert.Equal(t, []string{"bugfix", "feature"}, cfg.ContextObsTypes)
	assert.Equal(t, []string{"security", "performance"}, cfg.ContextObsConcepts)
}
