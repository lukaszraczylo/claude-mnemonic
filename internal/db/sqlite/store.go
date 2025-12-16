// Package sqlite provides SQLite database operations for claude-mnemonic.
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"
)

// Store provides database operations with connection pooling and prepared statements.
type Store struct {
	db        *sql.DB
	stmtCache map[string]*sql.Stmt
	stmtMu    sync.RWMutex
}

// StoreConfig holds configuration for the database store.
type StoreConfig struct {
	Path     string
	MaxConns int
	WALMode  bool
}

// NewStore creates a new database store with the given configuration.
func NewStore(cfg StoreConfig) (*Store, error) {
	// Register sqlite-vec extension for vector operations
	sqlite_vec.Auto()

	// Build connection string with pragmas
	connStr := cfg.Path + "?_journal_mode=WAL&_synchronous=NORMAL&_foreign_keys=ON"

	db, err := sql.Open("sqlite3", connStr)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Configure connection pool
	maxConns := cfg.MaxConns
	if maxConns <= 0 {
		maxConns = 4
	}
	db.SetMaxOpenConns(maxConns)
	db.SetMaxIdleConns(maxConns)
	db.SetConnMaxLifetime(0) // Never expire - SQLite connections are cheap

	// Verify connection
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	store := &Store{
		db:        db,
		stmtCache: make(map[string]*sql.Stmt),
	}

	// Run migrations
	mgr := NewMigrationManager(db)
	if err := mgr.RunMigrations(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	return store, nil
}

// Close closes the database connection and all cached statements.
func (s *Store) Close() error {
	s.stmtMu.Lock()
	defer s.stmtMu.Unlock()

	for _, stmt := range s.stmtCache {
		_ = stmt.Close()
	}
	s.stmtCache = nil

	return s.db.Close()
}

// GetStmt returns a cached prepared statement, creating it if necessary.
func (s *Store) GetStmt(query string) (*sql.Stmt, error) {
	s.stmtMu.RLock()
	stmt, ok := s.stmtCache[query]
	s.stmtMu.RUnlock()
	if ok {
		return stmt, nil
	}

	s.stmtMu.Lock()
	defer s.stmtMu.Unlock()

	// Double-check after acquiring write lock
	if stmt, ok := s.stmtCache[query]; ok {
		return stmt, nil
	}

	stmt, err := s.db.Prepare(query)
	if err != nil {
		return nil, err
	}

	s.stmtCache[query] = stmt
	return stmt, nil
}

// ExecContext executes a query that doesn't return rows.
func (s *Store) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	stmt, err := s.GetStmt(query)
	if err != nil {
		// Fall back to direct execution
		return s.db.ExecContext(ctx, query, args...)
	}
	return stmt.ExecContext(ctx, args...)
}

// QueryContext executes a query that returns rows.
func (s *Store) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	stmt, err := s.GetStmt(query)
	if err != nil {
		// Fall back to direct execution
		return s.db.QueryContext(ctx, query, args...)
	}
	return stmt.QueryContext(ctx, args...)
}

// QueryRowContext executes a query that returns a single row.
func (s *Store) QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row {
	stmt, err := s.GetStmt(query)
	if err != nil {
		// Fall back to direct execution
		return s.db.QueryRowContext(ctx, query, args...)
	}
	return stmt.QueryRowContext(ctx, args...)
}

// Ping checks if the database connection is alive.
func (s *Store) Ping() error {
	return s.db.Ping()
}

// DB returns the underlying database connection for direct access.
// Use this sparingly - prefer the store methods for most operations.
func (s *Store) DB() *sql.DB {
	return s.db
}
