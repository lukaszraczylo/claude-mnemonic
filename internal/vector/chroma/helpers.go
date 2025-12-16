// Package chroma provides ChromaDB vector database integration for claude-mnemonic.
package chroma

// DocType represents the type of document stored in ChromaDB.
type DocType string

const (
	DocTypeObservation    DocType = "observation"
	DocTypeSessionSummary DocType = "session_summary"
	DocTypeUserPrompt     DocType = "user_prompt"
)

// ExtractedIDs contains SQLite IDs extracted from ChromaDB results, grouped by document type.
type ExtractedIDs struct {
	ObservationIDs []int64
	SummaryIDs     []int64
	PromptIDs      []int64
}

// BuildWhereFilter creates a where filter map for ChromaDB queries.
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

// ExtractIDsByDocType extracts SQLite IDs from ChromaDB query results,
// grouped by document type and deduplicated.
func ExtractIDsByDocType(results []QueryResult) *ExtractedIDs {
	ids := &ExtractedIDs{}
	seenObs := make(map[int64]bool)
	seenSummary := make(map[int64]bool)
	seenPrompt := make(map[int64]bool)

	for _, result := range results {
		sqliteID, ok := result.Metadata["sqlite_id"].(float64)
		if !ok {
			continue
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

// ExtractObservationIDs extracts observation SQLite IDs from ChromaDB query results,
// optionally filtering by project or including global scope.
// If project is empty, all observation IDs are returned.
// If project is set, only observations matching the project or with global scope are returned.
func ExtractObservationIDs(results []QueryResult, project string) []int64 {
	var ids []int64
	seen := make(map[int64]bool)

	for _, result := range results {
		sqliteID, ok := result.Metadata["sqlite_id"].(float64)
		if !ok {
			continue
		}
		id := int64(sqliteID)

		// Check document type
		docType, _ := result.Metadata["doc_type"].(string)
		if docType != string(DocTypeObservation) {
			continue
		}

		// Apply project/scope filter if project is specified
		if project != "" {
			proj, _ := result.Metadata["project"].(string)
			scope, _ := result.Metadata["scope"].(string)
			// Include if project matches OR scope is global
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

// ExtractSummaryIDs extracts session summary SQLite IDs from ChromaDB query results.
func ExtractSummaryIDs(results []QueryResult, project string) []int64 {
	var ids []int64
	seen := make(map[int64]bool)

	for _, result := range results {
		sqliteID, ok := result.Metadata["sqlite_id"].(float64)
		if !ok {
			continue
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

// ExtractPromptIDs extracts user prompt SQLite IDs from ChromaDB query results.
func ExtractPromptIDs(results []QueryResult, project string) []int64 {
	var ids []int64
	seen := make(map[int64]bool)

	for _, result := range results {
		sqliteID, ok := result.Metadata["sqlite_id"].(float64)
		if !ok {
			continue
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
