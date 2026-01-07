// Package gorm provides GORM-based database operations for claude-mnemonic.
package gorm

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// runMigrations runs all database migrations using gormigrate.
func runMigrations(db *gorm.DB, sqlDB *sql.DB) error {
	m := gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{
		// Migration 001: Core tables (SDKSession, Observation, SessionSummary)
		{
			ID: "001_core_tables",
			Migrate: func(tx *gorm.DB) error {
				// AutoMigrate creates tables with all indexes from struct tags
				if err := tx.AutoMigrate(&SDKSession{}); err != nil {
					return err
				}
				if err := tx.AutoMigrate(&Observation{}); err != nil {
					return err
				}
				return tx.AutoMigrate(&SessionSummary{})
			},
			Rollback: func(tx *gorm.DB) error {
				return tx.Migrator().DropTable("sdk_sessions", "observations", "session_summaries")
			},
		},

		// Migration 002: User prompts table
		{
			ID: "002_user_prompts",
			Migrate: func(tx *gorm.DB) error {
				return tx.AutoMigrate(&UserPrompt{})
			},
			Rollback: func(tx *gorm.DB) error {
				return tx.Migrator().DropTable("user_prompts")
			},
		},

		// Migration 003: FTS5 virtual table for user prompts
		{
			ID: "003_user_prompts_fts",
			Migrate: func(tx *gorm.DB) error {
				sqls := []string{
					`CREATE VIRTUAL TABLE IF NOT EXISTS user_prompts_fts USING fts5(
						prompt_text,
						content='user_prompts',
						content_rowid='id'
					)`,
					`CREATE TRIGGER IF NOT EXISTS user_prompts_ai AFTER INSERT ON user_prompts BEGIN
						INSERT INTO user_prompts_fts(rowid, prompt_text)
						VALUES (new.id, new.prompt_text);
					END`,
					`CREATE TRIGGER IF NOT EXISTS user_prompts_ad AFTER DELETE ON user_prompts BEGIN
						INSERT INTO user_prompts_fts(user_prompts_fts, rowid, prompt_text)
						VALUES('delete', old.id, old.prompt_text);
					END`,
					`CREATE TRIGGER IF NOT EXISTS user_prompts_au AFTER UPDATE ON user_prompts BEGIN
						INSERT INTO user_prompts_fts(user_prompts_fts, rowid, prompt_text)
						VALUES('delete', old.id, old.prompt_text);
						INSERT INTO user_prompts_fts(rowid, prompt_text)
						VALUES (new.id, new.prompt_text);
					END`,
				}
				for _, s := range sqls {
					if err := tx.Exec(s).Error; err != nil {
						return err
					}
				}
				return nil
			},
			Rollback: func(tx *gorm.DB) error {
				sqls := []string{
					"DROP TRIGGER IF EXISTS user_prompts_au",
					"DROP TRIGGER IF EXISTS user_prompts_ad",
					"DROP TRIGGER IF EXISTS user_prompts_ai",
					"DROP TABLE IF EXISTS user_prompts_fts",
				}
				for _, s := range sqls {
					if err := tx.Exec(s).Error; err != nil {
						return err
					}
				}
				return nil
			},
		},

		// Migration 004: FTS5 virtual table for observations
		{
			ID: "004_observations_fts",
			Migrate: func(tx *gorm.DB) error {
				sqls := []string{
					`CREATE VIRTUAL TABLE IF NOT EXISTS observations_fts USING fts5(
						title, subtitle, narrative,
						content='observations',
						content_rowid='id'
					)`,
					`CREATE TRIGGER IF NOT EXISTS observations_ai AFTER INSERT ON observations BEGIN
						INSERT INTO observations_fts(rowid, title, subtitle, narrative)
						VALUES (new.id, new.title, new.subtitle, new.narrative);
					END`,
					`CREATE TRIGGER IF NOT EXISTS observations_ad AFTER DELETE ON observations BEGIN
						INSERT INTO observations_fts(observations_fts, rowid, title, subtitle, narrative)
						VALUES('delete', old.id, old.title, old.subtitle, old.narrative);
					END`,
					`CREATE TRIGGER IF NOT EXISTS observations_au AFTER UPDATE ON observations BEGIN
						INSERT INTO observations_fts(observations_fts, rowid, title, subtitle, narrative)
						VALUES('delete', old.id, old.title, old.subtitle, old.narrative);
						INSERT INTO observations_fts(rowid, title, subtitle, narrative)
						VALUES (new.id, new.title, new.subtitle, new.narrative);
					END`,
				}
				for _, s := range sqls {
					if err := tx.Exec(s).Error; err != nil {
						return err
					}
				}
				return nil
			},
			Rollback: func(tx *gorm.DB) error {
				sqls := []string{
					"DROP TRIGGER IF EXISTS observations_au",
					"DROP TRIGGER IF EXISTS observations_ad",
					"DROP TRIGGER IF EXISTS observations_ai",
					"DROP TABLE IF EXISTS observations_fts",
				}
				for _, s := range sqls {
					if err := tx.Exec(s).Error; err != nil {
						return err
					}
				}
				return nil
			},
		},

		// Migration 005: FTS5 virtual table for session summaries
		{
			ID: "005_session_summaries_fts",
			Migrate: func(tx *gorm.DB) error {
				sqls := []string{
					`CREATE VIRTUAL TABLE IF NOT EXISTS session_summaries_fts USING fts5(
						request, investigated, learned, completed, next_steps, notes,
						content='session_summaries',
						content_rowid='id'
					)`,
					`CREATE TRIGGER IF NOT EXISTS session_summaries_ai AFTER INSERT ON session_summaries BEGIN
						INSERT INTO session_summaries_fts(rowid, request, investigated, learned, completed, next_steps, notes)
						VALUES (new.id, new.request, new.investigated, new.learned, new.completed, new.next_steps, new.notes);
					END`,
					`CREATE TRIGGER IF NOT EXISTS session_summaries_ad AFTER DELETE ON session_summaries BEGIN
						INSERT INTO session_summaries_fts(session_summaries_fts, rowid, request, investigated, learned, completed, next_steps, notes)
						VALUES('delete', old.id, old.request, old.investigated, old.learned, old.completed, old.next_steps, old.notes);
					END`,
					`CREATE TRIGGER IF NOT EXISTS session_summaries_au AFTER UPDATE ON session_summaries BEGIN
						INSERT INTO session_summaries_fts(session_summaries_fts, rowid, request, investigated, learned, completed, next_steps, notes)
						VALUES('delete', old.id, old.request, old.investigated, old.learned, old.completed, old.next_steps, old.notes);
						INSERT INTO session_summaries_fts(rowid, request, investigated, learned, completed, next_steps, notes)
						VALUES (new.id, new.request, new.investigated, new.learned, new.completed, new.next_steps, new.notes);
					END`,
				}
				for _, s := range sqls {
					if err := tx.Exec(s).Error; err != nil {
						return err
					}
				}
				return nil
			},
			Rollback: func(tx *gorm.DB) error {
				sqls := []string{
					"DROP TRIGGER IF EXISTS session_summaries_au",
					"DROP TRIGGER IF EXISTS session_summaries_ad",
					"DROP TRIGGER IF EXISTS session_summaries_ai",
					"DROP TABLE IF EXISTS session_summaries_fts",
				}
				for _, s := range sqls {
					if err := tx.Exec(s).Error; err != nil {
						return err
					}
				}
				return nil
			},
		},

		// Migration 006: sqlite-vec vectors table
		{
			ID: "006_sqlite_vec_vectors",
			Migrate: func(tx *gorm.DB) error {
				// Note: Uses bge-small-en-v1.5 embeddings (384 dimensions) with model_version
				sql := `CREATE VIRTUAL TABLE IF NOT EXISTS vectors USING vec0(
					doc_id TEXT PRIMARY KEY,
					embedding float[384],
					sqlite_id INTEGER,
					doc_type TEXT,
					field_type TEXT,
					project TEXT,
					scope TEXT,
					model_version TEXT
				)`
				return tx.Exec(sql).Error
			},
			Rollback: func(tx *gorm.DB) error {
				return tx.Exec("DROP TABLE IF EXISTS vectors").Error
			},
		},

		// Migration 007: Concept weights table with seed data
		{
			ID: "007_concept_weights",
			Migrate: func(tx *gorm.DB) error {
				if err := tx.AutoMigrate(&ConceptWeight{}); err != nil {
					return err
				}

				// Seed default concept weights
				now := time.Now().Format(time.RFC3339)
				weights := []ConceptWeight{
					{Concept: "security", Weight: 0.30, UpdatedAt: now},
					{Concept: "gotcha", Weight: 0.25, UpdatedAt: now},
					{Concept: "best-practice", Weight: 0.20, UpdatedAt: now},
					{Concept: "anti-pattern", Weight: 0.20, UpdatedAt: now},
					{Concept: "architecture", Weight: 0.15, UpdatedAt: now},
					{Concept: "performance", Weight: 0.15, UpdatedAt: now},
					{Concept: "error-handling", Weight: 0.15, UpdatedAt: now},
					{Concept: "pattern", Weight: 0.10, UpdatedAt: now},
					{Concept: "testing", Weight: 0.10, UpdatedAt: now},
					{Concept: "debugging", Weight: 0.10, UpdatedAt: now},
					{Concept: "workflow", Weight: 0.05, UpdatedAt: now},
					{Concept: "tooling", Weight: 0.05, UpdatedAt: now},
				}

				// INSERT OR IGNORE equivalent in GORM
				return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&weights).Error
			},
			Rollback: func(tx *gorm.DB) error {
				return tx.Migrator().DropTable("concept_weights")
			},
		},

		// Migration 008: Observation conflicts table
		{
			ID: "008_observation_conflicts",
			Migrate: func(tx *gorm.DB) error {
				return tx.AutoMigrate(&ObservationConflict{})
			},
			Rollback: func(tx *gorm.DB) error {
				return tx.Migrator().DropTable("observation_conflicts")
			},
		},

		// Migration 009: Patterns table
		{
			ID: "009_patterns",
			Migrate: func(tx *gorm.DB) error {
				return tx.AutoMigrate(&Pattern{})
			},
			Rollback: func(tx *gorm.DB) error {
				return tx.Migrator().DropTable("patterns")
			},
		},

		// Migration 010: FTS5 virtual table for patterns
		{
			ID: "010_patterns_fts",
			Migrate: func(tx *gorm.DB) error {
				sqls := []string{
					`CREATE VIRTUAL TABLE IF NOT EXISTS patterns_fts USING fts5(
						name, description, recommendation,
						content='patterns',
						content_rowid='id'
					)`,
					`CREATE TRIGGER IF NOT EXISTS patterns_ai AFTER INSERT ON patterns BEGIN
						INSERT INTO patterns_fts(rowid, name, description, recommendation)
						VALUES (new.id, new.name, new.description, new.recommendation);
					END`,
					`CREATE TRIGGER IF NOT EXISTS patterns_ad AFTER DELETE ON patterns BEGIN
						INSERT INTO patterns_fts(patterns_fts, rowid, name, description, recommendation)
						VALUES('delete', old.id, old.name, old.description, old.recommendation);
					END`,
					`CREATE TRIGGER IF NOT EXISTS patterns_au AFTER UPDATE ON patterns BEGIN
						INSERT INTO patterns_fts(patterns_fts, rowid, name, description, recommendation)
						VALUES('delete', old.id, old.name, old.description, old.recommendation);
						INSERT INTO patterns_fts(rowid, name, description, recommendation)
						VALUES (new.id, new.name, new.description, new.recommendation);
					END`,
				}
				for _, s := range sqls {
					if err := tx.Exec(s).Error; err != nil {
						return err
					}
				}
				return nil
			},
			Rollback: func(tx *gorm.DB) error {
				sqls := []string{
					"DROP TRIGGER IF EXISTS patterns_au",
					"DROP TRIGGER IF EXISTS patterns_ad",
					"DROP TRIGGER IF EXISTS patterns_ai",
					"DROP TABLE IF EXISTS patterns_fts",
				}
				for _, s := range sqls {
					if err := tx.Exec(s).Error; err != nil {
						return err
					}
				}
				return nil
			},
		},

		// Migration 011: Observation relations table
		{
			ID: "011_observation_relations",
			Migrate: func(tx *gorm.DB) error {
				return tx.AutoMigrate(&ObservationRelation{})
			},
			Rollback: func(tx *gorm.DB) error {
				return tx.Migrator().DropTable("observation_relations")
			},
		},
	})

	if err := m.Migrate(); err != nil {
		return fmt.Errorf("run gormigrate migrations: %w", err)
	}

	return nil
}
