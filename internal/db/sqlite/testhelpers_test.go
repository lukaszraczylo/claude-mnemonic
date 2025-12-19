package sqlite

import (
	"database/sql"
	"os"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// newStoreFromDB creates a Store from an existing database connection for testing.
func newStoreFromDB(db *sql.DB) *Store {
	return &Store{
		db:        db,
		stmtCache: make(map[string]*sql.Stmt),
	}
}

// storeDB returns the underlying database connection from a store for testing.
func storeDB(s *Store) *sql.DB {
	return s.db
}

// testDB creates a temporary SQLite database for testing.
// Returns the database, path, and a cleanup function.
func testDB(t *testing.T) (*sql.DB, string, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "claude-mnemonic-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}

	dbPath := tmpDir + "/test.db"
	connStr := dbPath + "?_journal_mode=WAL&_synchronous=NORMAL&_foreign_keys=ON"

	db, err := sql.Open("sqlite3", connStr)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		t.Fatalf("open database: %v", err)
	}

	cleanup := func() {
		_ = db.Close()
		_ = os.RemoveAll(tmpDir)
	}

	return db, dbPath, cleanup
}

// createBaseTables creates the base tables without FTS5 for unit testing.
func createBaseTables(t *testing.T, db *sql.DB) {
	t.Helper()

	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_versions (
			id INTEGER PRIMARY KEY,
			version INTEGER UNIQUE NOT NULL,
			applied_at TEXT NOT NULL
		)
	`)
	if err != nil {
		t.Fatalf("create schema_versions: %v", err)
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS sdk_sessions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			claude_session_id TEXT UNIQUE NOT NULL,
			sdk_session_id TEXT UNIQUE,
			project TEXT NOT NULL,
			user_prompt TEXT,
			started_at TEXT NOT NULL,
			started_at_epoch INTEGER NOT NULL,
			completed_at TEXT,
			completed_at_epoch INTEGER,
			status TEXT CHECK(status IN ('active', 'completed', 'failed')) NOT NULL DEFAULT 'active',
			worker_port INTEGER,
			prompt_counter INTEGER DEFAULT 0
		)
	`)
	if err != nil {
		t.Fatalf("create sdk_sessions: %v", err)
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS observations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			sdk_session_id TEXT NOT NULL,
			project TEXT NOT NULL,
			text TEXT,
			type TEXT NOT NULL CHECK(type IN ('decision', 'bugfix', 'feature', 'refactor', 'discovery', 'change')),
			title TEXT,
			subtitle TEXT,
			facts TEXT,
			narrative TEXT,
			concepts TEXT,
			files_read TEXT,
			files_modified TEXT,
			file_mtimes TEXT,
			scope TEXT DEFAULT 'project' CHECK(scope IN ('project', 'global')),
			prompt_number INTEGER,
			discovery_tokens INTEGER DEFAULT 0,
			created_at TEXT NOT NULL,
			created_at_epoch INTEGER NOT NULL,
			importance_score REAL DEFAULT 1.0,
			user_feedback INTEGER DEFAULT 0,
			retrieval_count INTEGER DEFAULT 0,
			last_retrieved_at_epoch INTEGER,
			score_updated_at_epoch INTEGER,
			is_superseded INTEGER DEFAULT 0,
			FOREIGN KEY(sdk_session_id) REFERENCES sdk_sessions(sdk_session_id) ON DELETE CASCADE
		)
	`)
	if err != nil {
		t.Fatalf("create observations: %v", err)
	}

	// Create observation_conflicts table for conflict detection
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS observation_conflicts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			newer_obs_id INTEGER NOT NULL,
			older_obs_id INTEGER NOT NULL,
			conflict_type TEXT NOT NULL CHECK(conflict_type IN ('superseded', 'contradicts', 'outdated_pattern')),
			resolution TEXT NOT NULL CHECK(resolution IN ('prefer_newer', 'prefer_older', 'manual')),
			reason TEXT,
			detected_at TEXT NOT NULL,
			detected_at_epoch INTEGER NOT NULL,
			resolved INTEGER DEFAULT 0,
			resolved_at TEXT,
			FOREIGN KEY(newer_obs_id) REFERENCES observations(id) ON DELETE CASCADE,
			FOREIGN KEY(older_obs_id) REFERENCES observations(id) ON DELETE CASCADE
		)
	`)
	if err != nil {
		t.Fatalf("create observation_conflicts: %v", err)
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS session_summaries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			sdk_session_id TEXT NOT NULL,
			project TEXT NOT NULL,
			request TEXT,
			investigated TEXT,
			learned TEXT,
			completed TEXT,
			next_steps TEXT,
			files_read TEXT,
			files_edited TEXT,
			notes TEXT,
			prompt_number INTEGER,
			discovery_tokens INTEGER DEFAULT 0,
			created_at TEXT NOT NULL,
			created_at_epoch INTEGER NOT NULL,
			FOREIGN KEY(sdk_session_id) REFERENCES sdk_sessions(sdk_session_id) ON DELETE CASCADE
		)
	`)
	if err != nil {
		t.Fatalf("create session_summaries: %v", err)
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS user_prompts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			claude_session_id TEXT NOT NULL,
			prompt_number INTEGER NOT NULL,
			prompt_text TEXT NOT NULL,
			matched_observations INTEGER DEFAULT 0,
			created_at TEXT NOT NULL,
			created_at_epoch INTEGER NOT NULL,
			FOREIGN KEY(claude_session_id) REFERENCES sdk_sessions(claude_session_id) ON DELETE CASCADE
		)
	`)
	if err != nil {
		t.Fatalf("create user_prompts: %v", err)
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS patterns (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			type TEXT NOT NULL CHECK(type IN ('bug', 'refactor', 'architecture', 'anti-pattern', 'best-practice')),
			description TEXT,
			signature TEXT,
			recommendation TEXT,
			frequency INTEGER DEFAULT 1,
			projects TEXT,
			observation_ids TEXT,
			status TEXT DEFAULT 'active' CHECK(status IN ('active', 'deprecated', 'merged')),
			merged_into_id INTEGER,
			confidence REAL DEFAULT 0.5,
			last_seen_at TEXT NOT NULL,
			last_seen_at_epoch INTEGER NOT NULL,
			created_at TEXT NOT NULL,
			created_at_epoch INTEGER NOT NULL,
			FOREIGN KEY(merged_into_id) REFERENCES patterns(id) ON DELETE SET NULL
		)
	`)
	if err != nil {
		t.Fatalf("create patterns: %v", err)
	}

	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_sdk_sessions_claude_id ON sdk_sessions(claude_session_id)`,
		`CREATE INDEX IF NOT EXISTS idx_sdk_sessions_sdk_id ON sdk_sessions(sdk_session_id)`,
		`CREATE INDEX IF NOT EXISTS idx_sdk_sessions_project ON sdk_sessions(project)`,
		`CREATE INDEX IF NOT EXISTS idx_observations_sdk_session ON observations(sdk_session_id)`,
		`CREATE INDEX IF NOT EXISTS idx_observations_project ON observations(project)`,
		`CREATE INDEX IF NOT EXISTS idx_observations_scope ON observations(scope)`,
		`CREATE INDEX IF NOT EXISTS idx_observations_created ON observations(created_at_epoch DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_session_summaries_sdk_session ON session_summaries(sdk_session_id)`,
		`CREATE INDEX IF NOT EXISTS idx_session_summaries_project ON session_summaries(project)`,
		`CREATE INDEX IF NOT EXISTS idx_user_prompts_claude_session ON user_prompts(claude_session_id)`,
		`CREATE INDEX IF NOT EXISTS idx_user_prompts_created ON user_prompts(created_at_epoch DESC)`,
	}
	for _, idx := range indexes {
		if _, err := db.Exec(idx); err != nil {
			t.Fatalf("create index: %v", err)
		}
	}
}

// seedSession creates a test session in the database.
func seedSession(t *testing.T, db *sql.DB, claudeSessionID, sdkSessionID, project string) {
	t.Helper()

	_, err := db.Exec(`
		INSERT INTO sdk_sessions (claude_session_id, sdk_session_id, project, started_at, started_at_epoch, status)
		VALUES (?, ?, ?, datetime('now'), strftime('%s', 'now') * 1000, 'active')
	`, claudeSessionID, sdkSessionID, project)
	if err != nil {
		t.Fatalf("seed session: %v", err)
	}
}

// hasFTS5 checks if FTS5 is available in the SQLite build.
func hasFTS5(db *sql.DB) bool {
	_, err := db.Exec("CREATE VIRTUAL TABLE IF NOT EXISTS fts5_test USING fts5(content)")
	if err != nil {
		return false
	}
	_, _ = db.Exec("DROP TABLE IF EXISTS fts5_test")
	return true
}

// createFTSTables creates FTS5 virtual tables and triggers for full-text search.
func createFTSTables(t *testing.T, db *sql.DB) {
	t.Helper()

	if !hasFTS5(db) {
		t.Skip("FTS5 not available in this SQLite build")
	}

	_, err := db.Exec(`
		CREATE VIRTUAL TABLE IF NOT EXISTS observations_fts USING fts5(
			title, subtitle, narrative,
			content='observations',
			content_rowid='id'
		)
	`)
	if err != nil {
		t.Fatalf("create observations_fts: %v", err)
	}

	_, err = db.Exec(`
		CREATE TRIGGER IF NOT EXISTS observations_ai AFTER INSERT ON observations BEGIN
			INSERT INTO observations_fts(rowid, title, subtitle, narrative)
			VALUES (new.id, new.title, new.subtitle, new.narrative);
		END
	`)
	if err != nil {
		t.Fatalf("create observations_ai trigger: %v", err)
	}

	_, err = db.Exec(`
		CREATE TRIGGER IF NOT EXISTS observations_ad AFTER DELETE ON observations BEGIN
			INSERT INTO observations_fts(observations_fts, rowid, title, subtitle, narrative)
			VALUES ('delete', old.id, old.title, old.subtitle, old.narrative);
		END
	`)
	if err != nil {
		t.Fatalf("create observations_ad trigger: %v", err)
	}

	_, err = db.Exec(`
		CREATE TRIGGER IF NOT EXISTS observations_au AFTER UPDATE ON observations BEGIN
			INSERT INTO observations_fts(observations_fts, rowid, title, subtitle, narrative)
			VALUES ('delete', old.id, old.title, old.subtitle, old.narrative);
			INSERT INTO observations_fts(rowid, title, subtitle, narrative)
			VALUES (new.id, new.title, new.subtitle, new.narrative);
		END
	`)
	if err != nil {
		t.Fatalf("create observations_au trigger: %v", err)
	}

	_, err = db.Exec(`
		CREATE VIRTUAL TABLE IF NOT EXISTS session_summaries_fts USING fts5(
			request, investigated, learned, completed, next_steps, notes,
			content='session_summaries',
			content_rowid='id'
		)
	`)
	if err != nil {
		t.Fatalf("create session_summaries_fts: %v", err)
	}

	_, err = db.Exec(`
		CREATE TRIGGER IF NOT EXISTS summaries_ai AFTER INSERT ON session_summaries BEGIN
			INSERT INTO session_summaries_fts(rowid, request, investigated, learned, completed, next_steps, notes)
			VALUES (new.id, new.request, new.investigated, new.learned, new.completed, new.next_steps, new.notes);
		END
	`)
	if err != nil {
		t.Fatalf("create summaries_ai trigger: %v", err)
	}

	_, err = db.Exec(`
		CREATE TRIGGER IF NOT EXISTS summaries_ad AFTER DELETE ON session_summaries BEGIN
			INSERT INTO session_summaries_fts(session_summaries_fts, rowid, request, investigated, learned, completed, next_steps, notes)
			VALUES ('delete', old.id, old.request, old.investigated, old.learned, old.completed, old.next_steps, old.notes);
		END
	`)
	if err != nil {
		t.Fatalf("create summaries_ad trigger: %v", err)
	}

	_, err = db.Exec(`
		CREATE VIRTUAL TABLE IF NOT EXISTS user_prompts_fts USING fts5(
			prompt_text,
			content='user_prompts',
			content_rowid='id'
		)
	`)
	if err != nil {
		t.Fatalf("create user_prompts_fts: %v", err)
	}

	_, err = db.Exec(`
		CREATE TRIGGER IF NOT EXISTS prompts_ai AFTER INSERT ON user_prompts BEGIN
			INSERT INTO user_prompts_fts(rowid, prompt_text)
			VALUES (new.id, new.prompt_text);
		END
	`)
	if err != nil {
		t.Fatalf("create prompts_ai trigger: %v", err)
	}

	_, err = db.Exec(`
		CREATE TRIGGER IF NOT EXISTS prompts_ad AFTER DELETE ON user_prompts BEGIN
			INSERT INTO user_prompts_fts(user_prompts_fts, rowid, prompt_text)
			VALUES ('delete', old.id, old.prompt_text);
		END
	`)
	if err != nil {
		t.Fatalf("create prompts_ad trigger: %v", err)
	}
}

// createAllTables creates all tables including FTS5 for comprehensive testing.
func createAllTables(t *testing.T, db *sql.DB) {
	t.Helper()
	createBaseTables(t, db)
	createFTSTables(t, db)
}
