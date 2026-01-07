// Package chunking provides AST-aware code chunking for semantic code search.
// Chunks code files into logical units (functions, classes, methods) that preserve
// semantic boundaries for better vector embedding and retrieval.
package chunking

import (
	"context"
	"fmt"
	"strings"
)

// ChunkType represents the type of code chunk.
type ChunkType string

const (
	// ChunkTypeFunction represents a standalone function.
	ChunkTypeFunction ChunkType = "function"
	// ChunkTypeMethod represents a method on a class/struct/type.
	ChunkTypeMethod ChunkType = "method"
	// ChunkTypeClass represents a class or struct definition.
	ChunkTypeClass ChunkType = "class"
	// ChunkTypeInterface represents an interface definition.
	ChunkTypeInterface ChunkType = "interface"
	// ChunkTypeType represents a type alias or type definition.
	ChunkTypeType ChunkType = "type"
	// ChunkTypeConst represents constant declarations.
	ChunkTypeConst ChunkType = "const"
	// ChunkTypeVar represents variable declarations.
	ChunkTypeVar ChunkType = "var"
)

// Language represents a programming language.
type Language string

const (
	// LanguageGo represents the Go programming language.
	LanguageGo Language = "go"
	// LanguagePython represents the Python programming language.
	LanguagePython Language = "python"
	// LanguageTypeScript represents the TypeScript programming language.
	LanguageTypeScript Language = "typescript"
	// LanguageJavaScript represents the JavaScript programming language.
	LanguageJavaScript Language = "javascript"
)

// Chunk represents a semantic code chunk with AST-derived boundaries.
type Chunk struct {
	Metadata   map[string]interface{}
	FilePath   string
	Language   Language
	Type       ChunkType
	Name       string
	ParentName string
	Content    string
	Signature  string
	DocComment string
	StartLine  int
	EndLine    int
}

// Identifier returns a human-readable identifier for this chunk.
// Format: "ParentName.Name" for methods, "Name" for top-level.
func (c *Chunk) Identifier() string {
	if c.ParentName != "" {
		return fmt.Sprintf("%s.%s", c.ParentName, c.Name)
	}
	return c.Name
}

// LineRange returns a human-readable line range.
// Format: "L123-L456"
func (c *Chunk) LineRange() string {
	return fmt.Sprintf("L%d-L%d", c.StartLine, c.EndLine)
}

// SearchableContent returns content optimized for semantic search.
// Combines signature, doc comment, and content in a structured format.
func (c *Chunk) SearchableContent() string {
	var parts []string

	// Include signature for functions/methods
	if c.Signature != "" {
		parts = append(parts, c.Signature)
	}

	// Include doc comment
	if c.DocComment != "" {
		parts = append(parts, c.DocComment)
	}

	// Include actual content
	if c.Content != "" {
		parts = append(parts, c.Content)
	}

	return strings.Join(parts, "\n\n")
}

// Chunker is the interface for language-specific code chunkers.
type Chunker interface {
	// Chunk parses a source file and returns semantic code chunks.
	// Returns an error if the file cannot be parsed or read.
	Chunk(ctx context.Context, filePath string) ([]Chunk, error)

	// Language returns the language this chunker supports.
	Language() Language

	// SupportedExtensions returns file extensions this chunker handles.
	// Example: []string{".go"} for Go chunker
	SupportedExtensions() []string
}

// ChunkOptions provides options for chunking behavior.
type ChunkOptions struct {
	// MaxChunkSize is the maximum size of a chunk in bytes.
	// Chunks larger than this will be split (respecting boundaries where possible).
	// 0 means no limit.
	MaxChunkSize int

	// IncludeDocComments controls whether to include documentation comments.
	IncludeDocComments bool

	// IncludePrivate controls whether to include private/unexported symbols.
	IncludePrivate bool

	// MinLines is the minimum number of lines for a chunk to be included.
	// Chunks smaller than this will be skipped.
	// 0 means no minimum.
	MinLines int
}

// DefaultChunkOptions returns sensible default options.
func DefaultChunkOptions() ChunkOptions {
	return ChunkOptions{
		MaxChunkSize:       8192, // ~8KB per chunk (well under token limit)
		IncludeDocComments: true,
		IncludePrivate:     true, // Include all symbols for comprehensive search
		MinLines:           0,    // No minimum - include even single-line functions
	}
}
