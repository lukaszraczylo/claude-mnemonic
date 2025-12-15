// Package chroma provides ChromaDB vector database integration for claude-mnemonic.
package chroma

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/rs/zerolog/log"
)

// Document represents a document to store in ChromaDB.
type Document struct {
	ID       string         `json:"id"`
	Content  string         `json:"document"`
	Metadata map[string]any `json:"metadata"`
}

// QueryResult represents a search result from ChromaDB.
type QueryResult struct {
	ID       string
	Distance float64
	Metadata map[string]any
}

// Client is a ChromaDB client that communicates via MCP protocol.
type Client struct {
	collection string
	dataDir    string
	pythonVer  string
	batchSize  int

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	mu     sync.Mutex

	connected bool
	requestID int
}

// Config holds configuration for the ChromaDB client.
type Config struct {
	Project   string
	DataDir   string
	PythonVer string
	BatchSize int
}

// NewClient creates a new ChromaDB client.
func NewClient(cfg Config) (*Client, error) {
	if cfg.DataDir == "" {
		home, _ := os.UserHomeDir()
		cfg.DataDir = filepath.Join(home, ".claude-mnemonic", "vector-db")
	}
	if cfg.PythonVer == "" {
		cfg.PythonVer = "3.13"
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 100
	}

	return &Client{
		collection: fmt.Sprintf("cm__%s", cfg.Project),
		dataDir:    cfg.DataDir,
		pythonVer:  cfg.PythonVer,
		batchSize:  cfg.BatchSize,
	}, nil
}

// Connect starts the ChromaDB MCP server and establishes connection.
func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected {
		return nil
	}

	// Ensure data directory exists
	if err := os.MkdirAll(c.dataDir, 0750); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	// Start chroma-mcp server via uvx
	c.cmd = exec.CommandContext(ctx, "uvx", // #nosec G204 -- config values from internal settings
		"--python", c.pythonVer,
		"chroma-mcp",
		"--client-type", "persistent",
		"--data-dir", c.dataDir,
	)

	var err error
	c.stdin, err = c.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := c.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	c.stdout = bufio.NewReader(stdout)

	c.cmd.Stderr = os.Stderr

	if err := c.cmd.Start(); err != nil {
		return fmt.Errorf("start chroma-mcp: %w", err)
	}

	// Send initialize request
	if err := c.sendInitialize(); err != nil {
		_ = c.Close()
		return fmt.Errorf("initialize: %w", err)
	}

	c.connected = true
	log.Info().
		Str("collection", c.collection).
		Str("dataDir", c.dataDir).
		Msg("Connected to ChromaDB")

	return nil
}

// sendInitialize sends the MCP initialize request.
func (c *Client) sendInitialize() error {
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      c.nextID(),
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "claude-mnemonic",
				"version": "1.0.0",
			},
		},
	}

	if err := c.send(req); err != nil {
		return err
	}

	// Read response
	_, err := c.readResponse()
	return err
}

// EnsureCollection ensures the collection exists, creating it if needed.
func (c *Client) EnsureCollection(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Try to get collection info
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      c.nextID(),
		"method":  "tools/call",
		"params": map[string]any{
			"name": "chroma_get_collection_info",
			"arguments": map[string]any{
				"collection_name": c.collection,
			},
		},
	}

	if err := c.send(req); err != nil {
		return err
	}

	resp, err := c.readResponse()
	if err != nil {
		// Collection doesn't exist, create it
		return c.createCollection()
	}

	// Check if error in response (collection not found)
	if _, ok := resp["error"]; ok {
		return c.createCollection()
	}

	return nil
}

// createCollection creates a new collection.
func (c *Client) createCollection() error {
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      c.nextID(),
		"method":  "tools/call",
		"params": map[string]any{
			"name": "chroma_create_collection",
			"arguments": map[string]any{
				"collection_name":         c.collection,
				"embedding_function_name": "default",
			},
		},
	}

	if err := c.send(req); err != nil {
		return err
	}

	_, err := c.readResponse()
	if err != nil {
		return fmt.Errorf("create collection: %w", err)
	}

	log.Info().
		Str("collection", c.collection).
		Msg("Created ChromaDB collection")

	return nil
}

// AddDocuments adds documents to the collection in batches.
func (c *Client) AddDocuments(ctx context.Context, docs []Document) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return fmt.Errorf("not connected")
	}

	for i := 0; i < len(docs); i += c.batchSize {
		end := i + c.batchSize
		if end > len(docs) {
			end = len(docs)
		}
		batch := docs[i:end]

		// Extract fields
		documents := make([]string, len(batch))
		ids := make([]string, len(batch))
		metadatas := make([]map[string]any, len(batch))
		for j, doc := range batch {
			documents[j] = doc.Content
			ids[j] = doc.ID
			metadatas[j] = doc.Metadata
		}

		req := map[string]any{
			"jsonrpc": "2.0",
			"id":      c.nextID(),
			"method":  "tools/call",
			"params": map[string]any{
				"name": "chroma_add_documents",
				"arguments": map[string]any{
					"collection_name": c.collection,
					"documents":       documents,
					"ids":             ids,
					"metadatas":       metadatas,
				},
			},
		}

		if err := c.send(req); err != nil {
			return fmt.Errorf("send add_documents: %w", err)
		}

		if _, err := c.readResponse(); err != nil {
			return fmt.Errorf("add_documents response: %w", err)
		}

		log.Debug().
			Int("batchStart", i).
			Int("batchEnd", end).
			Int("total", len(docs)).
			Msg("Added document batch")
	}

	return nil
}

// DeleteDocuments deletes documents from the collection by their IDs.
func (c *Client) DeleteDocuments(ctx context.Context, ids []string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return fmt.Errorf("not connected")
	}

	if len(ids) == 0 {
		return nil
	}

	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      c.nextID(),
		"method":  "tools/call",
		"params": map[string]any{
			"name": "chroma_delete_documents",
			"arguments": map[string]any{
				"collection_name": c.collection,
				"ids":             ids,
			},
		},
	}

	if err := c.send(req); err != nil {
		return fmt.Errorf("send delete_documents: %w", err)
	}

	if _, err := c.readResponse(); err != nil {
		return fmt.Errorf("delete_documents response: %w", err)
	}

	log.Debug().
		Int("count", len(ids)).
		Msg("Deleted documents from ChromaDB")

	return nil
}

// Query performs a semantic search on the collection.
func (c *Client) Query(ctx context.Context, query string, limit int, where map[string]any) ([]QueryResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return nil, fmt.Errorf("not connected")
	}

	args := map[string]any{
		"collection_name": c.collection,
		"query_texts":     []string{query},
		"n_results":       limit,
		"include":         []string{"documents", "metadatas", "distances"},
	}
	if where != nil {
		args["where"] = where
	}

	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      c.nextID(),
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "chroma_query_documents",
			"arguments": args,
		},
	}

	if err := c.send(req); err != nil {
		return nil, fmt.Errorf("send query: %w", err)
	}

	resp, err := c.readResponse()
	if err != nil {
		return nil, fmt.Errorf("query response: %w", err)
	}

	return c.parseQueryResults(resp)
}

// parseQueryResults parses the query response into QueryResult structs.
func (c *Client) parseQueryResults(resp map[string]any) ([]QueryResult, error) {
	result, ok := resp["result"].(map[string]any)
	if !ok {
		return nil, nil
	}

	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		return nil, nil
	}

	first, ok := content[0].(map[string]any)
	if !ok {
		return nil, nil
	}

	text, ok := first["text"].(string)
	if !ok {
		return nil, nil
	}

	var parsed struct {
		IDs       [][]string         `json:"ids"`
		Distances [][]float64        `json:"distances"`
		Metadatas [][]map[string]any `json:"metadatas"`
	}
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return nil, err
	}

	if len(parsed.IDs) == 0 || len(parsed.IDs[0]) == 0 {
		return nil, nil
	}

	results := make([]QueryResult, len(parsed.IDs[0]))
	for i := range parsed.IDs[0] {
		results[i] = QueryResult{
			ID: parsed.IDs[0][i],
		}
		if i < len(parsed.Distances[0]) {
			results[i].Distance = parsed.Distances[0][i]
		}
		if i < len(parsed.Metadatas[0]) {
			results[i].Metadata = parsed.Metadatas[0][i]
		}
	}

	return results, nil
}

// send sends a JSON-RPC request to the MCP server.
func (c *Client) send(req map[string]any) error {
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = c.stdin.Write(data)
	return err
}

// readResponse reads a JSON-RPC response from the MCP server.
func (c *Client) readResponse() (map[string]any, error) {
	line, err := c.stdout.ReadString('\n')
	if err != nil {
		return nil, err
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		return nil, err
	}

	if errObj, ok := resp["error"]; ok {
		return nil, fmt.Errorf("MCP error: %v", errObj)
	}

	return resp, nil
}

// nextID returns the next request ID.
func (c *Client) nextID() int {
	c.requestID++
	return c.requestID
}

// IsConnected returns whether the client is currently connected to ChromaDB.
func (c *Client) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

// Close closes the connection to ChromaDB.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return nil
	}

	c.connected = false

	if c.stdin != nil {
		_ = c.stdin.Close()
	}

	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
		_ = c.cmd.Wait()
	}

	log.Info().
		Str("collection", c.collection).
		Msg("ChromaDB connection closed")

	return nil
}

// Reconnect closes the existing connection and establishes a new one.
// This is useful when the vector database directory has been deleted and recreated.
func (c *Client) Reconnect(ctx context.Context) error {
	log.Info().
		Str("collection", c.collection).
		Msg("Reconnecting to ChromaDB...")

	// Close existing connection
	if err := c.Close(); err != nil {
		log.Warn().Err(err).Msg("Error closing ChromaDB during reconnect")
	}

	// Small delay to allow cleanup
	// (ChromaDB may need a moment to release resources)
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Reconnect
	if err := c.Connect(ctx); err != nil {
		return fmt.Errorf("reconnect failed: %w", err)
	}

	// Ensure collection exists
	if err := c.EnsureCollection(ctx); err != nil {
		return fmt.Errorf("ensure collection after reconnect: %w", err)
	}

	log.Info().
		Str("collection", c.collection).
		Msg("ChromaDB reconnected successfully")

	return nil
}
