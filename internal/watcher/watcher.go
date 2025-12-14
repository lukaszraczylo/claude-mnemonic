// Package watcher provides file system watching utilities for detecting
// database file/directory deletions and triggering recreation.
package watcher

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog/log"
)

// Watcher monitors a file or directory for deletion and calls onDelete when removed.
// It watches the parent directory since fsnotify cannot watch non-existent files.
type Watcher struct {
	targetPath string // The file/directory to watch for deletion
	parentPath string // Parent directory (what we actually watch)
	onDelete   func() // Callback when target is deleted
	watcher    *fsnotify.Watcher
	ctx        context.Context
	cancel     context.CancelFunc
	mu         sync.Mutex
	running    bool
	debounce   time.Duration
}

// New creates a new Watcher for the given target path.
// The onDelete callback is called when the target is deleted.
func New(targetPath string, onDelete func()) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Watcher{
		targetPath: targetPath,
		parentPath: filepath.Dir(targetPath),
		onDelete:   onDelete,
		watcher:    fsw,
		ctx:        ctx,
		cancel:     cancel,
		debounce:   100 * time.Millisecond,
	}, nil
}

// Start begins watching for file deletion events.
func (w *Watcher) Start() error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return nil
	}
	w.running = true
	w.mu.Unlock()

	// Add watch on parent directory
	if err := w.addWatch(); err != nil {
		log.Warn().Err(err).Str("path", w.parentPath).Msg("Failed to add initial watch")
		// Continue anyway - we'll try to re-establish later
	}

	go w.watchLoop()
	return nil
}

// Stop stops the watcher.
func (w *Watcher) Stop() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.running {
		return nil
	}

	w.running = false
	w.cancel()
	return w.watcher.Close()
}

// addWatch adds the parent directory to the watch list.
func (w *Watcher) addWatch() error {
	// Ensure parent exists
	if _, err := os.Stat(w.parentPath); os.IsNotExist(err) {
		return err
	}
	return w.watcher.Add(w.parentPath)
}

// watchLoop is the main event loop.
func (w *Watcher) watchLoop() {
	var (
		debounceTimer *time.Timer
		pendingDelete bool
	)

	for {
		select {
		case <-w.ctx.Done():
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			return

		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}

			// Check if this event is for our target
			eventPath := filepath.Clean(event.Name)
			targetPath := filepath.Clean(w.targetPath)

			// Handle parent directory deletion (entire data dir removed)
			if eventPath == w.parentPath && event.Op&fsnotify.Remove != 0 {
				log.Info().Str("path", w.parentPath).Msg("Parent directory deleted")
				pendingDelete = true
				if debounceTimer != nil {
					debounceTimer.Stop()
				}
				debounceTimer = time.AfterFunc(w.debounce, func() {
					w.handleDeletion()
				})
				continue
			}

			// Handle target file/directory deletion
			if eventPath == targetPath && event.Op&fsnotify.Remove != 0 {
				log.Info().Str("path", w.targetPath).Msg("Target deleted")
				pendingDelete = true
				if debounceTimer != nil {
					debounceTimer.Stop()
				}
				debounceTimer = time.AfterFunc(w.debounce, func() {
					w.handleDeletion()
				})
				continue
			}

			// Handle parent directory recreation (re-establish watch)
			if eventPath == w.parentPath && event.Op&fsnotify.Create != 0 {
				log.Info().Str("path", w.parentPath).Msg("Parent directory recreated, re-establishing watch")
				_ = w.addWatch()
				continue
			}

			// If target was recreated after pending delete, cancel the callback
			if pendingDelete && eventPath == targetPath && event.Op&fsnotify.Create != 0 {
				log.Info().Str("path", w.targetPath).Msg("Target recreated, cancelling deletion callback")
				pendingDelete = false
				if debounceTimer != nil {
					debounceTimer.Stop()
				}
			}

		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			log.Error().Err(err).Msg("Watcher error")
		}
	}
}

// handleDeletion calls the onDelete callback and attempts to re-establish the watch.
func (w *Watcher) handleDeletion() {
	log.Info().Str("path", w.targetPath).Msg("Triggering deletion callback")

	// Call the callback
	if w.onDelete != nil {
		w.onDelete()
	}

	// Try to re-establish watch after a short delay (parent may have been recreated)
	go func() {
		time.Sleep(500 * time.Millisecond)
		if err := w.addWatch(); err != nil {
			log.Warn().Err(err).Str("path", w.parentPath).Msg("Failed to re-establish watch after deletion")
		} else {
			log.Info().Str("path", w.parentPath).Msg("Re-established watch after recreation")
		}
	}()
}
