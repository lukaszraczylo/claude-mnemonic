//go:build fts5

package gorm

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// TestFTS5Available verifies FTS5 is available in mattn/go-sqlite3
func TestFTS5Available(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "fts5_test_*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")

	// Open with mattn/go-sqlite3 driver
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()

	// Try to create FTS5 virtual table
	_, err = db.Exec(`
		CREATE VIRTUAL TABLE test_fts USING fts5(
			content
		)
	`)
	if err != nil {
		t.Fatalf("create FTS5 table failed: %v", err)
	}

	t.Logf("✅ FTS5 is available in mattn/go-sqlite3")
}
