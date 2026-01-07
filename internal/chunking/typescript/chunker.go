// Package typescript provides AST-aware chunking for TypeScript and JavaScript source files using tree-sitter.
package typescript

import (
	"context"
	"fmt"
	"os"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/typescript/typescript"

	"github.com/lukaszraczylo/claude-mnemonic/internal/chunking"
)

// Chunker implements AST-aware chunking for TypeScript/JavaScript files.
type Chunker struct {
	parser  *sitter.Parser
	options chunking.ChunkOptions
}

// NewChunker creates a new TypeScript chunker.
func NewChunker(options chunking.ChunkOptions) *Chunker {
	parser := sitter.NewParser()
	parser.SetLanguage(typescript.GetLanguage())

	return &Chunker{
		options: options,
		parser:  parser,
	}
}

// Language returns the language this chunker supports.
func (c *Chunker) Language() chunking.Language {
	return chunking.LanguageTypeScript
}

// SupportedExtensions returns the file extensions this chunker handles.
func (c *Chunker) SupportedExtensions() []string {
	return []string{".ts", ".tsx", ".js", ".jsx"}
}

// Chunk parses a TypeScript/JavaScript source file and returns semantic code chunks.
func (c *Chunker) Chunk(ctx context.Context, filePath string) ([]chunking.Chunk, error) {
	// Read file content
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	// Parse the file
	tree, err := c.parser.ParseCtx(ctx, nil, content)
	if err != nil {
		return nil, fmt.Errorf("parse TypeScript file: %w", err)
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
	case "function_declaration":
		chunk := c.extractFunction(node, source, sourceLines, filePath, parentName)
		if chunk != nil {
			*chunks = append(*chunks, *chunk)
		}

	case "method_definition":
		chunk := c.extractMethod(node, source, sourceLines, filePath, parentName)
		if chunk != nil {
			*chunks = append(*chunks, *chunk)
		}

	case "arrow_function", "function_expression":
		// Handle arrow functions and function expressions assigned to variables
		chunk := c.extractFunctionExpression(node, source, sourceLines, filePath, parentName)
		if chunk != nil {
			*chunks = append(*chunks, *chunk)
		}

	case "class_declaration":
		chunk := c.extractClass(node, source, sourceLines, filePath)
		if chunk != nil {
			*chunks = append(*chunks, *chunk)

			// Walk class body to find methods
			for i := 0; i < int(node.ChildCount()); i++ {
				child := node.Child(i)
				if child.Type() == "class_body" {
					c.walkNode(child, source, sourceLines, filePath, chunk.Name, chunks)
				}
			}
		}
		return // Don't walk children again

	case "interface_declaration":
		chunk := c.extractInterface(node, source, sourceLines, filePath)
		if chunk != nil {
			*chunks = append(*chunks, *chunk)
		}

	case "type_alias_declaration":
		chunk := c.extractTypeAlias(node, source, sourceLines, filePath)
		if chunk != nil {
			*chunks = append(*chunks, *chunk)
		}
	}

	// Walk all children
	for i := 0; i < int(node.ChildCount()); i++ {
		c.walkNode(node.Child(i), source, sourceLines, filePath, parentName, chunks)
	}
}

// extractFunction extracts a function declaration.
func (c *Chunker) extractFunction(node *sitter.Node, source []byte, sourceLines []string, filePath string, parentName string) *chunking.Chunk {
	name := c.findChildContent(node, "identifier", source)
	if name == "" {
		return nil
	}

	startLine := int(node.StartPoint().Row) + 1
	endLine := int(node.EndPoint().Row) + 1

	chunk := &chunking.Chunk{
		FilePath:   filePath,
		Language:   chunking.LanguageTypeScript,
		Type:       chunking.ChunkTypeFunction,
		Name:       name,
		ParentName: parentName,
		StartLine:  startLine,
		EndLine:    endLine,
		Content:    c.extractLines(sourceLines, startLine, endLine),
		Signature:  c.extractFunctionSignature(node, source, sourceLines),
	}

	// Extract JSDoc comment
	if c.options.IncludeDocComments {
		chunk.DocComment = c.extractComment(node, source)
	}

	return chunk
}

// extractMethod extracts a method definition from a class.
func (c *Chunker) extractMethod(node *sitter.Node, source []byte, sourceLines []string, filePath string, parentName string) *chunking.Chunk {
	name := c.findChildContent(node, "property_identifier", source)
	if name == "" {
		return nil
	}

	// Skip private methods if configured
	if !c.options.IncludePrivate && strings.HasPrefix(name, "_") {
		return nil
	}

	startLine := int(node.StartPoint().Row) + 1
	endLine := int(node.EndPoint().Row) + 1

	chunk := &chunking.Chunk{
		FilePath:   filePath,
		Language:   chunking.LanguageTypeScript,
		Type:       chunking.ChunkTypeMethod,
		Name:       name,
		ParentName: parentName,
		StartLine:  startLine,
		EndLine:    endLine,
		Content:    c.extractLines(sourceLines, startLine, endLine),
		Signature:  c.extractMethodSignature(node, source, sourceLines),
	}

	// Extract JSDoc comment
	if c.options.IncludeDocComments {
		chunk.DocComment = c.extractComment(node, source)
	}

	return chunk
}

// extractFunctionExpression extracts arrow functions and function expressions.
func (c *Chunker) extractFunctionExpression(node *sitter.Node, source []byte, sourceLines []string, filePath string, parentName string) *chunking.Chunk {
	// Try to find the variable name from parent
	parent := node.Parent()
	if parent == nil {
		return nil
	}

	var name string
	if parent.Type() == "variable_declarator" {
		name = c.findChildContent(parent, "identifier", source)
	} else if parent.Type() == "assignment_expression" {
		// Handle const foo = () => {}
		for i := 0; i < int(parent.ChildCount()); i++ {
			child := parent.Child(i)
			if child.Type() == "identifier" || child.Type() == "member_expression" {
				name = child.Content(source)
				break
			}
		}
	}

	if name == "" {
		return nil // Anonymous function, skip
	}

	startLine := int(node.StartPoint().Row) + 1
	endLine := int(node.EndPoint().Row) + 1

	chunk := &chunking.Chunk{
		FilePath:   filePath,
		Language:   chunking.LanguageTypeScript,
		Type:       chunking.ChunkTypeFunction,
		Name:       name,
		ParentName: parentName,
		StartLine:  startLine,
		EndLine:    endLine,
		Content:    c.extractLines(sourceLines, startLine, endLine),
	}

	return chunk
}

// extractClass extracts a class declaration.
func (c *Chunker) extractClass(node *sitter.Node, source []byte, sourceLines []string, filePath string) *chunking.Chunk {
	name := c.findChildContent(node, "type_identifier", source)
	if name == "" {
		return nil
	}

	startLine := int(node.StartPoint().Row) + 1
	endLine := int(node.EndPoint().Row) + 1

	chunk := &chunking.Chunk{
		FilePath:  filePath,
		Language:  chunking.LanguageTypeScript,
		Type:      chunking.ChunkTypeClass,
		Name:      name,
		StartLine: startLine,
		EndLine:   endLine,
		Content:   c.extractLines(sourceLines, startLine, endLine),
		Signature: c.extractClassSignature(node, source, sourceLines),
	}

	// Extract JSDoc comment
	if c.options.IncludeDocComments {
		chunk.DocComment = c.extractComment(node, source)
	}

	return chunk
}

// extractInterface extracts an interface declaration.
func (c *Chunker) extractInterface(node *sitter.Node, source []byte, sourceLines []string, filePath string) *chunking.Chunk {
	name := c.findChildContent(node, "type_identifier", source)
	if name == "" {
		return nil
	}

	startLine := int(node.StartPoint().Row) + 1
	endLine := int(node.EndPoint().Row) + 1

	chunk := &chunking.Chunk{
		FilePath:  filePath,
		Language:  chunking.LanguageTypeScript,
		Type:      chunking.ChunkTypeInterface,
		Name:      name,
		StartLine: startLine,
		EndLine:   endLine,
		Content:   c.extractLines(sourceLines, startLine, endLine),
	}

	// Extract JSDoc comment
	if c.options.IncludeDocComments {
		chunk.DocComment = c.extractComment(node, source)
	}

	return chunk
}

// extractTypeAlias extracts a type alias declaration.
func (c *Chunker) extractTypeAlias(node *sitter.Node, source []byte, sourceLines []string, filePath string) *chunking.Chunk {
	name := c.findChildContent(node, "type_identifier", source)
	if name == "" {
		return nil
	}

	startLine := int(node.StartPoint().Row) + 1
	endLine := int(node.EndPoint().Row) + 1

	chunk := &chunking.Chunk{
		FilePath:  filePath,
		Language:  chunking.LanguageTypeScript,
		Type:      chunking.ChunkTypeType,
		Name:      name,
		StartLine: startLine,
		EndLine:   endLine,
		Content:   c.extractLines(sourceLines, startLine, endLine),
	}

	return chunk
}

// findChildContent finds the first child of the given type and returns its content.
func (c *Chunker) findChildContent(node *sitter.Node, childType string, source []byte) string {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == childType {
			return child.Content(source)
		}
	}
	return ""
}

// extractFunctionSignature extracts the function signature.
func (c *Chunker) extractFunctionSignature(node *sitter.Node, source []byte, sourceLines []string) string {
	startLine := int(node.StartPoint().Row) + 1

	// Find the opening brace of the body
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == "statement_block" {
			endLine := int(child.StartPoint().Row) + 1
			return strings.TrimSpace(c.extractLines(sourceLines, startLine, endLine-1))
		}
	}

	// Fallback: just return first line
	return strings.TrimSpace(c.extractLines(sourceLines, startLine, startLine))
}

// extractMethodSignature extracts the method signature.
func (c *Chunker) extractMethodSignature(node *sitter.Node, source []byte, sourceLines []string) string {
	startLine := int(node.StartPoint().Row) + 1

	// Find the opening brace of the body
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == "statement_block" {
			endLine := int(child.StartPoint().Row) + 1
			return strings.TrimSpace(c.extractLines(sourceLines, startLine, endLine-1))
		}
	}

	return strings.TrimSpace(c.extractLines(sourceLines, startLine, startLine))
}

// extractClassSignature extracts the class declaration line.
func (c *Chunker) extractClassSignature(node *sitter.Node, source []byte, sourceLines []string) string {
	startLine := int(node.StartPoint().Row) + 1

	// Find the opening brace of the class body
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == "class_body" {
			endLine := int(child.StartPoint().Row) + 1
			return strings.TrimSpace(c.extractLines(sourceLines, startLine, endLine-1))
		}
	}

	return strings.TrimSpace(c.extractLines(sourceLines, startLine, startLine))
}

// extractComment extracts JSDoc or other comments from a node.
func (c *Chunker) extractComment(node *sitter.Node, source []byte) string {
	// Check previous sibling for comment
	prevSibling := node.PrevSibling()
	if prevSibling != nil && prevSibling.Type() == "comment" {
		comment := prevSibling.Content(source)
		// Remove comment markers
		comment = strings.TrimPrefix(comment, "/**")
		comment = strings.TrimPrefix(comment, "/*")
		comment = strings.TrimSuffix(comment, "*/")
		comment = strings.TrimPrefix(comment, "//")
		return strings.TrimSpace(comment)
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
