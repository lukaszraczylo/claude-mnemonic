// Package db defines database interfaces for the claude-mnemonic stores.
package db

import (
	"context"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
)

// ObservationReader defines read operations for observations.
type ObservationReader interface {
	GetObservationByID(ctx context.Context, id int64) (*models.Observation, error)
	GetObservationsByIDs(ctx context.Context, ids []int64, orderBy string, limit int) ([]*models.Observation, error)
	GetRecentObservations(ctx context.Context, project string, limit int) ([]*models.Observation, error)
	GetActiveObservations(ctx context.Context, project string, limit int) ([]*models.Observation, error)
	GetAllRecentObservations(ctx context.Context, limit int) ([]*models.Observation, error)
	GetAllObservations(ctx context.Context) ([]*models.Observation, error)
	SearchObservationsFTS(ctx context.Context, query, project string, limit int) ([]*models.Observation, error)
	GetObservationCount(ctx context.Context, project string) (int, error)
}

// ObservationWriter defines write operations for observations.
type ObservationWriter interface {
	StoreObservation(ctx context.Context, sdkSessionID, project string, obs *models.ParsedObservation, promptNumber int, discoveryTokens int64) (int64, int64, error)
	DeleteObservations(ctx context.Context, ids []int64) (int64, error)
}

// ObservationStore combines read and write operations for observations.
type ObservationStore interface {
	ObservationReader
	ObservationWriter
}

// SummaryReader defines read operations for summaries.
type SummaryReader interface {
	GetSummariesByIDs(ctx context.Context, ids []int64, orderBy string, limit int) ([]*models.SessionSummary, error)
	GetRecentSummaries(ctx context.Context, project string, limit int) ([]*models.SessionSummary, error)
	GetAllRecentSummaries(ctx context.Context, limit int) ([]*models.SessionSummary, error)
	GetAllSummaries(ctx context.Context) ([]*models.SessionSummary, error)
}

// SummaryWriter defines write operations for summaries.
type SummaryWriter interface {
	StoreSummary(ctx context.Context, sdkSessionID, project string, summary *models.ParsedSummary, promptNumber int, discoveryTokens int64) (int64, int64, error)
}

// SummaryStore combines read and write operations for summaries.
type SummaryStore interface {
	SummaryReader
	SummaryWriter
}

// PromptReader defines read operations for prompts.
type PromptReader interface {
	GetPromptsByIDs(ctx context.Context, ids []int64, orderBy string, limit int) ([]*models.UserPromptWithSession, error)
	GetAllRecentUserPrompts(ctx context.Context, limit int) ([]*models.UserPromptWithSession, error)
	GetAllPrompts(ctx context.Context) ([]*models.UserPromptWithSession, error)
	GetRecentUserPromptsByProject(ctx context.Context, project string, limit int) ([]*models.UserPromptWithSession, error)
	FindRecentPromptByText(ctx context.Context, claudeSessionID, promptText string, withinSeconds int) (int64, int, bool)
}

// PromptWriter defines write operations for prompts.
type PromptWriter interface {
	SaveUserPromptWithMatches(ctx context.Context, claudeSessionID string, promptNumber int, promptText string, matchedObservations int) (int64, error)
}

// PromptStore combines read and write operations for prompts.
type PromptStore interface {
	PromptReader
	PromptWriter
}
