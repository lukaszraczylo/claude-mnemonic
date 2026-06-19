//go:build fts5

// Package update provides self-update functionality for claude-mnemonic.
package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// isNewerVersion
// ---------------------------------------------------------------------------

func TestIsNewerVersion(t *testing.T) {
	tests := []struct {
		name    string
		latest  string
		current string
		want    bool
	}{
		{
			name:    "newer_major",
			latest:  "2.0.0",
			current: "1.0.0",
			want:    true,
		},
		{
			name:    "newer_minor",
			latest:  "1.1.0",
			current: "1.0.0",
			want:    true,
		},
		{
			name:    "newer_patch",
			latest:  "1.0.1",
			current: "1.0.0",
			want:    true,
		},
		{
			name:    "same_version",
			latest:  "1.0.0",
			current: "1.0.0",
			want:    false,
		},
		{
			name:    "older_major",
			latest:  "0.9.9",
			current: "1.0.0",
			want:    false,
		},
		{
			name:    "older_minor",
			latest:  "1.0.0",
			current: "1.1.0",
			want:    false,
		},
		{
			name:    "older_patch",
			latest:  "1.0.0",
			current: "1.0.1",
			want:    false,
		},
		{
			name:    "v_prefix_latest",
			latest:  "v1.2.0",
			current: "1.1.0",
			want:    true,
		},
		{
			name:    "v_prefix_current",
			latest:  "1.2.0",
			current: "v1.1.0",
			want:    true,
		},
		{
			name:    "v_prefix_both",
			latest:  "v1.2.0",
			current: "v1.2.0",
			want:    false,
		},
		{
			name:    "dev_build_current_same_base",
			latest:  "0.3.5",
			current: "0.3.5-2-gca711a8-dirty",
			want:    false,
		},
		{
			name:    "dev_build_current_older_base",
			latest:  "0.3.6",
			current: "0.3.5-2-gca711a8-dirty",
			want:    true,
		},
		{
			name:    "dev_build_current_newer_base",
			latest:  "0.3.4",
			current: "0.3.5-2-gca711a8-dirty",
			want:    false,
		},
		{
			name:    "longer_latest_semver",
			latest:  "1.0.0.1",
			current: "1.0.0",
			want:    true,
		},
		{
			name:    "longer_current_semver",
			latest:  "1.0.0",
			current: "1.0.0.1",
			want:    false,
		},
		{
			name:    "zero_versions",
			latest:  "0.0.0",
			current: "0.0.0",
			want:    false,
		},
		{
			name:    "major_rollback",
			latest:  "1.0.0",
			current: "2.0.0",
			want:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isNewerVersion(tc.latest, tc.current)
			assert.Equal(t, tc.want, got,
				"isNewerVersion(%q, %q)", tc.latest, tc.current)
		})
	}
}

// ---------------------------------------------------------------------------
// getPlatform
// ---------------------------------------------------------------------------

func TestGetPlatform(t *testing.T) {
	got := getPlatform()
	expected := fmt.Sprintf("%s_%s", runtime.GOOS, runtime.GOARCH)
	assert.Equal(t, expected, got)
	assert.Contains(t, got, "_", "platform string must contain underscore separator")
	assert.NotEmpty(t, got)
}

func TestGetPlatform_ContainsOSAndArch(t *testing.T) {
	got := getPlatform()
	parts := strings.SplitN(got, "_", 2)
	require.Len(t, parts, 2, "platform must have exactly two parts separated by underscore")
	assert.Equal(t, runtime.GOOS, parts[0])
	assert.Equal(t, runtime.GOARCH, parts[1])
}

// ---------------------------------------------------------------------------
// GetManualUpdateCommand
// ---------------------------------------------------------------------------

func TestGetManualUpdateCommand(t *testing.T) {
	tests := []struct {
		name            string
		version         string
		wantContains    []string
		wantNotContains []string
	}{
		{
			name:            "empty_version_returns_latest",
			version:         "",
			wantContains:    []string{"curl -sSL", InstallScriptURL, "| bash"},
			wantNotContains: []string{"bash -s --"},
		},
		{
			name:         "specific_version_appended",
			version:      "v1.2.3",
			wantContains: []string{"curl -sSL", InstallScriptURL, "| bash -s --", "v1.2.3"},
		},
		{
			name:         "version_without_v_prefix",
			version:      "1.2.3",
			wantContains: []string{"1.2.3"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := GetManualUpdateCommand(tc.version)
			for _, want := range tc.wantContains {
				assert.Contains(t, got, want)
			}
			for _, notWant := range tc.wantNotContains {
				assert.NotContains(t, got, notWant)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// getInstallDirectories
// ---------------------------------------------------------------------------

func TestGetInstallDirectories_AlwaysContainsInstallDir(t *testing.T) {
	dir := t.TempDir()
	u := New("1.0.0", dir)
	dirs := u.getInstallDirectories()
	assert.Contains(t, dirs, dir)
}

func TestGetInstallDirectories_NoDuplicateWhenInstallDirIsStableBin(t *testing.T) {
	// Create a fake home with stableBin == installDir
	home := t.TempDir()
	t.Setenv("HOME", home)

	stableBin := filepath.Join(home, ".claude-mnemonic", "bin")
	require.NoError(t, os.MkdirAll(stableBin, 0750))

	u := New("1.0.0", stableBin)
	dirs := u.getInstallDirectories()

	count := 0
	for _, d := range dirs {
		if d == stableBin {
			count++
		}
	}
	assert.Equal(t, 1, count, "stableBin should appear exactly once when it equals installDir")
}

func TestGetInstallDirectories_IncludesStableBinWhenExists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	installDir := filepath.Join(home, "some-other-dir")
	require.NoError(t, os.MkdirAll(installDir, 0750))

	stableBin := filepath.Join(home, ".claude-mnemonic", "bin")
	require.NoError(t, os.MkdirAll(stableBin, 0750))

	u := New("1.0.0", installDir)
	dirs := u.getInstallDirectories()

	assert.Contains(t, dirs, installDir)
	assert.Contains(t, dirs, stableBin)
}

func TestGetInstallDirectories_SkipsStableBinWhenAbsent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	installDir := filepath.Join(home, "install-dir")
	require.NoError(t, os.MkdirAll(installDir, 0750))
	// stableBin NOT created

	u := New("1.0.0", installDir)
	dirs := u.getInstallDirectories()

	stableBin := filepath.Join(home, ".claude-mnemonic", "bin")
	assert.NotContains(t, dirs, stableBin)
}

func TestGetInstallDirectories_IncludesCacheDirsWithWorkerBinary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	installDir := filepath.Join(home, "install-dir")
	require.NoError(t, os.MkdirAll(installDir, 0750))

	// Create a fake cache dir with a worker binary
	cacheBase := filepath.Join(home, ".claude/plugins/cache/claude-mnemonic/claude-mnemonic/v1.2.3")
	require.NoError(t, os.MkdirAll(cacheBase, 0750))
	require.NoError(t, os.WriteFile(filepath.Join(cacheBase, "worker"), []byte("fake"), 0755))

	// Cache dir without worker — should NOT be included
	cacheMissing := filepath.Join(home, ".claude/plugins/cache/claude-mnemonic/claude-mnemonic/v1.1.0")
	require.NoError(t, os.MkdirAll(cacheMissing, 0750))

	u := New("1.0.0", installDir)
	dirs := u.getInstallDirectories()

	assert.Contains(t, dirs, cacheBase)
	assert.NotContains(t, dirs, cacheMissing)
}

// ---------------------------------------------------------------------------
// verifyChecksum
// ---------------------------------------------------------------------------

func makeTarGzFile(t *testing.T, dir, content string) (path string, checksum string) {
	t.Helper()
	archivePath := filepath.Join(dir, "release.tar.gz")

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	body := []byte(content)
	hdr := &tar.Header{
		Name:     "worker",
		Mode:     0755,
		Size:     int64(len(body)),
		Typeflag: tar.TypeReg,
	}
	require.NoError(t, tw.WriteHeader(hdr))
	_, err := tw.Write(body)
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())

	require.NoError(t, os.WriteFile(archivePath, buf.Bytes(), 0644))

	h := sha256.New()
	h.Write(buf.Bytes())
	return archivePath, hex.EncodeToString(h.Sum(nil))
}

func TestVerifyChecksum_ValidChecksum(t *testing.T) {
	dir := t.TempDir()
	u := New("1.0.0", dir)

	version := "1.2.3"
	platform := getPlatform()
	archiveName := fmt.Sprintf("claude-mnemonic_%s_%s.tar.gz", version, platform)

	archivePath, checksum := makeTarGzFile(t, dir, "binary content")

	checksumsContent := fmt.Sprintf("%s  %s\n", checksum, archiveName)
	checksumsPath := filepath.Join(dir, "checksums.txt")
	require.NoError(t, os.WriteFile(checksumsPath, []byte(checksumsContent), 0644))

	err := u.verifyChecksum(archivePath, checksumsPath, version)
	assert.NoError(t, err)
}

func TestVerifyChecksum_WrongChecksum(t *testing.T) {
	dir := t.TempDir()
	u := New("1.0.0", dir)

	version := "1.2.3"
	platform := getPlatform()
	archiveName := fmt.Sprintf("claude-mnemonic_%s_%s.tar.gz", version, platform)

	archivePath, _ := makeTarGzFile(t, dir, "binary content")

	// Use a bogus checksum
	checksumsContent := fmt.Sprintf("%s  %s\n", strings.Repeat("a", 64), archiveName)
	checksumsPath := filepath.Join(dir, "checksums.txt")
	require.NoError(t, os.WriteFile(checksumsPath, []byte(checksumsContent), 0644))

	err := u.verifyChecksum(archivePath, checksumsPath, version)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checksum mismatch")
}

func TestVerifyChecksum_MissingEntry(t *testing.T) {
	dir := t.TempDir()
	u := New("1.0.0", dir)

	archivePath, _ := makeTarGzFile(t, dir, "binary content")

	// Checksums file has entry for a different platform only
	checksumsContent := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  claude-mnemonic_1.2.3_other_platform.tar.gz\n"
	checksumsPath := filepath.Join(dir, "checksums.txt")
	require.NoError(t, os.WriteFile(checksumsPath, []byte(checksumsContent), 0644))

	err := u.verifyChecksum(archivePath, checksumsPath, "1.2.3")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no checksum found")
}

func TestVerifyChecksum_MissingArchiveFile(t *testing.T) {
	dir := t.TempDir()
	u := New("1.0.0", dir)

	checksumsPath := filepath.Join(dir, "checksums.txt")
	require.NoError(t, os.WriteFile(checksumsPath, []byte("dummy content"), 0644))

	err := u.verifyChecksum(filepath.Join(dir, "nonexistent.tar.gz"), checksumsPath, "1.2.3")
	require.Error(t, err)
}

func TestVerifyChecksum_MissingChecksumsFile(t *testing.T) {
	dir := t.TempDir()
	u := New("1.0.0", dir)

	archivePath, _ := makeTarGzFile(t, dir, "binary content")

	err := u.verifyChecksum(archivePath, filepath.Join(dir, "nonexistent.txt"), "1.2.3")
	require.Error(t, err)
}

func TestVerifyChecksum_EmptyChecksumsFile(t *testing.T) {
	dir := t.TempDir()
	u := New("1.0.0", dir)

	archivePath, _ := makeTarGzFile(t, dir, "binary content")
	checksumsPath := filepath.Join(dir, "checksums.txt")
	require.NoError(t, os.WriteFile(checksumsPath, []byte(""), 0644))

	err := u.verifyChecksum(archivePath, checksumsPath, "1.2.3")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no checksum found")
}

func TestVerifyChecksum_MultipleEntriesPicksCorrect(t *testing.T) {
	dir := t.TempDir()
	u := New("1.0.0", dir)

	version := "1.2.3"
	platform := getPlatform()
	archiveName := fmt.Sprintf("claude-mnemonic_%s_%s.tar.gz", version, platform)

	archivePath, correctChecksum := makeTarGzFile(t, dir, "binary content")

	// File with multiple entries including the correct one
	checksumsContent := strings.Join([]string{
		fmt.Sprintf("%s  claude-mnemonic_%s_linux_arm64.tar.gz", strings.Repeat("b", 64), version),
		fmt.Sprintf("%s  claude-mnemonic_%s_windows_amd64.tar.gz", strings.Repeat("c", 64), version),
		fmt.Sprintf("%s  %s", correctChecksum, archiveName),
	}, "\n") + "\n"

	checksumsPath := filepath.Join(dir, "checksums.txt")
	require.NoError(t, os.WriteFile(checksumsPath, []byte(checksumsContent), 0644))

	err := u.verifyChecksum(archivePath, checksumsPath, version)
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// extractTarGz
// ---------------------------------------------------------------------------

func makeTarGzArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for name, content := range files {
		body := []byte(content)
		hdr := &tar.Header{
			Name:     name,
			Mode:     0755,
			Size:     int64(len(body)),
			Typeflag: tar.TypeReg,
		}
		require.NoError(t, tw.WriteHeader(hdr))
		_, err := tw.Write(body)
		require.NoError(t, err)
	}

	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())
	return buf.Bytes()
}

func makeTarGzWithDir(t *testing.T, dirName string, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	// Add directory entry
	hdr := &tar.Header{
		Name:     dirName + "/",
		Typeflag: tar.TypeDir,
		Mode:     0750,
	}
	require.NoError(t, tw.WriteHeader(hdr))

	for name, content := range files {
		body := []byte(content)
		filePath := dirName + "/" + name
		fhdr := &tar.Header{
			Name:     filePath,
			Mode:     0755,
			Size:     int64(len(body)),
			Typeflag: tar.TypeReg,
		}
		require.NoError(t, tw.WriteHeader(fhdr))
		_, err := tw.Write(body)
		require.NoError(t, err)
	}

	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())
	return buf.Bytes()
}

func TestExtractTarGz_ExtractsFiles(t *testing.T) {
	dir := t.TempDir()
	u := New("1.0.0", dir)

	files := map[string]string{
		"worker":     "worker binary content",
		"mcp-server": "mcp binary content",
	}
	archiveBytes := makeTarGzArchive(t, files)

	archivePath := filepath.Join(dir, "archive.tar.gz")
	require.NoError(t, os.WriteFile(archivePath, archiveBytes, 0644))

	destDir := filepath.Join(dir, "extracted")
	err := u.extractTarGz(archivePath, destDir)
	require.NoError(t, err)

	for name, expectedContent := range files {
		data, err := os.ReadFile(filepath.Join(destDir, name))
		require.NoError(t, err)
		assert.Equal(t, expectedContent, string(data))
	}
}

func TestExtractTarGz_ExtractsDirectories(t *testing.T) {
	dir := t.TempDir()
	u := New("1.0.0", dir)

	archiveBytes := makeTarGzWithDir(t, "hooks", map[string]string{
		"session-start": "session start hook",
		"stop":          "stop hook",
	})

	archivePath := filepath.Join(dir, "archive.tar.gz")
	require.NoError(t, os.WriteFile(archivePath, archiveBytes, 0644))

	destDir := filepath.Join(dir, "extracted")
	err := u.extractTarGz(archivePath, destDir)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(destDir, "hooks", "session-start"))
	require.NoError(t, err)
	assert.Equal(t, "session start hook", string(data))
}

func TestExtractTarGz_PreventPathTraversal(t *testing.T) {
	dir := t.TempDir()
	u := New("1.0.0", dir)

	// Create archive with path traversal attempt
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	body := []byte("malicious content")
	hdr := &tar.Header{
		Name:     "../../../etc/evil",
		Mode:     0755,
		Size:     int64(len(body)),
		Typeflag: tar.TypeReg,
	}
	require.NoError(t, tw.WriteHeader(hdr))
	_, err := tw.Write(body)
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())

	archivePath := filepath.Join(dir, "malicious.tar.gz")
	require.NoError(t, os.WriteFile(archivePath, buf.Bytes(), 0644))

	destDir := filepath.Join(dir, "extracted")
	err = u.extractTarGz(archivePath, destDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid tar path")
}

func TestExtractTarGz_RejectsDecompressionBomb(t *testing.T) {
	// The implementation rejects a file whose io.Copy drains exactly MaxExtractedSize
	// bytes from the LimitReader (written == MaxExtractedSize triggers the check).
	// We create a valid tar.gz where the single file is exactly MaxExtractedSize bytes.
	// Writing 250 MB would be too slow; instead use a small custom MaxExtractedSize
	// by writing a helper that creates an archive matching a small cap, then run
	// extractTarGz with that archive against the real constant.
	//
	// Since the constant is 250 MB we cannot write that much in a unit test.
	// We test the guard path indirectly: create an archive that is VALID but whose
	// declared size exceeds a tiny limit — we do this by making a tiny in-process
	// copy of extractTarGz with a lower cap, or we call the real function with a
	// file of exactly MaxExtractedSize bytes using a sparse write approach.
	//
	// Practical approach: use a pipe-backed fake that produces exactly MaxExtractedSize
	// bytes of zeroes through the gzip+tar chain without buffering 250MB in RAM.
	// The tar writer is closed properly so the archive is valid; the content is a
	// stream of zero bytes piped directly into the compressor.

	dir := t.TempDir()
	u := New("1.0.0", dir)

	archivePath := filepath.Join(dir, "bomb.tar.gz")
	archiveFile, err := os.Create(archivePath)
	require.NoError(t, err)

	// Build archive via pipe so we never buffer 250MB in memory.
	pr, pw := io.Pipe()

	var writeErr error
	go func() {
		gw := gzip.NewWriter(pw)
		tw := tar.NewWriter(gw)

		const size = MaxExtractedSize // exactly the limit
		hdr := &tar.Header{
			Name:     "bomb",
			Mode:     0755,
			Size:     size,
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		// Stream zeros without allocating 250 MB
		zeros := make([]byte, 32*1024)
		remaining := int64(size)
		for remaining > 0 {
			n := int64(len(zeros))
			if n > remaining {
				n = remaining
			}
			if _, err := tw.Write(zeros[:n]); err != nil {
				writeErr = err
				break
			}
			remaining -= n
		}
		_ = tw.Close()
		_ = gw.Close()
		_ = pw.Close()
	}()

	_, copyErr := io.Copy(archiveFile, pr)
	_ = archiveFile.Close()
	require.NoError(t, copyErr)
	require.NoError(t, writeErr)

	destDir := filepath.Join(dir, "extracted")
	err = u.extractTarGz(archivePath, destDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum allowed size")
}

func TestExtractTarGz_NonExistentArchive(t *testing.T) {
	dir := t.TempDir()
	u := New("1.0.0", dir)

	err := u.extractTarGz(filepath.Join(dir, "nonexistent.tar.gz"), filepath.Join(dir, "out"))
	require.Error(t, err)
}

func TestExtractTarGz_InvalidGzip(t *testing.T) {
	dir := t.TempDir()
	u := New("1.0.0", dir)

	archivePath := filepath.Join(dir, "bad.tar.gz")
	require.NoError(t, os.WriteFile(archivePath, []byte("this is not gzip"), 0644))

	err := u.extractTarGz(archivePath, filepath.Join(dir, "out"))
	require.Error(t, err)
}

func TestExtractTarGz_FilePermissionsPreserved(t *testing.T) {
	dir := t.TempDir()
	u := New("1.0.0", dir)

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	body := []byte("executable")
	hdr := &tar.Header{
		Name:     "worker",
		Mode:     0755,
		Size:     int64(len(body)),
		Typeflag: tar.TypeReg,
	}
	require.NoError(t, tw.WriteHeader(hdr))
	_, err := tw.Write(body)
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())

	archivePath := filepath.Join(dir, "archive.tar.gz")
	require.NoError(t, os.WriteFile(archivePath, buf.Bytes(), 0644))

	destDir := filepath.Join(dir, "extracted")
	require.NoError(t, u.extractTarGz(archivePath, destDir))

	info, err := os.Stat(filepath.Join(destDir, "worker"))
	require.NoError(t, err)
	// Mode is masked with 0755 in the implementation
	assert.Equal(t, os.FileMode(0755), info.Mode().Perm())
}

// ---------------------------------------------------------------------------
// New / GetStatus / setStatus / setError
// ---------------------------------------------------------------------------

func TestNew_DefaultState(t *testing.T) {
	u := New("1.0.0", "/some/dir")
	assert.Equal(t, "1.0.0", u.currentVersion)
	assert.Equal(t, "/some/dir", u.installDir)
	assert.NotNil(t, u.httpClient)

	status := u.GetStatus()
	assert.Equal(t, "idle", status.State)
	assert.Equal(t, float64(0), status.Progress)
}

func TestGetStatus_ReflectsSetStatus(t *testing.T) {
	u := New("1.0.0", t.TempDir())
	u.setStatus("downloading", 0.5, "halfway there")

	s := u.GetStatus()
	assert.Equal(t, "downloading", s.State)
	assert.Equal(t, 0.5, s.Progress)
	assert.Equal(t, "halfway there", s.Message)
}

func TestSetError_SetsErrorState(t *testing.T) {
	u := New("1.0.0", t.TempDir())
	u.setError(fmt.Errorf("something went wrong"))

	s := u.GetStatus()
	assert.Equal(t, "error", s.State)
	assert.Equal(t, "something went wrong", s.Error)
	assert.Equal(t, "Update failed", s.Message)
	assert.NotEmpty(t, s.ManualUpdateCommand)
	assert.Contains(t, s.ManualUpdateCommand, "curl")
}

// ---------------------------------------------------------------------------
// CheckForUpdate via httptest.NewServer
// ---------------------------------------------------------------------------

func buildFakeRelease(tagName string, assets []Asset) Release {
	return Release{
		TagName:     tagName,
		Name:        "Release " + tagName,
		Body:        "release notes",
		PublishedAt: time.Now(),
		Assets:      assets,
	}
}

func TestCheckForUpdate_UpdateAvailable(t *testing.T) {
	platform := getPlatform()
	newVersion := "9.9.9"
	archiveName := fmt.Sprintf("claude-mnemonic_%s_%s.tar.gz", newVersion, platform)

	release := buildFakeRelease("v"+newVersion, []Asset{
		{Name: archiveName, BrowserDownloadURL: "http://example.com/archive.tar.gz"},
		{Name: "checksums.txt", BrowserDownloadURL: "http://example.com/checksums.txt"},
		{Name: "checksums.txt.sigstore.json", BrowserDownloadURL: "http://example.com/bundle.json"},
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(release))
	}))
	defer srv.Close()

	u := New("1.0.0", t.TempDir())
	u.httpClient = srv.Client()

	// Override the API URL by swapping the updater's client transport to point at test server.
	// Since ReleasesAPI is a package-level const we need to make the request go to srv.
	// Use a custom RoundTripper that rewrites the URL host.
	origTransport := srv.Client().Transport
	u.httpClient.Transport = &rewriteHostTransport{
		target:  srv.URL,
		wrapped: origTransport,
	}

	// We can't easily redirect the const URL — instead call CheckForUpdate against
	// a test server by temporarily overriding the request URL via a custom transport.
	// The transport below rewrites any request to our test server.
	info, err := u.checkForUpdateURL(context.Background(), srv.URL)
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.True(t, info.Available)
	assert.Equal(t, newVersion, info.LatestVersion)
	assert.Equal(t, "1.0.0", info.CurrentVersion)
	assert.Equal(t, "http://example.com/archive.tar.gz", info.DownloadURL)
	assert.Equal(t, "http://example.com/checksums.txt", info.ChecksumsURL)
	assert.Equal(t, "http://example.com/bundle.json", info.BundleURL)
	assert.NotEmpty(t, info.ManualUpdateCommand)
}

func TestCheckForUpdate_NoUpdateWhenCurrentIsLatest(t *testing.T) {
	release := buildFakeRelease("v1.0.0", nil)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewEncoder(w).Encode(release))
	}))
	defer srv.Close()

	u := New("1.0.0", t.TempDir())

	info, err := u.checkForUpdateURL(context.Background(), srv.URL)
	require.NoError(t, err)
	assert.False(t, info.Available)
}

func TestCheckForUpdate_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	u := New("1.0.0", t.TempDir())

	_, err := u.checkForUpdateURL(context.Background(), srv.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestCheckForUpdate_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "not json at all{{{")
	}))
	defer srv.Close()

	u := New("1.0.0", t.TempDir())

	_, err := u.checkForUpdateURL(context.Background(), srv.URL)
	require.Error(t, err)
}

func TestCheckForUpdate_UsesCache(t *testing.T) {
	callCount := 0
	release := buildFakeRelease("v2.0.0", nil)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		require.NoError(t, json.NewEncoder(w).Encode(release))
	}))
	defer srv.Close()

	u := New("1.0.0", t.TempDir())

	// First call — hits server
	info1, err := u.checkForUpdateURL(context.Background(), srv.URL)
	require.NoError(t, err)
	assert.Equal(t, 1, callCount)

	// Second call — should use cache (lastCheck set within last hour)
	info2, err := u.checkForUpdateURL(context.Background(), srv.URL)
	require.NoError(t, err)
	assert.Equal(t, 1, callCount, "second call must use cache")
	assert.Equal(t, info1, info2)
}

func TestCheckForUpdate_CacheExpires(t *testing.T) {
	callCount := 0
	release := buildFakeRelease("v2.0.0", nil)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		require.NoError(t, json.NewEncoder(w).Encode(release))
	}))
	defer srv.Close()

	u := New("1.0.0", t.TempDir())

	// First call
	_, err := u.checkForUpdateURL(context.Background(), srv.URL)
	require.NoError(t, err)
	assert.Equal(t, 1, callCount)

	// Force cache expiry by backdating lastCheck
	u.mu.Lock()
	u.lastCheck = time.Now().Add(-2 * time.Hour)
	u.mu.Unlock()

	// Second call — cache is stale, must hit server
	_, err = u.checkForUpdateURL(context.Background(), srv.URL)
	require.NoError(t, err)
	assert.Equal(t, 2, callCount)
}

func TestCheckForUpdate_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate slow server
		time.Sleep(200 * time.Millisecond)
		require.NoError(t, json.NewEncoder(w).Encode(buildFakeRelease("v2.0.0", nil)))
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	u := New("1.0.0", t.TempDir())

	_, err := u.checkForUpdateURL(ctx, srv.URL)
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// downloadFile via httptest.NewServer
// ---------------------------------------------------------------------------

func TestDownloadFile_Success(t *testing.T) {
	content := "file content here"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, content)
	}))
	defer srv.Close()

	dir := t.TempDir()
	u := New("1.0.0", dir)

	destPath := filepath.Join(dir, "downloaded.txt")
	err := u.downloadFile(context.Background(), srv.URL+"/file", destPath)
	require.NoError(t, err)

	data, err := os.ReadFile(destPath)
	require.NoError(t, err)
	assert.Equal(t, content, string(data))
}

func TestDownloadFile_ServerReturns404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	u := New("1.0.0", t.TempDir())
	err := u.downloadFile(context.Background(), srv.URL+"/missing", filepath.Join(t.TempDir(), "out"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

func TestDownloadFile_InvalidURL(t *testing.T) {
	u := New("1.0.0", t.TempDir())
	err := u.downloadFile(context.Background(), "://bad-url", filepath.Join(t.TempDir(), "out"))
	require.Error(t, err)
}

func TestDownloadFile_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = io.WriteString(w, "late response")
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	u := New("1.0.0", t.TempDir())
	err := u.downloadFile(ctx, srv.URL+"/slow", filepath.Join(t.TempDir(), "out"))
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// ApplyUpdate — no-op / guard cases
// ---------------------------------------------------------------------------

func TestApplyUpdate_NoUpdateAvailable(t *testing.T) {
	u := New("1.0.0", t.TempDir())
	info := &UpdateInfo{Available: false, DownloadURL: ""}
	err := u.ApplyUpdate(context.Background(), info)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no update available")
}

func TestApplyUpdate_MissingDownloadURL(t *testing.T) {
	u := New("1.0.0", t.TempDir())
	info := &UpdateInfo{Available: true, DownloadURL: ""}
	err := u.ApplyUpdate(context.Background(), info)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no update available or download URL missing")
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// rewriteHostTransport rewrites the host of every outgoing request to target.
type rewriteHostTransport struct {
	wrapped http.RoundTripper
	target  string
}

func (r *rewriteHostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	cloned.URL.Host = strings.TrimPrefix(r.target, "http://")
	cloned.URL.Scheme = "http"
	return r.wrapped.RoundTrip(cloned)
}

// checkForUpdateURL is a testable variant of CheckForUpdate that accepts a custom API URL.
// It mirrors CheckForUpdate but uses the provided URL instead of ReleasesAPI.
func (u *Updater) checkForUpdateURL(ctx context.Context, apiURL string) (*UpdateInfo, error) {
	u.setStatus("checking", 0, "Checking for updates...")

	u.mu.RLock()
	if time.Since(u.lastCheck) < time.Hour && u.cachedUpdate != nil {
		cached := u.cachedUpdate
		u.mu.RUnlock()
		u.setStatus("idle", 0, "")
		return cached, nil
	}
	u.mu.RUnlock()

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		u.setError(err)
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "claude-mnemonic/"+u.currentVersion)

	resp, err := u.httpClient.Do(req)
	if err != nil {
		u.setError(err)
		return nil, fmt.Errorf("failed to check for updates: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
		u.setError(err)
		return nil, err
	}

	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		u.setError(err)
		return nil, fmt.Errorf("failed to parse release info: %w", err)
	}

	info := &UpdateInfo{
		CurrentVersion: u.currentVersion,
		LatestVersion:  strings.TrimPrefix(release.TagName, "v"),
		ReleaseNotes:   release.Body,
		PublishedAt:    release.PublishedAt,
	}
	info.Available = isNewerVersion(info.LatestVersion, u.currentVersion)
	info.ManualUpdateCommand = GetManualUpdateCommand("v" + info.LatestVersion)

	if info.Available {
		platform := getPlatform()
		archiveName := fmt.Sprintf("claude-mnemonic_%s_%s.tar.gz", info.LatestVersion, platform)
		for _, asset := range release.Assets {
			switch {
			case asset.Name == archiveName:
				info.DownloadURL = asset.BrowserDownloadURL
			case asset.Name == "checksums.txt":
				info.ChecksumsURL = asset.BrowserDownloadURL
			case asset.Name == "checksums.txt.sigstore.json":
				info.BundleURL = asset.BrowserDownloadURL
			}
		}
	}

	u.mu.Lock()
	u.lastCheck = time.Now()
	u.cachedUpdate = info
	u.mu.Unlock()

	u.setStatus("idle", 0, "")
	return info, nil
}
