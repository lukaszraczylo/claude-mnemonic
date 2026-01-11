package python

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

func createTempPythonFile(t *testing.T, content string) string {
	t.Helper()

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.py")

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

	assert.Equal(t, chunking.LanguagePython, c.Language())
}

func TestChunker_SupportedExtensions(t *testing.T) {
	t.Parallel()

	c := NewChunker(chunking.DefaultChunkOptions())
	exts := c.SupportedExtensions()

	assert.Contains(t, exts, ".py")
}

func TestChunker_Chunk_SimpleFunction(t *testing.T) {
	t.Parallel()

	code := `def greet(name):
    """Greets a person by name."""
    return f"Hello, {name}!"
`

	filePath := createTempPythonFile(t, code)
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
			assert.Equal(t, chunking.LanguagePython, chunk.Language)
			assert.Contains(t, chunk.Content, "def greet")
		}
	}
	assert.True(t, foundGreet, "Should find 'greet' function")
}

func TestChunker_Chunk_ClassWithMethods(t *testing.T) {
	t.Parallel()

	code := `class Calculator:
    """A simple calculator class."""

    def add(self, a, b):
        """Adds two numbers."""
        return a + b

    def multiply(self, a, b):
        """Multiplies two numbers."""
        return a * b
`

	filePath := createTempPythonFile(t, code)
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

func TestChunker_Chunk_MultipleFunctions(t *testing.T) {
	t.Parallel()

	code := `def first_function():
    pass

def second_function(x, y):
    return x + y

def third_function():
    """Has a docstring."""
    return 42
`

	filePath := createTempPythonFile(t, code)
	c := NewChunker(chunking.DefaultChunkOptions())

	chunks, err := c.Chunk(context.Background(), filePath)
	require.NoError(t, err)

	// Should find all three functions
	functionNames := make(map[string]bool)
	for _, chunk := range chunks {
		if chunk.Type == chunking.ChunkTypeFunction {
			functionNames[chunk.Name] = true
		}
	}

	assert.True(t, functionNames["first_function"])
	assert.True(t, functionNames["second_function"])
	assert.True(t, functionNames["third_function"])
}

func TestChunker_Chunk_FileNotFound(t *testing.T) {
	t.Parallel()

	c := NewChunker(chunking.DefaultChunkOptions())

	_, err := c.Chunk(context.Background(), "/nonexistent/path/file.py")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read file")
}

func TestChunker_Chunk_EmptyFile(t *testing.T) {
	t.Parallel()

	filePath := createTempPythonFile(t, "")
	c := NewChunker(chunking.DefaultChunkOptions())

	chunks, err := c.Chunk(context.Background(), filePath)
	require.NoError(t, err)
	assert.Empty(t, chunks)
}

func TestChunker_Chunk_OnlyComments(t *testing.T) {
	t.Parallel()

	code := `# This is a comment
# Another comment
"""
This is a module docstring
"""
`

	filePath := createTempPythonFile(t, code)
	c := NewChunker(chunking.DefaultChunkOptions())

	chunks, err := c.Chunk(context.Background(), filePath)
	require.NoError(t, err)
	// Comments and docstrings without code should not produce chunks
	assert.Empty(t, chunks)
}

func TestChunker_Chunk_NestedClass(t *testing.T) {
	t.Parallel()

	code := `class Outer:
    class Inner:
        def inner_method(self):
            pass

    def outer_method(self):
        pass
`

	filePath := createTempPythonFile(t, code)
	c := NewChunker(chunking.DefaultChunkOptions())

	chunks, err := c.Chunk(context.Background(), filePath)
	require.NoError(t, err)
	require.NotEmpty(t, chunks)

	// Should find the Outer class at minimum
	var foundOuter bool
	for _, chunk := range chunks {
		if chunk.Name == "Outer" {
			foundOuter = true
		}
	}
	assert.True(t, foundOuter, "Should find 'Outer' class")
}

func TestChunker_Chunk_Decorators(t *testing.T) {
	t.Parallel()

	code := `@staticmethod
def static_func():
    pass

@classmethod
def class_func(cls):
    pass

@property
def my_property(self):
    return self._value
`

	filePath := createTempPythonFile(t, code)
	c := NewChunker(chunking.DefaultChunkOptions())

	chunks, err := c.Chunk(context.Background(), filePath)
	require.NoError(t, err)
	require.NotEmpty(t, chunks)

	// Should find decorated functions
	functionNames := make(map[string]bool)
	for _, chunk := range chunks {
		functionNames[chunk.Name] = true
	}

	assert.True(t, functionNames["static_func"])
	assert.True(t, functionNames["class_func"])
	assert.True(t, functionNames["my_property"])
}

func TestChunker_Chunk_AsyncFunction(t *testing.T) {
	t.Parallel()

	code := `async def fetch_data(url):
    """Fetches data from URL asynchronously."""
    pass

async def process_items(items):
    for item in items:
        await process(item)
`

	filePath := createTempPythonFile(t, code)
	c := NewChunker(chunking.DefaultChunkOptions())

	chunks, err := c.Chunk(context.Background(), filePath)
	require.NoError(t, err)
	require.NotEmpty(t, chunks)

	// Should find async functions
	functionNames := make(map[string]bool)
	for _, chunk := range chunks {
		functionNames[chunk.Name] = true
	}

	assert.True(t, functionNames["fetch_data"])
	assert.True(t, functionNames["process_items"])
}
