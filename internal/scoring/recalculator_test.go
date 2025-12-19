// Package scoring provides importance score calculation for observations.
package scoring

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
)

// MockObservationStore is a mock implementation of ObservationStore for testing.
type MockObservationStore struct {
	mu                sync.Mutex
	observations      []*models.Observation
	scores            map[int64]float64
	conceptWeights    map[string]float64
	updateErr         error
	getErr            error
	getConceptsErr    error
	updateScoresCalls int
}

func NewMockObservationStore() *MockObservationStore {
	return &MockObservationStore{
		observations:   []*models.Observation{},
		scores:         make(map[int64]float64),
		conceptWeights: make(map[string]float64),
	}
}

func (m *MockObservationStore) GetObservationsNeedingScoreUpdate(ctx context.Context, threshold time.Duration, limit int) ([]*models.Observation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.getErr != nil {
		return nil, m.getErr
	}

	// Return observations that haven't been updated within threshold
	now := time.Now()
	var result []*models.Observation
	for _, obs := range m.observations {
		if !obs.ScoreUpdatedAt.Valid || now.Sub(time.Unix(obs.ScoreUpdatedAt.Int64, 0)) > threshold {
			result = append(result, obs)
			if len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

func (m *MockObservationStore) UpdateImportanceScores(ctx context.Context, scores map[int64]float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.updateScoresCalls++
	if m.updateErr != nil {
		return m.updateErr
	}

	for id, score := range scores {
		m.scores[id] = score
	}
	return nil
}

func (m *MockObservationStore) GetConceptWeights(ctx context.Context) (map[string]float64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.getConceptsErr != nil {
		return nil, m.getConceptsErr
	}
	return m.conceptWeights, nil
}

func (m *MockObservationStore) AddObservation(obs *models.Observation) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.observations = append(m.observations, obs)
}

func (m *MockObservationStore) SetConceptWeights(weights map[string]float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.conceptWeights = weights
}

func (m *MockObservationStore) GetScore(id int64) (float64, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	score, ok := m.scores[id]
	return score, ok
}

func (m *MockObservationStore) GetUpdateScoresCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.updateScoresCalls
}

func (m *MockObservationStore) SetUpdateError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updateErr = err
}

func (m *MockObservationStore) SetGetError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.getErr = err
}

// =============================================================================
// RECALCULATOR TESTS
// =============================================================================

func TestNewRecalculator(t *testing.T) {
	store := NewMockObservationStore()
	calc := NewCalculator(nil)
	log := zerolog.Nop()

	recalc := NewRecalculator(store, calc, log)

	require.NotNil(t, recalc)
	assert.NotNil(t, recalc.store)
	assert.NotNil(t, recalc.calculator)
	assert.Equal(t, 1*time.Hour, recalc.interval)
	assert.Equal(t, 500, recalc.batchSize)
	assert.False(t, recalc.running)
}

func TestRecalculator_RecalculateNow(t *testing.T) {
	store := NewMockObservationStore()
	calc := NewCalculator(nil)
	log := zerolog.Nop()
	recalc := NewRecalculator(store, calc, log)

	// Add observations
	now := time.Now()
	store.AddObservation(&models.Observation{
		ID:             1,
		Type:           models.ObsTypeBugfix,
		CreatedAtEpoch: now.UnixMilli(),
	})
	store.AddObservation(&models.Observation{
		ID:             2,
		Type:           models.ObsTypeFeature,
		CreatedAtEpoch: now.Add(-7 * 24 * time.Hour).UnixMilli(),
	})

	ctx := context.Background()
	err := recalc.RecalculateNow(ctx)

	require.NoError(t, err)

	// Verify scores were calculated
	score1, ok := store.GetScore(1)
	assert.True(t, ok)
	assert.Greater(t, score1, 0.0)

	score2, ok := store.GetScore(2)
	assert.True(t, ok)
	assert.Greater(t, score2, 0.0)
	assert.Less(t, score2, score1, "older observation should have lower score")
}

func TestRecalculator_RefreshConceptWeights(t *testing.T) {
	store := NewMockObservationStore()
	calc := NewCalculator(nil)
	log := zerolog.Nop()
	recalc := NewRecalculator(store, calc, log)

	// Set up custom weights in store
	customWeights := map[string]float64{
		"security":    0.50,
		"performance": 0.25,
	}
	store.SetConceptWeights(customWeights)

	ctx := context.Background()
	err := recalc.RefreshConceptWeights(ctx)

	require.NoError(t, err)

	// Verify weights were updated in calculator config
	config := calc.GetConfig()
	assert.Equal(t, 0.50, config.ConceptWeights["security"])
	assert.Equal(t, 0.25, config.ConceptWeights["performance"])
}

func TestRecalculator_RefreshConceptWeights_Error(t *testing.T) {
	store := NewMockObservationStore()
	calc := NewCalculator(nil)
	log := zerolog.Nop()
	recalc := NewRecalculator(store, calc, log)

	store.getConceptsErr = assert.AnError

	ctx := context.Background()
	err := recalc.RefreshConceptWeights(ctx)

	require.Error(t, err)
}

func TestRecalculator_GetStats(t *testing.T) {
	store := NewMockObservationStore()
	calc := NewCalculator(nil)
	log := zerolog.Nop()
	recalc := NewRecalculator(store, calc, log)

	// Set fields directly for testing
	recalc.interval = 2 * time.Hour
	recalc.batchSize = 250

	stats := recalc.GetStats()

	assert.False(t, stats.Running)
	assert.Equal(t, 2*time.Hour, stats.Interval)
	assert.Equal(t, 250, stats.BatchSize)
	assert.Equal(t, 7.0, stats.HalfLife)
	assert.Equal(t, 0.01, stats.MinScore)
}

func TestRecalculator_StartStop(t *testing.T) {
	store := NewMockObservationStore()
	calc := NewCalculator(nil)
	log := zerolog.Nop()
	recalc := NewRecalculator(store, calc, log)

	// Use a short interval for testing
	recalc.interval = 50 * time.Millisecond

	// Add an observation
	store.AddObservation(&models.Observation{
		ID:             1,
		Type:           models.ObsTypeBugfix,
		CreatedAtEpoch: time.Now().UnixMilli(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start in goroutine
	go recalc.Start(ctx)

	// Wait for initial run and at least one tick
	time.Sleep(100 * time.Millisecond)

	// Verify it ran
	calls := store.GetUpdateScoresCalls()
	assert.GreaterOrEqual(t, calls, 1, "should have run at least once")

	// Stop via context cancellation
	cancel()
	time.Sleep(100 * time.Millisecond)

	// Verify stopped
	stats := recalc.GetStats()
	assert.False(t, stats.Running)
}

func TestRecalculator_StartTwice(t *testing.T) {
	store := NewMockObservationStore()
	calc := NewCalculator(nil)
	log := zerolog.Nop()
	recalc := NewRecalculator(store, calc, log)

	recalc.interval = 1 * time.Hour // Long interval so it doesn't tick during test

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start first time
	go recalc.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	// Try to start second time (should return immediately)
	go recalc.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	// Should still be running (only once)
	stats := recalc.GetStats()
	assert.True(t, stats.Running)

	cancel()
}

func TestRecalculator_StopWhenNotRunning(t *testing.T) {
	store := NewMockObservationStore()
	calc := NewCalculator(nil)
	log := zerolog.Nop()
	recalc := NewRecalculator(store, calc, log)

	// Stop without starting - should not panic
	recalc.Stop()

	stats := recalc.GetStats()
	assert.False(t, stats.Running)
}

func TestRecalculator_EmptyStore(t *testing.T) {
	store := NewMockObservationStore()
	calc := NewCalculator(nil)
	log := zerolog.Nop()
	recalc := NewRecalculator(store, calc, log)

	ctx := context.Background()
	err := recalc.RecalculateNow(ctx)

	require.NoError(t, err)
	assert.Equal(t, 0, store.GetUpdateScoresCalls(), "should not call update with no observations")
}

func TestRecalculator_GetObservationsError(t *testing.T) {
	store := NewMockObservationStore()
	calc := NewCalculator(nil)
	log := zerolog.Nop()
	recalc := NewRecalculator(store, calc, log)

	store.SetGetError(assert.AnError)

	ctx := context.Background()
	err := recalc.RecalculateNow(ctx)

	// Should not return error (logs it instead)
	require.NoError(t, err)
	assert.Equal(t, 0, store.GetUpdateScoresCalls())
}

func TestRecalculator_UpdateScoresError(t *testing.T) {
	store := NewMockObservationStore()
	calc := NewCalculator(nil)
	log := zerolog.Nop()
	recalc := NewRecalculator(store, calc, log)

	store.AddObservation(&models.Observation{
		ID:             1,
		Type:           models.ObsTypeBugfix,
		CreatedAtEpoch: time.Now().UnixMilli(),
	})

	store.SetUpdateError(assert.AnError)

	ctx := context.Background()
	err := recalc.RecalculateNow(ctx)

	// Should not return error (logs it instead)
	require.NoError(t, err)
	assert.Equal(t, 1, store.GetUpdateScoresCalls())
}

func TestRecalculator_BatchProcessing(t *testing.T) {
	store := NewMockObservationStore()
	calc := NewCalculator(nil)
	log := zerolog.Nop()
	recalc := NewRecalculator(store, calc, log)

	// Set small batch size
	recalc.batchSize = 3

	// Add 5 observations
	now := time.Now()
	for i := 1; i <= 5; i++ {
		store.AddObservation(&models.Observation{
			ID:             int64(i),
			Type:           models.ObsTypeBugfix,
			CreatedAtEpoch: now.UnixMilli(),
		})
	}

	ctx := context.Background()
	err := recalc.RecalculateNow(ctx)

	require.NoError(t, err)

	// Should only process batch size (3)
	scores := 0
	for i := 1; i <= 5; i++ {
		if _, ok := store.GetScore(int64(i)); ok {
			scores++
		}
	}
	assert.Equal(t, 3, scores, "should only process batch size observations")
}

func TestRecalculator_ConcurrentAccess(t *testing.T) {
	store := NewMockObservationStore()
	calc := NewCalculator(nil)
	log := zerolog.Nop()
	recalc := NewRecalculator(store, calc, log)

	// Add observations
	now := time.Now()
	for i := 1; i <= 10; i++ {
		store.AddObservation(&models.Observation{
			ID:             int64(i),
			Type:           models.ObsTypeBugfix,
			CreatedAtEpoch: now.UnixMilli(),
		})
	}

	ctx := context.Background()
	var wg sync.WaitGroup

	// Run multiple recalculations concurrently
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = recalc.RecalculateNow(ctx)
		}()
	}

	wg.Wait()

	// Should complete without race conditions
	// (use -race flag to verify)
	assert.GreaterOrEqual(t, store.GetUpdateScoresCalls(), 1)
}

func TestRecalculator_StatsThreadSafe(t *testing.T) {
	store := NewMockObservationStore()
	calc := NewCalculator(nil)
	log := zerolog.Nop()
	recalc := NewRecalculator(store, calc, log)

	var wg sync.WaitGroup

	// Concurrent reads (use -race flag to verify)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = recalc.GetStats()
		}()
	}

	wg.Wait()
}
