package chunking

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// =============================================================================
// TESTS FOR Chunk METHODS
// =============================================================================

func TestChunk_Identifier(t *testing.T) {
	tests := []struct {
		name     string
		expected string
		chunk    Chunk
	}{
		// ===== GOOD CASES =====
		{
			name: "top-level function",
			chunk: Chunk{
				Name:       "MyFunction",
				ParentName: "",
			},
			expected: "MyFunction",
		},
		{
			name: "method with parent",
			chunk: Chunk{
				Name:       "Process",
				ParentName: "Handler",
			},
			expected: "Handler.Process",
		},
		{
			name: "nested method",
			chunk: Chunk{
				Name:       "Validate",
				ParentName: "UserService",
			},
			expected: "UserService.Validate",
		},

		// ===== EDGE CASES =====
		{
			name: "empty name",
			chunk: Chunk{
				Name:       "",
				ParentName: "",
			},
			expected: "",
		},
		{
			name: "parent but no name",
			chunk: Chunk{
				Name:       "",
				ParentName: "Parent",
			},
			expected: "Parent.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.chunk.Identifier()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestChunk_LineRange(t *testing.T) {
	tests := []struct {
		name     string
		expected string
		chunk    Chunk
	}{
		// ===== GOOD CASES =====
		{
			name: "single line",
			chunk: Chunk{
				StartLine: 10,
				EndLine:   10,
			},
			expected: "L10-L10",
		},
		{
			name: "multi-line",
			chunk: Chunk{
				StartLine: 25,
				EndLine:   50,
			},
			expected: "L25-L50",
		},

		// ===== EDGE CASES =====
		{
			name: "line 1",
			chunk: Chunk{
				StartLine: 1,
				EndLine:   5,
			},
			expected: "L1-L5",
		},
		{
			name: "large line numbers",
			chunk: Chunk{
				StartLine: 1000,
				EndLine:   2500,
			},
			expected: "L1000-L2500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.chunk.LineRange()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestChunk_SearchableContent(t *testing.T) {
	tests := []struct {
		name     string
		contains []string
		chunk    Chunk
	}{
		// ===== GOOD CASES =====
		{
			name: "full chunk with all fields",
			chunk: Chunk{
				Signature:  "func ProcessData(input []byte) error",
				DocComment: "// ProcessData handles incoming data",
				Content:    "func ProcessData(input []byte) error {\n\treturn nil\n}",
			},
			contains: []string{
				"func ProcessData(input []byte) error",
				"ProcessData handles incoming data",
				"return nil",
			},
		},
		{
			name: "only signature",
			chunk: Chunk{
				Signature: "func Hello()",
			},
			contains: []string{"func Hello()"},
		},
		{
			name: "only content",
			chunk: Chunk{
				Content: "some code here",
			},
			contains: []string{"some code here"},
		},

		// ===== EDGE CASES =====
		{
			name:     "empty chunk",
			chunk:    Chunk{},
			contains: []string{},
		},
		{
			name: "only doc comment",
			chunk: Chunk{
				DocComment: "// Important documentation",
			},
			contains: []string{"Important documentation"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.chunk.SearchableContent()
			for _, expected := range tt.contains {
				assert.Contains(t, result, expected)
			}
		})
	}
}

func TestDefaultChunkOptions(t *testing.T) {
	opts := DefaultChunkOptions()

	assert.Greater(t, opts.MaxChunkSize, 0, "MaxChunkSize should be positive")
	assert.True(t, opts.IncludeDocComments, "IncludeDocComments should be true by default")
	assert.True(t, opts.IncludePrivate, "IncludePrivate should be true by default")
	assert.Equal(t, 0, opts.MinLines, "MinLines should be 0 by default")
}

// =============================================================================
// TESTS FOR ChunkType AND Language CONSTANTS
// =============================================================================

func TestChunkType_Values(t *testing.T) {
	// Ensure all chunk types have expected values
	assert.Equal(t, ChunkType("function"), ChunkTypeFunction)
	assert.Equal(t, ChunkType("method"), ChunkTypeMethod)
	assert.Equal(t, ChunkType("class"), ChunkTypeClass)
	assert.Equal(t, ChunkType("interface"), ChunkTypeInterface)
	assert.Equal(t, ChunkType("type"), ChunkTypeType)
	assert.Equal(t, ChunkType("const"), ChunkTypeConst)
	assert.Equal(t, ChunkType("var"), ChunkTypeVar)
}

func TestLanguage_Values(t *testing.T) {
	// Ensure all language types have expected values
	assert.Equal(t, Language("go"), LanguageGo)
	assert.Equal(t, Language("python"), LanguagePython)
	assert.Equal(t, Language("typescript"), LanguageTypeScript)
	assert.Equal(t, Language("javascript"), LanguageJavaScript)
}
