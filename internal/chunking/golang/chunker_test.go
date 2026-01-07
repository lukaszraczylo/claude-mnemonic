package golang

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/lukaszraczylo/claude-mnemonic/internal/chunking"
)

func TestGoChunker_BasicFunctions(t *testing.T) {
	// Create temp test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")

	testCode := `package main

import "fmt"

// Greet prints a greeting message
func Greet(name string) {
	fmt.Printf("Hello, %s!\n", name)
}

// Add adds two numbers
func Add(a, b int) int {
	return a + b
}

// unexported function should be included by default
func helper() string {
	return "helper"
}
`

	if err := os.WriteFile(testFile, []byte(testCode), 0600); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create chunker with default options
	chunker := NewChunker(chunking.DefaultChunkOptions())

	// Chunk the file
	chunks, err := chunker.Chunk(context.Background(), testFile)
	if err != nil {
		t.Fatalf("Chunk() failed: %v", err)
	}

	// Verify we got all functions
	if len(chunks) != 3 {
		t.Errorf("Expected 3 chunks (Greet, Add, helper), got %d", len(chunks))
	}

	// Verify chunk details
	expectedNames := map[string]bool{
		"Greet":  false,
		"Add":    false,
		"helper": false,
	}

	for _, chunk := range chunks {
		if chunk.Type != chunking.ChunkTypeFunction {
			t.Errorf("Expected chunk type 'function', got '%s'", chunk.Type)
		}

		if chunk.Language != chunking.LanguageGo {
			t.Errorf("Expected language 'go', got '%s'", chunk.Language)
		}

		if _, ok := expectedNames[chunk.Name]; !ok {
			t.Errorf("Unexpected function name: %s", chunk.Name)
		} else {
			expectedNames[chunk.Name] = true
		}

		// Verify content is non-empty
		if chunk.Content == "" {
			t.Errorf("Chunk %s has empty content", chunk.Name)
		}

		// Verify signature is present for functions
		if chunk.Signature == "" {
			t.Errorf("Chunk %s has empty signature", chunk.Name)
		}
	}

	// Verify all expected functions were found
	for name, found := range expectedNames {
		if !found {
			t.Errorf("Expected function %s not found", name)
		}
	}
}

func TestGoChunker_StructsAndMethods(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")

	testCode := `package main

// User represents a user
type User struct {
	ID   int
	Name string
}

// GetName returns the user's name
func (u *User) GetName() string {
	return u.Name
}

// SetName sets the user's name
func (u *User) SetName(name string) {
	u.Name = name
}
`

	if err := os.WriteFile(testFile, []byte(testCode), 0600); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	chunker := NewChunker(chunking.DefaultChunkOptions())
	chunks, err := chunker.Chunk(context.Background(), testFile)
	if err != nil {
		t.Fatalf("Chunk() failed: %v", err)
	}

	// Should have 1 struct + 2 methods = 3 chunks
	if len(chunks) != 3 {
		t.Errorf("Expected 3 chunks (User struct, GetName, SetName), got %d", len(chunks))
	}

	// Find the struct and methods
	var structChunk, getNameChunk, setNameChunk *chunking.Chunk
	for i := range chunks {
		switch chunks[i].Name {
		case "User":
			structChunk = &chunks[i]
		case "GetName":
			getNameChunk = &chunks[i]
		case "SetName":
			setNameChunk = &chunks[i]
		}
	}

	// Verify struct
	if structChunk == nil {
		t.Fatal("User struct not found")
	}
	if structChunk.Type != chunking.ChunkTypeClass {
		t.Errorf("Expected User to be ChunkTypeClass, got %s", structChunk.Type)
	}

	// Verify methods
	if getNameChunk == nil {
		t.Fatal("GetName method not found")
	}
	if getNameChunk.Type != chunking.ChunkTypeMethod {
		t.Errorf("Expected GetName to be ChunkTypeMethod, got %s", getNameChunk.Type)
	}
	if getNameChunk.ParentName != "User" {
		t.Errorf("Expected GetName parent to be 'User', got '%s'", getNameChunk.ParentName)
	}

	if setNameChunk == nil {
		t.Fatal("SetName method not found")
	}
	if setNameChunk.Type != chunking.ChunkTypeMethod {
		t.Errorf("Expected SetName to be ChunkTypeMethod, got %s", setNameChunk.Type)
	}
	if setNameChunk.ParentName != "User" {
		t.Errorf("Expected SetName parent to be 'User', got '%s'", setNameChunk.ParentName)
	}
}

func TestGoChunker_DocComments(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")

	testCode := `package main

// Calculate performs a calculation.
// It takes two integers and returns their sum.
func Calculate(a, b int) int {
	return a + b
}
`

	if err := os.WriteFile(testFile, []byte(testCode), 0600); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	chunker := NewChunker(chunking.DefaultChunkOptions())
	chunks, err := chunker.Chunk(context.Background(), testFile)
	if err != nil {
		t.Fatalf("Chunk() failed: %v", err)
	}

	if len(chunks) != 1 {
		t.Fatalf("Expected 1 chunk, got %d", len(chunks))
	}

	chunk := chunks[0]
	if chunk.DocComment == "" {
		t.Error("Expected doc comment to be present")
	}

	// Doc comment should contain the comment text
	expectedComment := "Calculate performs a calculation.\nIt takes two integers and returns their sum."
	if chunk.DocComment != expectedComment {
		t.Errorf("Expected doc comment '%s', got '%s'", expectedComment, chunk.DocComment)
	}
}
