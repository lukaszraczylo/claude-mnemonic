// Package sqlite provides SQLite database operations for claude-mnemonic.
package sqlite

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// StoreSuite is a test suite for Store operations.
type StoreSuite struct {
	suite.Suite
	db      *sql.DB
	store   *Store
	cleanup func()
}

// SetupTest creates a fresh database before each test.
func (s *StoreSuite) SetupTest() {
	s.db, _, s.cleanup = testDB(s.T())
	createBaseTables(s.T(), s.db)
	s.store = newStoreFromDB(s.db)
}

// TearDownTest cleans up after each test.
func (s *StoreSuite) TearDownTest() {
	if s.cleanup != nil {
		s.cleanup()
	}
}

func TestStoreSuite(t *testing.T) {
	suite.Run(t, new(StoreSuite))
}

// TestGetStmt tests prepared statement caching.
func (s *StoreSuite) TestGetStmt() {
	tests := []struct {
		name    string
		query   string
		wantErr bool
	}{
		{
			name:    "valid simple query",
			query:   "SELECT 1",
			wantErr: false,
		},
		{
			name:    "valid query with parameter",
			query:   "SELECT * FROM sdk_sessions WHERE id = ?",
			wantErr: false,
		},
		{
			name:    "invalid query syntax",
			query:   "SELECT * FROM nonexistent_table WHERE",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			stmt, err := s.store.GetStmt(tt.query)
			if tt.wantErr {
				s.Error(err)
				s.Nil(stmt)
			} else {
				s.NoError(err)
				s.NotNil(stmt)

				// Second call should return cached statement
				stmt2, err := s.store.GetStmt(tt.query)
				s.NoError(err)
				s.Same(stmt, stmt2)
			}
		})
	}
}

// TestExecContext tests query execution.
func (s *StoreSuite) TestExecContext() {
	ctx := context.Background()

	tests := []struct {
		name         string
		query        string
		args         []interface{}
		wantErr      bool
		wantAffected int64
	}{
		{
			name: "insert session",
			query: `INSERT INTO sdk_sessions (claude_session_id, sdk_session_id, project, started_at, started_at_epoch, status)
				VALUES (?, ?, ?, datetime('now'), strftime('%s', 'now') * 1000, 'active')`,
			args:         []interface{}{"claude-1", "sdk-1", "test-project"},
			wantErr:      false,
			wantAffected: 1,
		},
		{
			name:         "invalid query",
			query:        "INSERT INTO nonexistent_table VALUES (?)",
			args:         []interface{}{"test"},
			wantErr:      true,
			wantAffected: 0,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			result, err := s.store.ExecContext(ctx, tt.query, tt.args...)
			if tt.wantErr {
				s.Error(err)
			} else {
				s.NoError(err)
				affected, _ := result.RowsAffected()
				s.Equal(tt.wantAffected, affected)
			}
		})
	}
}

// TestQueryContext tests query execution that returns rows.
func (s *StoreSuite) TestQueryContext() {
	ctx := context.Background()

	// Seed data
	seedSession(s.T(), s.db, "claude-1", "sdk-1", "project-a")

	tests := []struct {
		name       string
		query      string
		args       []interface{}
		wantErr    bool
		wantRows   int
		setupFunc  func()
		assertFunc func(rows *sql.Rows)
	}{
		{
			name:     "query existing session",
			query:    "SELECT id, project FROM sdk_sessions WHERE claude_session_id = ?",
			args:     []interface{}{"claude-1"},
			wantErr:  false,
			wantRows: 1,
		},
		{
			name:     "query non-existent session",
			query:    "SELECT id, project FROM sdk_sessions WHERE claude_session_id = ?",
			args:     []interface{}{"nonexistent"},
			wantErr:  false,
			wantRows: 0,
		},
		{
			name:     "query all sessions",
			query:    "SELECT id, project FROM sdk_sessions",
			args:     nil,
			wantErr:  false,
			wantRows: 1,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			rows, err := s.store.QueryContext(ctx, tt.query, tt.args...)
			if tt.wantErr {
				s.Error(err)
				return
			}

			s.NoError(err)
			defer rows.Close()

			count := 0
			for rows.Next() {
				count++
			}
			s.Equal(tt.wantRows, count)
		})
	}
}

// TestQueryRowContext tests single row query execution.
func (s *StoreSuite) TestQueryRowContext() {
	ctx := context.Background()

	// Seed data
	seedSession(s.T(), s.db, "claude-1", "sdk-1", "project-a")

	tests := []struct {
		name    string
		query   string
		args    []interface{}
		wantErr bool
	}{
		{
			name:    "query existing session",
			query:   "SELECT id FROM sdk_sessions WHERE claude_session_id = ?",
			args:    []interface{}{"claude-1"},
			wantErr: false,
		},
		{
			name:    "query non-existent session",
			query:   "SELECT id FROM sdk_sessions WHERE claude_session_id = ?",
			args:    []interface{}{"nonexistent"},
			wantErr: true, // sql.ErrNoRows
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			row := s.store.QueryRowContext(ctx, tt.query, tt.args...)
			var id int64
			err := row.Scan(&id)
			if tt.wantErr {
				s.Error(err)
			} else {
				s.NoError(err)
				s.Greater(id, int64(0))
			}
		})
	}
}

// TestPing tests database connection health check.
func (s *StoreSuite) TestPing() {
	err := s.store.Ping()
	s.NoError(err)
}

// TestDB tests getting the underlying database connection.
func (s *StoreSuite) TestDB() {
	db := s.store.DB()
	s.NotNil(db)
	s.Same(s.db, db)
}

// TestClose tests closing the store.
func (s *StoreSuite) TestClose() {
	// Create a separate store for close test
	db, _, cleanup := testDB(s.T())
	defer cleanup()

	store := newStoreFromDB(db)

	// Cache a statement first
	_, err := store.GetStmt("SELECT 1")
	s.NoError(err)

	// Close should not error
	err = store.Close()
	s.NoError(err)

	// Operations after close should fail
	err = store.Ping()
	s.Error(err)
}

// TestConcurrentStmtCache tests concurrent access to statement cache.
func (s *StoreSuite) TestConcurrentStmtCache() {
	ctx := context.Background()
	queries := []string{
		"SELECT 1",
		"SELECT 2",
		"SELECT id FROM sdk_sessions",
		"SELECT project FROM sdk_sessions",
	}

	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func(i int) {
			query := queries[i%len(queries)]
			_, _ = s.store.GetStmt(query)
			_, _ = s.store.ExecContext(ctx, "SELECT 1")
			done <- struct{}{}
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

// HelpersSuite tests helper functions.
type HelpersSuite struct {
	suite.Suite
}

func TestHelpersSuite(t *testing.T) {
	suite.Run(t, new(HelpersSuite))
}

func (s *HelpersSuite) TestNullString() {
	tests := []struct {
		name     string
		input    string
		wantStr  string
		wantBool bool
	}{
		{
			name:     "empty string",
			input:    "",
			wantStr:  "",
			wantBool: false,
		},
		{
			name:     "non-empty string",
			input:    "test",
			wantStr:  "test",
			wantBool: true,
		},
		{
			name:     "whitespace string",
			input:    "  ",
			wantStr:  "  ",
			wantBool: true,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			result := nullString(tt.input)
			s.Equal(tt.wantStr, result.String)
			s.Equal(tt.wantBool, result.Valid)
		})
	}
}

func (s *HelpersSuite) TestNullInt() {
	tests := []struct {
		name     string
		input    int
		wantInt  int64
		wantBool bool
	}{
		{
			name:     "zero",
			input:    0,
			wantInt:  0,
			wantBool: false,
		},
		{
			name:     "negative",
			input:    -1,
			wantInt:  -1,
			wantBool: false,
		},
		{
			name:     "positive",
			input:    42,
			wantInt:  42,
			wantBool: true,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			result := nullInt(tt.input)
			s.Equal(tt.wantInt, result.Int64)
			s.Equal(tt.wantBool, result.Valid)
		})
	}
}

func (s *HelpersSuite) TestRepeatPlaceholders() {
	tests := []struct {
		name     string
		input    int
		expected string
	}{
		{
			name:     "zero",
			input:    0,
			expected: "",
		},
		{
			name:     "negative",
			input:    -1,
			expected: "",
		},
		{
			name:     "one",
			input:    1,
			expected: ", ?",
		},
		{
			name:     "three",
			input:    3,
			expected: ", ?, ?, ?",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			result := repeatPlaceholders(tt.input)
			s.Equal(tt.expected, result)
		})
	}
}

func (s *HelpersSuite) TestInt64SliceToInterface() {
	tests := []struct {
		name     string
		input    []int64
		expected int
	}{
		{
			name:     "empty slice",
			input:    []int64{},
			expected: 0,
		},
		{
			name:     "single element",
			input:    []int64{42},
			expected: 1,
		},
		{
			name:     "multiple elements",
			input:    []int64{1, 2, 3, 4, 5},
			expected: 5,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			result := int64SliceToInterface(tt.input)
			s.Len(result, tt.expected)
			for i, v := range result {
				s.Equal(tt.input[i], v)
			}
		})
	}
}

// TestBuildGetByIDsQuery tests the shared query builder.
func TestBuildGetByIDsQuery(t *testing.T) {
	tests := []struct {
		name      string
		baseQuery string
		ids       []int64
		orderBy   string
		limit     int
		wantQuery string
		wantArgs  int
	}{
		{
			name:      "single id, no limit, desc order",
			baseQuery: "SELECT * FROM test",
			ids:       []int64{1},
			orderBy:   "date_desc",
			limit:     0,
			wantQuery: "SELECT * FROM test WHERE id IN (?)\n\t\tORDER BY created_at_epoch DESC",
			wantArgs:  1,
		},
		{
			name:      "multiple ids with limit and asc order",
			baseQuery: "SELECT * FROM test",
			ids:       []int64{1, 2, 3},
			orderBy:   "date_asc",
			limit:     10,
			wantQuery: "SELECT * FROM test WHERE id IN (?, ?, ?)\n\t\tORDER BY created_at_epoch ASC LIMIT ?",
			wantArgs:  4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query, args := BuildGetByIDsQuery(tt.baseQuery, tt.ids, tt.orderBy, tt.limit)
			assert.Contains(t, query, "WHERE id IN")
			assert.Len(t, args, tt.wantArgs)
		})
	}
}

// TestEnsureSessionExists tests session auto-creation.
func TestEnsureSessionExists(t *testing.T) {
	db, _, cleanup := testDB(t)
	defer cleanup()
	createBaseTables(t, db)

	store := newStoreFromDB(db)
	ctx := context.Background()

	tests := []struct {
		name         string
		sdkSessionID string
		project      string
		setup        func()
		wantErr      bool
	}{
		{
			name:         "create new session",
			sdkSessionID: "sdk-new",
			project:      "project-a",
			wantErr:      false,
		},
		{
			name:         "session already exists",
			sdkSessionID: "sdk-existing",
			project:      "project-b",
			setup: func() {
				seedSession(t, db, "sdk-existing", "sdk-existing", "project-b")
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup()
			}

			err := EnsureSessionExists(ctx, store, tt.sdkSessionID, tt.project)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)

				// Verify session exists
				var id int64
				err := db.QueryRow("SELECT id FROM sdk_sessions WHERE sdk_session_id = ?", tt.sdkSessionID).Scan(&id)
				require.NoError(t, err)
				assert.Greater(t, id, int64(0))
			}
		})
	}
}
