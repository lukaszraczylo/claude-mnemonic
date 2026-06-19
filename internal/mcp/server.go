// Package mcp provides the MCP (Model Context Protocol) server for claude-mnemonic.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
)

// Server is the MCP server that proxies tool calls to the worker HTTP API.
// Field order optimized for memory alignment (fieldalignment).
type Server struct {
	stdin        io.Reader
	stdout       io.Writer
	client       *http.Client
	workerURL    string
	project      string
	version      string
	writeMu      sync.Mutex
	lastActivity atomic.Int64
}

// NewServer creates a new MCP server that proxies to the worker HTTP API.
func NewServer(client *http.Client, workerURL, project, version string) *Server {
	return &Server{
		client:    client,
		workerURL: workerURL,
		project:   project,
		version:   version,
		stdin:     os.Stdin,
		stdout:    os.Stdout,
	}
}

// Request represents a JSON-RPC request.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response represents a JSON-RPC response.
type Response struct {
	ID      any    `json:"id"`
	Result  any    `json:"result,omitempty"`
	Error   *Error `json:"error,omitempty"`
	JSONRPC string `json:"jsonrpc"`
}

// Error represents a JSON-RPC error.
type Error struct {
	Data    any    `json:"data,omitempty"`
	Message string `json:"message"`
	Code    int    `json:"code"`
}

// ToolCallParams represents parameters for tools/call method.
type ToolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// Tool represents an MCP tool definition.
type Tool struct {
	InputSchema map[string]any `json:"inputSchema"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
}

// Run starts the MCP server loop.
func (s *Server) Run(ctx context.Context) error {
	scanner := bufio.NewScanner(s.stdin)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024) // 1MB max message size

	lines := make(chan string)
	scanErr := make(chan error, 1)

	// Track last activity for idle timeout
	s.lastActivity.Store(time.Now().Unix())

	go func() {
		defer close(lines)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		scanErr <- scanner.Err()
	}()

	// Monitor parent process liveness and idle timeout.
	// If the parent dies (ppid changes) or no messages arrive for 30 minutes, shut down.
	parentPID := os.Getppid()
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if os.Getppid() != parentPID {
					log.Info().Msg("Parent process died, shutting down MCP server")
					if closer, ok := s.stdin.(io.Closer); ok {
						_ = closer.Close()
					}
					return
				}
				if time.Since(time.Unix(s.lastActivity.Load(), 0)) > 30*time.Minute {
					log.Info().Msg("MCP server idle timeout (30m), shutting down")
					if closer, ok := s.stdin.(io.Closer); ok {
						_ = closer.Close()
					}
					return
				}
			}
		}
	}()

	// Semaphore limits concurrent request goroutines.
	const maxConcurrent = 10
	sem := make(chan struct{}, maxConcurrent)

	var wg sync.WaitGroup

	for {
		select {
		case <-ctx.Done():
			// Drain in-flight requests before returning.
			wg.Wait()
			return ctx.Err()
		case line, ok := <-lines:
			if !ok {
				// Scanner finished — drain in-flight requests, then check for errors.
				wg.Wait()
				err := <-scanErr
				if err != nil {
					if errors.Is(err, bufio.ErrTooLong) {
						log.Error().Msg("MCP message exceeded 1MB buffer limit")
					}
					return fmt.Errorf("scanner error: %w", err)
				}
				return nil
			}

			s.lastActivity.Store(time.Now().Unix())

			if line == "" {
				continue
			}

			var req Request
			if err := json.Unmarshal([]byte(line), &req); err != nil {
				// Parse errors are cheap — send inline, no goroutine needed.
				if werr := s.sendError(nil, -32700, "Parse error", err.Error()); werr != nil {
					return fmt.Errorf("write error: %w", werr)
				}
				continue
			}

			// Dispatch request to its own goroutine.
			// Semaphore is acquired inside the goroutine so the main
			// loop never blocks on a full semaphore (Fix #1).
			wg.Add(1)
			go func(r Request) {
				defer wg.Done()
				// Acquire semaphore inside goroutine, not blocking main loop
				select {
				case sem <- struct{}{}:
					defer func() { <-sem }()
				case <-ctx.Done():
					// Server shutting down — send error so caller isn't left waiting
					if r.ID != nil {
						_ = s.sendError(r.ID, -32000, "Server shutting down", nil)
					}
					return
				}

				resp := s.handleRequest(ctx, &r)
				if resp != nil {
					if werr := s.sendResponse(resp); werr != nil {
						log.Error().Err(werr).Msg("Failed to send response")
					}
				}
			}(req)
		}
	}
}

// handleRequest dispatches the request to the appropriate handler.
func (s *Server) handleRequest(ctx context.Context, req *Request) *Response {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(ctx, req)
	case "notifications/initialized", "notifications/cancelled":
		return nil // Notifications don't get responses
	default:
		if req.ID == nil {
			// Notifications must not receive responses per JSON-RPC 2.0
			return nil
		}
		return &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &Error{
				Code:    -32601,
				Message: "Method not found",
			},
		}
	}
}

// handleInitialize handles the initialize request.
func (s *Server) handleInitialize(req *Request) *Response {
	return &Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "claude-mnemonic",
				"version": s.version,
			},
		},
	}
}

// handleToolsList returns the list of available tools.
func (s *Server) handleToolsList(req *Request) *Response {
	tools := []Tool{
		{
			Name:        "search",
			Description: "Unified search across all memory types (observations, sessions, and user prompts) using vector-first semantic search (sqlite-vec).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query":     map[string]any{"type": "string", "description": "Natural language search query for semantic ranking"},
					"type":      map[string]any{"type": "string", "enum": []string{"observations", "sessions", "prompts"}, "description": "Filter by document type"},
					"project":   map[string]any{"type": "string", "description": "Filter by project name"},
					"obs_type":  map[string]any{"type": "string", "description": "Filter observations by type"},
					"concepts":  map[string]any{"type": "string", "description": "Filter by concept tags"},
					"files":     map[string]any{"type": "string", "description": "Filter by file paths"},
					"dateStart": map[string]any{"type": []string{"string", "number"}, "description": "Start date for filtering"},
					"dateEnd":   map[string]any{"type": []string{"string", "number"}, "description": "End date for filtering"},
					"orderBy":   map[string]any{"type": "string", "enum": []string{"relevance", "date_desc", "date_asc"}, "default": "date_desc"},
					"limit":     map[string]any{"type": "number", "default": 20, "minimum": 1, "maximum": 100},
					"offset":    map[string]any{"type": "number", "default": 0, "minimum": 0},
					"format":    map[string]any{"type": "string", "enum": []string{"index", "full"}, "default": "index"},
				},
			},
		},
		{
			Name:        "timeline",
			Description: "Fetch timeline of observations around a specific point in time.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"anchor_id": map[string]any{"type": "number", "description": "Observation ID to use as anchor"},
					"query":     map[string]any{"type": "string", "description": "Natural language query to find anchor observation"},
					"before":    map[string]any{"type": "number", "default": 10, "minimum": 0, "maximum": 100},
					"after":     map[string]any{"type": "number", "default": 10, "minimum": 0, "maximum": 100},
					"project":   map[string]any{"type": "string"},
					"concepts":  map[string]any{"type": "string"},
					"files":     map[string]any{"type": "string"},
					"obs_type":  map[string]any{"type": "string"},
					"format":    map[string]any{"type": "string", "enum": []string{"index", "full"}, "default": "index"},
				},
			},
		},
		{
			Name:        "observation",
			Description: "Manage individual observations. Set 'action' to choose the operation: get, edit, delete, supersede, boost, merge, related, similar, quality, relationships, scoring, tag, by_tag, batch_tag.",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"action"},
				"properties": map[string]any{
					"action":         map[string]any{"type": "string", "enum": []string{"get", "edit", "delete", "supersede", "boost", "merge", "related", "similar", "quality", "relationships", "scoring", "tag", "by_tag", "batch_tag"}, "description": "Operation to perform"},
					"id":             map[string]any{"type": "number", "description": "Observation ID (get, edit, quality, relationships, scoring, related)"},
					"ids":            map[string]any{"type": "array", "items": map[string]any{"type": "number"}, "description": "Observation IDs (delete, supersede, boost)"},
					"title":          map[string]any{"type": "string", "description": "edit: new title"},
					"subtitle":       map[string]any{"type": "string", "description": "edit: new subtitle"},
					"narrative":      map[string]any{"type": "string", "description": "edit: new narrative"},
					"facts":          map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "edit: new facts"},
					"concepts":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "edit: new concept tags"},
					"files_read":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "edit: new files read"},
					"files_modified": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "edit: new files modified"},
					"scope":          map[string]any{"type": "string", "enum": []string{"project", "global"}, "description": "edit: new scope"},
					"query":          map[string]any{"type": "string", "description": "similar: text to find similar observations for"},
					"project":        map[string]any{"type": "string", "description": "Filter by project (similar, by_tag, batch_tag)"},
					"min_confidence": map[string]any{"type": "number", "minimum": 0.0, "maximum": 1.0, "description": "related: minimum confidence (default 0.5)"},
					"min_similarity": map[string]any{"type": "number", "minimum": 0.0, "maximum": 1.0, "description": "similar: minimum similarity (default 0.7)"},
					"limit":          map[string]any{"type": "number", "description": "Max results (related, similar, by_tag, batch_tag)"},
					"delete_vectors": map[string]any{"type": "boolean", "description": "delete: also delete vectors (default true)"},
					"boost":          map[string]any{"type": "number", "minimum": -1.0, "maximum": 1.0, "description": "boost: amount; merge: target boost"},
					"source_id":      map[string]any{"type": "number", "description": "merge: source observation ID (superseded)"},
					"target_id":      map[string]any{"type": "number", "description": "merge: target observation ID (kept)"},
					"max_depth":      map[string]any{"type": "number", "minimum": 1, "maximum": 5, "description": "relationships: traversal hops (default 2)"},
					"tag":            map[string]any{"type": "string", "description": "by_tag: tag/concept to search for"},
					"tags":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "tag/batch_tag: tags to apply"},
					"mode":           map[string]any{"type": "string", "enum": []string{"add", "remove", "set"}, "description": "tag: add/remove/set (default add)"},
					"pattern":        map[string]any{"type": "string", "description": "batch_tag: search pattern to match"},
					"dry_run":        map[string]any{"type": "boolean", "description": "batch_tag: preview only (default true)"},
					"max_matches":    map[string]any{"type": "number", "description": "batch_tag: max observations to tag"},
				},
			},
		},
		{
			Name:        "memory_admin",
			Description: "Memory system administration and analytics. Set 'action': stats, health, maintenance_stats, run_maintenance, importance, search_patterns, explain_ranking, temporal_trends, data_quality, export, suggest_consolidations, patterns.",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"action"},
				"properties": map[string]any{
					"action":                  map[string]any{"type": "string", "enum": []string{"stats", "health", "maintenance_stats", "run_maintenance", "importance", "search_patterns", "explain_ranking", "temporal_trends", "data_quality", "export", "suggest_consolidations", "patterns"}, "description": "Operation to perform"},
					"project":                 map[string]any{"type": "string", "description": "Filter by project (importance, temporal_trends, data_quality, export, suggest_consolidations, patterns, explain_ranking)"},
					"query":                   map[string]any{"type": "string", "description": "explain_ranking: query to analyze; patterns: search by name/description"},
					"top_n":                   map[string]any{"type": "number", "description": "Top results (explain_ranking, search_patterns)"},
					"days":                    map[string]any{"type": "number", "description": "Days to analyze (search_patterns, temporal_trends)"},
					"group_by":                map[string]any{"type": "string", "enum": []string{"day", "week", "hour_of_day"}, "description": "temporal_trends: grouping (default day)"},
					"limit":                   map[string]any{"type": "number", "description": "Max results (importance, data_quality, export, suggest_consolidations, patterns)"},
					"min_similarity":          map[string]any{"type": "number", "minimum": 0.5, "maximum": 1.0, "description": "suggest_consolidations: grouping threshold (default 0.8)"},
					"format":                  map[string]any{"type": "string", "enum": []string{"json", "jsonl", "markdown"}, "description": "export: output format (default json)"},
					"date_start":              map[string]any{"type": "number", "description": "export: filter start (epoch ms)"},
					"date_end":                map[string]any{"type": "number", "description": "export: filter end (epoch ms)"},
					"obs_type":                map[string]any{"type": "string", "description": "export: filter by observation type"},
					"type":                    map[string]any{"type": "string", "enum": []string{"workflow", "preference", "best_practice", "anti_pattern", "tooling"}, "description": "patterns: filter by pattern type"},
					"include_top_scored":      map[string]any{"type": "boolean", "description": "importance: include top-scoring (default true)"},
					"include_most_retrieved":  map[string]any{"type": "boolean", "description": "importance: include most-retrieved (default true)"},
					"include_concept_weights": map[string]any{"type": "boolean", "description": "importance: include concept weights (default true)"},
				},
			},
		},
	}

	return &Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"tools": tools,
		},
	}
}

// handleToolsCall handles tool invocations.
func (s *Server) handleToolsCall(ctx context.Context, req *Request) *Response {
	var params ToolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &Error{
				Code:    -32602,
				Message: "Invalid params",
				Data:    err.Error(),
			},
		}
	}

	result, err := s.callTool(ctx, params.Name, params.Arguments)
	if err != nil {
		// MCP spec: tool failures use Result with isError, not top-level Error
		return &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": "Error: " + err.Error()},
				},
				"isError": true,
			},
		}
	}

	return &Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"content": []map[string]any{
				{
					"type": "text",
					"text": result,
				},
			},
		},
	}
}

// searchArgs holds common search parameters used by many tools.
type searchArgs struct {
	Query     string `json:"query"`
	Project   string `json:"project"`
	Type      string `json:"type"`
	ObsType   string `json:"obs_type"`
	Concepts  string `json:"concepts"`
	Files     string `json:"files"`
	DateStart any    `json:"dateStart"`
	DateEnd   any    `json:"dateEnd"`
	OrderBy   string `json:"orderBy"`
	Format    string `json:"format"`
	Limit     int    `json:"limit"`
	Offset    int    `json:"offset"`
}

// observationActions maps the 'observation' tool's action values to the internal tool name that implements them.
var observationActions = map[string]string{
	"get":           "get_observation",
	"edit":          "edit_observation",
	"delete":        "bulk_delete_observations",
	"supersede":     "bulk_mark_superseded",
	"boost":         "bulk_boost_observations",
	"merge":         "merge_observations",
	"related":       "find_related_observations",
	"similar":       "find_similar_observations",
	"quality":       "get_observation_quality",
	"relationships": "get_observation_relationships",
	"scoring":       "get_observation_scoring_breakdown",
	"tag":           "tag_observation",
	"by_tag":        "get_observations_by_tag",
	"batch_tag":     "batch_tag_by_pattern",
}

// adminActions maps the 'memory_admin' tool's action values to the internal tool name that implements them.
var adminActions = map[string]string{
	"stats":                  "get_memory_stats",
	"health":                 "check_system_health",
	"maintenance_stats":      "get_maintenance_stats",
	"run_maintenance":        "trigger_maintenance",
	"importance":             "analyze_observation_importance",
	"search_patterns":        "analyze_search_patterns",
	"explain_ranking":        "explain_search_ranking",
	"temporal_trends":        "get_temporal_trends",
	"data_quality":           "get_data_quality_report",
	"export":                 "export_observations",
	"suggest_consolidations": "suggest_consolidations",
	"patterns":               "get_patterns",
}

// dispatchAction routes a multiplexed tool call to its underlying implementation by reading the "action" field.
func (s *Server) dispatchAction(ctx context.Context, tool string, actions map[string]string, args json.RawMessage) (string, error) {
	var a struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("%s: invalid arguments: %w", tool, err)
	}
	target, ok := actions[a.Action]
	if !ok {
		return "", fmt.Errorf("%s: unknown action %q", tool, a.Action)
	}
	return s.callTool(ctx, target, args)
}

// callTool dispatches to the appropriate tool handler by proxying to the worker HTTP API.
func (s *Server) callTool(ctx context.Context, name string, args json.RawMessage) (string, error) {
	// Parse common search params used by many tools
	var sa searchArgs
	// Best-effort parse; individual handlers validate as needed
	_ = json.Unmarshal(args, &sa)

	// Default project from server config
	if sa.Project == "" {
		sa.Project = s.project
	}

	switch name {
	// --- Search-based tools: proxy to GET /api/context/search ---
	case "search":
		return s.handleSearchProxy(ctx, sa)
	case "observation":
		return s.dispatchAction(ctx, "observation", observationActions, args)
	case "memory_admin":
		return s.dispatchAction(ctx, "memory_admin", adminActions, args)
	case "decisions":
		sa.ObsType = "decision"
		return s.handleSearchProxy(ctx, sa)
	case "changes":
		sa.ObsType = "code_change"
		return s.handleSearchProxy(ctx, sa)
	case "how_it_works":
		sa.ObsType = "architecture"
		return s.handleSearchProxy(ctx, sa)
	case "find_by_concept":
		return s.handleSearchProxy(ctx, sa)
	case "find_by_file":
		return s.handleSearchProxy(ctx, sa)
	case "find_by_type":
		return s.handleSearchProxy(ctx, sa)
	case "get_recent_context":
		return s.handleSearchProxy(ctx, sa)
	case "timeline", "get_context_timeline":
		return s.handleTimelineProxy(ctx, args)
	case "get_timeline_by_query":
		return s.handleTimelineProxy(ctx, args)

	// --- Observation endpoints ---
	case "get_observation":
		return s.handleGetObservationProxy(ctx, args)
	case "edit_observation":
		return s.handleEditObservationProxy(ctx, args)
	case "find_related_observations":
		return s.handleFindRelatedProxy(ctx, args)
	case "find_similar_observations":
		return s.handleFindSimilarProxy(ctx, args)
	case "get_observation_quality":
		return s.handleGetObservationQualityProxy(ctx, args)
	case "get_observation_relationships":
		return s.handleGetRelationshipsProxy(ctx, args)
	case "get_observation_scoring_breakdown":
		return s.handleGetScoringBreakdownProxy(ctx, args)
	case "tag_observation":
		return s.handleTagObservationProxy(ctx, args)
	case "get_observations_by_tag":
		return s.handleGetObservationsByTagProxy(ctx, args)

	// --- Bulk operations ---
	case "bulk_delete_observations":
		return s.handleBulkStatusProxy(ctx, args, "delete")
	case "bulk_mark_superseded":
		return s.handleBulkStatusProxy(ctx, args, "supersede")
	case "bulk_boost_observations":
		return s.handleBulkStatusProxy(ctx, args, "boost")
	case "merge_observations":
		return s.handleMergeProxy(ctx, args)

	// --- Pattern endpoints ---
	case "get_patterns":
		return s.handleGetPatternsProxy(ctx, args)

	// --- Stats and analytics endpoints ---
	case "get_memory_stats":
		return s.proxyGetRaw(ctx, "/api/stats", map[string]string{
			"project": s.project,
		})
	case "check_system_health":
		return s.proxyGetRaw(ctx, "/api/selfcheck", nil)
	case "get_maintenance_stats":
		return s.proxyGetRaw(ctx, "/api/stats", map[string]string{
			"project": s.project,
		})
	case "trigger_maintenance":
		return s.proxyPostRaw(ctx, "/api/maintenance/run", nil)
	case "analyze_observation_importance":
		return s.handleAnalyzeImportanceProxy(ctx, args)
	case "analyze_search_patterns":
		return s.proxyGetRaw(ctx, "/api/search/analytics", nil)
	case "explain_search_ranking":
		return s.handleExplainSearchProxy(ctx, args)
	case "get_temporal_trends":
		return s.handleGetTemporalTrendsProxy(ctx, args)
	case "get_data_quality_report":
		return s.handleGetDataQualityProxy(ctx, args)

	// --- Export and batch tag ---
	case "export_observations":
		return s.handleExportProxy(ctx, args)
	case "suggest_consolidations":
		return s.handleSuggestConsolidationsProxy(ctx, args)
	case "batch_tag_by_pattern":
		return s.handleBatchTagProxy(ctx, args)

	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

// =============================================================================
// HTTP PROXY HELPERS
// =============================================================================

// proxyGetRaw performs a GET request to the worker and returns the raw JSON response body.
func (s *Server) proxyGetRaw(ctx context.Context, path string, params map[string]string) (string, error) {
	if s.client == nil {
		return "", fmt.Errorf("worker unavailable at %s: http client not configured", s.workerURL)
	}
	req, err := http.NewRequestWithContext(ctx, "GET", s.workerURL+path, nil)
	if err != nil {
		return "", err
	}
	if len(params) > 0 {
		q := req.URL.Query()
		for k, v := range params {
			if v != "" {
				q.Set(k, v)
			}
		}
		req.URL.RawQuery = q.Encode()
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("worker unavailable at %s: %w", s.workerURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read worker response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("worker returned %d: %s", resp.StatusCode, string(body))
	}

	return string(body), nil
}

// proxyPostRaw performs a POST request to the worker and returns the raw JSON response body.
func (s *Server) proxyPostRaw(ctx context.Context, path string, payload any) (string, error) {
	if s.client == nil {
		return "", fmt.Errorf("worker unavailable at %s: http client not configured", s.workerURL)
	}
	var bodyReader io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return "", err
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", s.workerURL+path, bodyReader)
	if err != nil {
		return "", err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("worker unavailable at %s: %w", s.workerURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read worker response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("worker returned %d: %s", resp.StatusCode, string(body))
	}

	return string(body), nil
}

// proxyPutRaw performs a PUT request to the worker and returns the raw JSON response body.
func (s *Server) proxyPutRaw(ctx context.Context, path string, payload any) (string, error) {
	if s.client == nil {
		return "", fmt.Errorf("worker unavailable at %s: http client not configured", s.workerURL)
	}
	var bodyReader io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return "", err
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, "PUT", s.workerURL+path, bodyReader)
	if err != nil {
		return "", err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("worker unavailable at %s: %w", s.workerURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read worker response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("worker returned %d: %s", resp.StatusCode, string(body))
	}

	return string(body), nil
}

// anyToString converts an interface{} value to its string representation for query params.
func anyToString(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return strconv.FormatInt(int64(val), 10)
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// =============================================================================
// TOOL HANDLER PROXIES
// =============================================================================

// handleSearchProxy proxies search requests to GET /api/context/search.
func (s *Server) handleSearchProxy(ctx context.Context, args searchArgs) (string, error) {
	params := map[string]string{
		"project": args.Project,
		"query":   args.Query,
	}
	if args.Limit > 0 {
		params["limit"] = strconv.Itoa(args.Limit)
	}
	if args.ObsType != "" {
		params["obs_type"] = args.ObsType
	}
	if args.Concepts != "" {
		params["concepts"] = args.Concepts
	}
	if args.Files != "" {
		params["files"] = args.Files
	}
	if args.DateStart != nil {
		if ds := anyToString(args.DateStart); ds != "" {
			params["dateStart"] = ds
		}
	}
	if args.DateEnd != nil {
		if de := anyToString(args.DateEnd); de != "" {
			params["dateEnd"] = de
		}
	}

	return s.proxyGetRaw(ctx, "/api/context/search", params)
}

// handleTimelineProxy proxies timeline requests. First searches for anchor, then fetches surrounding observations.
func (s *Server) handleTimelineProxy(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		AnchorID int64  `json:"anchor_id"`
		Query    string `json:"query"`
		Project  string `json:"project"`
		Before   int    `json:"before"`
		After    int    `json:"after"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid timeline params: %w", err)
	}

	if params.Project == "" {
		params.Project = s.project
	}
	if params.Before <= 0 {
		params.Before = 10
	}
	if params.After <= 0 {
		params.After = 10
	}

	// If query provided and no anchor, search for it first
	if params.Query != "" && params.AnchorID == 0 {
		searchResult, err := s.proxyGetRaw(ctx, "/api/context/search", map[string]string{
			"project": params.Project,
			"query":   params.Query,
			"limit":   "1",
		})
		if err != nil {
			return "", err
		}
		// Extract first observation ID from search results
		var searchResp struct {
			Observations []struct {
				ID int64 `json:"id"`
			} `json:"observations"`
		}
		if err := json.Unmarshal([]byte(searchResult), &searchResp); err == nil && len(searchResp.Observations) > 0 {
			params.AnchorID = searchResp.Observations[0].ID
		}
	}

	if params.AnchorID == 0 {
		result, _ := json.Marshal(map[string]any{"observations": []any{}, "message": "no anchor found"})
		return string(result), nil
	}

	// Get observations around the anchor
	limit := params.Before + params.After + 1
	return s.proxyGetRaw(ctx, "/api/observations", map[string]string{
		"project": params.Project,
		"limit":   strconv.Itoa(limit),
	})
}

// handleGetObservationProxy proxies get observation by ID.
func (s *Server) handleGetObservationProxy(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if params.ID == 0 {
		return "", fmt.Errorf("id is required")
	}

	return s.proxyGetRaw(ctx, fmt.Sprintf("/api/observations/%d", params.ID), nil)
}

// handleEditObservationProxy proxies observation edits.
func (s *Server) handleEditObservationProxy(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Title         *string  `json:"title,omitempty"`
		Subtitle      *string  `json:"subtitle,omitempty"`
		Narrative     *string  `json:"narrative,omitempty"`
		Scope         *string  `json:"scope,omitempty"`
		Facts         []string `json:"facts,omitempty"`
		Concepts      []string `json:"concepts,omitempty"`
		FilesRead     []string `json:"files_read,omitempty"`
		FilesModified []string `json:"files_modified,omitempty"`
		ID            int64    `json:"id"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if params.ID == 0 {
		return "", fmt.Errorf("id is required")
	}

	return s.proxyPutRaw(ctx, fmt.Sprintf("/api/observations/%d", params.ID), params)
}

// handleFindRelatedProxy proxies find related observations.
func (s *Server) handleFindRelatedProxy(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		ID            int64   `json:"id"`
		MinConfidence float64 `json:"min_confidence"`
		Limit         int     `json:"limit"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if params.ID == 0 {
		return "", fmt.Errorf("id is required")
	}

	qp := map[string]string{}
	if params.MinConfidence > 0 {
		qp["min_confidence"] = strconv.FormatFloat(params.MinConfidence, 'f', -1, 64)
	}
	if params.Limit > 0 {
		qp["limit"] = strconv.Itoa(params.Limit)
	}

	return s.proxyGetRaw(ctx, fmt.Sprintf("/api/observations/%d/related", params.ID), qp)
}

// handleFindSimilarProxy proxies find similar observations via context search.
func (s *Server) handleFindSimilarProxy(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Query         string  `json:"query"`
		Project       string  `json:"project"`
		MinSimilarity float64 `json:"min_similarity"`
		Limit         int     `json:"limit"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if params.Query == "" {
		return "", fmt.Errorf("query is required")
	}
	if params.Project == "" {
		params.Project = s.project
	}
	if params.Limit == 0 {
		params.Limit = 10
	}

	// Use context search which uses vector similarity
	return s.proxyGetRaw(ctx, "/api/context/search", map[string]string{
		"project": params.Project,
		"query":   params.Query,
		"limit":   strconv.Itoa(params.Limit),
	})
}

// handleGetObservationQualityProxy proxies observation quality check.
// Fetches the observation and computes quality metrics locally (lightweight computation).
func (s *Server) handleGetObservationQualityProxy(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if params.ID == 0 {
		return "", fmt.Errorf("id is required")
	}

	// Fetch the observation
	obsJSON, err := s.proxyGetRaw(ctx, fmt.Sprintf("/api/observations/%d", params.ID), nil)
	if err != nil {
		return "", err
	}

	// Parse observation for quality analysis
	var obs struct {
		Title           string   `json:"title"`
		Narrative       string   `json:"narrative"`
		Facts           []string `json:"facts"`
		Concepts        []string `json:"concepts"`
		FilesRead       []string `json:"files_read"`
		FilesModified   []string `json:"files_modified"`
		ImportanceScore float64  `json:"importance_score"`
		RetrievalCount  int      `json:"retrieval_count"`
		IsSuperseded    bool     `json:"is_superseded"`
	}
	if err := json.Unmarshal([]byte(obsJSON), &obs); err != nil {
		return "", fmt.Errorf("parse observation: %w", err)
	}

	// Calculate completeness score
	completenessScore := 0.0
	maxScore := 5.0
	var suggestions []string

	if obs.Title != "" {
		completenessScore += 1.0
	} else {
		suggestions = append(suggestions, "Add a descriptive title")
	}

	if len(obs.Narrative) > 50 {
		completenessScore += 1.5
	} else if obs.Narrative != "" {
		completenessScore += 0.5
		suggestions = append(suggestions, "Expand the narrative to provide more context (aim for 50+ characters)")
	} else {
		suggestions = append(suggestions, "Add a narrative explaining the observation")
	}

	if len(obs.Facts) >= 2 {
		completenessScore += 1.0
	} else if len(obs.Facts) == 1 {
		completenessScore += 0.5
		suggestions = append(suggestions, "Add more key facts (aim for 2+)")
	} else {
		suggestions = append(suggestions, "Add key facts to capture important details")
	}

	if len(obs.Concepts) >= 2 {
		completenessScore += 0.75
	} else if len(obs.Concepts) == 1 {
		completenessScore += 0.25
		suggestions = append(suggestions, "Add more concept tags for better discoverability")
	} else {
		suggestions = append(suggestions, "Add concept tags to categorize this observation")
	}

	if len(obs.FilesRead) > 0 || len(obs.FilesModified) > 0 {
		completenessScore += 0.75
	} else {
		suggestions = append(suggestions, "Consider adding file references if applicable")
	}

	qualityTier := "poor"
	switch {
	case completenessScore >= 4.0:
		qualityTier = "excellent"
	case completenessScore >= 3.0:
		qualityTier = "good"
	case completenessScore >= 2.0:
		qualityTier = "fair"
	}

	response := map[string]any{
		"id":                 params.ID,
		"completeness_score": completenessScore,
		"max_score":          maxScore,
		"completeness_pct":   (completenessScore / maxScore) * 100,
		"quality_tier":       qualityTier,
		"importance_score":   obs.ImportanceScore,
		"retrieval_count":    obs.RetrievalCount,
		"is_superseded":      obs.IsSuperseded,
		"suggestions":        suggestions,
		"field_stats": map[string]any{
			"has_title":            obs.Title != "",
			"has_narrative":        obs.Narrative != "",
			"narrative_length":     len(obs.Narrative),
			"facts_count":          len(obs.Facts),
			"concepts_count":       len(obs.Concepts),
			"files_read_count":     len(obs.FilesRead),
			"files_modified_count": len(obs.FilesModified),
		},
	}

	output, err := json.Marshal(response)
	if err != nil {
		return "", fmt.Errorf("marshal response: %w", err)
	}

	return string(output), nil
}

// handleGetRelationshipsProxy proxies observation relationship graph requests.
func (s *Server) handleGetRelationshipsProxy(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		ID       int64 `json:"id"`
		MaxDepth int   `json:"max_depth"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid params: %w", err)
	}
	if params.ID <= 0 {
		return "", fmt.Errorf("id is required and must be positive")
	}

	qp := map[string]string{}
	if params.MaxDepth > 0 {
		qp["max_depth"] = strconv.Itoa(params.MaxDepth)
	}

	return s.proxyGetRaw(ctx, fmt.Sprintf("/api/observations/%d/graph", params.ID), qp)
}

// handleGetScoringBreakdownProxy proxies observation scoring breakdown requests.
func (s *Server) handleGetScoringBreakdownProxy(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if params.ID <= 0 {
		return "", fmt.Errorf("id is required and must be positive")
	}

	return s.proxyGetRaw(ctx, fmt.Sprintf("/api/observations/%d/score", params.ID), nil)
}

// handleTagObservationProxy proxies tag operations on observations.
// Fetches current tags, computes new tag set, then updates via PUT.
func (s *Server) handleTagObservationProxy(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Mode string   `json:"mode"`
		Tags []string `json:"tags"`
		ID   int64    `json:"id"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if params.ID == 0 {
		return "", fmt.Errorf("id is required")
	}
	if len(params.Tags) == 0 {
		return "", fmt.Errorf("tags is required")
	}
	if params.Mode == "" {
		params.Mode = "add"
	}
	if params.Mode != "add" && params.Mode != "remove" && params.Mode != "set" {
		return "", fmt.Errorf("mode must be 'add', 'remove', or 'set'")
	}

	// Fetch current observation to compute new tags
	obsJSON, err := s.proxyGetRaw(ctx, fmt.Sprintf("/api/observations/%d", params.ID), nil)
	if err != nil {
		return "", fmt.Errorf("get observation: %w", err)
	}

	var obs struct {
		Concepts []string `json:"concepts"`
	}
	if err := json.Unmarshal([]byte(obsJSON), &obs); err != nil {
		return "", fmt.Errorf("parse observation: %w", err)
	}

	// Compute new tags
	var newTags []string
	switch params.Mode {
	case "set":
		newTags = params.Tags
	case "add":
		tagSet := make(map[string]bool)
		for _, t := range obs.Concepts {
			tagSet[t] = true
			newTags = append(newTags, t)
		}
		for _, t := range params.Tags {
			if !tagSet[t] {
				newTags = append(newTags, t)
			}
		}
	case "remove":
		removeSet := make(map[string]bool)
		for _, t := range params.Tags {
			removeSet[t] = true
		}
		for _, t := range obs.Concepts {
			if !removeSet[t] {
				newTags = append(newTags, t)
			}
		}
	}

	// Update via PUT
	updatePayload := map[string]any{
		"concepts": newTags,
	}
	result, err := s.proxyPutRaw(ctx, fmt.Sprintf("/api/observations/%d", params.ID), updatePayload)
	if err != nil {
		return "", fmt.Errorf("update observation: %w", err)
	}

	// Wrap response
	response := map[string]any{
		"id":           params.ID,
		"mode":         params.Mode,
		"tags_applied": params.Tags,
		"result":       json.RawMessage(result),
	}
	output, err := json.Marshal(response)
	if err != nil {
		return "", fmt.Errorf("marshal response: %w", err)
	}

	return string(output), nil
}

// handleGetObservationsByTagProxy proxies tag-based observation lookup.
func (s *Server) handleGetObservationsByTagProxy(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Tag     string `json:"tag"`
		Project string `json:"project"`
		Limit   int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if params.Tag == "" {
		return "", fmt.Errorf("tag is required")
	}
	if params.Project == "" {
		params.Project = s.project
	}
	if params.Limit == 0 {
		params.Limit = 50
	}

	return s.proxyGetRaw(ctx, "/api/context/search", map[string]string{
		"project":  params.Project,
		"query":    params.Tag,
		"concepts": params.Tag,
		"limit":    strconv.Itoa(params.Limit),
	})
}

// handleBulkStatusProxy proxies bulk status update operations.
func (s *Server) handleBulkStatusProxy(ctx context.Context, args json.RawMessage, action string) (string, error) {
	var params struct {
		IDs           []int64 `json:"ids"`
		Boost         float64 `json:"boost"`
		DeleteVectors bool    `json:"delete_vectors"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if len(params.IDs) == 0 {
		return "", fmt.Errorf("ids is required")
	}

	payload := map[string]any{
		"ids":    params.IDs,
		"action": action,
	}
	if action == "boost" {
		payload["boost"] = params.Boost
	}
	if action == "delete" {
		payload["delete_vectors"] = params.DeleteVectors
	}

	return s.proxyPostRaw(ctx, "/api/observations/bulk-status", payload)
}

// handleMergeProxy proxies merge observations request.
func (s *Server) handleMergeProxy(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		SourceID int64   `json:"source_id"`
		TargetID int64   `json:"target_id"`
		Boost    float64 `json:"boost"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if params.SourceID == 0 || params.TargetID == 0 {
		return "", fmt.Errorf("source_id and target_id are required")
	}

	// Merge = mark source as superseded + boost target
	// Step 1: Mark source as superseded
	_, err := s.proxyPostRaw(ctx, "/api/observations/bulk-status", map[string]any{
		"ids":    []int64{params.SourceID},
		"action": "supersede",
	})
	if err != nil {
		return "", fmt.Errorf("mark source as superseded: %w", err)
	}

	// Step 2: Boost target via feedback
	_, err = s.proxyPostRaw(ctx, fmt.Sprintf("/api/observations/%d/feedback", params.TargetID), map[string]any{
		"feedback": "positive",
	})
	if err != nil {
		log.Warn().Err(err).Msg("Failed to boost target observation after merge")
	}

	response := map[string]any{
		"merged":    true,
		"source_id": params.SourceID,
		"target_id": params.TargetID,
	}
	output, err := json.Marshal(response)
	if err != nil {
		return "", fmt.Errorf("marshal response: %w", err)
	}

	return string(output), nil
}

// handleGetPatternsProxy proxies pattern queries.
func (s *Server) handleGetPatternsProxy(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Type    string `json:"type"`
		Project string `json:"project"`
		Query   string `json:"query"`
		Limit   int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	qp := map[string]string{}
	if params.Type != "" {
		qp["type"] = params.Type
	}
	if params.Project != "" {
		qp["project"] = params.Project
	}
	if params.Limit > 0 {
		qp["limit"] = strconv.Itoa(params.Limit)
	}

	// Use search endpoint if query provided, otherwise get all
	if params.Query != "" {
		qp["query"] = params.Query
		return s.proxyGetRaw(ctx, "/api/patterns/search", qp)
	}

	return s.proxyGetRaw(ctx, "/api/patterns", qp)
}

// handleAnalyzeImportanceProxy proxies importance analysis.
func (s *Server) handleAnalyzeImportanceProxy(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Project string `json:"project"`
		Limit   int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if params.Project == "" {
		params.Project = s.project
	}
	if params.Limit == 0 {
		params.Limit = 10
	}

	qp := map[string]string{
		"project": params.Project,
		"limit":   strconv.Itoa(params.Limit),
	}

	// Combine results from multiple endpoints
	response := map[string]any{
		"project": params.Project,
	}

	// Get top-scoring observations
	topScored, err := s.proxyGetRaw(ctx, "/api/observations/top", qp)
	if err == nil {
		response["top_scoring"] = json.RawMessage(topScored)
	}

	// Get most-retrieved observations
	mostRetrieved, err := s.proxyGetRaw(ctx, "/api/observations/most-retrieved", qp)
	if err == nil {
		response["most_retrieved"] = json.RawMessage(mostRetrieved)
	}

	// Get scoring stats
	scoringStats, err := s.proxyGetRaw(ctx, "/api/scoring/stats", qp)
	if err == nil {
		response["scoring_stats"] = json.RawMessage(scoringStats)
	}

	// Get concept weights
	conceptWeights, err := s.proxyGetRaw(ctx, "/api/scoring/concepts", nil)
	if err == nil {
		response["concept_weights"] = json.RawMessage(conceptWeights)
	}

	output, err := json.Marshal(response)
	if err != nil {
		return "", fmt.Errorf("marshal response: %w", err)
	}
	return string(output), nil
}

// handleExplainSearchProxy proxies search ranking explanation.
func (s *Server) handleExplainSearchProxy(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Query   string `json:"query"`
		Project string `json:"project"`
		TopN    int    `json:"top_n"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if params.Query == "" {
		return "", fmt.Errorf("query is required")
	}
	if params.Project == "" {
		params.Project = s.project
	}
	if params.TopN == 0 {
		params.TopN = 5
	}

	return s.proxyGetRaw(ctx, "/api/context/search", map[string]string{
		"project": params.Project,
		"query":   params.Query,
		"limit":   strconv.Itoa(params.TopN),
	})
}

// handleGetTemporalTrendsProxy proxies temporal trend analysis.
func (s *Server) handleGetTemporalTrendsProxy(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Project string `json:"project"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if params.Project == "" {
		params.Project = s.project
	}

	return s.proxyGetRaw(ctx, "/api/stats", map[string]string{
		"project": params.Project,
	})
}

// handleGetDataQualityProxy proxies data quality report.
func (s *Server) handleGetDataQualityProxy(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Project string `json:"project"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if params.Project == "" {
		params.Project = s.project
	}

	return s.proxyGetRaw(ctx, "/api/stats", map[string]string{
		"project": params.Project,
	})
}

// handleExportProxy proxies observation export requests.
func (s *Server) handleExportProxy(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Format    string `json:"format"`
		Project   string `json:"project"`
		ObsType   string `json:"obs_type"`
		Limit     int    `json:"limit"`
		DateStart int64  `json:"date_start"`
		DateEnd   int64  `json:"date_end"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	qp := map[string]string{}
	if params.Format != "" {
		qp["format"] = params.Format
	}
	if params.Project != "" {
		qp["project"] = params.Project
	} else {
		qp["project"] = s.project
	}
	if params.ObsType != "" {
		qp["obs_type"] = params.ObsType
	}
	if params.Limit > 0 {
		qp["limit"] = strconv.Itoa(params.Limit)
	}
	if params.DateStart > 0 {
		qp["date_start"] = strconv.FormatInt(params.DateStart, 10)
	}
	if params.DateEnd > 0 {
		qp["date_end"] = strconv.FormatInt(params.DateEnd, 10)
	}

	return s.proxyGetRaw(ctx, "/api/observations/export", qp)
}

// handleSuggestConsolidationsProxy proxies consolidation suggestions via duplicates endpoint.
func (s *Server) handleSuggestConsolidationsProxy(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Project       string  `json:"project"`
		MinSimilarity float64 `json:"min_similarity"`
		Limit         int     `json:"limit"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if params.Project == "" {
		params.Project = s.project
	}

	qp := map[string]string{
		"project": params.Project,
	}
	if params.MinSimilarity > 0 {
		qp["min_similarity"] = strconv.FormatFloat(params.MinSimilarity, 'f', -1, 64)
	}
	if params.Limit > 0 {
		qp["limit"] = strconv.Itoa(params.Limit)
	}

	return s.proxyGetRaw(ctx, "/api/observations/duplicates", qp)
}

// handleBatchTagProxy proxies batch tag operations.
// Searches for matching observations and applies tags via PUT endpoint.
func (s *Server) handleBatchTagProxy(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Pattern    string   `json:"pattern"`
		Project    string   `json:"project"`
		Tags       []string `json:"tags"`
		MaxMatches int      `json:"max_matches"`
		DryRun     bool     `json:"dry_run"`
	}
	params.DryRun = true
	params.MaxMatches = 100

	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if params.Pattern == "" {
		return "", fmt.Errorf("pattern is required")
	}
	if len(params.Tags) == 0 {
		return "", fmt.Errorf("tags is required")
	}
	if params.Project == "" {
		params.Project = s.project
	}

	// Search for matching observations
	searchResult, err := s.proxyGetRaw(ctx, "/api/context/search", map[string]string{
		"project": params.Project,
		"query":   params.Pattern,
		"limit":   strconv.Itoa(params.MaxMatches),
	})
	if err != nil {
		return "", fmt.Errorf("search: %w", err)
	}

	var searchResp struct {
		Observations []struct {
			ID    int64  `json:"id"`
			Title string `json:"title"`
		} `json:"observations"`
	}
	if err := json.Unmarshal([]byte(searchResult), &searchResp); err != nil {
		return "", fmt.Errorf("parse search results: %w", err)
	}

	matches := make([]map[string]any, 0, len(searchResp.Observations))
	var taggedCount int

	for _, obs := range searchResp.Observations {
		matches = append(matches, map[string]any{
			"id":    obs.ID,
			"title": obs.Title,
		})

		if !params.DryRun {
			tagArgs, _ := json.Marshal(map[string]any{
				"id":   obs.ID,
				"tags": params.Tags,
				"mode": "add",
			})
			_, tagErr := s.handleTagObservationProxy(ctx, tagArgs)
			if tagErr == nil {
				taggedCount++
			}
		}
	}

	response := map[string]any{
		"pattern":       params.Pattern,
		"tags":          params.Tags,
		"dry_run":       params.DryRun,
		"matches_found": len(matches),
		"matches":       matches,
	}
	if !params.DryRun {
		response["tagged_count"] = taggedCount
	}

	output, err := json.Marshal(response)
	if err != nil {
		return "", fmt.Errorf("marshal response: %w", err)
	}

	return string(output), nil
}

// sendResponse sends a JSON-RPC response. Returns an error if writing fails.
func (s *Server) sendResponse(resp *Response) error {
	data, err := json.Marshal(resp)
	if err != nil {
		log.Error().Err(err).Msg("Failed to marshal response")
		// Send a fallback error so the caller isn't left waiting (Fix #3)
		fallback, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0",
			"id":      resp.ID,
			"error":   map[string]any{"code": -32603, "message": "internal marshal error"},
		})
		s.writeMu.Lock()
		_, _ = fmt.Fprintln(s.stdout, string(fallback))
		s.writeMu.Unlock()
		return fmt.Errorf("marshal error: %w", err)
	}

	// Bound the write to prevent pipe deadlock (Fix #2)
	done := make(chan error, 1)
	go func() {
		s.writeMu.Lock()
		_, werr := fmt.Fprintln(s.stdout, string(data))
		s.writeMu.Unlock()
		done <- werr
	}()

	select {
	case werr := <-done:
		if werr != nil {
			log.Error().Err(werr).Msg("Failed to write response to stdout")
			return werr
		}
		return nil
	case <-time.After(10 * time.Second):
		log.Error().Msg("Stdout write timed out — pipe likely full")
		return fmt.Errorf("write timeout: stdout pipe full")
	}
}

// sendError sends a JSON-RPC error response. Returns an error if writing fails.
func (s *Server) sendError(id any, code int, message string, data any) error {
	resp := &Response{
		JSONRPC: "2.0",
		ID:      id,
		Error: &Error{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
	return s.sendResponse(resp)
}
