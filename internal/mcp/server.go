// Package mcp provides the MCP (Model Context Protocol) server for claude-mnemonic.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/lukaszraczylo/claude-mnemonic/internal/db/gorm"
	"github.com/lukaszraczylo/claude-mnemonic/internal/maintenance"
	"github.com/lukaszraczylo/claude-mnemonic/internal/scoring"
	"github.com/lukaszraczylo/claude-mnemonic/internal/search"
	"github.com/lukaszraczylo/claude-mnemonic/internal/vector/sqlitevec"
	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
	"github.com/rs/zerolog/log"
)

// Server is the MCP server that exposes search tools.
// Field order optimized for memory alignment (fieldalignment).
type Server struct {
	stdin              io.Reader
	stdout             io.Writer
	searchMgr          *search.Manager
	observationStore   *gorm.ObservationStore
	patternStore       *gorm.PatternStore
	relationStore      *gorm.RelationStore
	sessionStore       *gorm.SessionStore
	vectorClient       *sqlitevec.Client
	scoreCalculator    *scoring.Calculator
	recalculator       *scoring.Recalculator
	maintenanceService *maintenance.Service
	version            string
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
	maintenanceService *maintenance.Service,
) *Server {
	return &Server{
		searchMgr:          searchMgr,
		version:            version,
		stdin:              os.Stdin,
		stdout:             os.Stdout,
		observationStore:   observationStore,
		patternStore:       patternStore,
		relationStore:      relationStore,
		sessionStore:       sessionStore,
		vectorClient:       vectorClient,
		scoreCalculator:    scoreCalculator,
		recalculator:       recalculator,
		maintenanceService: maintenanceService,
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

	// Channel to signal when scanner is done
	scanDone := make(chan error, 1)

	go func() {
		for scanner.Scan() {
			// Check for context cancellation before processing
			select {
			case <-ctx.Done():
				scanDone <- ctx.Err()
				return
			default:
			}

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
		scanDone <- scanner.Err()
	}()

	// Wait for either context cancellation or scanner completion
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-scanDone:
		if err != nil {
			return fmt.Errorf("scanner error: %w", err)
		}
		return nil
	}
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
		{
			Name:        "find_similar_observations",
			Description: "Find observations semantically similar to a query or observation. Uses vector similarity search to find related content. Useful for detecting duplicates before creating new observations.",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"query"},
				"properties": map[string]any{
					"query":          map[string]any{"type": "string", "description": "Text to find similar observations for"},
					"project":        map[string]any{"type": "string", "description": "Filter by project name"},
					"min_similarity": map[string]any{"type": "number", "default": 0.7, "minimum": 0.0, "maximum": 1.0, "description": "Minimum similarity threshold (0-1)"},
					"limit":          map[string]any{"type": "number", "default": 10, "minimum": 1, "maximum": 50},
				},
			},
		},
		{
			Name:        "get_patterns",
			Description: "Get detected patterns from observations. Patterns represent recurring themes, workflows, or practices discovered across observations.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"type":    map[string]any{"type": "string", "enum": []string{"workflow", "preference", "best_practice", "anti_pattern", "tooling"}, "description": "Filter by pattern type"},
					"project": map[string]any{"type": "string", "description": "Filter by project"},
					"query":   map[string]any{"type": "string", "description": "Search patterns by name/description"},
					"limit":   map[string]any{"type": "number", "default": 20, "minimum": 1, "maximum": 100},
				},
			},
		},
		{
			Name:        "get_memory_stats",
			Description: "Get statistics about the memory system including observation counts, vector stats, pattern counts, and search metrics.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "bulk_delete_observations",
			Description: "Delete multiple observations by their IDs. Returns count of successfully deleted observations.",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"ids"},
				"properties": map[string]any{
					"ids":            map[string]any{"type": "array", "items": map[string]any{"type": "number"}, "description": "Array of observation IDs to delete"},
					"delete_vectors": map[string]any{"type": "boolean", "default": true, "description": "Also delete associated vectors"},
				},
			},
		},
		{
			Name:        "bulk_mark_superseded",
			Description: "Mark multiple observations as superseded (stale). Useful for cleanup without permanent deletion.",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"ids"},
				"properties": map[string]any{
					"ids": map[string]any{"type": "array", "items": map[string]any{"type": "number"}, "description": "Array of observation IDs to mark as superseded"},
				},
			},
		},
		{
			Name:        "bulk_boost_observations",
			Description: "Boost or reduce the importance score of multiple observations. Positive values increase importance, negative decrease.",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"ids", "boost"},
				"properties": map[string]any{
					"ids":   map[string]any{"type": "array", "items": map[string]any{"type": "number"}, "description": "Array of observation IDs to boost"},
					"boost": map[string]any{"type": "number", "minimum": -1.0, "maximum": 1.0, "description": "Boost amount (-1.0 to 1.0)"},
				},
			},
		},
		{
			Name:        "trigger_maintenance",
			Description: "Trigger an immediate maintenance run (cleanup old observations, optimize database).",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "get_maintenance_stats",
			Description: "Get statistics about the maintenance system including last run time, cleanup counts, and configuration.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "merge_observations",
			Description: "Merge two observations into one. The target observation is kept and boosted, the source is marked as superseded. Useful for deduplication without data loss.",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"source_id", "target_id"},
				"properties": map[string]any{
					"source_id": map[string]any{"type": "number", "description": "ID of the observation to merge FROM (will be superseded)"},
					"target_id": map[string]any{"type": "number", "description": "ID of the observation to merge INTO (will be kept and boosted)"},
					"boost":     map[string]any{"type": "number", "default": 0.1, "minimum": 0, "maximum": 0.5, "description": "Score boost for the target observation (default 0.1)"},
				},
			},
		},
		{
			Name:        "get_observation",
			Description: "Get a single observation by its ID. Returns full observation details including all metadata.",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"id"},
				"properties": map[string]any{
					"id": map[string]any{"type": "number", "description": "Observation ID to retrieve"},
				},
			},
		},
		{
			Name:        "edit_observation",
			Description: "Edit an existing observation. Only provided fields will be updated, others remain unchanged. Useful for correcting errors, adding details, or updating scope.",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"id"},
				"properties": map[string]any{
					"id":             map[string]any{"type": "number", "description": "Observation ID to edit"},
					"title":          map[string]any{"type": "string", "description": "New title (optional)"},
					"subtitle":       map[string]any{"type": "string", "description": "New subtitle (optional)"},
					"narrative":      map[string]any{"type": "string", "description": "New narrative text (optional)"},
					"facts":          map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "New facts array (optional)"},
					"concepts":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "New concept tags (optional)"},
					"files_read":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "New files read list (optional)"},
					"files_modified": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "New files modified list (optional)"},
					"scope":          map[string]any{"type": "string", "enum": []string{"project", "global"}, "description": "New scope (optional)"},
				},
			},
		},
		{
			Name:        "get_observation_quality",
			Description: "Get quality metrics for an observation. Returns completeness score, usage stats, and improvement suggestions.",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"id"},
				"properties": map[string]any{
					"id": map[string]any{"type": "number", "description": "Observation ID to analyze"},
				},
			},
		},
		{
			Name:        "suggest_consolidations",
			Description: "Find observations that could be merged or consolidated. Returns groups of similar observations with merge recommendations.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"project":        map[string]any{"type": "string", "description": "Filter by project"},
					"min_similarity": map[string]any{"type": "number", "default": 0.8, "minimum": 0.5, "maximum": 1.0, "description": "Minimum similarity threshold for grouping"},
					"limit":          map[string]any{"type": "number", "default": 10, "minimum": 1, "maximum": 50, "description": "Maximum groups to return"},
				},
			},
		},
		{
			Name:        "tag_observation",
			Description: "Add or remove concept tags from an observation. Tags help with organization and filtering. Use mode 'add' to add new tags, 'remove' to remove specific tags, or 'set' to replace all tags.",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"id", "tags"},
				"properties": map[string]any{
					"id":   map[string]any{"type": "number", "description": "Observation ID to tag"},
					"tags": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Tags to add, remove, or set"},
					"mode": map[string]any{"type": "string", "enum": []string{"add", "remove", "set"}, "default": "add", "description": "Operation mode: 'add' appends tags, 'remove' removes specific tags, 'set' replaces all tags"},
				},
			},
		},
		{
			Name:        "get_observations_by_tag",
			Description: "Find all observations that have a specific concept tag. Useful for browsing by category.",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"tag"},
				"properties": map[string]any{
					"tag":     map[string]any{"type": "string", "description": "Tag/concept to search for"},
					"project": map[string]any{"type": "string", "description": "Filter by project (optional)"},
					"limit":   map[string]any{"type": "number", "default": 50, "minimum": 1, "maximum": 200, "description": "Maximum observations to return"},
				},
			},
		},
		{
			Name:        "get_temporal_trends",
			Description: "Analyze observation creation patterns over time. Returns daily counts, peak activity times, and trend insights. Useful for understanding work patterns.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"project":  map[string]any{"type": "string", "description": "Filter by project (optional)"},
					"days":     map[string]any{"type": "number", "default": 30, "minimum": 1, "maximum": 365, "description": "Number of days to analyze"},
					"group_by": map[string]any{"type": "string", "enum": []string{"day", "week", "hour_of_day"}, "default": "day", "description": "How to group the data"},
				},
			},
		},
		{
			Name:        "get_data_quality_report",
			Description: "Get a comprehensive quality assessment of observations. Shows completeness distribution, common issues, and improvement suggestions.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"project": map[string]any{"type": "string", "description": "Filter by project (optional)"},
					"limit":   map[string]any{"type": "number", "default": 100, "minimum": 10, "maximum": 500, "description": "Number of observations to analyze"},
				},
			},
		},
		{
			Name:        "batch_tag_by_pattern",
			Description: "Apply tags to observations matching a pattern. Useful for retroactive organization and categorization.",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"pattern", "tags"},
				"properties": map[string]any{
					"pattern":     map[string]any{"type": "string", "description": "Search pattern to match (searches title, narrative, facts)"},
					"tags":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Tags to add to matching observations"},
					"project":     map[string]any{"type": "string", "description": "Filter by project (optional)"},
					"dry_run":     map[string]any{"type": "boolean", "default": true, "description": "If true, only preview matches without applying tags"},
					"max_matches": map[string]any{"type": "number", "default": 100, "minimum": 1, "maximum": 500, "description": "Maximum observations to tag"},
				},
			},
		},
		{
			Name:        "explain_search_ranking",
			Description: "Debug search results by showing score breakdown for top matches. Explains why each observation ranked where it did.",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"query"},
				"properties": map[string]any{
					"query":   map[string]any{"type": "string", "description": "Search query to analyze"},
					"project": map[string]any{"type": "string", "description": "Project context for search"},
					"top_n":   map[string]any{"type": "number", "default": 5, "minimum": 1, "maximum": 20, "description": "Number of top results to explain"},
				},
			},
		},
		{
			Name:        "export_observations",
			Description: "Export observations in various formats for backup or analysis.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"format":     map[string]any{"type": "string", "enum": []string{"json", "jsonl", "markdown"}, "default": "json", "description": "Export format"},
					"project":    map[string]any{"type": "string", "description": "Filter by project (optional)"},
					"limit":      map[string]any{"type": "number", "default": 100, "minimum": 1, "maximum": 1000, "description": "Maximum observations to export"},
					"date_start": map[string]any{"type": "number", "description": "Filter by creation date (epoch milliseconds)"},
					"date_end":   map[string]any{"type": "number", "description": "Filter by creation date (epoch milliseconds)"},
					"obs_type":   map[string]any{"type": "string", "description": "Filter by observation type"},
				},
			},
		},
		{
			Name:        "check_system_health",
			Description: "Comprehensive system health check. Returns status of all subsystems (database, vectors, cache, search) with actionable diagnostics.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "analyze_search_patterns",
			Description: "Analyze search query patterns to identify common searches, missed queries, and optimization opportunities.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"days":  map[string]any{"type": "number", "default": 7, "minimum": 1, "maximum": 30, "description": "Number of days to analyze"},
					"top_n": map[string]any{"type": "number", "default": 10, "minimum": 1, "maximum": 50, "description": "Number of top patterns to return"},
				},
			},
		},
		{
			Name:        "get_observation_relationships",
			Description: "Get relationship graph for an observation. Shows how observations relate to each other (depends_on, extends, conflicts_with, supersedes). Useful for understanding dependencies and context.",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"id"},
				"properties": map[string]any{
					"id":        map[string]any{"type": "number", "description": "Observation ID to analyze relationships for"},
					"max_depth": map[string]any{"type": "number", "default": 2, "minimum": 1, "maximum": 5, "description": "How many hops to traverse (1=direct, 2=neighbors of neighbors)"},
				},
			},
		},
		{
			Name:        "get_observation_scoring_breakdown",
			Description: "Get detailed scoring breakdown for an observation. Shows how importance scores are calculated including type weight, recency decay, feedback contribution, concept boost, and retrieval frequency. Useful for understanding why observations are ranked the way they are.",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"id"},
				"properties": map[string]any{
					"id": map[string]any{"type": "number", "description": "Observation ID to get scoring breakdown for"},
				},
			},
		},
		{
			Name:        "analyze_observation_importance",
			Description: "Analyze observation importance patterns in a project. Returns statistics on feedback distribution, top-scoring observations, most-retrieved observations, and concept weights. Useful for understanding what makes observations valuable.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"project":                 map[string]any{"type": "string", "description": "Project to analyze (optional, analyzes all if omitted)"},
					"include_top_scored":      map[string]any{"type": "boolean", "default": true, "description": "Include top-scoring observations"},
					"include_most_retrieved":  map[string]any{"type": "boolean", "default": true, "description": "Include most-retrieved observations"},
					"include_concept_weights": map[string]any{"type": "boolean", "default": true, "description": "Include concept weight analysis"},
					"limit":                   map[string]any{"type": "number", "default": 10, "minimum": 1, "maximum": 50, "description": "Number of top observations to include"},
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
	// Special handlers for non-search tools
	switch name {
	case "find_related_observations":
		return s.handleFindRelatedObservations(ctx, args)
	case "find_similar_observations":
		return s.handleFindSimilarObservations(ctx, args)
	case "get_patterns":
		return s.handleGetPatterns(ctx, args)
	case "get_memory_stats":
		return s.handleGetMemoryStats(ctx)
	case "bulk_delete_observations":
		return s.handleBulkDeleteObservations(ctx, args)
	case "bulk_mark_superseded":
		return s.handleBulkMarkSuperseded(ctx, args)
	case "bulk_boost_observations":
		return s.handleBulkBoostObservations(ctx, args)
	case "trigger_maintenance":
		return s.handleTriggerMaintenance(ctx)
	case "get_maintenance_stats":
		return s.handleGetMaintenanceStats(ctx)
	case "merge_observations":
		return s.handleMergeObservations(ctx, args)
	case "get_observation":
		return s.handleGetObservation(ctx, args)
	case "edit_observation":
		return s.handleEditObservation(ctx, args)
	case "get_observation_quality":
		return s.handleGetObservationQuality(ctx, args)
	case "suggest_consolidations":
		return s.handleSuggestConsolidations(ctx, args)
	case "tag_observation":
		return s.handleTagObservation(ctx, args)
	case "get_observations_by_tag":
		return s.handleGetObservationsByTag(ctx, args)
	case "get_temporal_trends":
		return s.handleGetTemporalTrends(ctx, args)
	case "get_data_quality_report":
		return s.handleGetDataQualityReport(ctx, args)
	case "batch_tag_by_pattern":
		return s.handleBatchTagByPattern(ctx, args)
	case "explain_search_ranking":
		return s.handleExplainSearchRanking(ctx, args)
	case "export_observations":
		return s.handleExportObservations(ctx, args)
	case "check_system_health":
		return s.handleCheckSystemHealth(ctx)
	case "analyze_search_patterns":
		return s.handleAnalyzeSearchPatterns(ctx, args)
	case "get_observation_relationships":
		return s.handleGetObservationRelationships(ctx, args)
	case "get_observation_scoring_breakdown":
		return s.handleGetObservationScoringBreakdown(ctx, args)
	case "analyze_observation_importance":
		return s.handleAnalyzeObservationImportance(ctx, args)
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

	// Fetch full observations in batch (avoids N+1 query problem)
	observations, err := s.observationStore.GetObservationsByIDsPreserveOrder(ctx, relatedIDs)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to batch fetch related observations, falling back to individual fetch")
		// Fallback to individual fetch if batch fails
		observations = make([]*models.Observation, 0, len(relatedIDs))
		for _, id := range relatedIDs {
			obs, fetchErr := s.observationStore.GetObservationByID(ctx, id)
			if fetchErr == nil && obs != nil {
				observations = append(observations, obs)
			}
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

// handleFindSimilarObservations finds observations semantically similar to a query.
func (s *Server) handleFindSimilarObservations(ctx context.Context, args json.RawMessage) (string, error) {
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

	if params.MinSimilarity == 0 {
		params.MinSimilarity = 0.7
	}
	if params.Limit == 0 {
		params.Limit = 10
	}
	if params.Limit > 50 {
		params.Limit = 50
	}

	// Use vector search to find similar observations
	if s.vectorClient == nil {
		return "", fmt.Errorf("vector search not available")
	}

	where := sqlitevec.BuildWhereFilter(sqlitevec.DocTypeObservation, params.Project)
	results, err := s.vectorClient.Query(ctx, params.Query, params.Limit*2, where)
	if err != nil {
		return "", fmt.Errorf("vector search failed: %w", err)
	}

	// Filter by similarity threshold
	filtered := sqlitevec.FilterByThreshold(results, params.MinSimilarity, params.Limit)

	// Extract observation IDs and build similarity map
	obsIDs := sqlitevec.ExtractObservationIDs(filtered, params.Project)
	similarityMap := make(map[int64]float64, len(filtered))
	for _, r := range filtered {
		if sqliteID, ok := r.Metadata["sqlite_id"].(float64); ok {
			id := int64(sqliteID)
			if _, exists := similarityMap[id]; !exists {
				similarityMap[id] = r.Similarity
			}
		}
	}

	// Fetch full observations in batch (avoids N+1 query problem)
	observations, err := s.observationStore.GetObservationsByIDsPreserveOrder(ctx, obsIDs)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to batch fetch similar observations, falling back to individual fetch")
		observations = make([]*models.Observation, 0, len(obsIDs))
		for _, id := range obsIDs {
			obs, fetchErr := s.observationStore.GetObservationByID(ctx, id)
			if fetchErr == nil && obs != nil {
				observations = append(observations, obs)
			}
		}
	}

	// Build response with similarity scores
	type SimilarObservation struct {
		*models.Observation
		Similarity float64 `json:"similarity"`
	}

	similarObs := make([]SimilarObservation, 0, len(observations))
	for _, obs := range observations {
		sim := similarityMap[obs.ID]
		similarObs = append(similarObs, SimilarObservation{
			Observation: obs,
			Similarity:  sim,
		})
	}

	response := map[string]any{
		"observations":   similarObs,
		"count":          len(similarObs),
		"min_similarity": params.MinSimilarity,
	}

	output, err := json.Marshal(response)
	if err != nil {
		return "", fmt.Errorf("marshal response: %w", err)
	}

	return string(output), nil
}

// handleGetPatterns returns patterns from the pattern store.
func (s *Server) handleGetPatterns(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Type    string `json:"type"`
		Project string `json:"project"`
		Query   string `json:"query"`
		Limit   int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	if params.Limit == 0 {
		params.Limit = 20
	}
	if params.Limit > 100 {
		params.Limit = 100
	}

	var patterns []*models.Pattern
	var err error

	// Query patterns based on filters
	if params.Query != "" {
		// FTS search
		patterns, err = s.patternStore.SearchPatternsFTS(ctx, params.Query, params.Limit)
	} else if params.Type != "" {
		// Filter by type
		patterns, err = s.patternStore.GetPatternsByType(ctx, models.PatternType(params.Type), params.Limit)
	} else if params.Project != "" {
		// Filter by project
		patterns, err = s.patternStore.GetPatternsByProject(ctx, params.Project, params.Limit)
	} else {
		// Get all active patterns
		patterns, err = s.patternStore.GetActivePatterns(ctx, params.Limit)
	}

	if err != nil {
		return "", fmt.Errorf("failed to get patterns: %w", err)
	}

	response := map[string]any{
		"patterns": patterns,
		"count":    len(patterns),
	}

	output, err := json.Marshal(response)
	if err != nil {
		return "", fmt.Errorf("marshal response: %w", err)
	}

	return string(output), nil
}

// handleGetMemoryStats returns statistics about the memory system.
func (s *Server) handleGetMemoryStats(ctx context.Context) (string, error) {
	stats := make(map[string]any, 8) // Pre-allocate for expected stats keys

	// Get vector count
	if s.vectorClient != nil {
		count, err := s.vectorClient.Count(ctx)
		if err == nil {
			stats["vector_count"] = count
		}

		// Cache stats
		cacheSize, cacheMax := s.vectorClient.CacheStats()
		stats["embedding_cache"] = map[string]any{
			"size":     cacheSize,
			"max_size": cacheMax,
		}

		// Model version
		stats["embedding_model"] = s.vectorClient.ModelVersion()
	}

	// Get pattern stats
	if s.patternStore != nil {
		patternStats, err := s.patternStore.GetPatternStats(ctx)
		if err == nil && patternStats != nil {
			stats["patterns"] = map[string]any{
				"total":             patternStats.Total,
				"active":            patternStats.Active,
				"deprecated":        patternStats.Deprecated,
				"merged":            patternStats.Merged,
				"total_occurrences": patternStats.TotalOccurrences,
				"avg_confidence":    patternStats.AvgConfidence,
			}
		}
	}

	// Get search metrics
	if s.searchMgr != nil {
		searchMetrics := s.searchMgr.Metrics()
		if searchMetrics != nil {
			stats["search"] = searchMetrics.GetStats()
		}
	}

	output, err := json.Marshal(stats)
	if err != nil {
		return "", fmt.Errorf("marshal response: %w", err)
	}

	return string(output), nil
}

// handleBulkDeleteObservations deletes multiple observations by ID.
func (s *Server) handleBulkDeleteObservations(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		IDs           []int64 `json:"ids"`
		DeleteVectors bool    `json:"delete_vectors"`
	}
	params.DeleteVectors = true // default

	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	if len(params.IDs) == 0 {
		return "", fmt.Errorf("ids is required")
	}

	if len(params.IDs) > 1000 {
		return "", fmt.Errorf("maximum 1000 IDs per request")
	}

	var deleted int64
	var errors []string

	// Delete in batches
	batchSize := 100
	for i := 0; i < len(params.IDs); i += batchSize {
		end := min(i+batchSize, len(params.IDs))
		batch := params.IDs[i:end]

		for _, id := range batch {
			if err := s.observationStore.DeleteObservation(ctx, id); err != nil {
				errors = append(errors, fmt.Sprintf("id %d: %v", id, err))
				continue
			}
			deleted++

			// Delete associated vectors if requested
			if params.DeleteVectors && s.vectorClient != nil {
				_ = s.vectorClient.DeleteByObservationID(ctx, id)
			}
		}
	}

	response := map[string]any{
		"deleted": deleted,
		"total":   len(params.IDs),
	}
	if len(errors) > 0 {
		response["errors"] = errors
	}

	output, err := json.Marshal(response)
	if err != nil {
		return "", fmt.Errorf("marshal response: %w", err)
	}

	// Return error if all deletions failed (complete failure)
	if deleted == 0 && len(errors) > 0 {
		return string(output), fmt.Errorf("bulk delete failed: %d errors, first: %s", len(errors), errors[0])
	}

	return string(output), nil
}

// handleBulkMarkSuperseded marks multiple observations as superseded.
func (s *Server) handleBulkMarkSuperseded(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	if len(params.IDs) == 0 {
		return "", fmt.Errorf("ids is required")
	}

	if len(params.IDs) > 1000 {
		return "", fmt.Errorf("maximum 1000 IDs per request")
	}

	// Use batch update for efficiency (single query instead of N queries)
	updated, err := s.observationStore.MarkAsSupersededBatch(ctx, params.IDs)
	if err != nil {
		return "", fmt.Errorf("batch mark as superseded: %w", err)
	}

	response := map[string]any{
		"updated": updated,
		"total":   len(params.IDs),
	}

	output, err := json.Marshal(response)
	if err != nil {
		return "", fmt.Errorf("marshal response: %w", err)
	}

	return string(output), nil
}

// handleBulkBoostObservations boosts the importance score of multiple observations.
func (s *Server) handleBulkBoostObservations(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		IDs   []int64 `json:"ids"`
		Boost float64 `json:"boost"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	if len(params.IDs) == 0 {
		return "", fmt.Errorf("ids is required")
	}

	if len(params.IDs) > 1000 {
		return "", fmt.Errorf("maximum 1000 IDs per request")
	}

	if params.Boost < -1.0 || params.Boost > 1.0 {
		return "", fmt.Errorf("boost must be between -1.0 and 1.0")
	}

	var boosted int64
	var errors []string

	// Batch fetch all observations in one query instead of N queries
	observations, err := s.observationStore.GetObservationsByIDs(ctx, params.IDs, "", 0)
	if err != nil {
		return "", fmt.Errorf("batch fetch observations: %w", err)
	}

	// Build a map for O(1) lookup
	obsMap := make(map[int64]*models.Observation, len(observations))
	for _, obs := range observations {
		obsMap[obs.ID] = obs
	}

	// Calculate new scores and prepare batch update
	scoresToUpdate := make(map[int64]float64, len(params.IDs))
	for _, id := range params.IDs {
		obs, found := obsMap[id]
		if !found {
			errors = append(errors, fmt.Sprintf("id %d: not found", id))
			continue
		}

		// Calculate new importance score (clamp between 0 and 1)
		newScore := obs.ImportanceScore + params.Boost
		if newScore < 0 {
			newScore = 0
		}
		if newScore > 1 {
			newScore = 1
		}
		scoresToUpdate[id] = newScore
	}

	// Batch update all scores in one operation
	if len(scoresToUpdate) > 0 {
		if err := s.observationStore.UpdateImportanceScores(ctx, scoresToUpdate); err != nil {
			return "", fmt.Errorf("batch update scores: %w", err)
		}
		boosted = int64(len(scoresToUpdate))
	}

	response := map[string]any{
		"boosted":    boosted,
		"total":      len(params.IDs),
		"boost_used": params.Boost,
	}
	if len(errors) > 0 {
		response["errors"] = errors
	}

	output, err := json.Marshal(response)
	if err != nil {
		return "", fmt.Errorf("marshal response: %w", err)
	}

	return string(output), nil
}

// handleTriggerMaintenance triggers an immediate maintenance run.
func (s *Server) handleTriggerMaintenance(ctx context.Context) (string, error) {
	if s.maintenanceService == nil {
		return "", fmt.Errorf("maintenance service not available")
	}

	s.maintenanceService.RunNow(ctx)

	response := map[string]any{
		"status":  "triggered",
		"message": "Maintenance run started in background",
	}

	output, err := json.Marshal(response)
	if err != nil {
		return "", fmt.Errorf("marshal response: %w", err)
	}

	return string(output), nil
}

// handleGetMaintenanceStats returns maintenance statistics.
func (s *Server) handleGetMaintenanceStats(_ context.Context) (string, error) {
	if s.maintenanceService == nil {
		return "", fmt.Errorf("maintenance service not available")
	}

	stats := s.maintenanceService.Stats()

	output, err := json.Marshal(stats)
	if err != nil {
		return "", fmt.Errorf("marshal response: %w", err)
	}

	return string(output), nil
}

// handleMergeObservations merges two observations, keeping the target and superseding the source.
func (s *Server) handleMergeObservations(ctx context.Context, args json.RawMessage) (string, error) {
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

	if params.SourceID == params.TargetID {
		return "", fmt.Errorf("source_id and target_id cannot be the same")
	}

	// Set default boost if not provided
	if params.Boost == 0 {
		params.Boost = 0.1
	}
	if params.Boost < 0 || params.Boost > 0.5 {
		return "", fmt.Errorf("boost must be between 0 and 0.5")
	}

	// Get both observations to verify they exist
	source, err := s.observationStore.GetObservationByID(ctx, params.SourceID)
	if err != nil {
		return "", fmt.Errorf("get source observation: %w", err)
	}
	if source == nil {
		return "", fmt.Errorf("source observation %d not found", params.SourceID)
	}

	target, err := s.observationStore.GetObservationByID(ctx, params.TargetID)
	if err != nil {
		return "", fmt.Errorf("get target observation: %w", err)
	}
	if target == nil {
		return "", fmt.Errorf("target observation %d not found", params.TargetID)
	}

	// Mark source as superseded
	if err := s.observationStore.MarkAsSuperseded(ctx, params.SourceID); err != nil {
		return "", fmt.Errorf("mark source as superseded: %w", err)
	}

	// Boost target's importance score
	newScore := target.ImportanceScore + params.Boost
	if newScore > 1.0 {
		newScore = 1.0
	}
	if err := s.observationStore.UpdateImportanceScore(ctx, params.TargetID, newScore); err != nil {
		return "", fmt.Errorf("update target score: %w", err)
	}

	response := map[string]any{
		"merged":           true,
		"source_id":        params.SourceID,
		"source_title":     source.Title.String,
		"target_id":        params.TargetID,
		"target_title":     target.Title.String,
		"target_new_score": newScore,
		"target_old_score": target.ImportanceScore,
		"boost_applied":    params.Boost,
	}

	output, err := json.Marshal(response)
	if err != nil {
		return "", fmt.Errorf("marshal response: %w", err)
	}

	return string(output), nil
}

// handleGetObservation returns a single observation by ID.
func (s *Server) handleGetObservation(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	if params.ID == 0 {
		return "", fmt.Errorf("id is required")
	}

	obs, err := s.observationStore.GetObservationByID(ctx, params.ID)
	if err != nil {
		return "", fmt.Errorf("get observation: %w", err)
	}
	if obs == nil {
		return "", fmt.Errorf("observation %d not found", params.ID)
	}

	output, err := json.Marshal(obs)
	if err != nil {
		return "", fmt.Errorf("marshal observation: %w", err)
	}

	return string(output), nil
}

// handleEditObservation updates an existing observation with provided fields.
func (s *Server) handleEditObservation(ctx context.Context, args json.RawMessage) (string, error) {
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

	// Validate scope if provided
	if params.Scope != nil && *params.Scope != "project" && *params.Scope != "global" {
		return "", fmt.Errorf("scope must be 'project' or 'global'")
	}

	// Build update struct
	update := &gorm.ObservationUpdate{}
	if params.Title != nil {
		update.Title = params.Title
	}
	if params.Subtitle != nil {
		update.Subtitle = params.Subtitle
	}
	if params.Narrative != nil {
		update.Narrative = params.Narrative
	}
	if params.Facts != nil {
		update.Facts = &params.Facts
	}
	if params.Concepts != nil {
		update.Concepts = &params.Concepts
	}
	if params.FilesRead != nil {
		update.FilesRead = &params.FilesRead
	}
	if params.FilesModified != nil {
		update.FilesModified = &params.FilesModified
	}
	if params.Scope != nil {
		update.Scope = params.Scope
	}

	// Update the observation
	updatedObs, err := s.observationStore.UpdateObservation(ctx, params.ID, update)
	if err != nil {
		return "", fmt.Errorf("update observation: %w", err)
	}

	// Note: Vector resync is handled by the worker service when available
	// The MCP server doesn't have access to the embedding service

	response := map[string]any{
		"updated":       true,
		"observation":   updatedObs,
		"vector_resync": "deferred",
	}

	output, err := json.Marshal(response)
	if err != nil {
		return "", fmt.Errorf("marshal response: %w", err)
	}

	return string(output), nil
}

// handleGetObservationQuality returns quality metrics for an observation.
func (s *Server) handleGetObservationQuality(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	if params.ID == 0 {
		return "", fmt.Errorf("id is required")
	}

	obs, err := s.observationStore.GetObservationByID(ctx, params.ID)
	if err != nil {
		return "", fmt.Errorf("get observation: %w", err)
	}
	if obs == nil {
		return "", fmt.Errorf("observation %d not found", params.ID)
	}

	// Calculate completeness score
	completenessScore := 0.0
	maxScore := 5.0
	suggestions := []string{}

	// Check title (required, 1 point)
	if obs.Title.Valid && obs.Title.String != "" {
		completenessScore += 1.0
	} else {
		suggestions = append(suggestions, "Add a descriptive title")
	}

	// Check narrative (important, 1.5 points)
	if obs.Narrative.Valid && len(obs.Narrative.String) > 50 {
		completenessScore += 1.5
	} else if obs.Narrative.Valid && obs.Narrative.String != "" {
		completenessScore += 0.5
		suggestions = append(suggestions, "Expand the narrative to provide more context (aim for 50+ characters)")
	} else {
		suggestions = append(suggestions, "Add a narrative explaining the observation")
	}

	// Check facts (valuable, 1 point)
	if len(obs.Facts) >= 2 {
		completenessScore += 1.0
	} else if len(obs.Facts) == 1 {
		completenessScore += 0.5
		suggestions = append(suggestions, "Add more key facts (aim for 2+)")
	} else {
		suggestions = append(suggestions, "Add key facts to capture important details")
	}

	// Check concepts (useful, 0.75 points)
	if len(obs.Concepts) >= 2 {
		completenessScore += 0.75
	} else if len(obs.Concepts) == 1 {
		completenessScore += 0.25
		suggestions = append(suggestions, "Add more concept tags for better discoverability")
	} else {
		suggestions = append(suggestions, "Add concept tags to categorize this observation")
	}

	// Check file references (helpful, 0.75 points)
	if len(obs.FilesRead) > 0 || len(obs.FilesModified) > 0 {
		completenessScore += 0.75
	} else {
		suggestions = append(suggestions, "Consider adding file references if applicable")
	}

	// Determine quality tier
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
			"has_title":            obs.Title.Valid && obs.Title.String != "",
			"has_narrative":        obs.Narrative.Valid && obs.Narrative.String != "",
			"narrative_length":     len(obs.Narrative.String),
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

// handleSuggestConsolidations finds observations that could be merged.
func (s *Server) handleSuggestConsolidations(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Project       string  `json:"project"`
		MinSimilarity float64 `json:"min_similarity"`
		Limit         int     `json:"limit"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	// Set defaults
	if params.MinSimilarity == 0 {
		params.MinSimilarity = 0.8
	}
	if params.Limit == 0 {
		params.Limit = 10
	}
	if params.MinSimilarity < 0.5 || params.MinSimilarity > 1.0 {
		return "", fmt.Errorf("min_similarity must be between 0.5 and 1.0")
	}

	// Get recent observations to analyze
	obs, err := s.observationStore.GetRecentObservations(ctx, params.Project, 200)
	if err != nil {
		return "", fmt.Errorf("get observations: %w", err)
	}

	if len(obs) < 2 {
		response := map[string]any{
			"groups":  []any{},
			"message": "Not enough observations to analyze",
		}
		output, _ := json.Marshal(response)
		return string(output), nil
	}

	// Find similar pairs using vector search if available
	type consolidationGroup struct {
		Primary    *models.Observation   `json:"primary"`
		Reason     string                `json:"reason"`
		Similar    []*models.Observation `json:"similar"`
		Similarity float64               `json:"avg_similarity"`
	}

	groups := []consolidationGroup{}
	seen := make(map[int64]bool)

	// For each observation, find similar ones
	for _, primary := range obs {
		if seen[primary.ID] {
			continue
		}

		// Build search text from observation
		searchText := primary.Title.String
		if primary.Narrative.Valid {
			searchText += " " + primary.Narrative.String
		}

		if searchText == "" || s.vectorClient == nil {
			continue
		}

		// Query for similar observations
		where := sqlitevec.BuildWhereFilter(sqlitevec.DocTypeObservation, params.Project)
		results, err := s.vectorClient.Query(ctx, searchText, 10, where)
		if err != nil {
			continue
		}

		// Find similar observations above threshold
		similar := []*models.Observation{}
		totalSimilarity := 0.0

		for _, r := range results {
			// Extract observation ID from metadata
			sqliteID, ok := r.Metadata["sqlite_id"].(float64)
			if !ok {
				continue
			}
			obsID := int64(sqliteID)

			if obsID == primary.ID || seen[obsID] {
				continue
			}
			if r.Similarity >= params.MinSimilarity {
				// Fetch the similar observation
				simObs, err := s.observationStore.GetObservationByID(ctx, obsID)
				if err != nil || simObs == nil {
					continue
				}
				similar = append(similar, simObs)
				totalSimilarity += r.Similarity
				seen[obsID] = true
			}
		}

		if len(similar) > 0 {
			seen[primary.ID] = true
			avgSimilarity := totalSimilarity / float64(len(similar))

			// Determine consolidation reason
			reason := "Content similarity detected"
			if len(primary.Concepts) > 0 && len(similar) > 0 {
				// Check for concept overlap
				conceptMap := make(map[string]bool)
				for _, c := range primary.Concepts {
					conceptMap[c] = true
				}
				for _, sim := range similar {
					for _, c := range sim.Concepts {
						if conceptMap[c] {
							reason = "Similar content with shared concepts"
							break
						}
					}
				}
			}

			groups = append(groups, consolidationGroup{
				Primary:    primary,
				Similar:    similar,
				Similarity: avgSimilarity,
				Reason:     reason,
			})

			if len(groups) >= params.Limit {
				break
			}
		}
	}

	response := map[string]any{
		"groups":         groups,
		"total_analyzed": len(obs),
		"groups_found":   len(groups),
		"min_similarity": params.MinSimilarity,
		"recommendation": "Review each group and use merge_observations to consolidate where appropriate",
	}

	output, err := json.Marshal(response)
	if err != nil {
		return "", fmt.Errorf("marshal response: %w", err)
	}

	return string(output), nil
}

// handleTagObservation adds, removes, or sets tags on an observation.
func (s *Server) handleTagObservation(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Mode string   `json:"mode"`
		Tags []string `json:"tags"`
		ID   int64    `json:"id"`
	}
	params.Mode = "add" // default

	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	if params.ID == 0 {
		return "", fmt.Errorf("id is required")
	}
	if len(params.Tags) == 0 {
		return "", fmt.Errorf("tags is required")
	}
	if params.Mode != "add" && params.Mode != "remove" && params.Mode != "set" {
		return "", fmt.Errorf("mode must be 'add', 'remove', or 'set'")
	}

	// Get current observation
	obs, err := s.observationStore.GetObservationByID(ctx, params.ID)
	if err != nil {
		return "", fmt.Errorf("get observation: %w", err)
	}
	if obs == nil {
		return "", fmt.Errorf("observation %d not found", params.ID)
	}

	// Compute new tags
	var newTags []string
	switch params.Mode {
	case "set":
		newTags = params.Tags
	case "add":
		// Add new tags, avoiding duplicates
		tagSet := make(map[string]bool)
		for _, t := range obs.Concepts {
			tagSet[t] = true
			newTags = append(newTags, t)
		}
		for _, t := range params.Tags {
			if !tagSet[t] {
				tagSet[t] = true
				newTags = append(newTags, t)
			}
		}
	case "remove":
		// Remove specified tags
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

	// Update using existing UpdateObservation method
	update := &gorm.ObservationUpdate{
		Concepts: &newTags,
	}
	updatedObs, err := s.observationStore.UpdateObservation(ctx, params.ID, update)
	if err != nil {
		return "", fmt.Errorf("update observation: %w", err)
	}

	response := map[string]any{
		"id":           params.ID,
		"mode":         params.Mode,
		"tags_applied": params.Tags,
		"current_tags": updatedObs.Concepts,
	}

	output, err := json.Marshal(response)
	if err != nil {
		return "", fmt.Errorf("marshal response: %w", err)
	}

	return string(output), nil
}

// handleGetObservationsByTag retrieves observations with a specific concept tag.
func (s *Server) handleGetObservationsByTag(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Tag     string `json:"tag"`
		Project string `json:"project"`
		Limit   int    `json:"limit"`
	}
	params.Limit = 50 // default

	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	if params.Tag == "" {
		return "", fmt.Errorf("tag is required")
	}
	if params.Limit < 1 || params.Limit > 200 {
		params.Limit = 50
	}

	// Use search with concept filter
	searchParams := search.SearchParams{
		Query:    params.Tag,
		Type:     "observations",
		Project:  params.Project,
		Limit:    params.Limit,
		Concepts: params.Tag,
	}

	result, err := s.searchMgr.UnifiedSearch(ctx, searchParams)
	if err != nil {
		return "", fmt.Errorf("search: %w", err)
	}

	// Filter results to only include observations with the exact tag in metadata
	var filtered []search.SearchResult
	for _, r := range result.Results {
		if r.Type != "observation" {
			continue
		}
		// Check if concepts metadata contains the tag
		if concepts, ok := r.Metadata["concepts"].([]any); ok {
			for _, c := range concepts {
				if cs, ok := c.(string); ok && cs == params.Tag {
					filtered = append(filtered, r)
					break
				}
			}
		}
	}

	response := map[string]any{
		"tag":          params.Tag,
		"observations": filtered,
		"count":        len(filtered),
	}

	output, err := json.Marshal(response)
	if err != nil {
		return "", fmt.Errorf("marshal response: %w", err)
	}

	return string(output), nil
}

// handleGetTemporalTrends analyzes observation creation patterns over time.
func (s *Server) handleGetTemporalTrends(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Project string `json:"project"`
		GroupBy string `json:"group_by"`
		Days    int    `json:"days"`
	}
	params.Days = 30
	params.GroupBy = "day"

	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	if params.Days < 1 || params.Days > 365 {
		params.Days = 30
	}

	// Get observations for analysis
	obs, err := s.observationStore.GetRecentObservations(ctx, params.Project, params.Days*50) // Rough estimate
	if err != nil {
		return "", fmt.Errorf("get observations: %w", err)
	}

	// Calculate time range
	now := time.Now()
	startTime := now.AddDate(0, 0, -params.Days)
	startEpoch := startTime.UnixMilli()

	// Group observations by time bucket
	buckets := make(map[string]int)
	typeDistribution := make(map[string]int)
	conceptCounts := make(map[string]int)
	totalInRange := 0

	for _, o := range obs {
		if o.CreatedAtEpoch < startEpoch {
			continue
		}
		totalInRange++

		created := time.UnixMilli(o.CreatedAtEpoch)
		var key string
		switch params.GroupBy {
		case "week":
			year, week := created.ISOWeek()
			key = fmt.Sprintf("%d-W%02d", year, week)
		case "hour_of_day":
			key = fmt.Sprintf("%02d:00", created.Hour())
		default: // day
			key = created.Format("2006-01-02")
		}
		buckets[key]++

		// Track type distribution
		typeDistribution[string(o.Type)]++

		// Track top concepts
		for _, c := range o.Concepts {
			conceptCounts[c]++
		}
	}

	// Find peak period
	peakPeriod := ""
	peakCount := 0
	for k, v := range buckets {
		if v > peakCount {
			peakCount = v
			peakPeriod = k
		}
	}

	// Sort and get top concepts
	type conceptEntry struct {
		name  string
		count int
	}
	var topConcepts []conceptEntry
	for name, count := range conceptCounts {
		topConcepts = append(topConcepts, conceptEntry{name, count})
	}
	// Simple sort - just take top 10
	for i := 0; i < len(topConcepts) && i < 10; i++ {
		for j := i + 1; j < len(topConcepts); j++ {
			if topConcepts[j].count > topConcepts[i].count {
				topConcepts[i], topConcepts[j] = topConcepts[j], topConcepts[i]
			}
		}
	}
	if len(topConcepts) > 10 {
		topConcepts = topConcepts[:10]
	}
	topConceptsMap := make([]map[string]any, len(topConcepts))
	for i, c := range topConcepts {
		topConceptsMap[i] = map[string]any{"concept": c.name, "count": c.count}
	}

	response := map[string]any{
		"period": map[string]any{
			"start":    startTime.Format("2006-01-02"),
			"end":      now.Format("2006-01-02"),
			"days":     params.Days,
			"group_by": params.GroupBy,
		},
		"summary": map[string]any{
			"total_observations": totalInRange,
			"daily_average":      float64(totalInRange) / float64(params.Days),
			"peak_period":        peakPeriod,
			"peak_count":         peakCount,
		},
		"distribution":      buckets,
		"type_distribution": typeDistribution,
		"top_concepts":      topConceptsMap,
	}

	output, err := json.Marshal(response)
	if err != nil {
		return "", fmt.Errorf("marshal response: %w", err)
	}

	return string(output), nil
}

// handleGetDataQualityReport generates a comprehensive quality assessment.
func (s *Server) handleGetDataQualityReport(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Project string `json:"project"`
		Limit   int    `json:"limit"`
	}
	params.Limit = 100

	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	if params.Limit < 10 || params.Limit > 500 {
		params.Limit = 100
	}

	// Get observations for analysis
	obs, err := s.observationStore.GetRecentObservations(ctx, params.Project, params.Limit)
	if err != nil {
		return "", fmt.Errorf("get observations: %w", err)
	}

	if len(obs) == 0 {
		return `{"error": "no observations found", "analyzed": 0}`, nil
	}

	// Quality analysis
	qualityScores := make([]float64, 0, len(obs))
	issuesFound := make(map[string]int)
	improvements := make(map[string]int)
	scoreDistribution := map[string]int{"excellent": 0, "good": 0, "fair": 0, "poor": 0}

	for _, o := range obs {
		score := 0.0
		maxScore := 5.0

		// Check completeness
		if o.Title.Valid && o.Title.String != "" {
			score += 1.0
		} else {
			issuesFound["missing_title"]++
			improvements["add_title"]++
		}

		if o.Narrative.Valid && o.Narrative.String != "" {
			score += 1.0
		} else {
			issuesFound["missing_narrative"]++
			improvements["add_narrative"]++
		}

		if len(o.Facts) > 0 {
			score += 1.0
			if len(o.Facts) >= 3 {
				score += 0.5 // Bonus for multiple facts
			}
		} else {
			issuesFound["no_facts"]++
			improvements["add_facts"]++
		}

		if len(o.Concepts) > 0 {
			score += 1.0
		} else {
			issuesFound["no_concepts"]++
			improvements["add_concepts"]++
		}

		if len(o.FilesRead) > 0 || len(o.FilesModified) > 0 {
			score += 0.5
		}

		normalized := (score / maxScore) * 100
		qualityScores = append(qualityScores, normalized)

		// Categorize
		switch {
		case normalized >= 80:
			scoreDistribution["excellent"]++
		case normalized >= 60:
			scoreDistribution["good"]++
		case normalized >= 40:
			scoreDistribution["fair"]++
		default:
			scoreDistribution["poor"]++
		}
	}

	// Calculate average
	var avgScore float64
	for _, s := range qualityScores {
		avgScore += s
	}
	avgScore /= float64(len(qualityScores))

	// Build top issues list
	type issueEntry struct {
		name  string
		count int
	}
	var topIssues []issueEntry
	for name, count := range issuesFound {
		topIssues = append(topIssues, issueEntry{name, count})
	}
	for i := 0; i < len(topIssues) && i < 5; i++ {
		for j := i + 1; j < len(topIssues); j++ {
			if topIssues[j].count > topIssues[i].count {
				topIssues[i], topIssues[j] = topIssues[j], topIssues[i]
			}
		}
	}
	if len(topIssues) > 5 {
		topIssues = topIssues[:5]
	}

	// Convert top issues to response format
	topIssuesList := make([]map[string]any, 0, len(topIssues))
	for _, issue := range topIssues {
		topIssuesList = append(topIssuesList, map[string]any{
			"issue": issue.name,
			"count": issue.count,
		})
	}

	response := map[string]any{
		"analyzed": len(obs),
		"project":  params.Project,
		"quality_summary": map[string]any{
			"average_score": fmt.Sprintf("%.1f%%", avgScore),
			"distribution":  scoreDistribution,
		},
		"issues_found": issuesFound,
		"top_issues":   topIssuesList,
		"improvements": improvements,
		"recommendations": []string{
			"Add titles to observations for better discoverability",
			"Include narratives to provide context",
			"Add concept tags for better organization",
			"Include at least 2-3 key facts per observation",
		},
	}

	output, err := json.Marshal(response)
	if err != nil {
		return "", fmt.Errorf("marshal response: %w", err)
	}

	return string(output), nil
}

// handleBatchTagByPattern applies tags to observations matching a pattern.
func (s *Server) handleBatchTagByPattern(ctx context.Context, args json.RawMessage) (string, error) {
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
	if params.MaxMatches < 1 || params.MaxMatches > 500 {
		params.MaxMatches = 100
	}

	// Search for matching observations using the pattern
	searchParams := search.SearchParams{
		Query:   params.Pattern,
		Type:    "observations",
		Project: params.Project,
		Limit:   params.MaxMatches,
	}

	result, err := s.searchMgr.UnifiedSearch(ctx, searchParams)
	if err != nil {
		return "", fmt.Errorf("search: %w", err)
	}

	// Collect matching observation IDs
	var matches []map[string]any
	var taggedCount int

	for _, r := range result.Results {
		if r.Type != "observation" {
			continue
		}

		match := map[string]any{
			"id":    r.ID,
			"title": r.Title,
			"score": r.Score,
		}
		matches = append(matches, match)

		// Apply tags if not dry run
		if !params.DryRun {
			obs, err := s.observationStore.GetObservationByID(ctx, r.ID)
			if err != nil || obs == nil {
				continue
			}

			// Merge existing tags with new tags (avoid duplicates)
			tagSet := make(map[string]bool)
			newTags := make([]string, 0, len(obs.Concepts)+len(params.Tags))
			for _, t := range obs.Concepts {
				tagSet[t] = true
				newTags = append(newTags, t)
			}
			for _, t := range params.Tags {
				if !tagSet[t] {
					tagSet[t] = true
					newTags = append(newTags, t)
				}
			}

			update := &gorm.ObservationUpdate{
				Concepts: &newTags,
			}
			_, err = s.observationStore.UpdateObservation(ctx, r.ID, update)
			if err == nil {
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

// handleExplainSearchRanking explains why each observation ranked where it did in search results.
func (s *Server) handleExplainSearchRanking(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Query   string `json:"query"`
		Project string `json:"project"`
		TopN    int    `json:"top_n"`
	}
	params.TopN = 5 // default

	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	if params.Query == "" {
		return "", fmt.Errorf("query is required")
	}
	if params.TopN < 1 || params.TopN > 20 {
		params.TopN = 5
	}

	// Perform search to get results
	searchParams := search.SearchParams{
		Query:   params.Query,
		Type:    "observations",
		Project: params.Project,
		Limit:   params.TopN,
		OrderBy: "relevance",
	}

	result, err := s.searchMgr.UnifiedSearch(ctx, searchParams)
	if err != nil {
		return "", fmt.Errorf("search: %w", err)
	}

	// Build detailed explanations for each result
	type RankExplanation struct {
		ScoreBreakdown map[string]float64 `json:"score_breakdown"`
		Metadata       map[string]any     `json:"metadata,omitempty"`
		Title          string             `json:"title"`
		Type           string             `json:"type"`
		MatchedFields  []string           `json:"matched_fields"`
		Rank           int                `json:"rank"`
		ID             int64              `json:"id"`
		Score          float64            `json:"score"`
	}

	explanations := make([]RankExplanation, 0, len(result.Results))
	for i, r := range result.Results {
		exp := RankExplanation{
			Rank:     i + 1,
			ID:       r.ID,
			Title:    r.Title,
			Type:     r.Type,
			Score:    r.Score,
			Metadata: r.Metadata,
		}

		// Build score breakdown from available metadata
		exp.ScoreBreakdown = make(map[string]float64)
		if vs, ok := r.Metadata["vector_score"].(float64); ok {
			exp.ScoreBreakdown["vector_similarity"] = vs
		}
		if is, ok := r.Metadata["importance_score"].(float64); ok {
			exp.ScoreBreakdown["importance"] = is
		}
		if ts, ok := r.Metadata["text_score"].(float64); ok {
			exp.ScoreBreakdown["text_match"] = ts
		}
		if rs, ok := r.Metadata["recency_score"].(float64); ok {
			exp.ScoreBreakdown["recency"] = rs
		}
		// Add base score estimate if breakdown is incomplete
		if len(exp.ScoreBreakdown) == 0 {
			exp.ScoreBreakdown["combined_score"] = r.Score
		}

		// Determine matched fields
		exp.MatchedFields = []string{}
		if r.Metadata["field_type"] != nil {
			if ft, ok := r.Metadata["field_type"].(string); ok && ft != "" {
				exp.MatchedFields = append(exp.MatchedFields, ft)
			}
		}

		explanations = append(explanations, exp)
	}

	response := map[string]any{
		"query":        params.Query,
		"project":      params.Project,
		"result_count": len(explanations),
		"explanations": explanations,
		"tips": []string{
			"Higher vector_similarity indicates better semantic match with query",
			"Importance score reflects user feedback and retrieval history",
			"Recency boosts newer observations slightly",
			"Use tag_observation to boost important observations",
		},
	}

	output, err := json.Marshal(response)
	if err != nil {
		return "", fmt.Errorf("marshal response: %w", err)
	}

	return string(output), nil
}

// handleExportObservations exports observations in various formats.
func (s *Server) handleExportObservations(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Format    string `json:"format"`
		Project   string `json:"project"`
		ObsType   string `json:"obs_type"`
		Limit     int    `json:"limit"`
		DateStart int64  `json:"date_start"`
		DateEnd   int64  `json:"date_end"`
	}
	params.Format = "json"
	params.Limit = 100

	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	if params.Limit < 1 || params.Limit > 1000 {
		params.Limit = 100
	}

	// Build search params to fetch observations
	searchParams := search.SearchParams{
		Type:      "observations",
		Project:   params.Project,
		Limit:     params.Limit,
		OrderBy:   "date_desc",
		DateStart: params.DateStart,
		DateEnd:   params.DateEnd,
		ObsType:   params.ObsType,
	}

	result, err := s.searchMgr.UnifiedSearch(ctx, searchParams)
	if err != nil {
		return "", fmt.Errorf("search: %w", err)
	}

	// Fetch full observation data for export
	ids := make([]int64, 0, len(result.Results))
	for _, r := range result.Results {
		if r.Type == "observation" {
			ids = append(ids, r.ID)
		}
	}

	observations, err := s.observationStore.GetObservationsByIDs(ctx, ids, "", 0)
	if err != nil {
		return "", fmt.Errorf("get observations: %w", err)
	}

	// Format output based on requested format
	var output string
	switch params.Format {
	case "jsonl":
		// JSON Lines format - one JSON object per line
		var lines []string
		for _, obs := range observations {
			line, err := json.Marshal(obs)
			if err != nil {
				continue
			}
			lines = append(lines, string(line))
		}
		// Use proper JSON marshaling to avoid injection issues
		jsonlOutput := struct {
			Format string `json:"format"`
			Data   string `json:"data"`
			Count  int    `json:"count"`
		}{
			Format: "jsonl",
			Count:  len(observations),
			Data:   strings.Join(lines, "\n"),
		}
		outputBytes, err := json.Marshal(jsonlOutput)
		if err != nil {
			return "", fmt.Errorf("marshal jsonl output: %w", err)
		}
		output = string(outputBytes)

	case "markdown":
		// Markdown format for human reading
		var md strings.Builder
		md.WriteString("# Observations Export\n\n")
		md.WriteString(fmt.Sprintf("Total: %d observations\n\n", len(observations)))
		md.WriteString("---\n\n")

		for _, obs := range observations {
			title := ""
			if obs.Title.Valid {
				title = obs.Title.String
			}
			md.WriteString(fmt.Sprintf("## [%s] %s\n\n", obs.Type, title))
			if obs.Subtitle.Valid && obs.Subtitle.String != "" {
				md.WriteString(fmt.Sprintf("*%s*\n\n", obs.Subtitle.String))
			}
			if obs.Narrative.Valid && obs.Narrative.String != "" {
				md.WriteString(fmt.Sprintf("%s\n\n", obs.Narrative.String))
			}
			if len(obs.Facts) > 0 {
				md.WriteString("### Key Facts\n")
				for _, fact := range obs.Facts {
					md.WriteString(fmt.Sprintf("- %s\n", fact))
				}
				md.WriteString("\n")
			}
			if len(obs.Concepts) > 0 {
				md.WriteString(fmt.Sprintf("**Tags:** %s\n\n", strings.Join(obs.Concepts, ", ")))
			}
			md.WriteString(fmt.Sprintf("**ID:** %d | **Created:** %s | **Importance:** %.2f\n\n",
				obs.ID, obs.CreatedAt, obs.ImportanceScore))
			md.WriteString("---\n\n")
		}

		// Wrap markdown in JSON response
		response := map[string]any{
			"format": "markdown",
			"count":  len(observations),
			"data":   md.String(),
		}
		outputBytes, err := json.Marshal(response)
		if err != nil {
			return "", fmt.Errorf("marshal response: %w", err)
		}
		output = string(outputBytes)

	default: // json
		response := map[string]any{
			"format":       "json",
			"count":        len(observations),
			"observations": observations,
		}
		outputBytes, err := json.Marshal(response)
		if err != nil {
			return "", fmt.Errorf("marshal response: %w", err)
		}
		output = string(outputBytes)
	}

	return output, nil
}

// handleCheckSystemHealth performs comprehensive system health checks.
func (s *Server) handleCheckSystemHealth(ctx context.Context) (string, error) {
	type SubsystemHealth struct {
		Status   string         `json:"status"` // "healthy", "degraded", "unhealthy"
		Message  string         `json:"message,omitempty"`
		Metrics  map[string]any `json:"metrics,omitempty"`
		Warnings []string       `json:"warnings,omitempty"`
	}

	type HealthReport struct {
		Timestamp     time.Time                   `json:"timestamp"`
		Subsystems    map[string]*SubsystemHealth `json:"subsystems"`
		OverallStatus string                      `json:"overall_status"`
		Actions       []string                    `json:"recommended_actions,omitempty"`
		HealthScore   int                         `json:"health_score"`
	}

	report := &HealthReport{
		OverallStatus: "healthy",
		HealthScore:   100,
		Timestamp:     time.Now(),
		Subsystems:    make(map[string]*SubsystemHealth),
		Actions:       []string{},
	}

	// Check database health
	dbHealth := &SubsystemHealth{
		Status:  "healthy",
		Metrics: make(map[string]any),
	}
	if s.observationStore != nil {
		// Count observations
		count, err := s.observationStore.GetObservationCount(ctx, "")
		if err != nil {
			dbHealth.Status = "unhealthy"
			dbHealth.Message = "Database query failed: " + err.Error()
			report.HealthScore -= 30
		} else {
			dbHealth.Metrics["total_observations"] = count
			dbHealth.Message = "Database operational"
		}

		// Check for recent activity
		recent, err := s.observationStore.GetAllRecentObservations(ctx, 1)
		if err == nil && len(recent) > 0 {
			dbHealth.Metrics["last_observation"] = recent[0].CreatedAt
			// Check epoch for staleness warning
			if recent[0].CreatedAtEpoch > 0 {
				lastActivityTime := time.UnixMilli(recent[0].CreatedAtEpoch)
				if time.Since(lastActivityTime) > 7*24*time.Hour {
					dbHealth.Warnings = append(dbHealth.Warnings, "No observations in the last 7 days")
				}
			}
		}
	} else {
		dbHealth.Status = "unhealthy"
		dbHealth.Message = "Observation store not initialized"
		report.HealthScore -= 50
	}
	report.Subsystems["database"] = dbHealth

	// Check vector store health
	vectorHealth := &SubsystemHealth{
		Status:  "healthy",
		Metrics: make(map[string]any),
	}
	if s.vectorClient != nil {
		stats, err := s.vectorClient.GetHealthStats(ctx)
		if err != nil {
			vectorHealth.Status = "degraded"
			vectorHealth.Message = "Could not get vector stats: " + err.Error()
			report.HealthScore -= 15
		} else {
			vectorHealth.Metrics["total_vectors"] = stats.TotalVectors
			vectorHealth.Metrics["stale_vectors"] = stats.StaleVectors
			vectorHealth.Metrics["current_model"] = stats.CurrentModel
			vectorHealth.Metrics["needs_rebuild"] = stats.NeedsRebuild

			if stats.NeedsRebuild {
				vectorHealth.Status = "degraded"
				vectorHealth.Warnings = append(vectorHealth.Warnings, "Vector rebuild recommended: "+stats.RebuildReason)
				report.Actions = append(report.Actions, "Run vector rebuild to update embeddings")
				report.HealthScore -= 10
			}

			// Check stale ratio
			if stats.TotalVectors > 0 {
				staleRatio := float64(stats.StaleVectors) / float64(stats.TotalVectors)
				if staleRatio > 0.2 {
					vectorHealth.Warnings = append(vectorHealth.Warnings,
						fmt.Sprintf("%.1f%% of vectors are stale", staleRatio*100))
					report.HealthScore -= 5
				}
			}
		}

		// Check cache performance
		cacheStats := s.vectorClient.GetCacheStats()
		vectorHealth.Metrics["cache_hit_rate"] = fmt.Sprintf("%.1f%%", cacheStats.HitRate())
		vectorHealth.Metrics["embedding_hits"] = cacheStats.EmbeddingHits
		vectorHealth.Metrics["embedding_misses"] = cacheStats.EmbeddingMisses
		vectorHealth.Metrics["result_hits"] = cacheStats.ResultHits
		vectorHealth.Metrics["result_misses"] = cacheStats.ResultMisses

		if cacheStats.HitRate() < 20 && (cacheStats.EmbeddingHits+cacheStats.EmbeddingMisses) > 100 {
			vectorHealth.Warnings = append(vectorHealth.Warnings, "Low cache hit rate - consider cache tuning")
		}
	} else {
		vectorHealth.Status = "unhealthy"
		vectorHealth.Message = "Vector client not initialized"
		report.HealthScore -= 30
	}
	report.Subsystems["vectors"] = vectorHealth

	// Check pattern detection health
	patternHealth := &SubsystemHealth{
		Status:  "healthy",
		Metrics: make(map[string]any),
	}
	if s.patternStore != nil {
		patterns, err := s.patternStore.GetActivePatterns(ctx, 100)
		if err != nil {
			patternHealth.Status = "degraded"
			patternHealth.Message = "Could not query patterns: " + err.Error()
		} else {
			patternHealth.Metrics["total_patterns"] = len(patterns)

			// Count by type
			typeCounts := make(map[string]int)
			for _, p := range patterns {
				typeCounts[string(p.Type)]++
			}
			patternHealth.Metrics["patterns_by_type"] = typeCounts
		}
	}
	report.Subsystems["patterns"] = patternHealth

	// Check session store health
	sessionHealth := &SubsystemHealth{
		Status:  "healthy",
		Metrics: make(map[string]any),
	}
	if s.sessionStore != nil {
		sessionsToday, err := s.sessionStore.GetSessionsToday(ctx)
		if err != nil {
			sessionHealth.Status = "degraded"
			sessionHealth.Message = "Could not query sessions: " + err.Error()
		} else {
			sessionHealth.Metrics["sessions_today"] = sessionsToday
		}
	}
	report.Subsystems["sessions"] = sessionHealth

	// Determine overall status
	unhealthyCount := 0
	degradedCount := 0
	for _, sub := range report.Subsystems {
		switch sub.Status {
		case "unhealthy":
			unhealthyCount++
		case "degraded":
			degradedCount++
		}
	}

	if unhealthyCount > 0 {
		report.OverallStatus = "unhealthy"
	} else if degradedCount > 0 {
		report.OverallStatus = "degraded"
	}

	// Cap health score
	if report.HealthScore < 0 {
		report.HealthScore = 0
	}

	// Add recommended actions based on issues
	if report.HealthScore < 70 {
		report.Actions = append(report.Actions, "System needs attention - check subsystem details")
	}

	output, err := json.Marshal(report)
	if err != nil {
		return "", fmt.Errorf("marshal health report: %w", err)
	}
	return string(output), nil
}

// handleAnalyzeSearchPatterns analyzes search query patterns.
func (s *Server) handleAnalyzeSearchPatterns(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Days int `json:"days"`
		TopN int `json:"top_n"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid params: %w", err)
	}

	if params.Days <= 0 {
		params.Days = 7
	}
	if params.TopN <= 0 {
		params.TopN = 10
	}

	type QueryPattern struct {
		Query       string  `json:"query"`
		LastUsed    string  `json:"last_used"`
		Count       int     `json:"count"`
		AvgResults  float64 `json:"avg_results"`
		ZeroResults int     `json:"zero_result_count"`
	}

	type PatternAnalysis struct {
		Period            string         `json:"period"`
		TopQueries        []QueryPattern `json:"top_queries"`
		ZeroResultQueries []string       `json:"zero_result_queries,omitempty"`
		Insights          []string       `json:"insights,omitempty"`
		TotalSearches     int            `json:"total_searches"`
		UniqueQueries     int            `json:"unique_queries"`
	}

	analysis := &PatternAnalysis{
		Period:            fmt.Sprintf("Last %d days", params.Days),
		TopQueries:        []QueryPattern{},
		ZeroResultQueries: []string{},
		Insights:          []string{},
	}

	// Get search stats from the search manager if available
	if s.searchMgr != nil {
		metrics := s.searchMgr.Metrics()
		if metrics != nil {
			stats := metrics.GetStats()
			if totalSearches, ok := stats["total_searches"].(int); ok && totalSearches > 0 {
				analysis.TotalSearches = totalSearches
				analysis.Insights = append(analysis.Insights,
					fmt.Sprintf("Total searches: %d", totalSearches))
			}
			if avgLatency, ok := stats["avg_latency_ms"].(float64); ok {
				analysis.Insights = append(analysis.Insights,
					fmt.Sprintf("Average search latency: %.2fms", avgLatency))
			}
		}

		// Get cache stats
		cacheStats := s.searchMgr.CacheStats()
		if hitRate, ok := cacheStats["hit_rate"].(float64); ok {
			analysis.Insights = append(analysis.Insights,
				fmt.Sprintf("Cache hit rate: %.1f%%", hitRate*100))
		}
	}

	// Analyze observation patterns to suggest search improvements
	if s.observationStore != nil {
		// Get recent observations to understand content patterns
		observations, err := s.observationStore.GetAllRecentObservations(ctx, 100)
		if err == nil {
			analysis.UniqueQueries = len(observations)

			// Analyze observation types
			typeCounts := make(map[string]int)
			for _, obs := range observations {
				typeCounts[string(obs.Type)]++
			}

			// Find most common types
			mostCommon := ""
			maxCount := 0
			for t, c := range typeCounts {
				if c > maxCount {
					mostCommon = t
					maxCount = c
				}
			}
			if mostCommon != "" {
				analysis.Insights = append(analysis.Insights,
					fmt.Sprintf("Most common observation type: %s (%d occurrences)", mostCommon, maxCount))
			}

			// Check for concept coverage
			conceptCounts := make(map[string]int)
			for _, obs := range observations {
				for _, c := range obs.Concepts {
					conceptCounts[c]++
				}
			}
			if len(conceptCounts) > 0 {
				analysis.Insights = append(analysis.Insights,
					fmt.Sprintf("%d unique concepts across %d observations", len(conceptCounts), len(observations)))
			}
		}
	}

	// Add general recommendations
	if len(analysis.Insights) == 0 {
		analysis.Insights = append(analysis.Insights, "Insufficient data for pattern analysis")
	}

	output, err := json.Marshal(analysis)
	if err != nil {
		return "", fmt.Errorf("marshal analysis: %w", err)
	}
	return string(output), nil
}

// handleGetObservationRelationships returns the relationship graph for an observation.
func (s *Server) handleGetObservationRelationships(ctx context.Context, args json.RawMessage) (string, error) {
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
	if params.MaxDepth <= 0 {
		params.MaxDepth = 2
	}
	if params.MaxDepth > 5 {
		params.MaxDepth = 5
	}

	if s.relationStore == nil {
		return "", fmt.Errorf("relation store not available")
	}

	// Get the relationship graph
	graph, err := s.relationStore.GetRelationGraph(ctx, params.ID, params.MaxDepth)
	if err != nil {
		return "", fmt.Errorf("get relation graph: %w", err)
	}

	// Build response with additional context
	type RelationInfo struct {
		Type        string  `json:"type"`
		SourceTitle string  `json:"source_title,omitempty"`
		TargetTitle string  `json:"target_title,omitempty"`
		SourceType  string  `json:"source_type,omitempty"`
		TargetType  string  `json:"target_type,omitempty"`
		ID          int64   `json:"id"`
		SourceID    int64   `json:"source_id"`
		TargetID    int64   `json:"target_id"`
		Confidence  float64 `json:"confidence"`
	}

	type GraphResponse struct {
		Relations      []RelationInfo `json:"relations"`
		UniqueNodes    []int64        `json:"unique_nodes"`
		CenterID       int64          `json:"center_id"`
		MaxDepth       int            `json:"max_depth"`
		TotalRelations int            `json:"total_relations"`
	}

	// Collect unique node IDs
	nodeSet := make(map[int64]bool)
	nodeSet[params.ID] = true

	relations := make([]RelationInfo, 0, len(graph.Relations))
	for _, r := range graph.Relations {
		nodeSet[r.Relation.SourceID] = true
		nodeSet[r.Relation.TargetID] = true

		relations = append(relations, RelationInfo{
			ID:          r.Relation.ID,
			SourceID:    r.Relation.SourceID,
			TargetID:    r.Relation.TargetID,
			Type:        string(r.Relation.RelationType),
			Confidence:  r.Relation.Confidence,
			SourceTitle: r.SourceTitle,
			TargetTitle: r.TargetTitle,
			SourceType:  string(r.SourceType),
			TargetType:  string(r.TargetType),
		})
	}

	// Convert node set to slice
	nodes := make([]int64, 0, len(nodeSet))
	for id := range nodeSet {
		nodes = append(nodes, id)
	}

	response := GraphResponse{
		CenterID:       params.ID,
		MaxDepth:       params.MaxDepth,
		TotalRelations: len(relations),
		Relations:      relations,
		UniqueNodes:    nodes,
	}

	output, err := json.Marshal(response)
	if err != nil {
		return "", fmt.Errorf("marshal response: %w", err)
	}
	return string(output), nil
}

// handleGetObservationScoringBreakdown returns detailed scoring breakdown for an observation.
func (s *Server) handleGetObservationScoringBreakdown(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	if params.ID <= 0 {
		return "", fmt.Errorf("id is required and must be positive")
	}

	// Get the observation
	obs, err := s.observationStore.GetObservationByID(ctx, params.ID)
	if err != nil {
		return "", fmt.Errorf("get observation: %w", err)
	}
	if obs == nil {
		return "", fmt.Errorf("observation not found: %d", params.ID)
	}

	// Calculate scoring components
	if s.scoreCalculator == nil {
		return "", fmt.Errorf("score calculator not initialized")
	}

	components := s.scoreCalculator.CalculateComponents(obs, time.Now())

	// Build response with observation context
	response := map[string]any{
		"observation": map[string]any{
			"id":         obs.ID,
			"title":      obs.Title.String,
			"type":       string(obs.Type),
			"project":    obs.Project,
			"created_at": obs.CreatedAtEpoch,
		},
		"scoring": map[string]any{
			"final_score":       components.FinalScore,
			"type_weight":       components.TypeWeight,
			"recency_decay":     components.RecencyDecay,
			"core_score":        components.CoreScore,
			"feedback_contrib":  components.FeedbackContrib,
			"concept_contrib":   components.ConceptContrib,
			"retrieval_contrib": components.RetrievalContrib,
			"age_days":          components.AgeDays,
		},
		"explanation": map[string]any{
			"type_impact":      fmt.Sprintf("Observation type '%s' has weight %.2f", obs.Type, components.TypeWeight),
			"recency_impact":   fmt.Sprintf("%.1f days old, decay factor %.2f", components.AgeDays, components.RecencyDecay),
			"feedback_impact":  fmt.Sprintf("User feedback contributes %.2f to score", components.FeedbackContrib),
			"concept_impact":   fmt.Sprintf("Concept tags contribute %.2f to score", components.ConceptContrib),
			"retrieval_impact": fmt.Sprintf("Retrieval frequency contributes %.2f to score", components.RetrievalContrib),
		},
	}

	output, err := json.Marshal(response)
	if err != nil {
		return "", fmt.Errorf("marshal response: %w", err)
	}
	return string(output), nil
}

// handleAnalyzeObservationImportance returns importance analysis for a project's observations.
func (s *Server) handleAnalyzeObservationImportance(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		IncludeTopScored      *bool  `json:"include_top_scored"`
		IncludeMostRetrieved  *bool  `json:"include_most_retrieved"`
		IncludeConceptWeights *bool  `json:"include_concept_weights"`
		Project               string `json:"project"`
		Limit                 int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	// Set defaults
	if params.Limit <= 0 {
		params.Limit = 10
	}
	if params.Limit > 50 {
		params.Limit = 50
	}
	includeTopScored := params.IncludeTopScored == nil || *params.IncludeTopScored
	includeMostRetrieved := params.IncludeMostRetrieved == nil || *params.IncludeMostRetrieved
	includeConceptWeights := params.IncludeConceptWeights == nil || *params.IncludeConceptWeights

	response := make(map[string]any)
	response["project"] = params.Project
	if params.Project == "" {
		response["project"] = "(all projects)"
	}

	// Get feedback statistics
	stats, err := s.observationStore.GetObservationFeedbackStats(ctx, params.Project)
	if err != nil {
		return "", fmt.Errorf("get feedback stats: %w", err)
	}
	response["feedback_stats"] = stats

	// Get top-scoring observations
	if includeTopScored {
		topScored, err := s.observationStore.GetTopScoringObservations(ctx, params.Project, params.Limit)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to get top-scoring observations")
		} else {
			topScoredSummary := make([]map[string]any, 0, len(topScored))
			for _, obs := range topScored {
				topScoredSummary = append(topScoredSummary, map[string]any{
					"id":               obs.ID,
					"title":            obs.Title.String,
					"type":             string(obs.Type),
					"importance_score": obs.ImportanceScore,
				})
			}
			response["top_scoring_observations"] = topScoredSummary
		}
	}

	// Get most-retrieved observations
	if includeMostRetrieved {
		mostRetrieved, err := s.observationStore.GetMostRetrievedObservations(ctx, params.Project, params.Limit)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to get most-retrieved observations")
		} else {
			mostRetrievedSummary := make([]map[string]any, 0, len(mostRetrieved))
			for _, obs := range mostRetrieved {
				mostRetrievedSummary = append(mostRetrievedSummary, map[string]any{
					"id":              obs.ID,
					"title":           obs.Title.String,
					"type":            string(obs.Type),
					"retrieval_count": obs.RetrievalCount,
				})
			}
			response["most_retrieved_observations"] = mostRetrievedSummary
		}
	}

	// Get concept weights
	if includeConceptWeights {
		conceptWeights, err := s.observationStore.GetConceptWeights(ctx)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to get concept weights")
		} else if len(conceptWeights) > 0 {
			response["concept_weights"] = conceptWeights
		}
	}

	// Generate insights
	insights := []string{}
	if stats != nil {
		if stats.Positive > 0 {
			insights = append(insights, fmt.Sprintf("%d observations marked as valuable (positive feedback)", stats.Positive))
		}
		if stats.Negative > 0 {
			insights = append(insights, fmt.Sprintf("%d observations marked as not helpful (negative feedback)", stats.Negative))
		}
		if stats.AvgScore > 0 {
			insights = append(insights, fmt.Sprintf("Average importance score: %.2f", stats.AvgScore))
		}
		if stats.AvgRetrieval > 0 {
			insights = append(insights, fmt.Sprintf("Average retrieval count: %.1f", stats.AvgRetrieval))
		}
	}
	if len(insights) > 0 {
		response["insights"] = insights
	}

	output, err := json.Marshal(response)
	if err != nil {
		return "", fmt.Errorf("marshal response: %w", err)
	}
	return string(output), nil
}
