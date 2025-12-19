// Package models contains domain models for claude-mnemonic.
package models

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

// ConflictSuite is a test suite for conflict detection operations.
type ConflictSuite struct {
	suite.Suite
}

func TestConflictSuite(t *testing.T) {
	suite.Run(t, new(ConflictSuite))
}

// TestConflictTypeConstants tests conflict type constants.
func (s *ConflictSuite) TestConflictTypeConstants() {
	s.Equal(ConflictType("superseded"), ConflictSuperseded)
	s.Equal(ConflictType("contradicts"), ConflictContradicts)
	s.Equal(ConflictType("outdated_pattern"), ConflictOutdatedPattern)
}

// TestResolutionConstants tests resolution constants.
func (s *ConflictSuite) TestResolutionConstants() {
	s.Equal(ConflictResolution("prefer_newer"), ResolutionPreferNewer)
	s.Equal(ConflictResolution("prefer_older"), ResolutionPreferOlder)
	s.Equal(ConflictResolution("manual"), ResolutionManual)
}

// TestNewObservationConflict tests conflict creation.
func (s *ConflictSuite) TestNewObservationConflict() {
	conflict := NewObservationConflict(2, 1, ConflictSuperseded, ResolutionPreferNewer, "Test reason")

	s.Equal(int64(2), conflict.NewerObsID)
	s.Equal(int64(1), conflict.OlderObsID)
	s.Equal(ConflictSuperseded, conflict.ConflictType)
	s.Equal(ResolutionPreferNewer, conflict.Resolution)
	s.Equal("Test reason", conflict.Reason)
	s.False(conflict.Resolved)
	s.NotEmpty(conflict.DetectedAt)
	s.Greater(conflict.DetectedAtEpoch, int64(0))
}

// TestDetectExplicitCorrection_TableDriven tests explicit correction detection.
func (s *ConflictSuite) TestDetectExplicitCorrection_TableDriven() {
	tests := []struct {
		name          string
		text          string
		expectMatch   bool
		expectPattern string
	}{
		{
			name:          "actually that was wrong",
			text:          "Actually, that was wrong - we should use a different approach",
			expectMatch:   true,
			expectPattern: "actually, that was wrong",
		},
		{
			name:          "correction prefix",
			text:          "Correction: the previous implementation had a bug",
			expectMatch:   true,
			expectPattern: "correction:",
		},
		{
			name:          "ignore previous",
			text:          "Please ignore the previous recommendation",
			expectMatch:   true,
			expectPattern: "ignore",
		},
		{
			name:          "disregard earlier",
			text:          "Disregard the earlier suggestion, it was flawed",
			expectMatch:   true,
			expectPattern: "disregard",
		},
		{
			name:          "was wrong",
			text:          "The original approach was wrong",
			expectMatch:   true,
			expectPattern: "was wrong",
		},
		{
			name:          "no longer valid",
			text:          "This method is no longer valid after the refactor",
			expectMatch:   true,
			expectPattern: "no longer valid",
		},
		{
			name:          "deprecated approach",
			text:          "This is a deprecated approach that should not be used",
			expectMatch:   true,
			expectPattern: "deprecated approach",
		},
		{
			name:          "better approach is",
			text:          "A better approach is to use the new API",
			expectMatch:   true,
			expectPattern: "better approach is",
		},
		{
			name:        "normal text - no correction",
			text:        "This is a normal observation about the code",
			expectMatch: false,
		},
		{
			name:        "empty text",
			text:        "",
			expectMatch: false,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			found, reason := DetectExplicitCorrection(tt.text)
			s.Equal(tt.expectMatch, found)
			if tt.expectMatch {
				s.Contains(reason, "Explicit correction detected")
			}
		})
	}
}

// TestDetectOpposingFileChanges_TableDriven tests opposing file change detection.
func (s *ConflictSuite) TestDetectOpposingFileChanges_TableDriven() {
	tests := []struct {
		name           string
		newerObs       *Observation
		olderObs       *Observation
		expectConflict bool
	}{
		{
			name: "add then remove - conflict",
			newerObs: &Observation{
				Title:         sql.NullString{String: "Remove authentication middleware", Valid: true},
				Narrative:     sql.NullString{String: "Removed the auth middleware from handlers", Valid: true},
				FilesModified: []string{"middleware.go", "handler.go"},
			},
			olderObs: &Observation{
				Title:         sql.NullString{String: "Add authentication middleware", Valid: true},
				Narrative:     sql.NullString{String: "Added auth middleware to secure endpoints", Valid: true},
				FilesModified: []string{"middleware.go", "handler.go"},
			},
			expectConflict: true,
		},
		{
			name: "enable then disable - conflict",
			newerObs: &Observation{
				Title:         sql.NullString{String: "Disable caching feature", Valid: true},
				Narrative:     sql.NullString{String: "Disabled caching due to issues", Valid: true},
				FilesModified: []string{"cache.go"},
			},
			olderObs: &Observation{
				Title:         sql.NullString{String: "Enable caching feature", Valid: true},
				Narrative:     sql.NullString{String: "Enabled caching for performance", Valid: true},
				FilesModified: []string{"cache.go"},
			},
			expectConflict: true,
		},
		{
			name: "different files - no conflict",
			newerObs: &Observation{
				Title:         sql.NullString{String: "Remove old code", Valid: true},
				Narrative:     sql.NullString{String: "Removed deprecated functions", Valid: true},
				FilesModified: []string{"old.go"},
			},
			olderObs: &Observation{
				Title:         sql.NullString{String: "Add new feature", Valid: true},
				Narrative:     sql.NullString{String: "Added new functions", Valid: true},
				FilesModified: []string{"new.go"},
			},
			expectConflict: false,
		},
		{
			name: "same files but no opposing keywords - no conflict",
			newerObs: &Observation{
				Title:         sql.NullString{String: "Update handler logic", Valid: true},
				Narrative:     sql.NullString{String: "Updated the handler implementation", Valid: true},
				FilesModified: []string{"handler.go"},
			},
			olderObs: &Observation{
				Title:         sql.NullString{String: "Fix handler bug", Valid: true},
				Narrative:     sql.NullString{String: "Fixed a bug in handler", Valid: true},
				FilesModified: []string{"handler.go"},
			},
			expectConflict: false,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			found, _ := DetectOpposingFileChanges(tt.newerObs, tt.olderObs)
			s.Equal(tt.expectConflict, found)
		})
	}
}

// TestDetectConceptTagMismatch_TableDriven tests concept tag mismatch detection.
func (s *ConflictSuite) TestDetectConceptTagMismatch_TableDriven() {
	tests := []struct {
		name           string
		newerObs       *Observation
		olderObs       *Observation
		expectConflict bool
	}{
		{
			name: "same concepts and files - conflict",
			newerObs: &Observation{
				Concepts:      []string{"security", "authentication"},
				FilesModified: []string{"auth.go"},
			},
			olderObs: &Observation{
				Concepts:      []string{"security", "authentication"},
				FilesModified: []string{"auth.go"},
			},
			expectConflict: true,
		},
		{
			name: "overlapping concepts different files - no conflict",
			newerObs: &Observation{
				Concepts:      []string{"security"},
				FilesModified: []string{"new_auth.go"},
			},
			olderObs: &Observation{
				Concepts:      []string{"security"},
				FilesModified: []string{"old_auth.go"},
			},
			expectConflict: false,
		},
		{
			name: "different concepts same files - no conflict",
			newerObs: &Observation{
				Concepts:      []string{"performance"},
				FilesModified: []string{"handler.go"},
			},
			olderObs: &Observation{
				Concepts:      []string{"testing"},
				FilesModified: []string{"handler.go"},
			},
			expectConflict: false,
		},
		{
			name: "no overlapping concepts or files - no conflict",
			newerObs: &Observation{
				Concepts:      []string{"security"},
				FilesModified: []string{"auth.go"},
			},
			olderObs: &Observation{
				Concepts:      []string{"testing"},
				FilesModified: []string{"test.go"},
			},
			expectConflict: false,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			found, _ := DetectConceptTagMismatch(tt.newerObs, tt.olderObs)
			s.Equal(tt.expectConflict, found)
		})
	}
}

// TestDetectConflict tests comprehensive conflict detection.
func (s *ConflictSuite) TestDetectConflict() {
	// Test explicit correction takes precedence
	newer := &Observation{
		ID:        2,
		Project:   "test",
		Narrative: sql.NullString{String: "Actually, that was wrong. We should use a different approach.", Valid: true},
	}
	older := &Observation{
		ID:      1,
		Project: "test",
	}

	result := DetectConflict(newer, older)
	s.True(result.HasConflict)
	s.Equal(ConflictContradicts, result.Type)
	s.Equal(ResolutionPreferNewer, result.Resolution)
	s.Contains(result.Reason, "Explicit correction")
}

// TestDetectConflictsWithExisting tests conflict detection against multiple observations.
func (s *ConflictSuite) TestDetectConflictsWithExisting() {
	newer := &Observation{
		ID:            3,
		Project:       "test",
		Narrative:     sql.NullString{String: "Actually, that was wrong", Valid: true},
		Concepts:      []string{"security"},
		FilesModified: []string{"auth.go"},
	}

	existing := []*Observation{
		{
			ID:            1,
			Project:       "test",
			Concepts:      []string{"security"},
			FilesModified: []string{"auth.go"},
		},
		{
			ID:            2,
			Project:       "test",
			Concepts:      []string{"testing"},
			FilesModified: []string{"test.go"},
		},
		{
			ID:      3, // Same as newer - should be skipped
			Project: "test",
		},
	}

	results := DetectConflictsWithExisting(newer, existing)

	// Should find conflicts with obs 1 (concepts + files overlap + correction language)
	// but not with obs 2 (different concepts/files) or obs 3 (same ID)
	s.GreaterOrEqual(len(results), 1)

	// At least one result should reference obs 1
	foundObs1 := false
	for _, r := range results {
		for _, id := range r.OlderObsIDs {
			if id == 1 {
				foundObs1 = true
				break
			}
		}
	}
	s.True(foundObs1)
}

// TestDetectConflictsWithExisting_DifferentProjects tests that different projects don't conflict.
func (s *ConflictSuite) TestDetectConflictsWithExisting_DifferentProjects() {
	newer := &Observation{
		ID:        2,
		Project:   "project-a",
		Scope:     ScopeProject,
		Narrative: sql.NullString{String: "Actually, that was wrong", Valid: true},
	}

	existing := []*Observation{
		{
			ID:      1,
			Project: "project-b",
			Scope:   ScopeProject,
		},
	}

	results := DetectConflictsWithExisting(newer, existing)
	s.Empty(results) // Different projects should not conflict
}

// TestDetectConflictsWithExisting_GlobalScope tests that global observations can conflict across projects.
func (s *ConflictSuite) TestDetectConflictsWithExisting_GlobalScope() {
	newer := &Observation{
		ID:            2,
		Project:       "project-a",
		Scope:         ScopeGlobal,
		Narrative:     sql.NullString{String: "Actually, that was wrong", Valid: true},
		Concepts:      []string{"security"},
		FilesModified: []string{"auth.go"},
	}

	existing := []*Observation{
		{
			ID:            1,
			Project:       "project-b",
			Scope:         ScopeGlobal, // Global scope allows cross-project conflict detection
			Concepts:      []string{"security"},
			FilesModified: []string{"auth.go"},
		},
	}

	results := DetectConflictsWithExisting(newer, existing)
	s.GreaterOrEqual(len(results), 1) // Global scope allows conflict detection
}

// TestCorrectionPatterns_Compiled ensures all patterns compile correctly.
func TestCorrectionPatterns_Compiled(t *testing.T) {
	// This test verifies that all correction patterns are valid regexps
	// If any pattern fails to compile, the package won't load
	assert.NotEmpty(t, CorrectionPatterns)
	for i, pattern := range CorrectionPatterns {
		assert.NotNil(t, pattern, "Pattern %d should not be nil", i)
	}
}

// TestOpposingChangePatterns tests the opposing change pattern map.
func TestOpposingChangePatterns(t *testing.T) {
	assert.NotEmpty(t, OpposingChangePatterns)
	assert.Equal(t, "remove", OpposingChangePatterns["add"])
	assert.Equal(t, "removed", OpposingChangePatterns["added"])
	assert.Equal(t, "delete", OpposingChangePatterns["create"])
	assert.Equal(t, "disable", OpposingChangePatterns["enable"])
}

// TestObservationConflict_Fields tests field access.
func TestObservationConflict_Fields(t *testing.T) {
	conflict := &ObservationConflict{
		ID:              1,
		NewerObsID:      10,
		OlderObsID:      5,
		ConflictType:    ConflictSuperseded,
		Resolution:      ResolutionPreferNewer,
		Reason:          "Test reason",
		DetectedAt:      "2024-01-01T00:00:00Z",
		DetectedAtEpoch: 1704067200000,
		Resolved:        false,
	}

	assert.Equal(t, int64(1), conflict.ID)
	assert.Equal(t, int64(10), conflict.NewerObsID)
	assert.Equal(t, int64(5), conflict.OlderObsID)
	assert.Equal(t, ConflictSuperseded, conflict.ConflictType)
	assert.Equal(t, ResolutionPreferNewer, conflict.Resolution)
	assert.False(t, conflict.Resolved)
}
