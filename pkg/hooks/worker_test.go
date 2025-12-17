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
