package hybrid

import (
	"context"
	"sync"
	"time"

	"github.com/lukaszraczylo/claude-mnemonic/internal/vector/sqlitevec"
	"github.com/rs/zerolog/log"
)

// AutoTuner dynamically adjusts hub threshold based on query performance
type AutoTuner struct {
	ctx           context.Context
	client        *Client
	cancel        context.CancelFunc
	latencies     []time.Duration
	wg            sync.WaitGroup
	queries       int64
	targetLatency time.Duration
	adjustPeriod  time.Duration
	minThreshold  int
	maxThreshold  int
	adjustments   int
	latenciesMu   sync.Mutex
}

// AutoTunerConfig configures the auto-tuner
type AutoTunerConfig struct {
	TargetLatency time.Duration // Target p95 latency (default: 50ms)
	MinThreshold  int           // Min hub threshold (default: 2)
	MaxThreshold  int           // Max hub threshold (default: 20)
	AdjustPeriod  time.Duration // Adjustment frequency (default: 5min)
}

// DefaultAutoTunerConfig returns sensible defaults
func DefaultAutoTunerConfig() AutoTunerConfig {
	return AutoTunerConfig{
		TargetLatency: 50 * time.Millisecond,
		MinThreshold:  2,
		MaxThreshold:  20,
		AdjustPeriod:  5 * time.Minute,
	}
}

// NewAutoTuner creates a new auto-tuner for the hybrid client
func NewAutoTuner(client *Client, cfg AutoTunerConfig) *AutoTuner {
	ctx, cancel := context.WithCancel(context.Background())

	tuner := &AutoTuner{
		client:        client,
		targetLatency: cfg.TargetLatency,
		minThreshold:  cfg.MinThreshold,
		maxThreshold:  cfg.MaxThreshold,
		adjustPeriod:  cfg.AdjustPeriod,
		latencies:     make([]time.Duration, 0, 1000),
		ctx:           ctx,
		cancel:        cancel,
	}

	return tuner
}

// Start begins auto-tuning in the background
func (a *AutoTuner) Start() {
	a.wg.Add(1)
	go a.tuningLoop()

	log.Info().
		Dur("target_latency", a.targetLatency).
		Int("min_threshold", a.minThreshold).
		Int("max_threshold", a.maxThreshold).
		Dur("adjust_period", a.adjustPeriod).
		Msg("Auto-tuner started")
}

// Stop stops the auto-tuner
func (a *AutoTuner) Stop() {
	a.cancel()
	a.wg.Wait()
	log.Info().Msg("Auto-tuner stopped")
}

// RecordQuery records a query latency for analysis
func (a *AutoTuner) RecordQuery(latency time.Duration) {
	a.latenciesMu.Lock()
	defer a.latenciesMu.Unlock()

	a.queries++
	a.latencies = append(a.latencies, latency)

	// Keep only recent queries (last 1000)
	if len(a.latencies) > 1000 {
		a.latencies = a.latencies[len(a.latencies)-1000:]
	}
}

// tuningLoop periodically adjusts hub threshold
func (a *AutoTuner) tuningLoop() {
	defer a.wg.Done()

	ticker := time.NewTicker(a.adjustPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-a.ctx.Done():
			return

		case <-ticker.C:
			a.adjustThreshold()
		}
	}
}

// adjustThreshold analyzes recent queries and adjusts hub threshold
func (a *AutoTuner) adjustThreshold() {
	a.latenciesMu.Lock()
	defer a.latenciesMu.Unlock()

	if len(a.latencies) < 10 {
		// Not enough data yet
		return
	}

	// Calculate p95 latency
	p95 := calculateP95(a.latencies)

	currentThreshold := a.client.hubThreshold

	log.Debug().
		Dur("p95_latency", p95).
		Dur("target_latency", a.targetLatency).
		Int("current_threshold", currentThreshold).
		Int("queries", len(a.latencies)).
		Msg("Auto-tuner evaluating performance")

	// Determine adjustment direction
	var newThreshold int

	if p95 > a.targetLatency {
		// Too slow - lower threshold (more hubs = faster queries)
		adjustment := calculateAdjustment(p95, a.targetLatency)
		newThreshold = currentThreshold - adjustment

		if newThreshold < a.minThreshold {
			newThreshold = a.minThreshold
		}

		log.Info().
			Dur("p95", p95).
			Int("old_threshold", currentThreshold).
			Int("new_threshold", newThreshold).
			Msg("Auto-tuner: Lowering hub threshold (too slow)")

	} else if p95 < a.targetLatency*8/10 {
		// Too fast - raise threshold (fewer hubs = more savings)
		// Only adjust if significantly faster (20% margin)
		adjustment := calculateAdjustment(a.targetLatency, p95)
		newThreshold = currentThreshold + adjustment

		if newThreshold > a.maxThreshold {
			newThreshold = a.maxThreshold
		}

		log.Info().
			Dur("p95", p95).
			Int("old_threshold", currentThreshold).
			Int("new_threshold", newThreshold).
			Msg("Auto-tuner: Raising hub threshold (room for savings)")

	} else {
		// Within acceptable range, no adjustment needed
		log.Debug().
			Dur("p95", p95).
			Int("threshold", currentThreshold).
			Msg("Auto-tuner: Performance acceptable, no adjustment")
		return
	}

	// Apply adjustment
	if newThreshold != currentThreshold {
		a.client.hubThreshold = newThreshold
		a.adjustments++

		// Clear latency history after adjustment
		a.latencies = make([]time.Duration, 0, 1000)

		log.Info().
			Int("threshold", newThreshold).
			Int("total_adjustments", a.adjustments).
			Msg("Hub threshold adjusted by auto-tuner")
	}
}

// calculateP95 computes the 95th percentile latency
func calculateP95(latencies []time.Duration) time.Duration {
	if len(latencies) == 0 {
		return 0
	}

	// Sort latencies
	sorted := make([]time.Duration, len(latencies))
	copy(sorted, latencies)

	// Simple bubble sort (small dataset)
	n := len(sorted)
	for i := 0; i < n-1; i++ {
		for j := 0; j < n-i-1; j++ {
			if sorted[j] > sorted[j+1] {
				sorted[j], sorted[j+1] = sorted[j+1], sorted[j]
			}
		}
	}

	// Return 95th percentile
	idx := int(float64(len(sorted)) * 0.95)
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}

	return sorted[idx]
}

// calculateAdjustment determines how much to adjust threshold
func calculateAdjustment(actual, target time.Duration) int {
	// Calculate percentage difference
	diff := float64(actual-target) / float64(target)

	// Adjust more aggressively for larger differences
	if diff > 0.5 || diff < -0.5 {
		return 3 // Large adjustment
	} else if diff > 0.2 || diff < -0.2 {
		return 2 // Medium adjustment
	}

	return 1 // Small adjustment
}

// GetStats returns auto-tuner statistics
func (a *AutoTuner) GetStats() AutoTunerStats {
	a.latenciesMu.Lock()
	defer a.latenciesMu.Unlock()

	stats := AutoTunerStats{
		CurrentThreshold: a.client.hubThreshold,
		TargetLatency:    a.targetLatency,
		TotalQueries:     a.queries,
		TotalAdjustments: a.adjustments,
		RecentQueries:    len(a.latencies),
	}

	if len(a.latencies) > 0 {
		stats.P95Latency = calculateP95(a.latencies)

		// Calculate average
		var total time.Duration
		for _, lat := range a.latencies {
			total += lat
		}
		stats.AvgLatency = total / time.Duration(len(a.latencies))
	}

	return stats
}

// AutoTunerStats contains auto-tuner statistics
type AutoTunerStats struct {
	CurrentThreshold int
	TargetLatency    time.Duration
	P95Latency       time.Duration
	AvgLatency       time.Duration
	TotalQueries     int64
	TotalAdjustments int
	RecentQueries    int
}

// AutoTunedClient wraps Client with automatic performance tuning
type AutoTunedClient struct {
	*Client
	tuner *AutoTuner
}

// Query wraps the underlying Query call with latency tracking
func (a *AutoTunedClient) Query(ctx context.Context, query string, limit int, where map[string]any) ([]sqlitevec.QueryResult, error) {
	start := time.Now()
	results, err := a.Client.Query(ctx, query, limit, where)
	latency := time.Since(start)

	a.tuner.RecordQuery(latency)

	return results, err
}

// WithAutoTuning wraps a hybrid client with auto-tuning enabled
func WithAutoTuning(client *Client, cfg AutoTunerConfig) *AutoTunedClient {
	tuner := NewAutoTuner(client, cfg)
	tuner.Start()

	return &AutoTunedClient{
		Client: client,
		tuner:  tuner,
	}
}

// Stop stops the auto-tuner
func (a *AutoTunedClient) StopTuning() {
	a.tuner.Stop()
}
