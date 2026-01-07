package worker

import (
	"database/sql"
	"os"
	"testing"

	"github.com/lukaszraczylo/claude-mnemonic/internal/db/gorm"
	_ "github.com/mattn/go-sqlite3"
)

// testStore creates a gorm.Store with a temporary database for testing.
// Uses gorm.NewStore which runs migrations (requires FTS5).
// Skips the test if FTS5 is not available.
func testStore(t *testing.T) (*gorm.Store, func()) {
	t.Helper()

	// First check if FTS5 is available
	if !hasFTS5ForTest(t) {
		t.Skip("FTS5 not available in this SQLite build")
	}

	tmpDir, err := os.MkdirTemp("", "claude-mnemonic-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}

	dbPath := tmpDir + "/test.db"

	store, err := gorm.NewStore(gorm.Config{
		Path:     dbPath,
		MaxConns: 1,
	})
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		t.Fatalf("create store: %v", err)
	}

	cleanup := func() {
		_ = store.Close()
		_ = os.RemoveAll(tmpDir)
	}

	return store, cleanup
}

// hasFTS5ForTest checks if FTS5 is available in the SQLite build.
func hasFTS5ForTest(t *testing.T) bool {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "fts5-check-*")
	if err != nil {
		return false
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	dbPath := tmpDir + "/check.db"
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return false
	}
	defer func() { _ = db.Close() }()

	_, err = db.Exec("CREATE VIRTUAL TABLE IF NOT EXISTS fts5_test USING fts5(content)")
	if err != nil {
		return false
	}
	_, _ = db.Exec("DROP TABLE IF EXISTS fts5_test")
	return true
}
