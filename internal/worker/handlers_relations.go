// Package worker provides the main worker service for claude-mnemonic.
package worker

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
)

// DefaultRelationsLimit is the default number of relations to return.
const DefaultRelationsLimit = 50

// handleGetRelations returns relations for an observation.
func (s *Service) handleGetRelations(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid observation id", http.StatusBadRequest)
		return
	}

	relations, err := s.relationStore.GetRelationsWithDetails(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if relations == nil {
		relations = []*models.RelationWithDetails{}
	}

	writeJSON(w, relations)
}

// handleGetRelationGraph returns the relation graph for an observation.
func (s *Service) handleGetRelationGraph(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid observation id", http.StatusBadRequest)
		return
	}

	// Get depth parameter (default 2)
	depth := 2
	if depthStr := r.URL.Query().Get("depth"); depthStr != "" {
		if d, err := strconv.Atoi(depthStr); err == nil && d > 0 && d <= 5 {
			depth = d
		}
	}

	graph, err := s.relationStore.GetRelationGraph(r.Context(), id, depth)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, graph)
}

// handleGetRelatedObservations returns observations related to a given one.
func (s *Service) handleGetRelatedObservations(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid observation id", http.StatusBadRequest)
		return
	}

	// Get minimum confidence parameter (default 0.4)
	minConfidence := 0.4
	if confStr := r.URL.Query().Get("min_confidence"); confStr != "" {
		if c, err := strconv.ParseFloat(confStr, 64); err == nil && c >= 0 && c <= 1 {
			minConfidence = c
		}
	}

	// Get related observation IDs
	relatedIDs, err := s.relationStore.GetRelatedObservationIDs(r.Context(), id, minConfidence)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if len(relatedIDs) == 0 {
		writeJSON(w, []*models.Observation{})
		return
	}

	// Fetch full observations
	observations, err := s.observationStore.GetObservationsByIDs(r.Context(), relatedIDs, "importance", 50)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if observations == nil {
		observations = []*models.Observation{}
	}

	writeJSON(w, observations)
}

// handleGetRelationsByType returns all relations of a specific type.
func (s *Service) handleGetRelationsByType(w http.ResponseWriter, r *http.Request) {
	relType := chi.URLParam(r, "type")

	// Validate relation type
	validType := false
	for _, t := range models.AllRelationTypes {
		if string(t) == relType {
			validType = true
			break
		}
	}
	if !validType {
		http.Error(w, "invalid relation type", http.StatusBadRequest)
		return
	}

	limit := DefaultRelationsLimit
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	relations, err := s.relationStore.GetRelationsByType(r.Context(), models.RelationType(relType), limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if relations == nil {
		relations = []*models.ObservationRelation{}
	}

	writeJSON(w, relations)
}

// handleGetRelationStats returns statistics about relations.
func (s *Service) handleGetRelationStats(w http.ResponseWriter, r *http.Request) {
	// Get total relation count
	totalCount, err := s.relationStore.GetTotalRelationCount(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Get high confidence relations count
	highConfRelations, err := s.relationStore.GetHighConfidenceRelations(r.Context(), 0.7, 1000)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Count by relation type
	typeCounts := make(map[string]int)
	for _, t := range models.AllRelationTypes {
		relations, err := s.relationStore.GetRelationsByType(r.Context(), t, 1000)
		if err == nil {
			typeCounts[string(t)] = len(relations)
		}
	}

	writeJSON(w, map[string]interface{}{
		"total_count":         totalCount,
		"high_confidence":     len(highConfRelations),
		"by_type":             typeCounts,
		"min_confidence_used": 0.4,
	})
}
