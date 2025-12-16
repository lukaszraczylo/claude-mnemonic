// Package sdk provides SDK agent integration for claude-mnemonic.
package sdk

import (
	"regexp"
	"strings"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
	"github.com/rs/zerolog/log"
)

var (
	// Observation parsing
	observationRegex = regexp.MustCompile(`(?s)<observation>(.*?)</observation>`)

	// Summary parsing
	summaryRegex     = regexp.MustCompile(`(?s)<summary>(.*?)</summary>`)
	skipSummaryRegex = regexp.MustCompile(`<skip_summary\s+reason="([^"]+)"\s*/>`)

	// Valid observation types
	validObsTypes = map[string]bool{
		"bugfix":    true,
		"feature":   true,
		"refactor":  true,
		"change":    true,
		"discovery": true,
		"decision":  true,
	}

	// Valid concepts - expanded list matching GlobalizableConcepts and common use cases
	validConcepts = map[string]bool{
		// Semantic concepts
		"how-it-works":     true,
		"why-it-exists":    true,
		"what-changed":     true,
		"problem-solution": true,
		"gotcha":           true,
		"pattern":          true,
		"trade-off":        true,
		// Globalizable concepts (from models.GlobalizableConcepts)
		"best-practice": true,
		"anti-pattern":  true,
		"architecture":  true,
		"security":      true,
		"performance":   true,
		"testing":       true,
		"debugging":     true,
		"workflow":      true,
		"tooling":       true,
		// Additional useful concepts
		"refactoring":    true,
		"api":            true,
		"database":       true,
		"configuration":  true,
		"error-handling": true,
		"caching":        true,
		"logging":        true,
		"auth":           true,
		"validation":     true,
	}
)

// ParseObservations parses observation XML blocks from SDK response text.
func ParseObservations(text string, correlationID string) []*models.ParsedObservation {
	var observations []*models.ParsedObservation

	matches := observationRegex.FindAllStringSubmatch(text, -1)
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}

		obsContent := match[1]

		// Extract fields
		obsType := extractField(obsContent, "type")
		title := extractField(obsContent, "title")
		subtitle := extractField(obsContent, "subtitle")
		narrative := extractField(obsContent, "narrative")
		facts := extractArrayElements(obsContent, "facts", "fact")
		concepts := extractArrayElements(obsContent, "concepts", "concept")
		filesRead := extractArrayElements(obsContent, "files_read", "file")
		filesModified := extractArrayElements(obsContent, "files_modified", "file")

		// Determine final type (default to "change" if invalid)
		finalType := models.ObsTypeChange
		if obsType != "" {
			if validObsTypes[obsType] {
				finalType = models.ObservationType(obsType)
			} else {
				log.Warn().
					Str("correlationId", correlationID).
					Str("invalidType", obsType).
					Msg("Invalid observation type, using 'change'")
			}
		} else {
			log.Warn().
				Str("correlationId", correlationID).
				Msg("Observation missing type field, using 'change'")
		}

		// Filter concepts: only keep valid ones from the strict list
		cleanedConcepts := make([]string, 0, len(concepts))
		var invalidConcepts []string
		for _, c := range concepts {
			c = strings.ToLower(strings.TrimSpace(c))
			if c == string(finalType) {
				continue // Skip type in concepts
			}
			if validConcepts[c] {
				cleanedConcepts = append(cleanedConcepts, c)
			} else {
				invalidConcepts = append(invalidConcepts, c)
			}
		}
		if len(invalidConcepts) > 0 {
			log.Warn().
				Str("correlationId", correlationID).
				Strs("invalidConcepts", invalidConcepts).
				Msg("Filtered out invalid concepts (not in allowed list)")
		}

		observations = append(observations, &models.ParsedObservation{
			Type:          finalType,
			Title:         title,
			Subtitle:      subtitle,
			Facts:         facts,
			Narrative:     narrative,
			Concepts:      cleanedConcepts,
			FilesRead:     filesRead,
			FilesModified: filesModified,
		})
	}

	return observations
}

// ParseSummary parses a summary XML block from SDK response text.
func ParseSummary(text string, sessionID int64) *models.ParsedSummary {
	// Check for skip_summary first
	if skipMatch := skipSummaryRegex.FindStringSubmatch(text); skipMatch != nil {
		log.Info().
			Int64("sessionId", sessionID).
			Str("reason", skipMatch[1]).
			Msg("Summary skipped")
		return nil
	}

	// Find summary block
	match := summaryRegex.FindStringSubmatch(text)
	if len(match) < 2 {
		return nil
	}

	summaryContent := match[1]

	return &models.ParsedSummary{
		Request:      extractField(summaryContent, "request"),
		Investigated: extractField(summaryContent, "investigated"),
		Learned:      extractField(summaryContent, "learned"),
		Completed:    extractField(summaryContent, "completed"),
		NextSteps:    extractField(summaryContent, "next_steps"),
		Notes:        extractField(summaryContent, "notes"),
	}
}

// extractField extracts a simple field value from XML content.
func extractField(content, fieldName string) string {
	pattern := regexp.MustCompile(`<` + fieldName + `>([^<]*)</` + fieldName + `>`)
	match := pattern.FindStringSubmatch(content)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

// extractArrayElements extracts array elements from XML content.
func extractArrayElements(content, arrayName, elementName string) []string {
	var elements []string

	// Find the array block
	arrayPattern := regexp.MustCompile(`(?s)<` + arrayName + `>(.*?)</` + arrayName + `>`)
	arrayMatch := arrayPattern.FindStringSubmatch(content)
	if len(arrayMatch) < 2 {
		return elements
	}

	arrayContent := arrayMatch[1]

	// Extract individual elements
	elementPattern := regexp.MustCompile(`<` + elementName + `>([^<]+)</` + elementName + `>`)
	elementMatches := elementPattern.FindAllStringSubmatch(arrayContent, -1)
	for _, match := range elementMatches {
		if len(match) >= 2 {
			elements = append(elements, strings.TrimSpace(match[1]))
		}
	}

	return elements
}
