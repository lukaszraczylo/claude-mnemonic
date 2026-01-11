// Package worker provides context and search-related HTTP handlers.
package worker

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lukaszraczylo/claude-mnemonic/internal/db/gorm"
	"github.com/lukaszraczylo/claude-mnemonic/internal/reranking"
	"github.com/lukaszraczylo/claude-mnemonic/internal/search/expansion"
	"github.com/lukaszraczylo/claude-mnemonic/internal/vector/sqlitevec"
	"github.com/lukaszraczylo/claude-mnemonic/internal/worker/sdk"
	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
	"github.com/rs/zerolog/log"
)

// handleSearchByPrompt searches observations relevant to a user prompt.
// IMPORTANT: This is on the critical startup path - must be fast!
// No synchronous verification - just filter by staleness and return.
func (s *Service) handleSearchByPrompt(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	query := r.URL.Query().Get("query")
	cwd := r.URL.Query().Get("cwd")

	if project == "" || query == "" {
		http.Error(w, "project and query required", http.StatusBadRequest)
		return
	}

	// Validate project name to prevent path traversal
	if err := ValidateProjectName(project); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	limit := gorm.ParseLimitParamWithMax(r, DefaultSearchLimit, 200)

	var observations []*models.Observation
	var err error
	var usedVector bool
	similarityScores := make(map[int64]float64) // Track similarity per observation

	// Get threshold settings from config
	threshold := s.config.ContextRelevanceThreshold
	maxResults := s.config.ContextMaxPromptResults

	// Generate expanded queries if query expander is available
	// Use timeout context to prevent query expansion from blocking
	var expandedQueries []expansion.ExpandedQuery
	var detectedIntent string
	if s.queryExpander != nil {
		expandCtx, expandCancel := context.WithTimeout(r.Context(), 5*time.Second)
		cfg := expansion.DefaultConfig()
		cfg.EnableVocabularyExpansion = false // Vocabulary expansion is optional
		expandedQueries = s.queryExpander.Expand(expandCtx, query, cfg)
		expandCancel() // Cancel immediately after use (defer not needed - no panic possible between creation and here)
		if len(expandedQueries) > 0 {
			detectedIntent = string(expandedQueries[0].Intent)
		}
	}
	if len(expandedQueries) == 0 {
		// Fallback to just the original query
		expandedQueries = []expansion.ExpandedQuery{
			{Query: query, Weight: 1.0, Source: "original"},
		}
	}

	// Try vector search first if available
	var vectorSearchFailed bool
	if s.vectorClient != nil && s.vectorClient.IsConnected() {
		where := sqlitevec.BuildWhereFilter(sqlitevec.DocTypeObservation, "")

		// Search with each expanded query and merge results
		// Pre-allocate with estimated capacity to avoid repeated reallocation
		estimatedCapacity := len(expandedQueries) * limit * 2
		allVectorResults := make([]sqlitevec.QueryResult, 0, estimatedCapacity)
		queryWeights := make(map[string]float64, len(expandedQueries))
		var vectorErrors int

		for _, eq := range expandedQueries {
			vectorResults, vecErr := s.vectorClient.Query(r.Context(), eq.Query, limit*2, where)
			if vecErr != nil {
				vectorErrors++
				log.Debug().Err(vecErr).Str("query", eq.Query).Msg("Vector query failed")
			} else if len(vectorResults) > 0 {
				// Apply weight to similarity scores before merging
				for i := range vectorResults {
					vectorResults[i].Similarity *= eq.Weight
				}
				allVectorResults = append(allVectorResults, vectorResults...)
				queryWeights[eq.Query] = eq.Weight
			}
		}

		// Track if vector search had issues
		if vectorErrors > 0 && vectorErrors == len(expandedQueries) {
			vectorSearchFailed = true
			log.Warn().Int("errors", vectorErrors).Str("project", project).Msg("All vector queries failed, falling back to FTS")
		}

		if len(allVectorResults) > 0 {
			// Filter by relevance threshold before extracting IDs
			// Use a slightly lower threshold for expanded queries
			effectiveThreshold := threshold * 0.9 // Allow slightly lower scores for expanded queries
			filteredResults := sqlitevec.FilterByThreshold(allVectorResults, effectiveThreshold, 0)

			// Build similarity map for filtered results (keeping highest weighted score per observation)
			for _, vr := range filteredResults {
				if sqliteID, ok := vr.Metadata["sqlite_id"].(float64); ok {
					id := int64(sqliteID)
					// Keep the highest score for each observation
					if existing, exists := similarityScores[id]; !exists || vr.Similarity > existing {
						similarityScores[id] = vr.Similarity
					}
				}
			}

			// Extract observation IDs with project/scope filtering using shared helper
			obsIDs := sqlitevec.ExtractObservationIDs(filteredResults, project)

			if len(obsIDs) > 0 {
				// Fetch full observations from SQLite
				observations, err = s.observationStore.GetObservationsByIDs(r.Context(), obsIDs, "date_desc", limit)
				if err == nil {
					usedVector = true
				}
			}
		}
	}

	// Fall back to FTS if vector search not available, failed, or returned no results
	if !usedVector || len(observations) == 0 {
		if vectorSearchFailed {
			log.Info().Str("project", project).Msg("Using FTS fallback due to vector search failure")
		}
		observations, err = s.observationStore.SearchObservationsFTS(r.Context(), query, project, limit)
		if err != nil {
			// FTS might fail if query has special chars, try without
			log.Warn().Err(err).Str("query", query).Msg("FTS search failed, falling back to recent")
			observations, err = s.observationStore.GetRecentObservations(r.Context(), project, limit)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
	}

	// Fast staleness filter - NO verification (that's too slow for interactive use)
	// Just check mtimes and exclude obviously stale observations
	var staleCount int
	freshObservations := make([]*models.Observation, 0, len(observations))

	for _, obs := range observations {
		if len(obs.FileMtimes) > 0 && cwd != "" {
			var paths []string
			for path := range obs.FileMtimes {
				paths = append(paths, path)
			}
			currentMtimes := sdk.GetFileMtimes(paths, cwd)

			if obs.CheckStaleness(currentMtimes) {
				// Stale - exclude but don't verify (too slow)
				// Queue for background verification instead
				staleCount++
				s.queueStaleVerification(obs.ID, cwd)
				continue
			}
		}
		freshObservations = append(freshObservations, obs)
	}

	// Apply cross-encoder reranking if available
	var reranked bool
	if s.reranker != nil && len(freshObservations) > 0 && usedVector {
		// Build candidates from observations with their bi-encoder scores
		candidates := make([]reranking.Candidate, len(freshObservations))
		for i, obs := range freshObservations {
			// Use strings.Builder for efficient concatenation
			var content string
			if obs.Narrative.Valid && obs.Narrative.String != "" {
				var sb strings.Builder
				sb.Grow(len(obs.Title.String) + 1 + len(obs.Narrative.String))
				sb.WriteString(obs.Title.String)
				sb.WriteByte(' ')
				sb.WriteString(obs.Narrative.String)
				content = sb.String()
			} else {
				content = obs.Title.String
			}
			candidates[i] = reranking.Candidate{
				ID:       strconv.FormatInt(obs.ID, 10), // Faster than fmt.Sprintf
				Content:  content,
				Score:    similarityScores[obs.ID],
				Metadata: map[string]any{"obs_idx": i},
			}
		}

		// Rerank using cross-encoder - use pure mode or combined scores
		var rerankResults []reranking.RerankResult
		var rerankErr error
		if s.config.RerankingPureMode {
			rerankResults, rerankErr = s.reranker.RerankByScore(query, candidates, s.config.RerankingResults)
		} else {
			rerankResults, rerankErr = s.reranker.Rerank(query, candidates, s.config.RerankingResults)
		}
		if rerankErr != nil {
			log.Warn().Err(rerankErr).Msg("Cross-encoder reranking failed, using original order")
		} else if len(rerankResults) > 0 {
			// Update similarity scores with reranked scores
			for _, rr := range rerankResults {
				if id, err := strconv.ParseInt(rr.ID, 10, 64); err == nil {
					similarityScores[id] = rr.CombinedScore
				}
			}

			// Reorder observations based on rerank results
			reorderedObs := make([]*models.Observation, 0, len(rerankResults))
			obsMap := make(map[int64]*models.Observation)
			for _, obs := range freshObservations {
				obsMap[obs.ID] = obs
			}
			for _, rr := range rerankResults {
				if id, err := strconv.ParseInt(rr.ID, 10, 64); err == nil {
					if obs, ok := obsMap[id]; ok {
						reorderedObs = append(reorderedObs, obs)
					}
				}
			}
			freshObservations = reorderedObs
			reranked = true

			log.Debug().
				Int("candidates", len(candidates)).
				Int("returned", len(rerankResults)).
				Msg("Cross-encoder reranking complete")
		}
	}

	// Cluster similar observations to remove duplicates
	clusteredObservations := clusterObservations(freshObservations, 0.4)
	duplicatesRemoved := len(freshObservations) - len(clusteredObservations)

	// Sort by similarity score (highest first) if we have scores and didn't rerank
	if len(similarityScores) > 0 && len(clusteredObservations) > 0 && !reranked {
		sort.Slice(clusteredObservations, func(i, j int) bool {
			scoreI := similarityScores[clusteredObservations[i].ID]
			scoreJ := similarityScores[clusteredObservations[j].ID]
			return scoreI > scoreJ
		})
	}

	// Apply max results cap if configured
	if maxResults > 0 && len(clusteredObservations) > maxResults {
		clusteredObservations = clusteredObservations[:maxResults]
	}

	// Record retrieval stats with staleness metrics
	s.recordRetrievalStatsExtended(project, int64(len(clusteredObservations)), 0, 0,
		int64(staleCount), int64(len(freshObservations)), int64(duplicatesRemoved), true)

	// Increment retrieval counts for scoring (async, non-blocking)
	if len(clusteredObservations) > 0 {
		ids := make([]int64, len(clusteredObservations))
		for i, obs := range clusteredObservations {
			ids[i] = obs.ID
		}
		s.incrementRetrievalCounts(ids)
	}

	log.Info().
		Str("project", project).
		Str("query", query).
		Str("intent", detectedIntent).
		Int("expansions", len(expandedQueries)).
		Int("found", len(clusteredObservations)).
		Int("stale_excluded", staleCount).
		Float64("threshold", threshold).
		Msg("Prompt-based observation search")

	// Build response with similarity scores
	obsWithScores := make([]map[string]any, len(clusteredObservations))
	for i, obs := range clusteredObservations {
		obsMap := obs.ToMap()
		if score, ok := similarityScores[obs.ID]; ok {
			obsMap["similarity"] = score
		}
		obsWithScores[i] = obsMap
	}

	// Build expansion info for response
	expansionInfo := make([]map[string]any, len(expandedQueries))
	for i, eq := range expandedQueries {
		expansionInfo[i] = map[string]any{
			"query":  eq.Query,
			"weight": eq.Weight,
			"source": eq.Source,
		}
	}

	// Track this search for analytics
	s.trackSearchQuery(query, project, "observations", len(clusteredObservations), usedVector)

	writeJSON(w, map[string]any{
		"project":      project,
		"query":        query,
		"intent":       detectedIntent,
		"expansions":   expansionInfo,
		"observations": obsWithScores,
		"threshold":    threshold,
		"max_results":  maxResults,
	})
}

// handleFileContext returns observations relevant to specific files being worked on.
// Uses vector similarity search to find observations that mention or relate to the given files.
func (s *Service) handleFileContext(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	if project == "" {
		http.Error(w, "project required", http.StatusBadRequest)
		return
	}

	filesParam := r.URL.Query().Get("files")
	if filesParam == "" {
		http.Error(w, "files required", http.StatusBadRequest)
		return
	}

	// Parse comma-separated file paths
	files := strings.Split(filesParam, ",")
	if len(files) == 0 {
		http.Error(w, "at least one file required", http.StatusBadRequest)
		return
	}

	// Limit to reasonable number of files
	maxFiles := 20
	if len(files) > maxFiles {
		files = files[:maxFiles]
	}

	// Get limit parameter (default 10 per file)
	limitStr := r.URL.Query().Get("limit")
	limit := 10
	if limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 && parsed <= 50 {
			limit = parsed
		}
	}

	// Search for observations related to each file in parallel
	ctx := r.Context()

	// Check if vector search is available
	if s.vectorClient == nil || !s.vectorClient.IsConnected() {
		writeJSON(w, map[string]any{
			"files":   files,
			"results": map[string]any{},
			"count":   0,
			"error":   "vector search not available",
		})
		return
	}

	// Prepare for parallel execution
	type fileResult struct {
		file    string
		results []map[string]any
		obsIDs  []int64 // Track observation IDs for deduplication
	}

	resultsChan := make(chan fileResult, len(files))
	sem := make(chan struct{}, 5) // Limit concurrency to 5 parallel searches
	var wg sync.WaitGroup

	for _, file := range files {
		file = strings.TrimSpace(file)
		if file == "" {
			continue
		}

		wg.Add(1)
		go func(file string) {
			defer wg.Done()
			sem <- struct{}{}        // Acquire semaphore
			defer func() { <-sem }() // Release semaphore

			// Build search query from file path
			query := buildFileQuery(file)

			where := sqlitevec.BuildWhereFilter(sqlitevec.DocTypeObservation, "")
			vectorResults, vecErr := s.vectorClient.Query(ctx, query, limit*2, where)
			if vecErr != nil {
				log.Warn().Err(vecErr).Str("file", file).Msg("Vector search failed for file context")
				return
			}

			// Extract observation IDs from vector results
			obsIDs := sqlitevec.ExtractObservationIDs(vectorResults, project)
			if len(obsIDs) == 0 {
				return
			}

			// Fetch observations
			observations, err := s.observationStore.GetObservationsByIDs(ctx, obsIDs, "score_desc", limit*2)
			if err != nil {
				log.Warn().Err(err).Str("file", file).Msg("Failed to fetch observations for file context")
				return
			}

			// Pre-build score map from vector results (O(n) instead of O(n²))
			scoreMap := make(map[int64]float64, len(vectorResults))
			var avgScore float64
			for _, vr := range vectorResults {
				avgScore += vr.Similarity
				// Parse observation ID from vector result ID (format: obs_{id}_{field})
				// Use index-based parsing to avoid slice allocation from strings.Split
				if len(vr.ID) > 4 && vr.ID[:4] == "obs_" {
					rest := vr.ID[4:] // Skip "obs_"
					underscoreIdx := strings.IndexByte(rest, '_')
					var idStr string
					if underscoreIdx >= 0 {
						idStr = rest[:underscoreIdx]
					} else {
						idStr = rest
					}
					if id, parseErr := strconv.ParseInt(idStr, 10, 64); parseErr == nil {
						// Keep highest score for each observation
						if existing, exists := scoreMap[id]; !exists || vr.Similarity > existing {
							scoreMap[id] = vr.Similarity
						}
					}
				}
			}
			if len(vectorResults) > 0 {
				avgScore /= float64(len(vectorResults))
			}

			fileResults := make([]map[string]any, 0, limit)
			var usedIDs []int64
			for _, obs := range observations {
				// Check project scope
				if obs.Scope == "project" && obs.Project != project {
					continue
				}

				// O(1) score lookup instead of O(n) nested loop
				score, found := scoreMap[obs.ID]
				if !found {
					// Use average score as fallback
					score = avgScore
				}

				// Only include if score is above threshold
				if score < 0.3 {
					continue
				}

				fileResults = append(fileResults, map[string]any{
					"id":        obs.ID,
					"title":     obs.Title.String,
					"type":      obs.Type,
					"narrative": obs.Narrative.String,
					"facts":     obs.Facts,
					"score":     score,
				})
				usedIDs = append(usedIDs, obs.ID)

				if len(fileResults) >= limit {
					break
				}
			}

			if len(fileResults) > 0 {
				resultsChan <- fileResult{file: file, results: fileResults, obsIDs: usedIDs}
			}
		}(file)
	}

	// Close channel when all goroutines complete
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect results and deduplicate
	allResults := make(map[string]any)
	seenObservationIDs := make(map[int64]bool)

	for res := range resultsChan {
		// Filter out duplicates that were already seen in other files
		dedupedResults := make([]map[string]any, 0, len(res.results))
		for i, r := range res.results {
			obsID := res.obsIDs[i]
			if !seenObservationIDs[obsID] {
				seenObservationIDs[obsID] = true
				dedupedResults = append(dedupedResults, r)
			}
		}
		if len(dedupedResults) > 0 {
			allResults[res.file] = dedupedResults
		}
	}

	writeJSON(w, map[string]any{
		"files":   files,
		"results": allResults,
		"count":   len(allResults),
	})
}

// buildFileQuery extracts meaningful search terms from a file path.
func buildFileQuery(filePath string) string {
	// Remove common prefixes and extensions
	path := strings.TrimPrefix(filePath, "/")

	// Extract the filename and directory
	parts := strings.Split(path, "/")
	meaningful := make([]string, 0, len(parts))

	for _, part := range parts {
		// Skip common directory names that aren't meaningful
		switch strings.ToLower(part) {
		case "src", "lib", "internal", "pkg", "cmd", "api", "app", "test", "tests", "spec", "specs":
			continue
		default:
			// Remove file extension
			if idx := strings.LastIndex(part, "."); idx > 0 {
				part = part[:idx]
			}
			// Convert camelCase/PascalCase to spaces
			part = splitCamelCase(part)
			// Convert snake_case to spaces
			part = strings.ReplaceAll(part, "_", " ")
			// Convert kebab-case to spaces
			part = strings.ReplaceAll(part, "-", " ")
			meaningful = append(meaningful, part)
		}
	}

	return strings.Join(meaningful, " ")
}

// splitCamelCase splits camelCase or PascalCase into separate words.
func splitCamelCase(s string) string {
	var result strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result.WriteRune(' ')
		}
		result.WriteRune(r)
	}
	return result.String()
}

// handleContextInject returns context for injection at session start.
// IMPORTANT: This is on the critical startup path - must be fast!
// No synchronous verification - just filter by staleness and return.
func (s *Service) handleContextInject(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	if project == "" {
		http.Error(w, "project required", http.StatusBadRequest)
		return
	}

	// Validate project name to prevent path traversal
	if err := ValidateProjectName(project); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	cwd := r.URL.Query().Get("cwd")
	if cwd == "" {
		cwd = "/"
	}

	// Limit observations for fast startup (configurable, default 100)
	limit := s.config.ContextObservations
	if limit <= 0 {
		limit = DefaultContextLimit
	}

	// Full count determines how many observations get full detail (configurable, default 25)
	fullCount := s.config.ContextFullCount
	if fullCount <= 0 {
		fullCount = 25
	}

	// Get recent observations
	observations, err := s.observationStore.GetRecentObservations(r.Context(), project, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Fast staleness filter - NO verification (that's too slow for startup)
	var staleCount int
	freshObservations := make([]*models.Observation, 0, len(observations))

	for _, obs := range observations {
		if len(obs.FileMtimes) > 0 {
			var paths []string
			for path := range obs.FileMtimes {
				paths = append(paths, path)
			}
			currentMtimes := sdk.GetFileMtimes(paths, cwd)

			if obs.CheckStaleness(currentMtimes) {
				// Stale - exclude but don't verify (too slow)
				// Queue for background verification instead
				staleCount++
				s.queueStaleVerification(obs.ID, cwd)
				continue
			}
		}
		freshObservations = append(freshObservations, obs)
	}

	// Cluster similar observations to remove duplicates
	clusteredObservations := clusterObservations(freshObservations, 0.4)
	duplicatesRemoved := len(freshObservations) - len(clusteredObservations)

	// Record retrieval stats with staleness metrics
	s.recordRetrievalStatsExtended(project, int64(len(clusteredObservations)), 0, 0,
		int64(staleCount), int64(len(freshObservations)), int64(duplicatesRemoved), false)

	// Increment retrieval counts for scoring (async, non-blocking)
	if len(clusteredObservations) > 0 {
		ids := make([]int64, len(clusteredObservations))
		for i, obs := range clusteredObservations {
			ids[i] = obs.ID
		}
		s.incrementRetrievalCounts(ids)
	}

	log.Info().
		Str("project", project).
		Int("total", len(observations)).
		Int("fresh", len(freshObservations)).
		Int("clustered", len(clusteredObservations)).
		Int("duplicates", duplicatesRemoved).
		Int("stale_excluded", staleCount).
		Msg("Context injection with clustering")

	writeJSON(w, map[string]any{
		"project":            project,
		"observations":       clusteredObservations,
		"full_count":         fullCount,
		"stale_excluded":     staleCount,
		"duplicates_removed": duplicatesRemoved,
	})
}

// handleContextCount returns the count of observations for a project.
func (s *Service) handleContextCount(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	if project == "" {
		http.Error(w, "project required", http.StatusBadRequest)
		return
	}

	count, err := s.getCachedObservationCount(r.Context(), project)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]any{
		"project": project,
		"count":   count,
	})
}
