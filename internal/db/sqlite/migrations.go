// Package sqlite provides SQLite database operations for claude-mnemonic.
package sqlite

import (
	"database/sql"
	"fmt"
	"time"
)

// Migration represents a database schema migration.
type Migration struct {
	Version int
	Name    string
	SQL     string
}

// Migrations is the list of all database migrations in order.
var Migrations = []Migration{
	{
		Version: 4,
		Name:    "sdk_agent_architecture",
		SQL: `
			-- SDK Sessions (main session tracking)
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
				status TEXT CHECK(status IN ('active', 'completed', 'failed')) NOT NULL DEFAULT 'active'
			);

			CREATE INDEX IF NOT EXISTS idx_sdk_sessions_claude_id ON sdk_sessions(claude_session_id);
			CREATE INDEX IF NOT EXISTS idx_sdk_sessions_sdk_id ON sdk_sessions(sdk_session_id);
			CREATE INDEX IF NOT EXISTS idx_sdk_sessions_project ON sdk_sessions(project);
			CREATE INDEX IF NOT EXISTS idx_sdk_sessions_status ON sdk_sessions(status);
			CREATE INDEX IF NOT EXISTS idx_sdk_sessions_started ON sdk_sessions(started_at_epoch DESC);

			-- Observations (extracted learnings)
			CREATE TABLE IF NOT EXISTS observations (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				sdk_session_id TEXT NOT NULL,
				project TEXT NOT NULL,
				text TEXT,
				type TEXT NOT NULL CHECK(type IN ('decision', 'bugfix', 'feature', 'refactor', 'discovery', 'change')),
				created_at TEXT NOT NULL,
				created_at_epoch INTEGER NOT NULL,
				FOREIGN KEY(sdk_session_id) REFERENCES sdk_sessions(sdk_session_id) ON DELETE CASCADE
			);

			CREATE INDEX IF NOT EXISTS idx_observations_sdk_session ON observations(sdk_session_id);
			CREATE INDEX IF NOT EXISTS idx_observations_project ON observations(project);
			CREATE INDEX IF NOT EXISTS idx_observations_type ON observations(type);
			CREATE INDEX IF NOT EXISTS idx_observations_created ON observations(created_at_epoch DESC);

			-- Session Summaries
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
				created_at TEXT NOT NULL,
				created_at_epoch INTEGER NOT NULL,
				FOREIGN KEY(sdk_session_id) REFERENCES sdk_sessions(sdk_session_id) ON DELETE CASCADE
			);

			CREATE INDEX IF NOT EXISTS idx_session_summaries_sdk_session ON session_summaries(sdk_session_id);
			CREATE INDEX IF NOT EXISTS idx_session_summaries_project ON session_summaries(project);
			CREATE INDEX IF NOT EXISTS idx_session_summaries_created ON session_summaries(created_at_epoch DESC);
		`,
	},
	{
		Version: 5,
		Name:    "worker_port_column",
		SQL:     `ALTER TABLE sdk_sessions ADD COLUMN worker_port INTEGER;`,
	},
	{
		Version: 6,
		Name:    "prompt_tracking_columns",
		SQL: `
			ALTER TABLE sdk_sessions ADD COLUMN prompt_counter INTEGER DEFAULT 0;
			ALTER TABLE observations ADD COLUMN prompt_number INTEGER;
			ALTER TABLE session_summaries ADD COLUMN prompt_number INTEGER;
		`,
	},
	{
		Version: 8,
		Name:    "observation_hierarchical_fields",
		SQL: `
			ALTER TABLE observations ADD COLUMN title TEXT;
			ALTER TABLE observations ADD COLUMN subtitle TEXT;
			ALTER TABLE observations ADD COLUMN facts TEXT;
			ALTER TABLE observations ADD COLUMN narrative TEXT;
			ALTER TABLE observations ADD COLUMN concepts TEXT;
			ALTER TABLE observations ADD COLUMN files_read TEXT;
			ALTER TABLE observations ADD COLUMN files_modified TEXT;
		`,
	},
	{
		Version: 10,
		Name:    "user_prompts_table",
		SQL: `
			-- User prompts table
			CREATE TABLE IF NOT EXISTS user_prompts (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				claude_session_id TEXT NOT NULL,
				prompt_number INTEGER NOT NULL,
				prompt_text TEXT NOT NULL,
				created_at TEXT NOT NULL,
				created_at_epoch INTEGER NOT NULL,
				FOREIGN KEY(claude_session_id) REFERENCES sdk_sessions(claude_session_id) ON DELETE CASCADE
			);

			CREATE INDEX IF NOT EXISTS idx_user_prompts_claude_session ON user_prompts(claude_session_id);
			CREATE INDEX IF NOT EXISTS idx_user_prompts_created ON user_prompts(created_at_epoch DESC);
			CREATE INDEX IF NOT EXISTS idx_user_prompts_prompt_number ON user_prompts(prompt_number);
			CREATE INDEX IF NOT EXISTS idx_user_prompts_lookup ON user_prompts(claude_session_id, prompt_number);

			-- FTS5 virtual table for user prompts
			CREATE VIRTUAL TABLE IF NOT EXISTS user_prompts_fts USING fts5(
				prompt_text,
				content='user_prompts',
				content_rowid='id'
			);

			-- Triggers for FTS5 sync
			CREATE TRIGGER IF NOT EXISTS user_prompts_ai AFTER INSERT ON user_prompts BEGIN
				INSERT INTO user_prompts_fts(rowid, prompt_text)
				VALUES (new.id, new.prompt_text);
			END;

			CREATE TRIGGER IF NOT EXISTS user_prompts_ad AFTER DELETE ON user_prompts BEGIN
				INSERT INTO user_prompts_fts(user_prompts_fts, rowid, prompt_text)
				VALUES('delete', old.id, old.prompt_text);
			END;

			CREATE TRIGGER IF NOT EXISTS user_prompts_au AFTER UPDATE ON user_prompts BEGIN
				INSERT INTO user_prompts_fts(user_prompts_fts, rowid, prompt_text)
				VALUES('delete', old.id, old.prompt_text);
				INSERT INTO user_prompts_fts(rowid, prompt_text)
				VALUES (new.id, new.prompt_text);
			END;
		`,
	},
	{
		Version: 11,
		Name:    "discovery_tokens_column",
		SQL: `
			ALTER TABLE observations ADD COLUMN discovery_tokens INTEGER DEFAULT 0;
			ALTER TABLE session_summaries ADD COLUMN discovery_tokens INTEGER DEFAULT 0;
		`,
	},
	{
		Version: 12,
		Name:    "observations_fts",
		SQL: `
			-- FTS5 virtual table for observations
			CREATE VIRTUAL TABLE IF NOT EXISTS observations_fts USING fts5(
				title, subtitle, narrative,
				content='observations',
				content_rowid='id'
			);

			-- Triggers for FTS5 sync
			CREATE TRIGGER IF NOT EXISTS observations_ai AFTER INSERT ON observations BEGIN
				INSERT INTO observations_fts(rowid, title, subtitle, narrative)
				VALUES (new.id, new.title, new.subtitle, new.narrative);
			END;

			CREATE TRIGGER IF NOT EXISTS observations_ad AFTER DELETE ON observations BEGIN
				INSERT INTO observations_fts(observations_fts, rowid, title, subtitle, narrative)
				VALUES('delete', old.id, old.title, old.subtitle, old.narrative);
			END;

			CREATE TRIGGER IF NOT EXISTS observations_au AFTER UPDATE ON observations BEGIN
				INSERT INTO observations_fts(observations_fts, rowid, title, subtitle, narrative)
				VALUES('delete', old.id, old.title, old.subtitle, old.narrative);
				INSERT INTO observations_fts(rowid, title, subtitle, narrative)
				VALUES (new.id, new.title, new.subtitle, new.narrative);
			END;
		`,
	},
	{
		Version: 13,
		Name:    "session_summaries_fts",
		SQL: `
			-- FTS5 virtual table for session summaries
			CREATE VIRTUAL TABLE IF NOT EXISTS session_summaries_fts USING fts5(
				request, investigated, learned, completed, next_steps, notes,
				content='session_summaries',
				content_rowid='id'
			);

			-- Triggers for FTS5 sync
			CREATE TRIGGER IF NOT EXISTS session_summaries_ai AFTER INSERT ON session_summaries BEGIN
				INSERT INTO session_summaries_fts(rowid, request, investigated, learned, completed, next_steps, notes)
				VALUES (new.id, new.request, new.investigated, new.learned, new.completed, new.next_steps, new.notes);
			END;

			CREATE TRIGGER IF NOT EXISTS session_summaries_ad AFTER DELETE ON session_summaries BEGIN
				INSERT INTO session_summaries_fts(session_summaries_fts, rowid, request, investigated, learned, completed, next_steps, notes)
				VALUES('delete', old.id, old.request, old.investigated, old.learned, old.completed, old.next_steps, old.notes);
			END;

			CREATE TRIGGER IF NOT EXISTS session_summaries_au AFTER UPDATE ON session_summaries BEGIN
				INSERT INTO session_summaries_fts(session_summaries_fts, rowid, request, investigated, learned, completed, next_steps, notes)
				VALUES('delete', old.id, old.request, old.investigated, old.learned, old.completed, old.next_steps, old.notes);
				INSERT INTO session_summaries_fts(rowid, request, investigated, learned, completed, next_steps, notes)
				VALUES (new.id, new.request, new.investigated, new.learned, new.completed, new.next_steps, new.notes);
			END;
		`,
	},
	{
		Version: 14,
		Name:    "observation_scope_column",
		SQL: `
			-- Add scope column for project isolation
			-- 'project' = only visible within same project (default)
			-- 'global' = visible across all projects (best practices, patterns)
			ALTER TABLE observations ADD COLUMN scope TEXT DEFAULT 'project' CHECK(scope IN ('project', 'global'));

			-- Index for efficient scope-based queries
			CREATE INDEX IF NOT EXISTS idx_observations_scope ON observations(scope);
			CREATE INDEX IF NOT EXISTS idx_observations_project_scope ON observations(project, scope);
		`,
	},
	{
		Version: 15,
		Name:    "observation_file_mtimes",
		SQL: `
			-- Store file modification times at observation creation
			-- JSON object: {"path": mtime_epoch_ms, ...}
			-- Used to detect staleness when files change
			ALTER TABLE observations ADD COLUMN file_mtimes TEXT;
		`,
	},
	{
		Version: 16,
		Name:    "prompt_matched_observations",
		SQL: `
			-- Track how many observations were found relevant for each prompt
			-- Displayed in dashboard timeline
			ALTER TABLE user_prompts ADD COLUMN matched_observations INTEGER DEFAULT 0;
		`,
	},
	{
		Version: 17,
		Name:    "sqlite_vec_vectors",
		SQL: `
			-- Vector embeddings table using sqlite-vec
			-- Each document (narrative, fact, summary field, prompt) gets one vector
			-- Uses all-MiniLM-L6-v2 embeddings (384 dimensions)
			CREATE VIRTUAL TABLE IF NOT EXISTS vectors USING vec0(
				doc_id TEXT PRIMARY KEY,
				embedding float[384],
				sqlite_id INTEGER,
				doc_type TEXT,
				field_type TEXT,
				project TEXT,
				scope TEXT
			);
		`,
	},
	{
		Version: 18,
		Name:    "user_prompts_unique_constraint",
		SQL: `
			-- Add unique constraint to prevent duplicate prompts
			-- This fixes a bug where the user-prompt hook could fire multiple times
			-- creating duplicate prompt records with incrementing numbers
			CREATE UNIQUE INDEX IF NOT EXISTS idx_user_prompts_session_number_unique
			ON user_prompts(claude_session_id, prompt_number);
		`,
	},
	{
		Version: 19,
		Name:    "vectors_with_model_version",
		SQL: `
			-- Drop old vectors table (virtual tables cannot be altered)
			DROP TABLE IF EXISTS vectors;

			-- Recreate vectors table with model_version column
			-- Uses bge-small-en-v1.5 embeddings (384 dimensions)
			CREATE VIRTUAL TABLE IF NOT EXISTS vectors USING vec0(
				doc_id TEXT PRIMARY KEY,
				embedding float[384],
				sqlite_id INTEGER,
				doc_type TEXT,
				field_type TEXT,
				project TEXT,
				scope TEXT,
				model_version TEXT
			);
		`,
	},
	{
		Version: 20,
		Name:    "importance_scoring",
		SQL: `
			-- Importance scoring system for observations
			-- Implements multi-factor scoring: type weight, recency decay, user feedback, concept weights, retrieval boost

			-- Cached importance score (recalculated periodically)
			ALTER TABLE observations ADD COLUMN importance_score REAL DEFAULT 1.0;

			-- User feedback: -1 = thumbs down, 0 = neutral, 1 = thumbs up
			ALTER TABLE observations ADD COLUMN user_feedback INTEGER DEFAULT 0;

			-- Retrieval tracking: how many times this observation was returned in searches
			ALTER TABLE observations ADD COLUMN retrieval_count INTEGER DEFAULT 0;

			-- Last time this observation was retrieved (for analytics)
			ALTER TABLE observations ADD COLUMN last_retrieved_at_epoch INTEGER;

			-- Timestamp of last score recalculation
			ALTER TABLE observations ADD COLUMN score_updated_at_epoch INTEGER;

			-- Index for importance-based sorting (primary ordering strategy)
			CREATE INDEX IF NOT EXISTS idx_observations_importance
			ON observations(importance_score DESC, created_at_epoch DESC);

			-- Index for finding observations needing score recalculation
			CREATE INDEX IF NOT EXISTS idx_observations_score_updated
			ON observations(score_updated_at_epoch);

			-- Configurable concept weights table
			-- Allows runtime tuning of how much each concept contributes to importance
			CREATE TABLE IF NOT EXISTS concept_weights (
				concept TEXT PRIMARY KEY,
				weight REAL NOT NULL DEFAULT 0.1,
				updated_at TEXT NOT NULL
			);

			-- Seed default concept weights (security highest, tooling lowest)
			INSERT OR IGNORE INTO concept_weights (concept, weight, updated_at) VALUES
				('security', 0.30, datetime('now')),
				('gotcha', 0.25, datetime('now')),
				('best-practice', 0.20, datetime('now')),
				('anti-pattern', 0.20, datetime('now')),
				('architecture', 0.15, datetime('now')),
				('performance', 0.15, datetime('now')),
				('error-handling', 0.15, datetime('now')),
				('pattern', 0.10, datetime('now')),
				('testing', 0.10, datetime('now')),
				('debugging', 0.10, datetime('now')),
				('workflow', 0.05, datetime('now')),
				('tooling', 0.05, datetime('now'));
		`,
	},
	{
		Version: 21,
		Name:    "observation_conflicts",
		SQL: `
			-- Observation conflicts table for tracking contradictions and superseded observations
			-- Implements Issue #5: Contradiction & Obsolescence Detection
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
			);

			-- Index for looking up conflicts by observation ID
			CREATE INDEX IF NOT EXISTS idx_conflicts_newer ON observation_conflicts(newer_obs_id);
			CREATE INDEX IF NOT EXISTS idx_conflicts_older ON observation_conflicts(older_obs_id);
			CREATE INDEX IF NOT EXISTS idx_conflicts_unresolved ON observation_conflicts(resolved, detected_at_epoch DESC);

			-- Add is_superseded column to observations for quick filtering
			-- Set to 1 when this observation has been superseded by a newer one
			ALTER TABLE observations ADD COLUMN is_superseded INTEGER DEFAULT 0;

			-- Index for filtering out superseded observations in queries
			CREATE INDEX IF NOT EXISTS idx_observations_superseded ON observations(is_superseded, importance_score DESC);
		`,
	},
	{
		Version: 22,
		Name:    "patterns_table",
		SQL: `
			-- Pattern Recognition Engine (Issue #7)
			-- Tracks recurring patterns detected across observations
			-- Enables Claude to reference historical insights: "I've encountered this pattern 12 times."
			CREATE TABLE IF NOT EXISTS patterns (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				name TEXT NOT NULL,
				type TEXT NOT NULL CHECK(type IN ('bug', 'refactor', 'architecture', 'anti-pattern', 'best-practice')),
				description TEXT,
				signature TEXT,  -- JSON array of keywords/concepts for detection
				recommendation TEXT,  -- What works for this pattern
				frequency INTEGER DEFAULT 1,  -- How many times encountered
				projects TEXT,  -- JSON array of projects where seen
				observation_ids TEXT,  -- JSON array of source observation IDs
				status TEXT DEFAULT 'active' CHECK(status IN ('active', 'deprecated', 'merged')),
				merged_into_id INTEGER,  -- If status is 'merged', which pattern it merged into
				confidence REAL DEFAULT 0.5,  -- Detection confidence (0.0-1.0)
				last_seen_at TEXT NOT NULL,
				last_seen_at_epoch INTEGER NOT NULL,
				created_at TEXT NOT NULL,
				created_at_epoch INTEGER NOT NULL,
				FOREIGN KEY(merged_into_id) REFERENCES patterns(id) ON DELETE SET NULL
			);

			-- Indexes for efficient pattern queries
			CREATE INDEX IF NOT EXISTS idx_patterns_type ON patterns(type);
			CREATE INDEX IF NOT EXISTS idx_patterns_status ON patterns(status);
			CREATE INDEX IF NOT EXISTS idx_patterns_frequency ON patterns(frequency DESC);
			CREATE INDEX IF NOT EXISTS idx_patterns_confidence ON patterns(confidence DESC);
			CREATE INDEX IF NOT EXISTS idx_patterns_last_seen ON patterns(last_seen_at_epoch DESC);

			-- FTS5 virtual table for pattern search
			CREATE VIRTUAL TABLE IF NOT EXISTS patterns_fts USING fts5(
				name, description, recommendation,
				content='patterns',
				content_rowid='id'
			);

			-- Triggers for FTS5 sync
			CREATE TRIGGER IF NOT EXISTS patterns_ai AFTER INSERT ON patterns BEGIN
				INSERT INTO patterns_fts(rowid, name, description, recommendation)
				VALUES (new.id, new.name, new.description, new.recommendation);
			END;

			CREATE TRIGGER IF NOT EXISTS patterns_ad AFTER DELETE ON patterns BEGIN
				INSERT INTO patterns_fts(patterns_fts, rowid, name, description, recommendation)
				VALUES('delete', old.id, old.name, old.description, old.recommendation);
			END;

			CREATE TRIGGER IF NOT EXISTS patterns_au AFTER UPDATE ON patterns BEGIN
				INSERT INTO patterns_fts(patterns_fts, rowid, name, description, recommendation)
				VALUES('delete', old.id, old.name, old.description, old.recommendation);
				INSERT INTO patterns_fts(rowid, name, description, recommendation)
				VALUES (new.id, new.name, new.description, new.recommendation);
			END;
		`,
	},
	{
		Version: 23,
		Name:    "observation_relations",
		SQL: `
			-- Knowledge Graph: Observation Relations (Issue #4)
			-- Tracks explicit relationships between observations for knowledge graph navigation.
			-- Enables queries like "What caused this bug?" or "What depends on this decision?"
			CREATE TABLE IF NOT EXISTS observation_relations (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				source_id INTEGER NOT NULL,
				target_id INTEGER NOT NULL,
				relation_type TEXT NOT NULL CHECK(relation_type IN ('causes', 'fixes', 'supersedes', 'depends_on', 'relates_to', 'evolves_from')),
				confidence REAL NOT NULL DEFAULT 0.5,
				detection_source TEXT NOT NULL CHECK(detection_source IN ('file_overlap', 'embedding_similarity', 'temporal_proximity', 'narrative_mention', 'concept_overlap', 'type_progression')),
				reason TEXT,
				created_at TEXT NOT NULL,
				created_at_epoch INTEGER NOT NULL,
				FOREIGN KEY(source_id) REFERENCES observations(id) ON DELETE CASCADE,
				FOREIGN KEY(target_id) REFERENCES observations(id) ON DELETE CASCADE,
				UNIQUE(source_id, target_id, relation_type)
			);

			-- Index for finding relations by source observation
			CREATE INDEX IF NOT EXISTS idx_relations_source ON observation_relations(source_id);

			-- Index for finding relations by target observation
			CREATE INDEX IF NOT EXISTS idx_relations_target ON observation_relations(target_id);

			-- Index for relation type queries
			CREATE INDEX IF NOT EXISTS idx_relations_type ON observation_relations(relation_type);

			-- Index for confidence-based filtering
			CREATE INDEX IF NOT EXISTS idx_relations_confidence ON observation_relations(confidence DESC);

			-- Index for finding all relations involving an observation (either direction)
			CREATE INDEX IF NOT EXISTS idx_relations_both ON observation_relations(source_id, target_id);
		`,
	},
}

// MigrationManager handles database schema migrations.
type MigrationManager struct {
	db *sql.DB
}

// NewMigrationManager creates a new migration manager.
func NewMigrationManager(db *sql.DB) *MigrationManager {
	return &MigrationManager{db: db}
}

// EnsureSchemaVersionsTable creates the schema_versions table if it doesn't exist.
func (m *MigrationManager) EnsureSchemaVersionsTable() error {
	_, err := m.db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_versions (
			id INTEGER PRIMARY KEY,
			version INTEGER UNIQUE NOT NULL,
			applied_at TEXT NOT NULL
		)
	`)
	return err
}

// GetAppliedVersions returns all applied migration versions.
func (m *MigrationManager) GetAppliedVersions() (map[int]bool, error) {
	rows, err := m.db.Query("SELECT version FROM schema_versions ORDER BY version")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	versions := make(map[int]bool)
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return nil, err
		}
		versions[version] = true
	}
	return versions, rows.Err()
}

// ApplyMigration applies a single migration.
func (m *MigrationManager) ApplyMigration(migration Migration) error {
	tx, err := m.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Execute migration SQL
	if _, err := tx.Exec(migration.SQL); err != nil {
		return fmt.Errorf("execute migration %d (%s): %w", migration.Version, migration.Name, err)
	}

	// Record migration
	_, err = tx.Exec(
		"INSERT INTO schema_versions (version, applied_at) VALUES (?, ?)",
		migration.Version, time.Now().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("record migration %d: %w", migration.Version, err)
	}

	return tx.Commit()
}

// RunMigrations applies all pending migrations.
func (m *MigrationManager) RunMigrations() error {
	if err := m.EnsureSchemaVersionsTable(); err != nil {
		return fmt.Errorf("ensure schema_versions table: %w", err)
	}

	applied, err := m.GetAppliedVersions()
	if err != nil {
		return fmt.Errorf("get applied versions: %w", err)
	}

	for _, migration := range Migrations {
		if applied[migration.Version] {
			continue
		}

		if err := m.ApplyMigration(migration); err != nil {
			return err
		}
	}

	return nil
}
