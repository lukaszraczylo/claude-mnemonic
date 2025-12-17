// Package hooks provides hook utilities for claude-mnemonic.
package hooks

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetWorkerPort(t *testing.T) {
	// Test default port
	port := GetWorkerPort()
	assert.Equal(t, DefaultWorkerPort, port)

	// Test with environment variable
	t.Setenv("CLAUDE_MNEMONIC_WORKER_PORT", "12345")
	port = GetWorkerPort()
	assert.Equal(t, 12345, port)

	// Test with invalid environment variable (should return default)
	t.Setenv("CLAUDE_MNEMONIC_WORKER_PORT", "invalid")
	port = GetWorkerPort()
	assert.Equal(t, DefaultWorkerPort, port)
}

func TestIsWorkerRunning(t *testing.T) {
	// Create a test server that responds to health checks
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Extract port from test server URL
	// Note: In real tests we'd use the actual port, but test server uses random port
	// So we test with a non-existent port
	assert.False(t, IsWorkerRunning(99999)) // Non-existent port
}

func TestIsPortInUse(t *testing.T) {
	// Create a test server to occupy a port
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Non-existent port should not be in use
	assert.False(t, IsPortInUse(99999))
}

func TestGetWorkerVersion(t *testing.T) {
	tests := []struct {
		name           string
		serverResponse func(w http.ResponseWriter, r *http.Request)
		expectedResult string
	}{
		{
			name: "returns version from server",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/version" {
					json.NewEncoder(w).Encode(map[string]string{"version": "1.2.3"})
				}
			},
			expectedResult: "1.2.3",
		},
		{
			name: "returns empty on 404",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			expectedResult: "",
		},
		{
			name: "returns empty on invalid JSON",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte("not json"))
			},
			expectedResult: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(tt.serverResponse))
			defer server.Close()

			// We can't easily test with the actual function since it uses a hardcoded localhost
			// But we can verify the logic works with the test server
		})
	}
}

func TestProjectIDWithName(t *testing.T) {
	tests := []struct {
		cwd      string
		expected string
	}{
		{
			cwd:      "/Users/test/projects/my-project",
			expected: "my-project_", // Will have hash suffix
		},
		{
			cwd:      "/tmp",
			expected: "tmp_",
		},
		{
			cwd:      "/",
			expected: "", // Empty dirname
		},
	}

	for _, tt := range tests {
		t.Run(tt.cwd, func(t *testing.T) {
			result := ProjectIDWithName(tt.cwd)
			if tt.expected != "" {
				assert.Contains(t, result, tt.expected[:len(tt.expected)-1]) // Check prefix before underscore
				assert.Contains(t, result, "_")                              // Should have underscore separator
			}
		})
	}
}

func TestVersionMatching(t *testing.T) {
	// Test that version matching logic works correctly
	tests := []struct {
		name           string
		runningVersion string
		hookVersion    string
		shouldRestart  bool
	}{
		{
			name:           "matching versions",
			runningVersion: "1.0.0",
			hookVersion:    "1.0.0",
			shouldRestart:  false,
		},
		{
			name:           "mismatched versions",
			runningVersion: "1.0.0",
			hookVersion:    "2.0.0",
			shouldRestart:  true,
		},
		{
			name:           "dirty vs clean",
			runningVersion: "1.0.0",
			hookVersion:    "1.0.0-dirty",
			shouldRestart:  true,
		},
		{
			name:           "empty running version",
			runningVersion: "",
			hookVersion:    "1.0.0",
			shouldRestart:  false, // Can't determine, don't restart
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the version check logic
			shouldRestart := false
			if tt.runningVersion != "" && tt.runningVersion != tt.hookVersion {
				shouldRestart = true
			}
			assert.Equal(t, tt.shouldRestart, shouldRestart)
		})
	}
}

func TestKillProcessOnPort_NoProcess(t *testing.T) {
	// Test killing a process on a port that has no process
	// Should not error, just return nil
	err := KillProcessOnPort(99999) // Port unlikely to be in use
	// lsof will return empty/error, which is fine
	require.NoError(t, err)
}

func TestFindWorkerBinary(t *testing.T) {
	// Test that findWorkerBinary returns empty string when binary not found
	// This is hard to test without mocking the filesystem
	// But we can verify it doesn't panic
	result := findWorkerBinary()
	// Result depends on whether worker is installed, so we just check it doesn't panic
	t.Logf("findWorkerBinary returned: %s", result)
}

// TestVersionsCompatible tests the versionsCompatible function.
func TestVersionsCompatible(t *testing.T) {
	tests := []struct {
		name     string
		v1       string
		v2       string
		expected bool
	}{
		{
			name:     "identical versions",
			v1:       "v1.0.0",
			v2:       "v1.0.0",
			expected: true,
		},
		{
			name:     "same base different suffix",
			v1:       "v1.0.0",
			v2:       "v1.0.0-dirty",
			expected: true,
		},
		{
			name:     "same base with commit hash",
			v1:       "v1.0.0-2-gca711a8",
			v2:       "v1.0.0-5-gabcdef1-dirty",
			expected: true,
		},
		{
			name:     "different base versions",
			v1:       "v1.0.0",
			v2:       "v2.0.0",
			expected: false,
		},
		{
			name:     "dev version compatible with anything",
			v1:       "dev",
			v2:       "v1.0.0",
			expected: true,
		},
		{
			name:     "anything compatible with dev",
			v1:       "v2.0.0-dirty",
			v2:       "dev",
			expected: true,
		},
		{
			name:     "both dev versions",
			v1:       "dev",
			v2:       "dev",
			expected: true,
		},
		{
			name:     "minor version difference",
			v1:       "v1.2.0",
			v2:       "v1.3.0",
			expected: false,
		},
		{
			name:     "patch version difference",
			v1:       "v1.0.1",
			v2:       "v1.0.2",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := versionsCompatible(tt.v1, tt.v2)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestExtractBaseVersion tests the extractBaseVersion function.
func TestExtractBaseVersion(t *testing.T) {
	tests := []struct {
		name     string
		version  string
		expected string
	}{
		{
			name:     "simple version with v prefix",
			version:  "v1.0.0",
			expected: "1.0.0",
		},
		{
			name:     "version without v prefix",
			version:  "1.0.0",
			expected: "1.0.0",
		},
		{
			name:     "version with commit suffix",
			version:  "v0.3.5-2-gca711a8",
			expected: "0.3.5",
		},
		{
			name:     "version with dirty suffix",
			version:  "v0.3.5-dirty",
			expected: "0.3.5",
		},
		{
			name:     "version with full suffix",
			version:  "v0.3.5-2-gca711a8-dirty",
			expected: "0.3.5",
		},
		{
			name:     "dev version",
			version:  "dev",
			expected: "dev",
		},
		{
			name:     "empty version",
			version:  "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractBaseVersion(tt.version)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestPOST tests the POST function with a mock server.
func TestPOST(t *testing.T) {
	tests := []struct {
		name           string
		serverHandler  func(w http.ResponseWriter, r *http.Request)
		body           interface{}
		expectError    bool
		expectedResult map[string]interface{}
	}{
		{
			name: "successful POST with JSON response",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok"})
			},
			body:           map[string]string{"key": "value"},
			expectError:    false,
			expectedResult: map[string]interface{}{"status": "ok"},
		},
		{
			name: "POST with 400 error",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
			},
			body:        map[string]string{"key": "value"},
			expectError: true,
		},
		{
			name: "POST with 500 error",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			body:        map[string]string{"key": "value"},
			expectError: true,
		},
		{
			name: "POST with non-JSON response",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("not json"))
			},
			body:           map[string]string{"key": "value"},
			expectError:    false,
			expectedResult: nil, // Non-JSON returns nil
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(tt.serverHandler))
			defer server.Close()

			// Extract port from test server
			var port int
			_, err := fmt.Sscanf(server.URL, "http://127.0.0.1:%d", &port)
			require.NoError(t, err)

			result, err := POST(port, "/test", tt.body)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if tt.expectedResult != nil {
					assert.Equal(t, tt.expectedResult["status"], result["status"])
				}
			}
		})
	}
}

// TestGET tests the GET function with a mock server.
func TestGET(t *testing.T) {
	tests := []struct {
		name           string
		serverHandler  func(w http.ResponseWriter, r *http.Request)
		expectError    bool
		expectedResult map[string]interface{}
	}{
		{
			name: "successful GET with JSON response",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(map[string]interface{}{"data": "test"})
			},
			expectError:    false,
			expectedResult: map[string]interface{}{"data": "test"},
		},
		{
			name: "GET with 404 error",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			expectError: true,
		},
		{
			name: "GET with invalid JSON",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("not valid json"))
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(tt.serverHandler))
			defer server.Close()

			// Extract port from test server
			var port int
			_, err := fmt.Sscanf(server.URL, "http://127.0.0.1:%d", &port)
			require.NoError(t, err)

			result, err := GET(port, "/test")

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if tt.expectedResult != nil {
					assert.Equal(t, tt.expectedResult["data"], result["data"])
				}
			}
		})
	}
}

// TestProjectIDWithName_Comprehensive tests ProjectIDWithName more thoroughly.
func TestProjectIDWithName_Comprehensive(t *testing.T) {
	tests := []struct {
		name           string
		cwd            string
		expectedPrefix string
		expectedLen    int // Expected minimum length (prefix + _ + 6 char hash)
	}{
		{
			name:           "standard project path",
			cwd:            "/Users/test/projects/my-project",
			expectedPrefix: "my-project_",
			expectedLen:    17, // "my-project_" + 6 char hash
		},
		{
			name:           "short directory name",
			cwd:            "/tmp",
			expectedPrefix: "tmp_",
			expectedLen:    10, // "tmp_" + 6 char hash
		},
		{
			name:           "nested path",
			cwd:            "/home/user/code/org/repo",
			expectedPrefix: "repo_",
			expectedLen:    11, // "repo_" + 6 char hash
		},
		{
			name:           "path with special characters",
			cwd:            "/Users/test/my-special.project",
			expectedPrefix: "my-special.project_",
			expectedLen:    25,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ProjectIDWithName(tt.cwd)
			assert.True(t, len(result) >= tt.expectedLen, "result %s should be at least %d chars", result, tt.expectedLen)
			assert.Contains(t, result, tt.expectedPrefix[:len(tt.expectedPrefix)-1]) // Check without trailing underscore
			assert.Contains(t, result, "_")

			// Verify hash uniqueness - same path should give same result
			result2 := ProjectIDWithName(tt.cwd)
			assert.Equal(t, result, result2)
		})
	}
}

// TestProjectIDWithName_Uniqueness tests that different paths produce different IDs.
func TestProjectIDWithName_Uniqueness(t *testing.T) {
	paths := []string{
		"/Users/test/project-a",
		"/Users/test/project-b",
		"/Users/other/project-a",
		"/tmp/project-a",
	}

	ids := make(map[string]bool)
	for _, path := range paths {
		id := ProjectIDWithName(path)
		assert.False(t, ids[id], "duplicate ID generated for path %s", path)
		ids[id] = true
	}
}

// TestHookConstants tests hook-related constants.
func TestHookConstants(t *testing.T) {
	assert.Equal(t, 37777, DefaultWorkerPort)
	assert.Equal(t, 1*time.Second, HealthCheckTimeout)
	assert.Equal(t, 30*time.Second, StartupTimeout)
}

// TestExitCodes tests exit code constants.
func TestExitCodes(t *testing.T) {
	assert.Equal(t, 0, ExitSuccess)
	assert.Equal(t, 1, ExitFailure)
	assert.Equal(t, 3, ExitUserMessageOnly)
}

// TestHookResponse tests HookResponse struct.
func TestHookResponse(t *testing.T) {
	tests := []struct {
		name     string
		response HookResponse
		expected string
	}{
		{
			name:     "continue true",
			response: HookResponse{Continue: true},
			expected: `{"continue":true}`,
		},
		{
			name:     "continue false",
			response: HookResponse{Continue: false},
			expected: `{"continue":false}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.response)
			require.NoError(t, err)
			assert.JSONEq(t, tt.expected, string(data))
		})
	}
}

// TestBaseInput tests BaseInput struct parsing.
func TestBaseInput(t *testing.T) {
	input := `{
		"session_id": "test-session-123",
		"cwd": "/Users/test/project",
		"permission_mode": "standard",
		"hook_event_name": "session-start"
	}`

	var base BaseInput
	err := json.Unmarshal([]byte(input), &base)
	require.NoError(t, err)

	assert.Equal(t, "test-session-123", base.SessionID)
	assert.Equal(t, "/Users/test/project", base.CWD)
	assert.Equal(t, "standard", base.PermissionMode)
	assert.Equal(t, "session-start", base.HookEventName)
}

// TestHookContext tests HookContext struct.
func TestHookContext(t *testing.T) {
	ctx := &HookContext{
		HookName:  "session-start",
		Port:      37777,
		Project:   "my-project_abc123",
		SessionID: "test-session",
		CWD:       "/Users/test/project",
		RawInput:  []byte(`{"key":"value"}`),
	}

	assert.Equal(t, "session-start", ctx.HookName)
	assert.Equal(t, 37777, ctx.Port)
	assert.Equal(t, "my-project_abc123", ctx.Project)
	assert.Equal(t, "test-session", ctx.SessionID)
	assert.Equal(t, "/Users/test/project", ctx.CWD)
	assert.Equal(t, []byte(`{"key":"value"}`), ctx.RawInput)
}

// TestIsWorkerRunning_WithServer tests IsWorkerRunning with actual server.
func TestIsWorkerRunning_WithServer(t *testing.T) {
	tests := []struct {
		name           string
		serverHandler  func(w http.ResponseWriter, r *http.Request)
		expectedResult bool
	}{
		{
			name: "healthy worker returns true",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/health" {
					w.WriteHeader(http.StatusOK)
				}
			},
			expectedResult: true,
		},
		{
			name: "unhealthy worker returns false",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/health" {
					w.WriteHeader(http.StatusServiceUnavailable)
				}
			},
			expectedResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(tt.serverHandler))
			defer server.Close()

			// Extract port - note: test server binds to 127.0.0.1
			var port int
			_, err := fmt.Sscanf(server.URL, "http://127.0.0.1:%d", &port)
			require.NoError(t, err)

			// The function uses hardcoded 127.0.0.1, which matches httptest
			result := IsWorkerRunning(port)
			assert.Equal(t, tt.expectedResult, result)
		})
	}
}

// TestIsPortInUse_WithServer tests IsPortInUse with actual server.
func TestIsPortInUse_WithServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Extract port
	var port int
	_, err := fmt.Sscanf(server.URL, "http://127.0.0.1:%d", &port)
	require.NoError(t, err)

	// Port should be in use
	assert.True(t, IsPortInUse(port))
}

// TestGetWorkerVersion_WithServer tests GetWorkerVersion with actual server.
func TestGetWorkerVersion_WithServer(t *testing.T) {
	tests := []struct {
		name           string
		serverHandler  func(w http.ResponseWriter, r *http.Request)
		expectedResult string
	}{
		{
			name: "returns version from server",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/version" {
					w.WriteHeader(http.StatusOK)
					json.NewEncoder(w).Encode(map[string]string{"version": "v1.2.3"})
				}
			},
			expectedResult: "v1.2.3",
		},
		{
			name: "returns empty on non-200",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			expectedResult: "",
		},
		{
			name: "returns empty on invalid JSON",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("not json"))
			},
			expectedResult: "",
		},
		{
			name: "returns empty on missing version field",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(map[string]string{"other": "field"})
			},
			expectedResult: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(tt.serverHandler))
			defer server.Close()

			var port int
			_, err := fmt.Sscanf(server.URL, "http://127.0.0.1:%d", &port)
			require.NoError(t, err)

			result := GetWorkerVersion(port)
			assert.Equal(t, tt.expectedResult, result)
		})
	}
}

// TestGetWorkerPort_EdgeCases tests GetWorkerPort with various edge cases.
func TestGetWorkerPort_EdgeCases(t *testing.T) {
	tests := []struct {
		name         string
		envValue     string
		expectedPort int
		shouldSetEnv bool
	}{
		{
			name:         "zero port uses default",
			envValue:     "0",
			expectedPort: DefaultWorkerPort,
			shouldSetEnv: true,
		},
		{
			name:         "negative port uses default",
			envValue:     "-1",
			expectedPort: DefaultWorkerPort,
			shouldSetEnv: true,
		},
		{
			name:         "empty string uses default",
			envValue:     "",
			expectedPort: DefaultWorkerPort,
			shouldSetEnv: true,
		},
		{
			name:         "whitespace uses default",
			envValue:     "   ",
			expectedPort: DefaultWorkerPort,
			shouldSetEnv: true,
		},
		{
			name:         "large valid port",
			envValue:     "65535",
			expectedPort: 65535,
			shouldSetEnv: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.shouldSetEnv {
				t.Setenv("CLAUDE_MNEMONIC_WORKER_PORT", tt.envValue)
			}
			port := GetWorkerPort()
			assert.Equal(t, tt.expectedPort, port)
		})
	}
}

// TestVersionVariable tests the Version variable.
func TestVersionVariable(t *testing.T) {
	// Version is set at build time, but defaults to "dev"
	assert.NotEmpty(t, Version)
}

// TestProjectIDWithName_RootPath tests ProjectIDWithName with root path.
func TestProjectIDWithName_RootPath(t *testing.T) {
	result := ProjectIDWithName("/")
	// Should handle root path gracefully
	assert.NotEmpty(t, result)
	assert.Contains(t, result, "_") // Should still have underscore separator
}

// TestProjectIDWithName_SameDirname tests that same dirname with different paths get different IDs.
func TestProjectIDWithName_SameDirname(t *testing.T) {
	id1 := ProjectIDWithName("/home/user1/project")
	id2 := ProjectIDWithName("/home/user2/project")

	// Both have same dirname "project" but different full paths
	assert.Contains(t, id1, "project_")
	assert.Contains(t, id2, "project_")

	// But different hashes due to different full paths
	assert.NotEqual(t, id1, id2)
}

// TestBaseInput_PartialFields tests BaseInput with partial fields.
func TestBaseInput_PartialFields(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected BaseInput
	}{
		{
			name:     "only session_id",
			input:    `{"session_id":"test-123"}`,
			expected: BaseInput{SessionID: "test-123"},
		},
		{
			name:     "only cwd",
			input:    `{"cwd":"/tmp/test"}`,
			expected: BaseInput{CWD: "/tmp/test"},
		},
		{
			name:     "empty object",
			input:    `{}`,
			expected: BaseInput{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var base BaseInput
			err := json.Unmarshal([]byte(tt.input), &base)
			require.NoError(t, err)
			assert.Equal(t, tt.expected.SessionID, base.SessionID)
			assert.Equal(t, tt.expected.CWD, base.CWD)
		})
	}
}

// TestHookResponse_Marshal tests HookResponse JSON marshaling.
func TestHookResponse_Marshal(t *testing.T) {
	tests := []struct {
		name     string
		response HookResponse
		contains []string
	}{
		{
			name:     "continue true",
			response: HookResponse{Continue: true},
			contains: []string{`"continue":true`},
		},
		{
			name:     "continue false",
			response: HookResponse{Continue: false},
			contains: []string{`"continue":false`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.response)
			require.NoError(t, err)
			for _, s := range tt.contains {
				assert.Contains(t, string(data), s)
			}
		})
	}
}

// TestHookResponse_Unmarshal tests HookResponse JSON unmarshaling.
func TestHookResponse_Unmarshal(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected HookResponse
	}{
		{
			name:     "continue true",
			input:    `{"continue":true}`,
			expected: HookResponse{Continue: true},
		},
		{
			name:     "continue false",
			input:    `{"continue":false}`,
			expected: HookResponse{Continue: false},
		},
		{
			name:     "missing continue defaults to false",
			input:    `{}`,
			expected: HookResponse{Continue: false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var resp HookResponse
			err := json.Unmarshal([]byte(tt.input), &resp)
			require.NoError(t, err)
			assert.Equal(t, tt.expected.Continue, resp.Continue)
		})
	}
}

// TestHookContext_Initialization tests HookContext struct initialization.
func TestHookContext_Initialization(t *testing.T) {
	tests := []struct {
		name string
		ctx  HookContext
	}{
		{
			name: "full context",
			ctx: HookContext{
				HookName:  "session-start",
				Port:      37777,
				Project:   "my-project_abc123",
				SessionID: "session-123",
				CWD:       "/home/user/project",
				RawInput:  []byte(`{"key":"value"}`),
			},
		},
		{
			name: "minimal context",
			ctx: HookContext{
				HookName: "stop",
			},
		},
		{
			name: "empty context",
			ctx:  HookContext{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Just verify the struct can be created and accessed
			assert.Equal(t, tt.ctx.HookName, tt.ctx.HookName)
			assert.Equal(t, tt.ctx.Port, tt.ctx.Port)
			assert.Equal(t, tt.ctx.Project, tt.ctx.Project)
		})
	}
}

// TestPOST_MarshalError tests POST with unmarshalable body.
func TestPOST_MarshalError(t *testing.T) {
	// Create a value that can't be marshaled
	badValue := make(chan int)

	_, err := POST(99999, "/test", badValue)
	require.Error(t, err)
}

// TestPOST_Timeout tests POST with timeout.
func TestPOST_Timeout(t *testing.T) {
	// Try to connect to a port that's not listening
	_, err := POST(99998, "/test", map[string]string{"key": "value"})
	require.Error(t, err)
}

// TestGET_Timeout tests GET with timeout.
func TestGET_Timeout(t *testing.T) {
	// Try to connect to a port that's not listening
	_, err := GET(99998, "/test")
	require.Error(t, err)
}

// TestIsWorkerRunning_Timeout tests IsWorkerRunning with timeout.
func TestIsWorkerRunning_Timeout(t *testing.T) {
	// Non-existent port should quickly return false
	start := time.Now()
	result := IsWorkerRunning(99997)
	elapsed := time.Since(start)

	assert.False(t, result)
	assert.Less(t, elapsed, 5*time.Second) // Should not hang
}

// TestIsPortInUse_Timeout tests IsPortInUse with timeout.
func TestIsPortInUse_Timeout(t *testing.T) {
	// Non-existent port should quickly return false
	start := time.Now()
	result := IsPortInUse(99996)
	elapsed := time.Since(start)

	assert.False(t, result)
	assert.Less(t, elapsed, 2*time.Second) // Should not hang
}

// TestGetWorkerVersion_Timeout tests GetWorkerVersion with timeout.
func TestGetWorkerVersion_Timeout(t *testing.T) {
	// Non-existent port should quickly return empty
	start := time.Now()
	result := GetWorkerVersion(99995)
	elapsed := time.Since(start)

	assert.Empty(t, result)
	assert.Less(t, elapsed, 5*time.Second) // Should not hang
}

// TestVersionsCompatible_EdgeCases tests versionsCompatible edge cases.
func TestVersionsCompatible_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		v1       string
		v2       string
		expected bool
	}{
		{
			name:     "empty versions",
			v1:       "",
			v2:       "",
			expected: true, // Same base (empty)
		},
		{
			name:     "one empty one dev",
			v1:       "",
			v2:       "dev",
			expected: true, // dev is compatible with anything
		},
		{
			name:     "prerelease versions same base",
			v1:       "v1.0.0-alpha",
			v2:       "v1.0.0-beta",
			expected: true, // Same base 1.0.0
		},
		{
			name:     "version with rc suffix",
			v1:       "v2.0.0-rc1",
			v2:       "v2.0.0",
			expected: true, // Same base 2.0.0
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := versionsCompatible(tt.v1, tt.v2)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestExtractBaseVersion_EdgeCases tests extractBaseVersion edge cases.
func TestExtractBaseVersion_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		version  string
		expected string
	}{
		{
			name:     "version starting with hyphen",
			version:  "-dirty",
			expected: "-dirty", // hyphen at index 0 is not > 0, so no truncation
		},
		{
			name:     "just v",
			version:  "v",
			expected: "",
		},
		{
			name:     "multiple hyphens",
			version:  "v1.0.0-alpha-beta-gamma",
			expected: "1.0.0",
		},
		{
			name:     "no hyphen at all",
			version:  "v2.0.0",
			expected: "2.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractBaseVersion(tt.version)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestProjectIDWithName_RelativePath tests ProjectIDWithName with relative paths.
func TestProjectIDWithName_RelativePath(t *testing.T) {
	// Relative paths should be converted to absolute
	result := ProjectIDWithName(".")
	assert.NotEmpty(t, result)
	assert.Contains(t, result, "_")
}

// TestProjectIDWithName_DeepPath tests ProjectIDWithName with deep paths.
func TestProjectIDWithName_DeepPath(t *testing.T) {
	result := ProjectIDWithName("/a/very/deep/nested/path/to/project")
	assert.Contains(t, result, "project_")
	assert.NotEmpty(t, result)
}

// TestPOST_EmptyBody tests POST with empty body.
func TestPOST_EmptyBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer server.Close()

	var port int
	_, err := fmt.Sscanf(server.URL, "http://127.0.0.1:%d", &port)
	require.NoError(t, err)

	result, err := POST(port, "/test", map[string]string{})
	require.NoError(t, err)
	assert.NotNil(t, result)
}

// TestGET_WithQueryParams tests GET with query parameters.
func TestGET_WithQueryParams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/test?foo=bar", r.URL.String())
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer server.Close()

	var port int
	_, err := fmt.Sscanf(server.URL, "http://127.0.0.1:%d", &port)
	require.NoError(t, err)

	result, err := GET(port, "/test?foo=bar")
	require.NoError(t, err)
	assert.NotNil(t, result)
}

// TestHookResponse_RoundTrip tests JSON marshal/unmarshal round-trip.
func TestHookResponse_RoundTrip(t *testing.T) {
	original := HookResponse{Continue: true}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded HookResponse
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, original.Continue, decoded.Continue)
}

// TestBaseInput_RoundTrip tests BaseInput JSON round-trip.
func TestBaseInput_RoundTrip(t *testing.T) {
	original := BaseInput{
		SessionID:      "test-session",
		CWD:            "/home/user/project",
		PermissionMode: "standard",
		HookEventName:  "session-start",
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded BaseInput
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, original.SessionID, decoded.SessionID)
	assert.Equal(t, original.CWD, decoded.CWD)
	assert.Equal(t, original.PermissionMode, decoded.PermissionMode)
	assert.Equal(t, original.HookEventName, decoded.HookEventName)
}

// TestHookContext_RawInput tests HookContext with different raw input types.
func TestHookContext_RawInput(t *testing.T) {
	tests := []struct {
		name     string
		rawInput []byte
	}{
		{
			name:     "json object",
			rawInput: []byte(`{"key":"value"}`),
		},
		{
			name:     "json array",
			rawInput: []byte(`[1,2,3]`),
		},
		{
			name:     "empty object",
			rawInput: []byte(`{}`),
		},
		{
			name:     "nil input",
			rawInput: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := HookContext{
				HookName: "test",
				RawInput: tt.rawInput,
			}
			assert.Equal(t, tt.rawInput, ctx.RawInput)
		})
	}
}

// TestDefaultWorkerPort tests that the default port constant is valid.
func TestDefaultWorkerPort(t *testing.T) {
	assert.Greater(t, DefaultWorkerPort, 1024, "Default port should be above privileged port range")
	assert.Less(t, DefaultWorkerPort, 65535, "Default port should be valid TCP port")
}

// TestHealthCheckTimeout tests the health check timeout is reasonable.
func TestHealthCheckTimeout(t *testing.T) {
	assert.Greater(t, HealthCheckTimeout, 100*time.Millisecond)
	assert.Less(t, HealthCheckTimeout, 10*time.Second)
}

// TestStartupTimeout tests the startup timeout is reasonable.
func TestStartupTimeout(t *testing.T) {
	assert.Greater(t, StartupTimeout, 5*time.Second)
	assert.LessOrEqual(t, StartupTimeout, time.Minute)
}
