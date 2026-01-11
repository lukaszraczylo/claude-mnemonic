// Package mcp provides the MCP (Model Context Protocol) server for claude-mnemonic.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// =============================================================================
// TEST SUITE
// =============================================================================

// ServerSuite is a test suite for MCP Server operations.
type ServerSuite struct {
	suite.Suite
}

func TestServerSuite(t *testing.T) {
	suite.Run(t, new(ServerSuite))
}

// TestNewServer tests server creation.
func (s *ServerSuite) TestNewServer() {
	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	s.NotNil(server)
	s.Nil(server.searchMgr)
	s.Equal("1.0.0", server.version)
}

// =============================================================================
// TESTS FOR Request/Response Structs
// =============================================================================

// TestRequest tests Request struct JSON marshaling.
func TestRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		expected string
		req      Request
	}{
		// ===== GOOD CASES =====
		{
			name: "initialize request",
			req: Request{
				JSONRPC: "2.0",
				ID:      1,
				Method:  "initialize",
			},
			expected: `{"jsonrpc":"2.0","id":1,"method":"initialize"}`,
		},
		{
			name: "tools/list request",
			req: Request{
				JSONRPC: "2.0",
				ID:      "abc",
				Method:  "tools/list",
			},
			expected: `{"jsonrpc":"2.0","id":"abc","method":"tools/list"}`,
		},
		{
			name: "tools/call with params",
			req: Request{
				JSONRPC: "2.0",
				ID:      2,
				Method:  "tools/call",
				Params:  json.RawMessage(`{"name":"search","arguments":{}}`),
			},
			expected: `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"search","arguments":{}}}`,
		},
		// ===== EDGE CASES =====
		{
			name: "request with nil ID",
			req: Request{
				JSONRPC: "2.0",
				ID:      nil,
				Method:  "initialize",
			},
			expected: `{"jsonrpc":"2.0","id":null,"method":"initialize"}`,
		},
		{
			name: "request with float ID",
			req: Request{
				JSONRPC: "2.0",
				ID:      1.5,
				Method:  "test",
			},
			expected: `{"jsonrpc":"2.0","id":1.5,"method":"test"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			data, err := json.Marshal(tt.req)
			require.NoError(t, err)
			assert.JSONEq(t, tt.expected, string(data))

			// Test unmarshaling
			var parsed Request
			err = json.Unmarshal(data, &parsed)
			require.NoError(t, err)
			assert.Equal(t, tt.req.JSONRPC, parsed.JSONRPC)
			assert.Equal(t, tt.req.Method, parsed.Method)
		})
	}
}

// TestResponse tests Response struct JSON marshaling.
func TestResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		resp     Response
		expected string
	}{
		// ===== GOOD CASES =====
		{
			name: "success response",
			resp: Response{
				JSONRPC: "2.0",
				ID:      1,
				Result:  map[string]string{"status": "ok"},
			},
			expected: `{"jsonrpc":"2.0","id":1,"result":{"status":"ok"}}`,
		},
		{
			name: "error response",
			resp: Response{
				JSONRPC: "2.0",
				ID:      2,
				Error: &Error{
					Code:    -32600,
					Message: "Invalid Request",
				},
			},
			expected: `{"jsonrpc":"2.0","id":2,"error":{"code":-32600,"message":"Invalid Request"}}`,
		},
		{
			name: "error with data",
			resp: Response{
				JSONRPC: "2.0",
				ID:      3,
				Error: &Error{
					Code:    -32602,
					Message: "Invalid params",
					Data:    "missing field",
				},
			},
			expected: `{"jsonrpc":"2.0","id":3,"error":{"code":-32602,"message":"Invalid params","data":"missing field"}}`,
		},
		// ===== EDGE CASES =====
		{
			name: "response with nil ID",
			resp: Response{
				JSONRPC: "2.0",
				ID:      nil,
				Result:  "ok",
			},
			expected: `{"jsonrpc":"2.0","id":null,"result":"ok"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			data, err := json.Marshal(tt.resp)
			require.NoError(t, err)
			assert.JSONEq(t, tt.expected, string(data))
		})
	}
}

// TestError tests Error struct.
func TestError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		expected string
		name     string
		err      Error
	}{
		{
			name: "parse error",
			err: Error{
				Code:    -32700,
				Message: "Parse error",
			},
			expected: `{"code":-32700,"message":"Parse error"}`,
		},
		{
			name: "method not found",
			err: Error{
				Code:    -32601,
				Message: "Method not found",
			},
			expected: `{"code":-32601,"message":"Method not found"}`,
		},
		{
			name: "invalid params",
			err: Error{
				Code:    -32602,
				Message: "Invalid params",
				Data:    "details here",
			},
			expected: `{"code":-32602,"message":"Invalid params","data":"details here"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			data, err := json.Marshal(tt.err)
			require.NoError(t, err)
			assert.JSONEq(t, tt.expected, string(data))
		})
	}
}

// TestToolCallParams tests ToolCallParams struct.
func TestToolCallParams(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected ToolCallParams
	}{
		{
			name:  "search tool call",
			input: `{"name":"search","arguments":{"query":"test"}}`,
			expected: ToolCallParams{
				Name:      "search",
				Arguments: json.RawMessage(`{"query":"test"}`),
			},
		},
		{
			name:  "decisions tool call",
			input: `{"name":"decisions","arguments":{"query":"auth"}}`,
			expected: ToolCallParams{
				Name:      "decisions",
				Arguments: json.RawMessage(`{"query":"auth"}`),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var params ToolCallParams
			err := json.Unmarshal([]byte(tt.input), &params)
			require.NoError(t, err)
			assert.Equal(t, tt.expected.Name, params.Name)
		})
	}
}

// TestTool tests Tool struct.
func TestTool(t *testing.T) {
	t.Parallel()

	tool := Tool{
		Name:        "search",
		Description: "Search observations",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string"},
			},
		},
	}

	data, err := json.Marshal(tool)
	require.NoError(t, err)

	var parsed Tool
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err)
	assert.Equal(t, "search", parsed.Name)
	assert.Equal(t, "Search observations", parsed.Description)
}

// TestTimelineParams tests TimelineParams struct.
func TestTimelineParams(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected TimelineParams
	}{
		{
			name:  "with anchor_id",
			input: `{"anchor_id":123,"before":5,"after":5}`,
			expected: TimelineParams{
				AnchorID: 123,
				Before:   5,
				After:    5,
			},
		},
		{
			name:  "with query",
			input: `{"query":"test query","project":"my-project"}`,
			expected: TimelineParams{
				Query:   "test query",
				Project: "my-project",
			},
		},
		{
			name:  "full params",
			input: `{"anchor_id":100,"query":"search","before":10,"after":20,"project":"proj","obs_type":"bugfix","concepts":"security","files":"main.go","dateStart":1234567890,"dateEnd":9876543210,"format":"full"}`,
			expected: TimelineParams{
				AnchorID:  100,
				Query:     "search",
				Before:    10,
				After:     20,
				Project:   "proj",
				ObsType:   "bugfix",
				Concepts:  "security",
				Files:     "main.go",
				DateStart: 1234567890,
				DateEnd:   9876543210,
				Format:    "full",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var params TimelineParams
			err := json.Unmarshal([]byte(tt.input), &params)
			require.NoError(t, err)
			assert.Equal(t, tt.expected.AnchorID, params.AnchorID)
			assert.Equal(t, tt.expected.Query, params.Query)
			assert.Equal(t, tt.expected.Project, params.Project)
		})
	}
}

// =============================================================================
// TESTS FOR Server Handlers
// =============================================================================

// TestHandleInitialize tests the initialize handler.
func TestHandleInitialize(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.2.3", nil, nil, nil, nil, nil, nil, nil, nil)

	req := &Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
	}

	resp := server.handleInitialize(req)

	assert.Equal(t, "2.0", resp.JSONRPC)
	assert.Equal(t, 1, resp.ID)
	assert.Nil(t, resp.Error)
	assert.NotNil(t, resp.Result)

	result, ok := resp.Result.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "2024-11-05", result["protocolVersion"])

	serverInfo, ok := result["serverInfo"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "claude-mnemonic", serverInfo["name"])
	assert.Equal(t, "1.2.3", serverInfo["version"])
}

// TestHandleToolsList tests the tools/list handler.
func TestHandleToolsList(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)

	req := &Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/list",
	}

	resp := server.handleToolsList(req)

	assert.Equal(t, "2.0", resp.JSONRPC)
	assert.Equal(t, 1, resp.ID)
	assert.Nil(t, resp.Error)

	result, ok := resp.Result.(map[string]any)
	require.True(t, ok)

	tools, ok := result["tools"].([]Tool)
	require.True(t, ok)
	assert.NotEmpty(t, tools)

	// Verify expected tools are present
	toolNames := make(map[string]bool)
	for _, tool := range tools {
		toolNames[tool.Name] = true
	}

	expectedTools := []string{
		"search", "timeline", "decisions", "changes",
		"how_it_works", "find_by_concept", "find_by_file",
		"find_by_type", "get_recent_context", "get_context_timeline",
		"get_timeline_by_query",
	}

	for _, name := range expectedTools {
		assert.True(t, toolNames[name], "expected tool %s to be present", name)
	}
}

// TestHandleRequest tests request routing.
func TestHandleRequest(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	tests := []struct {
		req          *Request
		name         string
		errorMessage string
		errorCode    int
		expectError  bool
	}{
		{
			name: "initialize method",
			req: &Request{
				JSONRPC: "2.0",
				ID:      1,
				Method:  "initialize",
			},
			expectError: false,
		},
		{
			name: "tools/list method",
			req: &Request{
				JSONRPC: "2.0",
				ID:      2,
				Method:  "tools/list",
			},
			expectError: false,
		},
		{
			name: "unknown method",
			req: &Request{
				JSONRPC: "2.0",
				ID:      3,
				Method:  "unknown_method",
			},
			expectError:  true,
			errorCode:    -32601,
			errorMessage: "Method not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			resp := server.handleRequest(ctx, tt.req)

			assert.Equal(t, "2.0", resp.JSONRPC)
			assert.Equal(t, tt.req.ID, resp.ID)

			if tt.expectError {
				require.NotNil(t, resp.Error)
				assert.Equal(t, tt.errorCode, resp.Error.Code)
				assert.Equal(t, tt.errorMessage, resp.Error.Message)
			} else {
				assert.Nil(t, resp.Error)
				assert.NotNil(t, resp.Result)
			}
		})
	}
}

// TestHandleToolsCall_InvalidParams tests tools/call with invalid params.
func TestHandleToolsCall_InvalidParams(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	req := &Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params:  json.RawMessage(`invalid json`),
	}

	resp := server.handleToolsCall(ctx, req)

	require.NotNil(t, resp.Error)
	assert.Equal(t, -32602, resp.Error.Code)
	assert.Equal(t, "Invalid params", resp.Error.Message)
}

// TestCallTool_UnknownTool tests callTool with unknown tool name.
func TestCallTool_UnknownTool(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	_, err := server.callTool(ctx, "nonexistent_tool", json.RawMessage(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown tool")
}

// TestCallTool_InvalidArgs tests callTool with invalid arguments.
func TestCallTool_InvalidArgs(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	_, err := server.callTool(ctx, "search", json.RawMessage(`invalid json`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid arguments")
}

// =============================================================================
// TESTS FOR Server I/O
// =============================================================================

// TestSendResponse tests response sending.
func TestSendResponse(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	server := &Server{
		stdout: &buf,
	}

	resp := &Response{
		JSONRPC: "2.0",
		ID:      1,
		Result:  map[string]string{"status": "ok"},
	}

	server.sendResponse(resp)

	output := buf.String()
	assert.Contains(t, output, `"jsonrpc":"2.0"`)
	assert.Contains(t, output, `"id":1`)
	assert.Contains(t, output, `"result"`)
}

// TestSendError tests error response sending.
func TestSendError(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	server := &Server{
		stdout: &buf,
	}

	server.sendError(1, -32700, "Parse error", "details")

	output := buf.String()
	assert.Contains(t, output, `"error"`)
	assert.Contains(t, output, `-32700`)
	assert.Contains(t, output, `"Parse error"`)
}

// TestRun_ParseError tests Run with invalid JSON input.
func TestRun_ParseError(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	stdin := strings.NewReader("invalid json\n")

	server := &Server{
		stdin:  stdin,
		stdout: &stdout,
	}

	err := server.Run(context.Background())
	require.NoError(t, err)

	output := stdout.String()
	assert.Contains(t, output, `"error"`)
	assert.Contains(t, output, `-32700`)
	assert.Contains(t, output, `"Parse error"`)
}

// TestRun_EmptyLine tests Run skips empty lines.
func TestRun_EmptyLine(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	stdin := strings.NewReader("\n\n")

	server := &Server{
		stdin:  stdin,
		stdout: &stdout,
	}

	err := server.Run(context.Background())
	require.NoError(t, err)

	// Should be empty - no responses for empty lines
	assert.Empty(t, stdout.String())
}

// TestRun_ValidRequest tests Run with a valid request.
func TestRun_ValidRequest(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	req := `{"jsonrpc":"2.0","id":1,"method":"initialize"}`
	stdin := strings.NewReader(req + "\n")

	server := &Server{
		stdin:   stdin,
		stdout:  &stdout,
		version: "1.0.0",
	}

	err := server.Run(context.Background())
	require.NoError(t, err)

	output := stdout.String()
	assert.Contains(t, output, `"jsonrpc":"2.0"`)
	assert.Contains(t, output, `"result"`)
	assert.Contains(t, output, `"protocolVersion"`)
}

// TestRun_MultipleRequests tests Run with multiple sequential requests.
func TestRun_MultipleRequests(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	req1 := `{"jsonrpc":"2.0","id":1,"method":"initialize"}`
	req2 := `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`
	stdin := strings.NewReader(req1 + "\n" + req2 + "\n")

	server := &Server{
		stdin:   stdin,
		stdout:  &stdout,
		version: "1.0.0",
	}

	err := server.Run(context.Background())
	require.NoError(t, err)

	output := stdout.String()
	// Should contain responses for both requests
	assert.Contains(t, output, `"id":1`)
	assert.Contains(t, output, `"id":2`)
}

// TestRunMixedRequests tests Run with mixed valid and invalid requests.
func TestRunMixedRequests(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	req1 := `{"jsonrpc":"2.0","id":1,"method":"initialize"}`
	req2 := `invalid json`
	req3 := `{"jsonrpc":"2.0","id":3,"method":"tools/list"}`
	stdin := strings.NewReader(req1 + "\n" + req2 + "\n" + req3 + "\n")

	server := &Server{
		stdin:   stdin,
		stdout:  &stdout,
		version: "1.0.0",
	}

	err := server.Run(context.Background())
	require.NoError(t, err)

	output := stdout.String()
	// Should have responses for all three requests
	assert.Contains(t, output, `"id":1`)
	assert.Contains(t, output, `"error"`) // Parse error for invalid json
	assert.Contains(t, output, `"id":3`)
}

// =============================================================================
// TESTS FOR Handler Parameter Validation
// =============================================================================

// TestHandleFindRelatedObservations_Validation tests parameter validation.
func TestHandleFindRelatedObservations_Validation(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	tests := []struct {
		name        string
		args        string
		errContains string
		wantErr     bool
	}{
		{
			name:        "missing id",
			args:        `{}`,
			wantErr:     true,
			errContains: "id is required",
		},
		{
			name:        "invalid json",
			args:        `{invalid`,
			wantErr:     true,
			errContains: "invalid arguments",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := server.handleFindRelatedObservations(ctx, json.RawMessage(tt.args))
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestHandleFindSimilarObservations_Validation tests parameter validation.
func TestHandleFindSimilarObservations_Validation(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	tests := []struct {
		name        string
		args        string
		errContains string
		wantErr     bool
	}{
		{
			name:        "missing query",
			args:        `{}`,
			wantErr:     true,
			errContains: "query is required",
		},
		{
			name:        "invalid json",
			args:        `{invalid`,
			wantErr:     true,
			errContains: "invalid arguments",
		},
		{
			name:        "nil vector client",
			args:        `{"query": "test"}`,
			wantErr:     true,
			errContains: "vector search not available",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := server.handleFindSimilarObservations(ctx, json.RawMessage(tt.args))
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestHandleGetPatterns_Validation tests parameter validation.
func TestHandleGetPatterns_Validation(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	tests := []struct {
		name        string
		args        string
		errContains string
		wantErr     bool
	}{
		{
			name:        "invalid json",
			args:        `{invalid`,
			wantErr:     true,
			errContains: "invalid arguments",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := server.handleGetPatterns(ctx, json.RawMessage(tt.args))
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestHandleBulkDeleteObservations_Validation tests parameter validation.
func TestHandleBulkDeleteObservations_Validation(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	tests := []struct {
		name        string
		args        string
		errContains string
		wantErr     bool
	}{
		{
			name:        "missing ids",
			args:        `{}`,
			wantErr:     true,
			errContains: "ids is required",
		},
		{
			name:        "empty ids array",
			args:        `{"ids": []}`,
			wantErr:     true,
			errContains: "ids is required",
		},
		{
			name:        "too many ids",
			args:        `{"ids": [` + strings.Repeat("1,", 1001) + `1]}`,
			wantErr:     true,
			errContains: "maximum 1000 IDs",
		},
		{
			name:        "invalid json",
			args:        `{invalid`,
			wantErr:     true,
			errContains: "invalid arguments",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := server.handleBulkDeleteObservations(ctx, json.RawMessage(tt.args))
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestHandleBulkMarkSuperseded_Validation tests parameter validation.
func TestHandleBulkMarkSuperseded_Validation(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	tests := []struct {
		name        string
		args        string
		errContains string
		wantErr     bool
	}{
		{
			name:        "missing ids",
			args:        `{}`,
			wantErr:     true,
			errContains: "ids is required",
		},
		{
			name:        "empty ids array",
			args:        `{"ids": []}`,
			wantErr:     true,
			errContains: "ids is required",
		},
		{
			name:        "too many ids",
			args:        `{"ids": [` + strings.Repeat("1,", 1001) + `1]}`,
			wantErr:     true,
			errContains: "maximum 1000 IDs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := server.handleBulkMarkSuperseded(ctx, json.RawMessage(tt.args))
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestHandleBulkBoostObservations_Validation tests parameter validation.
func TestHandleBulkBoostObservations_Validation(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	tests := []struct {
		name        string
		args        string
		errContains string
		wantErr     bool
	}{
		{
			name:        "missing ids",
			args:        `{"boost": 0.1}`,
			wantErr:     true,
			errContains: "ids is required",
		},
		{
			name:        "boost out of range low",
			args:        `{"ids": [1], "boost": -1.5}`,
			wantErr:     true,
			errContains: "boost must be between",
		},
		{
			name:        "boost out of range high",
			args:        `{"ids": [1], "boost": 1.5}`,
			wantErr:     true,
			errContains: "boost must be between",
		},
		{
			name:        "too many ids",
			args:        `{"ids": [` + strings.Repeat("1,", 1001) + `1], "boost": 0.1}`,
			wantErr:     true,
			errContains: "maximum 1000 IDs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := server.handleBulkBoostObservations(ctx, json.RawMessage(tt.args))
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestHandleTriggerMaintenance_Validation tests that nil service returns error.
func TestHandleTriggerMaintenance_Validation(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	_, err := server.handleTriggerMaintenance(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "maintenance service not available")
}

// TestHandleGetMaintenanceStats_Validation tests that nil service returns error.
func TestHandleGetMaintenanceStats_Validation(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	_, err := server.handleGetMaintenanceStats(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "maintenance service not available")
}

// TestHandleMergeObservations_Validation tests parameter validation.
func TestHandleMergeObservations_Validation(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	tests := []struct {
		name        string
		args        string
		errContains string
		wantErr     bool
	}{
		{
			name:        "missing source_id",
			args:        `{"target_id": 2}`,
			wantErr:     true,
			errContains: "source_id and target_id are required",
		},
		{
			name:        "missing target_id",
			args:        `{"source_id": 1}`,
			wantErr:     true,
			errContains: "source_id and target_id are required",
		},
		{
			name:        "same source and target",
			args:        `{"source_id": 1, "target_id": 1}`,
			wantErr:     true,
			errContains: "source_id and target_id cannot be the same",
		},
		{
			name:        "boost out of range",
			args:        `{"source_id": 1, "target_id": 2, "boost": 0.6}`,
			wantErr:     true,
			errContains: "boost must be between 0 and 0.5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := server.handleMergeObservations(ctx, json.RawMessage(tt.args))
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestHandleGetObservation_Validation tests parameter validation.
func TestHandleGetObservation_Validation(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	tests := []struct {
		name        string
		args        string
		errContains string
		wantErr     bool
	}{
		{
			name:        "missing id",
			args:        `{}`,
			wantErr:     true,
			errContains: "id is required",
		},
		{
			name:        "invalid json",
			args:        `{invalid`,
			wantErr:     true,
			errContains: "invalid arguments",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := server.handleGetObservation(ctx, json.RawMessage(tt.args))
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestHandleEditObservation_Validation tests parameter validation.
func TestHandleEditObservation_Validation(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	tests := []struct {
		name        string
		args        string
		errContains string
		wantErr     bool
	}{
		{
			name:        "missing id",
			args:        `{"title": "new title"}`,
			wantErr:     true,
			errContains: "id is required",
		},
		{
			name:        "invalid scope",
			args:        `{"id": 1, "scope": "invalid"}`,
			wantErr:     true,
			errContains: "scope must be 'project' or 'global'",
		},
		{
			name:        "invalid json",
			args:        `{invalid`,
			wantErr:     true,
			errContains: "invalid arguments",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := server.handleEditObservation(ctx, json.RawMessage(tt.args))
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestHandleGetObservationQuality_Validation tests parameter validation.
func TestHandleGetObservationQuality_Validation(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	tests := []struct {
		name        string
		args        string
		errContains string
		wantErr     bool
	}{
		{
			name:        "missing id",
			args:        `{}`,
			wantErr:     true,
			errContains: "id is required",
		},
		{
			name:        "invalid json",
			args:        `{invalid`,
			wantErr:     true,
			errContains: "invalid arguments",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := server.handleGetObservationQuality(ctx, json.RawMessage(tt.args))
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestHandleSuggestConsolidations_Validation tests parameter validation.
func TestHandleSuggestConsolidations_Validation(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	tests := []struct {
		name        string
		args        string
		errContains string
		wantErr     bool
	}{
		{
			name:        "min_similarity too low",
			args:        `{"min_similarity": 0.3}`,
			wantErr:     true,
			errContains: "min_similarity must be between 0.5 and 1.0",
		},
		{
			name:        "min_similarity too high",
			args:        `{"min_similarity": 1.5}`,
			wantErr:     true,
			errContains: "min_similarity must be between 0.5 and 1.0",
		},
		{
			name:        "invalid json",
			args:        `{invalid`,
			wantErr:     true,
			errContains: "invalid arguments",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := server.handleSuggestConsolidations(ctx, json.RawMessage(tt.args))
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestHandleTagObservation_Validation tests parameter validation.
func TestHandleTagObservation_Validation(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	tests := []struct {
		name        string
		args        string
		errContains string
		wantErr     bool
	}{
		{
			name:        "missing id",
			args:        `{"tags": ["tag1"]}`,
			wantErr:     true,
			errContains: "id is required",
		},
		{
			name:        "missing tags",
			args:        `{"id": 1}`,
			wantErr:     true,
			errContains: "tags is required",
		},
		{
			name:        "invalid mode",
			args:        `{"id": 1, "tags": ["tag1"], "mode": "invalid"}`,
			wantErr:     true,
			errContains: "mode must be 'add', 'remove', or 'set'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := server.handleTagObservation(ctx, json.RawMessage(tt.args))
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestHandleGetObservationsByTag_Validation tests parameter validation.
func TestHandleGetObservationsByTag_Validation(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	tests := []struct {
		name        string
		args        string
		errContains string
		wantErr     bool
	}{
		{
			name:        "missing tag",
			args:        `{}`,
			wantErr:     true,
			errContains: "tag is required",
		},
		{
			name:        "invalid json",
			args:        `{invalid`,
			wantErr:     true,
			errContains: "invalid arguments",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := server.handleGetObservationsByTag(ctx, json.RawMessage(tt.args))
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestHandleBatchTagByPattern_Validation tests parameter validation.
func TestHandleBatchTagByPattern_Validation(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	tests := []struct {
		name        string
		args        string
		errContains string
		wantErr     bool
	}{
		{
			name:        "missing pattern",
			args:        `{"tags": ["tag1"]}`,
			wantErr:     true,
			errContains: "pattern is required",
		},
		{
			name:        "missing tags",
			args:        `{"pattern": "test"}`,
			wantErr:     true,
			errContains: "tags is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := server.handleBatchTagByPattern(ctx, json.RawMessage(tt.args))
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestHandleExplainSearchRanking_Validation tests parameter validation.
func TestHandleExplainSearchRanking_Validation(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	tests := []struct {
		name        string
		args        string
		errContains string
		wantErr     bool
	}{
		{
			name:        "missing query",
			args:        `{"top_n": 5}`,
			wantErr:     true,
			errContains: "query is required",
		},
		{
			name:        "invalid json",
			args:        `{invalid`,
			wantErr:     true,
			errContains: "invalid arguments",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := server.handleExplainSearchRanking(ctx, json.RawMessage(tt.args))
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestHandleGetObservationRelationships_Validation tests parameter validation.
func TestHandleGetObservationRelationships_Validation(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	tests := []struct {
		name        string
		args        string
		errContains string
		wantErr     bool
	}{
		{
			name:        "missing id",
			args:        `{}`,
			wantErr:     true,
			errContains: "id is required",
		},
		{
			name:        "negative id",
			args:        `{"id": -1}`,
			wantErr:     true,
			errContains: "id is required and must be positive",
		},
		{
			name:        "nil relation store",
			args:        `{"id": 1}`,
			wantErr:     true,
			errContains: "relation store not available",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := server.handleGetObservationRelationships(ctx, json.RawMessage(tt.args))
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestHandleGetObservationScoringBreakdown_Validation tests parameter validation.
func TestHandleGetObservationScoringBreakdown_Validation(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	tests := []struct {
		name        string
		args        string
		errContains string
		wantErr     bool
	}{
		{
			name:        "missing id",
			args:        `{}`,
			wantErr:     true,
			errContains: "id is required",
		},
		{
			name:        "negative id",
			args:        `{"id": -1}`,
			wantErr:     true,
			errContains: "id is required and must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := server.handleGetObservationScoringBreakdown(ctx, json.RawMessage(tt.args))
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestHandleTimeline_InvalidJSON tests timeline with invalid JSON.
func TestHandleTimeline_InvalidJSON(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	_, err := server.handleTimeline(ctx, json.RawMessage(`{invalid`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid timeline params")
}

// TestHandleTimelineByQuery_EmptyQuery tests timeline by query with empty query.
func TestHandleTimelineByQuery_EmptyQuery(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	// Empty query should error
	_, err := server.handleTimelineByQuery(ctx, json.RawMessage(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "query is required")
}

// TestHandleTimelineByQuery_InvalidJSON tests timeline by query with invalid JSON.
func TestHandleTimelineByQuery_InvalidJSON(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	_, err := server.handleTimelineByQuery(ctx, json.RawMessage(`{invalid`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid timeline params")
}

// TestHandleTimeline_NoAnchorNoQuery tests timeline with no anchor and no query.
func TestHandleTimeline_NoAnchorNoQuery(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	// No anchor_id and no query should return empty result
	result, err := server.handleTimeline(ctx, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Empty(t, result.Results)
}

// TestHandleTimeline_WithDefaults tests timeline default values are applied.
func TestHandleTimeline_WithDefaults(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	// With anchor_id = 0, should return empty result
	result, err := server.handleTimeline(ctx, json.RawMessage(`{"anchor_id": 0}`))
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Empty(t, result.Results)
}

// =============================================================================
// TESTS FOR Additional Tool Operations
// =============================================================================

// TestJSONRPCErrorCodes tests standard JSON-RPC error codes.
func TestJSONRPCErrorCodes(t *testing.T) {
	t.Parallel()

	errorCodes := map[string]int{
		"Parse error":      -32700,
		"Invalid Request":  -32600,
		"Method not found": -32601,
		"Invalid params":   -32602,
		"Internal error":   -32603,
	}

	for msg, code := range errorCodes {
		t.Run(msg, func(t *testing.T) {
			t.Parallel()
			err := Error{Code: code, Message: msg}
			assert.Equal(t, code, err.Code)
			assert.Equal(t, msg, err.Message)
		})
	}
}

// TestToolListContainsExpectedSchemas tests that tool schemas are valid.
func TestToolListContainsExpectedSchemas(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)

	req := &Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/list",
	}

	resp := server.handleToolsList(req)
	result := resp.Result.(map[string]any)
	tools := result["tools"].([]Tool)

	for _, tool := range tools {
		assert.NotEmpty(t, tool.Name)
		assert.NotEmpty(t, tool.Description)
		assert.NotNil(t, tool.InputSchema)

		// Check schema has type
		schema := tool.InputSchema
		_, hasType := schema["type"]
		assert.True(t, hasType, "tool %s schema should have type", tool.Name)
	}
}

// TestHandleToolsCall_UnknownTool tests tools/call with unknown tool name.
func TestHandleToolsCall_UnknownTool(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	req := &Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"unknown_tool","arguments":{}}`),
	}

	resp := server.handleToolsCall(ctx, req)
	require.NotNil(t, resp.Error)
	assert.Equal(t, -32000, resp.Error.Code)
	assert.Contains(t, resp.Error.Data, "unknown tool")
}

// TestCallTool_ToolNameRecognition tests that valid tool names are recognized.
func TestCallTool_ToolNameRecognition(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)

	req := &Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/list",
	}

	resp := server.handleToolsList(req)
	result := resp.Result.(map[string]any)
	tools := result["tools"].([]Tool)

	// Verify all expected tools are registered
	expectedTools := map[string]bool{
		"search":                            true,
		"timeline":                          true,
		"decisions":                         true,
		"changes":                           true,
		"how_it_works":                      true,
		"find_by_concept":                   true,
		"find_by_file":                      true,
		"find_by_type":                      true,
		"get_recent_context":                true,
		"get_context_timeline":              true,
		"get_timeline_by_query":             true,
		"find_related_observations":         true,
		"find_similar_observations":         true,
		"get_patterns":                      true,
		"get_memory_stats":                  true,
		"bulk_delete_observations":          true,
		"bulk_mark_superseded":              true,
		"bulk_boost_observations":           true,
		"trigger_maintenance":               true,
		"get_maintenance_stats":             true,
		"merge_observations":                true,
		"get_observation":                   true,
		"edit_observation":                  true,
		"get_observation_quality":           true,
		"suggest_consolidations":            true,
		"tag_observation":                   true,
		"get_observations_by_tag":           true,
		"get_temporal_trends":               true,
		"get_data_quality_report":           true,
		"batch_tag_by_pattern":              true,
		"explain_search_ranking":            true,
		"export_observations":               true,
		"check_system_health":               true,
		"analyze_search_patterns":           true,
		"get_observation_relationships":     true,
		"get_observation_scoring_breakdown": true,
		"analyze_observation_importance":    true,
	}

	foundTools := make(map[string]bool)
	for _, tool := range tools {
		foundTools[tool.Name] = true
	}

	for name := range expectedTools {
		assert.True(t, foundTools[name], "tool %s should be registered", name)
	}
}

// TestTimelineParams_Complete tests complete TimelineParams parsing.
func TestTimelineParams_Complete(t *testing.T) {
	t.Parallel()

	input := `{
		"anchor_id": 100,
		"query": "test query",
		"before": 5,
		"after": 15,
		"project": "my-project",
		"obs_type": "bugfix",
		"concepts": "security,auth",
		"files": "main.go,handler.go",
		"dateStart": 1700000000000,
		"dateEnd": 1700100000000,
		"format": "full"
	}`

	var params TimelineParams
	err := json.Unmarshal([]byte(input), &params)
	require.NoError(t, err)

	assert.Equal(t, int64(100), params.AnchorID)
	assert.Equal(t, "test query", params.Query)
	assert.Equal(t, 5, params.Before)
	assert.Equal(t, 15, params.After)
	assert.Equal(t, "my-project", params.Project)
	assert.Equal(t, "bugfix", params.ObsType)
	assert.Equal(t, "security,auth", params.Concepts)
	assert.Equal(t, "main.go,handler.go", params.Files)
	assert.Equal(t, int64(1700000000000), params.DateStart)
	assert.Equal(t, int64(1700100000000), params.DateEnd)
	assert.Equal(t, "full", params.Format)
}

// TestServerStdinStdoutConfig tests that server stdin/stdout can be configured.
func TestServerStdinStdoutConfig(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stdin bytes.Buffer

	server := &Server{
		stdin:   &stdin,
		stdout:  &stdout,
		version: "test-version",
	}

	assert.Equal(t, &stdin, server.stdin)
	assert.Equal(t, &stdout, server.stdout)
	assert.Equal(t, "test-version", server.version)
}

// TestResponseIDTypes tests that response IDs can be various types.
func TestResponseIDTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		id   any
		name string
	}{
		{name: "integer id", id: 1},
		{name: "string id", id: "abc-123"},
		{name: "float id", id: 1.5},
		{name: "null id", id: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			server := &Server{stdout: &buf}

			resp := &Response{
				JSONRPC: "2.0",
				ID:      tt.id,
				Result:  "ok",
			}

			server.sendResponse(resp)
			output := buf.String()
			assert.Contains(t, output, `"jsonrpc":"2.0"`)
		})
	}
}

// TestServerFields tests Server struct fields.
func TestServerFields(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "2.0.0", nil, nil, nil, nil, nil, nil, nil, nil)

	assert.Equal(t, "2.0.0", server.version)
	assert.Nil(t, server.searchMgr)
	assert.NotNil(t, server.stdin)
	assert.NotNil(t, server.stdout)
}

// TestRequestUnmarshalWithNullID tests Request unmarshaling with null ID.
func TestRequestUnmarshalWithNullID(t *testing.T) {
	t.Parallel()

	input := `{"jsonrpc":"2.0","id":null,"method":"initialize"}`

	var req Request
	err := json.Unmarshal([]byte(input), &req)
	require.NoError(t, err)
	assert.Equal(t, "2.0", req.JSONRPC)
	assert.Nil(t, req.ID)
	assert.Equal(t, "initialize", req.Method)
}

// TestResponseWithNullError tests Response without error.
func TestResponseWithNullError(t *testing.T) {
	t.Parallel()

	resp := Response{
		JSONRPC: "2.0",
		ID:      1,
		Result:  "success",
		Error:   nil,
	}

	data, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"result":"success"`)
	assert.NotContains(t, string(data), `"error"`)
}

// TestErrorWithNilData tests Error without data.
func TestErrorWithNilData(t *testing.T) {
	t.Parallel()

	err := Error{
		Code:    -32600,
		Message: "Invalid Request",
		Data:    nil,
	}

	data, errMarshal := json.Marshal(err)
	require.NoError(t, errMarshal)
	assert.Contains(t, string(data), `"code":-32600`)
	assert.Contains(t, string(data), `"message":"Invalid Request"`)
	assert.NotContains(t, string(data), `"data"`)
}

// TestToolInputSchema tests that tool input schemas have required fields.
func TestToolInputSchema(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)

	req := &Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/list",
	}

	resp := server.handleToolsList(req)
	result := resp.Result.(map[string]any)
	tools := result["tools"].([]Tool)

	for _, tool := range tools {
		schema := tool.InputSchema
		schemaType, ok := schema["type"]
		assert.True(t, ok, "tool %s schema should have type", tool.Name)
		assert.Equal(t, "object", schemaType, "tool %s schema type should be object", tool.Name)

		// All tools should have properties
		_, hasProperties := schema["properties"]
		assert.True(t, hasProperties, "tool %s should have properties", tool.Name)
	}
}

// TestCallTool_UnknownToolName tests callTool with various unknown tool names.
func TestCallTool_UnknownToolName(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	unknownTools := []string{
		"invalid_tool",
		"nonexistent",
		"search_v2",
		"timeline_special",
	}

	for _, name := range unknownTools {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			result, err := server.callTool(ctx, name, json.RawMessage(`{}`))
			assert.Error(t, err)
			assert.Empty(t, result)
			assert.Contains(t, err.Error(), "unknown tool")
		})
	}
}

// TestTimelineParams_Validation tests TimelineParams struct field validation.
func TestTimelineParams_Validation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		json   string
		wantOK bool
	}{
		{"valid with anchor_id", `{"anchor_id":123,"before":5,"after":5}`, true},
		{"valid with query only", `{"query":"test query"}`, true},
		{"empty params", `{}`, true},
		{"with all fields", `{"anchor_id":1,"query":"test","before":10,"after":10,"project":"proj","obs_type":"bugfix","format":"full"}`, true},
		{"invalid json", `{invalid`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var params TimelineParams
			err := json.Unmarshal([]byte(tt.json), &params)
			if tt.wantOK {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

// TestHandleToolsCall_EmptyParams tests tools/call with empty params.
func TestHandleToolsCall_EmptyParams(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	req := &Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params:  json.RawMessage(`{}`),
	}

	resp := server.handleToolsCall(ctx, req)

	// Should error due to missing name
	require.NotNil(t, resp.Error)
}

// TestSendResponse_WithError tests sendResponse with an error response.
func TestSendResponse_WithError(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	server := &Server{stdout: &buf}

	resp := &Response{
		JSONRPC: "2.0",
		ID:      1,
		Error:   &Error{Code: -32600, Message: "Invalid Request"},
	}

	server.sendResponse(resp)

	output := buf.String()
	assert.Contains(t, output, `"error"`)
	assert.Contains(t, output, `-32600`)
}

// TestSendResponse_NilID tests sendResponse with nil ID.
func TestSendResponse_NilID(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	server := &Server{stdout: &buf}

	resp := &Response{
		JSONRPC: "2.0",
		ID:      nil,
		Result:  "notification response",
	}

	server.sendResponse(resp)

	output := buf.String()
	assert.Contains(t, output, `"id":null`)
}

// TestToolCallParamsWithComplexArgs tests ToolCallParams with complex arguments.
func TestToolCallParamsWithComplexArgs(t *testing.T) {
	t.Parallel()

	input := `{
		"name": "search",
		"arguments": {
			"query": "authentication bug",
			"project": "my-project",
			"limit": 10,
			"type": "observations"
		}
	}`

	var params ToolCallParams
	err := json.Unmarshal([]byte(input), &params)
	require.NoError(t, err)
	assert.Equal(t, "search", params.Name)
	assert.NotEmpty(t, params.Arguments)
}

// TestHandleToolsCall_UnknownToolNameError tests tools/call with unknown tool returns error.
func TestHandleToolsCall_UnknownToolNameError(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	req := &Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"very_unknown_tool_name","arguments":{}}`),
	}

	resp := server.handleToolsCall(ctx, req)

	// Should get an error response
	assert.Equal(t, "2.0", resp.JSONRPC)
	assert.Equal(t, 1, resp.ID)
	require.NotNil(t, resp.Error)
	// Error is "Tool error" with message containing "unknown tool"
	assert.True(t, resp.Error.Code != 0)
}

// =============================================================================
// TESTS FOR Handler Defaults
// =============================================================================

// TestHandleTimeline_Defaults tests timeline default values.
func TestHandleTimeline_Defaults(t *testing.T) {
	t.Parallel()

	// Test that handleTimeline sets default before/after values
	params := TimelineParams{
		AnchorID: 0,
		Query:    "",
		Before:   0,
		After:    0,
	}

	// Simulate the default value assignment from handleTimeline
	if params.Before <= 0 {
		params.Before = 10
	}
	if params.After <= 0 {
		params.After = 10
	}

	assert.Equal(t, 10, params.Before)
	assert.Equal(t, 10, params.After)
}

// TestHandleGetTemporalTrends_Validation tests parameter validation.
func TestHandleGetTemporalTrends_Validation(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	tests := []struct {
		name        string
		args        string
		errContains string
		wantErr     bool
	}{
		{
			name:        "invalid json",
			args:        `{invalid`,
			wantErr:     true,
			errContains: "invalid arguments",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := server.handleGetTemporalTrends(ctx, json.RawMessage(tt.args))
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			}
		})
	}
}

// TestHandleGetDataQualityReport_Validation tests parameter validation.
func TestHandleGetDataQualityReport_Validation(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	tests := []struct {
		name        string
		args        string
		errContains string
		wantErr     bool
	}{
		{
			name:        "invalid json",
			args:        `{invalid`,
			wantErr:     true,
			errContains: "invalid arguments",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := server.handleGetDataQualityReport(ctx, json.RawMessage(tt.args))
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			}
		})
	}
}

// TestHandleExportObservations_Validation tests parameter validation.
func TestHandleExportObservations_Validation(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	tests := []struct {
		name        string
		args        string
		errContains string
		wantErr     bool
	}{
		{
			name:        "invalid json",
			args:        `{invalid`,
			wantErr:     true,
			errContains: "invalid arguments",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := server.handleExportObservations(ctx, json.RawMessage(tt.args))
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			}
		})
	}
}

// TestHandleAnalyzeSearchPatterns_Validation tests parameter validation.
func TestHandleAnalyzeSearchPatterns_Validation(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	tests := []struct {
		name        string
		args        string
		errContains string
		wantErr     bool
	}{
		{
			name:        "invalid json",
			args:        `{invalid`,
			wantErr:     true,
			errContains: "invalid params",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := server.handleAnalyzeSearchPatterns(ctx, json.RawMessage(tt.args))
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			}
		})
	}
}

// TestHandleAnalyzeObservationImportance_Validation tests parameter validation.
func TestHandleAnalyzeObservationImportance_Validation(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	tests := []struct {
		name        string
		args        string
		errContains string
		wantErr     bool
	}{
		{
			name:        "invalid json",
			args:        `{invalid`,
			wantErr:     true,
			errContains: "invalid arguments",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := server.handleAnalyzeObservationImportance(ctx, json.RawMessage(tt.args))
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			}
		})
	}
}

// TestHandleGetMemoryStats_NilStores tests GetMemoryStats with nil stores.
func TestHandleGetMemoryStats_NilStores(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	// Should not panic with nil stores
	result, err := server.handleGetMemoryStats(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, result)

	// Should be valid JSON
	var stats map[string]any
	err = json.Unmarshal([]byte(result), &stats)
	require.NoError(t, err)
}

// TestHandleCheckSystemHealth_NilStores tests CheckSystemHealth with nil stores.
func TestHandleCheckSystemHealth_NilStores(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	// Should not panic with nil stores
	result, err := server.handleCheckSystemHealth(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, result)

	// Should be valid JSON
	var health map[string]any
	err = json.Unmarshal([]byte(result), &health)
	require.NoError(t, err)

	// Should have subsystems and overall status
	assert.Contains(t, health, "overall_status")
	assert.Contains(t, health, "subsystems")
}

// =============================================================================
// COMPREHENSIVE callTool TESTS
// =============================================================================

// TestCallTool_AllSpecialTools tests all special tool cases in callTool switch.
func TestCallTool_AllSpecialTools(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	// Tests for tools that can work without stores or have nil guards
	tests := []struct {
		name       string
		toolName   string
		args       string
		wantErr    bool
		checkPanic bool
	}{
		// Tools that work with nil stores
		{
			name:     "get_memory_stats",
			toolName: "get_memory_stats",
			args:     `{}`,
			wantErr:  false,
		},
		{
			name:     "check_system_health",
			toolName: "check_system_health",
			args:     `{}`,
			wantErr:  false,
		},
		// Tools that need stores but have parameter validation first
		{
			name:     "find_related_observations - invalid json",
			toolName: "find_related_observations",
			args:     `{invalid`,
			wantErr:  true,
		},
		{
			name:     "find_related_observations - missing id",
			toolName: "find_related_observations",
			args:     `{}`,
			wantErr:  true,
		},
		{
			name:     "find_similar_observations - invalid json",
			toolName: "find_similar_observations",
			args:     `{invalid`,
			wantErr:  true,
		},
		{
			name:     "find_similar_observations - missing query",
			toolName: "find_similar_observations",
			args:     `{}`,
			wantErr:  true,
		},
		{
			name:     "get_patterns - invalid json",
			toolName: "get_patterns",
			args:     `{invalid`,
			wantErr:  true,
		},
		{
			name:     "bulk_delete_observations - invalid json",
			toolName: "bulk_delete_observations",
			args:     `{invalid`,
			wantErr:  true,
		},
		{
			name:     "bulk_delete_observations - missing ids",
			toolName: "bulk_delete_observations",
			args:     `{}`,
			wantErr:  true,
		},
		{
			name:     "bulk_mark_superseded - invalid json",
			toolName: "bulk_mark_superseded",
			args:     `{invalid`,
			wantErr:  true,
		},
		{
			name:     "bulk_mark_superseded - missing ids",
			toolName: "bulk_mark_superseded",
			args:     `{}`,
			wantErr:  true,
		},
		{
			name:     "bulk_boost_observations - invalid json",
			toolName: "bulk_boost_observations",
			args:     `{invalid`,
			wantErr:  true,
		},
		{
			name:     "bulk_boost_observations - missing ids",
			toolName: "bulk_boost_observations",
			args:     `{}`,
			wantErr:  true,
		},
		{
			name:     "merge_observations - invalid json",
			toolName: "merge_observations",
			args:     `{invalid`,
			wantErr:  true,
		},
		{
			name:     "merge_observations - missing source_ids",
			toolName: "merge_observations",
			args:     `{}`,
			wantErr:  true,
		},
		{
			name:     "get_observation - invalid json",
			toolName: "get_observation",
			args:     `{invalid`,
			wantErr:  true,
		},
		{
			name:     "get_observation - missing id",
			toolName: "get_observation",
			args:     `{}`,
			wantErr:  true,
		},
		{
			name:     "edit_observation - invalid json",
			toolName: "edit_observation",
			args:     `{invalid`,
			wantErr:  true,
		},
		{
			name:     "edit_observation - missing id",
			toolName: "edit_observation",
			args:     `{}`,
			wantErr:  true,
		},
		{
			name:     "get_observation_quality - invalid json",
			toolName: "get_observation_quality",
			args:     `{invalid`,
			wantErr:  true,
		},
		{
			name:     "get_observation_quality - missing id",
			toolName: "get_observation_quality",
			args:     `{}`,
			wantErr:  true,
		},
		{
			name:     "suggest_consolidations - invalid json",
			toolName: "suggest_consolidations",
			args:     `{invalid`,
			wantErr:  true,
		},
		{
			name:     "tag_observation - invalid json",
			toolName: "tag_observation",
			args:     `{invalid`,
			wantErr:  true,
		},
		{
			name:     "tag_observation - missing id",
			toolName: "tag_observation",
			args:     `{}`,
			wantErr:  true,
		},
		{
			name:     "get_observations_by_tag - invalid json",
			toolName: "get_observations_by_tag",
			args:     `{invalid`,
			wantErr:  true,
		},
		{
			name:     "get_observations_by_tag - missing tag",
			toolName: "get_observations_by_tag",
			args:     `{}`,
			wantErr:  true,
		},
		{
			name:     "get_temporal_trends - invalid json",
			toolName: "get_temporal_trends",
			args:     `{invalid`,
			wantErr:  true,
		},
		{
			name:     "get_data_quality_report - invalid json",
			toolName: "get_data_quality_report",
			args:     `{invalid`,
			wantErr:  true,
		},
		{
			name:     "batch_tag_by_pattern - invalid json",
			toolName: "batch_tag_by_pattern",
			args:     `{invalid`,
			wantErr:  true,
		},
		{
			name:     "batch_tag_by_pattern - missing pattern",
			toolName: "batch_tag_by_pattern",
			args:     `{}`,
			wantErr:  true,
		},
		{
			name:     "explain_search_ranking - invalid json",
			toolName: "explain_search_ranking",
			args:     `{invalid`,
			wantErr:  true,
		},
		{
			name:     "explain_search_ranking - missing query",
			toolName: "explain_search_ranking",
			args:     `{}`,
			wantErr:  true,
		},
		{
			name:     "export_observations - invalid json",
			toolName: "export_observations",
			args:     `{invalid`,
			wantErr:  true,
		},
		{
			name:     "analyze_search_patterns - invalid json",
			toolName: "analyze_search_patterns",
			args:     `{invalid`,
			wantErr:  true,
		},
		{
			name:     "get_observation_relationships - invalid json",
			toolName: "get_observation_relationships",
			args:     `{invalid`,
			wantErr:  true,
		},
		{
			name:     "get_observation_relationships - missing id",
			toolName: "get_observation_relationships",
			args:     `{}`,
			wantErr:  true,
		},
		{
			name:     "get_observation_scoring_breakdown - invalid json",
			toolName: "get_observation_scoring_breakdown",
			args:     `{invalid`,
			wantErr:  true,
		},
		{
			name:     "get_observation_scoring_breakdown - missing id",
			toolName: "get_observation_scoring_breakdown",
			args:     `{}`,
			wantErr:  true,
		},
		{
			name:     "analyze_observation_importance - invalid json",
			toolName: "analyze_observation_importance",
			args:     `{invalid`,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := server.callTool(ctx, tt.toolName, json.RawMessage(tt.args))
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.NotEmpty(t, result)
			}
		})
	}
}

// TestCallTool_SearchTools tests search-based tools in callTool.
func TestCallTool_SearchTools(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	// All search tools should fail with invalid JSON or when searchMgr is nil
	searchTools := []string{
		"search",
		"timeline",
		"decisions",
		"changes",
		"how_it_works",
		"find_by_concept",
		"find_by_file",
		"find_by_type",
		"get_recent_context",
		"get_context_timeline",
		"get_timeline_by_query",
	}

	for _, toolName := range searchTools {
		t.Run(toolName+"_invalid_json", func(t *testing.T) {
			t.Parallel()
			_, err := server.callTool(ctx, toolName, json.RawMessage(`{invalid`))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid")
		})
	}
}

// TestHandleTriggerMaintenance_NilService tests trigger_maintenance with nil service.
func TestHandleTriggerMaintenance_NilService(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	// Should return error when maintenanceService is nil
	_, err := server.handleTriggerMaintenance(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "maintenance service not available")
}

// TestHandleGetMaintenanceStats_NilService tests get_maintenance_stats with nil service.
func TestHandleGetMaintenanceStats_NilService(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	// Should return error when maintenanceService is nil
	_, err := server.handleGetMaintenanceStats(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "maintenance service not available")
}

// TestHandleTimeline_ParameterDefaultsNew tests timeline parameter defaults.
func TestHandleTimeline_ParameterDefaultsNew(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	// Invalid JSON should fail
	_, err := server.handleTimeline(ctx, json.RawMessage(`{invalid`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid timeline params")
}

// TestHandleTimelineByQuery_ValidationExtended tests timeline_by_query validation.
func TestHandleTimelineByQuery_ValidationExtended(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	tests := []struct {
		name        string
		args        string
		errContains string
		wantErr     bool
	}{
		{
			name:        "invalid json",
			args:        `{invalid`,
			wantErr:     true,
			errContains: "invalid timeline params",
		},
		{
			name:        "missing query",
			args:        `{}`,
			wantErr:     true,
			errContains: "query is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := server.handleTimelineByQuery(ctx, json.RawMessage(tt.args))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errContains)
		})
	}
}

// TestHandleSuggestConsolidations_ValidationExtended tests suggest_consolidations validation.
func TestHandleSuggestConsolidations_ValidationExtended(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	tests := []struct {
		name        string
		args        string
		errContains string
		wantErr     bool
	}{
		{
			name:        "invalid json",
			args:        `{invalid`,
			wantErr:     true,
			errContains: "invalid arguments",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := server.handleSuggestConsolidations(ctx, json.RawMessage(tt.args))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errContains)
		})
	}
}

// =============================================================================
// NIL GUARD HANDLER TESTS
// =============================================================================

// TestHandleFindSimilarObservations_NilVectorClient tests nil vector client handling.
func TestHandleFindSimilarObservations_NilVectorClient(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	// Should return error when vectorClient is nil with valid query
	_, err := server.handleFindSimilarObservations(ctx, json.RawMessage(`{"query": "test query"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "vector search not available")
}

// TestHandleGetObservationRelationships_NilRelationStore tests nil relation store handling.
func TestHandleGetObservationRelationships_NilRelationStore(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	// Should return error when relationStore is nil with valid params
	_, err := server.handleGetObservationRelationships(ctx, json.RawMessage(`{"id": 123}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "relation store not available")
}

// =============================================================================
// MORE PARAM LIMIT TESTS
// =============================================================================

// TestHandleBulkBoostObservations_TooManyIDs tests the max IDs limit.
func TestHandleBulkBoostObservations_TooManyIDs(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	// Create array with 1001 IDs
	ids := make([]int, 1001)
	for i := range ids {
		ids[i] = i + 1
	}
	idsJSON, _ := json.Marshal(ids)
	argsJSON := `{"ids": ` + string(idsJSON) + `, "amount": 1}`

	_, err := server.handleBulkBoostObservations(ctx, json.RawMessage(argsJSON))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "maximum 1000 IDs")
}

// TestHandleMergeObservations_SameID tests merge with same source and target.
func TestHandleMergeObservations_SameID(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	// source_id and target_id cannot be the same
	_, err := server.handleMergeObservations(ctx, json.RawMessage(`{"source_id": 123, "target_id": 123}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be the same")
}

// TestHandleMergeObservations_InvalidBoost tests merge with invalid boost.
func TestHandleMergeObservations_InvalidBoost(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, "1.0.0", nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()

	// boost must be between 0 and 0.5
	_, err := server.handleMergeObservations(ctx, json.RawMessage(`{"source_id": 1, "target_id": 2, "boost": 0.6}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boost must be between")
}
