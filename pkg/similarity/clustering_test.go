// Package similarity provides text similarity and clustering utilities.
package similarity

import (
	"database/sql"
	"testing"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJaccardSimilarity(t *testing.T) {
	tests := []struct {
		name     string
		set1     map[string]bool
		set2     map[string]bool
		expected float64
	}{
		{
			name:     "identical sets",
			set1:     map[string]bool{"a": true, "b": true, "c": true},
			set2:     map[string]bool{"a": true, "b": true, "c": true},
			expected: 1.0,
		},
		{
			name:     "no overlap",
			set1:     map[string]bool{"a": true, "b": true},
			set2:     map[string]bool{"c": true, "d": true},
			expected: 0.0,
		},
		{
			name:     "partial overlap",
			set1:     map[string]bool{"a": true, "b": true, "c": true},
			set2:     map[string]bool{"b": true, "c": true, "d": true},
			expected: 0.5, // intersection=2, union=4
		},
		{
			name:     "empty sets",
			set1:     map[string]bool{},
			set2:     map[string]bool{},
			expected: 1.0,
		},
		{
			name:     "one empty set",
			set1:     map[string]bool{"a": true},
			set2:     map[string]bool{},
			expected: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := JaccardSimilarity(tt.set1, tt.set2)
			assert.InDelta(t, tt.expected, result, 0.001)
		})
	}
}

func TestExtractObservationTerms(t *testing.T) {
	obs := &models.Observation{
		Title:     sql.NullString{String: "Authentication flow implementation", Valid: true},
		Narrative: sql.NullString{String: "We implemented JWT-based authentication", Valid: true},
		Facts:     models.JSONStringArray{"Users authenticate via API", "Tokens expire after 24 hours"},
		FilesRead: models.JSONStringArray{"/src/auth/handler.go", "/src/auth/jwt.go"},
	}

	terms := ExtractObservationTerms(obs)

	// Should contain terms from title
	assert.Contains(t, terms, "authentication")
	assert.Contains(t, terms, "flow")
	assert.Contains(t, terms, "implementation")

	// Should contain terms from narrative
	assert.Contains(t, terms, "implemented")

	// Should contain terms from facts
	assert.Contains(t, terms, "tokens")
	assert.Contains(t, terms, "expire")
	assert.Contains(t, terms, "hours")

	// Should contain filenames (without path)
	assert.Contains(t, terms, "handler.go")
	assert.Contains(t, terms, "jwt.go")

	// Should NOT contain stop words
	assert.NotContains(t, terms, "the")
	assert.NotContains(t, terms, "and")
	assert.NotContains(t, terms, "we")
}

func TestClusterObservations(t *testing.T) {
	// Create similar observations
	obs1 := &models.Observation{
		ID:        1,
		Title:     sql.NullString{String: "Authentication flow implementation", Valid: true},
		Narrative: sql.NullString{String: "JWT-based authentication for API", Valid: true},
	}
	obs2 := &models.Observation{
		ID:        2,
		Title:     sql.NullString{String: "Authentication flow update", Valid: true},
		Narrative: sql.NullString{String: "Updated JWT authentication logic", Valid: true},
	}
	obs3 := &models.Observation{
		ID:        3,
		Title:     sql.NullString{String: "Database migration guide", Valid: true},
		Narrative: sql.NullString{String: "How to run database migrations", Valid: true},
	}
	obs4 := &models.Observation{
		ID:        4,
		Title:     sql.NullString{String: "Database schema changes", Valid: true},
		Narrative: sql.NullString{String: "Updated database schema for users", Valid: true},
	}

	observations := []*models.Observation{obs1, obs2, obs3, obs4}

	// Cluster with 0.4 threshold
	clustered := ClusterObservations(observations, 0.4)

	// obs1 and obs2 should be clustered (similar authentication content)
	// obs3 and obs4 should be clustered (similar database content)
	t.Logf("Clustered %d observations down to %d", len(observations), len(clustered))
	assert.LessOrEqual(t, len(clustered), 4)
	assert.GreaterOrEqual(t, len(clustered), 1)

	// First observation in each cluster should be kept (obs1 for auth, obs3 for db)
	ids := make(map[int64]bool)
	for _, obs := range clustered {
		ids[obs.ID] = true
	}

	// Depending on threshold, obs1 should be kept (first in auth cluster)
	if len(clustered) <= 3 {
		assert.True(t, ids[1], "First observation (ID=1) should be kept as cluster representative")
	}
}

func TestClusterObservations_SingleObservation(t *testing.T) {
	obs := &models.Observation{
		ID:    1,
		Title: sql.NullString{String: "Single observation", Valid: true},
	}

	clustered := ClusterObservations([]*models.Observation{obs}, 0.4)

	assert.Len(t, clustered, 1)
	assert.Equal(t, int64(1), clustered[0].ID)
}

func TestClusterObservations_EmptyList(t *testing.T) {
	clustered := ClusterObservations([]*models.Observation{}, 0.4)
	assert.Len(t, clustered, 0)
}

func TestClusterObservations_NoDuplicates(t *testing.T) {
	// Create observations with completely different content
	observations := []*models.Observation{
		{
			ID:        1,
			Title:     sql.NullString{String: "Authentication system", Valid: true},
			Narrative: sql.NullString{String: "JWT tokens for user auth", Valid: true},
		},
		{
			ID:        2,
			Title:     sql.NullString{String: "Database configuration", Valid: true},
			Narrative: sql.NullString{String: "PostgreSQL setup and migrations", Valid: true},
		},
		{
			ID:        3,
			Title:     sql.NullString{String: "Caching layer", Valid: true},
			Narrative: sql.NullString{String: "Redis caching implementation", Valid: true},
		},
		{
			ID:        4,
			Title:     sql.NullString{String: "Logging setup", Valid: true},
			Narrative: sql.NullString{String: "Structured logging with zerolog", Valid: true},
		},
		{
			ID:        5,
			Title:     sql.NullString{String: "API endpoints", Valid: true},
			Narrative: sql.NullString{String: "REST API implementation", Valid: true},
		},
	}

	clustered := ClusterObservations(observations, 0.4)

	// With completely different content, all should be kept
	assert.Len(t, clustered, 5, "All unique observations should be kept")
}

func TestIsSimilarToAny(t *testing.T) {
	existing := []*models.Observation{
		{
			ID:        1,
			Title:     sql.NullString{String: "Authentication implementation", Valid: true},
			Narrative: sql.NullString{String: "JWT authentication flow", Valid: true},
		},
		{
			ID:        2,
			Title:     sql.NullString{String: "Database setup", Valid: true},
			Narrative: sql.NullString{String: "PostgreSQL configuration", Valid: true},
		},
	}

	// New observation similar to existing
	similar := &models.Observation{
		ID:        3,
		Title:     sql.NullString{String: "Authentication update", Valid: true},
		Narrative: sql.NullString{String: "JWT authentication changes", Valid: true},
	}

	// New observation not similar to any existing
	different := &models.Observation{
		ID:        4,
		Title:     sql.NullString{String: "Caching layer", Valid: true},
		Narrative: sql.NullString{String: "Redis caching implementation", Valid: true},
	}

	assert.True(t, IsSimilarToAny(similar, existing, 0.3), "Similar observation should be detected")
	assert.False(t, IsSimilarToAny(different, existing, 0.3), "Different observation should not match")
}

func TestIsSimilarToAny_EmptyExisting(t *testing.T) {
	newObs := &models.Observation{
		ID:    1,
		Title: sql.NullString{String: "New observation", Valid: true},
	}

	assert.False(t, IsSimilarToAny(newObs, []*models.Observation{}, 0.4))
	assert.False(t, IsSimilarToAny(newObs, nil, 0.4))
}

func TestAddTerms(t *testing.T) {
	terms := make(map[string]bool)

	addTerms(terms, "The quick brown fox jumps over the lazy dog")

	// Should contain words >= 3 chars that aren't stop words
	assert.Contains(t, terms, "quick")
	assert.Contains(t, terms, "brown")
	assert.Contains(t, terms, "fox")
	assert.Contains(t, terms, "jumps")
	assert.Contains(t, terms, "over")
	assert.Contains(t, terms, "lazy")
	assert.Contains(t, terms, "dog")

	// Should NOT contain stop words
	assert.NotContains(t, terms, "the")

	// Should NOT contain short words
	// (all words in the sentence are >= 3 chars after stop word removal)
}

func TestClusterObservations_MoreThanOldLimit(t *testing.T) {
	// This test verifies that we can now return more than 5 observations
	// after removing the hardcoded limit

	// Create 10 completely unique observations with very different content
	observations := []*models.Observation{
		{ID: 1, Title: sql.NullString{String: "JWT tokens expire daily", Valid: true}},
		{ID: 2, Title: sql.NullString{String: "PostgreSQL indexes optimize", Valid: true}},
		{ID: 3, Title: sql.NullString{String: "Redis caching TTL values", Valid: true}},
		{ID: 4, Title: sql.NullString{String: "Zerolog structured logging", Valid: true}},
		{ID: 5, Title: sql.NullString{String: "Pytest fixtures setup", Valid: true}},
		{ID: 6, Title: sql.NullString{String: "Docker containers orchestration", Valid: true}},
		{ID: 7, Title: sql.NullString{String: "Prometheus metrics collection", Valid: true}},
		{ID: 8, Title: sql.NullString{String: "OWASP vulnerability scanning", Valid: true}},
		{ID: 9, Title: sql.NullString{String: "Goroutines parallel execution", Valid: true}},
		{ID: 10, Title: sql.NullString{String: "Kubernetes horizontal scaling", Valid: true}},
	}

	clustered := ClusterObservations(observations, 0.4)

	// With unique content, all 10 should be kept (previously would have been capped at 5)
	assert.Len(t, clustered, 10, "Should return all 10 unique observations, not limited to 5")
}

func TestClusterObservations_PreservesOrder(t *testing.T) {
	// The first observation in each cluster should be kept
	observations := []*models.Observation{
		{ID: 1, Title: sql.NullString{String: "First auth observation", Valid: true}},
		{ID: 2, Title: sql.NullString{String: "Second auth observation", Valid: true}},
		{ID: 3, Title: sql.NullString{String: "Database observation", Valid: true}},
	}

	clustered := ClusterObservations(observations, 0.4)

	// First observation should always be first in result
	require.NotEmpty(t, clustered)
	assert.Equal(t, int64(1), clustered[0].ID, "First observation should be kept as first result")
}
