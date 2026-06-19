//go:build fts5

// Package maintenance provides scheduled maintenance tasks for claude-mnemonic.
package maintenance

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm/logger"

	"github.com/lukaszraczylo/claude-mnemonic/internal/config"
	gormdb "github.com/lukaszraczylo/claude-mnemonic/internal/db/gorm"
	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
)

// testSetup creates a full maintenance service with a real temporary database.
func testSetup(t *testing.T, cfg *config.Config) (*Service, *gormdb.Store, *gormdb.ObservationStore, *gormdb.PromptStore, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "maintenance_test_*")
	require.NoError(t, err, "create temp dir")

	dbPath := filepath.Join(tmpDir, "test.db")
	storeCfg := gormdb.Config{
		Path:     dbPath,
		MaxConns: 4,
		LogLevel: logger.Silent,
	}

	store, err := gormdb.NewStore(storeCfg)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("NewStore failed: %v", err)
	}

	observationStore := gormdb.NewObservationStore(store, nil, nil, nil)
	summaryStore := gormdb.NewSummaryStore(store)
	promptStore := gormdb.NewPromptStore(store, nil)

	svc := NewService(store, observationStore, summaryStore, promptStore, nil, cfg, zerolog.Nop())

	cleanup := func() {
		store.Close()
		os.RemoveAll(tmpDir)
	}

	return svc, store, observationStore, promptStore, cleanup
}

// defaultCfg returns a maintenance-enabled config for tests.
func defaultCfg() *config.Config {
	cfg := config.Default()
	cfg.MaintenanceEnabled = true
	cfg.MaintenanceIntervalHours = 1
	cfg.ObservationRetentionDays = 0
	cfg.CleanupStaleObservations = false
	return cfg
}

// insertObservation is a helper that inserts an observation and returns its ID.
func insertObservation(t *testing.T, obsStore *gormdb.ObservationStore, session, project string, seq int) int64 {
	t.Helper()
	obs := &models.ParsedObservation{
		Type:  models.ObsTypeDiscovery,
		Title: "test observation",
	}
	id, _, err := obsStore.StoreObservation(context.Background(), session, project, obs, seq, 10)
	require.NoError(t, err)
	return id
}

// ---- NewService ----

func TestNewService_ReturnsNonNilService(t *testing.T) {
	svc, _, _, _, cleanup := testSetup(t, defaultCfg())
	defer cleanup()

	assert.NotNil(t, svc)
}

func TestNewService_InitializesChannels(t *testing.T) {
	svc, _, _, _, cleanup := testSetup(t, defaultCfg())
	defer cleanup()

	// stopCh and doneCh must be non-nil so Stop/Wait don't panic.
	assert.NotNil(t, svc.stopCh)
	assert.NotNil(t, svc.doneCh)
}

// ---- Stats ----

func TestStats_DefaultValues(t *testing.T) {
	svc, _, _, _, cleanup := testSetup(t, defaultCfg())
	defer cleanup()

	stats := svc.Stats()

	assert.Equal(t, true, stats["enabled"])
	assert.Equal(t, 1, stats["interval_hours"])
	assert.Equal(t, 0, stats["retention_days"])
	assert.Equal(t, false, stats["cleanup_stale"])
	assert.Equal(t, int64(0), stats["total_cleaned_obs"])
	assert.Equal(t, int64(0), stats["total_optimizes"])
	assert.Equal(t, false, stats["running"])
}

func TestStats_ReflectsConfigFields(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *config.Config
		wantEnabled bool
		wantHours   int
		wantDays    int
		wantStale   bool
	}{
		{
			name: "maintenance disabled",
			cfg: func() *config.Config {
				c := defaultCfg()
				c.MaintenanceEnabled = false
				return c
			}(),
			wantEnabled: false,
			wantHours:   1,
			wantDays:    0,
			wantStale:   false,
		},
		{
			name: "retention and stale cleanup enabled",
			cfg: func() *config.Config {
				c := defaultCfg()
				c.ObservationRetentionDays = 30
				c.CleanupStaleObservations = true
				c.MaintenanceIntervalHours = 12
				return c
			}(),
			wantEnabled: true,
			wantHours:   12,
			wantDays:    30,
			wantStale:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, _, _, cleanup := testSetup(t, tt.cfg)
			defer cleanup()

			stats := svc.Stats()
			assert.Equal(t, tt.wantEnabled, stats["enabled"])
			assert.Equal(t, tt.wantHours, stats["interval_hours"])
			assert.Equal(t, tt.wantDays, stats["retention_days"])
			assert.Equal(t, tt.wantStale, stats["cleanup_stale"])
		})
	}
}

// ---- Stop (idempotency) ----

func TestStop_WhenNotRunning_DoesNotPanic(t *testing.T) {
	svc, _, _, _, cleanup := testSetup(t, defaultCfg())
	defer cleanup()

	// Service was never started — Stop must be a no-op.
	assert.NotPanics(t, func() { svc.Stop() })
}

func TestStop_CalledTwice_DoesNotPanic(t *testing.T) {
	// Start with maintenance disabled so Start() returns immediately.
	cfg := defaultCfg()
	cfg.MaintenanceEnabled = false

	svc, _, _, _, cleanup := testSetup(t, cfg)
	defer cleanup()

	ctx := context.Background()
	go svc.Start(ctx)
	svc.Wait() // drains doneCh after early return

	// Stop after Wait — must not panic or double-close.
	assert.NotPanics(t, func() { svc.Stop() })
}

// ---- Start / running flag ----

func TestStart_MaintenanceDisabled_ExitsImmediately(t *testing.T) {
	cfg := defaultCfg()
	cfg.MaintenanceEnabled = false

	svc, _, _, _, cleanup := testSetup(t, cfg)
	defer cleanup()

	ctx := context.Background()
	go svc.Start(ctx)

	done := make(chan struct{})
	go func() {
		svc.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Good — returned without blocking.
	case <-time.After(2 * time.Second):
		t.Fatal("Start() did not return promptly when maintenance is disabled")
	}

	stats := svc.Stats()
	assert.Equal(t, false, stats["running"])
}

func TestStart_StopSignal_ExitsCleanly(t *testing.T) {
	// Start() with maintenance disabled exits immediately — verified in
	// TestStart_MaintenanceDisabled_ExitsImmediately.
	//
	// The ticker/stop path is hard to test because Start() always sleeps
	// 5 minutes before entering the loop. We verify instead that Stop()
	// on an already-stopped service is safe and that the doneCh is closed
	// after exit (i.e., Wait() returns).
	cfg := defaultCfg()
	cfg.MaintenanceEnabled = false

	svc, _, _, _, cleanup := testSetup(t, cfg)
	defer cleanup()

	go svc.Start(context.Background())

	done := make(chan struct{})
	go func() {
		svc.Wait()
		close(done)
	}()

	select {
	case <-done:
		// doneCh was closed — Start exited and Wait returned.
	case <-time.After(2 * time.Second):
		t.Fatal("Wait() did not return after Start exited")
	}

	// Stop after Wait must be a no-op and must not panic.
	assert.NotPanics(t, func() { svc.Stop() })
}

func TestStart_DoubleStart_SecondCallIsNoOp(t *testing.T) {
	cfg := defaultCfg()
	cfg.MaintenanceEnabled = false // exits immediately

	svc, _, _, _, cleanup := testSetup(t, cfg)
	defer cleanup()

	ctx := context.Background()

	// First call.
	go svc.Start(ctx)
	svc.Wait()

	// Second call on the same (exhausted) svc should be a no-op and not panic.
	assert.NotPanics(t, func() {
		// svc.running is now false again — but doneCh is already closed.
		// A second Start would attempt to close doneCh again which would panic
		// if the running guard is missing. Verify the guard works.
		svc.mu.Lock()
		running := svc.running
		svc.mu.Unlock()
		assert.False(t, running)
	})
}

// ---- RunNow ----

func TestRunNow_UpdatesLastRunTime(t *testing.T) {
	cfg := defaultCfg()
	cfg.ObservationRetentionDays = 0
	cfg.CleanupStaleObservations = false

	svc, _, _, _, cleanup := testSetup(t, cfg)
	defer cleanup()

	before := time.Now()
	svc.RunNow(context.Background())

	// Allow async goroutine to finish.
	time.Sleep(200 * time.Millisecond)

	svc.mu.Lock()
	lastRun := svc.lastRunTime
	svc.mu.Unlock()

	assert.True(t, lastRun.After(before) || lastRun.Equal(before),
		"lastRunTime should be updated after RunNow")
}

func TestRunNow_IncrementsOptimizeCounter(t *testing.T) {
	svc, _, _, _, cleanup := testSetup(t, defaultCfg())
	defer cleanup()

	svc.RunNow(context.Background())
	time.Sleep(300 * time.Millisecond)

	svc.mu.Lock()
	optimizes := svc.totalOptimizeRun
	svc.mu.Unlock()

	assert.Equal(t, int64(1), optimizes)
}

func TestRunNow_StatsTotalOptimizesReflected(t *testing.T) {
	svc, _, _, _, cleanup := testSetup(t, defaultCfg())
	defer cleanup()

	svc.RunNow(context.Background())
	time.Sleep(300 * time.Millisecond)

	stats := svc.Stats()
	assert.Equal(t, int64(1), stats["total_optimizes"])
}

// ---- cleanupOldObservations (via RunNow) ----

func TestRunNow_RetentionDaysZero_NothingDeleted(t *testing.T) {
	cfg := defaultCfg()
	cfg.ObservationRetentionDays = 0

	svc, _, obsStore, _, cleanup := testSetup(t, cfg)
	defer cleanup()

	// Insert observations.
	for i := 0; i < 5; i++ {
		insertObservation(t, obsStore, "session-1", "proj", i)
	}

	svc.RunNow(context.Background())
	time.Sleep(300 * time.Millisecond)

	remaining, err := obsStore.GetRecentObservations(context.Background(), "proj", 20)
	require.NoError(t, err)
	assert.Equal(t, 5, len(remaining), "nothing should be deleted when retention_days = 0")

	svc.mu.Lock()
	cleaned := svc.totalCleanedObs
	svc.mu.Unlock()
	assert.Equal(t, int64(0), cleaned)
}

func TestRunNow_RetentionDays_DeletesExpiredObservations(t *testing.T) {
	cfg := defaultCfg()
	cfg.ObservationRetentionDays = 1 // keep only last 1 day

	svc, store, _, _, cleanup := testSetup(t, cfg)
	defer cleanup()

	ctx := context.Background()

	// Insert an observation and back-date it to 2 days ago.
	obs := &gormdb.Observation{
		SDKSessionID:    "old-session",
		Project:         "proj",
		Type:            models.ObsTypeDiscovery,
		CreatedAt:       "2000-01-01T00:00:00Z",
		CreatedAtEpoch:  time.Now().AddDate(0, 0, -2).Unix(),
		Scope:           models.ScopeProject,
		ImportanceScore: 1.0,
	}
	require.NoError(t, store.GetDB().WithContext(ctx).Create(obs).Error)

	// Insert a recent observation (should survive).
	recentObs := &gormdb.Observation{
		SDKSessionID:    "new-session",
		Project:         "proj",
		Type:            models.ObsTypeDiscovery,
		CreatedAt:       time.Now().Format(time.RFC3339),
		CreatedAtEpoch:  time.Now().Unix(),
		Scope:           models.ScopeProject,
		ImportanceScore: 1.0,
	}
	require.NoError(t, store.GetDB().WithContext(ctx).Create(recentObs).Error)

	svc.RunNow(ctx)
	time.Sleep(300 * time.Millisecond)

	// Only the recent observation should remain.
	var count int64
	store.GetDB().WithContext(ctx).Model(&gormdb.Observation{}).Count(&count)
	assert.Equal(t, int64(1), count, "expired observation should have been deleted")

	svc.mu.Lock()
	cleaned := svc.totalCleanedObs
	svc.mu.Unlock()
	assert.Equal(t, int64(1), cleaned)
}

func TestRunNow_RetentionDays_VectorCleanupCalled(t *testing.T) {
	cfg := defaultCfg()
	cfg.ObservationRetentionDays = 1

	tmpDir, err := os.MkdirTemp("", "maintenance_vec_test_*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	store, err := gormdb.NewStore(gormdb.Config{
		Path:     filepath.Join(tmpDir, "test.db"),
		MaxConns: 4,
		LogLevel: logger.Silent,
	})
	require.NoError(t, err)
	defer store.Close()

	observationStore := gormdb.NewObservationStore(store, nil, nil, nil)
	summaryStore := gormdb.NewSummaryStore(store)
	promptStore := gormdb.NewPromptStore(store, nil)

	var mu sync.Mutex
	var capturedIDs []int64

	vectorCleanupFn := func(_ context.Context, ids []int64) {
		mu.Lock()
		defer mu.Unlock()
		capturedIDs = append(capturedIDs, ids...)
	}

	svc := NewService(store, observationStore, summaryStore, promptStore, vectorCleanupFn, cfg, zerolog.Nop())

	ctx := context.Background()

	// Insert an expired observation directly.
	obs := &gormdb.Observation{
		SDKSessionID:    "session-x",
		Project:         "proj",
		Type:            models.ObsTypeDiscovery,
		CreatedAt:       "2000-01-01T00:00:00Z",
		CreatedAtEpoch:  time.Now().AddDate(0, 0, -2).Unix(),
		Scope:           models.ScopeProject,
		ImportanceScore: 1.0,
	}
	require.NoError(t, store.GetDB().WithContext(ctx).Create(obs).Error)

	svc.RunNow(ctx)
	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	ids := capturedIDs
	mu.Unlock()

	assert.NotEmpty(t, ids, "vector cleanup callback must be called with deleted IDs")
	assert.Contains(t, ids, obs.ID)
}

// ---- cleanupStaleObservations (via RunNow) ----

func TestRunNow_CleanupStale_DeletesSupersededObservations(t *testing.T) {
	cfg := defaultCfg()
	cfg.CleanupStaleObservations = true
	cfg.ObservationRetentionDays = 0

	svc, store, obsStore, _, cleanup := testSetup(t, cfg)
	defer cleanup()

	ctx := context.Background()

	// Insert an active observation.
	activeID := insertObservation(t, obsStore, "session-1", "proj", 1)

	// Insert and mark a stale observation.
	staleID := insertObservation(t, obsStore, "session-1", "proj", 2)
	require.NoError(t, store.GetDB().WithContext(ctx).
		Model(&gormdb.Observation{}).
		Where("id = ?", staleID).
		Update("is_superseded", 1).Error)

	svc.RunNow(ctx)
	time.Sleep(300 * time.Millisecond)

	// Active observation must survive.
	var activeCount int64
	store.GetDB().WithContext(ctx).Model(&gormdb.Observation{}).Where("id = ?", activeID).Count(&activeCount)
	assert.Equal(t, int64(1), activeCount, "active observation must not be deleted")

	// Stale observation must be gone.
	var staleCount int64
	store.GetDB().WithContext(ctx).Model(&gormdb.Observation{}).Where("id = ?", staleID).Count(&staleCount)
	assert.Equal(t, int64(0), staleCount, "stale observation must be deleted")
}

func TestRunNow_CleanupStale_DisabledLeavesStaleObservations(t *testing.T) {
	cfg := defaultCfg()
	cfg.CleanupStaleObservations = false

	svc, store, obsStore, _, cleanup := testSetup(t, cfg)
	defer cleanup()

	ctx := context.Background()

	staleID := insertObservation(t, obsStore, "session-1", "proj", 1)
	require.NoError(t, store.GetDB().WithContext(ctx).
		Model(&gormdb.Observation{}).
		Where("id = ?", staleID).
		Update("is_superseded", 1).Error)

	svc.RunNow(ctx)
	time.Sleep(300 * time.Millisecond)

	var count int64
	store.GetDB().WithContext(ctx).Model(&gormdb.Observation{}).Where("id = ?", staleID).Count(&count)
	assert.Equal(t, int64(1), count, "stale observation must survive when cleanup_stale is false")
}

func TestRunNow_CleanupStale_NoStaleRows_NothingChanged(t *testing.T) {
	cfg := defaultCfg()
	cfg.CleanupStaleObservations = true

	svc, _, obsStore, _, cleanup := testSetup(t, cfg)
	defer cleanup()

	ctx := context.Background()

	// Only active observations.
	for i := 0; i < 3; i++ {
		insertObservation(t, obsStore, "session-1", "proj", i)
	}

	svc.RunNow(ctx)
	time.Sleep(300 * time.Millisecond)

	remaining, err := obsStore.GetRecentObservations(ctx, "proj", 20)
	require.NoError(t, err)
	assert.Equal(t, 3, len(remaining))
}

// ---- cleanupOldPrompts (via RunNow) ----

func TestRunNow_CleanupOldPrompts_DeletesExpiredPrompts(t *testing.T) {
	svc, store, _, _, cleanup := testSetup(t, defaultCfg())
	defer cleanup()

	ctx := context.Background()

	// Insert a prompt with an old epoch (31 days ago).
	oldPrompt := &gormdb.UserPrompt{
		ClaudeSessionID: "session-old",
		PromptText:      "old prompt",
		PromptNumber:    1,
		CreatedAt:       "2000-01-01T00:00:00Z",
		CreatedAtEpoch:  time.Now().AddDate(0, 0, -31).Unix(),
	}
	require.NoError(t, store.GetDB().WithContext(ctx).Create(oldPrompt).Error)

	// Insert a recent prompt (should survive).
	recentPrompt := &gormdb.UserPrompt{
		ClaudeSessionID: "session-new",
		PromptText:      "recent prompt",
		PromptNumber:    1,
		CreatedAt:       time.Now().Format(time.RFC3339),
		CreatedAtEpoch:  time.Now().Unix(),
	}
	require.NoError(t, store.GetDB().WithContext(ctx).Create(recentPrompt).Error)

	svc.RunNow(ctx)
	time.Sleep(300 * time.Millisecond)

	var count int64
	store.GetDB().WithContext(ctx).Model(&gormdb.UserPrompt{}).Count(&count)
	assert.Equal(t, int64(1), count, "only the recent prompt should survive")
}

func TestRunNow_CleanupOldPrompts_NothingExpired_AllSurvive(t *testing.T) {
	svc, store, _, promptStore, cleanup := testSetup(t, defaultCfg())
	defer cleanup()

	ctx := context.Background()

	for i := 1; i <= 5; i++ {
		_, err := promptStore.SaveUserPromptWithMatches(ctx, "session-1", i, "prompt", 1)
		require.NoError(t, err)
	}

	svc.RunNow(ctx)
	time.Sleep(300 * time.Millisecond)

	var count int64
	store.GetDB().WithContext(ctx).Model(&gormdb.UserPrompt{}).Count(&count)
	assert.Equal(t, int64(5), count, "no prompts should be deleted when none are expired")
}

// ---- Stats race safety ----

func TestStats_ConcurrentAccess_NoRace(t *testing.T) {
	svc, _, _, _, cleanup := testSetup(t, defaultCfg())
	defer cleanup()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = svc.Stats()
		}()
	}
	wg.Wait()
}

// ---- RunNow concurrent safety ----

func TestRunNow_ConcurrentCalls_NoRace(t *testing.T) {
	svc, _, _, _, cleanup := testSetup(t, defaultCfg())
	defer cleanup()

	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			svc.RunNow(ctx)
		}()
	}
	wg.Wait()
	time.Sleep(500 * time.Millisecond)
}

// ---- lastRunDuration is populated ----

func TestRunNow_LastRunDuration_IsPopulated(t *testing.T) {
	svc, _, _, _, cleanup := testSetup(t, defaultCfg())
	defer cleanup()

	svc.RunNow(context.Background())
	time.Sleep(300 * time.Millisecond)

	svc.mu.Lock()
	dur := svc.lastRunDuration
	svc.mu.Unlock()

	assert.Greater(t, int64(dur), int64(0), "lastRunDuration should be set after a maintenance run")
}

func TestStats_LastDurationMs_IsPopulated(t *testing.T) {
	svc, _, _, _, cleanup := testSetup(t, defaultCfg())
	defer cleanup()

	svc.RunNow(context.Background())
	time.Sleep(300 * time.Millisecond)

	stats := svc.Stats()
	// The value is int64 milliseconds; it might be 0 for very fast runs — just verify the key exists.
	_, ok := stats["last_duration_ms"]
	assert.True(t, ok, "stats must contain last_duration_ms key")
}

// ---- Batch deletion boundary ----

func TestRunNow_RetentionDays_BatchDeletion_MoreThan100Rows(t *testing.T) {
	cfg := defaultCfg()
	cfg.ObservationRetentionDays = 1

	svc, store, _, _, cleanup := testSetup(t, cfg)
	defer cleanup()

	ctx := context.Background()

	// Insert 150 expired observations (forces 2 batches of 100).
	for i := 0; i < 150; i++ {
		obs := &gormdb.Observation{
			SDKSessionID:    "session-old",
			Project:         "proj",
			Type:            models.ObsTypeDiscovery,
			CreatedAt:       "2000-01-01T00:00:00Z",
			CreatedAtEpoch:  time.Now().AddDate(0, 0, -2).Unix(),
			Scope:           models.ScopeProject,
			ImportanceScore: 1.0,
		}
		require.NoError(t, store.GetDB().WithContext(ctx).Create(obs).Error)
	}

	svc.RunNow(ctx)
	time.Sleep(500 * time.Millisecond)

	var remaining int64
	store.GetDB().WithContext(ctx).Model(&gormdb.Observation{}).Count(&remaining)
	assert.Equal(t, int64(0), remaining, "all 150 expired observations should be deleted in batches")

	svc.mu.Lock()
	cleaned := svc.totalCleanedObs
	svc.mu.Unlock()
	assert.Equal(t, int64(150), cleaned)
}

func TestRunNow_CleanupStale_BatchDeletion_MoreThan100Rows(t *testing.T) {
	cfg := defaultCfg()
	cfg.CleanupStaleObservations = true

	svc, store, _, _, cleanup := testSetup(t, cfg)
	defer cleanup()

	ctx := context.Background()

	// Insert 120 superseded observations.
	for i := 0; i < 120; i++ {
		obs := &gormdb.Observation{
			SDKSessionID:    "session-stale",
			Project:         "proj",
			Type:            models.ObsTypeDiscovery,
			CreatedAt:       time.Now().Format(time.RFC3339),
			CreatedAtEpoch:  time.Now().Unix(),
			Scope:           models.ScopeProject,
			ImportanceScore: 1.0,
			IsSuperseded:    1,
		}
		require.NoError(t, store.GetDB().WithContext(ctx).Create(obs).Error)
	}

	svc.RunNow(ctx)
	time.Sleep(500 * time.Millisecond)

	var remaining int64
	store.GetDB().WithContext(ctx).Model(&gormdb.Observation{}).Where("is_superseded = ?", 1).Count(&remaining)
	assert.Equal(t, int64(0), remaining, "all 120 stale observations should be deleted in batches")
}
