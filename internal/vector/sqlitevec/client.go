// Package sqlitevec provides sqlite-vec based vector database integration for claude-mnemonic.
package sqlitevec

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	"github.com/lukaszraczylo/claude-mnemonic/internal/embedding"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/singleflight"
)

// embeddingCacheEntry stores a cached embedding with its timestamp.
type embeddingCacheEntry struct {
	embedding []float32
	timestamp int64 // Unix nano
}

// resultCacheEntry stores cached query results.
type resultCacheEntry struct {
	queryHash string
	results   []QueryResult
	timestamp int64
}

// Client provides vector operations via sqlite-vec.
type Client struct {
	embeddingGroup     singleflight.Group
	resultCache        map[string]resultCacheEntry
	db                 *sql.DB
	embedSvc           *embedding.Service
	queryCache         map[string]embeddingCacheEntry
	stopCleanup        chan struct{}
	stats              CacheStats
	cleanupWg          sync.WaitGroup
	resultCacheTTLNano int64
	cacheTTLNano       int64
	resultCacheMaxSize int
	cacheMaxSize       int
	resultCacheMu      sync.RWMutex
	queryCacheMu       sync.RWMutex
	readMu             sync.RWMutex
	writeMu            sync.Mutex
}

// CacheStats tracks cache performance metrics using atomic counters for lock-free updates.
type CacheStats struct {
	embeddingHits      atomic.Int64
	embeddingMisses    atomic.Int64
	resultHits         atomic.Int64
	resultMisses       atomic.Int64
	embeddingEvictions atomic.Int64
	resultEvictions    atomic.Int64
}

// CacheStatsSnapshot is the exported version of CacheStats for JSON marshaling.
type CacheStatsSnapshot struct {
	EmbeddingHits      int64 `json:"embedding_hits"`
	EmbeddingMisses    int64 `json:"embedding_misses"`
	ResultHits         int64 `json:"result_hits"`
	ResultMisses       int64 `json:"result_misses"`
	EmbeddingEvictions int64 `json:"embedding_evictions"`
	ResultEvictions    int64 `json:"result_evictions"`
}

// HitRate returns the cache hit rate as a percentage.
func (s CacheStatsSnapshot) HitRate() float64 {
	total := s.EmbeddingHits + s.EmbeddingMisses + s.ResultHits + s.ResultMisses
	if total == 0 {
		return 0
	}
	hits := s.EmbeddingHits + s.ResultHits
	return float64(hits) / float64(total) * 100
}

// HitRate returns the cache hit rate as a percentage.
func (s *CacheStats) HitRate() float64 {
	embHits := s.embeddingHits.Load()
	embMisses := s.embeddingMisses.Load()
	resHits := s.resultHits.Load()
	resMisses := s.resultMisses.Load()
	total := embHits + embMisses + resHits + resMisses
	if total == 0 {
		return 0
	}
	hits := embHits + resHits
	return float64(hits) / float64(total) * 100
}

// Snapshot returns a copy of the current stats.
func (s *CacheStats) Snapshot() CacheStatsSnapshot {
	return CacheStatsSnapshot{
		EmbeddingHits:      s.embeddingHits.Load(),
		EmbeddingMisses:    s.embeddingMisses.Load(),
		ResultHits:         s.resultHits.Load(),
		ResultMisses:       s.resultMisses.Load(),
		EmbeddingEvictions: s.embeddingEvictions.Load(),
		ResultEvictions:    s.resultEvictions.Load(),
	}
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

	c := &Client{
		db:                 cfg.DB,
		embedSvc:           embedSvc,
		queryCache:         make(map[string]embeddingCacheEntry),
		cacheMaxSize:       500,          // Cache up to 500 query embeddings
		cacheTTLNano:       5 * 60 * 1e9, // 5 minute TTL for embeddings
		resultCache:        make(map[string]resultCacheEntry),
		resultCacheMaxSize: 200,      // Cache up to 200 search results
		resultCacheTTLNano: 60 * 1e9, // 1 minute TTL for results (shorter since data changes)
		stopCleanup:        make(chan struct{}),
	}

	// Start background cache cleanup goroutine
	c.cleanupWg.Add(1)
	go c.cacheCleanupLoop()

	return c, nil
}

// AddDocuments adds documents with their embeddings to the vector store.
func (c *Client) AddDocuments(ctx context.Context, docs []Document) error {
	if len(docs) == 0 {
		return nil
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

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
			if rbErr := tx.Rollback(); rbErr != nil {
				// Rollback failure is serious - indicates potential data corruption risk
				log.Error().Err(rbErr).Err(err).Msg("Failed to rollback transaction after error - data may be inconsistent")
			}
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

	// Invalidate result cache since data changed
	c.InvalidateResultCache()

	log.Debug().Int("count", len(docs)).Str("model", modelVersion).Msg("Added documents to sqlite-vec")
	return nil
}

// DeleteDocuments removes documents by their IDs.
func (c *Client) DeleteDocuments(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	// Build placeholder string
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
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

	// Invalidate result cache since data changed
	c.InvalidateResultCache()

	log.Debug().Int("count", len(ids)).Msg("Deleted documents from sqlite-vec")
	return nil
}

// Query performs a vector similarity search.
func (c *Client) Query(ctx context.Context, query string, limit int, where map[string]any) ([]QueryResult, error) {
	// Build cache key from query + filters + limit
	cacheKey := c.buildResultCacheKey(query, limit, where)

	// Check result cache first
	if results, ok := c.getResultFromCache(cacheKey); ok {
		return results, nil
	}

	// Generate query embedding OUTSIDE the lock for better concurrency
	queryEmb, err := c.getOrComputeEmbedding(query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	// Serialize query embedding
	queryBlob, err := sqlite_vec.SerializeFloat32(queryEmb)
	if err != nil {
		return nil, fmt.Errorf("serialize query embedding: %w", err)
	}

	// Now acquire read lock for the actual DB query
	c.readMu.RLock()
	defer c.readMu.RUnlock()

	// Build query with filters
	// vec0 supports WHERE clauses on metadata columns
	args := []any{queryBlob}

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

	// Cache the results
	c.cacheResults(cacheKey, results)

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

// Close stops the background cleanup goroutine (db managed externally).
func (c *Client) Close() error {
	// Signal cleanup goroutine to stop
	close(c.stopCleanup)
	// Wait for cleanup to finish
	c.cleanupWg.Wait()
	return nil
}

// cacheCleanupLoop periodically removes expired cache entries.
func (c *Client) cacheCleanupLoop() {
	defer c.cleanupWg.Done()

	ticker := time.NewTicker(30 * time.Second) // Cleanup every 30 seconds
	defer ticker.Stop()

	for {
		select {
		case <-c.stopCleanup:
			return
		case <-ticker.C:
			c.cleanupExpiredCaches()
		}
	}
}

// cleanupExpiredCaches removes expired entries from both caches.
func (c *Client) cleanupExpiredCaches() {
	now := time.Now().UnixNano()
	var embeddingExpired, resultExpired int64

	// Cleanup embedding cache
	c.queryCacheMu.Lock()
	for key, entry := range c.queryCache {
		if now-entry.timestamp > c.cacheTTLNano {
			delete(c.queryCache, key)
			embeddingExpired++
		}
	}
	c.queryCacheMu.Unlock()

	// Cleanup result cache
	c.resultCacheMu.Lock()
	for key, entry := range c.resultCache {
		if now-entry.timestamp > c.resultCacheTTLNano {
			delete(c.resultCache, key)
			resultExpired++
		}
	}
	c.resultCacheMu.Unlock()

	// Update stats atomically
	if embeddingExpired > 0 || resultExpired > 0 {
		c.stats.embeddingEvictions.Add(embeddingExpired)
		c.stats.resultEvictions.Add(resultExpired)

		log.Debug().
			Int64("embedding_expired", embeddingExpired).
			Int64("result_expired", resultExpired).
			Msg("Cache cleanup completed")
	}
}

// BatchQueryResult holds results from a batch query operation.
type BatchQueryResult struct {
	Error   error
	Query   string
	Results []QueryResult
}

// QueryBatch performs multiple vector searches concurrently.
// Returns results in the same order as input queries.
// Uses a worker pool to limit concurrent queries.
func (c *Client) QueryBatch(ctx context.Context, queries []string, limit int, where map[string]any) []BatchQueryResult {
	if len(queries) == 0 {
		return nil
	}

	// Limit concurrency to avoid overwhelming the database
	maxConcurrent := min(4, len(queries))

	results := make([]BatchQueryResult, len(queries))
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup

	for i, query := range queries {
		wg.Add(1)
		go func(idx int, q string) {
			defer wg.Done()

			// Acquire semaphore
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results[idx] = BatchQueryResult{
					Query: q,
					Error: ctx.Err(),
				}
				return
			}

			// Execute query
			queryResults, err := c.Query(ctx, q, limit, where)
			results[idx] = BatchQueryResult{
				Query:   q,
				Results: queryResults,
				Error:   err,
			}
		}(i, query)
	}

	wg.Wait()
	return results
}

// QueryMultiField searches across multiple fields for a single query.
// Combines results from different field types and deduplicates by document ID.
func (c *Client) QueryMultiField(ctx context.Context, query string, limit int, docType string, project string) ([]QueryResult, error) {
	// Generate embedding once
	queryEmb, err := c.getOrComputeEmbedding(query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	// Serialize query embedding
	queryBlob, err := sqlite_vec.SerializeFloat32(queryEmb)
	if err != nil {
		return nil, fmt.Errorf("serialize query embedding: %w", err)
	}

	c.readMu.RLock()
	defer c.readMu.RUnlock()

	// Query with field type aggregation - get best match per document
	sqlQuery := `
		WITH ranked_results AS (
			SELECT
				doc_id,
				distance,
				sqlite_id,
				doc_type,
				field_type,
				project,
				scope,
				ROW_NUMBER() OVER (PARTITION BY sqlite_id ORDER BY distance ASC) as rn
			FROM vectors
			WHERE embedding MATCH ?
				AND doc_type = ?
				AND (project = ? OR scope = 'global')
		)
		SELECT doc_id, distance, sqlite_id, doc_type, field_type, project, scope
		FROM ranked_results
		WHERE rn = 1
		ORDER BY distance
		LIMIT ?
	`

	rows, err := c.db.QueryContext(ctx, sqlQuery, queryBlob, docType, project, limit)
	if err != nil {
		return nil, fmt.Errorf("query vectors: %w", err)
	}
	defer rows.Close()

	// Pre-allocate with limit to avoid repeated slice growth
	results := make([]QueryResult, 0, limit)
	for rows.Next() {
		var r QueryResult
		var sqliteID int64
		var docTypeVal, fieldType, projectVal, scope sql.NullString

		if err := rows.Scan(&r.ID, &r.Distance, &sqliteID, &docTypeVal, &fieldType, &projectVal, &scope); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}

		r.Similarity = DistanceToSimilarity(r.Distance)
		r.Metadata = map[string]any{
			"sqlite_id":  float64(sqliteID),
			"doc_type":   docTypeVal.String,
			"field_type": fieldType.String,
			"project":    projectVal.String,
			"scope":      scope.String,
		}

		results = append(results, r)
	}

	return results, rows.Err()
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
	c.readMu.RLock()
	defer c.readMu.RUnlock()

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
	c.readMu.RLock()
	defer c.readMu.RUnlock()

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
	DocType   string
	FieldType string
	Project   string
	Scope     string
	SQLiteID  int64
}

// GetStaleVectors returns doc_ids of vectors with mismatched or null model versions.
// This enables granular rebuild - only re-embedding documents that need updating.
func (c *Client) GetStaleVectors(ctx context.Context) ([]StaleVectorInfo, error) {
	c.readMu.RLock()
	defer c.readMu.RUnlock()

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

// VectorHealthStats contains comprehensive health information about the vector store.
type VectorHealthStats struct {
	CoverageByType map[string]int64   `json:"coverage_by_type"`
	ModelVersions  map[string]int64   `json:"model_versions"`
	ProjectCounts  map[string]int64   `json:"project_counts"`
	CurrentModel   string             `json:"current_model"`
	RebuildReason  string             `json:"rebuild_reason,omitempty"`
	EmbeddingCache CacheStatsSnapshot `json:"embedding_cache"`
	TotalVectors   int64              `json:"total_vectors"`
	StaleVectors   int64              `json:"stale_vectors"`
	NeedsRebuild   bool               `json:"needs_rebuild"`
}

// GetHealthStats returns comprehensive health statistics about the vector store.
func (c *Client) GetHealthStats(ctx context.Context) (*VectorHealthStats, error) {
	c.readMu.RLock()
	defer c.readMu.RUnlock()

	stats := &VectorHealthStats{
		CurrentModel:   c.embedSvc.Version(),
		CoverageByType: make(map[string]int64),
		ModelVersions:  make(map[string]int64),
		ProjectCounts:  make(map[string]int64),
		EmbeddingCache: c.stats.Snapshot(),
	}

	// Get total count
	err := c.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM vectors").Scan(&stats.TotalVectors)
	if err != nil {
		return nil, fmt.Errorf("count total vectors: %w", err)
	}

	// Get stale count
	err = c.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM vectors WHERE model_version != ? OR model_version IS NULL",
		stats.CurrentModel,
	).Scan(&stats.StaleVectors)
	if err != nil {
		return nil, fmt.Errorf("count stale vectors: %w", err)
	}

	// Check if rebuild needed
	stats.NeedsRebuild, stats.RebuildReason = c.needsRebuildUnlocked(ctx, stats.CurrentModel)

	// Get coverage by doc_type
	rows, err := c.db.QueryContext(ctx, "SELECT doc_type, COUNT(*) FROM vectors GROUP BY doc_type")
	if err != nil {
		return nil, fmt.Errorf("query doc types: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var docType sql.NullString
		var count int64
		if err := rows.Scan(&docType, &count); err != nil {
			return nil, fmt.Errorf("scan doc type: %w", err)
		}
		if docType.Valid {
			stats.CoverageByType[docType.String] = count
		} else {
			stats.CoverageByType["unknown"] = count
		}
	}

	// Get model version distribution
	rows2, err := c.db.QueryContext(ctx, "SELECT COALESCE(model_version, 'unknown'), COUNT(*) FROM vectors GROUP BY model_version")
	if err != nil {
		return nil, fmt.Errorf("query model versions: %w", err)
	}
	defer rows2.Close()

	for rows2.Next() {
		var version string
		var count int64
		if err := rows2.Scan(&version, &count); err != nil {
			return nil, fmt.Errorf("scan model version: %w", err)
		}
		stats.ModelVersions[version] = count
	}

	// Get project counts (top 10)
	rows3, err := c.db.QueryContext(ctx,
		"SELECT COALESCE(project, 'global'), COUNT(*) FROM vectors GROUP BY project ORDER BY COUNT(*) DESC LIMIT 10")
	if err != nil {
		return nil, fmt.Errorf("query projects: %w", err)
	}
	defer rows3.Close()

	for rows3.Next() {
		var project string
		var count int64
		if err := rows3.Scan(&project, &count); err != nil {
			return nil, fmt.Errorf("scan project: %w", err)
		}
		stats.ProjectCounts[project] = count
	}

	return stats, nil
}

// needsRebuildUnlocked checks if rebuild is needed without acquiring lock (caller must hold lock).
func (c *Client) needsRebuildUnlocked(ctx context.Context, currentModel string) (bool, string) {
	var totalCount int64
	err := c.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM vectors").Scan(&totalCount)
	if err != nil {
		return false, ""
	}

	if totalCount == 0 {
		return true, "empty"
	}

	var staleCount int64
	err = c.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM vectors WHERE model_version != ? OR model_version IS NULL",
		currentModel,
	).Scan(&staleCount)
	if err != nil {
		return false, ""
	}

	if staleCount > 0 {
		return true, fmt.Sprintf("model_mismatch:%d", staleCount)
	}

	return false, ""
}

// DeleteVectorsByDocIDs removes vectors by their doc_ids.
// Used for granular rebuild - delete stale vectors before re-adding.
func (c *Client) DeleteVectorsByDocIDs(ctx context.Context, docIDs []string) error {
	if len(docIDs) == 0 {
		return nil
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	// Build placeholder string
	placeholders := make([]string, len(docIDs))
	args := make([]any, len(docIDs))
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

// DeleteByObservationID removes all vectors associated with an observation ID.
// Vectors are stored with doc_ids that include the observation ID, e.g., "obs_123_narrative".
func (c *Client) DeleteByObservationID(ctx context.Context, obsID int64) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	// Vectors have doc_ids like "obs_123_narrative", "obs_123_facts_0", etc.
	pattern := fmt.Sprintf("obs_%d_%%", obsID)

	_, err := c.db.ExecContext(ctx, "DELETE FROM vectors WHERE doc_id LIKE ?", pattern)
	if err != nil {
		return fmt.Errorf("delete vectors for observation %d: %w", obsID, err)
	}

	log.Debug().Int64("observation_id", obsID).Msg("Deleted vectors for observation")
	return nil
}

// getOrComputeEmbedding returns a cached embedding or computes a new one.
// Uses singleflight to prevent duplicate concurrent computations for the same query.
func (c *Client) getOrComputeEmbedding(query string) ([]float32, error) {
	now := time.Now().UnixNano()

	// Check cache first (read lock)
	c.queryCacheMu.RLock()
	if entry, ok := c.queryCache[query]; ok {
		if now-entry.timestamp < c.cacheTTLNano {
			c.queryCacheMu.RUnlock()
			c.stats.embeddingHits.Add(1)
			return entry.embedding, nil
		}
	}
	c.queryCacheMu.RUnlock()

	// Cache miss - use singleflight to deduplicate concurrent embedding requests
	result, err, _ := c.embeddingGroup.Do(query, func() (any, error) {
		// Double-check cache inside singleflight (another goroutine may have just cached it)
		c.queryCacheMu.RLock()
		if entry, ok := c.queryCache[query]; ok {
			if time.Now().UnixNano()-entry.timestamp < c.cacheTTLNano {
				c.queryCacheMu.RUnlock()
				return entry.embedding, nil
			}
		}
		c.queryCacheMu.RUnlock()

		// Record cache miss
		c.stats.embeddingMisses.Add(1)

		// Compute embedding
		emb, err := c.embedSvc.Embed(query)
		if err != nil {
			return nil, err
		}

		// Store in cache (write lock)
		c.queryCacheMu.Lock()
		nowCache := time.Now().UnixNano()
		// Evict old entries if cache is full or near capacity (80% threshold)
		evictionThreshold := (c.cacheMaxSize * 8) / 10
		if len(c.queryCache) >= evictionThreshold {
			// Phase 1: Remove ALL expired entries first (not just 10%)
			evicted := int64(0)
			for k, v := range c.queryCache {
				if nowCache-v.timestamp > c.cacheTTLNano {
					delete(c.queryCache, k)
					evicted++
				}
			}

			// Phase 2: If still at capacity, evict 10% using random iteration (O(n) instead of O(n log n))
			// Go map iteration order is randomized, providing good cache behavior without sorting
			if len(c.queryCache) >= c.cacheMaxSize {
				evictCount := max(c.cacheMaxSize/10, 1)
				for k := range c.queryCache {
					delete(c.queryCache, k)
					evicted++
					evictCount--
					if evictCount <= 0 {
						break
					}
				}
			}

			if evicted > 0 {
				c.stats.embeddingEvictions.Add(evicted)
			}
		}
		c.queryCache[query] = embeddingCacheEntry{
			embedding: emb,
			timestamp: nowCache,
		}
		c.queryCacheMu.Unlock()

		return emb, nil
	})

	if err != nil {
		return nil, err
	}
	return result.([]float32), nil
}

// ClearCache clears the embedding cache.
func (c *Client) ClearCache() {
	c.queryCacheMu.Lock()
	c.queryCache = make(map[string]embeddingCacheEntry)
	c.queryCacheMu.Unlock()
}

// GetCacheStats returns comprehensive cache statistics.
func (c *Client) GetCacheStats() CacheStatsSnapshot {
	return c.stats.Snapshot()
}

// CacheStats returns basic cache size info for backward compatibility.
// Deprecated: Use GetCacheStats() for comprehensive statistics.
func (c *Client) CacheStats() (size int, maxSize int) {
	c.queryCacheMu.RLock()
	size = len(c.queryCache)
	c.queryCacheMu.RUnlock()
	return size, c.cacheMaxSize
}

// EmbeddingCacheSize returns the current embedding cache size.
func (c *Client) EmbeddingCacheSize() int {
	c.queryCacheMu.RLock()
	defer c.queryCacheMu.RUnlock()
	return len(c.queryCache)
}

// ResultCacheSize returns the current result cache size.
func (c *Client) ResultCacheSize() int {
	c.resultCacheMu.RLock()
	defer c.resultCacheMu.RUnlock()
	return len(c.resultCache)
}

// buildResultCacheKey creates a unique key for caching query results.
// Uses strings.Builder to avoid intermediate allocations.
func (c *Client) buildResultCacheKey(query string, limit int, where map[string]any) string {
	// Pre-allocate with typical key size to avoid reallocation
	var b strings.Builder
	b.Grow(len(query) + 32) // query + typical prefix/suffix overhead

	b.WriteString("q:")
	b.WriteString(query)
	b.WriteString(":l:")
	b.WriteString(strconv.Itoa(limit))

	if docType, ok := where["doc_type"].(string); ok {
		b.WriteString(":dt:")
		b.WriteString(docType)
	}
	if project, ok := where["project"].(string); ok {
		b.WriteString(":p:")
		b.WriteString(project)
	}
	return b.String()
}

// getResultFromCache retrieves cached results if available and not expired.
func (c *Client) getResultFromCache(cacheKey string) ([]QueryResult, bool) {
	now := time.Now().UnixNano()

	c.resultCacheMu.RLock()
	entry, ok := c.resultCache[cacheKey]
	c.resultCacheMu.RUnlock()

	if !ok {
		c.stats.resultMisses.Add(1)
		return nil, false
	}

	// Check if entry is expired
	if now-entry.timestamp > c.resultCacheTTLNano {
		c.stats.resultMisses.Add(1)
		return nil, false
	}

	c.stats.resultHits.Add(1)

	// Return a copy to prevent mutation
	results := make([]QueryResult, len(entry.results))
	copy(results, entry.results)
	return results, true
}

// cacheResults stores query results in the cache.
func (c *Client) cacheResults(cacheKey string, results []QueryResult) {
	now := time.Now().UnixNano()

	c.resultCacheMu.Lock()
	defer c.resultCacheMu.Unlock()

	// Evict old entries if cache is full
	if len(c.resultCache) >= c.resultCacheMaxSize {
		// Two-phase eviction: (1) TTL-expired entries, (2) random if still over capacity
		evicted := 0
		targetSize := c.resultCacheMaxSize * 8 / 10 // Target 80% capacity

		// Phase 1: Remove all TTL-expired entries
		for k, v := range c.resultCache {
			if now-v.timestamp > c.resultCacheTTLNano {
				delete(c.resultCache, k)
				evicted++
			}
		}

		// Phase 2: If still over target, remove random entries until at target
		if len(c.resultCache) >= targetSize {
			evictCount := len(c.resultCache) - targetSize + 1
			for k := range c.resultCache {
				delete(c.resultCache, k)
				evicted++
				evictCount--
				if evictCount <= 0 {
					break
				}
			}
		}

		if evicted > 0 {
			c.stats.resultEvictions.Add(int64(evicted))
		}
	}

	// Make a copy of results to store
	resultsCopy := make([]QueryResult, len(results))
	copy(resultsCopy, results)

	c.resultCache[cacheKey] = resultCacheEntry{
		results:   resultsCopy,
		timestamp: now,
		queryHash: cacheKey,
	}
}

// InvalidateResultCache clears the result cache.
// Should be called after write operations that modify vectors.
func (c *Client) InvalidateResultCache() {
	c.resultCacheMu.Lock()
	c.resultCache = make(map[string]resultCacheEntry)
	c.resultCacheMu.Unlock()
}
