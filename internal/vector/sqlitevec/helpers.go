// Package sqlitevec provides sqlite-vec based vector database integration for claude-mnemonic.
package sqlitevec

// DocType represents the type of document stored in the vector table.
type DocType string

const (
	DocTypeObservation    DocType = "observation"
	DocTypeSessionSummary DocType = "session_summary"
	DocTypeUserPrompt     DocType = "user_prompt"
)

// Document represents a document to store with vector embedding.
type Document struct {
	ID       string
	Content  string
	Metadata map[string]any
}

// QueryResult represents a search result from vector search.
type QueryResult struct {
	ID         string
	Distance   float64
	Similarity float64 // 1.0 = identical, 0.0 = opposite (derived from distance)
	Metadata   map[string]any
}

// DistanceToSimilarity converts sqlite-vec cosine distance to similarity score.
// Cosine distance: 0 = identical, 2 = opposite
// Similarity: 1.0 = identical, 0.0 = opposite
func DistanceToSimilarity(distance float64) float64 {
	return 1.0 - (distance / 2.0)
}

// FilterByThreshold filters results to only include those above the similarity threshold.
// If maxResults > 0, also caps the number of results.
func FilterByThreshold(results []QueryResult, threshold float64, maxResults int) []QueryResult {
	var filtered []QueryResult
	for _, r := range results {
		if r.Similarity >= threshold {
			filtered = append(filtered, r)
			if maxResults > 0 && len(filtered) >= maxResults {
				break
			}
		}
	}
	return filtered
}

// ExtractedIDs contains SQLite IDs extracted from query results, grouped by document type.
type ExtractedIDs struct {
	ObservationIDs []int64
	SummaryIDs     []int64
	PromptIDs      []int64
}

// BuildWhereFilter creates a where filter map for vector queries.
// If docType is empty, no doc_type filter is added.
func BuildWhereFilter(docType DocType, project string) map[string]interface{} {
	where := make(map[string]interface{})
	if docType != "" {
		where["doc_type"] = string(docType)
	}
	if project != "" {
		where["project"] = project
	}
	return where
}

// ExtractIDsByDocType extracts SQLite IDs from query results,
// grouped by document type and deduplicated.
func ExtractIDsByDocType(results []QueryResult) *ExtractedIDs {
	ids := &ExtractedIDs{}
	seenObs := make(map[int64]bool)
	seenSummary := make(map[int64]bool)
	seenPrompt := make(map[int64]bool)

	for _, result := range results {
		sqliteID, ok := result.Metadata["sqlite_id"].(float64)
		if !ok {
			// Try int64 directly
			if id, ok := result.Metadata["sqlite_id"].(int64); ok {
				sqliteID = float64(id)
			} else {
				continue
			}
		}
		id := int64(sqliteID)

		docType, _ := result.Metadata["doc_type"].(string)
		switch docType {
		case string(DocTypeObservation):
			if !seenObs[id] {
				seenObs[id] = true
				ids.ObservationIDs = append(ids.ObservationIDs, id)
			}
		case string(DocTypeSessionSummary):
			if !seenSummary[id] {
				seenSummary[id] = true
				ids.SummaryIDs = append(ids.SummaryIDs, id)
			}
		case string(DocTypeUserPrompt):
			if !seenPrompt[id] {
				seenPrompt[id] = true
				ids.PromptIDs = append(ids.PromptIDs, id)
			}
		}
	}

	return ids
}

// ExtractObservationIDs extracts observation SQLite IDs from query results,
// optionally filtering by project or including global scope.
func ExtractObservationIDs(results []QueryResult, project string) []int64 {
	var ids []int64
	seen := make(map[int64]bool)

	for _, result := range results {
		sqliteID, ok := result.Metadata["sqlite_id"].(float64)
		if !ok {
			if id, ok := result.Metadata["sqlite_id"].(int64); ok {
				sqliteID = float64(id)
			} else {
				continue
			}
		}
		id := int64(sqliteID)

		docType, _ := result.Metadata["doc_type"].(string)
		if docType != string(DocTypeObservation) {
			continue
		}

		if project != "" {
			proj, _ := result.Metadata["project"].(string)
			scope, _ := result.Metadata["scope"].(string)
			if proj != project && scope != "global" {
				continue
			}
		}

		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}

	return ids
}

// ExtractSummaryIDs extracts session summary SQLite IDs from query results.
func ExtractSummaryIDs(results []QueryResult, project string) []int64 {
	var ids []int64
	seen := make(map[int64]bool)

	for _, result := range results {
		sqliteID, ok := result.Metadata["sqlite_id"].(float64)
		if !ok {
			if id, ok := result.Metadata["sqlite_id"].(int64); ok {
				sqliteID = float64(id)
			} else {
				continue
			}
		}
		id := int64(sqliteID)

		docType, _ := result.Metadata["doc_type"].(string)
		if docType != string(DocTypeSessionSummary) {
			continue
		}

		if project != "" {
			proj, _ := result.Metadata["project"].(string)
			if proj != project {
				continue
			}
		}

		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}

	return ids
}

// ExtractPromptIDs extracts user prompt SQLite IDs from query results.
func ExtractPromptIDs(results []QueryResult, project string) []int64 {
	var ids []int64
	seen := make(map[int64]bool)

	for _, result := range results {
		sqliteID, ok := result.Metadata["sqlite_id"].(float64)
		if !ok {
			if id, ok := result.Metadata["sqlite_id"].(int64); ok {
				sqliteID = float64(id)
			} else {
				continue
			}
		}
		id := int64(sqliteID)

		docType, _ := result.Metadata["doc_type"].(string)
		if docType != string(DocTypeUserPrompt) {
			continue
		}

		if project != "" {
			proj, _ := result.Metadata["project"].(string)
			if proj != project {
				continue
			}
		}

		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}

	return ids
}

// Helper functions for metadata manipulation

func copyMetadata(base map[string]any, key string, value any) map[string]any {
	result := make(map[string]any, len(base)+1)
	for k, v := range base {
		result[k] = v
	}
	result[key] = value
	return result
}

func copyMetadataMulti(base map[string]any, extra map[string]any) map[string]any {
	result := make(map[string]any, len(base)+len(extra))
	for k, v := range base {
		result[k] = v
	}
	for k, v := range extra {
		result[k] = v
	}
	return result
}

func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	result := strs[0]
	for i := 1; i < len(strs); i++ {
		result += sep + strs[i]
	}
	return result
}
