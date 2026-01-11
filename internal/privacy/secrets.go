// Package privacy provides utilities for protecting sensitive data.
package privacy

import (
	"regexp"
	"slices"
	"strings"
)

// secretPatterns contains compiled regular expressions for detecting secrets.
// These patterns are designed to catch common secret formats with minimal false positives.
var secretPatterns = []*regexp.Regexp{
	// API keys with common prefixes
	regexp.MustCompile(`(?i)(api[_-]?key|apikey)\s*[:=]\s*['"]?[a-zA-Z0-9_-]{20,}['"]?`),

	// Passwords in configuration
	regexp.MustCompile(`(?i)(password|passwd|pwd)\s*[:=]\s*['"][^'"]{8,}['"]`),

	// Secret tokens
	regexp.MustCompile(`(?i)(secret[_-]?key|secret[_-]?token|auth[_-]?token)\s*[:=]\s*['"]?[a-zA-Z0-9_-]{20,}['"]?`),

	// OpenAI API keys
	regexp.MustCompile(`sk-[a-zA-Z0-9]{20,}`),

	// Anthropic API keys
	regexp.MustCompile(`sk-ant-[a-zA-Z0-9-]{20,}`),

	// GitHub tokens
	regexp.MustCompile(`gh[pous]_[a-zA-Z0-9]{36,}`),
	regexp.MustCompile(`github_pat_[a-zA-Z0-9_]{22,}`),

	// AWS keys
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	regexp.MustCompile(`(?i)aws[_-]?secret[_-]?access[_-]?key\s*[:=]\s*['"]?[a-zA-Z0-9/+=]{40}['"]?`),

	// Private keys (PEM format indicators)
	regexp.MustCompile(`-----BEGIN (RSA |EC |DSA |OPENSSH )?PRIVATE KEY-----`),

	// JWT tokens (base64.base64.base64 format)
	regexp.MustCompile(`eyJ[a-zA-Z0-9_-]+\.eyJ[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+`),

	// Generic secret assignment patterns
	regexp.MustCompile(`(?i)bearer\s+[a-zA-Z0-9_-]{20,}`),
}

// ContainsSecrets checks if the given text contains any patterns that look like secrets.
// Returns true if potential secrets are detected.
func ContainsSecrets(text string) bool {
	if text == "" {
		return false
	}

	for _, pattern := range secretPatterns {
		if pattern.MatchString(text) {
			return true
		}
	}
	return false
}

// RedactSecrets replaces detected secrets with a redaction marker.
// This allows the text to be stored while protecting sensitive data.
func RedactSecrets(text string) string {
	if text == "" {
		return text
	}

	result := text
	for _, pattern := range secretPatterns {
		result = pattern.ReplaceAllStringFunc(result, func(match string) string {
			// Preserve the key name, redact only the value
			if idx := strings.Index(match, "="); idx != -1 {
				return match[:idx+1] + "[REDACTED]"
			}
			if idx := strings.Index(match, ":"); idx != -1 {
				return match[:idx+1] + "[REDACTED]"
			}
			// For standalone secrets, show just the prefix
			if len(match) > 8 {
				return match[:4] + "...[REDACTED]"
			}
			return "[REDACTED]"
		})
	}
	return result
}

// SanitizeObservation checks multiple fields of an observation for secrets.
// Returns true if any secrets were found.
// This function is used as a validation gate before storing observations.
func SanitizeObservation(narrative string, facts []string) bool {
	if ContainsSecrets(narrative) {
		return true
	}
	return slices.ContainsFunc(facts, func(fact string) bool {
		return ContainsSecrets(fact)
	})
}
