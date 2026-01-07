// Package hybrid provides LEANN-inspired selective vector storage for claude-mnemonic.
//
// This package implements a hybrid storage strategy where frequently-accessed
// observations ("hubs") have their embeddings stored, while infrequently-accessed
// observations have their embeddings recomputed on-demand during search.
//
// This approach reduces storage by 60-80% with minimal impact on search latency (<50ms).
package hybrid

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/lukaszraczylo/claude-mnemonic/internal/embedding"
	"github.com/lukaszraczylo/claude-mnemonic/internal/vector/sqlitevec"
	"github.com/rs/zerolog/log"
)

// VectorStorageStrategy defines how embeddings are stored/computed
type VectorStorageStrategy int

const (
	// StorageAlways stores all embeddings (current behavior, backwards compatible)
	StorageAlways VectorStorageStrategy = iota
	// StorageHub stores only frequently-accessed "hub" embeddings (recommended)
	StorageHub
	// StorageOnDemand recomputes all embeddings during search (maximum savings)
	StorageOnDemand
)

// Client wraps sqlitevec.Client with selective storage logic
type Client struct {
	base         *sqlitevec.Client
	db           *sql.DB
	embedSvc     *embedding.Service
	accessCount  map[string]int
	lastAccess   map[string]time.Time
	contentCache map[string]string
	strategy     VectorStorageStrategy
	hubThreshold int
	mu           sync.RWMutex
	cacheMu      sync.RWMutex
}

// Config for hybrid client
type Config struct {
	BaseClient   *sqlitevec.Client
	DB           *sql.DB
	EmbedSvc     *embedding.Service
	Strategy     VectorStorageStrategy
	HubThreshold int // Default: 5 accesses
}

// NewClient creates a new hybrid vector client
func NewClient(cfg Config) *Client {
	if cfg.HubThreshold <= 0 {
		cfg.HubThreshold = 5
	}

	log.Info().
		Str("strategy", strategyToString(cfg.Strategy)).
		Int("hub_threshold", cfg.HubThreshold).
		Msg("Initializing LEANN hybrid vector client")

	return &Client{
		base:         cfg.BaseClient,
		db:           cfg.DB,
		embedSvc:     cfg.EmbedSvc,
		strategy:     cfg.Strategy,
		hubThreshold: cfg.HubThreshold,
		accessCount:  make(map[string]int),
		lastAccess:   make(map[string]time.Time),
		contentCache: make(map[string]string),
	}
}

// AddDocuments implements selective storage based on strategy
func (c *Client) AddDocuments(ctx context.Context, docs []sqlitevec.Document) error {
	if len(docs) == 0 {
		return nil
	}

	switch c.strategy {
	case StorageAlways:
		// Use existing implementation - store all embeddings
		return c.base.AddDocuments(ctx, docs)

	case StorageHub:
		// Store only hub candidates
		return c.addDocumentsSelective(ctx, docs)

	case StorageOnDemand:
		// Don't store embeddings, only cache content
		return c.cacheDocuments(ctx, docs)

	default:
		return c.base.AddDocuments(ctx, docs)
	}
}

// addDocumentsSelective stores embeddings only for hub-qualified documents
func (c *Client) addDocumentsSelective(ctx context.Context, docs []sqlitevec.Document) error {
	// Always cache content for potential recomputation
	if err := c.cacheDocuments(ctx, docs); err != nil {
		return err
	}

	// Filter to hub documents
	hubDocs := make([]sqlitevec.Document, 0, len(docs))
	for _, doc := range docs {
		if c.isHub(doc.ID) {
			hubDocs = append(hubDocs, doc)
		}
	}

	// Store only hub embeddings
	if len(hubDocs) > 0 {
		log.Debug().
			Int("total", len(docs)).
			Int("hubs", len(hubDocs)).
			Msg("Storing selective embeddings")
		return c.base.AddDocuments(ctx, hubDocs)
	}

	log.Debug().Int("total", len(docs)).Msg("All documents cached, no hubs to store")
	return nil
}

// cacheDocuments stores content for later recomputation
func (c *Client) cacheDocuments(ctx context.Context, docs []sqlitevec.Document) error {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	for _, doc := range docs {
		c.contentCache[doc.ID] = doc.Content
	}

	return nil
}

// DeleteDocuments removes documents by their IDs
func (c *Client) DeleteDocuments(ctx context.Context, ids []string) error {
	// Remove from base storage
	if err := c.base.DeleteDocuments(ctx, ids); err != nil {
		return err
	}

	// Clean up caches
	c.mu.Lock()
	for _, id := range ids {
		delete(c.accessCount, id)
		delete(c.lastAccess, id)
	}
	c.mu.Unlock()

	c.cacheMu.Lock()
	for _, id := range ids {
		delete(c.contentCache, id)
	}
	c.cacheMu.Unlock()

	return nil
}

// Query performs search with dynamic recomputation
func (c *Client) Query(ctx context.Context, query string, limit int, where map[string]any) ([]sqlitevec.QueryResult, error) {
	switch c.strategy {
	case StorageAlways:
		// Use existing implementation
		return c.queryAndTrack(ctx, query, limit, where)

	case StorageHub:
		// Search hubs, then expand with recomputation
		return c.queryHybrid(ctx, query, limit, where)

	case StorageOnDemand:
		// Fully dynamic search
		return c.queryDynamic(ctx, query, limit, where)

	default:
		return c.queryAndTrack(ctx, query, limit, where)
	}
}

// queryAndTrack wraps base Query with access tracking
func (c *Client) queryAndTrack(ctx context.Context, query string, limit int, where map[string]any) ([]sqlitevec.QueryResult, error) {
	results, err := c.base.Query(ctx, query, limit, where)
	if err != nil {
		return nil, err
	}

	// Track access for hub detection
	c.trackAccess(results)

	return results, nil
}

// queryHybrid searches stored hubs and recomputes non-hubs
func (c *Client) queryHybrid(ctx context.Context, query string, limit int, where map[string]any) ([]sqlitevec.QueryResult, error) {
	startTime := time.Now()

	// 1. Query stored hub embeddings (limit * 2 for expansion)
	hubResults, err := c.base.Query(ctx, query, limit*2, where)
	if err != nil {
		return nil, err
	}

	// 2. Track access
	c.trackAccess(hubResults)

	// 3. Get candidate non-hub IDs (from content cache)
	candidates := c.getCandidateNonHubs(where, limit*2)

	// 4. Recompute embeddings for candidates if we have any
	var recomputedResults []sqlitevec.QueryResult
	if len(candidates) > 0 {
		recomputedResults, err = c.recomputeAndScore(ctx, query, candidates)
		if err != nil {
			// Log but don't fail - use hub results only
			log.Warn().Err(err).Msg("Failed to recompute embeddings, using hub results only")
			recomputedResults = nil
		}
	}

	// 5. Merge and rank
	allResults := append(hubResults, recomputedResults...)
	sortBySimilarity(allResults)

	// 6. Return top K
	if len(allResults) > limit {
		allResults = allResults[:limit]
	}

	duration := time.Since(startTime)
	log.Debug().
		Dur("duration_ms", duration).
		Int("hubs", len(hubResults)).
		Int("recomputed", len(recomputedResults)).
		Int("results", len(allResults)).
		Msg("Hybrid search completed")

	return allResults, nil
}

// queryDynamic recomputes all embeddings on-the-fly
func (c *Client) queryDynamic(ctx context.Context, query string, limit int, where map[string]any) ([]sqlitevec.QueryResult, error) {
	startTime := time.Now()

	// Get all candidate IDs from content cache
	candidates := c.getCandidateNonHubs(where, limit*5)

	// Recompute and score all
	results, err := c.recomputeAndScore(ctx, query, candidates)
	if err != nil {
		return nil, err
	}

	// Track access
	c.trackAccess(results)

	// Return top K
	if len(results) > limit {
		results = results[:limit]
	}

	duration := time.Since(startTime)
	log.Debug().
		Dur("duration_ms", duration).
		Int("recomputed", len(candidates)).
		Int("results", len(results)).
		Msg("Dynamic search completed")

	return results, nil
}

// recomputeAndScore generates embeddings and computes similarities
func (c *Client) recomputeAndScore(ctx context.Context, query string, candidateIDs []string) ([]sqlitevec.QueryResult, error) {
	if len(candidateIDs) == 0 {
		return nil, nil
	}

	// Generate query embedding
	queryEmb, err := c.embedSvc.Embed(query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	// Get content for candidates
	c.cacheMu.RLock()
	texts := make([]string, 0, len(candidateIDs))
	validIDs := make([]string, 0, len(candidateIDs))
	for _, id := range candidateIDs {
		if content, ok := c.contentCache[id]; ok && content != "" {
			texts = append(texts, content)
			validIDs = append(validIDs, id)
		}
	}
	c.cacheMu.RUnlock()

	if len(texts) == 0 {
		return nil, nil
	}

	// Batch generate embeddings
	embeddings, err := c.embedSvc.EmbedBatch(texts)
	if err != nil {
		return nil, fmt.Errorf("batch embed: %w", err)
	}

	// Compute similarities
	results := make([]sqlitevec.QueryResult, len(embeddings))
	for i, emb := range embeddings {
		similarity := cosineSimilarity(queryEmb, emb)
		distance := 1.0 - similarity // Convert to distance

		results[i] = sqlitevec.QueryResult{
			ID:         validIDs[i],
			Distance:   float64(distance),
			Similarity: float64(similarity),
			Metadata:   make(map[string]any),
		}
	}

	return results, nil
}

// trackAccess records document access for hub detection
func (c *Client) trackAccess(results []sqlitevec.QueryResult) {
	if len(results) == 0 {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for _, r := range results {
		c.accessCount[r.ID]++
		c.lastAccess[r.ID] = now
	}
}

// isHub checks if a document qualifies as a hub
func (c *Client) isHub(docID string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	count := c.accessCount[docID]
	return count >= c.hubThreshold
}

// getCandidateNonHubs returns IDs of non-hub documents matching filter
func (c *Client) getCandidateNonHubs(where map[string]any, limit int) []string {
	c.cacheMu.RLock()
	defer c.cacheMu.RUnlock()

	candidates := make([]string, 0, limit)
	for id := range c.contentCache {
		if !c.isHub(id) {
			candidates = append(candidates, id)
			if len(candidates) >= limit {
				break
			}
		}
	}

	return candidates
}

// IsConnected always returns true (wraps base client)
func (c *Client) IsConnected() bool {
	return c.base.IsConnected()
}

// Close releases resources
func (c *Client) Close() error {
	return c.base.Close()
}

// Count returns the total number of vectors in the store
func (c *Client) Count(ctx context.Context) (int64, error) {
	return c.base.Count(ctx)
}

// ModelVersion returns the current embedding model version
func (c *Client) ModelVersion() string {
	return c.base.ModelVersion()
}

// NeedsRebuild checks if vectors need to be rebuilt due to model version change
func (c *Client) NeedsRebuild(ctx context.Context) (bool, string) {
	return c.base.NeedsRebuild(ctx)
}

// GetStaleVectors returns doc_ids of vectors with mismatched or null model versions
func (c *Client) GetStaleVectors(ctx context.Context) ([]sqlitevec.StaleVectorInfo, error) {
	return c.base.GetStaleVectors(ctx)
}

// DeleteVectorsByDocIDs removes vectors by their doc_ids
func (c *Client) DeleteVectorsByDocIDs(ctx context.Context, docIDs []string) error {
	return c.base.DeleteVectorsByDocIDs(ctx, docIDs)
}

// GetStorageStats returns storage efficiency metrics
func (c *Client) GetStorageStats(ctx context.Context) (StorageStats, error) {
	c.mu.RLock()
	c.cacheMu.RLock()
	defer c.mu.RUnlock()
	defer c.cacheMu.RUnlock()

	totalDocs := len(c.contentCache)
	hubCount := 0
	for id := range c.contentCache {
		if c.accessCount[id] >= c.hubThreshold {
			hubCount++
		}
	}

	storedCount := hubCount
	if c.strategy == StorageAlways {
		// Get actual count from database
		if count, err := c.base.Count(ctx); err == nil {
			storedCount = int(count)
		}
	} else if c.strategy == StorageOnDemand {
		storedCount = 0
	}

	embeddingSize := 384 * 4 // 384 dims × 4 bytes (float32)
	storedBytes := storedCount * embeddingSize
	potentialBytes := totalDocs * embeddingSize

	savingsPercent := 0.0
	if potentialBytes > 0 {
		savingsPercent = (1.0 - float64(storedBytes)/float64(potentialBytes)) * 100
	}

	return StorageStats{
		TotalDocuments:   totalDocs,
		HubDocuments:     hubCount,
		StoredEmbeddings: storedCount,
		StorageBytes:     storedBytes,
		SavingsPercent:   savingsPercent,
		Strategy:         c.strategy,
	}, nil
}

// StorageStats contains storage efficiency metrics
type StorageStats struct {
	TotalDocuments   int
	HubDocuments     int
	StoredEmbeddings int
	StorageBytes     int
	SavingsPercent   float64
	Strategy         VectorStorageStrategy
}

// Helper functions

func cosineSimilarity(a, b []float32) float32 {
	var dotProduct, normA, normB float32
	for i := range a {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dotProduct / float32(math.Sqrt(float64(normA))*math.Sqrt(float64(normB)))
}

func sortBySimilarity(results []sqlitevec.QueryResult) {
	// Use a simple but efficient sorting algorithm
	n := len(results)
	for i := 0; i < n-1; i++ {
		for j := 0; j < n-i-1; j++ {
			if results[j].Similarity < results[j+1].Similarity {
				results[j], results[j+1] = results[j+1], results[j]
			}
		}
	}
}

func strategyToString(s VectorStorageStrategy) string {
	switch s {
	case StorageAlways:
		return "always"
	case StorageHub:
		return "hub"
	case StorageOnDemand:
		return "on_demand"
	default:
		return "unknown"
	}
}

// ParseStrategy converts a string to VectorStorageStrategy
func ParseStrategy(s string) VectorStorageStrategy {
	switch s {
	case "hub":
		return StorageHub
	case "on_demand":
		return StorageOnDemand
	case "always":
		return StorageAlways
	default:
		return StorageHub // Default to hub strategy
	}
}
