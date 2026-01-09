// Package mcp provides the MCP (Model Context Protocol) server for claude-mnemonic.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/lukaszraczylo/claude-mnemonic/internal/db/gorm"
	"github.com/lukaszraczylo/claude-mnemonic/internal/scoring"
	"github.com/lukaszraczylo/claude-mnemonic/internal/search"
	"github.com/lukaszraczylo/claude-mnemonic/internal/vector/sqlitevec"
	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
	"github.com/rs/zerolog/log"
)

// Server is the MCP server that exposes search tools.
type Server struct {
	stdin            io.Reader
	stdout           io.Writer
	searchMgr        *search.Manager
	observationStore *gorm.ObservationStore
	patternStore     *gorm.PatternStore
	relationStore    *gorm.RelationStore
	sessionStore     *gorm.SessionStore
	vectorClient     *sqlitevec.Client
	scoreCalculator  *scoring.Calculator
	recalculator     *scoring.Recalculator
	version          string
}

// NewServer creates a new MCP server.
func NewServer(
	searchMgr *search.Manager,
	version string,
	observationStore *gorm.ObservationStore,
	patternStore *gorm.PatternStore,
	relationStore *gorm.RelationStore,
	sessionStore *gorm.SessionStore,
	vectorClient *sqlitevec.Client,
	scoreCalculator *scoring.Calculator,
	recalculator *scoring.Recalculator,
) *Server {
	return &Server{
		searchMgr:        searchMgr,
		version:          version,
		stdin:            os.Stdin,
		stdout:           os.Stdout,
		observationStore: observationStore,
		patternStore:     patternStore,
		relationStore:    relationStore,
		sessionStore:     sessionStore,
		vectorClient:     vectorClient,
		scoreCalculator:  scoreCalculator,
		recalculator:     recalculator,
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
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var req Request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			s.sendError(nil, -32700, "Parse error", err)
			continue
		}

		resp := s.handleRequest(ctx, &req)
		s.sendResponse(resp)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scanner error: %w", err)
	}
	return nil
}

// handleRequest dispatches the request to the appropriate handler.
func (s *Server) handleRequest(ctx context.Context, req *Request) *Response {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(ctx, req)
	default:
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
			Name:        "decisions",
			Description: "Semantic shortcut for finding architectural, design, and implementation decisions.",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"query"},
				"properties": map[string]any{
					"query":     map[string]any{"type": "string", "description": "Natural language query for finding decisions"},
					"dateStart": map[string]any{"type": []string{"string", "number"}},
					"dateEnd":   map[string]any{"type": []string{"string", "number"}},
					"limit":     map[string]any{"type": "number", "default": 20, "minimum": 1, "maximum": 100},
					"format":    map[string]any{"type": "string", "enum": []string{"index", "full"}, "default": "index"},
				},
			},
		},
		{
			Name:        "changes",
			Description: "Semantic shortcut for finding code changes, refactorings, and modifications.",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"query"},
				"properties": map[string]any{
					"query":     map[string]any{"type": "string", "description": "Natural language query for finding changes"},
					"dateStart": map[string]any{"type": []string{"string", "number"}},
					"dateEnd":   map[string]any{"type": []string{"string", "number"}},
					"limit":     map[string]any{"type": "number", "default": 20, "minimum": 1, "maximum": 100},
					"format":    map[string]any{"type": "string", "enum": []string{"index", "full"}, "default": "index"},
				},
			},
		},
		{
			Name:        "how_it_works",
			Description: "Semantic shortcut for understanding system architecture, design patterns, and implementation details.",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"query"},
				"properties": map[string]any{
					"query":     map[string]any{"type": "string", "description": "Natural language query for understanding how something works"},
					"dateStart": map[string]any{"type": []string{"string", "number"}},
					"dateEnd":   map[string]any{"type": []string{"string", "number"}},
					"limit":     map[string]any{"type": "number", "default": 20, "minimum": 1, "maximum": 100},
					"format":    map[string]any{"type": "string", "enum": []string{"index", "full"}, "default": "index"},
				},
			},
		},
		{
			Name:        "find_by_concept",
			Description: "Find observations tagged with specific concepts.",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"concepts"},
				"properties": map[string]any{
					"concepts":  map[string]any{"type": "string", "description": "Concept tag(s) to filter by"},
					"type":      map[string]any{"type": "string"},
					"files":     map[string]any{"type": "string"},
					"project":   map[string]any{"type": "string"},
					"dateStart": map[string]any{"type": []string{"string", "number"}},
					"dateEnd":   map[string]any{"type": []string{"string", "number"}},
					"orderBy":   map[string]any{"type": "string", "enum": []string{"date_desc", "date_asc"}, "default": "date_desc"},
					"limit":     map[string]any{"type": "number", "default": 20},
					"offset":    map[string]any{"type": "number", "default": 0},
					"format":    map[string]any{"type": "string", "enum": []string{"index", "full"}, "default": "index"},
				},
			},
		},
		{
			Name:        "find_by_file",
			Description: "Find observations related to specific file paths.",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"files"},
				"properties": map[string]any{
					"files":     map[string]any{"type": "string", "description": "File path(s) to filter by"},
					"type":      map[string]any{"type": "string"},
					"concepts":  map[string]any{"type": "string"},
					"project":   map[string]any{"type": "string"},
					"dateStart": map[string]any{"type": []string{"string", "number"}},
					"dateEnd":   map[string]any{"type": []string{"string", "number"}},
					"orderBy":   map[string]any{"type": "string", "enum": []string{"date_desc", "date_asc"}, "default": "date_desc"},
					"limit":     map[string]any{"type": "number", "default": 20},
					"offset":    map[string]any{"type": "number", "default": 0},
					"format":    map[string]any{"type": "string", "enum": []string{"index", "full"}, "default": "index"},
				},
			},
		},
		{
			Name:        "find_by_type",
			Description: "Find observations of specific types.",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"type"},
				"properties": map[string]any{
					"type":      map[string]any{"type": "string", "description": "Observation type(s) to filter by"},
					"concepts":  map[string]any{"type": "string"},
					"files":     map[string]any{"type": "string"},
					"project":   map[string]any{"type": "string"},
					"dateStart": map[string]any{"type": []string{"string", "number"}},
					"dateEnd":   map[string]any{"type": []string{"string", "number"}},
					"orderBy":   map[string]any{"type": "string", "enum": []string{"date_desc", "date_asc"}, "default": "date_desc"},
					"limit":     map[string]any{"type": "number", "default": 20},
					"offset":    map[string]any{"type": "number", "default": 0},
					"format":    map[string]any{"type": "string", "enum": []string{"index", "full"}, "default": "index"},
				},
			},
		},
		{
			Name:        "get_recent_context",
			Description: "Get recent session context for timeline display.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"project":   map[string]any{"type": "string"},
					"type":      map[string]any{"type": "string"},
					"concepts":  map[string]any{"type": "string"},
					"files":     map[string]any{"type": "string"},
					"dateStart": map[string]any{"type": []string{"string", "number"}},
					"dateEnd":   map[string]any{"type": []string{"string", "number"}},
					"limit":     map[string]any{"type": "number", "default": 30, "minimum": 1, "maximum": 100},
					"format":    map[string]any{"type": "string", "enum": []string{"index", "full"}, "default": "index"},
				},
			},
		},
		{
			Name:        "get_context_timeline",
			Description: "Get timeline of observations around a specific observation ID.",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"anchor_id"},
				"properties": map[string]any{
					"anchor_id": map[string]any{"type": "number", "description": "Observation ID to use as anchor point"},
					"before":    map[string]any{"type": "number", "default": 10, "minimum": 0, "maximum": 100},
					"after":     map[string]any{"type": "number", "default": 10, "minimum": 0, "maximum": 100},
					"project":   map[string]any{"type": "string"},
					"type":      map[string]any{"type": "string"},
					"concepts":  map[string]any{"type": "string"},
					"files":     map[string]any{"type": "string"},
					"format":    map[string]any{"type": "string", "enum": []string{"index", "full"}, "default": "index"},
				},
			},
		},
		{
			Name:        "get_timeline_by_query",
			Description: "Combined search + timeline tool. First searches for observations matching the query, then returns timeline around the best match.",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"query"},
				"properties": map[string]any{
					"query":     map[string]any{"type": "string", "description": "Natural language query to find anchor observation"},
					"before":    map[string]any{"type": "number", "default": 10, "minimum": 0, "maximum": 100},
					"after":     map[string]any{"type": "number", "default": 10, "minimum": 0, "maximum": 100},
					"project":   map[string]any{"type": "string"},
					"type":      map[string]any{"type": "string"},
					"concepts":  map[string]any{"type": "string"},
					"files":     map[string]any{"type": "string"},
					"dateStart": map[string]any{"type": []string{"string", "number"}},
					"dateEnd":   map[string]any{"type": []string{"string", "number"}},
					"format":    map[string]any{"type": "string", "enum": []string{"index", "full"}, "default": "index"},
				},
			},
		},
		{
			Name:        "find_related_observations",
			Description: "Find observations related to a given observation ID filtered by confidence threshold. Returns related observations sorted by confidence score. Useful for discovering relevant context.",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"id"},
				"properties": map[string]any{
					"id":             map[string]any{"type": "number", "description": "Observation ID"},
					"min_confidence": map[string]any{"type": "number", "default": 0.5, "minimum": 0.0, "maximum": 1.0, "description": "Minimum confidence threshold"},
					"limit":          map[string]any{"type": "number", "default": 20, "minimum": 1, "maximum": 100},
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
		return &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &Error{
				Code:    -32000,
				Message: "Tool error",
				Data:    err.Error(),
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

// callTool dispatches to the appropriate tool handler.
func (s *Server) callTool(ctx context.Context, name string, args json.RawMessage) (string, error) {
	// Relation discovery tool
	if name == "find_related_observations" {
		return s.handleFindRelatedObservations(ctx, args)
	}

	// Original search-based tools
	var params search.SearchParams
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	var result *search.UnifiedSearchResult
	var err error

	switch name {
	case "search":
		result, err = s.searchMgr.UnifiedSearch(ctx, params)
	case "timeline":
		result, err = s.handleTimeline(ctx, args)
	case "decisions":
		result, err = s.searchMgr.Decisions(ctx, params)
	case "changes":
		result, err = s.searchMgr.Changes(ctx, params)
	case "how_it_works":
		result, err = s.searchMgr.HowItWorks(ctx, params)
	case "find_by_concept":
		params.Type = "observations"
		result, err = s.searchMgr.UnifiedSearch(ctx, params)
	case "find_by_file":
		params.Type = "observations"
		result, err = s.searchMgr.UnifiedSearch(ctx, params)
	case "find_by_type":
		params.Type = "observations"
		result, err = s.searchMgr.UnifiedSearch(ctx, params)
	case "get_recent_context":
		result, err = s.searchMgr.UnifiedSearch(ctx, params)
	case "get_context_timeline":
		result, err = s.handleTimeline(ctx, args)
	case "get_timeline_by_query":
		result, err = s.handleTimelineByQuery(ctx, args)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}

	if err != nil {
		return "", err
	}

	output, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("marshal result: %w", err)
	}

	return string(output), nil
}

// TimelineParams represents parameters for timeline operations.
type TimelineParams struct {
	Query     string `json:"query"`
	Project   string `json:"project"`
	ObsType   string `json:"obs_type"`
	Concepts  string `json:"concepts"`
	Files     string `json:"files"`
	Format    string `json:"format"`
	AnchorID  int64  `json:"anchor_id"`
	Before    int    `json:"before"`
	After     int    `json:"after"`
	DateStart int64  `json:"dateStart"`
	DateEnd   int64  `json:"dateEnd"`
}

// handleTimeline handles timeline requests.
func (s *Server) handleTimeline(ctx context.Context, args json.RawMessage) (*search.UnifiedSearchResult, error) {
	var params TimelineParams
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid timeline params: %w", err)
	}

	if params.Before <= 0 {
		params.Before = 10
	}
	if params.After <= 0 {
		params.After = 10
	}

	// If query provided, first find anchor
	if params.Query != "" && params.AnchorID == 0 {
		searchParams := search.SearchParams{
			Query:   params.Query,
			Type:    "observations",
			Project: params.Project,
			Limit:   1,
		}
		result, err := s.searchMgr.UnifiedSearch(ctx, searchParams)
		if err != nil {
			return nil, err
		}
		if len(result.Results) > 0 {
			params.AnchorID = result.Results[0].ID
		}
	}

	if params.AnchorID == 0 {
		return &search.UnifiedSearchResult{Results: []search.SearchResult{}}, nil
	}

	// Fetch observations around anchor
	searchParams := search.SearchParams{
		Type:     "observations",
		Project:  params.Project,
		ObsType:  params.ObsType,
		Concepts: params.Concepts,
		Files:    params.Files,
		Limit:    params.Before + params.After + 1,
		Format:   params.Format,
	}

	return s.searchMgr.UnifiedSearch(ctx, searchParams)
}

// handleTimelineByQuery handles combined search + timeline requests.
func (s *Server) handleTimelineByQuery(ctx context.Context, args json.RawMessage) (*search.UnifiedSearchResult, error) {
	var params TimelineParams
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid timeline params: %w", err)
	}

	if params.Query == "" {
		return nil, fmt.Errorf("query is required")
	}

	// First search
	searchParams := search.SearchParams{
		Query:     params.Query,
		Type:      "observations",
		Project:   params.Project,
		DateStart: params.DateStart,
		DateEnd:   params.DateEnd,
		Limit:     1,
	}

	result, err := s.searchMgr.UnifiedSearch(ctx, searchParams)
	if err != nil {
		return nil, err
	}

	if len(result.Results) == 0 {
		return result, nil
	}

	// Now get timeline around that result
	params.AnchorID = result.Results[0].ID
	return s.handleTimeline(ctx, args)
}

// handleFindRelatedObservations finds observations related to a given observation ID.
func (s *Server) handleFindRelatedObservations(ctx context.Context, args json.RawMessage) (string, error) {
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

	// Use -1 as sentinel for "not provided" since 0.0 is a valid threshold
	if params.MinConfidence < 0 {
		params.MinConfidence = 0.5
	}

	if params.Limit == 0 {
		params.Limit = 20
	}
	if params.Limit > 100 {
		params.Limit = 100
	}

	// Get related observation IDs with confidence filter
	relatedIDs, err := s.relationStore.GetRelatedObservationIDs(ctx, params.ID, params.MinConfidence)
	if err != nil {
		return "", fmt.Errorf("failed to get related observations: %w", err)
	}

	if relatedIDs == nil {
		relatedIDs = []int64{}
	}

	// Limit results
	if len(relatedIDs) > params.Limit {
		relatedIDs = relatedIDs[:params.Limit]
	}

	// Fetch full observations
	observations := make([]*models.Observation, 0, len(relatedIDs))
	for _, id := range relatedIDs {
		obs, err := s.observationStore.GetObservationByID(ctx, id)
		if err != nil {
			continue // Skip errors for individual observations
		}
		if obs != nil {
			observations = append(observations, obs)
		}
	}

	response := map[string]any{
		"observations": observations,
		"count":        len(observations),
	}

	output, err := json.Marshal(response)
	if err != nil {
		return "", fmt.Errorf("marshal response: %w", err)
	}

	return string(output), nil
}

// sendResponse sends a JSON-RPC response.
func (s *Server) sendResponse(resp *Response) {
	data, err := json.Marshal(resp)
	if err != nil {
		log.Error().Err(err).Msg("Failed to marshal response")
		return
	}
	fmt.Fprintln(s.stdout, string(data))
}

// sendError sends a JSON-RPC error response.
func (s *Server) sendError(id any, code int, message string, data any) {
	resp := &Response{
		JSONRPC: "2.0",
		ID:      id,
		Error: &Error{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
	s.sendResponse(resp)
}
