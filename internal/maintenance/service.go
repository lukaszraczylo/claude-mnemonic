// Package maintenance provides scheduled maintenance tasks for claude-mnemonic.
package maintenance

import (
	"context"
	"sync"
	"time"

	"github.com/lukaszraczylo/claude-mnemonic/internal/config"
	"github.com/lukaszraczylo/claude-mnemonic/internal/db/gorm"
	"github.com/rs/zerolog"
)

// Service handles scheduled maintenance tasks.
type Service struct {
	log              zerolog.Logger
	lastRunTime      time.Time
	promptStore      *gorm.PromptStore
	store            *gorm.Store
	vectorCleanupFn  func(ctx context.Context, deletedIDs []int64)
	config           *config.Config
	summaryStore     *gorm.SummaryStore
	stopCh           chan struct{}
	doneCh           chan struct{}
	observationStore *gorm.ObservationStore
	lastRunDuration  time.Duration
	totalCleanedObs  int64
	totalOptimizeRun int64
	mu               sync.Mutex
	running          bool
}

// NewService creates a new maintenance service.
func NewService(
	store *gorm.Store,
	observationStore *gorm.ObservationStore,
	summaryStore *gorm.SummaryStore,
	promptStore *gorm.PromptStore,
	vectorCleanupFn func(ctx context.Context, deletedIDs []int64),
	cfg *config.Config,
	log zerolog.Logger,
) *Service {
	return &Service{
		store:            store,
		observationStore: observationStore,
		summaryStore:     summaryStore,
		promptStore:      promptStore,
		vectorCleanupFn:  vectorCleanupFn,
		config:           cfg,
		log:              log.With().Str("component", "maintenance").Logger(),
		stopCh:           make(chan struct{}),
		doneCh:           make(chan struct{}),
	}
}

// Start begins the maintenance loop.
func (s *Service) Start(ctx context.Context) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
		close(s.doneCh)
	}()

	if !s.config.MaintenanceEnabled {
		s.log.Info().Msg("Maintenance disabled, not starting scheduler")
		return
	}

	interval := max(time.Duration(s.config.MaintenanceIntervalHours)*time.Hour, time.Hour)

	s.log.Info().
		Dur("interval", interval).
		Int("retention_days", s.config.ObservationRetentionDays).
		Bool("cleanup_stale", s.config.CleanupStaleObservations).
		Msg("Starting maintenance scheduler")

	// Initial run after 5 minutes (allow system to stabilize)
	time.Sleep(5 * time.Minute)
	s.runMaintenance(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.log.Info().Msg("Maintenance shutting down due to context cancellation")
			return
		case <-s.stopCh:
			s.log.Info().Msg("Maintenance shutting down due to stop signal")
			return
		case <-ticker.C:
			s.runMaintenance(ctx)
		}
	}
}

// Stop signals the maintenance service to stop.
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return
	}

	close(s.stopCh)
}

// Wait waits for the maintenance service to finish.
func (s *Service) Wait() {
	<-s.doneCh
}

// runMaintenance executes all maintenance tasks.
func (s *Service) runMaintenance(ctx context.Context) {
	start := time.Now()
	s.log.Info().Msg("Starting maintenance run")

	var totalCleaned int64

	// Task 1: Clean up old observations by age
	if s.config.ObservationRetentionDays > 0 {
		cleaned, err := s.cleanupOldObservations(ctx)
		if err != nil {
			s.log.Error().Err(err).Msg("Failed to cleanup old observations")
		} else {
			totalCleaned += cleaned
			s.log.Info().Int64("cleaned", cleaned).Msg("Cleaned old observations by age")
		}
	}

	// Task 2: Clean up stale observations
	if s.config.CleanupStaleObservations {
		cleaned, err := s.cleanupStaleObservations(ctx)
		if err != nil {
			s.log.Error().Err(err).Msg("Failed to cleanup stale observations")
		} else {
			totalCleaned += cleaned
			s.log.Info().Int64("cleaned", cleaned).Msg("Cleaned stale observations")
		}
	}

	// Task 3: Optimize database
	if err := s.store.Optimize(ctx); err != nil {
		s.log.Error().Err(err).Msg("Failed to optimize database")
	} else {
		s.totalOptimizeRun++
	}

	// Task 4: Clean up old prompts (keep last 1000 per session)
	cleanedPrompts, err := s.cleanupOldPrompts(ctx)
	if err != nil {
		s.log.Error().Err(err).Msg("Failed to cleanup old prompts")
	} else if cleanedPrompts > 0 {
		s.log.Info().Int64("cleaned", cleanedPrompts).Msg("Cleaned old prompts")
	}

	// Update metrics
	s.mu.Lock()
	s.lastRunTime = time.Now()
	s.lastRunDuration = time.Since(start)
	s.totalCleanedObs += totalCleaned
	s.mu.Unlock()

	s.log.Info().
		Dur("duration", time.Since(start)).
		Int64("observations_cleaned", totalCleaned).
		Msg("Maintenance run completed")
}

// cleanupOldObservations deletes observations older than the retention period.
func (s *Service) cleanupOldObservations(ctx context.Context) (int64, error) {
	cutoffEpoch := time.Now().AddDate(0, 0, -s.config.ObservationRetentionDays).Unix()

	// Get IDs of old observations
	var deletedIDs []int64
	err := s.store.GetDB().WithContext(ctx).
		Model(&gorm.Observation{}).
		Where("created_at_epoch < ?", cutoffEpoch).
		Pluck("id", &deletedIDs).Error
	if err != nil {
		return 0, err
	}

	if len(deletedIDs) == 0 {
		return 0, nil
	}

	// Delete in batches to avoid long transactions
	batchSize := 100
	for i := 0; i < len(deletedIDs); i += batchSize {
		end := min(i+batchSize, len(deletedIDs))
		batch := deletedIDs[i:end]

		if err := s.store.GetDB().WithContext(ctx).
			Where("id IN ?", batch).
			Delete(&gorm.Observation{}).Error; err != nil {
			return int64(i), err
		}

		// Sync vector DB deletions
		if s.vectorCleanupFn != nil {
			s.vectorCleanupFn(ctx, batch)
		}
	}

	return int64(len(deletedIDs)), nil
}

// cleanupStaleObservations deletes observations marked as stale.
func (s *Service) cleanupStaleObservations(ctx context.Context) (int64, error) {
	// Get IDs of stale observations (is_superseded = true)
	var deletedIDs []int64
	err := s.store.GetDB().WithContext(ctx).
		Model(&gorm.Observation{}).
		Where("is_superseded = ?", true).
		Pluck("id", &deletedIDs).Error
	if err != nil {
		return 0, err
	}

	if len(deletedIDs) == 0 {
		return 0, nil
	}

	// Delete in batches
	batchSize := 100
	for i := 0; i < len(deletedIDs); i += batchSize {
		end := min(i+batchSize, len(deletedIDs))
		batch := deletedIDs[i:end]

		if err := s.store.GetDB().WithContext(ctx).
			Where("id IN ?", batch).
			Delete(&gorm.Observation{}).Error; err != nil {
			return int64(i), err
		}

		// Sync vector DB deletions
		if s.vectorCleanupFn != nil {
			s.vectorCleanupFn(ctx, batch)
		}
	}

	return int64(len(deletedIDs)), nil
}

// cleanupOldPrompts removes old prompts keeping only the most recent per session.
func (s *Service) cleanupOldPrompts(ctx context.Context) (int64, error) {
	// Delete prompts older than 30 days that aren't the most recent in their session
	cutoffEpoch := time.Now().AddDate(0, 0, -30).Unix()

	result := s.store.GetDB().WithContext(ctx).
		Where("created_at_epoch < ?", cutoffEpoch).
		Delete(&gorm.UserPrompt{})

	return result.RowsAffected, result.Error
}

// Stats returns maintenance statistics.
func (s *Service) Stats() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()

	return map[string]any{
		"enabled":           s.config.MaintenanceEnabled,
		"interval_hours":    s.config.MaintenanceIntervalHours,
		"retention_days":    s.config.ObservationRetentionDays,
		"cleanup_stale":     s.config.CleanupStaleObservations,
		"last_run":          s.lastRunTime,
		"last_duration_ms":  s.lastRunDuration.Milliseconds(),
		"total_cleaned_obs": s.totalCleanedObs,
		"total_optimizes":   s.totalOptimizeRun,
		"running":           s.running,
	}
}

// RunNow triggers an immediate maintenance run.
func (s *Service) RunNow(ctx context.Context) {
	go s.runMaintenance(ctx)
}
