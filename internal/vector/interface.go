// Package vector provides common interfaces for vector storage implementations
package vector

import (
	"context"

	"github.com/lukaszraczylo/claude-mnemonic/internal/vector/sqlitevec"
)

// Client defines the interface for vector storage operations.
// Both sqlitevec.Client and hybrid.Client implement this interface.
type Client interface {
	// AddDocuments adds documents with their embeddings to the vector store
	AddDocuments(ctx context.Context, docs []sqlitevec.Document) error

	// DeleteDocuments removes documents by their IDs
	DeleteDocuments(ctx context.Context, ids []string) error

	// Query performs a vector similarity search
	Query(ctx context.Context, query string, limit int, where map[string]any) ([]sqlitevec.QueryResult, error)

	// IsConnected checks if the vector store is available
	IsConnected() bool

	// Close releases resources
	Close() error

	// Count returns the total number of vectors in the store
	Count(ctx context.Context) (int64, error)

	// ModelVersion returns the current embedding model version
	ModelVersion() string

	// NeedsRebuild checks if vectors need to be rebuilt due to model version change
	NeedsRebuild(ctx context.Context) (bool, string)

	// GetStaleVectors returns doc_ids of vectors with mismatched or null model versions
	GetStaleVectors(ctx context.Context) ([]sqlitevec.StaleVectorInfo, error)

	// DeleteVectorsByDocIDs removes vectors by their doc_ids
	DeleteVectorsByDocIDs(ctx context.Context, docIDs []string) error
}
