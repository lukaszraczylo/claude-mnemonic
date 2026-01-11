// Package gorm provides GORM-based database operations for claude-mnemonic.
package gorm

import (
	"database/sql"
	"time"

	"gorm.io/gorm"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
)

// GORM Models

// Note: JSON types (JSONStringArray, JSONInt64Map) are imported from pkg/models
// and already implement sql.Scanner and driver.Valuer interfaces.

// SDKSession represents a Claude Code session.
type SDKSession struct {
	ClaudeSessionID  string         `gorm:"uniqueIndex;not null"`
	Project          string         `gorm:"index;not null"`
	Status           string         `gorm:"type:text;check:status IN ('active', 'completed', 'failed');default:'active';index"`
	StartedAt        string         `gorm:"not null"`
	SDKSessionID     sql.NullString `gorm:"uniqueIndex"`
	UserPrompt       sql.NullString
	CompletedAt      sql.NullString
	WorkerPort       sql.NullInt64
	CompletedAtEpoch sql.NullInt64
	ID               int64 `gorm:"primaryKey;autoIncrement"`
	PromptCounter    int   `gorm:"default:0"`
	StartedAtEpoch   int64 `gorm:"index:idx_sessions_started,sort:desc;not null"`
}

func (SDKSession) TableName() string { return "sdk_sessions" }

// BeforeCreate hook to ensure timestamps are set.
func (s *SDKSession) BeforeCreate(tx *gorm.DB) error {
	if s.StartedAtEpoch == 0 {
		s.StartedAtEpoch = time.Now().UnixMilli()
	}
	if s.StartedAt == "" {
		s.StartedAt = time.Now().Format(time.RFC3339)
	}
	return nil
}

// Observation represents a stored observation (learning).
// Field order optimized for memory alignment (fieldalignment).
type Observation struct {
	FileMtimes      models.JSONInt64Map     `gorm:"type:text"`
	SDKSessionID    string                  `gorm:"index;not null"`
	Project         string                  `gorm:"index:idx_observations_project;index:idx_observations_project_created,priority:1;not null"`
	Scope           models.ObservationScope `gorm:"type:text;default:'project';check:scope IN ('project', 'global');index:idx_observations_scope;index:idx_observations_project_scope,priority:2"`
	Type            models.ObservationType  `gorm:"type:text;check:type IN ('decision', 'bugfix', 'feature', 'refactor', 'discovery', 'change');index;not null"`
	CreatedAt       string                  `gorm:"not null"`
	Facts           models.JSONStringArray  `gorm:"type:text"`
	Narrative       sql.NullString          `gorm:"type:text"`
	Concepts        models.JSONStringArray  `gorm:"type:text"`
	FilesRead       models.JSONStringArray  `gorm:"type:text"`
	FilesModified   models.JSONStringArray  `gorm:"type:text"`
	Subtitle        sql.NullString          `gorm:"type:text"`
	Title           sql.NullString          `gorm:"type:text"`
	ArchivedReason  sql.NullString
	ScoreUpdatedAt  sql.NullInt64 `gorm:"column:score_updated_at_epoch;index:idx_observations_score_updated"`
	PromptNumber    sql.NullInt64
	ArchivedAt      sql.NullInt64 `gorm:"column:archived_at_epoch"`
	LastRetrievedAt sql.NullInt64 `gorm:"column:last_retrieved_at_epoch"`
	ID              int64         `gorm:"primaryKey;autoIncrement"`
	ImportanceScore float64       `gorm:"type:real;default:1.0;index:idx_observations_importance,priority:1,sort:desc"`
	UserFeedback    int           `gorm:"default:0"`
	RetrievalCount  int           `gorm:"default:0"`
	CreatedAtEpoch  int64         `gorm:"index:idx_observations_created,sort:desc;index:idx_observations_project_created,priority:2,sort:desc;not null"`
	DiscoveryTokens int64         `gorm:"default:0"`
	IsSuperseded    int           `gorm:"default:0;index:idx_observations_superseded;index:idx_observations_active,priority:2"`
	IsArchived      int           `gorm:"default:0;index:idx_observations_archived;index:idx_observations_active,priority:1"`
}

func (Observation) TableName() string { return "observations" }

// BeforeCreate hook to ensure defaults are set.
func (o *Observation) BeforeCreate(tx *gorm.DB) error {
	if o.CreatedAtEpoch == 0 {
		o.CreatedAtEpoch = time.Now().UnixMilli()
	}
	if o.CreatedAt == "" {
		o.CreatedAt = time.Now().Format(time.RFC3339)
	}
	if o.ImportanceScore == 0 {
		o.ImportanceScore = 1.0
	}
	return nil
}

// SessionSummary represents a session summary.
type SessionSummary struct {
	CreatedAt       string `gorm:"not null"`
	SDKSessionID    string `gorm:"index;not null"`
	Project         string `gorm:"index;not null"`
	Completed       sql.NullString
	Investigated    sql.NullString
	Learned         sql.NullString
	NextSteps       sql.NullString `gorm:"column:next_steps"`
	Notes           sql.NullString
	Request         sql.NullString
	PromptNumber    sql.NullInt64
	ID              int64 `gorm:"primaryKey;autoIncrement"`
	DiscoveryTokens int64 `gorm:"default:0"`
	CreatedAtEpoch  int64 `gorm:"index:idx_summaries_created,sort:desc;not null"`
}

func (SessionSummary) TableName() string { return "session_summaries" }

// BeforeCreate hook to ensure timestamps are set.
func (s *SessionSummary) BeforeCreate(tx *gorm.DB) error {
	if s.CreatedAtEpoch == 0 {
		s.CreatedAtEpoch = time.Now().UnixMilli()
	}
	if s.CreatedAt == "" {
		s.CreatedAt = time.Now().Format(time.RFC3339)
	}
	return nil
}

// UserPrompt represents a user prompt.
type UserPrompt struct {
	ClaudeSessionID     string `gorm:"index;not null;uniqueIndex:idx_user_prompts_session_number_unique,priority:1"`
	PromptText          string `gorm:"type:text;not null"`
	CreatedAt           string `gorm:"not null"`
	ID                  int64  `gorm:"primaryKey;autoIncrement"`
	PromptNumber        int    `gorm:"index;not null;uniqueIndex:idx_user_prompts_session_number_unique,priority:2"`
	MatchedObservations int    `gorm:"default:0"`
	CreatedAtEpoch      int64  `gorm:"index:idx_prompts_created,sort:desc;not null"`
}

func (UserPrompt) TableName() string { return "user_prompts" }

// BeforeCreate hook to ensure timestamps are set.
func (p *UserPrompt) BeforeCreate(tx *gorm.DB) error {
	if p.CreatedAtEpoch == 0 {
		p.CreatedAtEpoch = time.Now().UnixMilli()
	}
	if p.CreatedAt == "" {
		p.CreatedAt = time.Now().Format(time.RFC3339)
	}
	return nil
}

// ObservationConflict tracks conflicts between observations.
type ObservationConflict struct {
	ConflictType    models.ConflictType       `gorm:"type:text;check:conflict_type IN ('superseded', 'contradicts', 'outdated_pattern');not null"`
	Resolution      models.ConflictResolution `gorm:"type:text;check:resolution IN ('prefer_newer', 'prefer_older', 'manual');not null"`
	DetectedAt      string                    `gorm:"not null"`
	Reason          sql.NullString            `gorm:"type:text"`
	ResolvedAt      sql.NullString
	ID              int64 `gorm:"primaryKey;autoIncrement"`
	NewerObsID      int64 `gorm:"index:idx_conflicts_newer;not null"`
	OlderObsID      int64 `gorm:"index:idx_conflicts_older;not null"`
	DetectedAtEpoch int64 `gorm:"index:idx_conflicts_unresolved,priority:2,sort:desc;not null"`
	Resolved        int   `gorm:"default:0;index:idx_conflicts_unresolved,priority:1"`
}

func (ObservationConflict) TableName() string { return "observation_conflicts" }

// BeforeCreate hook to ensure timestamps are set.
func (c *ObservationConflict) BeforeCreate(tx *gorm.DB) error {
	if c.DetectedAtEpoch == 0 {
		c.DetectedAtEpoch = time.Now().UnixMilli()
	}
	if c.DetectedAt == "" {
		c.DetectedAt = time.Now().Format(time.RFC3339)
	}
	return nil
}

// ObservationRelation tracks relationships between observations.
type ObservationRelation struct {
	RelationType    models.RelationType            `gorm:"type:text;check:relation_type IN ('causes', 'fixes', 'supersedes', 'depends_on', 'relates_to', 'evolves_from');index:idx_relations_type;uniqueIndex:idx_relations_unique,priority:3;not null"`
	DetectionSource models.RelationDetectionSource `gorm:"type:text;check:detection_source IN ('file_overlap', 'embedding_similarity', 'temporal_proximity', 'narrative_mention', 'concept_overlap', 'type_progression');not null"`
	CreatedAt       string                         `gorm:"not null"`
	Reason          sql.NullString                 `gorm:"type:text"`
	ID              int64                          `gorm:"primaryKey;autoIncrement"`
	SourceID        int64                          `gorm:"index:idx_relations_source;index:idx_relations_both,priority:1;uniqueIndex:idx_relations_unique,priority:1;not null"`
	TargetID        int64                          `gorm:"index:idx_relations_target;index:idx_relations_both,priority:2;uniqueIndex:idx_relations_unique,priority:2;not null"`
	Confidence      float64                        `gorm:"type:real;default:0.5;index:idx_relations_confidence,sort:desc;not null"`
	CreatedAtEpoch  int64                          `gorm:"not null"`
}

func (ObservationRelation) TableName() string { return "observation_relations" }

// BeforeCreate hook to ensure timestamps are set.
func (r *ObservationRelation) BeforeCreate(tx *gorm.DB) error {
	if r.CreatedAtEpoch == 0 {
		r.CreatedAtEpoch = time.Now().UnixMilli()
	}
	if r.CreatedAt == "" {
		r.CreatedAt = time.Now().Format(time.RFC3339)
	}
	if r.Confidence == 0 {
		r.Confidence = 0.5
	}
	return nil
}

// Pattern represents a detected recurring pattern.
type Pattern struct {
	Status          models.PatternStatus   `gorm:"type:text;default:'active';check:status IN ('active', 'deprecated', 'merged');index"`
	Name            string                 `gorm:"type:text;not null"`
	Type            models.PatternType     `gorm:"type:text;check:type IN ('bug', 'refactor', 'architecture', 'anti-pattern', 'best-practice');index;not null"`
	CreatedAt       string                 `gorm:"not null"`
	LastSeenAt      string                 `gorm:"not null"`
	Signature       models.JSONStringArray `gorm:"type:text"`
	Projects        models.JSONStringArray `gorm:"type:text"`
	ObservationIDs  models.JSONInt64Array  `gorm:"type:text"`
	Recommendation  sql.NullString         `gorm:"type:text"`
	Description     sql.NullString         `gorm:"type:text"`
	MergedIntoID    sql.NullInt64
	Frequency       int     `gorm:"default:1;index:idx_patterns_frequency,sort:desc"`
	Confidence      float64 `gorm:"type:real;default:0.5;index:idx_patterns_confidence,sort:desc"`
	ID              int64   `gorm:"primaryKey;autoIncrement"`
	LastSeenAtEpoch int64   `gorm:"index:idx_patterns_last_seen,sort:desc;not null"`
	CreatedAtEpoch  int64   `gorm:"not null"`
}

func (Pattern) TableName() string { return "patterns" }

// BeforeCreate hook to ensure timestamps and defaults are set.
func (p *Pattern) BeforeCreate(tx *gorm.DB) error {
	now := time.Now()
	if p.CreatedAtEpoch == 0 {
		p.CreatedAtEpoch = now.UnixMilli()
	}
	if p.CreatedAt == "" {
		p.CreatedAt = now.Format(time.RFC3339)
	}
	if p.LastSeenAtEpoch == 0 {
		p.LastSeenAtEpoch = now.UnixMilli()
	}
	if p.LastSeenAt == "" {
		p.LastSeenAt = now.Format(time.RFC3339)
	}
	if p.Confidence == 0 {
		p.Confidence = 0.5
	}
	if p.Frequency == 0 {
		p.Frequency = 1
	}
	return nil
}

// ConceptWeight stores configurable weights for importance scoring.
type ConceptWeight struct {
	Concept   string  `gorm:"primaryKey;type:text"`
	UpdatedAt string  `gorm:"not null"`
	Weight    float64 `gorm:"type:real;not null;default:0.1"`
}

func (ConceptWeight) TableName() string { return "concept_weights" }

// BeforeCreate hook to ensure timestamp is set.
func (c *ConceptWeight) BeforeCreate(tx *gorm.DB) error {
	if c.UpdatedAt == "" {
		c.UpdatedAt = time.Now().Format(time.RFC3339)
	}
	if c.Weight == 0 {
		c.Weight = 0.1
	}
	return nil
}
