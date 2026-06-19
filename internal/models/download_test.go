// Package models provides runtime model download and caching.
package models

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sha256Hex returns the hex-encoded SHA-256 of b.
func sha256Hex(t *testing.T, b []byte) string {
	t.Helper()
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// isolateCacheDir points ModelsDir() at a fresh temp directory by overriding
// the OS cache-dir resolution env vars (HOME and XDG_CACHE_HOME, which
// os.UserCacheDir honors on Linux and macOS). It also clears
// CLAUDE_MNEMONIC_MODEL_DIR so EnsureModel takes the download/cache path
// rather than the local-dir shortcut. It returns the resulting models dir.
func isolateCacheDir(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("CLAUDE_MNEMONIC_MODEL_DIR", "")

	dir := ModelsDir()
	require.True(t, strings.HasPrefix(dir, home),
		"ModelsDir() %q must live under temp HOME %q for hermetic test", dir, home)
	return dir
}

// fileServer returns an httptest server that serves body for any request and
// records how many requests it received.
func fileServer(t *testing.T, body []byte) (*httptest.Server, *int32) {
	t.Helper()
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// TestEnsureModel_Download_CachesValidModel verifies the happy path: a model is
// downloaded, passes checksum verification, and the version file is written.
func TestEnsureModel_Download_CachesValidModel(t *testing.T) {
	dir := isolateCacheDir(t)
	body := []byte("model-bytes-happy-path")
	srv, hits := fileServer(t, body)

	const asset = "test-model.onnx"
	path, err := EnsureModel(asset, srv.URL, sha256Hex(t, body))
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(dir, asset), path)
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, body, got, "cached file content must match downloaded body")

	ver, err := os.ReadFile(filepath.Join(dir, ".model_version"))
	require.NoError(t, err)
	assert.Equal(t, ModelVersion, string(ver), "version file must record current ModelVersion")

	assert.Equal(t, int32(1), atomic.LoadInt32(hits), "exactly one download on first fetch")
}

// TestEnsureModel_ChecksumMismatch_FailsAndRemoves verifies that a downloaded
// file whose content does not match the expected SHA-256 is rejected with a
// verification error and is not left behind in the cache.
func TestEnsureModel_ChecksumMismatch_FailsAndRemoves(t *testing.T) {
	dir := isolateCacheDir(t)
	body := []byte("the-actual-downloaded-bytes")
	srv, _ := fileServer(t, body)

	const asset = "mismatch-model.onnx"
	wrongSum := sha256Hex(t, []byte("a-different-expected-payload"))

	path, err := EnsureModel(asset, srv.URL, wrongSum)
	require.Error(t, err)
	assert.Empty(t, path)
	assert.Contains(t, err.Error(), "checksum verification")

	_, statErr := os.Stat(filepath.Join(dir, asset))
	assert.True(t, os.IsNotExist(statErr),
		"corrupt download must be removed from cache, got statErr=%v", statErr)
}

// TestEnsureModel_VersionBump_InvalidatesCache verifies that when the on-disk
// .model_version differs from ModelVersion, the cache directory is cleared and
// the model is re-downloaded fresh.
func TestEnsureModel_VersionBump_InvalidatesCache(t *testing.T) {
	dir := isolateCacheDir(t)
	require.NoError(t, os.MkdirAll(dir, 0700))

	// Seed a stale cache: an old version file, a leftover model file, and an
	// unrelated sidecar file. Only the version-bump branch wipes the whole dir
	// (os.RemoveAll), so the sidecar's removal uniquely proves invalidation ran
	// — a plain checksum-driven re-download would leave the sidecar in place.
	const asset = "versioned-model.onnx"
	staleModel := filepath.Join(dir, asset)
	staleSidecar := filepath.Join(dir, "leftover-sidecar.bin")
	require.NoError(t, os.WriteFile(staleModel, []byte("stale-garbage"), 0600))
	require.NoError(t, os.WriteFile(staleSidecar, []byte("unrelated-leftover"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".model_version"), []byte("OLD-VERSION"), 0600))

	body := []byte("freshly-downloaded-model-bytes")
	srv, hits := fileServer(t, body)

	path, err := EnsureModel(asset, srv.URL, sha256Hex(t, body))
	require.NoError(t, err)

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, body, got, "stale cached file must be replaced by fresh download")

	ver, err := os.ReadFile(filepath.Join(dir, ".model_version"))
	require.NoError(t, err)
	assert.Equal(t, ModelVersion, string(ver), "version file must be rewritten to current ModelVersion")

	assert.Equal(t, int32(1), atomic.LoadInt32(hits),
		"version bump must force exactly one re-download, not serve the stale file")

	_, sidecarErr := os.Stat(staleSidecar)
	assert.True(t, os.IsNotExist(sidecarErr),
		"version bump must wipe the whole cache dir, removing unrelated sidecar files")
}

// TestEnsureModel_SameVersionValidCache_NoDownload verifies that a matching
// version file plus a checksum-valid cached file short-circuits the download.
func TestEnsureModel_SameVersionValidCache_NoDownload(t *testing.T) {
	dir := isolateCacheDir(t)
	require.NoError(t, os.MkdirAll(dir, 0700))

	const asset = "cached-model.onnx"
	body := []byte("already-cached-valid-bytes")
	require.NoError(t, os.WriteFile(filepath.Join(dir, asset), body, 0600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".model_version"), []byte(ModelVersion), 0600))

	// Server would record a hit if EnsureModel wrongly re-downloaded.
	srv, hits := fileServer(t, []byte("should-not-be-fetched"))

	path, err := EnsureModel(asset, srv.URL, sha256Hex(t, body))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, asset), path)
	assert.Equal(t, int32(0), atomic.LoadInt32(hits), "valid same-version cache must not trigger a download")
}

// TestEnsureModel_RetryExhaustion_ReturnsError verifies that when every download
// attempt fails (server always returns HTTP 500), EnsureModel exhausts its
// retries and returns an error reporting the attempt count, leaving no model
// file behind.
//
// Note: downloadWithRetries sleeps with exponential backoff between attempts,
// so this test takes ~6s by design; the backoff is production behavior and is
// intentionally not stubbed.
func TestEnsureModel_RetryExhaustion_ReturnsError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping retry-exhaustion test (real backoff sleeps) in -short mode")
	}

	dir := isolateCacheDir(t)

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	const asset = "unreachable-model.onnx"
	path, err := EnsureModel(asset, srv.URL, sha256Hex(t, []byte("never-served")))
	require.Error(t, err)
	assert.Empty(t, path)
	assert.Contains(t, err.Error(), "attempts", "error must report retry exhaustion")

	assert.Equal(t, int32(maxRetries), atomic.LoadInt32(&hits),
		"server must be hit exactly maxRetries times")

	_, statErr := os.Stat(filepath.Join(dir, asset))
	assert.True(t, os.IsNotExist(statErr), "failed download must not leave a model file")
}

// TestEnsureModel_LocalDir verifies the CLAUDE_MNEMONIC_MODEL_DIR shortcut:
// a present, checksum-valid file is returned directly, and a checksum mismatch
// or missing file is reported without any network access.
func TestEnsureModel_LocalDir(t *testing.T) {
	body := []byte("local-dir-model-bytes")
	validSum := sha256Hex(t, body)

	tests := []struct {
		name      string
		writeFile bool
		expectSum string
		wantErr   string
	}{
		{name: "valid_local_model", writeFile: true, expectSum: validSum, wantErr: ""},
		{name: "checksum_mismatch", writeFile: true, expectSum: sha256Hex(t, []byte("other")), wantErr: "checksum mismatch"},
		{name: "missing_file", writeFile: false, expectSum: validSum, wantErr: "not found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			localDir := t.TempDir()
			t.Setenv("CLAUDE_MNEMONIC_MODEL_DIR", localDir)

			const asset = "local-model.onnx"
			if tt.writeFile {
				require.NoError(t, os.WriteFile(filepath.Join(localDir, asset), body, 0600))
			}

			path, err := EnsureModel(asset, "http://127.0.0.1:0/should-not-be-used", tt.expectSum)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				assert.Empty(t, path)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, filepath.Join(localDir, asset), path)
		})
	}
}
