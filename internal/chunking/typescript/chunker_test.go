package typescript

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lukaszraczylo/claude-mnemonic/internal/chunking"
)

// =============================================================================
// TEST HELPERS
// =============================================================================

func createTempTSFile(t *testing.T, content string, ext string) string {
	t.Helper()

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test"+ext)

	err := os.WriteFile(filePath, []byte(content), 0600)
	require.NoError(t, err)

	return filePath
}

// =============================================================================
// TESTS FOR Chunker
// =============================================================================

func TestNewChunker(t *testing.T) {
	t.Parallel()

	opts := chunking.DefaultChunkOptions()
	c := NewChunker(opts)

	assert.NotNil(t, c)
	assert.NotNil(t, c.parser)
}

func TestChunker_Language(t *testing.T) {
	t.Parallel()

	c := NewChunker(chunking.DefaultChunkOptions())

	assert.Equal(t, chunking.LanguageTypeScript, c.Language())
}

func TestChunker_SupportedExtensions(t *testing.T) {
	t.Parallel()

	c := NewChunker(chunking.DefaultChunkOptions())
	exts := c.SupportedExtensions()

	assert.Contains(t, exts, ".ts")
	assert.Contains(t, exts, ".tsx")
	assert.Contains(t, exts, ".js")
	assert.Contains(t, exts, ".jsx")
}

func TestChunker_Chunk_SimpleFunction(t *testing.T) {
	t.Parallel()

	code := `function greet(name: string): string {
    return "Hello, " + name + "!";
}
`

	filePath := createTempTSFile(t, code, ".ts")
	c := NewChunker(chunking.DefaultChunkOptions())

	chunks, err := c.Chunk(context.Background(), filePath)
	require.NoError(t, err)
	require.NotEmpty(t, chunks)

	// Should find the greet function
	var foundGreet bool
	for _, chunk := range chunks {
		if chunk.Name == "greet" {
			foundGreet = true
			assert.Equal(t, chunking.ChunkTypeFunction, chunk.Type)
			assert.Equal(t, chunking.LanguageTypeScript, chunk.Language)
			assert.Contains(t, chunk.Content, "function greet")
		}
	}
	assert.True(t, foundGreet, "Should find 'greet' function")
}

func TestChunker_Chunk_ClassWithMethods(t *testing.T) {
	t.Parallel()

	code := `class Calculator {
    add(a: number, b: number): number {
        return a + b;
    }

    multiply(a: number, b: number): number {
        return a * b;
    }
}
`

	filePath := createTempTSFile(t, code, ".ts")
	c := NewChunker(chunking.DefaultChunkOptions())

	chunks, err := c.Chunk(context.Background(), filePath)
	require.NoError(t, err)
	require.NotEmpty(t, chunks)

	// Should find the Calculator class and its methods
	var foundClass, foundAdd, foundMultiply bool
	for _, chunk := range chunks {
		switch chunk.Name {
		case "Calculator":
			foundClass = true
			assert.Equal(t, chunking.ChunkTypeClass, chunk.Type)
		case "add":
			foundAdd = true
			assert.Equal(t, chunking.ChunkTypeMethod, chunk.Type)
			assert.Equal(t, "Calculator", chunk.ParentName)
		case "multiply":
			foundMultiply = true
			assert.Equal(t, chunking.ChunkTypeMethod, chunk.Type)
			assert.Equal(t, "Calculator", chunk.ParentName)
		}
	}

	assert.True(t, foundClass, "Should find 'Calculator' class")
	assert.True(t, foundAdd, "Should find 'add' method")
	assert.True(t, foundMultiply, "Should find 'multiply' method")
}

func TestChunker_Chunk_Interface(t *testing.T) {
	t.Parallel()

	code := `interface User {
    id: number;
    name: string;
    email: string;
}

interface Authenticator {
    login(username: string, password: string): boolean;
    logout(): void;
}
`

	filePath := createTempTSFile(t, code, ".ts")
	c := NewChunker(chunking.DefaultChunkOptions())

	chunks, err := c.Chunk(context.Background(), filePath)
	require.NoError(t, err)
	require.NotEmpty(t, chunks)

	// Should find interfaces
	interfaceNames := make(map[string]bool)
	for _, chunk := range chunks {
		if chunk.Type == chunking.ChunkTypeInterface {
			interfaceNames[chunk.Name] = true
		}
	}

	assert.True(t, interfaceNames["User"])
	assert.True(t, interfaceNames["Authenticator"])
}

func TestChunker_Chunk_TypeAlias(t *testing.T) {
	t.Parallel()

	code := `type UserID = string;

type Handler = (event: Event) => void;

type Result<T> = { success: true; data: T } | { success: false; error: Error };
`

	filePath := createTempTSFile(t, code, ".ts")
	c := NewChunker(chunking.DefaultChunkOptions())

	chunks, err := c.Chunk(context.Background(), filePath)
	require.NoError(t, err)
	require.NotEmpty(t, chunks)

	// Should find type aliases
	typeNames := make(map[string]bool)
	for _, chunk := range chunks {
		if chunk.Type == chunking.ChunkTypeType {
			typeNames[chunk.Name] = true
		}
	}

	assert.True(t, typeNames["UserID"])
	assert.True(t, typeNames["Handler"])
	assert.True(t, typeNames["Result"])
}

func TestChunker_Chunk_ArrowFunction(t *testing.T) {
	t.Parallel()

	code := `const add = (a: number, b: number): number => a + b;

const greet = (name: string): string => {
    return "Hello, " + name;
};
`

	filePath := createTempTSFile(t, code, ".ts")
	c := NewChunker(chunking.DefaultChunkOptions())

	_, err := c.Chunk(context.Background(), filePath)
	require.NoError(t, err)
	// Arrow functions may or may not be captured depending on AST structure
	// At minimum, no error should occur
}

func TestChunker_Chunk_FileNotFound(t *testing.T) {
	t.Parallel()

	c := NewChunker(chunking.DefaultChunkOptions())

	_, err := c.Chunk(context.Background(), "/nonexistent/path/file.ts")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read file")
}

func TestChunker_Chunk_EmptyFile(t *testing.T) {
	t.Parallel()

	filePath := createTempTSFile(t, "", ".ts")
	c := NewChunker(chunking.DefaultChunkOptions())

	chunks, err := c.Chunk(context.Background(), filePath)
	require.NoError(t, err)
	assert.Empty(t, chunks)
}

func TestChunker_Chunk_OnlyComments(t *testing.T) {
	t.Parallel()

	code := `// This is a comment
/* Another comment */
/**
 * JSDoc comment
 */
`

	filePath := createTempTSFile(t, code, ".ts")
	c := NewChunker(chunking.DefaultChunkOptions())

	chunks, err := c.Chunk(context.Background(), filePath)
	require.NoError(t, err)
	// Comments without code should not produce chunks
	assert.Empty(t, chunks)
}

func TestChunker_Chunk_AsyncFunction(t *testing.T) {
	t.Parallel()

	code := `async function fetchData(url: string): Promise<any> {
    const response = await fetch(url);
    return response.json();
}

async function processItems(items: string[]): Promise<void> {
    for (const item of items) {
        await process(item);
    }
}
`

	filePath := createTempTSFile(t, code, ".ts")
	c := NewChunker(chunking.DefaultChunkOptions())

	chunks, err := c.Chunk(context.Background(), filePath)
	require.NoError(t, err)
	require.NotEmpty(t, chunks)

	// Should find async functions
	functionNames := make(map[string]bool)
	for _, chunk := range chunks {
		if chunk.Type == chunking.ChunkTypeFunction {
			functionNames[chunk.Name] = true
		}
	}

	assert.True(t, functionNames["fetchData"])
	assert.True(t, functionNames["processItems"])
}

func TestChunker_Chunk_ExportedFunction(t *testing.T) {
	t.Parallel()

	code := `export function publicFunction(): void {
    console.log("public");
}

export default function defaultExport(): void {
    console.log("default");
}
`

	filePath := createTempTSFile(t, code, ".ts")
	c := NewChunker(chunking.DefaultChunkOptions())

	chunks, err := c.Chunk(context.Background(), filePath)
	require.NoError(t, err)
	require.NotEmpty(t, chunks)

	// Should find exported functions
	functionNames := make(map[string]bool)
	for _, chunk := range chunks {
		if chunk.Type == chunking.ChunkTypeFunction {
			functionNames[chunk.Name] = true
		}
	}

	assert.True(t, functionNames["publicFunction"])
	assert.True(t, functionNames["defaultExport"])
}

func TestChunker_Chunk_JSXFile(t *testing.T) {
	t.Parallel()

	code := `function Button({ label }: { label: string }) {
    return <button>{label}</button>;
}

function App() {
    return (
        <div>
            <Button label="Click me" />
        </div>
    );
}
`

	filePath := createTempTSFile(t, code, ".tsx")
	c := NewChunker(chunking.DefaultChunkOptions())

	chunks, err := c.Chunk(context.Background(), filePath)
	require.NoError(t, err)
	require.NotEmpty(t, chunks)

	// Should find JSX components as functions
	functionNames := make(map[string]bool)
	for _, chunk := range chunks {
		if chunk.Type == chunking.ChunkTypeFunction {
			functionNames[chunk.Name] = true
		}
	}

	assert.True(t, functionNames["Button"])
	assert.True(t, functionNames["App"])
}

func TestChunker_Chunk_JavaScript(t *testing.T) {
	t.Parallel()

	code := `function simpleFunc() {
    return 42;
}

class MyClass {
    constructor() {
        this.value = 0;
    }

    getValue() {
        return this.value;
    }
}
`

	filePath := createTempTSFile(t, code, ".js")
	c := NewChunker(chunking.DefaultChunkOptions())

	chunks, err := c.Chunk(context.Background(), filePath)
	require.NoError(t, err)
	require.NotEmpty(t, chunks)

	// Should find JavaScript functions and classes
	var foundFunc, foundClass bool
	for _, chunk := range chunks {
		if chunk.Name == "simpleFunc" {
			foundFunc = true
		}
		if chunk.Name == "MyClass" {
			foundClass = true
		}
	}

	assert.True(t, foundFunc, "Should find 'simpleFunc' function")
	assert.True(t, foundClass, "Should find 'MyClass' class")
}
