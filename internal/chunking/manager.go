package chunking

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// Manager dispatches files to appropriate language-specific chunkers.
type Manager struct {
	chunkers map[string]Chunker // extension -> chunker
	options  ChunkOptions
}

// NewManager creates a new chunking manager with the given chunkers.
func NewManager(chunkers []Chunker, options ChunkOptions) *Manager {
	m := &Manager{
		chunkers: make(map[string]Chunker),
		options:  options,
	}

	// Register chunkers by their supported extensions
	for _, chunker := range chunkers {
		for _, ext := range chunker.SupportedExtensions() {
			m.chunkers[ext] = chunker
		}
	}

	return m
}

// ChunkFile chunks a single file using the appropriate language chunker.
// Returns an error if no chunker is found for the file extension.
func (m *Manager) ChunkFile(ctx context.Context, filePath string) ([]Chunk, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	chunker, ok := m.chunkers[ext]
	if !ok {
		return nil, fmt.Errorf("no chunker for extension %s", ext)
	}

	chunks, err := chunker.Chunk(ctx, filePath)
	if err != nil {
		return nil, fmt.Errorf("chunk %s: %w", filePath, err)
	}

	// Apply options-based filtering
	filtered := make([]Chunk, 0, len(chunks))
	for _, chunk := range chunks {
		// Filter by minimum lines
		if m.options.MinLines > 0 {
			lineCount := chunk.EndLine - chunk.StartLine + 1
			if lineCount < m.options.MinLines {
				continue
			}
		}

		// Filter by maximum chunk size
		if m.options.MaxChunkSize > 0 && len(chunk.Content) > m.options.MaxChunkSize {
			// TODO: Consider splitting large chunks intelligently
			// For now, skip chunks that are too large
			continue
		}

		filtered = append(filtered, chunk)
	}

	return filtered, nil
}

// ChunkFiles chunks multiple files in parallel.
// Returns a map of file path to chunks, and any errors encountered.
// Errors for individual files do not stop processing of other files.
func (m *Manager) ChunkFiles(ctx context.Context, filePaths []string) (map[string][]Chunk, []error) {
	results := make(map[string][]Chunk)
	var errors []error

	for _, filePath := range filePaths {
		chunks, err := m.ChunkFile(ctx, filePath)
		if err != nil {
			errors = append(errors, fmt.Errorf("%s: %w", filePath, err))
			continue
		}
		if len(chunks) > 0 {
			results[filePath] = chunks
		}
	}

	return results, errors
}

// SupportsFile checks if the manager can chunk the given file based on extension.
func (m *Manager) SupportsFile(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	_, ok := m.chunkers[ext]
	return ok
}

// SupportedExtensions returns all file extensions supported by registered chunkers.
func (m *Manager) SupportedExtensions() []string {
	exts := make([]string, 0, len(m.chunkers))
	for ext := range m.chunkers {
		exts = append(exts, ext)
	}
	return exts
}
