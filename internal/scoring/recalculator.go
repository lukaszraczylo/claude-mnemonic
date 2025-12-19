// Package scoring provides importance score calculation for observations.
package scoring

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/lukaszraczylo/claude-mnemonic/internal/db/sqlite"
	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
)

// ObservationStore defines the interface for observation storage operations needed by the recalculator.
type ObservationStore interface {
	GetObservationsNeedingScoreUpdate(ctx context.Context, threshold time.Duration, limit int) ([]*models.Observation, error)
	UpdateImportanceScores(ctx context.Context, scores map[int64]float64) error
	GetConceptWeights(ctx context.Context) (map[string]float64, error)
}

// Recalculator periodically recalculates importance scores for observations.
type Recalculator struct {
	store      ObservationStore
	calculator *Calculator
	log        zerolog.Logger
	interval   time.Duration
	batchSize  int
	stopCh     chan struct{}
	doneCh     chan struct{}
	mu         sync.Mutex
	running    bool
}

// NewRecalculator creates a new background recalculator.
func NewRecalculator(store ObservationStore, calc *Calculator, log zerolog.Logger) *Recalculator {
	return &Recalculator{
		store:      store,
		calculator: calc,
		log:        log.With().Str("component", "recalculator").Logger(),
		interval:   1 * time.Hour, // Run every hour
		batchSize:  500,           // Process 500 observations at a time
		stopCh:     make(chan struct{}),
		doneCh:     make(chan struct{}),
	}
}

// Start begins the background recalculation loop.
// This should be called in a goroutine.
func (r *Recalculator) Start(ctx context.Context) {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return
	}
	r.running = true
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		r.running = false
		r.mu.Unlock()
		close(r.doneCh)
	}()

	// Initial run
	r.recalculate(ctx)

	r.mu.Lock()
	interval := r.interval
	r.mu.Unlock()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			r.log.Info().Msg("recalculator shutting down due to context cancellation")
			return
		case <-r.stopCh:
			r.log.Info().Msg("recalculator stopping")
			return
		case <-ticker.C:
			r.recalculate(ctx)
		}
	}
}

// Stop stops the background recalculation loop.
func (r *Recalculator) Stop() {
	r.mu.Lock()
	if !r.running {
		r.mu.Unlock()
		return
	}
	r.mu.Unlock()

	close(r.stopCh)
	<-r.doneCh
}

// recalculate performs a single recalculation batch.
func (r *Recalculator) recalculate(ctx context.Context) {
	now := time.Now()
	threshold := r.calculator.RecalculateThreshold()

	r.mu.Lock()
	batchSize := r.batchSize
	r.mu.Unlock()

	observations, err := r.store.GetObservationsNeedingScoreUpdate(ctx, threshold, batchSize)
	if err != nil {
		r.log.Error().Err(err).Msg("failed to get observations for score update")
		return
	}

	if len(observations) == 0 {
		return
	}

	scores := r.calculator.BatchCalculate(observations, now)

	if err := r.store.UpdateImportanceScores(ctx, scores); err != nil {
		r.log.Error().Err(err).Msg("failed to update importance scores")
		return
	}

	r.log.Info().
		Int("count", len(scores)).
		Dur("elapsed", time.Since(now)).
		Msg("recalculated importance scores")
}

// RecalculateNow triggers an immediate recalculation.
// This is useful for testing or when scores need to be updated urgently.
func (r *Recalculator) RecalculateNow(ctx context.Context) error {
	r.recalculate(ctx)
	return nil
}

// RefreshConceptWeights reloads concept weights from the database.
// Call this after updating concept weights to apply changes.
func (r *Recalculator) RefreshConceptWeights(ctx context.Context) error {
	weights, err := r.store.GetConceptWeights(ctx)
	if err != nil {
		return err
	}

	config := r.calculator.GetConfig()
	config.ConceptWeights = weights
	r.calculator.UpdateConfig(config)

	r.log.Info().Int("count", len(weights)).Msg("refreshed concept weights")
	return nil
}

// Stats returns statistics about the recalculator.
type Stats struct {
	Running     bool          `json:"running"`
	Interval    time.Duration `json:"interval"`
	BatchSize   int           `json:"batch_size"`
	HalfLife    float64       `json:"half_life_days"`
	MinScore    float64       `json:"min_score"`
	ConceptsLen int           `json:"concepts_count"`
}

// GetStats returns current recalculator statistics.
func (r *Recalculator) GetStats() Stats {
	r.mu.Lock()
	defer r.mu.Unlock()

	config := r.calculator.GetConfig()

	return Stats{
		Running:     r.running,
		Interval:    r.interval,
		BatchSize:   r.batchSize,
		HalfLife:    config.RecencyHalfLifeDays,
		MinScore:    config.MinScore,
		ConceptsLen: len(config.ConceptWeights),
	}
}

// Ensure ObservationStore satisfies the interface
var _ ObservationStore = (*sqlite.ObservationStore)(nil)
