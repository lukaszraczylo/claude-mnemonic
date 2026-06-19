// Package sanitize provides content cleaning utilities for stripping
// Claude Code internal XML artifacts from captured text.
package sanitize

import (
	"regexp"
	"strings"
	"sync"
)

// systemXMLTags lists Claude Code internal XML tags that should be stripped
// from captured content before processing. These are system-level artifacts
// that pollute observations and summaries when stored.
var systemXMLTags = []string{
	// Claude Code task/agent system
	"task-notification",
	// System reminders injected by Claude Code
	"system-reminder",
	// Claude-mnemonic's own context injection
	"relevant-memory",
	// Hook output wrappers
	"user-prompt-submit-hook",
	// Large output persistence
	"persisted-output",
	// Tool loading system
	"available-deferred-tools",
	// Fast mode info
	"fast_mode_info",
	// Anthropic internal
	"antml_thinking",
	"antml_function_calls",
}

// systemXMLRegexps lazily compiles a regexp for each tag exactly once on first
// use. The result is immutable after compilation, so sync.OnceValue safely
// shares it across callers without re-compiling at package init.
var systemXMLRegexps = sync.OnceValue(func() []*regexp.Regexp {
	res := make([]*regexp.Regexp, len(systemXMLTags))
	for i, tag := range systemXMLTags {
		// Match opening tag (with optional attributes), content (including newlines), and closing tag
		res[i] = regexp.MustCompile(`(?s)<` + regexp.QuoteMeta(tag) + `[^>]*>.*?</` + regexp.QuoteMeta(tag) + `>`)
	}
	return res
})

// StripSystemXML removes known Claude Code internal XML blocks from text.
// This prevents system artifacts like <task-notification>, <system-reminder>,
// and <relevant-memory> from being stored in observations and summaries.
func StripSystemXML(s string) string {
	// Quick check: if no angle brackets, nothing to strip
	if !strings.Contains(s, "<") {
		return s
	}

	for _, re := range systemXMLRegexps() {
		s = re.ReplaceAllString(s, "")
	}

	// Clean up resulting double-blank-lines from removed blocks
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}

	return strings.TrimSpace(s)
}
