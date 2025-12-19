// Package sqlitevec provides sqlite-vec based vector database integration for claude-mnemonic.
package sqlitevec

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	"github.com/lukaszraczylo/claude-mnemonic/internal/embedding"
	"github.com/rs/zerolog/log"
)

// Client provides vector operations via sqlite-vec.
type Client struct {
	db       *sql.DB
	embedSvc *embedding.Service
	mu       sync.Mutex
}

// Config holds configuration for the client.
type Config struct {
	DB *sql.DB
}

// NewClient creates a new sqlite-vec client.
func NewClient(cfg Config, embedSvc *embedding.Service) (*Client, error) {
	if cfg.DB == nil {
		return nil, fmt.Errorf("database connection required")
	}
	if embedSvc == nil {
		return nil, fmt.Errorf("embedding service required")
	}

	return &Client{
		db:       cfg.DB,
		embedSvc: embedSvc,
	}, nil
}

// AddDocuments adds documents with their embeddings to the vector store.
func (c *Client) AddDocuments(ctx context.Context, docs []Document) error {
	if len(docs) == 0 {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Generate embeddings for all documents
	texts := make([]string, len(docs))
	for i, doc := range docs {
		texts[i] = doc.Content
	}

	embeddings, err := c.embedSvc.EmbedBatch(texts)
	if err != nil {
		return fmt.Errorf("generate embeddings: %w", err)
	}

	// Insert into vectors table with model version tracking
	const insertQuery = `
		INSERT OR REPLACE INTO vectors (doc_id, embedding, sqlite_id, doc_type, field_type, project, scope, model_version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`

	// Get current model version for tracking
	modelVersion := c.embedSvc.Version()

	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	stmt, err := tx.PrepareContext(ctx, insertQuery)
	if err != nil {
		return fmt.Errorf("prepare statement: %w", err)
	}
	defer stmt.Close()

	for i, doc := range docs {
		// Serialize embedding to blob format
		embBlob, err := sqlite_vec.SerializeFloat32(embeddings[i])
		if err != nil {
			return fmt.Errorf("serialize embedding for %s: %w", doc.ID, err)
		}

		// Extract metadata
		sqliteID, _ := doc.Metadata["sqlite_id"].(int64)
		docType, _ := doc.Metadata["doc_type"].(string)
		fieldType, _ := doc.Metadata["field_type"].(string)
		project, _ := doc.Metadata["project"].(string)
		scope, _ := doc.Metadata["scope"].(string)

		_, err = stmt.ExecContext(ctx,
			doc.ID,
			embBlob,
			sqliteID,
			docType,
			fieldType,
			project,
			scope,
			modelVersion,
		)
		if err != nil {
			return fmt.Errorf("insert document %s: %w", doc.ID, err)
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	log.Debug().Int("count", len(docs)).Str("model", modelVersion).Msg("Added documents to sqlite-vec")
	return nil
}

// DeleteDocuments removes documents by their IDs.
func (c *Client) DeleteDocuments(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Build placeholder string
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	// #nosec G201 -- Placeholders are "?" strings, actual values are parameterized via args
	query := fmt.Sprintf("DELETE FROM vectors WHERE doc_id IN (%s)",
		strings.Join(placeholders, ","))

	_, err := c.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("delete documents: %w", err)
	}

	log.Debug().Int("count", len(ids)).Msg("Deleted documents from sqlite-vec")
	return nil
}

// Query performs a vector similarity search.
func (c *Client) Query(ctx context.Context, query string, limit int, where map[string]any) ([]QueryResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Generate query embedding
	queryEmb, err := c.embedSvc.Embed(query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	// Serialize query embedding
	queryBlob, err := sqlite_vec.SerializeFloat32(queryEmb)
	if err != nil {
		return nil, fmt.Errorf("serialize query embedding: %w", err)
	}

	// Build query with filters
	// vec0 supports WHERE clauses on metadata columns
	args := []interface{}{queryBlob}

	sqlQuery := `
		SELECT
			doc_id,
			distance,
			sqlite_id,
			doc_type,
			field_type,
			project,
			scope
		FROM vectors
		WHERE embedding MATCH ?
	`

	// Add filters - these work with vec0 metadata columns
	if docType, ok := where["doc_type"].(string); ok && docType != "" {
		sqlQuery += " AND doc_type = ?"
		args = append(args, docType)
	}
	if project, ok := where["project"].(string); ok && project != "" {
		// Include project-specific OR global scope
		sqlQuery += " AND (project = ? OR scope = 'global')"
		args = append(args, project)
	}

	sqlQuery += " ORDER BY distance LIMIT ?"
	args = append(args, limit)

	rows, err := c.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("query vectors: %w", err)
	}
	defer rows.Close()

	var results []QueryResult
	for rows.Next() {
		var r QueryResult
		var sqliteID int64
		var docType, fieldType, project, scope sql.NullString

		if err := rows.Scan(&r.ID, &r.Distance, &sqliteID, &docType, &fieldType, &project, &scope); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}

		r.Similarity = DistanceToSimilarity(r.Distance)
		r.Metadata = map[string]any{
			"sqlite_id":  float64(sqliteID), // Keep as float64 for compatibility
			"doc_type":   docType.String,
			"field_type": fieldType.String,
			"project":    project.String,
			"scope":      scope.String,
		}

		results = append(results, r)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows: %w", err)
	}

	log.Debug().
		Str("query", truncateString(query, 50)).
		Int("results", len(results)).
		Msg("Vector search completed")

	return results, nil
}

// IsConnected always returns true (no external process).
func (c *Client) IsConnected() bool {
	return c.db != nil
}

// Close is a no-op (db managed externally).
func (c *Client) Close() error {
	return nil
}

// truncateString truncates a string to maxLen characters.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// Count returns the total number of vectors in the store.
func (c *Client) Count(ctx context.Context) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var count int64
	err := c.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM vectors").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count vectors: %w", err)
	}
	return count, nil
}

// ModelVersion returns the current embedding model version.
func (c *Client) ModelVersion() string {
	return c.embedSvc.Version()
}

// NeedsRebuild checks if vectors need to be rebuilt due to model version change.
// Returns true if:
// - The vectors table is empty
// - Any vectors have a different model_version than the current model
func (c *Client) NeedsRebuild(ctx context.Context) (bool, string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	currentModel := c.embedSvc.Version()

	// Check total count
	var totalCount int64
	err := c.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM vectors").Scan(&totalCount)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to count vectors for rebuild check")
		return false, ""
	}

	if totalCount == 0 {
		return true, "empty"
	}

	// Check for vectors with different model version
	var staleCount int64
	err = c.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM vectors WHERE model_version != ? OR model_version IS NULL",
		currentModel,
	).Scan(&staleCount)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to count stale vectors")
		return false, ""
	}

	if staleCount > 0 {
		return true, fmt.Sprintf("model_mismatch:%d", staleCount)
	}

	return false, ""
}

// StaleVectorInfo contains information about a vector that needs rebuilding.
type StaleVectorInfo struct {
	DocID     string
	SQLiteID  int64
	DocType   string
	FieldType string
	Project   string
	Scope     string
}

// GetStaleVectors returns doc_ids of vectors with mismatched or null model versions.
// This enables granular rebuild - only re-embedding documents that need updating.
func (c *Client) GetStaleVectors(ctx context.Context) ([]StaleVectorInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	currentModel := c.embedSvc.Version()

	query := `
		SELECT doc_id, sqlite_id, doc_type, field_type, project, scope
		FROM vectors
		WHERE model_version != ? OR model_version IS NULL
	`

	rows, err := c.db.QueryContext(ctx, query, currentModel)
	if err != nil {
		return nil, fmt.Errorf("query stale vectors: %w", err)
	}
	defer rows.Close()

	var results []StaleVectorInfo
	for rows.Next() {
		var info StaleVectorInfo
		var sqliteID sql.NullInt64
		var docType, fieldType, project, scope sql.NullString

		if err := rows.Scan(&info.DocID, &sqliteID, &docType, &fieldType, &project, &scope); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}

		info.SQLiteID = sqliteID.Int64
		info.DocType = docType.String
		info.FieldType = fieldType.String
		info.Project = project.String
		info.Scope = scope.String

		results = append(results, info)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows: %w", err)
	}

	return results, nil
}

// DeleteVectorsByDocIDs removes vectors by their doc_ids.
// Used for granular rebuild - delete stale vectors before re-adding.
func (c *Client) DeleteVectorsByDocIDs(ctx context.Context, docIDs []string) error {
	if len(docIDs) == 0 {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Build placeholder string
	placeholders := make([]string, len(docIDs))
	args := make([]interface{}, len(docIDs))
	for i, id := range docIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	// #nosec G201 -- Placeholders are "?" strings, actual values are parameterized via args
	query := fmt.Sprintf("DELETE FROM vectors WHERE doc_id IN (%s)",
		strings.Join(placeholders, ","))

	_, err := c.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("delete vectors by doc_ids: %w", err)
	}

	log.Debug().Int("count", len(docIDs)).Msg("Deleted stale vectors by doc_id")
	return nil
}
