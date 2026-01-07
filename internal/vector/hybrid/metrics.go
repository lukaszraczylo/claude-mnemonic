package hybrid

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Metrics tracks performance and usage statistics for hybrid vector storage
type Metrics struct {
	startTime         time.Time
	recentLatencies   []time.Duration
	latenciesMu       sync.Mutex
	totalQueries      atomic.Int64
	hubOnlyQueries    atomic.Int64
	hybridQueries     atomic.Int64
	onDemandQueries   atomic.Int64
	graphQueries      atomic.Int64
	totalLatency      atomic.Int64 // Sum in microseconds
	hubLatency        atomic.Int64
	recomputeLatency  atomic.Int64
	totalDocuments    atomic.Int64
	hubDocuments      atomic.Int64
	storedEmbeddings  atomic.Int64
	recomputedCount   atomic.Int64
	cacheHits         atomic.Int64
	cacheMisses       atomic.Int64
	graphTraversals   atomic.Int64
	avgTraversalDepth atomic.Int64
}

// NewMetrics creates a new metrics tracker
func NewMetrics() *Metrics {
	return &Metrics{
		recentLatencies: make([]time.Duration, 0, 1000),
		startTime:       time.Now(),
	}
}

// RecordQuery records a query execution
func (m *Metrics) RecordQuery(queryType string, latency time.Duration, recomputed int) {
	m.totalQueries.Add(1)
	m.totalLatency.Add(latency.Microseconds())

	switch queryType {
	case "hub_only":
		m.hubOnlyQueries.Add(1)
	case "hybrid":
		m.hybridQueries.Add(1)
	case "on_demand":
		m.onDemandQueries.Add(1)
	case "graph":
		m.graphQueries.Add(1)
	}

	if recomputed > 0 {
		m.recomputedCount.Add(int64(recomputed))
	}

	// Track recent latencies
	m.latenciesMu.Lock()
	m.recentLatencies = append(m.recentLatencies, latency)
	if len(m.recentLatencies) > 1000 {
		m.recentLatencies = m.recentLatencies[len(m.recentLatencies)-1000:]
	}
	m.latenciesMu.Unlock()
}

// RecordHubLatency records time spent in hub search
func (m *Metrics) RecordHubLatency(latency time.Duration) {
	m.hubLatency.Add(latency.Microseconds())
}

// RecordRecomputeLatency records time spent recomputing embeddings
func (m *Metrics) RecordRecomputeLatency(latency time.Duration) {
	m.recomputeLatency.Add(latency.Microseconds())
}

// RecordCacheHit records a content cache hit
func (m *Metrics) RecordCacheHit() {
	m.cacheHits.Add(1)
}

// RecordCacheMiss records a content cache miss
func (m *Metrics) RecordCacheMiss() {
	m.cacheMisses.Add(1)
}

// RecordGraphTraversal records a graph traversal operation
func (m *Metrics) RecordGraphTraversal(depth int) {
	m.graphTraversals.Add(1)
	m.avgTraversalDepth.Add(int64(depth))
}

// UpdateStorageStats updates current storage statistics
func (m *Metrics) UpdateStorageStats(total, hubs, stored int) {
	m.totalDocuments.Store(int64(total))
	m.hubDocuments.Store(int64(hubs))
	m.storedEmbeddings.Store(int64(stored))
}

// GetSnapshot returns current metrics snapshot
func (m *Metrics) GetSnapshot() MetricsSnapshot {
	m.latenciesMu.Lock()
	defer m.latenciesMu.Unlock()

	totalQueries := m.totalQueries.Load()

	snapshot := MetricsSnapshot{
		// Query counts
		TotalQueries:    totalQueries,
		HubOnlyQueries:  m.hubOnlyQueries.Load(),
		HybridQueries:   m.hybridQueries.Load(),
		OnDemandQueries: m.onDemandQueries.Load(),
		GraphQueries:    m.graphQueries.Load(),

		// Storage
		TotalDocuments:   int(m.totalDocuments.Load()),
		HubDocuments:     int(m.hubDocuments.Load()),
		StoredEmbeddings: int(m.storedEmbeddings.Load()),
		RecomputedTotal:  m.recomputedCount.Load(),

		// Cache
		CacheHits:   m.cacheHits.Load(),
		CacheMisses: m.cacheMisses.Load(),

		// Graph
		GraphTraversals: m.graphTraversals.Load(),

		// Runtime
		Uptime: time.Since(m.startTime),
	}

	// Calculate latencies
	if totalQueries > 0 {
		snapshot.AvgLatency = time.Duration(m.totalLatency.Load()/totalQueries) * time.Microsecond
		snapshot.AvgHubLatency = time.Duration(m.hubLatency.Load()/totalQueries) * time.Microsecond
	}

	if m.recomputedCount.Load() > 0 {
		snapshot.AvgRecomputeLatency = time.Duration(m.recomputeLatency.Load()/m.recomputedCount.Load()) * time.Microsecond
	}

	// Calculate percentiles
	if len(m.recentLatencies) > 0 {
		sorted := make([]time.Duration, len(m.recentLatencies))
		copy(sorted, m.recentLatencies)
		sortDurations(sorted)

		snapshot.P50Latency = percentile(sorted, 0.50)
		snapshot.P95Latency = percentile(sorted, 0.95)
		snapshot.P99Latency = percentile(sorted, 0.99)
	}

	// Calculate cache hit rate
	totalCacheOps := snapshot.CacheHits + snapshot.CacheMisses
	if totalCacheOps > 0 {
		snapshot.CacheHitRate = float64(snapshot.CacheHits) / float64(totalCacheOps)
	}

	// Calculate storage savings
	if snapshot.TotalDocuments > 0 {
		embeddingSize := 384 * 4 // 384 dims × 4 bytes
		fullStorage := snapshot.TotalDocuments * embeddingSize
		actualStorage := snapshot.StoredEmbeddings * embeddingSize

		if fullStorage > 0 {
			snapshot.StorageSavingsPercent = (1.0 - float64(actualStorage)/float64(fullStorage)) * 100
		}
	}

	// Calculate avg traversal depth
	if snapshot.GraphTraversals > 0 {
		snapshot.AvgTraversalDepth = float64(m.avgTraversalDepth.Load()) / float64(snapshot.GraphTraversals)
	}

	return snapshot
}

// MetricsSnapshot represents a point-in-time metrics snapshot
type MetricsSnapshot struct {
	// Query metrics
	TotalQueries    int64
	HubOnlyQueries  int64
	HybridQueries   int64
	OnDemandQueries int64
	GraphQueries    int64

	// Latency metrics
	AvgLatency          time.Duration
	P50Latency          time.Duration
	P95Latency          time.Duration
	P99Latency          time.Duration
	AvgHubLatency       time.Duration
	AvgRecomputeLatency time.Duration

	// Storage metrics
	TotalDocuments        int
	HubDocuments          int
	StoredEmbeddings      int
	StorageSavingsPercent float64
	RecomputedTotal       int64

	// Cache metrics
	CacheHits    int64
	CacheMisses  int64
	CacheHitRate float64

	// Graph metrics
	GraphTraversals   int64
	AvgTraversalDepth float64

	// Runtime
	Uptime time.Duration
}

// sortDurations sorts a slice of durations in ascending order
func sortDurations(durations []time.Duration) {
	n := len(durations)
	for i := 0; i < n-1; i++ {
		for j := 0; j < n-i-1; j++ {
			if durations[j] > durations[j+1] {
				durations[j], durations[j+1] = durations[j+1], durations[j]
			}
		}
	}
}

// percentile calculates the Nth percentile from a sorted slice
func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}

	idx := int(float64(len(sorted)) * p)
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}

	return sorted[idx]
}

// String returns a human-readable representation of metrics
func (s MetricsSnapshot) String() string {
	return fmt.Sprintf(`Hybrid Vector Storage Metrics:
  Queries:
    Total: %d (Hub: %d, Hybrid: %d, OnDemand: %d, Graph: %d)
    Avg Latency: %v (p50: %v, p95: %v, p99: %v)
    Hub Latency: %v, Recompute Latency: %v
  Storage:
    Documents: %d (Hubs: %d, %.1f%%)
    Stored Embeddings: %d
    Savings: %.1f%%
    Total Recomputed: %d
  Cache:
    Hits: %d, Misses: %d (Hit Rate: %.1f%%)
  Graph:
    Traversals: %d (Avg Depth: %.2f)
  Runtime: %v`,
		s.TotalQueries, s.HubOnlyQueries, s.HybridQueries, s.OnDemandQueries, s.GraphQueries,
		s.AvgLatency, s.P50Latency, s.P95Latency, s.P99Latency,
		s.AvgHubLatency, s.AvgRecomputeLatency,
		s.TotalDocuments, s.HubDocuments, float64(s.HubDocuments)/float64(s.TotalDocuments)*100,
		s.StoredEmbeddings,
		s.StorageSavingsPercent,
		s.RecomputedTotal,
		s.CacheHits, s.CacheMisses, s.CacheHitRate*100,
		s.GraphTraversals, s.AvgTraversalDepth,
		s.Uptime,
	)
}
