// Package privacy provides privacy tag handling for claude-mnemonic.
package privacy

import (
	"regexp"
	"strings"
)

var (
	// privateTagRegex matches <private>...</private> tags
	privateTagRegex = regexp.MustCompile(`(?s)<private>.*?</private>`)

	// memoryTagRegex matches <claude-mnemonic-context>...</claude-mnemonic-context> tags
	memoryTagRegex = regexp.MustCompile(`(?s)<claude-mnemonic-context>.*?</claude-mnemonic-context>`)
)

// StripPrivateTags removes all <private>...</private> content from text.
func StripPrivateTags(text string) string {
	return privateTagRegex.ReplaceAllString(text, "")
}

// StripMemoryTags removes all <claude-mnemonic-context>...</claude-mnemonic-context> content from text.
func StripMemoryTags(text string) string {
	return memoryTagRegex.ReplaceAllString(text, "")
}

// StripAllTags removes both private and memory context tags.
func StripAllTags(text string) string {
	text = StripPrivateTags(text)
	text = StripMemoryTags(text)
	return text
}

// IsEntirelyPrivate checks if the text is entirely within <private> tags.
func IsEntirelyPrivate(text string) bool {
	stripped := StripPrivateTags(text)
	return strings.TrimSpace(stripped) == ""
}

// Clean performs full privacy cleaning on text.
// This is the main function to use before storing any user content.
func Clean(text string) string {
	// Strip both types of tags
	text = StripAllTags(text)
	// Trim whitespace
	return strings.TrimSpace(text)
}
