//go:build fts5

// Package gorm provides GORM-based database operations for claude-mnemonic.
package gorm

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm/logger"
)

func TestNewStore(t *testing.T) {
	// Create temporary directory for test database
	tmpDir, err := os.MkdirTemp("", "gorm_test_*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")

	// Create store with migrations
	cfg := Config{
		Path:     dbPath,
		MaxConns: 4,
		LogLevel: logger.Silent,
	}

	store, err := NewStore(cfg)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	// Verify connection works
	sqlDB := store.GetRawDB()
	if err := sqlDB.Ping(); err != nil {
		t.Fatalf("ping failed: %v", err)
	}

	// Verify WAL mode is enabled
	var journalMode string
	err = store.DB.Raw("PRAGMA journal_mode").Scan(&journalMode).Error
	if err != nil {
		t.Fatalf("query journal_mode failed: %v", err)
	}
	if journalMode != "wal" {
		t.Errorf("expected WAL mode, got %q", journalMode)
	}

	// Verify core tables exist
	tables := []string{
		"sdk_sessions",
		"observations",
		"session_summaries",
		"user_prompts",
		"observation_conflicts",
		"observation_relations",
		"patterns",
		"concept_weights",
	}

	for _, table := range tables {
		exists := store.DB.Migrator().HasTable(table)
		if !exists {
			t.Errorf("table %q does not exist", table)
		}
	}

	// Verify FTS5 virtual tables exist (cannot use Migrator().HasTable for virtual tables)
	ftsTables := []string{
		"user_prompts_fts",
		"observations_fts",
		"session_summaries_fts",
		"patterns_fts",
	}

	for _, table := range ftsTables {
		var count int
		err := store.DB.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&count).Error
		if err != nil {
			t.Errorf("check FTS table %q failed: %v", table, err)
		}
		if count != 1 {
			t.Errorf("FTS table %q does not exist", table)
		}
	}

	// Verify vectors table exists (virtual table)
	var vectorsCount int
	err = store.DB.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='vectors'").Scan(&vectorsCount).Error
	if err != nil {
		t.Errorf("check vectors table failed: %v", err)
	}
	if vectorsCount != 1 {
		t.Errorf("vectors table does not exist")
	}

	// Verify concept_weights seed data exists
	var conceptCount int64
	store.DB.Model(&ConceptWeight{}).Count(&conceptCount)
	if conceptCount != 12 {
		t.Errorf("expected 12 concept weights, got %d", conceptCount)
	}

	t.Logf("✅ Phase 1 Foundation: All migrations successful")
	t.Logf("   - Core tables: %d", len(tables))
	t.Logf("   - FTS5 tables: %d", len(ftsTables))
	t.Logf("   - Vector table: 1")
	t.Logf("   - Seed data: %d concept weights", conceptCount)
}

func TestMigrationIdempotency(t *testing.T) {
	// Create temporary directory for test database
	tmpDir, err := os.MkdirTemp("", "gorm_idempotency_*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	cfg := Config{
		Path:     dbPath,
		MaxConns: 4,
		LogLevel: logger.Silent,
	}

	// Run migrations first time
	store1, err := NewStore(cfg)
	if err != nil {
		t.Fatalf("NewStore (first) failed: %v", err)
	}
	store1.Close()

	// Run migrations second time (should be idempotent)
	store2, err := NewStore(cfg)
	if err != nil {
		t.Fatalf("NewStore (second) failed: %v", err)
	}
	defer store2.Close()

	// Verify concept_weights seed data is still exactly 12 (INSERT OR IGNORE)
	var conceptCount int64
	store2.DB.Model(&ConceptWeight{}).Count(&conceptCount)
	if conceptCount != 12 {
		t.Errorf("expected 12 concept weights after second migration, got %d", conceptCount)
	}

	t.Logf("✅ Migrations are idempotent")
}

func TestWALAutocheckpoint(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gorm_wal_checkpoint_*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := NewStore(Config{
		Path:     dbPath,
		MaxConns: 2,
		LogLevel: logger.Silent,
	})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	// Verify wal_autocheckpoint is set to 1000
	var checkpoint int
	err = store.GetRawDB().QueryRow("PRAGMA wal_autocheckpoint").Scan(&checkpoint)
	if err != nil {
		t.Fatalf("query wal_autocheckpoint: %v", err)
	}
	if checkpoint != 1000 {
		t.Errorf("expected wal_autocheckpoint=1000, got %d", checkpoint)
	}
}

func TestOptimize_RunsWALCheckpoint(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gorm_optimize_*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := NewStore(Config{
		Path:     dbPath,
		MaxConns: 2,
		LogLevel: logger.Silent,
	})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	// Insert some data to generate WAL frames
	_, err = store.GetRawDB().Exec("INSERT INTO observations (sdk_session_id, title, scope, project, type, created_at, created_at_epoch) VALUES ('test-sess', 'test data', 'project', '/tmp/test', 'decision', '2026-01-01T00:00:00Z', 1735689600)")
	if err != nil {
		t.Fatalf("insert test data: %v", err)
	}

	// Optimize should succeed (includes PASSIVE WAL checkpoint)
	err = store.Optimize(context.Background())
	if err != nil {
		t.Fatalf("Optimize failed: %v", err)
	}
}

func TestOptimize_RespectsContextCancellation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gorm_optimize_cancel_*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := NewStore(Config{
		Path:     dbPath,
		MaxConns: 2,
		LogLevel: logger.Silent,
	})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	// Already-cancelled context should cause Optimize to fail
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = store.Optimize(ctx)
	if err == nil {
		t.Error("expected error with cancelled context, got nil")
	}
}

// growWAL inserts sizeable rows to push the SQLite WAL well past a few hundred KB so
// checkpoint behaviour can be observed. Returns the WAL file size after the inserts.
func growWAL(t *testing.T, store *Store, rows int) int64 {
	t.Helper()
	bigTitle := strings.Repeat("x", 2048)
	for i := 0; i < rows; i++ {
		_, err := store.GetRawDB().Exec(
			"INSERT INTO observations (sdk_session_id, title, scope, project, type, created_at, created_at_epoch) "+
				"VALUES (?, ?, 'project', '/tmp/test', 'decision', '2026-01-01T00:00:00Z', 1735689600)",
			"sess", bigTitle)
		if err != nil {
			t.Fatalf("insert row %d: %v", i, err)
		}
	}
	return store.WALSize()
}

func countObservations(t *testing.T, store *Store) int64 {
	t.Helper()
	var n int64
	if err := store.GetRawDB().QueryRow("SELECT COUNT(*) FROM observations").Scan(&n); err != nil {
		t.Fatalf("count observations: %v", err)
	}
	return n
}

// TestCheckpoint_TruncateShrinksWAL verifies Checkpoint() performs a TRUNCATE checkpoint
// that actually reclaims the -wal file. This is the load-bearing fix for issue #49: a
// PASSIVE checkpoint drains frames but never shrinks the file, so reverting Checkpoint to
// PASSIVE would leave the WAL grown and fail this test.
func TestCheckpoint_TruncateShrinksWAL(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gorm_checkpoint_truncate_*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := NewStore(Config{Path: dbPath, MaxConns: 2, LogLevel: logger.Silent})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	walBefore := growWAL(t, store, 1000)
	if walBefore < 64*1024 {
		t.Fatalf("expected WAL to grow above 64KiB, got %d bytes", walBefore)
	}

	if err := store.Checkpoint(context.Background()); err != nil {
		t.Fatalf("Checkpoint failed: %v", err)
	}

	walAfter := store.WALSize()
	if walAfter >= walBefore {
		t.Errorf("expected WAL to shrink after TRUNCATE checkpoint: before=%d after=%d", walBefore, walAfter)
	}
	if walAfter > 64*1024 {
		t.Errorf("expected WAL truncated to near-zero, got %d bytes", walAfter)
	}
}

// TestCheckpointIfLarge_GatesOnThreshold verifies the size-gated periodic checkpoint used
// by the worker's walCheckpointLoop: a no-op below the threshold, a truncating checkpoint
// at/above it.
func TestCheckpointIfLarge_GatesOnThreshold(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gorm_checkpoint_gated_*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := NewStore(Config{Path: dbPath, MaxConns: 2, LogLevel: logger.Silent})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	// Below an enormous threshold -> no checkpoint.
	done, err := store.CheckpointIfLarge(context.Background(), 1<<30) // 1 GiB
	if err != nil {
		t.Fatalf("CheckpointIfLarge (small) failed: %v", err)
	}
	if done {
		t.Errorf("expected no checkpoint below threshold, but one was performed")
	}

	// Grow the WAL, then a low threshold triggers a truncating checkpoint.
	walBefore := growWAL(t, store, 1000)
	if walBefore < 64*1024 {
		t.Fatalf("expected WAL to grow above 64KiB, got %d bytes", walBefore)
	}

	done, err = store.CheckpointIfLarge(context.Background(), 64*1024)
	if err != nil {
		t.Fatalf("CheckpointIfLarge (large) failed: %v", err)
	}
	if !done {
		t.Errorf("expected checkpoint above threshold, but none was performed")
	}
	if walAfter := store.WALSize(); walAfter >= walBefore {
		t.Errorf("expected WAL to shrink after gated checkpoint: before=%d after=%d", walBefore, walAfter)
	}
}

// TestClose_CheckpointsWAL verifies Close() reclaims the WAL and leaves the data intact on
// the next open (issue #49: shutdown must not leave a large dirty WAL on disk).
func TestClose_CheckpointsWAL(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gorm_close_checkpoint_*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := NewStore(Config{Path: dbPath, MaxConns: 2, LogLevel: logger.Silent})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	if walBefore := growWAL(t, store, 800); walBefore == 0 {
		t.Fatalf("expected WAL to grow before close, got 0")
	}
	count := countObservations(t, store)
	if count < 800 {
		t.Fatalf("expected >=800 observations before close, got %d", count)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// The -wal file must not persist large on disk after a clean shutdown.
	if info, statErr := os.Stat(dbPath + "-wal"); statErr == nil && info.Size() > 64*1024 {
		t.Errorf("expected WAL reclaimed on close, -wal still %d bytes", info.Size())
	}

	// Reopen and verify data survived the checkpoint.
	store2, err := NewStore(Config{Path: dbPath, MaxConns: 2, LogLevel: logger.Silent})
	if err != nil {
		t.Fatalf("reopen NewStore failed: %v", err)
	}
	defer store2.Close()

	if count2 := countObservations(t, store2); count2 != count {
		t.Errorf("expected %d observations after reopen, got %d", count, count2)
	}
}

// TestBusyTimeoutAppliedToAllConnections verifies the issue #49 DSN fix: busy_timeout is
// applied to EVERY pooled connection (not just one arbitrary connection as happened when
// it was set via a single post-open sqlDB.Exec).
func TestBusyTimeoutAppliedToAllConnections(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gorm_busy_timeout_*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	const maxConns = 4
	store, err := NewStore(Config{Path: dbPath, MaxConns: maxConns, LogLevel: logger.Silent})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	// Pin all connections concurrently so each distinct connection is inspected, then
	// assert every one reports busy_timeout=5000.
	raw := store.GetRawDB()
	conns := make([]*sql.Conn, 0, maxConns)
	defer func() {
		for _, c := range conns {
			_ = c.Close()
		}
	}()
	for i := 0; i < maxConns; i++ {
		c, err := raw.Conn(context.Background())
		if err != nil {
			t.Fatalf("acquire conn %d: %v", i, err)
		}
		conns = append(conns, c)
	}
	for i, c := range conns {
		var timeout int
		if err := c.QueryRowContext(context.Background(), "PRAGMA busy_timeout").Scan(&timeout); err != nil {
			t.Fatalf("query busy_timeout on conn %d: %v", i, err)
		}
		if timeout != 5000 {
			t.Errorf("conn %d: expected busy_timeout=5000, got %d", i, timeout)
		}
	}
}

// TestAllPragmasAppliedToAllConnections verifies the issue #49 (F6) ConnectHook fix: not
// just busy_timeout but the full pragma set — including the best-effort pragmas that used
// to be set via a single post-open sqlDB.Exec (journal_size_limit, temp_store,
// wal_autocheckpoint) — is applied to EVERY pooled connection. It pins all connections so
// each distinct connection is inspected, then asserts each reports the expected value.
func TestAllPragmasAppliedToAllConnections(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gorm_all_pragmas_*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	const maxConns = 4
	store, err := NewStore(Config{Path: dbPath, MaxConns: maxConns, LogLevel: logger.Silent})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	// Expected per-connection pragma values. temp_store=MEMORY reports as 2; the others are
	// numeric. foreign_keys/journal_mode/synchronous are covered by TestNewStore and the
	// busy_timeout test; here we focus on the previously single-connection pragmas.
	checks := []struct {
		name string
		want int64
	}{
		{"busy_timeout", 5000},
		{"journal_size_limit", 8388608},
		{"temp_store", 2}, // 2 == MEMORY
		{"wal_autocheckpoint", 1000},
		{"foreign_keys", 1},
	}

	raw := store.GetRawDB()
	conns := make([]*sql.Conn, 0, maxConns)
	defer func() {
		for _, c := range conns {
			_ = c.Close()
		}
	}()
	for i := 0; i < maxConns; i++ {
		c, err := raw.Conn(context.Background())
		if err != nil {
			t.Fatalf("acquire conn %d: %v", i, err)
		}
		conns = append(conns, c)
	}

	for i, c := range conns {
		for _, chk := range checks {
			var got int64
			query := "PRAGMA " + chk.name
			if err := c.QueryRowContext(context.Background(), query).Scan(&got); err != nil {
				t.Fatalf("conn %d: query %q: %v", i, chk.name, err)
			}
			if got != chk.want {
				t.Errorf("conn %d: %s = %d, want %d", i, chk.name, got, chk.want)
			}
		}
	}
}

// TestRecycledConnectionRetainsPragmas verifies that recycling a connection (which now
// happens because SetConnMaxLifetime is finite, not 0) does NOT drop the correctness
// pragmas: the ConnectHook reapplies them on every new connection. We force recycling by
// setting a near-zero max lifetime so the next acquisition opens a fresh connection, then
// assert the new connection still reports the safe values rather than SQLite defaults
// (busy_timeout would default to 0 and journal_mode to "delete" without the hook).
func TestRecycledConnectionRetainsPragmas(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gorm_recycle_pragmas_*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := NewStore(Config{Path: dbPath, MaxConns: 2, LogLevel: logger.Silent})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	raw := store.GetRawDB()

	// Force aggressive recycling: any connection older than 1ns is expired on next use, so
	// database/sql opens a brand-new connection (running the ConnectHook again).
	raw.SetConnMaxLifetime(time.Nanosecond)
	time.Sleep(5 * time.Millisecond)

	// Acquire a connection that is necessarily freshly opened (all prior ones are expired),
	// and verify the hook reapplied the correctness pragmas.
	conn, err := raw.Conn(context.Background())
	if err != nil {
		t.Fatalf("acquire recycled conn: %v", err)
	}
	defer conn.Close()

	var busyTimeout int
	if err := conn.QueryRowContext(context.Background(), "PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		t.Fatalf("query busy_timeout: %v", err)
	}
	if busyTimeout != 5000 {
		t.Errorf("recycled conn: busy_timeout = %d, want 5000 (hook did not reapply)", busyTimeout)
	}

	var journalMode string
	if err := conn.QueryRowContext(context.Background(), "PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("query journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Errorf("recycled conn: journal_mode = %q, want \"wal\" (hook did not reapply)", journalMode)
	}

	var foreignKeys int
	if err := conn.QueryRowContext(context.Background(), "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatalf("query foreign_keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Errorf("recycled conn: foreign_keys = %d, want 1 (hook did not reapply)", foreignKeys)
	}
}
