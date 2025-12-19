// Package models contains domain models for claude-mnemonic.
package models

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"time"
)

// PatternType represents the category of detected pattern.
type PatternType string

const (
	// PatternTypeBug represents recurring bug patterns (e.g., "nil handling oversight").
	PatternTypeBug PatternType = "bug"
	// PatternTypeRefactor represents recurring refactoring approaches (e.g., "interface extraction").
	PatternTypeRefactor PatternType = "refactor"
	// PatternTypeArchitecture represents consistent architectural patterns.
	PatternTypeArchitecture PatternType = "architecture"
	// PatternTypeAntiPattern represents identified anti-patterns to avoid.
	PatternTypeAntiPattern PatternType = "anti-pattern"
	// PatternTypeBestPractice represents best practices that work consistently.
	PatternTypeBestPractice PatternType = "best-practice"
)

// PatternStatus represents the lifecycle status of a pattern.
type PatternStatus string

const (
	// PatternStatusActive means the pattern is actively being tracked and can be referenced.
	PatternStatusActive PatternStatus = "active"
	// PatternStatusDeprecated means the pattern has been superseded or is no longer relevant.
	PatternStatusDeprecated PatternStatus = "deprecated"
	// PatternStatusMerged means this pattern was merged into another pattern.
	PatternStatusMerged PatternStatus = "merged"
)

// Pattern represents a recurring pattern detected across observations.
// This enables Claude to reference historical insights: "I've encountered this pattern 12 times."
type Pattern struct {
	ID             int64           `db:"id" json:"id"`
	Name           string          `db:"name" json:"name"`                       // e.g., "State Management Anti-Pattern"
	Type           PatternType     `db:"type" json:"type"`                       // bug, refactor, architecture, etc.
	Description    sql.NullString  `db:"description" json:"description"`         // Detailed description
	Signature      JSONStringArray `db:"signature" json:"signature"`             // Keyword clusters for detection
	Recommendation sql.NullString  `db:"recommendation" json:"recommendation"`   // What works for this pattern
	Frequency      int             `db:"frequency" json:"frequency"`             // How many times encountered
	Projects       JSONStringArray `db:"projects" json:"projects"`               // Projects where this pattern was seen
	ObservationIDs JSONInt64Array  `db:"observation_ids" json:"observation_ids"` // Source observation IDs
	Status         PatternStatus   `db:"status" json:"status"`                   // active, deprecated, merged
	MergedIntoID   sql.NullInt64   `db:"merged_into_id" json:"merged_into_id,omitempty"`
	Confidence     float64         `db:"confidence" json:"confidence"`     // Detection confidence (0.0-1.0)
	LastSeenAt     string          `db:"last_seen_at" json:"last_seen_at"` // Last time pattern was detected
	LastSeenEpoch  int64           `db:"last_seen_at_epoch" json:"last_seen_at_epoch"`
	CreatedAt      string          `db:"created_at" json:"created_at"`
	CreatedAtEpoch int64           `db:"created_at_epoch" json:"created_at_epoch"`
}

// JSONInt64Array is a custom type for handling JSON int64 arrays in SQLite.
type JSONInt64Array []int64

// Scan implements sql.Scanner for JSONInt64Array.
func (j *JSONInt64Array) Scan(src interface{}) error {
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
		return nil
	}

	if len(data) == 0 {
		*j = nil
		return nil
	}

	return json.Unmarshal(data, j)
}

// Value implements driver.Valuer for JSONInt64Array.
func (j JSONInt64Array) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}

// PatternJSON is a JSON-friendly representation of Pattern.
type PatternJSON struct {
	ID             int64         `json:"id"`
	Name           string        `json:"name"`
	Type           PatternType   `json:"type"`
	Description    string        `json:"description,omitempty"`
	Signature      []string      `json:"signature,omitempty"`
	Recommendation string        `json:"recommendation,omitempty"`
	Frequency      int           `json:"frequency"`
	Projects       []string      `json:"projects,omitempty"`
	ObservationIDs []int64       `json:"observation_ids,omitempty"`
	Status         PatternStatus `json:"status"`
	MergedIntoID   int64         `json:"merged_into_id,omitempty"`
	Confidence     float64       `json:"confidence"`
	LastSeenAt     string        `json:"last_seen_at"`
	LastSeenEpoch  int64         `json:"last_seen_at_epoch"`
	CreatedAt      string        `json:"created_at"`
	CreatedAtEpoch int64         `json:"created_at_epoch"`
}

// MarshalJSON implements json.Marshaler for Pattern.
func (p *Pattern) MarshalJSON() ([]byte, error) {
	j := PatternJSON{
		ID:             p.ID,
		Name:           p.Name,
		Type:           p.Type,
		Signature:      p.Signature,
		Frequency:      p.Frequency,
		Projects:       p.Projects,
		ObservationIDs: p.ObservationIDs,
		Status:         p.Status,
		Confidence:     p.Confidence,
		LastSeenAt:     p.LastSeenAt,
		LastSeenEpoch:  p.LastSeenEpoch,
		CreatedAt:      p.CreatedAt,
		CreatedAtEpoch: p.CreatedAtEpoch,
	}
	if p.Description.Valid {
		j.Description = p.Description.String
	}
	if p.Recommendation.Valid {
		j.Recommendation = p.Recommendation.String
	}
	if p.MergedIntoID.Valid {
		j.MergedIntoID = p.MergedIntoID.Int64
	}
	return json.Marshal(j)
}

// NewPattern creates a new pattern from detected data.
func NewPattern(name string, patternType PatternType, description string, signature []string, project string, observationID int64) *Pattern {
	now := time.Now()
	return &Pattern{
		Name:           name,
		Type:           patternType,
		Description:    sql.NullString{String: description, Valid: description != ""},
		Signature:      signature,
		Frequency:      1,
		Projects:       []string{project},
		ObservationIDs: []int64{observationID},
		Status:         PatternStatusActive,
		Confidence:     0.5, // Initial confidence
		LastSeenAt:     now.Format(time.RFC3339),
		LastSeenEpoch:  now.UnixMilli(),
		CreatedAt:      now.Format(time.RFC3339),
		CreatedAtEpoch: now.UnixMilli(),
	}
}

// AddOccurrence records a new occurrence of this pattern.
func (p *Pattern) AddOccurrence(project string, observationID int64) {
	p.Frequency++

	// Add project if not already tracked
	found := false
	for _, proj := range p.Projects {
		if proj == project {
			found = true
			break
		}
	}
	if !found {
		p.Projects = append(p.Projects, project)
	}

	// Add observation ID if not already tracked
	for _, id := range p.ObservationIDs {
		if id == observationID {
			return
		}
	}
	p.ObservationIDs = append(p.ObservationIDs, observationID)

	// Update confidence based on frequency and cross-project occurrence
	p.updateConfidence()

	// Update last seen timestamp
	now := time.Now()
	p.LastSeenAt = now.Format(time.RFC3339)
	p.LastSeenEpoch = now.UnixMilli()
}

// updateConfidence adjusts confidence based on frequency and cross-project validation.
func (p *Pattern) updateConfidence() {
	// Base confidence from frequency (logarithmic scaling)
	freqConfidence := 0.3 + (0.4 * (float64(min(p.Frequency, 10)) / 10.0))

	// Cross-project bonus: patterns seen across multiple projects are more reliable
	projectBonus := 0.0
	if len(p.Projects) >= 2 {
		projectBonus = 0.1
	}
	if len(p.Projects) >= 5 {
		projectBonus = 0.2
	}

	p.Confidence = min(1.0, freqConfidence+projectBonus)
}

// PatternMatch represents a match between an observation and a potential pattern.
type PatternMatch struct {
	PatternID     int64   `json:"pattern_id"`
	Score         float64 `json:"score"`      // Match score (0.0-1.0)
	MatchedOn     string  `json:"matched_on"` // What triggered the match (concept, keyword, type, etc.)
	IsNew         bool    `json:"is_new"`     // Whether this would create a new pattern
	SuggestedName string  `json:"suggested_name,omitempty"`
}

// PatternSignatureKeywords are common keywords used in pattern detection.
var PatternSignatureKeywords = map[PatternType][]string{
	PatternTypeBug: {
		"nil", "null", "undefined", "panic", "crash", "error handling",
		"race condition", "deadlock", "memory leak", "overflow",
		"off-by-one", "boundary", "timeout", "concurrency",
	},
	PatternTypeRefactor: {
		"extract", "inline", "rename", "move", "split", "merge",
		"interface", "abstraction", "decouple", "simplify",
		"consolidate", "modularize", "encapsulate",
	},
	PatternTypeArchitecture: {
		"layer", "service", "repository", "controller", "handler",
		"middleware", "dependency injection", "factory", "singleton",
		"observer", "strategy", "adapter", "facade", "builder",
	},
	PatternTypeAntiPattern: {
		"god class", "spaghetti", "copy paste", "magic number",
		"hardcoded", "circular dependency", "premature optimization",
		"over-engineering", "feature envy", "data clump",
	},
	PatternTypeBestPractice: {
		"test", "validation", "logging", "monitoring", "documentation",
		"error handling", "retry", "timeout", "circuit breaker",
		"graceful shutdown", "health check", "metrics",
	},
}

// DetectPatternType analyzes concepts and content to determine pattern type.
func DetectPatternType(concepts []string, title, narrative string) PatternType {
	// Check concepts first
	for _, concept := range concepts {
		switch concept {
		case "anti-pattern":
			return PatternTypeAntiPattern
		case "best-practice":
			return PatternTypeBestPractice
		case "architecture":
			return PatternTypeArchitecture
		case "refactor":
			return PatternTypeRefactor
		}
	}

	// Check for bug-related patterns in content
	content := title + " " + narrative
	for _, keyword := range PatternSignatureKeywords[PatternTypeBug] {
		if containsIgnoreCase(content, keyword) {
			return PatternTypeBug
		}
	}

	// Default to refactor for other patterns
	return PatternTypeRefactor
}

// containsIgnoreCase checks if text contains substr (case-insensitive).
func containsIgnoreCase(text, substr string) bool {
	textLower := toLower(text)
	substrLower := toLower(substr)
	return contains(textLower, substrLower)
}

// Simple implementations to avoid strings package dependency in this file
func toLower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

func contains(s, substr string) bool {
	if len(substr) > len(s) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ExtractSignature creates a signature from observation content.
func ExtractSignature(concepts []string, title, narrative string) []string {
	var signature []string

	// Add all concepts
	signature = append(signature, concepts...)

	// Extract key terms from title (simple word extraction)
	for _, word := range splitWords(title) {
		if len(word) > 3 && isSignificantWord(word) {
			signature = append(signature, toLower(word))
		}
	}

	return uniqueStrings(signature)
}

// splitWords is a simple word splitter.
func splitWords(s string) []string {
	var words []string
	word := ""
	for _, r := range s {
		if r == ' ' || r == '-' || r == '_' || r == '.' || r == ',' {
			if word != "" {
				words = append(words, word)
				word = ""
			}
		} else {
			word += string(r)
		}
	}
	if word != "" {
		words = append(words, word)
	}
	return words
}

// isSignificantWord filters out common stop words.
func isSignificantWord(word string) bool {
	stopWords := map[string]bool{
		"the": true, "and": true, "for": true, "with": true, "that": true,
		"this": true, "from": true, "have": true, "not": true, "are": true,
		"was": true, "but": true, "all": true, "can": true, "had": true,
		"were": true, "been": true, "will": true, "when": true, "what": true,
	}
	return !stopWords[toLower(word)]
}

// uniqueStrings returns a slice with duplicate strings removed.
func uniqueStrings(s []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	return result
}

// CalculateMatchScore computes similarity between two signatures.
func CalculateMatchScore(sig1, sig2 []string) float64 {
	if len(sig1) == 0 || len(sig2) == 0 {
		return 0.0
	}

	set1 := make(map[string]bool)
	for _, s := range sig1 {
		set1[toLower(s)] = true
	}

	matches := 0
	for _, s := range sig2 {
		if set1[toLower(s)] {
			matches++
		}
	}

	// Jaccard similarity
	unionSize := len(sig1) + len(sig2) - matches
	if unionSize == 0 {
		return 0.0
	}
	return float64(matches) / float64(unionSize)
}
