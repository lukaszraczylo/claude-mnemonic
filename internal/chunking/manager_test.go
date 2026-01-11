package chunking

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// mockChunker is a test chunker that returns dummy chunks
type mockChunker struct{}

func (m *mockChunker) Chunk(ctx context.Context, filePath string) ([]Chunk, error) {
	// Just return an empty chunk for testing
	return []Chunk{
		{
			FilePath:  filePath,
			Language:  LanguageGo,
			Type:      ChunkTypeFunction,
			Name:      "TestFunc",
			StartLine: 1,
			EndLine:   1,
			Content:   "test",
		},
	}, nil
}

func (m *mockChunker) Language() Language {
	return LanguageGo
}

func (m *mockChunker) SupportedExtensions() []string {
	return []string{".go", ".py", ".ts"}
}

func TestManager_ChunkMultipleFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a Go file
	goFile := filepath.Join(tmpDir, "test.go")
	goCode := `package main

func Hello() string {
	return "hello"
}
`
	if err := os.WriteFile(goFile, []byte(goCode), 0600); err != nil {
		t.Fatalf("Failed to create Go file: %v", err)
	}

	// Create a Python file
	pyFile := filepath.Join(tmpDir, "test.py")
	pyCode := `def greet(name):
    return f"Hello, {name}!"

class User:
    def __init__(self, name):
        self.name = name
`
	if err := os.WriteFile(pyFile, []byte(pyCode), 0600); err != nil {
		t.Fatalf("Failed to create Python file: %v", err)
	}

	// Create a TypeScript file
	tsFile := filepath.Join(tmpDir, "test.ts")
	tsCode := `function add(a: number, b: number): number {
    return a + b;
}

class Calculator {
    multiply(a: number, b: number): number {
        return a * b;
    }
}
`
	if err := os.WriteFile(tsFile, []byte(tsCode), 0600); err != nil {
		t.Fatalf("Failed to create TypeScript file: %v", err)
	}

	// Create manager
	manager := NewManager([]Chunker{&mockChunker{}}, DefaultChunkOptions())

	// Test SupportsFile
	if !manager.SupportsFile(goFile) {
		t.Error("Manager should support .go files")
	}
	if !manager.SupportsFile(pyFile) {
		t.Error("Manager should support .py files")
	}
	if !manager.SupportsFile(tsFile) {
		t.Error("Manager should support .ts files")
	}

	unsupportedFile := filepath.Join(tmpDir, "test.txt")
	if manager.SupportsFile(unsupportedFile) {
		t.Error("Manager should not support .txt files")
	}

	// Test ChunkFiles
	results, errs := manager.ChunkFiles(context.Background(), []string{goFile, pyFile, tsFile})
	if len(errs) > 0 {
		t.Errorf("ChunkFiles returned errors: %v", errs)
	}

	if len(results) != 3 {
		t.Errorf("Expected results for 3 files, got %d", len(results))
	}

	// Verify each file has chunks
	for _, file := range []string{goFile, pyFile, tsFile} {
		if chunks, ok := results[file]; !ok || len(chunks) == 0 {
			t.Errorf("No chunks found for file %s", file)
		}
	}
}

// mockChunkerWithExts is a test chunker with configurable extensions
type mockChunkerWithExts struct {
	exts []string
}

func (m *mockChunkerWithExts) Chunk(ctx context.Context, filePath string) ([]Chunk, error) {
	return nil, nil
}

func (m *mockChunkerWithExts) Language() Language {
	return LanguageGo
}

func (m *mockChunkerWithExts) SupportedExtensions() []string {
	return m.exts
}

func TestManager_SupportedExtensions(t *testing.T) {

	// Create manager with mock chunkers
	manager := NewManager([]Chunker{
		&mockChunkerWithExts{exts: []string{".go"}},
		&mockChunkerWithExts{exts: []string{".py", ".pyw"}},
	}, DefaultChunkOptions())

	exts := manager.SupportedExtensions()
	expectedExts := map[string]bool{
		".go":  false,
		".py":  false,
		".pyw": false,
	}

	for _, ext := range exts {
		if _, ok := expectedExts[ext]; ok {
			expectedExts[ext] = true
		} else {
			t.Errorf("Unexpected extension: %s", ext)
		}
	}

	for ext, found := range expectedExts {
		if !found {
			t.Errorf("Expected extension %s not found", ext)
		}
	}
}
