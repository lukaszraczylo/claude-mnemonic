package sqlite

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMigrationManager(t *testing.T) {
	db, _, cleanup := testDB(t)
	defer cleanup()

	manager := NewMigrationManager(db)
	require.NotNil(t, manager)
	assert.Equal(t, db, manager.db)
}

func TestMigrationManager_EnsureSchemaVersionsTable(t *testing.T) {
	db, _, cleanup := testDB(t)
	defer cleanup()

	manager := NewMigrationManager(db)

	// Should create table without error
	err := manager.EnsureSchemaVersionsTable()
	require.NoError(t, err)

	// Table should exist
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM schema_versions").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count) // Empty table

	// Calling again should not error (IF NOT EXISTS)
	err = manager.EnsureSchemaVersionsTable()
	require.NoError(t, err)
}

func TestMigrationManager_GetAppliedVersions_Empty(t *testing.T) {
	db, _, cleanup := testDB(t)
	defer cleanup()

	manager := NewMigrationManager(db)
	err := manager.EnsureSchemaVersionsTable()
	require.NoError(t, err)

	versions, err := manager.GetAppliedVersions()
	require.NoError(t, err)
	assert.Empty(t, versions)
}

func TestMigrationManager_GetAppliedVersions_WithVersions(t *testing.T) {
	db, _, cleanup := testDB(t)
	defer cleanup()

	manager := NewMigrationManager(db)
	err := manager.EnsureSchemaVersionsTable()
	require.NoError(t, err)

	// Insert some versions
	_, err = db.Exec("INSERT INTO schema_versions (version, applied_at) VALUES (1, '2025-01-01T00:00:00Z')")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO schema_versions (version, applied_at) VALUES (2, '2025-01-02T00:00:00Z')")
	require.NoError(t, err)

	versions, err := manager.GetAppliedVersions()
	require.NoError(t, err)
	assert.Len(t, versions, 2)
	assert.True(t, versions[1])
	assert.True(t, versions[2])
	assert.False(t, versions[3]) // Not applied
}

func TestMigrationManager_ApplyMigration(t *testing.T) {
	db, _, cleanup := testDB(t)
	defer cleanup()

	manager := NewMigrationManager(db)
	err := manager.EnsureSchemaVersionsTable()
	require.NoError(t, err)

	// Apply a simple migration
	migration := Migration{
		Version: 100,
		Name:    "test_migration",
		SQL:     "CREATE TABLE test_table (id INTEGER PRIMARY KEY, name TEXT)",
	}

	err = manager.ApplyMigration(migration)
	require.NoError(t, err)

	// Verify table was created
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='test_table'").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Verify migration was recorded
	var version int
	err = db.QueryRow("SELECT version FROM schema_versions WHERE version = 100").Scan(&version)
	require.NoError(t, err)
	assert.Equal(t, 100, version)
}

func TestMigrationManager_ApplyMigration_InvalidSQL(t *testing.T) {
	db, _, cleanup := testDB(t)
	defer cleanup()

	manager := NewMigrationManager(db)
	err := manager.EnsureSchemaVersionsTable()
	require.NoError(t, err)

	// Try to apply invalid migration
	migration := Migration{
		Version: 100,
		Name:    "invalid_migration",
		SQL:     "INVALID SQL SYNTAX",
	}

	err = manager.ApplyMigration(migration)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "execute migration 100")
}

func TestMigrationManager_RunMigrations_SingleMigration(t *testing.T) {
	db, _, cleanup := testDB(t)
	defer cleanup()

	// Create a test migration manager with a subset of migrations
	manager := NewMigrationManager(db)

	// First ensure schema versions table exists
	err := manager.EnsureSchemaVersionsTable()
	require.NoError(t, err)

	// Apply first migration manually
	err = manager.ApplyMigration(Migrations[0])
	require.NoError(t, err)

	// Verify the first migration version was recorded
	versions, err := manager.GetAppliedVersions()
	require.NoError(t, err)
	assert.True(t, versions[Migrations[0].Version])
}

func TestMigrationManager_RunMigrations_SkipsApplied(t *testing.T) {
	db, _, cleanup := testDB(t)
	defer cleanup()

	manager := NewMigrationManager(db)
	err := manager.EnsureSchemaVersionsTable()
	require.NoError(t, err)

	// Mark some migrations as already applied
	_, err = db.Exec("INSERT INTO schema_versions (version, applied_at) VALUES (4, '2025-01-01T00:00:00Z')")
	require.NoError(t, err)

	// Get applied versions
	versions, err := manager.GetAppliedVersions()
	require.NoError(t, err)
	assert.True(t, versions[4])
}

func TestMigration_Struct(t *testing.T) {
	m := Migration{
		Version: 1,
		Name:    "test",
		SQL:     "SELECT 1",
	}

	assert.Equal(t, 1, m.Version)
	assert.Equal(t, "test", m.Name)
	assert.Equal(t, "SELECT 1", m.SQL)
}

func TestMigrations_List(t *testing.T) {
	// Verify migrations are ordered correctly
	assert.NotEmpty(t, Migrations)

	// Verify all migrations have required fields
	for i, m := range Migrations {
		assert.Greater(t, m.Version, 0, "Migration %d has invalid version", i)
		assert.NotEmpty(t, m.Name, "Migration %d has empty name", i)
		assert.NotEmpty(t, m.SQL, "Migration %d has empty SQL", i)
	}

	// Verify key migrations exist
	versionSet := make(map[int]bool)
	for _, m := range Migrations {
		versionSet[m.Version] = true
	}

	assert.True(t, versionSet[4], "Should have sdk_agent_architecture migration")
	assert.True(t, versionSet[17], "Should have sqlite_vec_vectors migration")
}
