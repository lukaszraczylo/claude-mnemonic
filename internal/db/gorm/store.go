// Package gorm provides GORM-based database operations for claude-mnemonic.
package gorm

import (
	"database/sql"
	"fmt"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3" // Import SQLite driver with FTS5 support
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Store represents the GORM database connection with sqlite-vec support.
type Store struct {
	DB    *gorm.DB
	sqlDB *sql.DB // For FTS5 and sqlite-vec operations that require raw SQL
}

// Config holds database configuration.
type Config struct {
	Path     string          // Path to SQLite database file
	MaxConns int             // Maximum number of open connections (default: 4)
	LogLevel logger.LogLevel // GORM log level (logger.Silent for production)
}

// NewStore creates a new Store with WAL mode enabled and sqlite-vec registered.
// CRITICAL: WAL mode and foreign keys are enabled via pragmas for concurrent reads.
func NewStore(cfg Config) (*Store, error) {
	// 1. Register sqlite-vec extension (must be done before opening database)
	sqlite_vec.Auto()

	// 2. Build connection string (foreign keys enabled in DSN)
	// Use sqlite3 driver (mattn/go-sqlite3) which has FTS5 support
	dsn := cfg.Path + "?_foreign_keys=ON"

	// 3. Open raw database connection with mattn/go-sqlite3 (has FTS5 support)
	sqlDB, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// 4. Wrap with GORM using existing connection
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

	// 5. Configure connection pool (same settings as current implementation)
	maxConns := cfg.MaxConns
	if maxConns <= 0 {
		maxConns = 4
	}
	sqlDB.SetMaxOpenConns(maxConns)
	sqlDB.SetMaxIdleConns(maxConns)
	sqlDB.SetConnMaxLifetime(0) // Never expire (SQLite connections are cheap)

	// 6. Verify connection
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("ping database: %w", err)
	}

	store := &Store{
		DB:    db,
		sqlDB: sqlDB,
	}

	// 7. Run migrations FIRST (before PRAGMA commands)
	if err := runMigrations(db, sqlDB); err != nil {
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	// 8. CRITICAL: Set WAL mode and synchronous mode via raw SQL
	// Use raw sqlDB to avoid GORM transaction issues
	if _, err := sqlDB.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}
	if _, err := sqlDB.Exec("PRAGMA synchronous=NORMAL"); err != nil {
		return nil, fmt.Errorf("set synchronous mode: %w", err)
	}
	// Set busy timeout to 5 seconds to handle concurrent writes
	// This allows SQLite to retry when database is locked instead of failing immediately
	if _, err := sqlDB.Exec("PRAGMA busy_timeout=5000"); err != nil {
		return nil, fmt.Errorf("set busy timeout: %w", err)
	}

	return store, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.sqlDB.Close()
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
