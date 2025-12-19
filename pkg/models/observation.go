// Package models contains domain models for claude-mnemonic.
package models

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// ObservationType represents the type of observation.
type ObservationType string

const (
	ObsTypeDecision  ObservationType = "decision"
	ObsTypeBugfix    ObservationType = "bugfix"
	ObsTypeFeature   ObservationType = "feature"
	ObsTypeRefactor  ObservationType = "refactor"
	ObsTypeDiscovery ObservationType = "discovery"
	ObsTypeChange    ObservationType = "change"
)

// ObservationScope defines the visibility scope of an observation.
type ObservationScope string

const (
	// ScopeProject means the observation is only visible within the same project.
	ScopeProject ObservationScope = "project"
	// ScopeGlobal means the observation is visible across all projects.
	// Used for best practices, advanced patterns, and generalizable knowledge.
	ScopeGlobal ObservationScope = "global"
)

// GlobalizableConcepts are concept tags that indicate an observation
// should be considered for global scope (best practices, patterns, etc.)
var GlobalizableConcepts = []string{
	"best-practice",
	"pattern",
	"anti-pattern",
	"architecture",
	"security",
	"performance",
	"testing",
	"debugging",
	"workflow",
	"tooling",
}

// JSONStringArray is a custom type for handling JSON string arrays in SQLite.
type JSONStringArray []string

// Scan implements sql.Scanner for JSONStringArray.
func (j *JSONStringArray) Scan(src interface{}) error {
	if src == nil {
		*j = nil
		return nil
	}

	var data []byte
	switch v := src.(type) {
	case string:
		data = []byte(v)
	case []byte:
		data = v
	default:
		return fmt.Errorf("JSONStringArray: unsupported type %T", src)
	}

	if len(data) == 0 {
		*j = nil
		return nil
	}

	return json.Unmarshal(data, j)
}

// Value implements driver.Valuer for JSONStringArray.
func (j JSONStringArray) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}

// JSONInt64Map is a custom type for handling JSON int64 maps in SQLite.
type JSONInt64Map map[string]int64

// Scan implements sql.Scanner for JSONInt64Map.
func (j *JSONInt64Map) Scan(src interface{}) error {
	if src == nil {
		*j = nil
		return nil
	}

	var data []byte
	switch v := src.(type) {
	case string:
		data = []byte(v)
	case []byte:
		data = v
	default:
		return fmt.Errorf("JSONInt64Map: unsupported type %T", src)
	}

	if len(data) == 0 {
		*j = nil
		return nil
	}

	return json.Unmarshal(data, j)
}

// Value implements driver.Valuer for JSONInt64Map.
func (j JSONInt64Map) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}

// Observation represents a learning extracted from a Claude Code session.
type Observation struct {
	ID              int64            `db:"id" json:"id"`
	SDKSessionID    string           `db:"sdk_session_id" json:"sdk_session_id"`
	Project         string           `db:"project" json:"project"`
	Scope           ObservationScope `db:"scope" json:"scope"`
	Type            ObservationType  `db:"type" json:"type"`
	Title           sql.NullString   `db:"title" json:"title,omitempty"`
	Subtitle        sql.NullString   `db:"subtitle" json:"subtitle,omitempty"`
	Facts           JSONStringArray  `db:"facts" json:"facts,omitempty"`
	Narrative       sql.NullString   `db:"narrative" json:"narrative,omitempty"`
	Concepts        JSONStringArray  `db:"concepts" json:"concepts,omitempty"`
	FilesRead       JSONStringArray  `db:"files_read" json:"files_read,omitempty"`
	FilesModified   JSONStringArray  `db:"files_modified" json:"files_modified,omitempty"`
	FileMtimes      JSONInt64Map     `db:"file_mtimes" json:"file_mtimes,omitempty"`
	PromptNumber    sql.NullInt64    `db:"prompt_number" json:"prompt_number,omitempty"`
	DiscoveryTokens int64            `db:"discovery_tokens" json:"discovery_tokens"`
	CreatedAt       string           `db:"created_at" json:"created_at"`
	CreatedAtEpoch  int64            `db:"created_at_epoch" json:"created_at_epoch"`
	IsStale         bool             `db:"-" json:"is_stale,omitempty"`

	// Importance scoring fields
	ImportanceScore float64       `db:"importance_score" json:"importance_score"`
	UserFeedback    int           `db:"user_feedback" json:"user_feedback"`
	RetrievalCount  int           `db:"retrieval_count" json:"retrieval_count"`
	LastRetrievedAt sql.NullInt64 `db:"last_retrieved_at_epoch" json:"last_retrieved_at_epoch,omitempty"`
	ScoreUpdatedAt  sql.NullInt64 `db:"score_updated_at_epoch" json:"score_updated_at_epoch,omitempty"`

	// Conflict detection fields
	IsSuperseded bool `db:"is_superseded" json:"is_superseded,omitempty"`
}

// ParsedObservation represents an observation parsed from SDK response XML.
type ParsedObservation struct {
	Type          ObservationType
	Title         string
	Subtitle      string
	Facts         []string
	Narrative     string
	Concepts      []string
	FilesRead     []string
	FilesModified []string
	FileMtimes    map[string]int64 // File path -> mtime epoch ms
	Scope         ObservationScope // Optional: if empty, will be auto-determined
}

// ToStoredObservation converts a ParsedObservation to the stored Observation format.
// Used for similarity comparison before storage.
func (p *ParsedObservation) ToStoredObservation() *Observation {
	return &Observation{
		Type:          p.Type,
		Title:         sql.NullString{String: p.Title, Valid: p.Title != ""},
		Subtitle:      sql.NullString{String: p.Subtitle, Valid: p.Subtitle != ""},
		Facts:         p.Facts,
		Narrative:     sql.NullString{String: p.Narrative, Valid: p.Narrative != ""},
		Concepts:      p.Concepts,
		FilesRead:     p.FilesRead,
		FilesModified: p.FilesModified,
		FileMtimes:    p.FileMtimes,
	}
}

// DetermineScope determines the appropriate scope based on observation concepts.
// Returns ScopeGlobal if any concept matches globalizable patterns, else ScopeProject.
func DetermineScope(concepts []string) ObservationScope {
	for _, concept := range concepts {
		for _, globalConcept := range GlobalizableConcepts {
			if concept == globalConcept {
				return ScopeGlobal
			}
		}
	}
	return ScopeProject
}

// ObservationJSON is a JSON-friendly representation of Observation.
// It converts sql.NullString to plain strings for clean JSON output.
type ObservationJSON struct {
	ID              int64            `json:"id"`
	SDKSessionID    string           `json:"sdk_session_id"`
	Project         string           `json:"project"`
	Scope           ObservationScope `json:"scope"`
	Type            ObservationType  `json:"type"`
	Title           string           `json:"title,omitempty"`
	Subtitle        string           `json:"subtitle,omitempty"`
	Facts           []string         `json:"facts,omitempty"`
	Narrative       string           `json:"narrative,omitempty"`
	Concepts        []string         `json:"concepts,omitempty"`
	FilesRead       []string         `json:"files_read,omitempty"`
	FilesModified   []string         `json:"files_modified,omitempty"`
	FileMtimes      map[string]int64 `json:"file_mtimes,omitempty"`
	PromptNumber    int64            `json:"prompt_number,omitempty"`
	DiscoveryTokens int64            `json:"discovery_tokens"`
	CreatedAt       string           `json:"created_at"`
	CreatedAtEpoch  int64            `json:"created_at_epoch"`
	IsStale         bool             `json:"is_stale,omitempty"`

	// Importance scoring fields
	ImportanceScore float64 `json:"importance_score"`
	UserFeedback    int     `json:"user_feedback"`
	RetrievalCount  int     `json:"retrieval_count"`
	LastRetrievedAt int64   `json:"last_retrieved_at_epoch,omitempty"`
	ScoreUpdatedAt  int64   `json:"score_updated_at_epoch,omitempty"`

	// Conflict detection fields
	IsSuperseded bool `json:"is_superseded,omitempty"`
}

// MarshalJSON implements json.Marshaler for Observation.
// Converts sql.NullString fields to plain strings.
func (o *Observation) MarshalJSON() ([]byte, error) {
	j := ObservationJSON{
		ID:              o.ID,
		SDKSessionID:    o.SDKSessionID,
		Project:         o.Project,
		Scope:           o.Scope,
		Type:            o.Type,
		Facts:           o.Facts,
		Concepts:        o.Concepts,
		FilesRead:       o.FilesRead,
		FilesModified:   o.FilesModified,
		FileMtimes:      o.FileMtimes,
		DiscoveryTokens: o.DiscoveryTokens,
		CreatedAt:       o.CreatedAt,
		CreatedAtEpoch:  o.CreatedAtEpoch,
		IsStale:         o.IsStale,
		// Importance scoring fields
		ImportanceScore: o.ImportanceScore,
		UserFeedback:    o.UserFeedback,
		RetrievalCount:  o.RetrievalCount,
		// Conflict detection fields
		IsSuperseded: o.IsSuperseded,
	}
	if o.Title.Valid {
		j.Title = o.Title.String
	}
	if o.Subtitle.Valid {
		j.Subtitle = o.Subtitle.String
	}
	if o.Narrative.Valid {
		j.Narrative = o.Narrative.String
	}
	if o.PromptNumber.Valid {
		j.PromptNumber = o.PromptNumber.Int64
	}
	if o.LastRetrievedAt.Valid {
		j.LastRetrievedAt = o.LastRetrievedAt.Int64
	}
	if o.ScoreUpdatedAt.Valid {
		j.ScoreUpdatedAt = o.ScoreUpdatedAt.Int64
	}
	return json.Marshal(j)
}

// NewObservation creates a new observation from parsed data.
func NewObservation(sdkSessionID, project string, parsed *ParsedObservation, promptNumber int, discoveryTokens int64) *Observation {
	now := time.Now()

	// Determine scope: use parsed scope if set, otherwise auto-determine from concepts
	scope := parsed.Scope
	if scope == "" {
		scope = DetermineScope(parsed.Concepts)
	}

	return &Observation{
		SDKSessionID:    sdkSessionID,
		Project:         project,
		Scope:           scope,
		Type:            parsed.Type,
		Title:           sql.NullString{String: parsed.Title, Valid: parsed.Title != ""},
		Subtitle:        sql.NullString{String: parsed.Subtitle, Valid: parsed.Subtitle != ""},
		Facts:           parsed.Facts,
		Narrative:       sql.NullString{String: parsed.Narrative, Valid: parsed.Narrative != ""},
		Concepts:        parsed.Concepts,
		FilesRead:       parsed.FilesRead,
		FilesModified:   parsed.FilesModified,
		FileMtimes:      parsed.FileMtimes,
		PromptNumber:    sql.NullInt64{Int64: int64(promptNumber), Valid: promptNumber > 0},
		DiscoveryTokens: discoveryTokens,
		CreatedAt:       now.Format(time.RFC3339),
		CreatedAtEpoch:  now.UnixMilli(),
		// Importance scoring: new observations start with score 1.0
		ImportanceScore: 1.0,
		UserFeedback:    0,
		RetrievalCount:  0,
	}
}

// ToMap converts the observation to a map for JSON response building.
// This allows adding extra fields like similarity scores.
func (o *Observation) ToMap() map[string]interface{} {
	// Marshal to JSON then unmarshal to map (uses MarshalJSON for proper conversion)
	data, err := json.Marshal(o)
	if err != nil {
		return map[string]interface{}{"id": o.ID, "error": err.Error()}
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return map[string]interface{}{"id": o.ID, "error": err.Error()}
	}
	return result
}

// CheckStaleness checks if an observation is stale based on current file mtimes.
// Returns true if any tracked file has been modified since the observation was created.
func (o *Observation) CheckStaleness(currentMtimes map[string]int64) bool {
	if len(o.FileMtimes) == 0 {
		return false // No file tracking, assume fresh
	}

	for path, recordedMtime := range o.FileMtimes {
		if currentMtime, exists := currentMtimes[path]; exists {
			if currentMtime > recordedMtime {
				return true // File was modified since observation was created
			}
		}
		// If file doesn't exist in currentMtimes, it may have been deleted
		// We don't mark as stale for missing files - they might just not be checked
	}
	return false
}
