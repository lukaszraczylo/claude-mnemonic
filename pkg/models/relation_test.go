// Package models contains domain models for claude-mnemonic.
package models

import (
	"database/sql"
	"testing"
)

func TestDetectFileOverlapRelation(t *testing.T) {
	tests := []struct {
		name          string
		newer         *Observation
		older         *Observation
		wantRelation  bool
		wantRelType   RelationType
		wantMinConfid float64
	}{
		{
			name: "no file overlap",
			newer: &Observation{
				ID:            1,
				FilesModified: []string{"file1.go", "file2.go"},
			},
			older: &Observation{
				ID:            2,
				FilesModified: []string{"file3.go", "file4.go"},
			},
			wantRelation: false,
		},
		{
			name: "shared modified files",
			newer: &Observation{
				ID:            1,
				Type:          ObsTypeRefactor,
				FilesModified: []string{"shared.go", "file2.go"},
			},
			older: &Observation{
				ID:            2,
				Type:          ObsTypeRefactor,
				FilesModified: []string{"shared.go", "file4.go"},
			},
			wantRelation:  true,
			wantRelType:   RelationSupersedes,
			wantMinConfid: 0.5,
		},
		{
			name: "bugfix on feature file",
			newer: &Observation{
				ID:            1,
				Type:          ObsTypeBugfix,
				FilesModified: []string{"feature.go"},
			},
			older: &Observation{
				ID:            2,
				Type:          ObsTypeFeature,
				FilesModified: []string{"feature.go"},
			},
			wantRelation:  true,
			wantRelType:   RelationFixes,
			wantMinConfid: 0.6,
		},
		{
			name: "newer reads older modified",
			newer: &Observation{
				ID:            1,
				Type:          ObsTypeChange,
				FilesRead:     []string{"dep.go"},
				FilesModified: []string{"caller.go"},
			},
			older: &Observation{
				ID:            2,
				Type:          ObsTypeDecision,
				FilesModified: []string{"dep.go"},
			},
			wantRelation:  true,
			wantMinConfid: 0.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetectFileOverlapRelation(tt.newer, tt.older)

			if tt.wantRelation {
				if result == nil {
					t.Fatal("expected relation, got nil")
				}
				if tt.wantRelType != "" && result.RelationType != tt.wantRelType {
					t.Errorf("relation type = %v, want %v", result.RelationType, tt.wantRelType)
				}
				if result.Confidence < tt.wantMinConfid {
					t.Errorf("confidence = %v, want at least %v", result.Confidence, tt.wantMinConfid)
				}
				if result.DetectionSource != DetectionSourceFileOverlap {
					t.Errorf("source = %v, want %v", result.DetectionSource, DetectionSourceFileOverlap)
				}
			} else {
				if result != nil {
					t.Errorf("expected no relation, got %+v", result)
				}
			}
		})
	}
}

func TestDetectConceptOverlapRelation(t *testing.T) {
	tests := []struct {
		name          string
		newer         *Observation
		older         *Observation
		wantRelation  bool
		wantMinConfid float64
	}{
		{
			name: "no concept overlap",
			newer: &Observation{
				ID:       1,
				Concepts: []string{"auth", "api"},
			},
			older: &Observation{
				ID:       2,
				Concepts: []string{"database", "caching"},
			},
			wantRelation: false,
		},
		{
			name: "shared concepts",
			newer: &Observation{
				ID:       1,
				Concepts: []string{"security", "auth"},
			},
			older: &Observation{
				ID:       2,
				Concepts: []string{"security", "validation"},
			},
			wantRelation:  true,
			wantMinConfid: 0.4, // security is a high-value concept
		},
		{
			name: "multiple shared concepts",
			newer: &Observation{
				ID:       1,
				Concepts: []string{"auth", "api", "validation"},
			},
			older: &Observation{
				ID:       2,
				Concepts: []string{"auth", "api", "database"},
			},
			wantRelation:  true,
			wantMinConfid: 0.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetectConceptOverlapRelation(tt.newer, tt.older)

			if tt.wantRelation {
				if result == nil {
					t.Fatal("expected relation, got nil")
				}
				if result.Confidence < tt.wantMinConfid {
					t.Errorf("confidence = %v, want at least %v", result.Confidence, tt.wantMinConfid)
				}
				if result.DetectionSource != DetectionSourceConceptOverlap {
					t.Errorf("source = %v, want %v", result.DetectionSource, DetectionSourceConceptOverlap)
				}
			} else {
				if result != nil {
					t.Errorf("expected no relation, got %+v", result)
				}
			}
		})
	}
}

func TestDetectTypeProgressionRelation(t *testing.T) {
	tests := []struct {
		name         string
		newerType    ObservationType
		olderType    ObservationType
		wantRelation bool
		wantRelType  RelationType
	}{
		{
			name:         "bugfix fixes discovery",
			newerType:    ObsTypeBugfix,
			olderType:    ObsTypeDiscovery,
			wantRelation: true,
			wantRelType:  RelationFixes,
		},
		{
			name:         "bugfix fixes feature",
			newerType:    ObsTypeBugfix,
			olderType:    ObsTypeFeature,
			wantRelation: true,
			wantRelType:  RelationFixes,
		},
		{
			name:         "feature depends on decision",
			newerType:    ObsTypeFeature,
			olderType:    ObsTypeDecision,
			wantRelation: true,
			wantRelType:  RelationDependsOn,
		},
		{
			name:         "refactor evolves from discovery",
			newerType:    ObsTypeRefactor,
			olderType:    ObsTypeDiscovery,
			wantRelation: true,
			wantRelType:  RelationEvolvesFrom,
		},
		{
			name:         "no progression discovery to bugfix",
			newerType:    ObsTypeDiscovery,
			olderType:    ObsTypeBugfix,
			wantRelation: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newer := &Observation{ID: 1, Type: tt.newerType}
			older := &Observation{ID: 2, Type: tt.olderType}
			result := DetectTypeProgressionRelation(newer, older)

			if tt.wantRelation {
				if result == nil {
					t.Fatal("expected relation, got nil")
				}
				if result.RelationType != tt.wantRelType {
					t.Errorf("relation type = %v, want %v", result.RelationType, tt.wantRelType)
				}
				if result.DetectionSource != DetectionSourceTypeProgression {
					t.Errorf("source = %v, want %v", result.DetectionSource, DetectionSourceTypeProgression)
				}
			} else {
				if result != nil {
					t.Errorf("expected no relation, got %+v", result)
				}
			}
		})
	}
}

func TestDetectTemporalProximityRelation(t *testing.T) {
	baseTime := int64(1700000000000) // some base epoch ms

	tests := []struct {
		name         string
		newerSession string
		olderSession string
		newerTime    int64
		olderTime    int64
		wantRelation bool
	}{
		{
			name:         "same session close time",
			newerSession: "session-1",
			olderSession: "session-1",
			newerTime:    baseTime + 60000, // 1 minute later
			olderTime:    baseTime,
			wantRelation: true,
		},
		{
			name:         "same session far apart",
			newerSession: "session-1",
			olderSession: "session-1",
			newerTime:    baseTime + 600000, // 10 minutes later
			olderTime:    baseTime,
			wantRelation: false, // > 5 minutes
		},
		{
			name:         "different sessions close time",
			newerSession: "session-1",
			olderSession: "session-2",
			newerTime:    baseTime + 30000,
			olderTime:    baseTime,
			wantRelation: false, // different sessions
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newer := &Observation{
				ID:             1,
				SDKSessionID:   tt.newerSession,
				CreatedAtEpoch: tt.newerTime,
			}
			older := &Observation{
				ID:             2,
				SDKSessionID:   tt.olderSession,
				CreatedAtEpoch: tt.olderTime,
			}
			result := DetectTemporalProximityRelation(newer, older)

			if tt.wantRelation {
				if result == nil {
					t.Fatal("expected relation, got nil")
				}
				if result.DetectionSource != DetectionSourceTemporalProximity {
					t.Errorf("source = %v, want %v", result.DetectionSource, DetectionSourceTemporalProximity)
				}
			} else {
				if result != nil {
					t.Errorf("expected no relation, got %+v", result)
				}
			}
		})
	}
}

func TestDetectNarrativeMentionRelation(t *testing.T) {
	tests := []struct {
		name         string
		narrative    string
		wantRelation bool
		wantRelType  RelationType
	}{
		{
			name:         "fixes language",
			narrative:    "This change fixes the issue with authentication",
			wantRelation: true,
			wantRelType:  RelationFixes,
		},
		{
			name:         "causes language",
			narrative:    "This decision caused unexpected side effects",
			wantRelation: true,
			wantRelType:  RelationCauses,
		},
		{
			name:         "supersedes language",
			narrative:    "This approach supersedes the previous workaround",
			wantRelation: true,
			wantRelType:  RelationSupersedes,
		},
		{
			name:         "depends on language",
			narrative:    "This feature depends on the authentication module",
			wantRelation: true,
			wantRelType:  RelationDependsOn,
		},
		{
			name:         "no relationship language",
			narrative:    "Added new feature for user management",
			wantRelation: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newer := &Observation{
				ID:        1,
				Narrative: sql.NullString{String: tt.narrative, Valid: true},
			}
			older := &Observation{ID: 2}
			result := DetectNarrativeMentionRelation(newer, older)

			if tt.wantRelation {
				if result == nil {
					t.Fatal("expected relation, got nil")
				}
				if result.RelationType != tt.wantRelType {
					t.Errorf("relation type = %v, want %v", result.RelationType, tt.wantRelType)
				}
				if result.DetectionSource != DetectionSourceNarrativeMention {
					t.Errorf("source = %v, want %v", result.DetectionSource, DetectionSourceNarrativeMention)
				}
			} else {
				if result != nil {
					t.Errorf("expected no relation, got %+v", result)
				}
			}
		})
	}
}

func TestDetectRelationsWithExisting(t *testing.T) {
	newer := &Observation{
		ID:            1,
		SDKSessionID:  "session-1",
		Project:       "test-project",
		Type:          ObsTypeBugfix,
		FilesModified: []string{"auth.go"},
		Concepts:      []string{"security", "auth"},
		Narrative:     sql.NullString{String: "Fixed security issue in auth module", Valid: true},
	}

	existing := []*Observation{
		{
			ID:            2,
			SDKSessionID:  "session-1",
			Project:       "test-project",
			Type:          ObsTypeDiscovery,
			FilesModified: []string{"auth.go"},
			Concepts:      []string{"security"},
		},
		{
			ID:            3,
			SDKSessionID:  "session-2",
			Project:       "test-project",
			Type:          ObsTypeFeature,
			FilesModified: []string{"other.go"},
			Concepts:      []string{"api"},
		},
		{
			ID:           4,
			SDKSessionID: "session-1",
			Project:      "other-project", // different project
			Type:         ObsTypeDiscovery,
		},
	}

	results := DetectRelationsWithExisting(newer, existing, 0.4)

	// Should find relation with observation 2 (file overlap + concept overlap + type progression)
	// Should not find relation with observation 3 (no overlap)
	// Should not find relation with observation 4 (different project)

	if len(results) == 0 {
		t.Fatal("expected at least one relation")
	}

	// Check that we found relation with observation 2
	foundObs2 := false
	for _, r := range results {
		if r.TargetID == 2 {
			foundObs2 = true
			// Should be high confidence due to multiple signals
			if r.Confidence < 0.5 {
				t.Errorf("expected higher confidence for obs 2, got %v", r.Confidence)
			}
		}
		// Should not find relation with obs 4
		if r.TargetID == 4 {
			t.Error("should not find relation with different project")
		}
	}

	if !foundObs2 {
		t.Error("expected to find relation with observation 2")
	}
}

func TestNewObservationRelation(t *testing.T) {
	rel := NewObservationRelation(1, 2, RelationFixes, 0.8, DetectionSourceFileOverlap, "test reason")

	if rel.SourceID != 1 {
		t.Errorf("SourceID = %v, want 1", rel.SourceID)
	}
	if rel.TargetID != 2 {
		t.Errorf("TargetID = %v, want 2", rel.TargetID)
	}
	if rel.RelationType != RelationFixes {
		t.Errorf("RelationType = %v, want %v", rel.RelationType, RelationFixes)
	}
	if rel.Confidence != 0.8 {
		t.Errorf("Confidence = %v, want 0.8", rel.Confidence)
	}
	if rel.DetectionSource != DetectionSourceFileOverlap {
		t.Errorf("DetectionSource = %v, want %v", rel.DetectionSource, DetectionSourceFileOverlap)
	}
	if rel.Reason != "test reason" {
		t.Errorf("Reason = %v, want 'test reason'", rel.Reason)
	}
	if rel.CreatedAt == "" {
		t.Error("CreatedAt should be set")
	}
	if rel.CreatedAtEpoch == 0 {
		t.Error("CreatedAtEpoch should be set")
	}
}
