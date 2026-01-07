//go:build fts5

// Package gorm provides GORM-based database operations for claude-mnemonic.
package gorm

import (
	"os"
	"path/filepath"
	"testing"

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
