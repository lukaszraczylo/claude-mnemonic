// Package worker provides data retrieval HTTP handlers.
package worker

import (
	"encoding/json"
	"net/http"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/lukaszraczylo/claude-mnemonic/internal/db/gorm"
	"github.com/lukaszraczylo/claude-mnemonic/internal/embedding"
	"github.com/lukaszraczylo/claude-mnemonic/internal/vector/sqlitevec"
	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
	"github.com/rs/zerolog/log"
)

// handleGetObservations returns recent observations.
// Supports optional query parameter for semantic search via sqlite-vec.
// Supports pagination via limit and offset query parameters.
func (s *Service) handleGetObservations(w http.ResponseWriter, r *http.Request) {
	pagination := gorm.ParsePaginationParams(r, DefaultObservationsLimit)
	project := r.URL.Query().Get("project")
	query := r.URL.Query().Get("query")

	// Validate project name to prevent path traversal
	if err := ValidateProjectName(project); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var observations []*models.Observation
	var total int64
	var err error
	var usedVector bool

	// Use vector search if query is provided and vector client is available
	if query != "" && s.vectorClient != nil && s.vectorClient.IsConnected() {
		where := sqlitevec.BuildWhereFilter(sqlitevec.DocTypeObservation, "")
		vectorResults, vecErr := s.vectorClient.Query(r.Context(), query, pagination.Limit*2, where)
		if vecErr == nil && len(vectorResults) > 0 {
			obsIDs := sqlitevec.ExtractObservationIDs(vectorResults, project)
			if len(obsIDs) > 0 {
				observations, err = s.observationStore.GetObservationsByIDs(r.Context(), obsIDs, "date_desc", pagination.Limit)
				if err == nil {
					usedVector = true
					total = int64(len(observations)) // Vector search doesn't have total, use returned count
				}
			}
		}
	}

	// Fall back to SQLite if vector search not used
	if !usedVector {
		if project != "" {
			// Strict project filtering for dashboard - only observations from this project
			observations, total, err = s.observationStore.GetObservationsByProjectStrictPaginated(r.Context(), project, pagination.Limit, pagination.Offset)
		} else {
			// All projects
			observations, total, err = s.observationStore.GetAllRecentObservationsPaginated(r.Context(), pagination.Limit, pagination.Offset)
		}
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Ensure we return empty array, not null
	if observations == nil {
		observations = []*models.Observation{}
	}

	// Track search if query was provided
	if query != "" {
		s.trackSearchQuery(query, project, "observations", len(observations), usedVector)
	}

	// Return paginated response
	writeJSON(w, map[string]any{
		"observations": observations,
		"total":        total,
		"limit":        pagination.Limit,
		"offset":       pagination.Offset,
		"hasMore":      int64(pagination.Offset)+int64(len(observations)) < total,
	})
}

// handleGetSummaries returns recent summaries.
// Supports optional query parameter for semantic search via sqlite-vec.
func (s *Service) handleGetSummaries(w http.ResponseWriter, r *http.Request) {
	limit := gorm.ParseLimitParam(r, DefaultSummariesLimit)
	project := r.URL.Query().Get("project")
	query := r.URL.Query().Get("query")

	// Validate project name to prevent path traversal
	if err := ValidateProjectName(project); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var summaries []*models.SessionSummary
	var err error
	var usedVector bool

	// Use vector search if query is provided and vector client is available
	if query != "" && s.vectorClient != nil && s.vectorClient.IsConnected() {
		where := sqlitevec.BuildWhereFilter(sqlitevec.DocTypeSessionSummary, "")
		vectorResults, vecErr := s.vectorClient.Query(r.Context(), query, limit*2, where)
		if vecErr == nil && len(vectorResults) > 0 {
			summaryIDs := sqlitevec.ExtractSummaryIDs(vectorResults, project)
			if len(summaryIDs) > 0 {
				summaries, err = s.summaryStore.GetSummariesByIDs(r.Context(), summaryIDs, "date_desc", limit)
				if err == nil {
					usedVector = true
				}
			}
		}
	}

	// Fall back to SQLite if vector search not used
	if !usedVector {
		if project != "" {
			summaries, err = s.summaryStore.GetRecentSummaries(r.Context(), project, limit)
		} else {
			summaries, err = s.summaryStore.GetAllRecentSummaries(r.Context(), limit)
		}
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Ensure we return empty array, not null
	if summaries == nil {
		summaries = []*models.SessionSummary{}
	}
	writeJSON(w, summaries)
}

// handleGetPrompts returns recent user prompts.
// Supports optional query parameter for semantic search via sqlite-vec.
func (s *Service) handleGetPrompts(w http.ResponseWriter, r *http.Request) {
	limit := gorm.ParseLimitParam(r, DefaultPromptsLimit)
	project := r.URL.Query().Get("project")
	query := r.URL.Query().Get("query")

	// Validate project name to prevent path traversal
	if err := ValidateProjectName(project); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var prompts []*models.UserPromptWithSession
	var err error
	var usedVector bool

	// Use vector search if query is provided and vector client is available
	if query != "" && s.vectorClient != nil && s.vectorClient.IsConnected() {
		where := sqlitevec.BuildWhereFilter(sqlitevec.DocTypeUserPrompt, "")
		vectorResults, vecErr := s.vectorClient.Query(r.Context(), query, limit*2, where)
		if vecErr == nil && len(vectorResults) > 0 {
			promptIDs := sqlitevec.ExtractPromptIDs(vectorResults, project)
			if len(promptIDs) > 0 {
				prompts, err = s.promptStore.GetPromptsByIDs(r.Context(), promptIDs, "date_desc", limit)
				if err == nil {
					usedVector = true
				}
			}
		}
	}

	// Fall back to SQLite if vector search not used
	if !usedVector {
		if project != "" {
			prompts, err = s.promptStore.GetRecentUserPromptsByProject(r.Context(), project, limit)
		} else {
			prompts, err = s.promptStore.GetAllRecentUserPrompts(r.Context(), limit)
		}
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Ensure we return empty array, not null
	if prompts == nil {
		prompts = []*models.UserPromptWithSession{}
	}
	writeJSON(w, prompts)
}

// handleGetProjects returns all projects.
// Response is cacheable for 5 minutes since project list changes infrequently.
func (s *Service) handleGetProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := s.sessionStore.GetAllProjects(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Cache for 5 minutes - project list changes infrequently
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, projects)
}

// handleGetTypes returns the canonical list of observation and concept types.
// This provides a single source of truth for both backend and frontend.
// Response is cacheable as these values never change at runtime.
func (s *Service) handleGetTypes(w http.ResponseWriter, r *http.Request) {
	// Cache for 24 hours - these values are compile-time constants
	w.Header().Set("Cache-Control", "public, max-age=86400")
	writeJSON(w, map[string]any{
		"observation_types": ObservationTypes,
		"concept_types":     ConceptTypes,
	})
}

// handleGetModels returns available embedding models.
// Response is cacheable as model list doesn't change without restart.
func (s *Service) handleGetModels(w http.ResponseWriter, _ *http.Request) {
	// Cache for 1 hour - model list is static during runtime
	w.Header().Set("Cache-Control", "public, max-age=3600")

	models := embedding.ListModels()
	defaultModel := embedding.GetDefaultModel()

	writeJSON(w, map[string]any{
		"models":  models,
		"default": defaultModel,
		"current": s.embedSvc.Version(),
	})
}

// handleGetStats returns worker statistics.
func (s *Service) handleGetStats(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")

	// Validate project name to prevent path traversal
	if err := ValidateProjectName(project); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	retrievalStats := s.GetRetrievalStats(project)
	sessionsToday, _ := s.sessionStore.GetSessionsToday(r.Context())

	response := map[string]any{
		"uptime":           time.Since(s.startTime).String(),
		"uptimeSeconds":    time.Since(s.startTime).Seconds(),
		"activeSessions":   s.sessionManager.GetActiveSessionCount(),
		"queueDepth":       s.sessionManager.GetTotalQueueDepth(),
		"isProcessing":     s.sessionManager.IsAnySessionProcessing(),
		"connectedClients": s.sseBroadcaster.ClientCount(),
		"sessionsToday":    sessionsToday,
		"retrieval":        retrievalStats,
		"ready":            s.ready.Load(),
	}

	// Add memory stats
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	response["memory"] = map[string]any{
		"alloc_mb":          float64(memStats.Alloc) / 1024 / 1024,
		"total_alloc_mb":    float64(memStats.TotalAlloc) / 1024 / 1024,
		"sys_mb":            float64(memStats.Sys) / 1024 / 1024,
		"heap_alloc_mb":     float64(memStats.HeapAlloc) / 1024 / 1024,
		"heap_inuse_mb":     float64(memStats.HeapInuse) / 1024 / 1024,
		"heap_objects":      memStats.HeapObjects,
		"goroutines":        runtime.NumGoroutine(),
		"gc_cycles":         memStats.NumGC,
		"gc_pause_total_ms": float64(memStats.PauseTotalNs) / 1e6,
	}

	// Add database health if available
	if s.store != nil {
		dbHealth := s.store.HealthCheck(r.Context())
		response["database"] = map[string]any{
			"status":           dbHealth.Status,
			"query_latency_ms": float64(dbHealth.QueryLatency) / 1e6,
			"pool":             dbHealth.PoolStats,
			"warning":          dbHealth.Warning,
		}
	}

	// Add embedding model info
	if s.embedSvc != nil {
		response["embeddingModel"] = map[string]any{
			"name":       s.embedSvc.Name(),
			"version":    s.embedSvc.Version(),
			"dimensions": s.embedSvc.Dimensions(),
		}
	}

	// Add vector cache stats
	if s.vectorClient != nil {
		if count, err := s.vectorClient.Count(r.Context()); err == nil {
			response["vectorCount"] = count
		}
		cacheSize, cacheMax := s.vectorClient.CacheStats()
		response["vectorCache"] = map[string]any{
			"size":     cacheSize,
			"max_size": cacheMax,
		}
	}

	// Include project-specific observation count if project is specified
	if project != "" {
		count, err := s.getCachedObservationCount(r.Context(), project)
		if err == nil {
			response["projectObservations"] = count
			response["project"] = project
		}
	}

	// Add rate limiter stats
	if s.rateLimiter != nil {
		response["rateLimiter"] = s.rateLimiter.Stats()
	}

	// Add circuit breaker metrics
	if s.processor != nil {
		response["circuitBreaker"] = s.processor.CircuitBreakerMetrics()
	}

	writeJSON(w, response)
}

// handleGetRetrievalStats returns detailed retrieval statistics.
func (s *Service) handleGetRetrievalStats(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	stats := s.GetRetrievalStats(project)
	writeJSON(w, stats)
}

// handleGetRecentQueries returns recent search queries for analytics.
func (s *Service) handleGetRecentQueries(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	limit := gorm.ParseLimitParam(r, 20)

	queries := s.getRecentSearchQueries(project, limit)

	writeJSON(w, map[string]any{
		"queries": queries,
		"count":   len(queries),
		"project": project,
	})
}

// handleGetSearchAnalytics returns comprehensive search analytics and statistics.
func (s *Service) handleGetSearchAnalytics(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")

	// Get all recent queries for analysis
	queries := s.getRecentSearchQueries(project, maxRecentQueries)

	// Calculate analytics
	totalQueries := len(queries)
	vectorSearches := 0
	totalResults := 0
	zeroResultQueries := 0
	queryTypes := make(map[string]int)
	topKeywords := make(map[string]int)

	for _, q := range queries {
		if q.UsedVector {
			vectorSearches++
		}
		totalResults += q.Results
		if q.Results == 0 {
			zeroResultQueries++
		}
		queryTypes[q.Type]++

		// Extract keywords (simple word tokenization using iterator)
		for word := range strings.FieldsSeq(strings.ToLower(q.Query)) {
			if len(word) > 3 { // Skip short words
				topKeywords[word]++
			}
		}
	}

	// Sort keywords by frequency
	type keywordCount struct {
		Keyword string `json:"keyword"`
		Count   int    `json:"count"`
	}
	sortedKeywords := make([]keywordCount, 0, len(topKeywords))
	for kw, count := range topKeywords {
		sortedKeywords = append(sortedKeywords, keywordCount{Keyword: kw, Count: count})
	}
	sort.Slice(sortedKeywords, func(i, j int) bool {
		return sortedKeywords[i].Count > sortedKeywords[j].Count
	})
	if len(sortedKeywords) > 10 {
		sortedKeywords = sortedKeywords[:10]
	}

	// Calculate averages
	avgResults := float64(0)
	vectorSearchRate := float64(0)
	zeroResultRate := float64(0)
	if totalQueries > 0 {
		avgResults = float64(totalResults) / float64(totalQueries)
		vectorSearchRate = float64(vectorSearches) / float64(totalQueries) * 100
		zeroResultRate = float64(zeroResultQueries) / float64(totalQueries) * 100
	}

	writeJSON(w, map[string]any{
		"total_queries":      totalQueries,
		"vector_search_rate": vectorSearchRate,
		"avg_results":        avgResults,
		"zero_result_rate":   zeroResultRate,
		"query_types":        queryTypes,
		"top_keywords":       sortedKeywords,
		"project":            project,
	})
}

// handleVectorHealth returns comprehensive health information about the vector database.
func (s *Service) handleVectorHealth(w http.ResponseWriter, r *http.Request) {
	if s.vectorClient == nil {
		http.Error(w, "vector client not initialized", http.StatusServiceUnavailable)
		return
	}

	stats, err := s.vectorClient.GetHealthStats(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Add additional computed metrics
	healthScore := 100.0
	var warnings []string

	// Penalize for stale vectors
	if stats.TotalVectors > 0 {
		staleRatio := float64(stats.StaleVectors) / float64(stats.TotalVectors)
		if staleRatio > 0 {
			healthScore -= staleRatio * 50 // Up to 50 points off for stale vectors
			warnings = append(warnings, formatWarning("%.1f%% vectors need rebuild", staleRatio*100))
		}
	}

	// Check cache effectiveness
	cacheHitRate := stats.EmbeddingCache.HitRate()
	if cacheHitRate < 20 && (stats.EmbeddingCache.EmbeddingHits+stats.EmbeddingCache.EmbeddingMisses) > 100 {
		healthScore -= 10
		warnings = append(warnings, formatWarning("Low cache hit rate: %.1f%%", cacheHitRate))
	}

	// Penalize if rebuild is needed
	if stats.NeedsRebuild {
		healthScore -= 20
		warnings = append(warnings, "Vector rebuild recommended: "+stats.RebuildReason)
	}

	if healthScore < 0 {
		healthScore = 0
	}

	status := "healthy"
	if healthScore < 50 {
		status = "unhealthy"
	} else if healthScore < 80 {
		status = "degraded"
	}

	writeJSON(w, map[string]any{
		"status":         status,
		"health_score":   healthScore,
		"warnings":       warnings,
		"stats":          stats,
		"cache_hit_rate": cacheHitRate,
	})
}

// UpdateObservationRequest is the request body for updating an observation.
type UpdateObservationRequest struct {
	Title         *string  `json:"title,omitempty"`
	Subtitle      *string  `json:"subtitle,omitempty"`
	Narrative     *string  `json:"narrative,omitempty"`
	Scope         *string  `json:"scope,omitempty"`
	Facts         []string `json:"facts,omitempty"`
	Concepts      []string `json:"concepts,omitempty"`
	FilesRead     []string `json:"files_read,omitempty"`
	FilesModified []string `json:"files_modified,omitempty"`
}

// handleUpdateObservation updates an existing observation.
// PUT /api/observations/{id}
func (s *Service) handleUpdateObservation(w http.ResponseWriter, r *http.Request) {
	// Parse observation ID from URL
	id, ok := parseIDParam(w, r.PathValue("id"), "observation")
	if !ok {
		return
	}

	// Parse request body
	var req UpdateObservationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Build update struct - only include fields that were provided
	update := &gorm.ObservationUpdate{}

	if req.Title != nil {
		update.Title = req.Title
	}
	if req.Subtitle != nil {
		update.Subtitle = req.Subtitle
	}
	if req.Narrative != nil {
		update.Narrative = req.Narrative
	}
	if req.Facts != nil {
		update.Facts = &req.Facts
	}
	if req.Concepts != nil {
		update.Concepts = &req.Concepts
	}
	if req.FilesRead != nil {
		update.FilesRead = &req.FilesRead
	}
	if req.FilesModified != nil {
		update.FilesModified = &req.FilesModified
	}
	if req.Scope != nil {
		// Validate scope
		if *req.Scope != "project" && *req.Scope != "global" {
			http.Error(w, "scope must be 'project' or 'global'", http.StatusBadRequest)
			return
		}
		update.Scope = req.Scope
	}

	// Update the observation
	updatedObs, err := s.observationStore.UpdateObservation(r.Context(), id, update)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, "failed to update observation: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Trigger vector resync for the updated observation
	if s.vectorSync != nil {
		s.asyncVectorSync(func() {
			if err := s.vectorSync.SyncObservation(s.ctx, updatedObs); err != nil {
				log.Warn().Err(err).Int64("id", id).Msg("Failed to resync observation vectors after update")
			}
		})
	}

	// Broadcast update event
	s.sseBroadcaster.Broadcast(map[string]any{
		"type": "observation_updated",
		"id":   id,
	})

	writeJSON(w, map[string]any{
		"observation": updatedObs,
		"message":     "observation updated successfully",
	})
}

// handleGetObservationByID returns a single observation by ID.
// GET /api/observations/{id}
func (s *Service) handleGetObservationByID(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r.PathValue("id"), "observation")
	if !ok {
		return
	}

	obs, err := s.observationStore.GetObservationByID(r.Context(), id)
	if err != nil {
		http.Error(w, "failed to get observation: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if obs == nil {
		http.Error(w, "observation not found", http.StatusNotFound)
		return
	}

	writeJSON(w, obs)
}

// handleGraphStats returns graph statistics for the dashboard.
// Uses relation data to compute knowledge graph metrics.
func (s *Service) handleGraphStats(w http.ResponseWriter, r *http.Request) {
	// Get relation count (edges) - this represents the knowledge graph
	edgeCount, err := s.relationStore.GetTotalRelationCount(r.Context())
	if err != nil {
		edgeCount = 0
	}

	// Count by relation type
	edgeTypes := make(map[string]int)
	for _, t := range models.AllRelationTypes {
		relations, err := s.relationStore.GetRelationsByType(r.Context(), t, 10000)
		if err == nil {
			edgeTypes[string(t)] = len(relations)
		}
	}

	// Get unique observation IDs involved in relations (approximate node count)
	// For now, use edge count as a proxy - each edge has 2 nodes
	nodeCount := 0
	if edgeCount > 0 {
		// Rough estimate: unique nodes ≈ edges * 1.5 (since nodes can have multiple edges)
		nodeCount = int(float64(edgeCount) * 1.5)
	}

	// Calculate average degree
	var avgDegree float64
	if nodeCount > 0 {
		avgDegree = float64(edgeCount*2) / float64(nodeCount)
	}

	// Graph is enabled if we have any edges (relations)
	enabled := edgeCount > 0

	writeJSON(w, map[string]any{
		"enabled":      enabled,
		"nodeCount":    nodeCount,
		"edgeCount":    edgeCount,
		"avgDegree":    avgDegree,
		"maxDegree":    0,
		"minDegree":    0,
		"medianDegree": 0.0,
		"edgeTypes":    edgeTypes,
		"config": map[string]any{
			"maxHops":            2,
			"branchFactor":       10,
			"edgeWeight":         0.3,
			"rebuildIntervalMin": 30,
		},
	})
}

// handleVectorMetrics returns vector database metrics for the dashboard.
// Returns enabled: false if vector features are not available.
func (s *Service) handleVectorMetrics(w http.ResponseWriter, r *http.Request) {
	if s.vectorClient == nil {
		writeJSON(w, map[string]any{
			"enabled": false,
			"message": "Vector database not initialized",
		})
		return
	}

	// Get cache stats from vector client
	cacheSize, cacheMax := s.vectorClient.CacheStats()
	cacheStats := s.vectorClient.GetCacheStats()
	count, _ := s.vectorClient.Count(r.Context())

	uptime := time.Since(s.startTime).Round(time.Second).String()

	// Calculate total queries from cache hits/misses
	totalQueries := cacheStats.EmbeddingHits + cacheStats.EmbeddingMisses + cacheStats.ResultHits + cacheStats.ResultMisses
	totalHits := cacheStats.EmbeddingHits + cacheStats.ResultHits
	totalMisses := cacheStats.EmbeddingMisses + cacheStats.ResultMisses

	writeJSON(w, map[string]any{
		"enabled": true,
		"queries": map[string]any{
			"total":    totalQueries,
			"hubOnly":  0,
			"hybrid":   0,
			"onDemand": 0,
			"graph":    0,
		},
		"latency": map[string]any{
			"avg":          "0ms",
			"p50":          "0ms",
			"p95":          "0ms",
			"p99":          "0ms",
			"avgHub":       "0ms",
			"avgRecompute": "0ms",
		},
		"storage": map[string]any{
			"totalDocuments":   count,
			"hubDocuments":     0,
			"storedEmbeddings": count,
			"savingsPercent":   0.0,
			"recomputedTotal":  0,
		},
		"cache": map[string]any{
			"hits":    totalHits,
			"misses":  totalMisses,
			"hitRate": cacheStats.HitRate(),
			"size":    cacheSize,
			"maxSize": cacheMax,
		},
		"graph": map[string]any{
			"traversals": 0,
			"avgDepth":   0.0,
		},
		"uptime": uptime,
	})
}
