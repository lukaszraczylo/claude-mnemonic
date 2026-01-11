// Package expansion provides context-aware query expansion for improved search recall.
package expansion

import (
	"context"
	"math"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/lukaszraczylo/claude-mnemonic/internal/embedding"
	"github.com/rs/zerolog/log"
)

// QueryIntent represents the detected intent of a query.
type QueryIntent string

const (
	// IntentQuestion indicates a question-type query (how, why, what, etc.)
	IntentQuestion QueryIntent = "question"
	// IntentError indicates an error/debugging query
	IntentError QueryIntent = "error"
	// IntentImplementation indicates an implementation/coding query
	IntentImplementation QueryIntent = "implementation"
	// IntentArchitecture indicates an architecture/design query
	IntentArchitecture QueryIntent = "architecture"
	// IntentGeneral indicates a general lookup query
	IntentGeneral QueryIntent = "general"
)

// ExpandedQuery represents a query variant with metadata.
type ExpandedQuery struct {
	Query  string      `json:"query"`
	Source string      `json:"source"`
	Intent QueryIntent `json:"intent"`
	Weight float64     `json:"weight"`
}

// Expander provides context-aware query expansion.
type Expander struct {
	embedSvc       *embedding.Service
	intentPatterns map[QueryIntent][]*regexp.Regexp
	vocabulary     []VocabEntry
	vocabVectors   [][]float32
	vocabMu        sync.RWMutex
}

// VocabEntry represents a vocabulary term from observations.
type VocabEntry struct {
	Term   string
	Source string
	Weight float64
}

// Config holds expander configuration.
type Config struct {
	// MaxExpansions limits the number of expanded queries returned
	MaxExpansions int
	// MinSimilarity is the minimum similarity score for vocabulary expansion
	MinSimilarity float64
	// EnableVocabularyExpansion enables finding related terms from observations
	EnableVocabularyExpansion bool
}

// DefaultConfig returns sensible default configuration.
func DefaultConfig() Config {
	return Config{
		MaxExpansions:             4,
		MinSimilarity:             0.5,
		EnableVocabularyExpansion: true,
	}
}

// NewExpander creates a new query expander.
func NewExpander(embedSvc *embedding.Service) *Expander {
	e := &Expander{
		embedSvc:       embedSvc,
		intentPatterns: buildIntentPatterns(),
	}
	return e
}

// buildIntentPatterns creates regex patterns for intent detection.
func buildIntentPatterns() map[QueryIntent][]*regexp.Regexp {
	patterns := make(map[QueryIntent][]*regexp.Regexp)

	// Question patterns
	patterns[IntentQuestion] = []*regexp.Regexp{
		regexp.MustCompile(`(?i)^(how|why|what|when|where|which|who)\b`),
		regexp.MustCompile(`(?i)\?$`),
		regexp.MustCompile(`(?i)\b(explain|describe|understand)\b`),
	}

	// Error/debugging patterns
	patterns[IntentError] = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(error|bug|issue|problem|fail|crash|exception|panic)\b`),
		regexp.MustCompile(`(?i)\b(fix|debug|troubleshoot|resolve)\b`),
		regexp.MustCompile(`(?i)\b(doesn't work|not working|broken)\b`),
	}

	// Implementation patterns
	patterns[IntentImplementation] = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(implement|add|create|build|write|code)\b`),
		regexp.MustCompile(`(?i)\b(function|method|handler|endpoint|api)\b`),
		regexp.MustCompile(`(?i)\b(feature|functionality)\b`),
	}

	// Architecture patterns
	patterns[IntentArchitecture] = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(architecture|design|pattern|structure)\b`),
		regexp.MustCompile(`(?i)\b(component|module|layer|service)\b`),
		regexp.MustCompile(`(?i)\b(flow|pipeline|workflow)\b`),
	}

	return patterns
}

// DetectIntent analyzes a query to determine its intent.
func (e *Expander) DetectIntent(query string) QueryIntent {
	query = strings.TrimSpace(query)
	if query == "" {
		return IntentGeneral
	}

	// Check patterns in priority order
	intentOrder := []QueryIntent{IntentError, IntentQuestion, IntentImplementation, IntentArchitecture}

	for _, intent := range intentOrder {
		patterns := e.intentPatterns[intent]
		for _, pattern := range patterns {
			if pattern.MatchString(query) {
				return intent
			}
		}
	}

	return IntentGeneral
}

// Expand generates expanded query variants based on the original query.
func (e *Expander) Expand(ctx context.Context, query string, cfg Config) []ExpandedQuery {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}

	intent := e.DetectIntent(query)
	expansions := make([]ExpandedQuery, 0, cfg.MaxExpansions)

	// Always include the original query with highest weight
	expansions = append(expansions, ExpandedQuery{
		Query:  query,
		Weight: 1.0,
		Source: "original",
		Intent: intent,
	})

	// Generate intent-based expansions
	intentExpansions := e.expandByIntent(query, intent)
	expansions = append(expansions, intentExpansions...)

	// Generate vocabulary-based expansions if enabled and we have vocabulary
	if cfg.EnableVocabularyExpansion && e.embedSvc != nil && len(e.vocabulary) > 0 {
		vocabExpansions := e.expandByVocabulary(ctx, query, cfg.MinSimilarity)
		expansions = append(expansions, vocabExpansions...)
	}

	// Deduplicate and limit
	expansions = deduplicateExpansions(expansions)
	if len(expansions) > cfg.MaxExpansions {
		expansions = expansions[:cfg.MaxExpansions]
	}

	log.Debug().
		Str("query", truncate(query, 50)).
		Str("intent", string(intent)).
		Int("expansions", len(expansions)).
		Msg("Query expanded")

	return expansions
}

// expandByIntent generates expansions based on detected query intent.
func (e *Expander) expandByIntent(query string, intent QueryIntent) []ExpandedQuery {
	var expansions []ExpandedQuery

	// Extract key terms from query for context-aware expansion
	keyTerms := extractKeyTerms(query)

	switch intent {
	case IntentQuestion:
		// For questions, create a declarative variant
		declarative := makeDeclarative(query)
		if declarative != query {
			expansions = append(expansions, ExpandedQuery{
				Query:  declarative,
				Weight: 0.85,
				Source: "intent:declarative",
				Intent: intent,
			})
		}

	case IntentError:
		// For errors, expand with solution-oriented terms
		if len(keyTerms) > 0 {
			solutionQuery := strings.Join(keyTerms, " ") + " solution fix"
			expansions = append(expansions, ExpandedQuery{
				Query:  solutionQuery,
				Weight: 0.8,
				Source: "intent:solution",
				Intent: intent,
			})
		}

	case IntentImplementation:
		// For implementation queries, focus on the what/how
		if len(keyTerms) > 0 {
			howQuery := "how " + strings.Join(keyTerms, " ")
			expansions = append(expansions, ExpandedQuery{
				Query:  howQuery,
				Weight: 0.75,
				Source: "intent:how",
				Intent: intent,
			})
		}

	case IntentArchitecture:
		// For architecture queries, expand with design context
		if len(keyTerms) > 0 {
			designQuery := strings.Join(keyTerms, " ") + " design structure"
			expansions = append(expansions, ExpandedQuery{
				Query:  designQuery,
				Weight: 0.75,
				Source: "intent:design",
				Intent: intent,
			})
		}

	case IntentGeneral:
		// For general queries, try noun phrase extraction
		// No additional expansion - rely on vocabulary expansion
	}

	return expansions
}

// expandByVocabulary finds similar terms from the observation vocabulary.
func (e *Expander) expandByVocabulary(ctx context.Context, query string, minSimilarity float64) []ExpandedQuery {
	e.vocabMu.RLock()
	defer e.vocabMu.RUnlock()

	if len(e.vocabulary) == 0 || e.embedSvc == nil {
		return nil
	}

	// Embed the query
	queryEmb, err := e.embedSvc.Embed(query)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to embed query for vocabulary expansion")
		return nil
	}

	// Find similar vocabulary terms
	type scoredTerm struct {
		entry VocabEntry
		score float64
	}

	var similar []scoredTerm
	for i, entry := range e.vocabulary {
		if i >= len(e.vocabVectors) {
			break
		}

		score := cosineSimilarity(queryEmb, e.vocabVectors[i])
		if score >= minSimilarity {
			similar = append(similar, scoredTerm{entry: entry, score: score})
		}
	}

	if len(similar) == 0 {
		return nil
	}

	// Sort by score (descending) using Go's standard sort - O(n log n)
	sort.Slice(similar, func(i, j int) bool {
		return similar[i].score > similar[j].score
	})

	// Create expansion by combining top similar terms with query
	var expansions []ExpandedQuery
	if len(similar) > 0 {
		// Take top 2 similar terms and combine with original key terms
		keyTerms := extractKeyTerms(query)
		for i := 0; i < min(2, len(similar)); i++ {
			term := similar[i].entry.Term
			// Don't add if term is already in query
			if strings.Contains(strings.ToLower(query), strings.ToLower(term)) {
				continue
			}

			combinedQuery := strings.Join(keyTerms, " ") + " " + term
			expansions = append(expansions, ExpandedQuery{
				Query:  combinedQuery,
				Weight: 0.7 * similar[i].score * similar[i].entry.Weight,
				Source: "vocabulary:" + term,
				Intent: IntentGeneral,
			})
		}
	}

	return expansions
}

// Helper functions

// extractKeyTerms extracts meaningful terms from a query.
func extractKeyTerms(query string) []string {
	// Common stop words to filter out
	stopWords := map[string]bool{
		"a": true, "an": true, "the": true, "is": true, "are": true,
		"was": true, "were": true, "be": true, "been": true, "being": true,
		"have": true, "has": true, "had": true, "do": true, "does": true,
		"did": true, "will": true, "would": true, "could": true, "should": true,
		"may": true, "might": true, "must": true, "can": true,
		"i": true, "me": true, "my": true, "we": true, "our": true,
		"you": true, "your": true, "it": true, "its": true,
		"this": true, "that": true, "these": true, "those": true,
		"what": true, "which": true, "who": true, "whom": true,
		"how": true, "why": true, "when": true, "where": true,
		"to": true, "for": true, "with": true, "about": true, "from": true,
		"in": true, "on": true, "at": true, "by": true, "of": true,
		"and": true, "or": true, "but": true, "if": true, "then": true,
	}

	// Split and filter
	words := strings.Fields(strings.ToLower(query))
	var terms []string

	for _, word := range words {
		// Remove punctuation
		word = strings.Trim(word, ".,?!;:'\"()[]{}")
		if len(word) < 2 {
			continue
		}
		if stopWords[word] {
			continue
		}
		terms = append(terms, word)
	}

	return terms
}

// makeDeclarative converts a question to a declarative statement.
func makeDeclarative(query string) string {
	query = strings.TrimSpace(query)

	// Remove question mark
	query = strings.TrimSuffix(query, "?")

	// Handle common question patterns
	patterns := []struct {
		prefix      string
		replacement string
	}{
		{"how do i ", ""},
		{"how to ", ""},
		{"how does ", ""},
		{"how is ", ""},
		{"what is ", ""},
		{"what are ", ""},
		{"why does ", ""},
		{"why is ", ""},
		{"where is ", ""},
		{"where are ", ""},
		{"when does ", ""},
		{"when is ", ""},
	}

	lower := strings.ToLower(query)
	for _, p := range patterns {
		if strings.HasPrefix(lower, p.prefix) {
			return strings.TrimSpace(query[len(p.prefix):])
		}
	}

	return query
}

// deduplicateExpansions removes duplicate queries while preserving order.
func deduplicateExpansions(expansions []ExpandedQuery) []ExpandedQuery {
	seen := make(map[string]bool)
	result := make([]ExpandedQuery, 0, len(expansions))

	for _, exp := range expansions {
		normalized := strings.ToLower(strings.TrimSpace(exp.Query))
		if !seen[normalized] {
			seen[normalized] = true
			result = append(result, exp)
		}
	}

	return result
}

// cosineSimilarity computes cosine similarity between two vectors.
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dot / (sqrt(normA) * sqrt(normB))
}

// sqrt uses the standard math.Sqrt for better performance and accuracy.
// Returns 0 for non-positive values (original behavior for compatibility).
func sqrt(x float64) float64 {
	if x <= 0 {
		return 0
	}
	return math.Sqrt(x)
}

// truncate truncates a string to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
