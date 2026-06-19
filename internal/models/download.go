// Package models provides runtime model download and caching.
// Models are downloaded from Hugging Face on first use and cached
// to the OS cache directory (~/Library/Caches/claude-mnemonic/models/ on macOS,
// ~/.cache/claude-mnemonic/models/ on Linux, %LocalAppData%/claude-mnemonic/models/ on Windows).
// This replaces the previous approach of embedding models in the binary
// via go:embed and tracking them with Git LFS.
package models

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	// ModelVersion is incremented to force re-download of all cached models.
	ModelVersion = "1"

	maxRetries      = 3
	retryBaseDelay  = 2 * time.Second
	progressEvery   = 3 * time.Second
	downloadTimeout = 15 * time.Minute
)

// ModelsDir returns the path to the model cache directory.
// Uses the OS cache dir for cross-platform compatibility:
//
//	macOS:   ~/Library/Caches/claude-mnemonic/models
//	Linux:   ~/.cache/claude-mnemonic/models
//	Windows: %LocalAppData%/claude-mnemonic/models
func ModelsDir() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".cache")
	}
	return filepath.Join(dir, "claude-mnemonic", "models")
}

// EnsureModel ensures the model file exists and returns its path.
//
// Resolution order:
//  1. CLAUDE_MNEMONIC_MODEL_DIR env var — if set, load models from this directory directly.
//     This is intended for development and CI where models are already on disk.
//  2. User cache directory — if the model is already cached and its checksum matches,
//     return it immediately.
//  3. url — download the model from the given URL with retries and cache it.
//
// assetName is the local filename (e.g. "bge-small-en-v1.5-model.onnx").
// expectedSHA256 is the hex-encoded SHA-256 of the file.
func EnsureModel(assetName, url, expectedSHA256 string) (string, error) {
	if localDir := os.Getenv("CLAUDE_MNEMONIC_MODEL_DIR"); localDir != "" {
		localPath := filepath.Join(localDir, assetName)
		if _, err := os.Stat(localPath); err == nil {
			if valid, err := verifyChecksum(localPath, expectedSHA256); err != nil || !valid {
				return "", fmt.Errorf("local model %s checksum mismatch", assetName)
			}
			return localPath, nil
		}
		return "", fmt.Errorf("local model %s not found in %s", assetName, localDir)
	}

	dir := ModelsDir()
	versionFile := filepath.Join(dir, ".model_version")
	modelPath := filepath.Join(dir, assetName)

	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("create model cache dir: %w", err)
	}

	currentVersion, _ := os.ReadFile(versionFile)

	if string(currentVersion) == ModelVersion {
		if fi, err := os.Stat(modelPath); err == nil && fi.Size() > 0 {
			if valid, _ := verifyChecksum(modelPath, expectedSHA256); valid {
				return modelPath, nil
			}
			log.Warn().Str("model", assetName).Msg("Cached model corrupted, re-downloading")
			_ = os.Remove(modelPath) // best-effort cleanup
		}
	} else if len(currentVersion) > 0 {
		log.Info().Str("old", string(currentVersion)).Str("new", ModelVersion).Msg("Model version updated, clearing cache")
		_ = os.RemoveAll(dir) // best-effort cleanup
		if err := os.MkdirAll(dir, 0700); err != nil {
			return "", fmt.Errorf("create model cache dir: %w", err)
		}
	}

	if err := downloadWithRetries(url, modelPath); err != nil {
		_ = os.Remove(modelPath) // best-effort cleanup
		return "", fmt.Errorf("download %s: %w", assetName, err)
	}

	if valid, err := verifyChecksum(modelPath, expectedSHA256); err != nil || !valid {
		_ = os.Remove(modelPath) // best-effort cleanup
		return "", fmt.Errorf("downloaded model %s failed checksum verification — the file at %s may have changed", assetName, url)
	}

	if err := os.WriteFile(versionFile, []byte(ModelVersion), 0600); err != nil {
		log.Warn().Err(err).Msg("Failed to write model version file")
	}

	log.Info().Str("model", assetName).Str("cache", dir).Msg("Model ready")
	return modelPath, nil
}

func downloadWithRetries(url, dest string) error {
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			delay := retryBaseDelay * time.Duration(1<<(attempt-1))
			log.Warn().Int("attempt", attempt+1).Int("max", maxRetries).Dur("delay", delay).Msg("Retrying download")
			time.Sleep(delay)
		}
		if err := downloadFile(url, dest); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return fmt.Errorf("failed after %d attempts: %w", maxRetries, lastErr)
}

func downloadFile(url, dest string) error {
	client := &http.Client{Timeout: downloadTimeout}

	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("HTTP GET: %w", err)
	}
	defer func() { _ = resp.Body.Close() }() // best-effort cleanup

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	tmp := dest + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	cleanup := func() { _ = f.Close(); _ = os.Remove(tmp) } // best-effort cleanup

	name := filepath.Base(dest)
	reader := &progressReader{
		reader: resp.Body,
		total:  resp.ContentLength,
		name:   name,
	}

	if _, err := io.Copy(f, reader); err != nil {
		cleanup()
		return fmt.Errorf("write file: %w", err)
	}

	if err := f.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("sync file: %w", err)
	}
	_ = f.Close() // best-effort cleanup; data already flushed via Sync above

	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp) // best-effort cleanup
		return fmt.Errorf("commit file: %w", err)
	}

	return nil
}

// progressReader wraps an io.Reader and logs progress for large downloads.
type progressReader struct {
	reader    io.Reader
	total     int64
	current   int64
	name      string
	lastLog   time.Time
	loggedPct int
}

func (pr *progressReader) Read(p []byte) (n int, err error) {
	n, err = pr.reader.Read(p)
	pr.current += int64(n)

	if time.Since(pr.lastLog) >= progressEvery || err == io.EOF {
		if pr.total > 0 {
			pct := int(math.Round(float64(pr.current) / float64(pr.total) * 100))
			if pct != pr.loggedPct {
				pr.loggedPct = pct
				mibCurrent := float64(pr.current) / (1024 * 1024)
				mibTotal := float64(pr.total) / (1024 * 1024)
				log.Info().
					Str("model", pr.name).
					Int("pct", pct).
					Str("progress", fmt.Sprintf("%.0f/%.0f MiB", mibCurrent, mibTotal)).
					Msg("Downloading")
			}
		} else {
			mibCurrent := float64(pr.current) / (1024 * 1024)
			log.Info().
				Str("model", pr.name).
				Str("progress", fmt.Sprintf("%.0f MiB", mibCurrent)).
				Msg("Downloading (size unknown)")
		}
		pr.lastLog = time.Now()
	}

	return n, err
}

func verifyChecksum(path, expectedHex string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }() // best-effort cleanup

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, err
	}

	actual := hex.EncodeToString(h.Sum(nil))
	return actual == expectedHex, nil
}

// CleanStale removes any temporary download files that may have been left behind
// by a previous interrupted download.
func CleanStale() error {
	dir := ModelsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			tmpPath := filepath.Join(dir, e.Name())
			if info, err := e.Info(); err == nil {
				if time.Since(info.ModTime()) > time.Hour {
					_ = os.Remove(tmpPath) // best-effort cleanup
					log.Debug().Str("file", tmpPath).Msg("Cleaned stale temp file")
				}
			}
		}
	}
	return nil
}
