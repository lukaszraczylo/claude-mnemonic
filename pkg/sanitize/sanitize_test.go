package sanitize

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStripSystemXML(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no XML tags",
			input:    "Hello, this is plain text with no XML.",
			expected: "Hello, this is plain text with no XML.",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "task-notification block",
			input:    "Before\n<task-notification>\n<task-id>abc123</task-id>\n<status>completed</status>\n<summary>Agent completed</summary>\n<result>Some result text</result>\n</task-notification>\nAfter",
			expected: "Before\n\nAfter",
		},
		{
			name:     "system-reminder block",
			input:    "Content before\n<system-reminder>\nSome system reminder text\n</system-reminder>\nContent after",
			expected: "Content before\n\nContent after",
		},
		{
			name:     "relevant-memory block",
			input:    "<relevant-memory>\n# Relevant Knowledge\n## 1. Something\n</relevant-memory>\nActual content",
			expected: "Actual content",
		},
		{
			name:     "system-reminder with attributes",
			input:    "Before <system-reminder type=\"hook\">reminder text</system-reminder> After",
			expected: "Before  After",
		},
		{
			name:     "multiple different tags",
			input:    "Start\n<task-notification><status>done</status></task-notification>\nMiddle\n<system-reminder>reminder</system-reminder>\nEnd",
			expected: "Start\n\nMiddle\n\nEnd",
		},
		{
			name:     "persisted-output block",
			input:    "Result: <persisted-output>\nLarge output stored at: /tmp/foo\n</persisted-output> done",
			expected: "Result:  done",
		},
		{
			name:     "available-deferred-tools block",
			input:    "<available-deferred-tools>\nTool1\nTool2\n</available-deferred-tools>\nUser message",
			expected: "User message",
		},
		{
			name:     "fast_mode_info block",
			input:    "Text <fast_mode_info>\nFast mode uses same model\n</fast_mode_info> more text",
			expected: "Text  more text",
		},
		{
			name:     "preserves non-system XML",
			input:    "<observation><type>bugfix</type><title>Fixed thing</title></observation>",
			expected: "<observation><type>bugfix</type><title>Fixed thing</title></observation>",
		},
		{
			name:     "no angle brackets fast path",
			input:    "Just plain text without any special characters",
			expected: "Just plain text without any special characters",
		},
		{
			name:     "collapses triple newlines",
			input:    "Before\n<system-reminder>x</system-reminder>\n\n\nAfter",
			expected: "Before\n\nAfter",
		},
		{
			name:     "real-world task notification",
			input:    "Here is the analysis:\n<task-notification>\n<task-id>a077fd04aed547ce1</task-id>\n<tool-use-id>toolu_01Kw2GwsArQQR1F9aV3VfzhP</tool-use-id>\n<status>completed</status>\n<summary>Agent \"Analyze agency-agents repo structure\" completed</summary>\n<result>Now I have all the information I need. Let me compile a comprehensive report.\n\n## Agency-Agents Directory Exploration Report\n\n### 1. Agent File Count and Organization\n\n**Total Agent Files (excluding strategy docs):** 61 agents across 9 categories</result>\n</task-notification>\nThe repo has 61 agents.",
			expected: "Here is the analysis:\n\nThe repo has 61 agents.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := StripSystemXML(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func BenchmarkStripSystemXML(b *testing.B) {
	// Text with no XML (fast path)
	b.Run("no_xml", func(b *testing.B) {
		text := "This is plain text without any XML tags at all."
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			StripSystemXML(text)
		}
	})

	// Text with system XML to strip
	b.Run("with_xml", func(b *testing.B) {
		text := "Before\n<task-notification>\n<task-id>abc</task-id>\n<status>done</status>\n</task-notification>\n<system-reminder>reminder text here</system-reminder>\nAfter"
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			StripSystemXML(text)
		}
	})
}
