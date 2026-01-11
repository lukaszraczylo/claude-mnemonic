// Package search provides unified search capabilities for claude-mnemonic.
package search

import (
	"context"
	"hash/fnv"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lukaszraczylo/claude-mnemonic/internal/db/gorm"
	"github.com/lukaszraczylo/claude-mnemonic/internal/vector/sqlitevec"
	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/singleflight"
)

// multiSpaceRegex matches multiple consecutive whitespace characters.
// Pre-compiled for performance in normalizeQuery.
var multiSpaceRegex = regexp.MustCompile(`\s+`)

// Search configuration constants.
const (
	// Cache configuration
	defaultCacheTTL        = 30 * time.Second // Short TTL for freshness
	defaultCacheMaxSize    = 200              // Max cached results
	cacheEvictionPercent   = 10               // Evict 10% when cache is full
	cacheEvictionThreshold = 80               // Start eviction scan at 80% capacity

	// Latency tracking
	latencyHistogramCap  = 1000      // Max latency samples for histogram
	slowQueryThresholdNs = 100 * 1e6 // 100ms threshold for slow query logging

	// Query frequency tracking
	maxFrequencyEntries    = 1000           // Max queries to track for warming
	frequencyEvictionBatch = 100            // Remove 10% when frequency map is full
	staleQueryThreshold    = 24 * time.Hour // Remove queries older than 24 hours
	recentQueryWindow      = time.Hour      // Only consider queries from last hour for warming

	// Cache warming configuration
	cacheWarmingInitDelay    = 30 * time.Second // Delay before starting warming
	cacheWarmingInterval     = 20 * time.Second // Run warming cycle every 20 seconds
	frequencyCleanupInterval = 5 * time.Minute  // Cleanup stale entries every 5 minutes
	cacheCleanupInterval     = time.Minute      // Cleanup expired cache every minute
	warmingQueryTimeout      = 5 * time.Second  // Timeout for warming queries
	warmingBatchSize         = 5                // Warm top 5 queries per cycle
	minRecencyFactor         = 0.1              // Minimum recency factor for scoring

	// Default query limits
	defaultQueryLimit = 20
	maxQueryLimit     = 100
	defaultOrderBy    = "date_desc"

	// Truncation lengths
	queryLogTruncateLen   = 50  // Truncate query in logs
	titleTruncateLen      = 100 // Truncate titles in results
	warmingLogTruncateLen = 30  // Truncate query in warming logs
)

// SearchMetrics tracks search performance statistics.
type SearchMetrics struct {
	latencyHistogram  []int64
	TotalSearches     int64
	VectorSearches    int64
	FilterSearches    int64
	TotalLatencyNs    int64
	VectorLatencyNs   int64
	FilterLatencyNs   int64
	CacheHits         int64
	CoalescedRequests int64
	SearchErrors      int64
	histogramMu       sync.Mutex
}

// GetStats returns the current search statistics.
func (m *SearchMetrics) GetStats() map[string]any {
	totalSearches := atomic.LoadInt64(&m.TotalSearches)
	totalLatency := atomic.LoadInt64(&m.TotalLatencyNs)
	vectorSearches := atomic.LoadInt64(&m.VectorSearches)
	vectorLatency := atomic.LoadInt64(&m.VectorLatencyNs)
	filterSearches := atomic.LoadInt64(&m.FilterSearches)
	filterLatency := atomic.LoadInt64(&m.FilterLatencyNs)

	avgLatencyMs := float64(0)
	if totalSearches > 0 {
		avgLatencyMs = float64(totalLatency) / float64(totalSearches) / 1e6
	}

	avgVectorLatencyMs := float64(0)
	if vectorSearches > 0 {
		avgVectorLatencyMs = float64(vectorLatency) / float64(vectorSearches) / 1e6
	}

	avgFilterLatencyMs := float64(0)
	if filterSearches > 0 {
		avgFilterLatencyMs = float64(filterLatency) / float64(filterSearches) / 1e6
	}

	return map[string]any{
		"total_searches":        totalSearches,
		"vector_searches":       vectorSearches,
		"filter_searches":       filterSearches,
		"cache_hits":            atomic.LoadInt64(&m.CacheHits),
		"coalesced_requests":    atomic.LoadInt64(&m.CoalescedRequests),
		"search_errors":         atomic.LoadInt64(&m.SearchErrors),
		"avg_latency_ms":        avgLatencyMs,
		"avg_vector_latency_ms": avgVectorLatencyMs,
		"avg_filter_latency_ms": avgFilterLatencyMs,
	}
}

// Manager provides unified search across SQLite and sqlite-vec.
type Manager struct {
	ctx              context.Context
	searchGroup      singleflight.Group
	cancel           context.CancelFunc
	vectorClient     *sqlitevec.Client
	metrics          *SearchMetrics
	promptStore      *gorm.PromptStore
	observationStore *gorm.ObservationStore
	summaryStore     *gorm.SummaryStore
	resultCache      map[string]*cachedResult
	queryFrequency   map[string]*queryFrequencyInfo
	cacheTTL         time.Duration
	cacheMaxSize     int
	resultCacheMu    sync.RWMutex
	queryFrequencyMu sync.RWMutex
}

// queryFrequencyInfo tracks how often a query is used.
type queryFrequencyInfo struct {
	lastUsed   time.Time
	lastCached time.Time
	params     SearchParams
	count      int64
}

// cachedResult stores a cached search result with expiry.
type cachedResult struct {
	result    *UnifiedSearchResult
	expiresAt time.Time
}

// NewManager creates a new search manager.
func NewManager(
	observationStore *gorm.ObservationStore,
	summaryStore *gorm.SummaryStore,
	promptStore *gorm.PromptStore,
	vectorClient *sqlitevec.Client,
) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{
		observationStore: observationStore,
		summaryStore:     summaryStore,
		promptStore:      promptStore,
		vectorClient:     vectorClient,
		metrics:          &SearchMetrics{latencyHistogram: make([]int64, 0, latencyHistogramCap)},
		ctx:              ctx,
		cancel:           cancel,
		resultCache:      make(map[string]*cachedResult),
		cacheTTL:         defaultCacheTTL,
		cacheMaxSize:     defaultCacheMaxSize,
		queryFrequency:   make(map[string]*queryFrequencyInfo),
	}
	// Start cache cleanup goroutine
	go m.cleanupCacheLoop()
	// Start cache warming goroutine
	go m.cacheWarmingLoop()
	return m
}

// Close stops background goroutines and cleans up resources.
func (m *Manager) Close() {
	if m.cancel != nil {
		m.cancel()
	}
}

// cleanupCacheLoop periodically removes expired cache entries.
func (m *Manager) cleanupCacheLoop() {
	ticker := time.NewTicker(cacheCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.cleanupExpiredCache()
		}
	}
}

// cleanupExpiredCache removes expired entries from the cache.
func (m *Manager) cleanupExpiredCache() {
	m.resultCacheMu.Lock()
	defer m.resultCacheMu.Unlock()

	now := time.Now()
	for key, cached := range m.resultCache {
		if now.After(cached.expiresAt) {
			delete(m.resultCache, key)
		}
	}
}

// cacheWarmingLoop periodically warms the cache for frequently used queries.
func (m *Manager) cacheWarmingLoop() {
	// Wait a bit before starting to allow system to stabilize
	select {
	case <-m.ctx.Done():
		return
	case <-time.After(cacheWarmingInitDelay):
	}

	warmingTicker := time.NewTicker(cacheWarmingInterval)
	cleanupTicker := time.NewTicker(frequencyCleanupInterval)
	defer warmingTicker.Stop()
	defer cleanupTicker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-warmingTicker.C:
			m.warmFrequentQueries()
		case <-cleanupTicker.C:
			m.cleanupStaleFrequencyEntries()
		}
	}
}

// cleanupStaleFrequencyEntries removes query frequency entries older than staleQueryThreshold.
// This prevents memory bloat from queries that haven't been used in a long time.
func (m *Manager) cleanupStaleFrequencyEntries() {
	m.queryFrequencyMu.Lock()
	now := time.Now()
	var keysToDelete []string
	for k, v := range m.queryFrequency {
		if now.Sub(v.lastUsed) > staleQueryThreshold {
			keysToDelete = append(keysToDelete, k)
		}
	}
	for _, k := range keysToDelete {
		delete(m.queryFrequency, k)
	}
	m.queryFrequencyMu.Unlock()

	if len(keysToDelete) > 0 {
		log.Debug().Int("removed", len(keysToDelete)).Msg("Cleaned up stale query frequency entries")
	}
}

// warmFrequentQueries pre-executes frequently used queries to warm the cache.
func (m *Manager) warmFrequentQueries() {
	m.queryFrequencyMu.RLock()
	// Find top N most frequent queries that aren't recently cached
	type queryScore struct {
		info  *queryFrequencyInfo
		key   string
		score float64
	}
	candidates := make([]queryScore, 0, len(m.queryFrequency))
	now := time.Now()

	for key, info := range m.queryFrequency {
		// Only consider queries used recently
		if now.Sub(info.lastUsed) > recentQueryWindow {
			continue
		}
		// Only warm if not recently cached (cache about to expire or already expired)
		timeSinceLastCache := now.Sub(info.lastCached)
		if timeSinceLastCache < m.cacheTTL/2 {
			continue
		}

		// Score: frequency * recency factor
		recencyFactor := 1.0 - (now.Sub(info.lastUsed).Seconds() / recentQueryWindow.Seconds())
		if recencyFactor < minRecencyFactor {
			recencyFactor = minRecencyFactor
		}
		score := float64(info.count) * recencyFactor

		candidates = append(candidates, queryScore{key: key, info: info, score: score})
	}
	m.queryFrequencyMu.RUnlock()

	// Sort by score descending using O(n log n) algorithm
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	// Warm top queries
	warmCount := min(warmingBatchSize, len(candidates))
	for i := range warmCount {
		candidate := candidates[i]
		ctx, cancel := context.WithTimeout(context.Background(), warmingQueryTimeout)
		result, err := m.executeSearch(ctx, candidate.info.params)
		cancel()

		if err == nil && result != nil {
			cacheKey := m.getCacheKey(candidate.info.params)
			m.putInCache(cacheKey, result)

			// Update last cached time
			m.queryFrequencyMu.Lock()
			if info, ok := m.queryFrequency[candidate.key]; ok {
				info.lastCached = time.Now()
			}
			m.queryFrequencyMu.Unlock()

			log.Debug().
				Str("query", truncate(candidate.info.params.Query, warmingLogTruncateLen)).
				Float64("score", candidate.score).
				Msg("Cache warmed for frequent query")
		}
	}
}

// trackQueryFrequency records query usage for cache warming decisions.
func (m *Manager) trackQueryFrequency(params SearchParams) {
	key := m.getCacheKey(params)

	m.queryFrequencyMu.Lock()

	if info, ok := m.queryFrequency[key]; ok {
		info.count++
		info.lastUsed = time.Now()
		m.queryFrequencyMu.Unlock()
		return // Fast path: no eviction needed
	}

	m.queryFrequency[key] = &queryFrequencyInfo{
		params:   params,
		count:    1,
		lastUsed: time.Now(),
	}

	// Limit frequency map size to prevent memory bloat
	mapLen := len(m.queryFrequency)
	if mapLen <= maxFrequencyEntries {
		m.queryFrequencyMu.Unlock()
		return // Fast path: no eviction needed
	}

	// Collect keys and times for eviction (still under lock, but fast)
	type entry struct {
		lastUsed time.Time
		key      string
	}
	entries := make([]entry, 0, mapLen)
	for k, v := range m.queryFrequency {
		entries = append(entries, entry{key: k, lastUsed: v.lastUsed})
	}
	m.queryFrequencyMu.Unlock()

	// Sort outside lock to reduce contention (O(n log n))
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].lastUsed.Before(entries[j].lastUsed)
	})

	// Collect keys to delete
	deleteCount := min(frequencyEvictionBatch, len(entries))
	keysToDelete := make([]string, deleteCount)
	for i := range deleteCount {
		keysToDelete[i] = entries[i].key
	}

	// Re-acquire lock only for deletion (brief critical section)
	m.queryFrequencyMu.Lock()
	for _, k := range keysToDelete {
		delete(m.queryFrequency, k)
	}
	m.queryFrequencyMu.Unlock()
}

// RecentQuery represents a recently executed search query.
type RecentQuery struct {
	LastUsed time.Time `json:"last_used"`
	Query    string    `json:"query"`
	Project  string    `json:"project,omitempty"`
	Type     string    `json:"type,omitempty"`
	Count    int64     `json:"count"`
}

// GetRecentQueries returns the most recent search queries, sorted by last used time.
func (m *Manager) GetRecentQueries(limit int) []RecentQuery {
	if limit <= 0 {
		limit = defaultQueryLimit
	}
	if limit > maxQueryLimit {
		limit = maxQueryLimit
	}

	m.queryFrequencyMu.RLock()
	defer m.queryFrequencyMu.RUnlock()

	// Collect all queries
	queries := make([]RecentQuery, 0, len(m.queryFrequency))
	for _, info := range m.queryFrequency {
		queries = append(queries, RecentQuery{
			Query:    info.params.Query,
			Project:  info.params.Project,
			Type:     info.params.Type,
			Count:    info.count,
			LastUsed: info.lastUsed,
		})
	}

	// Sort by last used (most recent first)
	sort.Slice(queries, func(i, j int) bool {
		return queries[i].LastUsed.After(queries[j].LastUsed)
	})

	// Limit results
	if len(queries) > limit {
		queries = queries[:limit]
	}

	return queries
}

// GetFrequentQueries returns the most frequently used search queries.
func (m *Manager) GetFrequentQueries(limit int) []RecentQuery {
	if limit <= 0 {
		limit = defaultQueryLimit
	}
	if limit > maxQueryLimit {
		limit = maxQueryLimit
	}

	m.queryFrequencyMu.RLock()
	defer m.queryFrequencyMu.RUnlock()

	// Only include queries used recently
	now := time.Now()
	queries := make([]RecentQuery, 0, len(m.queryFrequency))
	for _, info := range m.queryFrequency {
		if now.Sub(info.lastUsed) > recentQueryWindow {
			continue
		}
		queries = append(queries, RecentQuery{
			Query:    info.params.Query,
			Project:  info.params.Project,
			Type:     info.params.Type,
			Count:    info.count,
			LastUsed: info.lastUsed,
		})
	}

	// Sort by count (highest first)
	sort.Slice(queries, func(i, j int) bool {
		return queries[i].Count > queries[j].Count
	})

	// Limit results
	if len(queries) > limit {
		queries = queries[:limit]
	}

	return queries
}

// normalizeQuery normalizes a search query for consistent cache keys.
// Converts to lowercase, trims whitespace, and collapses multiple spaces.
// Uses pre-compiled regex for O(n) performance instead of O(n*m) loop.
func normalizeQuery(query string) string {
	// Lowercase for case-insensitive matching
	query = strings.ToLower(query)
	// Collapse multiple whitespace into single space using pre-compiled regex
	query = multiSpaceRegex.ReplaceAllString(query, " ")
	// Trim leading/trailing whitespace (after collapsing)
	return strings.TrimSpace(query)
}

// getCacheKey generates a cache key from search params.
// Uses direct string concatenation instead of JSON marshal for better performance.
// Queries are normalized for consistent cache hits across whitespace variations.
func (m *Manager) getCacheKey(params SearchParams) string {
	// Normalize query for consistent cache keys
	normalizedQuery := normalizeQuery(params.Query)

	// Hash directly without intermediate string allocation.
	// FNV-64a is fast and collision-safe for cache keys.
	h := fnv.New64a()

	// Write each field directly to the hasher with separators
	h.Write([]byte(normalizedQuery))
	h.Write([]byte{'|'})
	h.Write([]byte(params.Type))
	h.Write([]byte{'|'})
	h.Write([]byte(params.Project))
	h.Write([]byte{'|'})
	h.Write([]byte(params.ObsType))
	h.Write([]byte{'|'})
	h.Write([]byte(params.Concepts))
	h.Write([]byte{'|'})
	h.Write([]byte(params.Files))
	h.Write([]byte{'|'})
	h.Write([]byte(strconv.FormatInt(params.DateStart, 10)))
	h.Write([]byte{'|'})
	h.Write([]byte(strconv.FormatInt(params.DateEnd, 10)))
	h.Write([]byte{'|'})
	h.Write([]byte(params.OrderBy))
	h.Write([]byte{'|'})
	h.Write([]byte(strconv.Itoa(params.Limit)))
	h.Write([]byte{'|'})
	h.Write([]byte(strconv.Itoa(params.Offset)))
	h.Write([]byte{'|'})
	h.Write([]byte(params.Format))
	h.Write([]byte{'|'})
	h.Write([]byte(params.Scope))
	h.Write([]byte{'|'})
	if params.IncludeGlobal {
		h.Write([]byte{'1'})
	} else {
		h.Write([]byte{'0'})
	}
	h.Write([]byte{'|'})
	if params.ExcludeSuperseded {
		h.Write([]byte{'1'})
	} else {
		h.Write([]byte{'0'})
	}

	return strconv.FormatUint(h.Sum64(), 36) // Base36 for compact representation
}

// getFromCache retrieves a result from cache if valid.
func (m *Manager) getFromCache(key string) (*UnifiedSearchResult, bool) {
	m.resultCacheMu.RLock()
	defer m.resultCacheMu.RUnlock()

	if cached, ok := m.resultCache[key]; ok {
		if time.Now().Before(cached.expiresAt) {
			atomic.AddInt64(&m.metrics.CacheHits, 1)
			return cached.result, true
		}
	}
	return nil, false
}

// putInCache stores a result in the cache.
// Optimized to skip expensive scans when cache is below capacity threshold.
func (m *Manager) putInCache(key string, result *UnifiedSearchResult) {
	m.resultCacheMu.Lock()
	defer m.resultCacheMu.Unlock()

	now := time.Now()
	cacheLen := len(m.resultCache)

	// Only scan for expired entries when at threshold+ capacity (amortized cleanup)
	evictionThreshold := (m.cacheMaxSize * cacheEvictionThreshold) / 100
	if cacheLen >= evictionThreshold {
		// Evict expired entries
		for k, v := range m.resultCache {
			if now.After(v.expiresAt) {
				delete(m.resultCache, k)
			}
		}
		cacheLen = len(m.resultCache) // Update after eviction
	}

	// If still at capacity after removing expired, use simple FIFO-style eviction
	// Go map iteration order is random, which provides good cache behavior
	if cacheLen >= m.cacheMaxSize {
		// Evict percentage using random-order iteration (O(n) single pass)
		evictCount := max(m.cacheMaxSize*cacheEvictionPercent/100, 1)
		evicted := 0
		for k := range m.resultCache {
			delete(m.resultCache, k)
			evicted++
			if evicted >= evictCount {
				break
			}
		}
	}

	m.resultCache[key] = &cachedResult{
		result:    result,
		expiresAt: now.Add(m.cacheTTL),
	}
}

// Metrics returns the search metrics for monitoring.
func (m *Manager) Metrics() *SearchMetrics {
	return m.metrics
}

// CacheStats returns current cache statistics.
func (m *Manager) CacheStats() map[string]any {
	m.resultCacheMu.RLock()
	cacheSize := len(m.resultCache)
	m.resultCacheMu.RUnlock()

	return map[string]any{
		"size":     cacheSize,
		"max_size": m.cacheMaxSize,
		"ttl_sec":  m.cacheTTL.Seconds(),
	}
}

// ClearCache clears the result cache. Useful for testing or after data changes.
func (m *Manager) ClearCache() {
	m.resultCacheMu.Lock()
	m.resultCache = make(map[string]*cachedResult)
	m.resultCacheMu.Unlock()
}

// SearchParams contains parameters for unified search.
type SearchParams struct {
	Format            string
	Type              string
	Project           string
	ObsType           string
	Concepts          string
	Files             string
	Query             string
	Scope             string
	OrderBy           string
	DateStart         int64
	Offset            int
	Limit             int
	DateEnd           int64
	IncludeGlobal     bool
	ExcludeSuperseded bool
}

// SearchResult represents a unified search result.
// Field order optimized for memory alignment (fieldalignment).
type SearchResult struct {
	Metadata  map[string]any `json:"metadata,omitempty"`
	Type      string         `json:"type"`
	Title     string         `json:"title,omitempty"`
	Content   string         `json:"content,omitempty"`
	Project   string         `json:"project"`
	Scope     string         `json:"scope,omitempty"`
	ID        int64          `json:"id"`
	CreatedAt int64          `json:"created_at_epoch"`
	Score     float64        `json:"score,omitempty"`
}

// UnifiedSearchResult contains the combined search results.
type UnifiedSearchResult struct {
	Query      string         `json:"query,omitempty"`
	Results    []SearchResult `json:"results"`
	TotalCount int            `json:"total_count"`
}

// UnifiedSearch performs a unified search across all document types.
// Uses caching and request coalescing for optimal performance.
func (m *Manager) UnifiedSearch(ctx context.Context, params SearchParams) (*UnifiedSearchResult, error) {
	start := time.Now()
	defer func() {
		latency := time.Since(start).Nanoseconds()
		atomic.AddInt64(&m.metrics.TotalSearches, 1)
		atomic.AddInt64(&m.metrics.TotalLatencyNs, latency)

		// Sample latency for histogram (reservoir sampling)
		m.metrics.histogramMu.Lock()
		if len(m.metrics.latencyHistogram) < latencyHistogramCap {
			m.metrics.latencyHistogram = append(m.metrics.latencyHistogram, latency)
		}
		m.metrics.histogramMu.Unlock()

		// Log slow queries
		if latency > slowQueryThresholdNs {
			log.Warn().
				Str("query", truncate(params.Query, queryLogTruncateLen)).
				Dur("latency", time.Duration(latency)).
				Str("type", params.Type).
				Msg("Slow search query")
		}
	}()

	if params.Limit <= 0 {
		params.Limit = defaultQueryLimit
	}
	if params.Limit > maxQueryLimit {
		params.Limit = maxQueryLimit
	}
	if params.OrderBy == "" {
		params.OrderBy = defaultOrderBy
	}

	// Check cache first
	cacheKey := m.getCacheKey(params)
	if cached, ok := m.getFromCache(cacheKey); ok {
		return cached, nil
	}

	// Use singleflight to coalesce concurrent identical requests
	result, err, _ := m.searchGroup.Do(cacheKey, func() (any, error) {
		return m.executeSearch(ctx, params)
	})

	if err != nil {
		return nil, err
	}

	searchResult := result.(*UnifiedSearchResult)

	// Cache the result
	m.putInCache(cacheKey, searchResult)

	// Track query frequency for cache warming
	m.trackQueryFrequency(params)

	return searchResult, nil
}

// executeSearch performs the actual search without caching/coalescing.
func (m *Manager) executeSearch(ctx context.Context, params SearchParams) (*UnifiedSearchResult, error) {
	// If query is provided and vector client is available, use vector search
	if params.Query != "" && m.vectorClient != nil && m.vectorClient.IsConnected() {
		return m.vectorSearch(ctx, params)
	}

	// Otherwise fall back to structured filter search
	return m.filterSearch(ctx, params)
}

// vectorSearch performs semantic search via sqlite-vec.
func (m *Manager) vectorSearch(ctx context.Context, params SearchParams) (*UnifiedSearchResult, error) {
	start := time.Now()
	defer func() {
		latency := time.Since(start).Nanoseconds()
		atomic.AddInt64(&m.metrics.VectorSearches, 1)
		atomic.AddInt64(&m.metrics.VectorLatencyNs, latency)
	}()

	// Build where filter based on search type
	var docType sqlitevec.DocType
	switch params.Type {
	case "observations":
		docType = sqlitevec.DocTypeObservation
	case "sessions":
		docType = sqlitevec.DocTypeSessionSummary
	case "prompts":
		docType = sqlitevec.DocTypeUserPrompt
	}
	where := sqlitevec.BuildWhereFilter(docType, params.Project)

	// Query sqlite-vec
	vectorResults, err := m.vectorClient.Query(ctx, params.Query, params.Limit*2, where)
	if err != nil {
		atomic.AddInt64(&m.metrics.SearchErrors, 1)
		// Fall back to filter search on error
		return m.filterSearch(ctx, params)
	}

	// Extract IDs grouped by document type using shared helper
	extracted := sqlitevec.ExtractIDsByDocType(vectorResults)
	obsIDs := extracted.ObservationIDs
	summaryIDs := extracted.SummaryIDs
	promptIDs := extracted.PromptIDs

	// Fetch full records from SQLite
	var results []SearchResult

	if len(obsIDs) > 0 && (params.Type == "" || params.Type == "observations") {
		obs, err := m.observationStore.GetObservationsByIDs(ctx, obsIDs, params.OrderBy, 0)
		if err != nil {
			log.Warn().Err(err).Int("count", len(obsIDs)).Msg("Failed to fetch observations by IDs in vector search")
		} else {
			for _, o := range obs {
				// Skip superseded observations when requested
				if params.ExcludeSuperseded && o.IsSuperseded {
					continue
				}
				results = append(results, m.observationToResult(o, params.Format))
			}
		}
	}

	if len(summaryIDs) > 0 && (params.Type == "" || params.Type == "sessions") {
		summaries, err := m.summaryStore.GetSummariesByIDs(ctx, summaryIDs, params.OrderBy, 0)
		if err != nil {
			log.Warn().Err(err).Int("count", len(summaryIDs)).Msg("Failed to fetch summaries by IDs in vector search")
		} else {
			for _, s := range summaries {
				results = append(results, m.summaryToResult(s, params.Format))
			}
		}
	}

	if len(promptIDs) > 0 && (params.Type == "" || params.Type == "prompts") {
		prompts, err := m.promptStore.GetPromptsByIDs(ctx, promptIDs, params.OrderBy, 0)
		if err != nil {
			log.Warn().Err(err).Int("count", len(promptIDs)).Msg("Failed to fetch prompts by IDs in vector search")
		} else {
			for _, p := range prompts {
				results = append(results, m.promptToResult(p, params.Format))
			}
		}
	}

	// Apply limit
	if len(results) > params.Limit {
		results = results[:params.Limit]
	}

	return &UnifiedSearchResult{
		Results:    results,
		TotalCount: len(results),
		Query:      params.Query,
	}, nil
}

// filterSearch performs structured filter search via SQLite.
func (m *Manager) filterSearch(ctx context.Context, params SearchParams) (*UnifiedSearchResult, error) {
	start := time.Now()
	defer func() {
		latency := time.Since(start).Nanoseconds()
		atomic.AddInt64(&m.metrics.FilterSearches, 1)
		atomic.AddInt64(&m.metrics.FilterLatencyNs, latency)
	}()

	var results []SearchResult

	// Search observations
	if params.Type == "" || params.Type == "observations" {
		var obs []*models.Observation
		var err error

		// Use active observations (excluding superseded) when requested
		if params.ExcludeSuperseded {
			obs, err = m.observationStore.GetActiveObservations(ctx, params.Project, params.Limit)
		} else {
			obs, err = m.observationStore.GetRecentObservations(ctx, params.Project, params.Limit)
		}

		if err != nil {
			log.Warn().Err(err).Str("project", params.Project).Msg("Failed to fetch observations in filter search")
		} else {
			for _, o := range obs {
				results = append(results, m.observationToResult(o, params.Format))
			}
		}
	}

	// Search summaries
	if params.Type == "" || params.Type == "sessions" {
		summaries, err := m.summaryStore.GetRecentSummaries(ctx, params.Project, params.Limit)
		if err != nil {
			log.Warn().Err(err).Str("project", params.Project).Msg("Failed to fetch summaries in filter search")
		} else {
			for _, s := range summaries {
				results = append(results, m.summaryToResult(s, params.Format))
			}
		}
	}

	// Apply limit
	if len(results) > params.Limit {
		results = results[:params.Limit]
	}

	return &UnifiedSearchResult{
		Results:    results,
		TotalCount: len(results),
	}, nil
}

// Decisions performs a semantic search optimized for finding decisions.
func (m *Manager) Decisions(ctx context.Context, params SearchParams) (*UnifiedSearchResult, error) {
	// Boost query with decision-related keywords
	if params.Query != "" {
		params.Query = params.Query + " decision chose architecture"
	}
	params.Type = "observations"
	return m.UnifiedSearch(ctx, params)
}

// Changes performs a semantic search optimized for finding code changes.
func (m *Manager) Changes(ctx context.Context, params SearchParams) (*UnifiedSearchResult, error) {
	// Boost query with change-related keywords
	if params.Query != "" {
		params.Query = params.Query + " changed modified refactored"
	}
	params.Type = "observations"
	return m.UnifiedSearch(ctx, params)
}

// HowItWorks performs a semantic search optimized for understanding architecture.
func (m *Manager) HowItWorks(ctx context.Context, params SearchParams) (*UnifiedSearchResult, error) {
	// Boost query with architecture-related keywords
	if params.Query != "" {
		params.Query = params.Query + " architecture design pattern implements"
	}
	params.Type = "observations"
	return m.UnifiedSearch(ctx, params)
}

// Helper methods

func (m *Manager) observationToResult(obs *models.Observation, format string) SearchResult {
	result := SearchResult{
		Type:      "observation",
		ID:        obs.ID,
		Project:   obs.Project,
		Scope:     string(obs.Scope),
		CreatedAt: obs.CreatedAtEpoch,
		Metadata: map[string]any{
			"obs_type": string(obs.Type),
			"scope":    string(obs.Scope),
		},
	}

	if obs.Title.Valid {
		result.Title = obs.Title.String
	}

	if format == "full" && obs.Narrative.Valid {
		result.Content = obs.Narrative.String
	}

	return result
}

func (m *Manager) summaryToResult(summary *models.SessionSummary, format string) SearchResult {
	result := SearchResult{
		Type:      "session",
		ID:        summary.ID,
		Project:   summary.Project,
		CreatedAt: summary.CreatedAtEpoch,
	}

	if summary.Request.Valid {
		result.Title = truncate(summary.Request.String, titleTruncateLen)
	}

	if format == "full" && summary.Learned.Valid {
		result.Content = summary.Learned.String
	}

	return result
}

func (m *Manager) promptToResult(prompt *models.UserPromptWithSession, format string) SearchResult {
	result := SearchResult{
		Type:      "prompt",
		ID:        prompt.ID,
		Project:   prompt.Project,
		CreatedAt: prompt.CreatedAtEpoch,
	}

	result.Title = truncate(prompt.PromptText, titleTruncateLen)

	if format == "full" {
		result.Content = prompt.PromptText
	}

	return result
}

func truncate(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
