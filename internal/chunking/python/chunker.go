// Package python provides AST-aware chunking for Python source files using tree-sitter.
package python

import (
	"context"
	"fmt"
	"os"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/python"

	"github.com/lukaszraczylo/claude-mnemonic/internal/chunking"
)

// Chunker implements AST-aware chunking for Python files.
type Chunker struct {
	parser  *sitter.Parser
	options chunking.ChunkOptions
}

// NewChunker creates a new Python chunker.
func NewChunker(options chunking.ChunkOptions) *Chunker {
	parser := sitter.NewParser()
	parser.SetLanguage(python.GetLanguage())

	return &Chunker{
		options: options,
		parser:  parser,
	}
}

// Language returns the language this chunker supports.
func (c *Chunker) Language() chunking.Language {
	return chunking.LanguagePython
}

// SupportedExtensions returns the file extensions this chunker handles.
func (c *Chunker) SupportedExtensions() []string {
	return []string{".py"}
}

// Chunk parses a Python source file and returns semantic code chunks.
func (c *Chunker) Chunk(ctx context.Context, filePath string) ([]chunking.Chunk, error) {
	// Read file content
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	// Parse the Python file
	tree, err := c.parser.ParseCtx(ctx, nil, content)
	if err != nil {
		return nil, fmt.Errorf("parse Python file: %w", err)
	}
	defer tree.Close()

	sourceLines := strings.Split(string(content), "\n")
	chunks := make([]chunking.Chunk, 0)

	// Walk the AST and extract chunks
	c.walkNode(tree.RootNode(), content, sourceLines, filePath, "", &chunks)

	return chunks, nil
}

// walkNode recursively walks the tree-sitter AST and extracts chunks.
func (c *Chunker) walkNode(node *sitter.Node, source []byte, sourceLines []string, filePath string, parentName string, chunks *[]chunking.Chunk) {
	nodeType := node.Type()

	switch nodeType {
	case "function_definition":
		chunk := c.extractFunction(node, source, sourceLines, filePath, parentName)
		if chunk != nil {
			*chunks = append(*chunks, *chunk)
		}

	case "class_definition":
		chunk := c.extractClass(node, source, sourceLines, filePath)
		if chunk != nil {
			*chunks = append(*chunks, *chunk)

			// Walk class body to find methods
			for i := 0; i < int(node.ChildCount()); i++ {
				child := node.Child(i)
				if child.Type() == "block" {
					c.walkNode(child, source, sourceLines, filePath, chunk.Name, chunks)
				}
			}
		}
		return // Don't walk children again

	case "block":
		// Walk statements in block
		for i := 0; i < int(node.ChildCount()); i++ {
			c.walkNode(node.Child(i), source, sourceLines, filePath, parentName, chunks)
		}
		return
	}

	// Walk all children
	for i := 0; i < int(node.ChildCount()); i++ {
		c.walkNode(node.Child(i), source, sourceLines, filePath, parentName, chunks)
	}
}

// extractFunction extracts a function definition chunk.
func (c *Chunker) extractFunction(node *sitter.Node, source []byte, sourceLines []string, filePath string, parentName string) *chunking.Chunk {
	// Find function name
	var nameNode *sitter.Node
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == "identifier" {
			nameNode = child
			break
		}
	}

	if nameNode == nil {
		return nil
	}

	name := nameNode.Content(source)

	// Skip private functions if configured
	if !c.options.IncludePrivate && strings.HasPrefix(name, "_") && !strings.HasPrefix(name, "__") {
		return nil
	}

	startLine := int(node.StartPoint().Row) + 1
	endLine := int(node.EndPoint().Row) + 1

	chunk := &chunking.Chunk{
		FilePath:   filePath,
		Language:   chunking.LanguagePython,
		Name:       name,
		ParentName: parentName,
		StartLine:  startLine,
		EndLine:    endLine,
		Content:    c.extractLines(sourceLines, startLine, endLine),
	}

	// Determine if this is a method or function
	if parentName != "" {
		chunk.Type = chunking.ChunkTypeMethod
	} else {
		chunk.Type = chunking.ChunkTypeFunction
	}

	// Extract signature (def line)
	chunk.Signature = c.extractFunctionSignature(node, source, sourceLines)

	// Extract docstring as doc comment
	if c.options.IncludeDocComments {
		chunk.DocComment = c.extractDocstring(node, source)
	}

	return chunk
}

// extractClass extracts a class definition chunk.
func (c *Chunker) extractClass(node *sitter.Node, source []byte, sourceLines []string, filePath string) *chunking.Chunk {
	// Find class name
	var nameNode *sitter.Node
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == "identifier" {
			nameNode = child
			break
		}
	}

	if nameNode == nil {
		return nil
	}

	name := nameNode.Content(source)

	// Skip private classes if configured
	if !c.options.IncludePrivate && strings.HasPrefix(name, "_") && !strings.HasPrefix(name, "__") {
		return nil
	}

	startLine := int(node.StartPoint().Row) + 1
	endLine := int(node.EndPoint().Row) + 1

	chunk := &chunking.Chunk{
		FilePath:  filePath,
		Language:  chunking.LanguagePython,
		Type:      chunking.ChunkTypeClass,
		Name:      name,
		StartLine: startLine,
		EndLine:   endLine,
		Content:   c.extractLines(sourceLines, startLine, endLine),
	}

	// Extract class signature (class line)
	chunk.Signature = c.extractClassSignature(node, source, sourceLines)

	// Extract docstring as doc comment
	if c.options.IncludeDocComments {
		chunk.DocComment = c.extractDocstring(node, source)
	}

	return chunk
}

// extractFunctionSignature extracts the function definition line.
func (c *Chunker) extractFunctionSignature(node *sitter.Node, source []byte, sourceLines []string) string {
	startLine := int(node.StartPoint().Row) + 1

	// Find the colon that ends the signature
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == ":" {
			endLine := int(child.EndPoint().Row) + 1
			return strings.TrimSpace(c.extractLines(sourceLines, startLine, endLine))
		}
	}

	// Fallback: just return first line
	return strings.TrimSpace(c.extractLines(sourceLines, startLine, startLine))
}

// extractClassSignature extracts the class definition line.
func (c *Chunker) extractClassSignature(node *sitter.Node, source []byte, sourceLines []string) string {
	startLine := int(node.StartPoint().Row) + 1

	// Find the colon that ends the signature
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == ":" {
			endLine := int(child.EndPoint().Row) + 1
			return strings.TrimSpace(c.extractLines(sourceLines, startLine, endLine))
		}
	}

	// Fallback: just return first line
	return strings.TrimSpace(c.extractLines(sourceLines, startLine, startLine))
}

// extractDocstring extracts the docstring from a function or class.
func (c *Chunker) extractDocstring(node *sitter.Node, source []byte) string {
	// Find the block
	var blockNode *sitter.Node
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == "block" {
			blockNode = child
			break
		}
	}

	if blockNode == nil {
		return ""
	}

	// Check if first statement in block is a string (docstring)
	for i := 0; i < int(blockNode.ChildCount()); i++ {
		child := blockNode.Child(i)
		if child.Type() == "expression_statement" {
			// Check if it contains a string
			for j := 0; j < int(child.ChildCount()); j++ {
				grandchild := child.Child(j)
				if grandchild.Type() == "string" {
					docstring := grandchild.Content(source)
					// Remove quotes
					docstring = strings.Trim(docstring, `"'`)
					return strings.TrimSpace(docstring)
				}
			}
		}
	}

	return ""
}

// extractLines extracts a range of lines from source (1-indexed, inclusive).
func (c *Chunker) extractLines(lines []string, start, end int) string {
	if start < 1 || end < start || start > len(lines) {
		return ""
	}

	startIdx := start - 1
	endIdx := end
	if endIdx > len(lines) {
		endIdx = len(lines)
	}

	return strings.Join(lines[startIdx:endIdx], "\n")
}
