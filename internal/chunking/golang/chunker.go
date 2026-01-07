// Package golang provides AST-aware chunking for Go source files.
package golang

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"

	"github.com/lukaszraczylo/claude-mnemonic/internal/chunking"
)

// Chunker implements AST-aware chunking for Go files.
type Chunker struct {
	options chunking.ChunkOptions
}

// NewChunker creates a new Go chunker.
func NewChunker(options chunking.ChunkOptions) *Chunker {
	return &Chunker{options: options}
}

// Language returns the language this chunker supports.
func (c *Chunker) Language() chunking.Language {
	return chunking.LanguageGo
}

// SupportedExtensions returns the file extensions this chunker handles.
func (c *Chunker) SupportedExtensions() []string {
	return []string{".go"}
}

// Chunk parses a Go source file and returns semantic code chunks.
func (c *Chunker) Chunk(ctx context.Context, filePath string) ([]chunking.Chunk, error) {
	// Read file content
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	// Parse the Go file
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filePath, content, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse Go file: %w", err)
	}

	chunks := make([]chunking.Chunk, 0)
	sourceLines := strings.Split(string(content), "\n")

	// Extract chunks from declarations
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			chunk := c.extractFunction(fset, d, sourceLines, filePath)
			if chunk != nil {
				chunks = append(chunks, *chunk)
			}
		case *ast.GenDecl:
			extracted := c.extractGenDecl(fset, d, sourceLines, filePath)
			chunks = append(chunks, extracted...)
		}
	}

	return chunks, nil
}

// extractFunction extracts a function or method declaration as a chunk.
func (c *Chunker) extractFunction(fset *token.FileSet, fn *ast.FuncDecl, sourceLines []string, filePath string) *chunking.Chunk {
	// Skip unexported if configured
	if !c.options.IncludePrivate && !fn.Name.IsExported() {
		return nil
	}

	startPos := fset.Position(fn.Pos())
	endPos := fset.Position(fn.End())

	chunk := &chunking.Chunk{
		FilePath:  filePath,
		Language:  chunking.LanguageGo,
		Name:      fn.Name.Name,
		StartLine: startPos.Line,
		EndLine:   endPos.Line,
	}

	// Determine if this is a method or a function
	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		chunk.Type = chunking.ChunkTypeMethod
		chunk.ParentName = c.extractReceiverType(fn.Recv)
	} else {
		chunk.Type = chunking.ChunkTypeFunction
	}

	// Extract content
	chunk.Content = c.extractLines(sourceLines, startPos.Line, endPos.Line)

	// Extract signature (function declaration without body)
	chunk.Signature = c.extractFunctionSignature(fn, fset, sourceLines)

	// Extract doc comment
	if c.options.IncludeDocComments && fn.Doc != nil {
		chunk.DocComment = strings.TrimSpace(fn.Doc.Text())
	}

	return chunk
}

// extractGenDecl extracts general declarations (type, const, var).
func (c *Chunker) extractGenDecl(fset *token.FileSet, gd *ast.GenDecl, sourceLines []string, filePath string) []chunking.Chunk {
	var chunks []chunking.Chunk

	for _, spec := range gd.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			chunk := c.extractTypeSpec(fset, gd, s, sourceLines, filePath)
			if chunk != nil {
				chunks = append(chunks, *chunk)
			}
		case *ast.ValueSpec:
			// Handle const and var declarations
			chunk := c.extractValueSpec(fset, gd, s, sourceLines, filePath)
			if chunk != nil {
				chunks = append(chunks, *chunk)
			}
		}
	}

	return chunks
}

// extractTypeSpec extracts a type declaration (struct, interface, type alias).
func (c *Chunker) extractTypeSpec(fset *token.FileSet, gd *ast.GenDecl, ts *ast.TypeSpec, sourceLines []string, filePath string) *chunking.Chunk {
	// Skip unexported if configured
	if !c.options.IncludePrivate && !ts.Name.IsExported() {
		return nil
	}

	startPos := fset.Position(gd.Pos())
	endPos := fset.Position(gd.End())

	chunk := &chunking.Chunk{
		FilePath:  filePath,
		Language:  chunking.LanguageGo,
		Name:      ts.Name.Name,
		StartLine: startPos.Line,
		EndLine:   endPos.Line,
		Content:   c.extractLines(sourceLines, startPos.Line, endPos.Line),
	}

	// Determine chunk type based on type expression
	switch ts.Type.(type) {
	case *ast.StructType:
		chunk.Type = chunking.ChunkTypeClass // Treat struct as class
	case *ast.InterfaceType:
		chunk.Type = chunking.ChunkTypeInterface
	default:
		chunk.Type = chunking.ChunkTypeType
	}

	// Extract doc comment
	if c.options.IncludeDocComments && gd.Doc != nil {
		chunk.DocComment = strings.TrimSpace(gd.Doc.Text())
	}

	return chunk
}

// extractValueSpec extracts const or var declarations.
func (c *Chunker) extractValueSpec(fset *token.FileSet, gd *ast.GenDecl, vs *ast.ValueSpec, sourceLines []string, filePath string) *chunking.Chunk {
	// Skip if all names are unexported and we're excluding private
	if !c.options.IncludePrivate {
		allUnexported := true
		for _, name := range vs.Names {
			if name.IsExported() {
				allUnexported = false
				break
			}
		}
		if allUnexported {
			return nil
		}
	}

	startPos := fset.Position(gd.Pos())
	endPos := fset.Position(gd.End())

	// Use first name as the chunk name, join multiple if present
	names := make([]string, len(vs.Names))
	for i, name := range vs.Names {
		names[i] = name.Name
	}

	chunk := &chunking.Chunk{
		FilePath:  filePath,
		Language:  chunking.LanguageGo,
		Name:      strings.Join(names, ", "),
		StartLine: startPos.Line,
		EndLine:   endPos.Line,
		Content:   c.extractLines(sourceLines, startPos.Line, endPos.Line),
	}

	// Set type based on token
	if gd.Tok == token.CONST {
		chunk.Type = chunking.ChunkTypeConst
	} else {
		chunk.Type = chunking.ChunkTypeVar
	}

	// Extract doc comment
	if c.options.IncludeDocComments && gd.Doc != nil {
		chunk.DocComment = strings.TrimSpace(gd.Doc.Text())
	}

	return chunk
}

// extractReceiverType extracts the receiver type name from a method.
func (c *Chunker) extractReceiverType(recv *ast.FieldList) string {
	if len(recv.List) == 0 {
		return ""
	}

	field := recv.List[0]
	switch t := field.Type.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		if ident, ok := t.X.(*ast.Ident); ok {
			return ident.Name
		}
	}

	return ""
}

// extractFunctionSignature extracts the function signature without the body.
func (c *Chunker) extractFunctionSignature(fn *ast.FuncDecl, fset *token.FileSet, sourceLines []string) string {
	if fn.Body == nil {
		// No body, return entire declaration
		startPos := fset.Position(fn.Pos())
		endPos := fset.Position(fn.End())
		return c.extractLines(sourceLines, startPos.Line, endPos.Line)
	}

	// Extract from start of function to just before body
	startPos := fset.Position(fn.Pos())
	bodyPos := fset.Position(fn.Body.Pos())

	// If body is on the same line, extract just that line up to the opening brace
	if startPos.Line == bodyPos.Line {
		line := sourceLines[startPos.Line-1]
		// Find the opening brace position
		if idx := strings.Index(line[startPos.Column-1:], "{"); idx >= 0 {
			return strings.TrimSpace(line[startPos.Column-1 : startPos.Column-1+idx])
		}
		return strings.TrimSpace(line[startPos.Column-1:])
	}

	// Get lines from start to the line containing the opening brace
	sig := c.extractLines(sourceLines, startPos.Line, bodyPos.Line)
	// Remove the opening brace and anything after it
	if idx := strings.Index(sig, "{"); idx >= 0 {
		sig = sig[:idx]
	}
	return strings.TrimSpace(sig)
}

// extractLines extracts a range of lines from source (1-indexed, inclusive).
func (c *Chunker) extractLines(lines []string, start, end int) string {
	if start < 1 || end < start || start > len(lines) {
		return ""
	}

	// Adjust for 0-indexed array (start and end are 1-indexed)
	startIdx := start - 1
	endIdx := end
	if endIdx > len(lines) {
		endIdx = len(lines)
	}

	return strings.Join(lines[startIdx:endIdx], "\n")
}
