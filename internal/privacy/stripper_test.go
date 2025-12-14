package privacy

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStripPrivateTags(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no tags",
			input:    "Hello world",
			expected: "Hello world",
		},
		{
			name:     "single private tag",
			input:    "Hello <private>secret</private> world",
			expected: "Hello  world",
		},
		{
			name:     "multiple private tags",
			input:    "Hello <private>secret1</private> and <private>secret2</private> world",
			expected: "Hello  and  world",
		},
		{
			name:     "nested content in private tag",
			input:    "Hello <private>secret with\nnewline</private> world",
			expected: "Hello  world",
		},
		{
			name:     "multiline private tag",
			input:    "Hello <private>\nmultiline\nsecret\n</private> world",
			expected: "Hello  world",
		},
		{
			name:     "empty private tag",
			input:    "Hello <private></private> world",
			expected: "Hello  world",
		},
		{
			name:     "entirely private",
			input:    "<private>everything is secret</private>",
			expected: "",
		},
		{
			name:     "unmatched opening tag",
			input:    "Hello <private>unclosed",
			expected: "Hello <private>unclosed",
		},
		{
			name:     "unmatched closing tag",
			input:    "Hello </private> world",
			expected: "Hello </private> world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := StripPrivateTags(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestStripMemoryTags(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no tags",
			input:    "Hello world",
			expected: "Hello world",
		},
		{
			name:     "single memory tag",
			input:    "Hello <claude-mnemonic-context>memory</claude-mnemonic-context> world",
			expected: "Hello  world",
		},
		{
			name:     "multiline memory tag",
			input:    "Hello <claude-mnemonic-context>\nmemory\ncontent\n</claude-mnemonic-context> world",
			expected: "Hello  world",
		},
		{
			name:     "entirely memory context",
			input:    "<claude-mnemonic-context>all memory</claude-mnemonic-context>",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := StripMemoryTags(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestStripAllTags(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no tags",
			input:    "Hello world",
			expected: "Hello world",
		},
		{
			name:     "both tag types",
			input:    "Hello <private>secret</private> and <claude-mnemonic-context>memory</claude-mnemonic-context> world",
			expected: "Hello  and  world",
		},
		{
			name:     "interleaved tags",
			input:    "A <private>B</private> C <claude-mnemonic-context>D</claude-mnemonic-context> E",
			expected: "A  C  E",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := StripAllTags(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsEntirelyPrivate(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "not private",
			input:    "Hello world",
			expected: false,
		},
		{
			name:     "entirely private",
			input:    "<private>secret</private>",
			expected: true,
		},
		{
			name:     "entirely private with whitespace",
			input:    "  <private>secret</private>  ",
			expected: true,
		},
		{
			name:     "partially private",
			input:    "Hello <private>secret</private>",
			expected: false,
		},
		{
			name:     "multiple private tags covering everything",
			input:    "<private>a</private><private>b</private>",
			expected: true,
		},
		{
			name:     "empty string",
			input:    "",
			expected: true, // Empty after stripping means nothing remains
		},
		{
			name:     "only whitespace",
			input:    "   ",
			expected: true, // Whitespace-only after stripping is empty
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsEntirelyPrivate(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestClean(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no tags or whitespace",
			input:    "Hello world",
			expected: "Hello world",
		},
		{
			name:     "strips private tags and trims",
			input:    "  Hello <private>secret</private> world  ",
			expected: "Hello  world",
		},
		{
			name:     "strips memory tags and trims",
			input:    "  Hello <claude-mnemonic-context>memory</claude-mnemonic-context> world  ",
			expected: "Hello  world",
		},
		{
			name:     "strips both tag types and trims",
			input:    "\n  Hello <private>secret</private> and <claude-mnemonic-context>memory</claude-mnemonic-context> world  \n",
			expected: "Hello  and  world",
		},
		{
			name:     "entirely stripped content",
			input:    "  <private>secret</private>  ",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Clean(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Edge cases and security-related tests
func TestPrivacyEdgeCases(t *testing.T) {
	t.Run("nested tags are handled correctly", func(t *testing.T) {
		// Inner tag should be stripped as part of outer content
		input := "<private>outer <private>inner</private> outer</private>"
		result := StripPrivateTags(input)
		// The regex is non-greedy, so it matches the first closing tag
		assert.Equal(t, " outer</private>", result)
	})

	t.Run("html-like content is not confused with tags", func(t *testing.T) {
		input := "Hello <div>world</div>"
		result := StripPrivateTags(input)
		assert.Equal(t, "Hello <div>world</div>", result)
	})

	t.Run("case sensitive tags", func(t *testing.T) {
		input := "Hello <PRIVATE>secret</PRIVATE> world"
		result := StripPrivateTags(input)
		// Should not strip uppercase tags
		assert.Equal(t, "Hello <PRIVATE>secret</PRIVATE> world", result)
	})

	t.Run("special characters in private content", func(t *testing.T) {
		input := "Hello <private>secret$%^&*()</private> world"
		result := StripPrivateTags(input)
		assert.Equal(t, "Hello  world", result)
	})

	t.Run("unicode content", func(t *testing.T) {
		input := "Hello <private>秘密 🔒</private> world"
		result := StripPrivateTags(input)
		assert.Equal(t, "Hello  world", result)
	})

	t.Run("very long private content", func(t *testing.T) {
		longSecret := ""
		for i := 0; i < 10000; i++ {
			longSecret += "x"
		}
		input := "Hello <private>" + longSecret + "</private> world"
		result := StripPrivateTags(input)
		assert.Equal(t, "Hello  world", result)
	})
}
