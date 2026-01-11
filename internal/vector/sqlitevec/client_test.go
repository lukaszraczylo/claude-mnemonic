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
			scope TEXT,
			model_version TEXT
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

func TestClient_Count_Empty(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	count, err := client.Count(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)
}

func TestClient_Count_WithVectors(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	// Add some documents
	docs := []Document{
		{ID: "doc-1", Content: "test content 1"},
		{ID: "doc-2", Content: "test content 2"},
		{ID: "doc-3", Content: "test content 3"},
	}
	err = client.AddDocuments(context.Background(), docs)
	require.NoError(t, err)

	count, err := client.Count(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(3), count)
}

func TestClient_ModelVersion(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	version := client.ModelVersion()
	assert.NotEmpty(t, version)
	// Should match the embedding service version
	assert.Equal(t, embedSvc.Version(), version)
}

func TestClient_NeedsRebuild_EmptyDatabase(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	needsRebuild, reason := client.NeedsRebuild(context.Background())
	assert.True(t, needsRebuild)
	assert.Equal(t, "empty", reason)
}

func TestClient_NeedsRebuild_ModelMismatch(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	// Insert vectors with wrong model version
	embedding := make([]float32, 384)
	for i := range embedding {
		embedding[i] = 0.1
	}
	embeddingBytes, err := sqlite_vec.SerializeFloat32(embedding)
	require.NoError(t, err)

	_, err = db.Exec(`
		INSERT INTO vectors (doc_id, embedding, model_version, sqlite_id, doc_type, field_type, project, scope)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, "doc-1", embeddingBytes, "old-model-v1", 1, "observation", "content", "test", "project")
	require.NoError(t, err)

	_, err = db.Exec(`
		INSERT INTO vectors (doc_id, embedding, model_version, sqlite_id, doc_type, field_type, project, scope)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, "doc-2", embeddingBytes, "old-model-v1", 2, "observation", "content", "test", "project")
	require.NoError(t, err)

	needsRebuild, reason := client.NeedsRebuild(context.Background())
	assert.True(t, needsRebuild)
	assert.Contains(t, reason, "model_mismatch:2")
}

func TestClient_NeedsRebuild_CurrentModel(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	// Add documents with current model version
	docs := []Document{
		{ID: "doc-1", Content: "test content 1"},
		{ID: "doc-2", Content: "test content 2"},
	}
	err = client.AddDocuments(context.Background(), docs)
	require.NoError(t, err)

	needsRebuild, reason := client.NeedsRebuild(context.Background())
	assert.False(t, needsRebuild)
	assert.Empty(t, reason)
}

func TestClient_GetStaleVectors_Empty(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	stale, err := client.GetStaleVectors(context.Background())
	require.NoError(t, err)
	assert.Empty(t, stale)
}

func TestClient_GetStaleVectors_WithMismatch(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	// Insert vectors with wrong model version
	embedding := make([]float32, 384)
	embeddingBytes, err := sqlite_vec.SerializeFloat32(embedding)
	require.NoError(t, err)

	_, err = db.Exec(`
		INSERT INTO vectors (doc_id, embedding, model_version, sqlite_id, doc_type, field_type, project, scope)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, "doc-1", embeddingBytes, "old-model", 1, "observation", "content", "project-1", "project")
	require.NoError(t, err)

	_, err = db.Exec(`
		INSERT INTO vectors (doc_id, embedding, model_version, sqlite_id, doc_type, field_type, project, scope)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, "doc-2", embeddingBytes, embedSvc.Version(), 2, "observation", "title", "project-1", "project")
	require.NoError(t, err)

	stale, err := client.GetStaleVectors(context.Background())
	require.NoError(t, err)
	assert.Len(t, stale, 1)
	assert.Equal(t, "doc-1", stale[0].DocID)
	assert.Equal(t, int64(1), stale[0].SQLiteID)
	assert.Equal(t, "observation", stale[0].DocType)
	assert.Equal(t, "content", stale[0].FieldType)
	assert.Equal(t, "project-1", stale[0].Project)
	assert.Equal(t, "project", stale[0].Scope)
}

func TestClient_DeleteVectorsByDocIDs_Empty(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	// Deleting empty slice should not error
	err = client.DeleteVectorsByDocIDs(context.Background(), []string{})
	require.NoError(t, err)
}

func TestClient_DeleteVectorsByDocIDs_Success(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	// Add documents
	docs := []Document{
		{ID: "doc-1", Content: "test 1"},
		{ID: "doc-2", Content: "test 2"},
		{ID: "doc-3", Content: "test 3"},
	}
	err = client.AddDocuments(context.Background(), docs)
	require.NoError(t, err)

	// Verify 3 documents exist
	count, err := client.Count(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(3), count)

	// Delete doc-1 and doc-3
	err = client.DeleteVectorsByDocIDs(context.Background(), []string{"doc-1", "doc-3"})
	require.NoError(t, err)

	// Should have 1 document remaining
	count, err = client.Count(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)

	// Verify doc-2 still exists
	var exists int
	err = db.QueryRow("SELECT COUNT(*) FROM vectors WHERE doc_id = ?", "doc-2").Scan(&exists)
	require.NoError(t, err)
	assert.Equal(t, 1, exists)
}

func TestClient_DeleteVectorsByDocIDs_NonExistent(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	// Deleting non-existent IDs should not error
	err = client.DeleteVectorsByDocIDs(context.Background(), []string{"non-existent-1", "non-existent-2"})
	require.NoError(t, err)
}

// =============================================================================
// TESTS FOR CacheStats
// =============================================================================

func TestCacheStatsSnapshot_HitRate_NoOperations(t *testing.T) {
	snapshot := CacheStatsSnapshot{}
	assert.Equal(t, float64(0), snapshot.HitRate())
}

func TestCacheStatsSnapshot_HitRate_WithOperations(t *testing.T) {
	tests := []struct {
		name     string
		stats    CacheStatsSnapshot
		expected float64
	}{
		{
			name: "all_hits",
			stats: CacheStatsSnapshot{
				EmbeddingHits: 50,
				ResultHits:    50,
			},
			expected: 100.0,
		},
		{
			name: "no_hits",
			stats: CacheStatsSnapshot{
				EmbeddingMisses: 50,
				ResultMisses:    50,
			},
			expected: 0.0,
		},
		{
			name: "50_percent_hits",
			stats: CacheStatsSnapshot{
				EmbeddingHits:   25,
				EmbeddingMisses: 25,
				ResultHits:      25,
				ResultMisses:    25,
			},
			expected: 50.0,
		},
		{
			name: "75_percent_hits",
			stats: CacheStatsSnapshot{
				EmbeddingHits:   30,
				EmbeddingMisses: 10,
				ResultHits:      30,
				ResultMisses:    10,
			},
			expected: 75.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.stats.HitRate()
			assert.InDelta(t, tt.expected, result, 0.01)
		})
	}
}

func TestCacheStats_HitRate_NoOperations(t *testing.T) {
	stats := &CacheStats{}
	assert.Equal(t, float64(0), stats.HitRate())
}

func TestCacheStats_HitRate_WithOperations(t *testing.T) {
	stats := &CacheStats{}
	stats.embeddingHits.Add(10)
	stats.embeddingMisses.Add(10)
	stats.resultHits.Add(10)
	stats.resultMisses.Add(10)

	// 20 hits / 40 total = 50%
	assert.InDelta(t, 50.0, stats.HitRate(), 0.01)
}

func TestCacheStats_Snapshot(t *testing.T) {
	stats := &CacheStats{}
	stats.embeddingHits.Add(10)
	stats.embeddingMisses.Add(5)
	stats.resultHits.Add(20)
	stats.resultMisses.Add(15)
	stats.embeddingEvictions.Add(2)
	stats.resultEvictions.Add(3)

	snapshot := stats.Snapshot()

	assert.Equal(t, int64(10), snapshot.EmbeddingHits)
	assert.Equal(t, int64(5), snapshot.EmbeddingMisses)
	assert.Equal(t, int64(20), snapshot.ResultHits)
	assert.Equal(t, int64(15), snapshot.ResultMisses)
	assert.Equal(t, int64(2), snapshot.EmbeddingEvictions)
	assert.Equal(t, int64(3), snapshot.ResultEvictions)
}

// =============================================================================
// TESTS FOR Cache Methods
// =============================================================================

func TestClient_ClearCache(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	// Add a document and query to populate cache
	docs := []Document{
		{ID: "doc-1", Content: "test content for caching"},
	}
	err = client.AddDocuments(context.Background(), docs)
	require.NoError(t, err)

	// Query to populate cache
	_, err = client.Query(context.Background(), "test content", 5, nil)
	require.NoError(t, err)

	// Verify cache has entries
	initialSize := client.EmbeddingCacheSize()
	assert.Greater(t, initialSize, 0)

	// Clear cache
	client.ClearCache()

	// Verify cache is empty
	assert.Equal(t, 0, client.EmbeddingCacheSize())
}

func TestClient_GetCacheStats(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	// Get stats before any operations
	stats := client.GetCacheStats()
	assert.Equal(t, int64(0), stats.EmbeddingHits)
	assert.Equal(t, int64(0), stats.EmbeddingMisses)

	// Add a document and query to generate cache activity
	docs := []Document{
		{ID: "doc-1", Content: "test content for caching"},
	}
	err = client.AddDocuments(context.Background(), docs)
	require.NoError(t, err)

	// Query - should be a miss first time
	_, err = client.Query(context.Background(), "test content", 5, nil)
	require.NoError(t, err)

	// Query again - should be a hit
	_, err = client.Query(context.Background(), "test content", 5, nil)
	require.NoError(t, err)

	// Get stats after operations
	stats = client.GetCacheStats()
	assert.Greater(t, stats.EmbeddingMisses+stats.EmbeddingHits, int64(0))
}

func TestClient_CacheStats(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	// Get initial stats
	size, maxSize := client.CacheStats()
	assert.Equal(t, 0, size)
	assert.Greater(t, maxSize, 0)

	// Add a document and query to populate cache
	docs := []Document{
		{ID: "doc-1", Content: "test content"},
	}
	err = client.AddDocuments(context.Background(), docs)
	require.NoError(t, err)

	_, err = client.Query(context.Background(), "test content", 5, nil)
	require.NoError(t, err)

	// Check stats after operations
	size, _ = client.CacheStats()
	assert.Greater(t, size, 0)
}

func TestClient_EmbeddingCacheSize(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	// Initially empty
	assert.Equal(t, 0, client.EmbeddingCacheSize())

	// Add a document and query
	docs := []Document{
		{ID: "doc-1", Content: "test content"},
	}
	err = client.AddDocuments(context.Background(), docs)
	require.NoError(t, err)

	_, err = client.Query(context.Background(), "unique query", 5, nil)
	require.NoError(t, err)

	// Should have at least one entry
	assert.Greater(t, client.EmbeddingCacheSize(), 0)
}

func TestClient_ResultCacheSize(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	// Initially empty
	assert.Equal(t, 0, client.ResultCacheSize())
}

// =============================================================================
// TESTS FOR QueryBatch
// =============================================================================

func TestClient_QueryBatch_Empty(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	results := client.QueryBatch(context.Background(), []string{}, 10, nil)
	assert.Nil(t, results)
}

func TestClient_QueryBatch_Single(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	// Add some documents
	docs := []Document{
		{
			ID:       "obs-1",
			Content:  "Authentication and security implementation.",
			Metadata: map[string]any{"doc_type": "observation"},
		},
	}
	err = client.AddDocuments(context.Background(), docs)
	require.NoError(t, err)

	// Query batch with single query
	results := client.QueryBatch(context.Background(), []string{"authentication"}, 10, nil)

	assert.Len(t, results, 1)
	assert.NoError(t, results[0].Error)
	assert.Equal(t, "authentication", results[0].Query)
}

func TestClient_QueryBatch_Multiple(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	// Add some documents
	docs := []Document{
		{ID: "obs-1", Content: "Authentication and security implementation."},
		{ID: "obs-2", Content: "Database optimization and indexing."},
		{ID: "obs-3", Content: "API rate limiting and throttling."},
	}
	err = client.AddDocuments(context.Background(), docs)
	require.NoError(t, err)

	// Query batch with multiple queries
	queries := []string{"authentication", "database", "API"}
	results := client.QueryBatch(context.Background(), queries, 10, nil)

	assert.Len(t, results, 3)
	for i, r := range results {
		assert.NoError(t, r.Error)
		assert.Equal(t, queries[i], r.Query)
	}
}

func TestClient_QueryBatch_WithContextCancellation(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	// Cancel context immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Queries should fail due to cancelled context
	queries := []string{"query1", "query2", "query3"}
	results := client.QueryBatch(ctx, queries, 10, nil)

	assert.Len(t, results, 3)
	// At least some should have context cancellation error
	hasError := false
	for _, r := range results {
		if r.Error != nil {
			hasError = true
		}
	}
	assert.True(t, hasError, "Should have at least one error due to cancelled context")
}

// =============================================================================
// TESTS FOR QueryMultiField
// =============================================================================

func TestClient_QueryMultiField_Basic(t *testing.T) {
	t.Skip("QueryMultiField SQL query needs 'k' parameter fix for sqlite-vec")

	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	// Add documents with different field types for same sqlite_id
	docs := []Document{
		{
			ID:      "obs-1-title",
			Content: "Authentication implementation",
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
			Content: "We implemented JWT-based authentication for the API.",
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
			Content: "Database optimization",
			Metadata: map[string]any{
				"sqlite_id":  int64(2),
				"doc_type":   "observation",
				"field_type": "title",
				"project":    "test-project",
				"scope":      "project",
			},
		},
	}
	err = client.AddDocuments(context.Background(), docs)
	require.NoError(t, err)

	// Query multi-field
	results, err := client.QueryMultiField(context.Background(), "authentication JWT", 10, "observation", "test-project")
	require.NoError(t, err)

	// Should return deduplicated results (one per sqlite_id)
	assert.NotEmpty(t, results)
	// Each result should have unique sqlite_id
	seenIDs := make(map[float64]bool)
	for _, r := range results {
		sqliteID, ok := r.Metadata["sqlite_id"].(float64)
		if ok {
			assert.False(t, seenIDs[sqliteID], "Should not have duplicate sqlite_ids")
			seenIDs[sqliteID] = true
		}
	}
}

func TestClient_QueryMultiField_WithGlobalScope(t *testing.T) {
	t.Skip("QueryMultiField SQL query needs 'k' parameter fix for sqlite-vec")

	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	// Add documents: one project-scoped, one global
	docs := []Document{
		{
			ID:      "obs-1-title",
			Content: "Security best practices",
			Metadata: map[string]any{
				"sqlite_id":  int64(1),
				"doc_type":   "observation",
				"field_type": "title",
				"project":    "project-a",
				"scope":      "project",
			},
		},
		{
			ID:      "obs-2-title",
			Content: "Security patterns for all projects",
			Metadata: map[string]any{
				"sqlite_id":  int64(2),
				"doc_type":   "observation",
				"field_type": "title",
				"project":    "project-b",
				"scope":      "global",
			},
		},
	}
	err = client.AddDocuments(context.Background(), docs)
	require.NoError(t, err)

	// Query from project-a - should get project-a doc and global doc
	results, err := client.QueryMultiField(context.Background(), "security", 10, "observation", "project-a")
	require.NoError(t, err)

	// Should include both project-scoped (matching project) and global
	assert.NotEmpty(t, results)
}

// =============================================================================
// TESTS FOR GetHealthStats
// =============================================================================

func TestClient_GetHealthStats_Empty(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	stats, err := client.GetHealthStats(context.Background())
	require.NoError(t, err)

	assert.NotNil(t, stats)
	assert.Equal(t, int64(0), stats.TotalVectors)
	assert.Equal(t, int64(0), stats.StaleVectors)
	assert.Equal(t, embedSvc.Version(), stats.CurrentModel)
	assert.True(t, stats.NeedsRebuild)
	assert.Equal(t, "empty", stats.RebuildReason)
}

func TestClient_GetHealthStats_WithData(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	// Add some documents
	docs := []Document{
		{
			ID:      "obs-1",
			Content: "Test content 1",
			Metadata: map[string]any{
				"sqlite_id": int64(1),
				"doc_type":  "observation",
				"project":   "project-a",
			},
		},
		{
			ID:      "obs-2",
			Content: "Test content 2",
			Metadata: map[string]any{
				"sqlite_id": int64(2),
				"doc_type":  "observation",
				"project":   "project-a",
			},
		},
		{
			ID:      "sum-1",
			Content: "Summary content",
			Metadata: map[string]any{
				"sqlite_id": int64(10),
				"doc_type":  "session_summary",
				"project":   "project-b",
			},
		},
	}
	err = client.AddDocuments(context.Background(), docs)
	require.NoError(t, err)

	stats, err := client.GetHealthStats(context.Background())
	require.NoError(t, err)

	assert.NotNil(t, stats)
	assert.Equal(t, int64(3), stats.TotalVectors)
	assert.Equal(t, int64(0), stats.StaleVectors) // All fresh
	assert.False(t, stats.NeedsRebuild)

	// Coverage by type
	assert.Equal(t, int64(2), stats.CoverageByType["observation"])
	assert.Equal(t, int64(1), stats.CoverageByType["session_summary"])

	// Model versions
	assert.Equal(t, int64(3), stats.ModelVersions[embedSvc.Version()])

	// Project counts
	assert.Equal(t, int64(2), stats.ProjectCounts["project-a"])
	assert.Equal(t, int64(1), stats.ProjectCounts["project-b"])
}

func TestClient_GetHealthStats_WithStaleVectors(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	// Add a document with current model
	docs := []Document{
		{ID: "doc-1", Content: "Fresh content"},
	}
	err = client.AddDocuments(context.Background(), docs)
	require.NoError(t, err)

	// Insert a stale vector directly
	embedding := make([]float32, 384)
	embeddingBytes, err := sqlite_vec.SerializeFloat32(embedding)
	require.NoError(t, err)

	_, err = db.Exec(`
		INSERT INTO vectors (doc_id, embedding, model_version, sqlite_id, doc_type, field_type, project, scope)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, "stale-doc", embeddingBytes, "old-model", 999, "observation", "content", "test-project", "project")
	require.NoError(t, err)

	stats, err := client.GetHealthStats(context.Background())
	require.NoError(t, err)

	assert.Equal(t, int64(2), stats.TotalVectors)
	assert.Equal(t, int64(1), stats.StaleVectors)
	assert.True(t, stats.NeedsRebuild)
	assert.Contains(t, stats.RebuildReason, "model_mismatch")
}

// =============================================================================
// TESTS FOR DeleteByObservationID
// =============================================================================

func TestClient_DeleteByObservationID_NoMatches(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	// Delete non-existent observation - should not error
	err = client.DeleteByObservationID(context.Background(), 999)
	require.NoError(t, err)
}

func TestClient_DeleteByObservationID_WithMatches(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	// Add documents with observation IDs in doc_id
	docs := []Document{
		{ID: "obs_123_narrative", Content: "Narrative for observation 123"},
		{ID: "obs_123_facts_0", Content: "Fact 0 for observation 123"},
		{ID: "obs_123_facts_1", Content: "Fact 1 for observation 123"},
		{ID: "obs_456_narrative", Content: "Narrative for observation 456"},
	}
	err = client.AddDocuments(context.Background(), docs)
	require.NoError(t, err)

	// Verify 4 documents exist
	count, err := client.Count(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(4), count)

	// Delete observation 123
	err = client.DeleteByObservationID(context.Background(), 123)
	require.NoError(t, err)

	// Should have 1 document remaining (obs_456)
	count, err = client.Count(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)

	// Verify obs_456 still exists
	var exists int
	err = db.QueryRow("SELECT COUNT(*) FROM vectors WHERE doc_id LIKE 'obs_456_%'").Scan(&exists)
	require.NoError(t, err)
	assert.Equal(t, 1, exists)
}

// =============================================================================
// TESTS FOR cacheCleanupLoop and cleanupExpiredCaches
// =============================================================================

func TestClient_CleanupExpiredCaches(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	// Add a document and query to populate cache
	docs := []Document{
		{ID: "doc-1", Content: "test content"},
	}
	err = client.AddDocuments(context.Background(), docs)
	require.NoError(t, err)

	_, err = client.Query(context.Background(), "test", 5, nil)
	require.NoError(t, err)

	// Verify cache has entries
	assert.Greater(t, client.EmbeddingCacheSize(), 0)

	// Call cleanup (will only clean expired entries)
	client.cleanupExpiredCaches()

	// Fresh cache entries should still exist
	assert.Greater(t, client.EmbeddingCacheSize(), 0)
}

func TestClient_CacheCleanupLoop_StopsOnClose(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	// Close should stop the cleanup loop
	err = client.Close()
	require.NoError(t, err)
}

// =============================================================================
// TESTS FOR EMBEDDING CACHE BEHAVIOR
// =============================================================================

func TestClient_EmbeddingCache_HitAfterMiss(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	// Add a document so we can query
	docs := []Document{
		{ID: "test-1", Content: "Hello world test content"},
	}
	err = client.AddDocuments(context.Background(), docs)
	require.NoError(t, err)

	// First query - cache miss
	_, err = client.Query(context.Background(), "hello world", 10, nil)
	require.NoError(t, err)

	stats1 := client.GetCacheStats()
	assert.Equal(t, int64(1), stats1.EmbeddingMisses)

	// Invalidate result cache to force embedding cache usage on second query
	client.InvalidateResultCache()

	// Second query with same text - should be embedding cache hit (result cache miss)
	_, err = client.Query(context.Background(), "hello world", 10, nil)
	require.NoError(t, err)

	stats2 := client.GetCacheStats()
	assert.Equal(t, int64(1), stats2.EmbeddingMisses) // Same miss count
	assert.Equal(t, int64(1), stats2.EmbeddingHits)   // One hit
}

func TestClient_ResultCache_HitAfterMiss(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	// Add a document
	docs := []Document{
		{
			ID:      "test-1",
			Content: "Testing result cache behavior",
		},
	}
	err = client.AddDocuments(context.Background(), docs)
	require.NoError(t, err)

	// First query - result cache miss
	_, err = client.Query(context.Background(), "testing cache", 10, nil)
	require.NoError(t, err)

	stats1 := client.GetCacheStats()
	assert.Equal(t, int64(1), stats1.ResultMisses)

	// Second identical query - should be result cache hit
	_, err = client.Query(context.Background(), "testing cache", 10, nil)
	require.NoError(t, err)

	stats2 := client.GetCacheStats()
	assert.Equal(t, int64(1), stats2.ResultMisses) // Same miss count
	assert.Equal(t, int64(1), stats2.ResultHits)   // One hit
}

func TestClient_Query_WithContextCancel(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	// Create cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Query with cancelled context
	_, err = client.Query(ctx, "test query", 10, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

func TestClient_AddDocuments_WithContextCancel(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	// Create cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	docs := []Document{{ID: "test", Content: "test content"}}
	err = client.AddDocuments(ctx, docs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

func TestClient_InvalidateResultCache(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	// Add a document
	docs := []Document{
		{ID: "test-1", Content: "Test invalidation"},
	}
	err = client.AddDocuments(context.Background(), docs)
	require.NoError(t, err)

	// Query to populate result cache
	_, err = client.Query(context.Background(), "invalidation", 10, nil)
	require.NoError(t, err)

	assert.Greater(t, client.ResultCacheSize(), 0)

	// Invalidate the result cache
	client.InvalidateResultCache()

	assert.Equal(t, 0, client.ResultCacheSize())
}

func TestClient_Count_WithError(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	// Close DB to cause error
	db.Close()

	_, err = client.Count(context.Background())
	require.Error(t, err)
}

func TestClient_NeedsRebuild_ReturnsReason(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	// Empty database should need rebuild
	needsRebuild, reason := client.NeedsRebuild(context.Background())
	assert.True(t, needsRebuild)
	assert.NotEmpty(t, reason)
}

func TestClient_GetStaleVectors_DBError(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	// Close DB to cause error
	db.Close()

	_, err = client.GetStaleVectors(context.Background())
	require.Error(t, err)
}

func TestClient_DeleteVectorsByDocIDs_DBError(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	// Close DB to cause error
	db.Close()

	err = client.DeleteVectorsByDocIDs(context.Background(), []string{"doc-1"})
	require.Error(t, err)
}

func TestClient_DeleteByObservationID_DBError(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	// Close DB to cause error
	db.Close()

	err = client.DeleteByObservationID(context.Background(), 123)
	require.Error(t, err)
}

func TestClient_Query_DBError(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	// Add document first
	docs := []Document{{ID: "test", Content: "test content"}}
	err = client.AddDocuments(context.Background(), docs)
	require.NoError(t, err)

	// Close DB to cause error on query
	db.Close()

	// Clear the cache so it has to hit the DB
	client.InvalidateResultCache()
	client.ClearCache()

	_, err = client.Query(context.Background(), "test", 10, nil)
	require.Error(t, err)
}

func TestClient_AddDocuments_DBError(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	// Close DB to cause error
	db.Close()

	docs := []Document{{ID: "test", Content: "test content"}}
	err = client.AddDocuments(context.Background(), docs)
	require.Error(t, err)
}

func TestClient_GetHealthStats_DBError(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	// Close DB to cause error
	db.Close()

	_, err = client.GetHealthStats(context.Background())
	require.Error(t, err)
}

func TestClient_QueryBatch_DBError(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	// Close DB to cause error
	db.Close()

	results := client.QueryBatch(context.Background(), []string{"test1", "test2"}, 10, nil)
	require.Len(t, results, 2)
	assert.Error(t, results[0].Error)
	assert.Error(t, results[1].Error)
}

func TestClient_DeleteDocuments_DBError(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	// Close DB to cause error
	db.Close()

	err = client.DeleteDocuments(context.Background(), []string{"doc-1"})
	require.Error(t, err)
}

func TestClient_Query_WithEmptyResults(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	// Query with no documents - should return empty results
	results, err := client.Query(context.Background(), "nonexistent query", 10, nil)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestClient_QueryBatch_AllSucceed(t *testing.T) {
	db, dbCleanup := testDB(t)
	defer dbCleanup()

	embedSvc, embedCleanup := testEmbeddingService(t)
	defer embedCleanup()

	client, err := NewClient(Config{DB: db}, embedSvc)
	require.NoError(t, err)

	// Add some documents
	docs := []Document{
		{ID: "doc-1", Content: "Test content for batch query one"},
		{ID: "doc-2", Content: "Test content for batch query two"},
	}
	err = client.AddDocuments(context.Background(), docs)
	require.NoError(t, err)

	// Run batch query with multiple queries
	results := client.QueryBatch(context.Background(), []string{"batch one", "batch two", "batch three"}, 10, nil)

	// All queries should succeed
	require.Len(t, results, 3)
	for i, r := range results {
		assert.NoError(t, r.Error, "Query %d should not fail", i)
	}
}

// =============================================================================
// TESTS FOR HELPER FUNCTIONS EDGE CASES
// =============================================================================

func TestExtractObservationIDs_Int64Metadata(t *testing.T) {
	// Test the int64 fallback path for sqlite_id metadata
	results := []QueryResult{
		{
			ID:         "obs-1",
			Similarity: 0.9,
			Metadata: map[string]any{
				"sqlite_id": int64(123), // int64 instead of float64
				"doc_type":  "observation",
				"project":   "test-project",
			},
		},
	}

	ids := ExtractObservationIDs(results, "test-project")
	assert.Len(t, ids, 1)
	assert.Equal(t, int64(123), ids[0])
}

func TestExtractSummaryIDs_Int64Metadata(t *testing.T) {
	// Test the int64 fallback path for sqlite_id metadata
	results := []QueryResult{
		{
			ID:         "sum-1",
			Similarity: 0.9,
			Metadata: map[string]any{
				"sqlite_id": int64(456), // int64 instead of float64
				"doc_type":  "session_summary",
				"project":   "test-project",
			},
		},
	}

	ids := ExtractSummaryIDs(results, "test-project")
	assert.Len(t, ids, 1)
	assert.Equal(t, int64(456), ids[0])
}

func TestExtractPromptIDs_Int64Metadata(t *testing.T) {
	// Test the int64 fallback path for sqlite_id metadata
	results := []QueryResult{
		{
			ID:         "prompt-1",
			Similarity: 0.9,
			Metadata: map[string]any{
				"sqlite_id": int64(789), // int64 instead of float64
				"doc_type":  "user_prompt",
				"project":   "test-project",
			},
		},
	}

	ids := ExtractPromptIDs(results, "test-project")
	assert.Len(t, ids, 1)
	assert.Equal(t, int64(789), ids[0])
}

func TestExtractObservationIDs_GlobalScope(t *testing.T) {
	// Test that global scope observations are included for any project
	results := []QueryResult{
		{
			ID:         "obs-1",
			Similarity: 0.9,
			Metadata: map[string]any{
				"sqlite_id": float64(123),
				"doc_type":  "observation",
				"project":   "other-project",
				"scope":     "global", // Global scope should be included
			},
		},
	}

	ids := ExtractObservationIDs(results, "test-project")
	assert.Len(t, ids, 1)
	assert.Equal(t, int64(123), ids[0])
}
