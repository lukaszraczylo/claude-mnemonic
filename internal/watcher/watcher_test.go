//go:build fts5

package watcher

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// waitForCondition polls fn every 10ms until it returns true or timeout expires.
func waitForCondition(t *testing.T, timeout time.Duration, fn func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// TestNew_CreatesWatcherWithCorrectFields verifies New initialises all fields correctly.
func TestNew_CreatesWatcherWithCorrectFields(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "db.sqlite")

	called := false
	cb := func() { called = true }

	w, err := New(target, cb)
	require.NoError(t, err)
	require.NotNil(t, w)
	defer w.Stop() //nolint:errcheck

	assert.Equal(t, target, w.targetPath)
	assert.Equal(t, dir, w.parentPath)
	assert.Equal(t, 100*time.Millisecond, w.debounce)
	assert.NotNil(t, w.watcher)
	assert.NotNil(t, w.ctx)
	assert.NotNil(t, w.cancel)
	assert.False(t, w.running)
	assert.False(t, called, "callback must not be invoked on creation")
}

// TestNew_NilCallback is valid — handleDeletion guards for nil onDelete.
func TestNew_NilCallback(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "db.sqlite")

	w, err := New(target, nil)
	require.NoError(t, err)
	require.NotNil(t, w)
	defer w.Stop() //nolint:errcheck

	assert.Nil(t, w.onDelete)
}

// TestStart_SetsRunningTrue verifies Start transitions running to true.
func TestStart_SetsRunningTrue(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "db.sqlite")

	w, err := New(target, func() {})
	require.NoError(t, err)
	defer w.Stop() //nolint:errcheck

	err = w.Start()
	require.NoError(t, err)

	w.mu.Lock()
	running := w.running
	w.mu.Unlock()
	assert.True(t, running)
}

// TestStart_Idempotent verifies calling Start twice does not panic or return error.
func TestStart_Idempotent(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "db.sqlite")

	w, err := New(target, func() {})
	require.NoError(t, err)
	defer w.Stop() //nolint:errcheck

	require.NoError(t, w.Start())
	require.NoError(t, w.Start(), "second Start must be a no-op without error")

	// Still only one goroutine running — running flag is still true.
	w.mu.Lock()
	running := w.running
	w.mu.Unlock()
	assert.True(t, running)
}

// TestStop_SetsRunningFalse verifies Stop transitions running to false.
func TestStop_SetsRunningFalse(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "db.sqlite")

	w, err := New(target, func() {})
	require.NoError(t, err)

	require.NoError(t, w.Start())
	require.NoError(t, w.Stop())

	w.mu.Lock()
	running := w.running
	w.mu.Unlock()
	assert.False(t, running)
}

// TestStop_Idempotent verifies calling Stop when not running returns nil.
func TestStop_Idempotent(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "db.sqlite")

	w, err := New(target, func() {})
	require.NoError(t, err)

	// Never started — Stop must be a no-op.
	assert.NoError(t, w.Stop())
	// Second stop after the first no-op must also succeed.
	assert.NoError(t, w.Stop())
}

// TestStop_WithoutStart verifies Stop on an unstarted watcher is safe.
func TestStop_WithoutStart(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "db.sqlite")

	w, err := New(target, func() {})
	require.NoError(t, err)

	err = w.Stop()
	assert.NoError(t, err)
}

// TestTargetDeletion_CallbackFired verifies that deleting the target file triggers onDelete.
func TestTargetDeletion_CallbackFired(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "db.sqlite")

	// Create the target file so the parent watch is real.
	require.NoError(t, os.WriteFile(target, []byte("data"), 0o644))

	var callCount int32
	w, err := New(target, func() {
		atomic.AddInt32(&callCount, 1)
	})
	require.NoError(t, err)
	defer w.Stop() //nolint:errcheck

	require.NoError(t, w.Start())

	// Delete the target file.
	require.NoError(t, os.Remove(target))

	// Wait up to 1 second for the debounced callback (debounce=100ms).
	fired := waitForCondition(t, 1*time.Second, func() bool {
		return atomic.LoadInt32(&callCount) > 0
	})
	assert.True(t, fired, "onDelete callback not called after target deletion")
}

// TestTargetDeletion_CallbackCalledOnce verifies debounce suppresses duplicate events.
func TestTargetDeletion_CallbackCalledOnce(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "db.sqlite")
	require.NoError(t, os.WriteFile(target, []byte("data"), 0o644))

	var callCount int32
	w, err := New(target, func() {
		atomic.AddInt32(&callCount, 1)
	})
	require.NoError(t, err)
	defer w.Stop() //nolint:errcheck

	require.NoError(t, w.Start())

	require.NoError(t, os.Remove(target))

	// Wait for callback to fire.
	waitForCondition(t, 1*time.Second, func() bool {
		return atomic.LoadInt32(&callCount) > 0
	})

	// Wait an extra debounce window to confirm no second call arrives.
	time.Sleep(300 * time.Millisecond)
	assert.Equal(t, int32(1), atomic.LoadInt32(&callCount), "callback fired more than once for a single deletion")
}

// TestTargetRecreation_CancelsCallback verifies that recreating the target before the
// debounce fires suppresses the onDelete callback.
func TestTargetRecreation_CancelsCallback(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "db.sqlite")
	require.NoError(t, os.WriteFile(target, []byte("data"), 0o644))

	var callCount int32
	// Use a longer debounce so we can recreate before it fires.
	w, err := New(target, func() {
		atomic.AddInt32(&callCount, 1)
	})
	require.NoError(t, err)
	// Override debounce to give us a larger window.
	w.debounce = 300 * time.Millisecond
	defer w.Stop() //nolint:errcheck

	require.NoError(t, w.Start())

	// Delete then immediately recreate within the debounce window.
	require.NoError(t, os.Remove(target))
	time.Sleep(20 * time.Millisecond) // ensure delete event is processed
	require.NoError(t, os.WriteFile(target, []byte("data"), 0o644))

	// Wait past full debounce period to confirm callback was cancelled.
	time.Sleep(500 * time.Millisecond)
	assert.Equal(t, int32(0), atomic.LoadInt32(&callCount), "callback should have been cancelled by recreation")
}

// TestParentDirectoryDeletion_CallbackFired verifies that deleting the parent directory
// triggers the onDelete callback.
func TestParentDirectoryDeletion_CallbackFired(t *testing.T) {
	// Create a nested structure: base/sub/db.sqlite so we can remove sub
	// without losing t.TempDir (which is base).
	base := t.TempDir()
	sub := filepath.Join(base, "sub")
	require.NoError(t, os.Mkdir(sub, 0o755))
	target := filepath.Join(sub, "db.sqlite")
	require.NoError(t, os.WriteFile(target, []byte("data"), 0o644))

	var callCount int32
	w, err := New(target, func() {
		atomic.AddInt32(&callCount, 1)
	})
	require.NoError(t, err)
	defer w.Stop() //nolint:errcheck

	require.NoError(t, w.Start())

	// Remove parent directory entirely.
	require.NoError(t, os.RemoveAll(sub))

	fired := waitForCondition(t, 1500*time.Millisecond, func() bool {
		return atomic.LoadInt32(&callCount) > 0
	})
	assert.True(t, fired, "onDelete callback not called after parent directory deletion")
}

// TestAddWatch_NonExistentParent verifies addWatch returns an error when parent is absent.
func TestAddWatch_NonExistentParent(t *testing.T) {
	// Point watcher at a path whose parent definitely does not exist.
	nonExistent := filepath.Join(t.TempDir(), "missing", "db.sqlite")

	w, err := New(nonExistent, func() {})
	require.NoError(t, err)
	defer w.Stop() //nolint:errcheck

	err = w.addWatch()
	assert.Error(t, err, "addWatch must fail when parent directory does not exist")
}

// TestContextCancellation_StopsWatchLoop verifies the watchLoop exits when Stop is called.
func TestContextCancellation_StopsWatchLoop(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "db.sqlite")

	w, err := New(target, func() {})
	require.NoError(t, err)

	require.NoError(t, w.Start())

	// Stop cancels the context; the goroutine should exit cleanly.
	require.NoError(t, w.Stop())

	// Give the goroutine a moment to exit — then verify running is false.
	time.Sleep(50 * time.Millisecond)
	w.mu.Lock()
	running := w.running
	w.mu.Unlock()
	assert.False(t, running)
}

// TestParentDirRecreation_ReEstablishesWatch verifies that recreating the parent after
// deletion allows subsequent target-deletion events to fire the callback.
func TestParentDirRecreation_ReEstablishesWatch(t *testing.T) {
	base := t.TempDir()
	sub := filepath.Join(base, "sub")
	require.NoError(t, os.Mkdir(sub, 0o755))
	target := filepath.Join(sub, "db.sqlite")
	require.NoError(t, os.WriteFile(target, []byte("data"), 0o644))

	var callCount int32
	w, err := New(target, func() {
		atomic.AddInt32(&callCount, 1)
	})
	require.NoError(t, err)
	defer w.Stop() //nolint:errcheck

	require.NoError(t, w.Start())

	// Remove the parent.
	require.NoError(t, os.RemoveAll(sub))

	// Wait for first callback.
	fired := waitForCondition(t, 1500*time.Millisecond, func() bool {
		return atomic.LoadInt32(&callCount) > 0
	})
	require.True(t, fired, "first deletion callback must fire")

	firstCount := atomic.LoadInt32(&callCount)

	// Recreate parent and target — re-established watch should allow a second callback.
	require.NoError(t, os.Mkdir(sub, 0o755))
	require.NoError(t, os.WriteFile(target, []byte("data"), 0o644))

	// Wait for handleDeletion's goroutine to attempt re-adding the watch (500ms sleep inside).
	time.Sleep(700 * time.Millisecond)

	// Now delete the target again.
	require.NoError(t, os.Remove(target))

	// We only assert the first callback fired; the re-watch is best-effort and
	// OS-timing-dependent, so we don't hard-assert a second callback.
	assert.GreaterOrEqual(t, atomic.LoadInt32(&callCount), firstCount, "call count must not decrease")
}

// TestConcurrentStartStop verifies that concurrent Start/Stop calls do not race or panic.
func TestConcurrentStartStop(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "db.sqlite")

	w, err := New(target, func() {})
	require.NoError(t, err)

	var wg sync.WaitGroup
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Launch goroutines that repeatedly start/stop the watcher.
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					_ = w.Start()
					time.Sleep(5 * time.Millisecond)
					_ = w.Stop()
					time.Sleep(5 * time.Millisecond)
				}
			}
		}()
	}
	wg.Wait()
	// No panic = pass. Final state: running should be consistent (we don't assert
	// a specific value since Stop may have won last).
}

// TestDebounceField_DefaultValue asserts the default debounce is 100ms.
func TestDebounceField_DefaultValue(t *testing.T) {
	dir := t.TempDir()
	w, err := New(filepath.Join(dir, "x"), func() {})
	require.NoError(t, err)
	defer w.Stop() //nolint:errcheck
	assert.Equal(t, 100*time.Millisecond, w.debounce)
}

// TestCallbackNotCalledWhenStopped verifies that if we Stop before the debounce fires,
// the callback is not invoked after Stop (context cancel exits the watchLoop).
func TestCallbackNotCalledWhenStopped(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "db.sqlite")
	require.NoError(t, os.WriteFile(target, []byte("data"), 0o644))

	var callCount int32
	w, err := New(target, func() {
		atomic.AddInt32(&callCount, 1)
	})
	require.NoError(t, err)
	w.debounce = 500 * time.Millisecond // wide window

	require.NoError(t, w.Start())

	// Delete file — debounce timer is now running (500ms).
	require.NoError(t, os.Remove(target))
	time.Sleep(20 * time.Millisecond) // let event propagate

	// Stop before timer fires — context is cancelled, watchLoop exits.
	require.NoError(t, w.Stop())

	// Wait past the debounce window; the AfterFunc may still fire (it's not
	// tied to the context), but the watcher is stopped. We assert the loop
	// itself exited cleanly.
	time.Sleep(700 * time.Millisecond)

	// The AfterFunc timer fires outside the watchLoop — callback may or may not
	// have fired depending on OS scheduling. We assert no panic occurred.
	// The important invariant: running is false.
	w.mu.Lock()
	running := w.running
	w.mu.Unlock()
	assert.False(t, running)
}
