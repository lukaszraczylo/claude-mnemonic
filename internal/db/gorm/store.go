// Package gorm provides GORM-based database operations for claude-mnemonic.
package gorm

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"slices"
	"sync"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	sqlite3 "github.com/mattn/go-sqlite3" // SQLite driver with FTS5 support
	"github.com/rs/zerolog/log"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// driverName is the name of the custom mattn/go-sqlite3 driver registered with a
// ConnectHook that applies ALL connection pragmas (correctness + best-effort) to EVERY
// pooled connection at open time. The stock "sqlite3" driver has no hook, so pragmas set
// via a single post-open Exec only reach one arbitrary pooled connection (issue #49, F6).
const driverName = "sqlite3_mnemonic"

// registerDriverOnce guards driver registration so it runs exactly once per process.
// database/sql panics with "sql: Register called twice" on a duplicate name, and NewStore
// may be called multiple times (e.g. after a config-change reinitialization).
var registerDriverOnce sync.Once

// correctnessPragmas MUST succeed on every connection: getting any of them wrong changes
// transactional/locking semantics, not just performance. A failure here aborts the open.
var correctnessPragmas = []string{
	"PRAGMA foreign_keys=ON",
	"PRAGMA journal_mode=WAL",
	"PRAGMA synchronous=NORMAL",
	"PRAGMA busy_timeout=5000",
	"PRAGMA cache_size=-64000",
}

// bestEffortPragmas are per-connection or database-wide optimizations. A failure is logged
// and tolerated: the connection is still correct, just less tuned. (page_size only takes
// effect on an empty database / next VACUUM, but applying it per-connection is harmless.)
var bestEffortPragmas = []string{
	"PRAGMA temp_store=MEMORY",          // Store temp tables in memory
	"PRAGMA mmap_size=268435456",        // 256MB memory-mapped I/O
	"PRAGMA page_size=4096",             // 4KB pages (optimal for most systems)
	"PRAGMA wal_autocheckpoint=1000",    // Auto-checkpoint (PASSIVE) every 1000 WAL frames
	"PRAGMA journal_size_limit=8388608", // Backstop: cap -wal at 8MiB (issue #49)
}

// connectHook applies all pragmas to a freshly opened connection. mattn/go-sqlite3 calls
// it at the very end of Open, after DSN params and extensions, so it is authoritative.
func connectHook(c *sqlite3.SQLiteConn) error {
	for _, pragma := range correctnessPragmas {
		if _, err := c.Exec(pragma, nil); err != nil {
			return fmt.Errorf("apply correctness pragma %q: %w", pragma, err)
		}
	}
	for _, pragma := range bestEffortPragmas {
		if _, err := c.Exec(pragma, nil); err != nil {
			log.Warn().Str("pragma", pragma).Err(err).Msg("Failed to set pragma (non-fatal)")
		}
	}
	return nil
}

// registerDriver registers the custom driver once. sqlite_vec.Auto() registers the vec
// extension globally via sqlite3_auto_extension, which applies to connections from any
// sqlite3-based driver, so the new driver still gets vec + FTS5.
func registerDriver() {
	registerDriverOnce.Do(func() {
		sql.Register(driverName, &sqlite3.SQLiteDriver{
			ConnectHook: func(c *sqlite3.SQLiteConn) error {
				return connectHook(c)
			},
		})
	})
}

// Store represents the GORM database connection with sqlite-vec support.
type Store struct {
	healthCacheTime time.Time
	DB              *gorm.DB
	sqlDB           *sql.DB
	metrics         *PoolMetrics
	cachedHealth    *HealthInfo
	path            string
	healthCacheTTL  time.Duration
	healthCacheMu   sync.RWMutex
}

// Config holds database configuration.
type Config struct {
	Path     string          // Path to SQLite database file
	MaxConns int             // Maximum number of open connections (default: 4)
	LogLevel logger.LogLevel // GORM log level (logger.Silent for production)
}

// NewStore creates a new Store with WAL mode enabled and sqlite-vec registered.
// CRITICAL: all connection pragmas (WAL, foreign_keys, busy_timeout, etc.) are applied to
// EVERY pooled connection via a driver ConnectHook (see registerDriver), so the pool is
// uniformly configured and connections may be recycled safely (issue #49, F6).
func NewStore(cfg Config) (*Store, error) {
	// 1. Register sqlite-vec extension (must be done before opening database).
	// sqlite_vec.Auto() uses sqlite3_auto_extension, which is global to all sqlite3-based
	// drivers, so connections from our custom driver also get the vec virtual table.
	sqlite_vec.Auto()

	// 2. Register the custom driver whose ConnectHook applies ALL pragmas to EVERY pooled
	// connection (issue #49, F6). Without this, pragmas set via a single post-open
	// sqlDB.Exec reach only one arbitrary pooled connection. The hook is authoritative.
	registerDriver()

	// 3. Build a minimal DSN. _foreign_keys is kept as belt-and-suspenders (the hook sets
	// it too); all other pragmas are applied per-connection by the ConnectHook, so they no
	// longer need to live in the DSN.
	dsn := cfg.Path + "?_foreign_keys=ON"

	// 4. Open raw database connection with the custom driver (FTS5 + per-connection pragmas).
	sqlDB, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// 5. Wrap with GORM using existing connection
	db, err := gorm.Open(sqlite.Dialector{
		Conn: sqlDB,
	}, &gorm.Config{
		Logger: logger.Default.LogMode(cfg.LogLevel),
		// PrepareStmt enables prepared statement caching for performance
		PrepareStmt: true,
		// Disable default timestamp fields (we manage created_at manually)
		NowFunc: nil,
	})
	if err != nil {
		_ = sqlDB.Close() // Explicitly ignore close error during cleanup
		return nil, fmt.Errorf("open gorm: %w", err)
	}

	// 6. Configure connection pool.
	maxConns := cfg.MaxConns
	if maxConns <= 0 {
		maxConns = 4
	}
	sqlDB.SetMaxOpenConns(maxConns)
	sqlDB.SetMaxIdleConns(maxConns)
	// Finite recycling (issue #49): previously SetConnMaxLifetime(0) meant connections
	// NEVER recycled, so a long-lived read connection could pin an old WAL read-mark for the
	// whole process lifetime and block TRUNCATE checkpoints from reclaiming the -wal file.
	// Recycling is safe now because the ConnectHook reapplies every correctness pragma on
	// each new connection — a recycled connection comes back fully configured, not with
	// defaults. 1h lifetime bounds read-mark staleness without churning the pool; 30m idle
	// time reclaims connections that sit unused (e.g. between sessions) so the pool shrinks
	// back to one warm connection during quiet periods, dropping their WAL read-marks.
	sqlDB.SetConnMaxLifetime(1 * time.Hour)
	sqlDB.SetConnMaxIdleTime(30 * time.Minute)

	// 7. Verify connection
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("ping database: %w", err)
	}

	store := &Store{
		DB:             db,
		sqlDB:          sqlDB,
		path:           cfg.Path,
		metrics:        NewPoolMetrics(100), // Track last 100 latency samples
		healthCacheTTL: 5 * time.Second,     // Cache health checks for 5 seconds
	}

	// 8. Run migrations. All pragmas (correctness + best-effort) are applied per-connection
	// by the ConnectHook at open time, so there is no post-open pragma loop here anymore:
	// such a loop only ever reached one arbitrary pooled connection (issue #49, F6).
	if err := runMigrations(db, sqlDB); err != nil {
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	// 9. Warm the connection pool
	store.WarmPool(maxConns)

	return store, nil
}

// WarmPool pre-creates connections to avoid cold start latency.
func (s *Store) WarmPool(numConns int) {
	if numConns <= 0 {
		numConns = 4
	}

	var wg sync.WaitGroup
	for i := 0; i < numConns; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			conn, err := s.sqlDB.Conn(ctx)
			if err != nil {
				return
			}
			// Execute a simple query to ensure the connection is fully initialized
			_ = conn.PingContext(ctx)
			// Return connection to pool (don't close it)
			_ = conn.Close()
		}()
	}
	wg.Wait()
	log.Debug().Int("connections", numConns).Msg("Connection pool warmed")
}

// Close checkpoints the WAL (TRUNCATE) before closing the connection. Checkpointing on
// shutdown prevents the WAL file from persisting in a large, dirty state across restarts
// and config-change reinitializations, which otherwise leaves a multi-megabyte -wal file
// on disk (issue #49). The checkpoint is best-effort: a failure is logged, not fatal.
func (s *Store) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.Checkpoint(ctx); err != nil {
		log.Warn().Err(err).Msg("WAL checkpoint on close failed (non-fatal)")
	}
	return s.sqlDB.Close()
}

// Checkpoint runs a TRUNCATE WAL checkpoint: it flushes WAL frames into the main
// database file and shrinks the -wal file back to zero. Unlike a PASSIVE checkpoint
// (which never truncates the file and is all SQLite's auto-checkpoint ever performs), a
// TRUNCATE checkpoint reclaims disk and is the mechanism that bounds WAL growth.
// It waits up to the connection busy_timeout for the write lock and returns an error
// rather than blocking indefinitely.
func (s *Store) Checkpoint(ctx context.Context) error {
	if _, err := s.sqlDB.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		return fmt.Errorf("wal checkpoint (truncate): %w", err)
	}
	return nil
}

// WALSize returns the size in bytes of the SQLite WAL sidecar file (<db>-wal), or 0 if
// it does not exist or cannot be stat'd. Used to decide when a checkpoint is worthwhile.
func (s *Store) WALSize() int64 {
	if s.path == "" {
		return 0
	}
	info, err := os.Stat(s.path + "-wal")
	if err != nil {
		return 0
	}
	return info.Size()
}

// CheckpointIfLarge performs a TRUNCATE checkpoint only when the WAL file has grown to or
// beyond threshold bytes. Returns true if a checkpoint was performed. This keeps the
// periodic checkpoint cheap: it does no work while the WAL is small.
func (s *Store) CheckpointIfLarge(ctx context.Context, threshold int64) (bool, error) {
	if s.WALSize() < threshold {
		return false, nil
	}
	return true, s.Checkpoint(ctx)
}

// Ping verifies the database connection is alive.
func (s *Store) Ping() error {
	return s.sqlDB.Ping()
}

// GetRawDB returns the underlying *sql.DB for operations GORM can't handle.
// Use this for:
// - FTS5 full-text search queries (MATCH operator)
// - sqlite-vec vector operations
// - Complex raw SQL queries
func (s *Store) GetRawDB() *sql.DB {
	return s.sqlDB
}

// GetDB returns the GORM DB instance for standard queries.
func (s *Store) GetDB() *gorm.DB {
	return s.DB
}

// Stats returns database connection pool statistics.
func (s *Store) Stats() sql.DBStats {
	return s.sqlDB.Stats()
}

// Optimize runs VACUUM and ANALYZE to optimize the database.
// Should be called periodically (e.g., daily) during low activity.
func (s *Store) Optimize(ctx context.Context) error {
	log.Info().Msg("Starting database optimization")
	start := time.Now()

	// ANALYZE updates statistics for query optimizer
	if _, err := s.sqlDB.ExecContext(ctx, "ANALYZE"); err != nil {
		return fmt.Errorf("analyze: %w", err)
	}

	// PRAGMA optimize runs optimization based on query statistics
	if _, err := s.sqlDB.ExecContext(ctx, "PRAGMA optimize"); err != nil {
		log.Warn().Err(err).Msg("PRAGMA optimize failed (non-fatal)")
	}

	// TRUNCATE WAL checkpoint — reclaims the -wal file during low-activity optimization.
	// (PASSIVE never shrinks the file, so it cannot bound WAL growth — see issue #49.)
	if err := s.Checkpoint(ctx); err != nil {
		log.Warn().Err(err).Msg("WAL checkpoint failed (non-fatal)")
	}

	log.Info().Dur("duration", time.Since(start)).Msg("Database optimization complete")
	return nil
}

// HealthCheck performs a comprehensive health check with latency measurement.
// Returns detailed health information including connection pool stats and query latency.
// Results are cached for healthCacheTTL (default 5 seconds) to reduce database load
// from frequent monitoring calls.
func (s *Store) HealthCheck(ctx context.Context) *HealthInfo {
	// Fast path: check cache with read lock
	s.healthCacheMu.RLock()
	if s.cachedHealth != nil && time.Since(s.healthCacheTime) < s.healthCacheTTL {
		cached := s.cachedHealth
		s.healthCacheMu.RUnlock()
		return cached
	}
	s.healthCacheMu.RUnlock()

	// Slow path: perform actual health check
	info := s.performHealthCheck(ctx)

	// Cache the result
	s.healthCacheMu.Lock()
	s.cachedHealth = info
	s.healthCacheTime = time.Now()
	s.healthCacheMu.Unlock()

	return info
}

// HealthCheckForce performs a health check bypassing the cache.
// Use this when you need real-time health data (e.g., debugging, alerting).
func (s *Store) HealthCheckForce(ctx context.Context) *HealthInfo {
	info := s.performHealthCheck(ctx)

	// Update the cache with fresh data
	s.healthCacheMu.Lock()
	s.cachedHealth = info
	s.healthCacheTime = time.Now()
	s.healthCacheMu.Unlock()

	return info
}

// performHealthCheck does the actual health check work.
func (s *Store) performHealthCheck(ctx context.Context) *HealthInfo {
	info := &HealthInfo{
		Status:    "healthy",
		Timestamp: time.Now(),
	}

	// Check pool stats
	stats := s.sqlDB.Stats()
	info.PoolStats = PoolStats{
		OpenConnections:   stats.OpenConnections,
		InUse:             stats.InUse,
		Idle:              stats.Idle,
		WaitCount:         stats.WaitCount,
		WaitDuration:      stats.WaitDuration,
		MaxIdleClosed:     stats.MaxIdleClosed,
		MaxLifetimeClosed: stats.MaxLifetimeClosed,
	}

	// Record pool stats for metrics tracking
	if s.metrics != nil {
		s.metrics.RecordPoolStats(stats)
	}

	// Measure query latency with a simple SELECT
	start := time.Now()
	var dummy int
	err := s.sqlDB.QueryRowContext(ctx, "SELECT 1").Scan(&dummy)
	info.QueryLatency = time.Since(start)

	// Record latency for historical tracking
	if s.metrics != nil {
		s.metrics.RecordLatency(info.QueryLatency)
		info.HistoricalMetrics = s.metrics.GetMetricsSummary()
	}

	if err != nil {
		info.Status = "unhealthy"
		info.Error = err.Error()
		return info
	}

	// Check for connection saturation (degraded if pool is heavily used)
	if stats.InUse > 0 && float64(stats.InUse)/float64(stats.OpenConnections) > 0.8 {
		info.Status = "degraded"
		info.Warning = "Connection pool heavily utilized"
	}

	// Check for wait contention
	if stats.WaitCount > 100 && stats.WaitDuration > 100*time.Millisecond {
		info.Status = "degraded"
		info.Warning = "Connection pool contention detected"
	}

	// Check query latency (warn if > 10ms for simple query)
	if info.QueryLatency > 10*time.Millisecond {
		if info.Status == "healthy" {
			info.Status = "degraded"
		}
		info.Warning = fmt.Sprintf("Slow query latency: %v", info.QueryLatency)
	}

	// Check historical latency trend (degraded if P95 is high)
	if s.metrics != nil && info.HistoricalMetrics.P95Latency > 50*time.Millisecond {
		if info.Status == "healthy" {
			info.Status = "degraded"
		}
		info.Warning = fmt.Sprintf("High P95 latency: %v", info.HistoricalMetrics.P95Latency)
	}

	return info
}

// HealthInfo contains database health check results.
type HealthInfo struct {
	Timestamp         time.Time      `json:"timestamp"`
	Status            string         `json:"status"`
	Error             string         `json:"error,omitempty"`
	Warning           string         `json:"warning,omitempty"`
	HistoricalMetrics MetricsSummary `json:"historical_metrics,omitempty"`
	PoolStats         PoolStats      `json:"pool_stats"`
	QueryLatency      time.Duration  `json:"query_latency_ns"`
}

// PoolStats contains connection pool statistics.
type PoolStats struct {
	OpenConnections   int           `json:"open_connections"`
	InUse             int           `json:"in_use"`
	Idle              int           `json:"idle"`
	WaitCount         int64         `json:"wait_count"`
	WaitDuration      time.Duration `json:"wait_duration_ns"`
	MaxIdleClosed     int64         `json:"max_idle_closed"`
	MaxLifetimeClosed int64         `json:"max_lifetime_closed"`
}

// QueryTimeout constants for different query types.
const (
	// DefaultQueryTimeout is the default timeout for regular queries.
	DefaultQueryTimeout = 5 * time.Second
	// FastQueryTimeout is for queries that should be very fast (health checks, etc).
	FastQueryTimeout = 1 * time.Second
	// SlowQueryTimeout is for queries that may take longer (bulk operations, rebuilds).
	SlowQueryTimeout = 30 * time.Second
)

// PoolMetrics tracks historical connection pool metrics with a sliding window.
type PoolMetrics struct {
	lastSampleTime time.Time
	latencySamples []time.Duration
	latencyIdx     int
	latencyCount   int
	totalQueries   int64
	totalWaitTime  time.Duration
	peakInUse      int
	peakWaitCount  int64
	windowSize     int
	mu             sync.RWMutex
}

// NewPoolMetrics creates a new pool metrics collector with the given window size.
func NewPoolMetrics(windowSize int) *PoolMetrics {
	if windowSize <= 0 {
		windowSize = 100 // Default: track last 100 samples
	}
	return &PoolMetrics{
		latencySamples: make([]time.Duration, windowSize),
		windowSize:     windowSize,
		lastSampleTime: time.Now(),
	}
}

// RecordLatency records a query latency sample.
func (m *PoolMetrics) RecordLatency(latency time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.latencySamples[m.latencyIdx] = latency
	m.latencyIdx = (m.latencyIdx + 1) % m.windowSize
	if m.latencyCount < m.windowSize {
		m.latencyCount++
	}
	m.totalQueries++
	m.lastSampleTime = time.Now()
}

// RecordPoolStats records pool statistics for peak tracking.
func (m *PoolMetrics) RecordPoolStats(stats sql.DBStats) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if stats.InUse > m.peakInUse {
		m.peakInUse = stats.InUse
	}
	if stats.WaitCount > m.peakWaitCount {
		m.peakWaitCount = stats.WaitCount
	}
	m.totalWaitTime += stats.WaitDuration
}

// GetMetricsSummary returns a summary of collected metrics.
func (m *PoolMetrics) GetMetricsSummary() MetricsSummary {
	m.mu.RLock()
	defer m.mu.RUnlock()

	summary := MetricsSummary{
		TotalQueries:   m.totalQueries,
		SampleCount:    m.latencyCount,
		PeakInUse:      m.peakInUse,
		PeakWaitCount:  m.peakWaitCount,
		TotalWaitTime:  m.totalWaitTime,
		LastSampleTime: m.lastSampleTime,
	}

	if m.latencyCount == 0 {
		return summary
	}

	// Calculate latency statistics
	var total time.Duration
	var min, max time.Duration = m.latencySamples[0], m.latencySamples[0]

	for i := 0; i < m.latencyCount; i++ {
		sample := m.latencySamples[i]
		total += sample
		if sample < min {
			min = sample
		}
		if sample > max {
			max = sample
		}
	}

	summary.AvgLatency = total / time.Duration(m.latencyCount)
	summary.MinLatency = min
	summary.MaxLatency = max

	// Calculate P95 latency (approximate using sorted samples)
	if m.latencyCount >= 20 {
		// Copy samples for sorting
		samples := make([]time.Duration, m.latencyCount)
		copy(samples, m.latencySamples[:m.latencyCount])
		// Use slices.Sort for O(n log n) instead of O(n²) insertion sort
		slices.Sort(samples)
		p95Idx := int(float64(len(samples)) * 0.95)
		summary.P95Latency = samples[p95Idx]
	}

	return summary
}

// MetricsSummary contains aggregated pool metrics.
type MetricsSummary struct {
	LastSampleTime time.Time     `json:"last_sample_time"`
	TotalQueries   int64         `json:"total_queries"`
	SampleCount    int           `json:"sample_count"`
	AvgLatency     time.Duration `json:"avg_latency_ns"`
	MinLatency     time.Duration `json:"min_latency_ns"`
	MaxLatency     time.Duration `json:"max_latency_ns"`
	P95Latency     time.Duration `json:"p95_latency_ns,omitempty"`
	PeakInUse      int           `json:"peak_in_use"`
	PeakWaitCount  int64         `json:"peak_wait_count"`
	TotalWaitTime  time.Duration `json:"total_wait_time_ns"`
}

// GetMetrics returns the current metrics without performing a health check.
func (s *Store) GetMetrics() MetricsSummary {
	if s.metrics == nil {
		return MetricsSummary{}
	}
	return s.metrics.GetMetricsSummary()
}

// ResetMetrics resets the metrics collector (useful for testing or after major changes).
func (s *Store) ResetMetrics() {
	if s.metrics != nil {
		s.metrics = NewPoolMetrics(s.metrics.windowSize)
	}
}

// WithTimeout wraps a context with the given timeout and logs slow queries.
// Returns the wrapped context and a cancel function that should be called when done.
func (s *Store) WithTimeout(ctx context.Context, timeout time.Duration, operation string) (context.Context, context.CancelFunc) {
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	start := time.Now()

	// Return wrapped cancel that logs if query was slow
	return timeoutCtx, func() {
		elapsed := time.Since(start)
		cancel()

		// Log slow queries (> 100ms)
		if elapsed > 100*time.Millisecond {
			log.Warn().
				Str("operation", operation).
				Dur("elapsed", elapsed).
				Dur("timeout", timeout).
				Msg("Slow database operation")
		}
	}
}

// ExecWithTimeout executes a raw SQL query with timeout.
// Returns error if query takes longer than timeout.
func (s *Store) ExecWithTimeout(ctx context.Context, timeout time.Duration, query string, args ...any) error {
	timeoutCtx, cancel := s.WithTimeout(ctx, timeout, "exec")
	defer cancel()

	_, err := s.sqlDB.ExecContext(timeoutCtx, query, args...)
	if err != nil {
		if err == context.DeadlineExceeded {
			return fmt.Errorf("query timeout after %v: %s", timeout, query)
		}
		return err
	}
	return nil
}

// TransactionWithTimeout wraps a transaction function with timeout handling.
// The transaction is automatically rolled back if the context times out.
func (s *Store) TransactionWithTimeout(ctx context.Context, timeout time.Duration, fn func(*gorm.DB) error) error {
	timeoutCtx, cancel := s.WithTimeout(ctx, timeout, "transaction")
	defer cancel()

	return s.DB.WithContext(timeoutCtx).Transaction(func(tx *gorm.DB) error {
		// Check context before proceeding
		select {
		case <-timeoutCtx.Done():
			return timeoutCtx.Err()
		default:
		}
		return fn(tx)
	})
}
