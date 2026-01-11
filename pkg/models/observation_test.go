// Package models contains domain models for claude-mnemonic.
package models

import (
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// ObservationSuite is a test suite for Observation operations.
type ObservationSuite struct {
	suite.Suite
}

func TestObservationSuite(t *testing.T) {
	suite.Run(t, new(ObservationSuite))
}

// TestObservationTypeConstants tests observation type constants.
func (s *ObservationSuite) TestObservationTypeConstants() {
	s.Equal(ObservationType("discovery"), ObsTypeDiscovery)
	s.Equal(ObservationType("decision"), ObsTypeDecision)
	s.Equal(ObservationType("bugfix"), ObsTypeBugfix)
	s.Equal(ObservationType("feature"), ObsTypeFeature)
	s.Equal(ObservationType("refactor"), ObsTypeRefactor)
	s.Equal(ObservationType("change"), ObsTypeChange)
}

// TestScopeConstants tests scope constants.
func (s *ObservationSuite) TestScopeConstants() {
	s.Equal(ObservationScope("project"), ScopeProject)
	s.Equal(ObservationScope("global"), ScopeGlobal)
}

// TestGlobalizableConcepts tests that globalizable concepts are defined.
func (s *ObservationSuite) TestGlobalizableConcepts() {
	expected := []string{
		"best-practice", "pattern", "anti-pattern", "architecture",
		"security", "performance", "testing",
		"debugging", "workflow", "tooling",
	}
	s.Equal(expected, GlobalizableConcepts)
}

// TestDetermineScope_TableDriven tests scope determination with various concepts.
func (s *ObservationSuite) TestDetermineScope_TableDriven() {
	tests := []struct {
		name     string
		expected ObservationScope
		concepts []string
	}{
		{
			name:     "empty concepts - project scope",
			concepts: []string{},
			expected: ScopeProject,
		},
		{
			name:     "no globalizable concepts - project scope",
			concepts: []string{"how-it-works", "custom-tag"},
			expected: ScopeProject,
		},
		{
			name:     "security concept - global scope",
			concepts: []string{"security"},
			expected: ScopeGlobal,
		},
		{
			name:     "best-practice concept - global scope",
			concepts: []string{"best-practice"},
			expected: ScopeGlobal,
		},
		{
			name:     "mixed concepts with globalizable - global scope",
			concepts: []string{"how-it-works", "security"},
			expected: ScopeGlobal,
		},
		{
			name:     "performance concept - global scope",
			concepts: []string{"performance"},
			expected: ScopeGlobal,
		},
		{
			name:     "testing concept - global scope",
			concepts: []string{"testing"},
			expected: ScopeGlobal,
		},
		{
			name:     "pattern concept - global scope",
			concepts: []string{"pattern"},
			expected: ScopeGlobal,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			result := DetermineScope(tt.concepts)
			s.Equal(tt.expected, result)
		})
	}
}

// TestParsedObservation_FileMtimesJSON tests FileMtimes JSON serialization.
func (s *ObservationSuite) TestParsedObservation_FileMtimesJSON() {
	obs := &ParsedObservation{
		Type:       ObsTypeDiscovery,
		Title:      "Test",
		FileMtimes: map[string]int64{"file1.go": 1234567890, "file2.go": 1234567891},
	}

	// Verify mtimes can be marshaled
	data, err := json.Marshal(obs.FileMtimes)
	s.NoError(err)
	s.Contains(string(data), "file1.go")
	s.Contains(string(data), "1234567890")
}

// TestObservation_CheckStaleness_TableDriven tests staleness checking.
func (s *ObservationSuite) TestObservation_CheckStaleness_TableDriven() {
	tests := []struct {
		storedMtimes  map[string]int64
		currentMtimes map[string]int64
		name          string
		expectedStale bool
	}{
		{
			name:          "empty stored mtimes - not stale",
			storedMtimes:  map[string]int64{},
			currentMtimes: map[string]int64{"file.go": 1000},
			expectedStale: false,
		},
		{
			name:          "matching mtimes - not stale",
			storedMtimes:  map[string]int64{"file.go": 1000},
			currentMtimes: map[string]int64{"file.go": 1000},
			expectedStale: false,
		},
		{
			name:          "file modified - stale",
			storedMtimes:  map[string]int64{"file.go": 1000},
			currentMtimes: map[string]int64{"file.go": 2000},
			expectedStale: true,
		},
		{
			name:          "file missing from current - not stale (files might not be checked)",
			storedMtimes:  map[string]int64{"file.go": 1000},
			currentMtimes: map[string]int64{},
			expectedStale: false, // Missing files don't mark as stale per the implementation
		},
		{
			name:          "multiple files, one modified - stale",
			storedMtimes:  map[string]int64{"file1.go": 1000, "file2.go": 2000},
			currentMtimes: map[string]int64{"file1.go": 1000, "file2.go": 3000},
			expectedStale: true,
		},
		{
			name:          "nil current mtimes - not stale",
			storedMtimes:  map[string]int64{"file.go": 1000},
			currentMtimes: nil,
			expectedStale: false,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			obs := &Observation{
				FileMtimes: tt.storedMtimes,
			}
			result := obs.CheckStaleness(tt.currentMtimes)
			s.Equal(tt.expectedStale, result)
		})
	}
}

// TestObservation_MarshalJSON tests JSON marshaling of Observation.
func (s *ObservationSuite) TestObservation_MarshalJSON() {
	obs := &Observation{
		ID:      1,
		Project: "test-project",
		Type:    ObsTypeDiscovery,
		Title:   sql.NullString{String: "Test Title", Valid: true},
		Scope:   ScopeProject,
	}

	data, err := json.Marshal(obs)
	s.NoError(err)
	s.Contains(string(data), `"id":1`)
	s.Contains(string(data), `"project":"test-project"`)
	s.Contains(string(data), `"type":"discovery"`)
}

// TestParsedObservation_Fields tests ParsedObservation field access.
func (s *ObservationSuite) TestParsedObservation_Fields() {
	obs := &ParsedObservation{
		Type:          ObsTypeFeature,
		Title:         "Add authentication",
		Subtitle:      "JWT-based auth",
		Narrative:     "Implemented JWT authentication for API endpoints",
		Facts:         []string{"Uses RS256 algorithm", "Tokens expire in 24h"},
		Concepts:      []string{"security", "auth"},
		FilesRead:     []string{"config.go"},
		FilesModified: []string{"handler.go", "middleware.go"},
		FileMtimes:    map[string]int64{"handler.go": 1234567890},
	}

	s.Equal(ObsTypeFeature, obs.Type)
	s.Equal("Add authentication", obs.Title)
	s.Equal("JWT-based auth", obs.Subtitle)
	s.Contains(obs.Narrative, "JWT")
	s.Len(obs.Facts, 2)
	s.Len(obs.Concepts, 2)
	s.Len(obs.FilesRead, 1)
	s.Len(obs.FilesModified, 2)
	s.Len(obs.FileMtimes, 1)
}

// TestObservation_NullFields tests handling of nullable fields.
func (s *ObservationSuite) TestObservation_NullFields() {
	// Test with null fields
	obs := &Observation{
		ID:        1,
		Project:   "test",
		Type:      ObsTypeDiscovery,
		Title:     sql.NullString{Valid: false},
		Subtitle:  sql.NullString{Valid: false},
		Narrative: sql.NullString{Valid: false},
	}

	s.False(obs.Title.Valid)
	s.False(obs.Subtitle.Valid)
	s.False(obs.Narrative.Valid)

	// Test with valid fields
	obs2 := &Observation{
		ID:        2,
		Project:   "test",
		Type:      ObsTypeBugfix,
		Title:     sql.NullString{String: "Fix bug", Valid: true},
		Subtitle:  sql.NullString{String: "Memory leak", Valid: true},
		Narrative: sql.NullString{String: "Fixed memory leak in handler", Valid: true},
	}

	s.True(obs2.Title.Valid)
	s.Equal("Fix bug", obs2.Title.String)
	s.True(obs2.Subtitle.Valid)
	s.Equal("Memory leak", obs2.Subtitle.String)
}

// TestNewObservation tests observation creation from parsed data.
func TestNewObservation(t *testing.T) {
	parsed := &ParsedObservation{
		Type:          ObsTypeFeature,
		Title:         "Add authentication",
		Subtitle:      "JWT-based",
		Narrative:     "Implemented JWT auth",
		Facts:         []string{"Uses RS256"},
		Concepts:      []string{"security"},
		FilesRead:     []string{"config.go"},
		FilesModified: []string{"handler.go"},
		FileMtimes:    map[string]int64{"handler.go": 1234567890},
	}

	obs := NewObservation("sdk-123", "test-project", parsed, 5, 1000)

	assert.Equal(t, "sdk-123", obs.SDKSessionID)
	assert.Equal(t, "test-project", obs.Project)
	assert.Equal(t, ScopeGlobal, obs.Scope) // security triggers global
	assert.Equal(t, ObsTypeFeature, obs.Type)
	assert.Equal(t, "Add authentication", obs.Title.String)
	assert.True(t, obs.Title.Valid)
	assert.Equal(t, int64(5), obs.PromptNumber.Int64)
	assert.Equal(t, int64(1000), obs.DiscoveryTokens)
	assert.NotEmpty(t, obs.CreatedAt)
	assert.Greater(t, obs.CreatedAtEpoch, int64(0))
}

// TestParsedObservation_ToStoredObservation tests conversion.
func TestParsedObservation_ToStoredObservation(t *testing.T) {
	parsed := &ParsedObservation{
		Type:      ObsTypeDiscovery,
		Title:     "Test Title",
		Subtitle:  "Test Subtitle",
		Narrative: "Test narrative",
		Facts:     []string{"Fact 1"},
		Concepts:  []string{"testing"},
	}

	obs := parsed.ToStoredObservation()

	assert.Equal(t, ObsTypeDiscovery, obs.Type)
	assert.Equal(t, "Test Title", obs.Title.String)
	assert.True(t, obs.Title.Valid)
	assert.Equal(t, "Test Subtitle", obs.Subtitle.String)
	assert.True(t, obs.Subtitle.Valid)
}

// TestJSONStringArray tests JSONStringArray scanning.
func TestJSONStringArray(t *testing.T) {
	tests := []struct {
		input    interface{}
		name     string
		expected JSONStringArray
		wantErr  bool
	}{
		{
			name:     "nil input",
			input:    nil,
			wantErr:  false,
			expected: nil,
		},
		{
			name:     "empty string",
			input:    "",
			wantErr:  false,
			expected: nil,
		},
		{
			name:     "json array string",
			input:    `["item1", "item2"]`,
			wantErr:  false,
			expected: JSONStringArray{"item1", "item2"},
		},
		{
			name:     "json array bytes",
			input:    []byte(`["a", "b", "c"]`),
			wantErr:  false,
			expected: JSONStringArray{"a", "b", "c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var arr JSONStringArray
			err := arr.Scan(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, arr)
			}
		})
	}
}

// TestJSONInt64Map tests JSONInt64Map scanning.
func TestJSONInt64Map(t *testing.T) {
	tests := []struct {
		input    interface{}
		expected JSONInt64Map
		name     string
		wantErr  bool
	}{
		{
			name:     "nil input",
			input:    nil,
			wantErr:  false,
			expected: nil,
		},
		{
			name:     "empty string",
			input:    "",
			wantErr:  false,
			expected: nil,
		},
		{
			name:     "json map string",
			input:    `{"file.go": 1234567890}`,
			wantErr:  false,
			expected: JSONInt64Map{"file.go": 1234567890},
		},
		{
			name:     "json map bytes",
			input:    []byte(`{"a.go": 100, "b.go": 200}`),
			wantErr:  false,
			expected: JSONInt64Map{"a.go": 100, "b.go": 200},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var m JSONInt64Map
			err := m.Scan(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, m)
			}
		})
	}
}

// TestObservation_JSONRoundTrip tests that observations can be marshaled and unmarshaled.
func TestObservation_JSONRoundTrip(t *testing.T) {
	original := &Observation{
		ID:             1,
		SDKSessionID:   "session-123",
		Project:        "test-project",
		Type:           ObsTypeDiscovery,
		Title:          sql.NullString{String: "Test Title", Valid: true},
		Subtitle:       sql.NullString{String: "Test Subtitle", Valid: true},
		Narrative:      sql.NullString{String: "Test narrative content", Valid: true},
		Scope:          ScopeProject,
		CreatedAt:      "2024-01-01T00:00:00Z",
		CreatedAtEpoch: 1704067200000,
	}

	// Marshal
	data, err := json.Marshal(original)
	require.NoError(t, err)

	// Unmarshal into map to check fields
	var result map[string]interface{}
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	assert.Equal(t, float64(1), result["id"])
	assert.Equal(t, "test-project", result["project"])
	assert.Equal(t, "discovery", result["type"])
	assert.Equal(t, "Test Title", result["title"])
}
