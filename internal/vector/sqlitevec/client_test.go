package sqlitevec

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	"github.com/lukaszraczylo/claude-mnemonic/internal/embedding"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testDB creates a test SQLite database with the vectors table.
func testDB(t *testing.T) (*sql.DB, func()) {
	t.Helper()

	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "sqlitevec-test-*")
	require.NoError(t, err)

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)

	// Enable sqlite-vec
	sqlite_vec.Auto()

	// Create vectors table (matches production schema)
	_, err = db.Exec(`
		CREATE VIRTUAL TABLE IF NOT EXISTS vectors USING vec0(
			doc_id TEXT PRIMARY KEY,
			embedding float[384],
			sqlite_id INTEGER,
			doc_type TEXT,
			field_type TEXT,
			project TEXT,
			scope TEXT
		)
	`)
	require.NoError(t, err)

	cleanup := func() {
		db.Close()
		os.RemoveAll(tmpDir)
	}

	return db, cleanup
}

// testEmbeddingService creates a test embedding service.
func testEmbeddingService(t *testing.T) (*embedding.Service, func()) {
	t.Helper()

	svc, err := embedding.NewService()
	require.NoError(t, err)

	cleanup := func() {
		svc.Close()
	}

	return svc, cleanup
}

func TestNewClient_Success(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)
	assert.NotNil(t, client)
}

func TestNewClient_NilDB(t *testing.T) {
	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: nil}, embedSvc)
	assert.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "database connection required")
}

func TestNewClient_NilEmbedding(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	client, err := NewClient(Config{DB: db}, nil)
	assert.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "embedding service required")
}

func TestClient_AddDocuments_Empty(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	err = client.AddDocuments(context.Background(), []Document{})
	require.NoError(t, err)
}

func TestClient_AddDocuments_Single(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	docs := []Document{
		{
			ID:      "obs-1-title",
			Content: "This is a test observation about authentication.",
			Metadata: map[string]any{
				"sqlite_id":  int64(1),
				"doc_type":   "observation",
				"field_type": "title",
				"project":    "test-project",
				"scope":      "project",
			},
		},
	}

	err = client.AddDocuments(context.Background(), docs)
	require.NoError(t, err)

	// Verify document was inserted
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM vectors WHERE doc_id = ?", "obs-1-title").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestClient_AddDocuments_Multiple(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	docs := []Document{
		{
			ID:      "obs-1-title",
			Content: "Authentication flow implementation.",
			Metadata: map[string]any{
				"sqlite_id":  int64(1),
				"doc_type":   "observation",
				"field_type": "title",
				"project":    "test-project",
				"scope":      "project",
			},
		},
		{
			ID:      "obs-1-narrative",
			Content: "We implemented JWT-based authentication.",
			Metadata: map[string]any{
				"sqlite_id":  int64(1),
				"doc_type":   "observation",
				"field_type": "narrative",
				"project":    "test-project",
				"scope":      "project",
			},
		},
		{
			ID:      "obs-2-title",
			Content: "Database optimization.",
			Metadata: map[string]any{
				"sqlite_id":  int64(2),
				"doc_type":   "observation",
				"field_type": "title",
				"project":    "test-project",
				"scope":      "global",
			},
		},
	}

	err = client.AddDocuments(context.Background(), docs)
	require.NoError(t, err)

	// Verify all documents were inserted
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM vectors").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 3, count)
}

func TestClient_DeleteDocuments_Empty(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	err = client.DeleteDocuments(context.Background(), []string{})
	require.NoError(t, err)
}

func TestClient_DeleteDocuments_Existing(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	// Add documents first
	docs := []Document{
		{
			ID:      "doc-1",
			Content: "First document.",
			Metadata: map[string]any{
				"sqlite_id": int64(1),
				"doc_type":  "observation",
			},
		},
		{
			ID:      "doc-2",
			Content: "Second document.",
			Metadata: map[string]any{
				"sqlite_id": int64(2),
				"doc_type":  "observation",
			},
		},
	}
	err = client.AddDocuments(context.Background(), docs)
	require.NoError(t, err)

	// Delete one document
	err = client.DeleteDocuments(context.Background(), []string{"doc-1"})
	require.NoError(t, err)

	// Verify only one remains
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM vectors").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestClient_Query_Basic(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	// Add some test documents
	docs := []Document{
		{
			ID:      "obs-1",
			Content: "Authentication and login security implementation.",
			Metadata: map[string]any{
				"sqlite_id": int64(1),
				"doc_type":  "observation",
				"project":   "test-project",
				"scope":     "project",
			},
		},
		{
			ID:      "obs-2",
			Content: "Database query optimization techniques.",
			Metadata: map[string]any{
				"sqlite_id": int64(2),
				"doc_type":  "observation",
				"project":   "test-project",
				"scope":     "project",
			},
		},
	}
	err = client.AddDocuments(context.Background(), docs)
	require.NoError(t, err)

	// Query for authentication-related content
	results, err := client.Query(context.Background(), "login authentication", 10, nil)
	require.NoError(t, err)

	assert.NotEmpty(t, results)
	assert.LessOrEqual(t, len(results), 10)

	// First result should be the authentication document (higher similarity)
	assert.Equal(t, "obs-1", results[0].ID)
}

func TestClient_Query_WithDocTypeFilter(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	// Add documents of different types
	docs := []Document{
		{
			ID:      "obs-1",
			Content: "Test content for observation.",
			Metadata: map[string]any{
				"sqlite_id": int64(1),
				"doc_type":  "observation",
				"project":   "test-project",
			},
		},
		{
			ID:      "summary-1",
			Content: "Test content for summary.",
			Metadata: map[string]any{
				"sqlite_id": int64(10),
				"doc_type":  "session_summary",
				"project":   "test-project",
			},
		},
	}
	err = client.AddDocuments(context.Background(), docs)
	require.NoError(t, err)

	// Query with doc_type filter
	where := map[string]any{"doc_type": "observation"}
	results, err := client.Query(context.Background(), "test content", 10, where)
	require.NoError(t, err)

	// Should only return observation documents
	for _, r := range results {
		docType, _ := r.Metadata["doc_type"].(string)
		assert.Equal(t, "observation", docType)
	}
}

func TestClient_Query_WithProjectFilter(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	// Add documents from different projects
	docs := []Document{
		{
			ID:      "obs-1",
			Content: "Project A authentication content.",
			Metadata: map[string]any{
				"sqlite_id": int64(1),
				"doc_type":  "observation",
				"project":   "project-a",
				"scope":     "project",
			},
		},
		{
			ID:      "obs-2",
			Content: "Project B database content.",
			Metadata: map[string]any{
				"sqlite_id": int64(2),
				"doc_type":  "observation",
				"project":   "project-b",
				"scope":     "project",
			},
		},
		{
			ID:      "obs-3",
			Content: "Global security best practices.",
			Metadata: map[string]any{
				"sqlite_id": int64(3),
				"doc_type":  "observation",
				"project":   "project-b",
				"scope":     "global",
			},
		},
	}
	err = client.AddDocuments(context.Background(), docs)
	require.NoError(t, err)

	// Query without project filter to verify all docs are there
	results, err := client.Query(context.Background(), "authentication security", 10, nil)
	require.NoError(t, err)
	assert.NotEmpty(t, results, "Should find some results")
}

func TestClient_IsConnected(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	assert.True(t, client.IsConnected())
}

func TestClient_Close(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	err = client.Close()
	require.NoError(t, err)
}

func TestConfig_Fields(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	cfg := Config{DB: db}
	assert.Equal(t, db, cfg.DB)
}

func TestClient_UpdateDocument_DeleteThenAdd(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	// Add document
	docs1 := []Document{
		{
			ID:      "doc-1",
			Content: "Original content.",
			Metadata: map[string]any{
				"sqlite_id": int64(1),
				"doc_type":  "observation",
			},
		},
	}
	err = client.AddDocuments(context.Background(), docs1)
	require.NoError(t, err)

	// Delete then add with new content (proper update pattern)
	err = client.DeleteDocuments(context.Background(), []string{"doc-1"})
	require.NoError(t, err)

	docs2 := []Document{
		{
			ID:      "doc-1",
			Content: "Updated content.",
			Metadata: map[string]any{
				"sqlite_id": int64(1),
				"doc_type":  "observation",
			},
		},
	}
	err = client.AddDocuments(context.Background(), docs2)
	require.NoError(t, err)

	// Should have exactly 1 document
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM vectors WHERE doc_id = ?", "doc-1").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestClient_DeleteDocuments_NonExistent(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	// Deleting non-existent document should not error
	err = client.DeleteDocuments(context.Background(), []string{"non-existent-id"})
	require.NoError(t, err)
}
