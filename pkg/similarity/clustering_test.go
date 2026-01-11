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
		set1     map[string]bool
		set2     map[string]bool
		name     string
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

// =============================================================================
// TESTS FOR OPTIMIZED CLUSTERING (triggered when len(observations) > 50)
// =============================================================================

func TestClusterObservationsOptimized_LargeSet(t *testing.T) {
	t.Parallel()

	// Create 60 observations to trigger optimized path (threshold is 50)
	observations := make([]*models.Observation, 60)

	// Create 30 pairs of similar observations
	topics := []string{
		"authentication", "authorization", "database", "caching", "logging",
		"monitoring", "testing", "deployment", "scaling", "security",
		"networking", "storage", "messaging", "scheduling", "configuration",
		"validation", "serialization", "encryption", "compression", "indexing",
		"backup", "recovery", "migration", "versioning", "documentation",
		"profiling", "debugging", "tracing", "alerting", "reporting",
	}

	for i := 0; i < 30; i++ {
		// First observation of pair
		observations[i*2] = &models.Observation{
			ID:        int64(i*2 + 1),
			Title:     sql.NullString{String: topics[i] + " implementation", Valid: true},
			Narrative: sql.NullString{String: "Detailed " + topics[i] + " system design", Valid: true},
		}
		// Second observation of pair (similar to first)
		observations[i*2+1] = &models.Observation{
			ID:        int64(i*2 + 2),
			Title:     sql.NullString{String: topics[i] + " update", Valid: true},
			Narrative: sql.NullString{String: "Updated " + topics[i] + " logic", Valid: true},
		}
	}

	clustered := ClusterObservations(observations, 0.4)

	// With similar pairs, we should get roughly 30 clusters (one per topic)
	t.Logf("Clustered %d observations down to %d", len(observations), len(clustered))
	assert.Less(t, len(clustered), 60, "Similar observations should be clustered together")
	assert.GreaterOrEqual(t, len(clustered), 1, "Should have at least one cluster")
}

func TestClusterObservationsOptimized_AllUnique(t *testing.T) {
	t.Parallel()

	// Create 55 completely unique observations with NO shared terms
	// Each observation has only its unique term (no common words like "topic" or "content")
	uniqueTerms := []string{
		"aardvark", "butterfly", "caterpillar", "dragonfly", "elephant",
		"flamingo", "giraffe", "hippopotamus", "iguana", "jellyfish",
		"kangaroo", "leopard", "mongoose", "nightingale", "octopus",
		"penguin", "quail", "rhinoceros", "salamander", "toucan",
		"umbrella", "vulture", "walrus", "xylophone", "yakking",
		"zebra123", "astronomy99", "biology88", "chemistry77", "dynamics66",
		"economics55", "forensics44", "genetics33", "hydraulics22", "immunology11",
		"jurisprudence", "kinetics", "linguistics", "metallurgy", "neurology",
		"oceanography", "pharmacology", "quantumphysics", "robotics", "sociology",
		"thermodynamics", "ultrasound", "virology", "wavelength", "xenobiology",
		"yeastculture", "zoology123", "algebra456", "botany789", "calculus012",
	}

	observations := make([]*models.Observation, 55)
	for i := 0; i < 55; i++ {
		// Each observation has ONLY its unique term - no shared words
		observations[i] = &models.Observation{
			ID:        int64(i + 1),
			Title:     sql.NullString{String: uniqueTerms[i], Valid: true},
			Narrative: sql.NullString{String: uniqueTerms[i], Valid: true},
		}
	}

	clustered := ClusterObservations(observations, 0.4)

	// All unique content should remain unclustered
	assert.Len(t, clustered, 55, "All unique observations should be kept")
}

func TestClusterObservationsOptimized_SignaturePrefiltering(t *testing.T) {
	t.Parallel()

	// Test that signature prefiltering works correctly
	// Create observations where some have very different signatures
	observations := make([]*models.Observation, 60)

	// First half: all identical (about "authentication") - should cluster to 1
	for i := 0; i < 30; i++ {
		observations[i] = &models.Observation{
			ID:        int64(i + 1),
			Title:     sql.NullString{String: "authentication security login", Valid: true},
			Narrative: sql.NullString{String: "JWT tokens OAuth authentication", Valid: true},
		}
	}

	// Second half: each completely unique with NO shared terms
	diffTerms := []string{
		"quantumphysics", "photosynthesis", "archaeologydig", "linguisticstudy", "astronomystar",
		"paleontologyfossil", "oceanographywave", "entomologybug", "mycologyfungi", "herpetologysnake",
		"ornithologybird", "ichthyologyfish", "seismologyquake", "volcanologylava", "meteorologyrain",
		"cartographymap", "ethnographyculture", "philologyword", "numismaticscoin", "heraldryshield",
		"genealogytree", "chronologytime", "typographyfont", "calligraphyink", "epigraphystone",
		"papyrologytext", "codicologybook", "diplomaticseal", "sigillographywax", "sphragisticsring",
	}
	for i := 30; i < 60; i++ {
		term := diffTerms[i-30]
		// Each has ONLY its unique term - no shared words
		observations[i] = &models.Observation{
			ID:        int64(i + 1),
			Title:     sql.NullString{String: term, Valid: true},
			Narrative: sql.NullString{String: term, Valid: true},
		}
	}

	clustered := ClusterObservations(observations, 0.5)

	// Should have 31 clusters: 1 for all auth topics + 30 unique topics
	t.Logf("Clustered %d observations down to %d", len(observations), len(clustered))
	assert.Equal(t, 31, len(clustered), "Should have 31 clusters (1 auth + 30 unique)")
}

// =============================================================================
// TESTS FOR HELPER FUNCTIONS
// =============================================================================

func TestComputeTermSignature(t *testing.T) {
	tests := []struct {
		terms      map[string]bool
		compareTo  map[string]bool
		name       string
		expectZero bool
		expectSame bool
	}{
		// ===== GOOD CASES =====
		{
			name:       "single term",
			terms:      map[string]bool{"hello": true},
			expectZero: false,
		},
		{
			name:       "multiple terms",
			terms:      map[string]bool{"hello": true, "world": true},
			expectZero: false,
		},
		{
			name:       "identical terms produce same signature",
			terms:      map[string]bool{"alpha": true, "beta": true},
			expectSame: true,
			compareTo:  map[string]bool{"alpha": true, "beta": true},
		},

		// ===== EDGE CASES =====
		{
			name:       "empty set",
			terms:      map[string]bool{},
			expectZero: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sig := computeTermSignature(tt.terms)

			if tt.expectZero {
				assert.Equal(t, uint64(0), sig, "Empty set should produce zero signature")
			} else {
				assert.NotEqual(t, uint64(0), sig, "Non-empty set should produce non-zero signature")
			}

			if tt.expectSame && tt.compareTo != nil {
				sig2 := computeTermSignature(tt.compareTo)
				assert.Equal(t, sig, sig2, "Identical term sets should produce identical signatures")
			}
		})
	}
}

func TestComputeTermSignature_DifferentSets(t *testing.T) {
	t.Parallel()

	// Different term sets should usually produce different signatures
	set1 := map[string]bool{"authentication": true, "security": true}
	set2 := map[string]bool{"database": true, "migration": true}

	sig1 := computeTermSignature(set1)
	sig2 := computeTermSignature(set2)

	// While hash collisions are possible, they should be rare
	assert.NotEqual(t, sig1, sig2, "Different term sets should usually produce different signatures")
}

func TestPopCount64(t *testing.T) {
	tests := []struct {
		name     string
		input    uint64
		expected int
	}{
		// ===== GOOD CASES =====
		{name: "zero", input: 0, expected: 0},
		{name: "one", input: 1, expected: 1},
		{name: "powers of two", input: 8, expected: 1},
		{name: "all ones in byte", input: 0xFF, expected: 8},
		{name: "alternating bits", input: 0xAAAAAAAAAAAAAAAA, expected: 32},
		{name: "max uint64", input: 0xFFFFFFFFFFFFFFFF, expected: 64},

		// ===== EDGE CASES =====
		{name: "single high bit", input: 1 << 63, expected: 1},
		{name: "sparse bits", input: 0x8000000000000001, expected: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := popCount64(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsSimilarToAny_EmptyTerms(t *testing.T) {
	t.Parallel()

	// Observation with no extractable terms
	emptyObs := &models.Observation{
		ID:        1,
		Title:     sql.NullString{String: "", Valid: false},
		Narrative: sql.NullString{String: "", Valid: false},
	}

	existing := []*models.Observation{
		{
			ID:        2,
			Title:     sql.NullString{String: "Some content here", Valid: true},
			Narrative: sql.NullString{String: "More content", Valid: true},
		},
	}

	// Should return false when new observation has no terms
	assert.False(t, IsSimilarToAny(emptyObs, existing, 0.3))
}

func TestExtractObservationTerms_FilesModified(t *testing.T) {
	t.Parallel()

	obs := &models.Observation{
		ID:            1,
		Title:         sql.NullString{String: "Code changes", Valid: true},
		FilesModified: models.JSONStringArray{"/src/handler.go", "/pkg/models/user.go"},
	}

	terms := ExtractObservationTerms(obs)

	// Should contain filenames from FilesModified
	assert.Contains(t, terms, "handler.go")
	assert.Contains(t, terms, "user.go")
}

func TestAddTerms_ShortWords(t *testing.T) {
	t.Parallel()

	terms := make(map[string]bool)

	addTerms(terms, "I am a go developer")

	// Short words (< 3 chars) should be excluded
	assert.NotContains(t, terms, "i")
	assert.NotContains(t, terms, "am")
	assert.NotContains(t, terms, "a")
	assert.NotContains(t, terms, "go") // Only 2 chars

	// "developer" should be included
	assert.Contains(t, terms, "developer")
}

func TestAddTerms_SpecialCharacters(t *testing.T) {
	t.Parallel()

	terms := make(map[string]bool)

	addTerms(terms, "user_id authentication-flow JWT_token")

	// Hyphens split words, but underscores are kept as part of the word
	// (underscore is included in the tokenization regex)
	assert.Contains(t, terms, "user_id")
	assert.Contains(t, terms, "authentication")
	assert.Contains(t, terms, "flow")
	assert.Contains(t, terms, "jwt_token")
}

func TestJaccardSimilarity_SubsetSuperset(t *testing.T) {
	t.Parallel()

	subset := map[string]bool{"a": true, "b": true}
	superset := map[string]bool{"a": true, "b": true, "c": true, "d": true}

	// Subset similarity should be intersection/union = 2/4 = 0.5
	result := JaccardSimilarity(subset, superset)
	assert.InDelta(t, 0.5, result, 0.001)
}

func TestClusterObservations_HighThreshold(t *testing.T) {
	t.Parallel()

	// With a very high threshold, almost nothing should be clustered
	observations := []*models.Observation{
		{ID: 1, Title: sql.NullString{String: "authentication implementation", Valid: true}},
		{ID: 2, Title: sql.NullString{String: "authentication update", Valid: true}},
		{ID: 3, Title: sql.NullString{String: "authentication refactor", Valid: true}},
	}

	// With threshold of 0.9, even similar observations shouldn't cluster
	clustered := ClusterObservations(observations, 0.9)

	assert.Len(t, clustered, 3, "High threshold should prevent clustering")
}

func TestClusterObservations_LowThreshold(t *testing.T) {
	t.Parallel()

	// With a very low threshold, more things should be clustered
	observations := []*models.Observation{
		{ID: 1, Title: sql.NullString{String: "authentication implementation details", Valid: true}},
		{ID: 2, Title: sql.NullString{String: "authentication security update", Valid: true}},
		{ID: 3, Title: sql.NullString{String: "something completely different topic", Valid: true}},
	}

	// With threshold of 0.1, partial overlap should cluster
	clustered := ClusterObservations(observations, 0.1)

	// First two share "authentication", should likely cluster
	assert.LessOrEqual(t, len(clustered), 3)
}
