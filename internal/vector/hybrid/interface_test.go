package hybrid

import (
	"testing"

	"github.com/lukaszraczylo/claude-mnemonic/internal/vector"
	_ "github.com/mattn/go-sqlite3" // Import SQLite driver for CGO linking
)

// TestInterfaceImplementation verifies that hybrid clients implement vector.Client interface
func TestInterfaceImplementation(t *testing.T) {
	// Compile-time check that Client implements vector.Client
	var _ vector.Client = (*Client)(nil)

	// Compile-time check that GraphSearchClient implements vector.Client
	var _ vector.Client = (*GraphSearchClient)(nil)
}
