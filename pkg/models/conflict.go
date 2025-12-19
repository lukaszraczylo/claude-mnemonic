// Package models contains domain models for claude-mnemonic.
package models

import (
	"regexp"
	"strings"
	"time"
)

// ConflictType represents the type of conflict between observations.
type ConflictType string

const (
	// ConflictSuperseded means newer observation supersedes older one (same topic, updated info).
	ConflictSuperseded ConflictType = "superseded"
	// ConflictContradicts means observations contain contradictory information.
	ConflictContradicts ConflictType = "contradicts"
	// ConflictOutdatedPattern means an outdated pattern/practice was identified.
	ConflictOutdatedPattern ConflictType = "outdated_pattern"
)

// ConflictResolution indicates which observation to prefer.
type ConflictResolution string

const (
	// ResolutionPreferNewer means prefer the newer observation.
	ResolutionPreferNewer ConflictResolution = "prefer_newer"
	// ResolutionPreferOlder means prefer the older observation (rare).
	ResolutionPreferOlder ConflictResolution = "prefer_older"
	// ResolutionManual means manual review is needed.
	ResolutionManual ConflictResolution = "manual"
)

// ObservationConflict tracks conflicting observations.
type ObservationConflict struct {
	ID              int64              `db:"id" json:"id"`
	NewerObsID      int64              `db:"newer_obs_id" json:"newer_obs_id"`
	OlderObsID      int64              `db:"older_obs_id" json:"older_obs_id"`
	ConflictType    ConflictType       `db:"conflict_type" json:"conflict_type"`
	Resolution      ConflictResolution `db:"resolution" json:"resolution"`
	Reason          string             `db:"reason" json:"reason"`
	DetectedAt      string             `db:"detected_at" json:"detected_at"`
	DetectedAtEpoch int64              `db:"detected_at_epoch" json:"detected_at_epoch"`
	Resolved        bool               `db:"resolved" json:"resolved"`
	ResolvedAt      *string            `db:"resolved_at" json:"resolved_at,omitempty"`
}

// ConflictDetectionResult contains the result of conflict detection.
type ConflictDetectionResult struct {
	HasConflict bool
	Type        ConflictType
	Resolution  ConflictResolution
	Reason      string
	OlderObsIDs []int64 // IDs of observations that conflict with the new one
}

// NewObservationConflict creates a new conflict record.
func NewObservationConflict(newerID, olderID int64, conflictType ConflictType, resolution ConflictResolution, reason string) *ObservationConflict {
	now := time.Now()
	return &ObservationConflict{
		NewerObsID:      newerID,
		OlderObsID:      olderID,
		ConflictType:    conflictType,
		Resolution:      resolution,
		Reason:          reason,
		DetectedAt:      now.Format(time.RFC3339),
		DetectedAtEpoch: now.UnixMilli(),
		Resolved:        false,
	}
}

// CorrectionPatterns contains regex patterns that indicate explicit corrections.
var CorrectionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bactually[,\s]+that\s+was\s+wrong\b`),
	regexp.MustCompile(`(?i)\bactually[,\s]+that's\s+(wrong|incorrect|not\s+right)\b`),
	regexp.MustCompile(`(?i)\bpreviously\s+(said|mentioned|noted)\s+.*\s+but\b`),
	regexp.MustCompile(`(?i)\bcorrection:\s*`),
	regexp.MustCompile(`(?i)\bignore\s+(the\s+)?(previous|earlier)\b`),
	regexp.MustCompile(`(?i)\bdisregard\s+(the\s+)?(previous|earlier)\b`),
	regexp.MustCompile(`(?i)\bwas\s+(wrong|incorrect|mistaken)\b`),
	regexp.MustCompile(`(?i)\bturns\s+out\s+.*(wrong|incorrect|not\s+the\s+case)\b`),
	regexp.MustCompile(`(?i)\b(supersedes|replaces|overrides)\s+(the\s+)?(previous|earlier|old)\b`),
	regexp.MustCompile(`(?i)\b(don't|do\s+not)\s+use\s+.*\s+anymore\b`),
	regexp.MustCompile(`(?i)\bno\s+longer\s+(valid|applicable|correct|recommended)\b`),
	regexp.MustCompile(`(?i)\bdeprecated\s+(approach|method|pattern|way)\b`),
	regexp.MustCompile(`(?i)\bshould\s+have\s+(been|used)\b.*instead\b`),
	regexp.MustCompile(`(?i)\bbetter\s+(approach|way|method|solution)\s+is\b`),
}

// OpposingChangePatterns detects add/remove conflicts.
var OpposingChangePatterns = map[string]string{
	"add":     "remove",
	"added":   "removed",
	"create":  "delete",
	"created": "deleted",
	"enable":  "disable",
	"enabled": "disabled",
	"include": "exclude",
	"allow":   "deny",
	"permit":  "block",
}

// DetectExplicitCorrection checks if text contains explicit correction language.
func DetectExplicitCorrection(text string) (bool, string) {
	for _, pattern := range CorrectionPatterns {
		if match := pattern.FindString(text); match != "" {
			return true, "Explicit correction detected: " + match
		}
	}
	return false, ""
}

// DetectOpposingFileChanges checks if two observations have opposing changes on the same file.
func DetectOpposingFileChanges(newer, older *Observation) (bool, string) {
	// Check for overlapping modified files
	newerFiles := make(map[string]bool)
	for _, f := range newer.FilesModified {
		newerFiles[f] = true
	}

	var overlappingFiles []string
	for _, f := range older.FilesModified {
		if newerFiles[f] {
			overlappingFiles = append(overlappingFiles, f)
		}
	}

	if len(overlappingFiles) == 0 {
		return false, ""
	}

	// Check for opposing action words in titles/narratives
	newerText := strings.ToLower(newer.Title.String + " " + newer.Narrative.String)
	olderText := strings.ToLower(older.Title.String + " " + older.Narrative.String)

	for action, opposite := range OpposingChangePatterns {
		if (strings.Contains(newerText, action) && strings.Contains(olderText, opposite)) ||
			(strings.Contains(newerText, opposite) && strings.Contains(olderText, action)) {
			return true, "Opposing changes on files: " + strings.Join(overlappingFiles, ", ")
		}
	}

	return false, ""
}

// DetectConceptTagMismatch checks if observations have same concepts but different recommendations.
func DetectConceptTagMismatch(newer, older *Observation) (bool, string) {
	// Find overlapping concepts
	newerConcepts := make(map[string]bool)
	for _, c := range newer.Concepts {
		newerConcepts[c] = true
	}

	var overlapping []string
	for _, c := range older.Concepts {
		if newerConcepts[c] {
			overlapping = append(overlapping, c)
		}
	}

	if len(overlapping) == 0 {
		return false, ""
	}

	// Check if same file was modified and concepts overlap
	// This suggests the newer observation may update the approach
	newerFiles := make(map[string]bool)
	for _, f := range newer.FilesModified {
		newerFiles[f] = true
	}
	for _, f := range older.FilesModified {
		if newerFiles[f] {
			// Same file modified with same concepts - likely an update
			return true, "Same concepts (" + strings.Join(overlapping, ", ") + ") with overlapping file changes"
		}
	}

	return false, ""
}

// DetectConflict performs comprehensive conflict detection between a new observation
// and an existing one. Returns detection result.
func DetectConflict(newer, older *Observation) *ConflictDetectionResult {
	result := &ConflictDetectionResult{
		HasConflict: false,
	}

	// 1. Check for explicit correction language in newer observation
	if newer.Narrative.Valid {
		if isCorrection, reason := DetectExplicitCorrection(newer.Narrative.String); isCorrection {
			result.HasConflict = true
			result.Type = ConflictContradicts
			result.Resolution = ResolutionPreferNewer
			result.Reason = reason
			result.OlderObsIDs = append(result.OlderObsIDs, older.ID)
			return result
		}
	}

	// Check title as well
	if newer.Title.Valid {
		if isCorrection, reason := DetectExplicitCorrection(newer.Title.String); isCorrection {
			result.HasConflict = true
			result.Type = ConflictContradicts
			result.Resolution = ResolutionPreferNewer
			result.Reason = reason
			result.OlderObsIDs = append(result.OlderObsIDs, older.ID)
			return result
		}
	}

	// 2. Check for opposing file changes
	if isOpposing, reason := DetectOpposingFileChanges(newer, older); isOpposing {
		result.HasConflict = true
		result.Type = ConflictSuperseded
		result.Resolution = ResolutionPreferNewer
		result.Reason = reason
		result.OlderObsIDs = append(result.OlderObsIDs, older.ID)
		return result
	}

	// 3. Check for concept tag mismatches with same files
	if isMismatch, reason := DetectConceptTagMismatch(newer, older); isMismatch {
		result.HasConflict = true
		result.Type = ConflictSuperseded
		result.Resolution = ResolutionPreferNewer
		result.Reason = reason
		result.OlderObsIDs = append(result.OlderObsIDs, older.ID)
		return result
	}

	return result
}

// DetectConflictsWithExisting checks a new observation against a list of existing observations.
// Returns all detected conflicts.
func DetectConflictsWithExisting(newer *Observation, existing []*Observation) []*ConflictDetectionResult {
	var results []*ConflictDetectionResult

	for _, older := range existing {
		// Skip self-comparison
		if older.ID == newer.ID {
			continue
		}

		// Only compare within same project (or both global)
		if newer.Project != older.Project && newer.Scope != ScopeGlobal && older.Scope != ScopeGlobal {
			continue
		}

		result := DetectConflict(newer, older)
		if result.HasConflict {
			results = append(results, result)
		}
	}

	return results
}
