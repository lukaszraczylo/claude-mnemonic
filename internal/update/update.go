// Package update provides self-update functionality for claude-mnemonic.
package update

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	GitHubRepo       = "lukaszraczylo/claude-mnemonic"
	ReleasesAPI      = "https://api.github.com/repos/" + GitHubRepo + "/releases/latest"
	CheckInterval    = 24 * time.Hour
	MaxExtractedSize = 100 * 1024 * 1024 // 100MB max per extracted file
	RestartDelay     = 500 * time.Millisecond
)

// Release represents a GitHub release.
type Release struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	PublishedAt time.Time `json:"published_at"`
	Assets      []Asset   `json:"assets"`
	Body        string    `json:"body"`
}

// Asset represents a release asset.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// UpdateInfo contains information about an available update.
type UpdateInfo struct {
	Available      bool      `json:"available"`
	CurrentVersion string    `json:"current_version"`
	LatestVersion  string    `json:"latest_version"`
	ReleaseNotes   string    `json:"release_notes,omitempty"`
	PublishedAt    time.Time `json:"published_at,omitempty"`
	DownloadURL    string    `json:"download_url,omitempty"`
	ChecksumsURL   string    `json:"checksums_url,omitempty"`
	BundleURL      string    `json:"bundle_url,omitempty"` // Sigstore bundle (.sigstore.json)
}

// UpdateStatus represents the current update status.
type UpdateStatus struct {
	State    string  `json:"state"` // "idle", "checking", "downloading", "verifying", "applying", "done", "error"
	Progress float64 `json:"progress"`
	Message  string  `json:"message"`
	Error    string  `json:"error,omitempty"`
}

// Updater handles self-updates.
type Updater struct {
	currentVersion string
	installDir     string
	httpClient     *http.Client

	mu           sync.RWMutex
	status       UpdateStatus
	lastCheck    time.Time
	cachedUpdate *UpdateInfo
}

// New creates a new Updater.
func New(currentVersion, installDir string) *Updater {
	return &Updater{
		currentVersion: currentVersion,
		installDir:     installDir,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		status: UpdateStatus{State: "idle"},
	}
}

// GetStatus returns the current update status.
func (u *Updater) GetStatus() UpdateStatus {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.status
}

func (u *Updater) setStatus(state string, progress float64, message string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.status = UpdateStatus{
		State:    state,
		Progress: progress,
		Message:  message,
	}
}

func (u *Updater) setError(err error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.status = UpdateStatus{
		State:   "error",
		Message: "Update failed",
		Error:   err.Error(),
	}
}

// CheckForUpdate checks if a new version is available.
func (u *Updater) CheckForUpdate(ctx context.Context) (*UpdateInfo, error) {
	u.setStatus("checking", 0, "Checking for updates...")

	// Check cache first (within last hour)
	u.mu.RLock()
	if time.Since(u.lastCheck) < time.Hour && u.cachedUpdate != nil {
		cached := u.cachedUpdate
		u.mu.RUnlock()
		u.setStatus("idle", 0, "")
		return cached, nil
	}
	u.mu.RUnlock()

	req, err := http.NewRequestWithContext(ctx, "GET", ReleasesAPI, nil)
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

	// Compare versions
	info.Available = isNewerVersion(info.LatestVersion, u.currentVersion)

	if info.Available {
		// Find download URLs for current platform
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

		if info.DownloadURL == "" {
			log.Warn().Str("platform", platform).Msg("No release asset found for current platform")
		}
	}

	// Cache the result
	u.mu.Lock()
	u.lastCheck = time.Now()
	u.cachedUpdate = info
	u.mu.Unlock()

	u.setStatus("idle", 0, "")
	return info, nil
}

// ApplyUpdate downloads and applies the update.
func (u *Updater) ApplyUpdate(ctx context.Context, info *UpdateInfo) error {
	if !info.Available || info.DownloadURL == "" {
		return fmt.Errorf("no update available or download URL missing")
	}

	tmpDir, err := os.MkdirTemp("", "claude-mnemonic-update-*")
	if err != nil {
		u.setError(err)
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Step 1: Download checksums and sigstore bundle
	u.setStatus("downloading", 0.1, "Downloading checksums...")

	checksumsPath := filepath.Join(tmpDir, "checksums.txt")
	bundlePath := filepath.Join(tmpDir, "checksums.txt.sigstore.json")

	if info.ChecksumsURL != "" {
		if err := u.downloadFile(ctx, info.ChecksumsURL, checksumsPath); err != nil {
			u.setError(err)
			return fmt.Errorf("failed to download checksums: %w", err)
		}
	}

	if info.BundleURL != "" {
		if err := u.downloadFile(ctx, info.BundleURL, bundlePath); err != nil {
			u.setError(err)
			return fmt.Errorf("failed to download sigstore bundle: %w", err)
		}
	}

	// Step 2: Verify sigstore bundle with cosign (if available)
	u.setStatus("verifying", 0.2, "Verifying signature...")

	if info.ChecksumsURL != "" && info.BundleURL != "" {
		if err := u.verifySigstoreBundle(ctx, checksumsPath, bundlePath); err != nil {
			// Log warning but continue - signature verification is optional if cosign isn't installed
			log.Warn().Err(err).Msg("Signature verification failed or skipped")
		} else {
			log.Info().Msg("Sigstore signature verification passed")
		}
	}

	// Step 3: Download the archive
	u.setStatus("downloading", 0.3, "Downloading update...")

	archivePath := filepath.Join(tmpDir, "release.tar.gz")
	if err := u.downloadFile(ctx, info.DownloadURL, archivePath); err != nil {
		u.setError(err)
		return fmt.Errorf("failed to download release: %w", err)
	}

	// Step 4: Verify checksum
	u.setStatus("verifying", 0.6, "Verifying checksum...")

	if info.ChecksumsURL != "" {
		if err := u.verifyChecksum(archivePath, checksumsPath, info.LatestVersion); err != nil {
			u.setError(err)
			return fmt.Errorf("checksum verification failed: %w", err)
		}
		log.Info().Msg("Checksum verification passed")
	}

	// Step 5: Extract archive
	u.setStatus("applying", 0.7, "Extracting files...")

	extractDir := filepath.Join(tmpDir, "extracted")
	if err := u.extractTarGz(archivePath, extractDir); err != nil {
		u.setError(err)
		return fmt.Errorf("failed to extract archive: %w", err)
	}

	// Step 6: Replace binaries
	u.setStatus("applying", 0.85, "Installing update...")

	if err := u.replaceBinaries(extractDir); err != nil {
		u.setError(err)
		return fmt.Errorf("failed to replace binaries: %w", err)
	}

	// Clear cache so next check shows no update available
	u.mu.Lock()
	u.cachedUpdate = nil
	u.lastCheck = time.Time{}
	u.mu.Unlock()

	u.setStatus("done", 1.0, fmt.Sprintf("Updated to v%s. Restart required.", info.LatestVersion))
	log.Info().Str("version", info.LatestVersion).Msg("Update applied successfully")

	return nil
}

func (u *Updater) downloadFile(ctx context.Context, url, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "claude-mnemonic/"+u.currentVersion)

	resp, err := u.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	f, err := os.Create(destPath) // #nosec G304 -- destPath is constructed internally from temp directory
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	return err
}

func (u *Updater) verifySigstoreBundle(ctx context.Context, checksumsPath, bundlePath string) error {
	// Check if cosign is available
	if _, err := exec.LookPath("cosign"); err != nil {
		return fmt.Errorf("cosign not installed: %w", err)
	}

	// Verify sigstore bundle - uses keyless verification with certificate identity
	// The bundle contains the signature, certificate, and transparency log entry
	// Certificate identity matches GitHub Actions workflow for this repo
	cmd := exec.CommandContext(ctx, "cosign", "verify-blob",
		"--bundle", bundlePath,
		"--certificate-identity-regexp", "https://github.com/lukaszraczylo/claude-mnemonic/.*",
		"--certificate-oidc-issuer", "https://token.actions.githubusercontent.com",
		checksumsPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("cosign verification failed: %w, output: %s", err, string(output))
	}

	return nil
}

func (u *Updater) verifyChecksum(archivePath, checksumsPath, version string) error {
	// Read checksums file
	data, err := os.ReadFile(checksumsPath) // #nosec G304 -- checksumsPath is constructed internally from temp directory
	if err != nil {
		return fmt.Errorf("failed to read checksums: %w", err)
	}

	// Calculate archive checksum
	f, err := os.Open(archivePath) // #nosec G304 -- archivePath is constructed internally from temp directory
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	actualChecksum := hex.EncodeToString(h.Sum(nil))

	// Find expected checksum for our archive
	platform := getPlatform()
	expectedName := fmt.Sprintf("claude-mnemonic_%s_%s.tar.gz", version, platform)

	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.Fields(line)
		if len(parts) >= 2 && strings.HasSuffix(parts[1], expectedName) {
			if parts[0] == actualChecksum {
				return nil
			}
			return fmt.Errorf("checksum mismatch: expected %s, got %s", parts[0], actualChecksum)
		}
	}

	return fmt.Errorf("no checksum found for %s", expectedName)
}

func (u *Updater) extractTarGz(archivePath, destDir string) error {
	if err := os.MkdirAll(destDir, 0750); err != nil {
		return err
	}

	f, err := os.Open(archivePath) // #nosec G304 -- archivePath is constructed internally from temp directory
	if err != nil {
		return err
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		// #nosec G305 -- path traversal is prevented by the check below
		target := filepath.Join(destDir, header.Name)

		// Prevent path traversal
		if !strings.HasPrefix(target, filepath.Clean(destDir)+string(os.PathSeparator)) {
			return fmt.Errorf("invalid tar path: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0750); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0750); err != nil {
				return err
			}
			// #nosec G304,G115 -- target is validated above; mode from trusted tar header
			outFile, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode)&0755)
			if err != nil {
				return err
			}
			// Limit extraction size to prevent decompression bombs
			written, err := io.Copy(outFile, io.LimitReader(tr, MaxExtractedSize))
			if err != nil {
				_ = outFile.Close()
				return err
			}
			if written == MaxExtractedSize {
				_ = outFile.Close()
				return fmt.Errorf("file %s exceeds maximum allowed size", header.Name)
			}
			if err := outFile.Close(); err != nil {
				return err
			}
		}
	}

	return nil
}

func (u *Updater) replaceBinaries(extractDir string) error {
	// Files to replace
	binaries := []struct {
		src  string
		dest string
	}{
		{"worker", filepath.Join(u.installDir, "worker")},
		{"mcp-server", filepath.Join(u.installDir, "mcp-server")},
		{"hooks/session-start", filepath.Join(u.installDir, "hooks", "session-start")},
		{"hooks/user-prompt", filepath.Join(u.installDir, "hooks", "user-prompt")},
		{"hooks/post-tool-use", filepath.Join(u.installDir, "hooks", "post-tool-use")},
		{"hooks/stop", filepath.Join(u.installDir, "hooks", "stop")},
		{"hooks/subagent-stop", filepath.Join(u.installDir, "hooks", "subagent-stop")},
	}

	for _, b := range binaries {
		src := filepath.Join(extractDir, b.src)
		if _, err := os.Stat(src); os.IsNotExist(err) {
			continue // Skip if not in archive
		}

		// Backup existing binary
		if _, err := os.Stat(b.dest); err == nil {
			backup := b.dest + ".bak"
			if err := os.Rename(b.dest, backup); err != nil {
				return fmt.Errorf("failed to backup %s: %w", b.dest, err)
			}
			defer func(backup, dest string) {
				// Clean up backup on success, restore on failure
				if _, err := os.Stat(dest); err == nil {
					_ = os.Remove(backup)
				} else {
					_ = os.Rename(backup, dest)
				}
			}(backup, b.dest)
		}

		// Copy new binary
		if err := copyFile(src, b.dest); err != nil {
			return fmt.Errorf("failed to install %s: %w", b.dest, err)
		}

		// Make executable
		// #nosec G302 -- executables require 0755 permissions
		if err := os.Chmod(b.dest, 0755); err != nil {
			return fmt.Errorf("failed to chmod %s: %w", b.dest, err)
		}
	}

	return nil
}

func copyFile(src, dst string) error {
	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(dst), 0750); err != nil {
		return err
	}

	in, err := os.Open(src) // #nosec G304 -- src is constructed internally from extracted temp directory
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst) // #nosec G304 -- dst is constructed internally from install directory
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func getPlatform() string {
	os := runtime.GOOS
	arch := runtime.GOARCH

	// Map to release naming convention
	return fmt.Sprintf("%s_%s", os, arch)
}

func isNewerVersion(latest, current string) bool {
	// Strip 'v' prefix if present
	latest = strings.TrimPrefix(latest, "v")
	current = strings.TrimPrefix(current, "v")

	// For dev/dirty builds, extract the base version for comparison
	// e.g., "0.3.5-2-gca711a8-dirty" -> "0.3.5"
	currentBase := current
	if idx := strings.Index(current, "-"); idx > 0 {
		currentBase = current[:idx]
	}

	// Simple semver comparison using base version
	latestParts := strings.Split(latest, ".")
	currentParts := strings.Split(currentBase, ".")

	for i := 0; i < len(latestParts) && i < len(currentParts); i++ {
		latestNum, _ := strconv.Atoi(latestParts[i])
		currentNum, _ := strconv.Atoi(currentParts[i])

		if latestNum > currentNum {
			return true
		}
		if latestNum < currentNum {
			return false
		}
	}

	return len(latestParts) > len(currentParts)
}

// Restart spawns the new worker binary and exits the current process.
// This should be called after a successful update to apply the new version.
func (u *Updater) Restart() error {
	workerPath := filepath.Join(u.installDir, "worker")

	// Verify the new binary exists
	if _, err := os.Stat(workerPath); err != nil {
		return fmt.Errorf("new worker binary not found: %w", err)
	}

	log.Info().Str("path", workerPath).Msg("Restarting worker with new binary")

	// Use nohup to start a detached process that survives parent exit
	// The new worker will retry binding to the port after the old process exits
	cmd := exec.Command("nohup", workerPath) // #nosec G204 -- workerPath is from internal installDir
	cmd.Stdout = nil                         // Detach stdout
	cmd.Stderr = nil                         // Detach stderr
	cmd.Stdin = nil                          // Detach stdin
	cmd.Env = append(os.Environ(), "CLAUDE_MNEMONIC_RESTART=1")

	// Start in background - don't wait
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start new worker: %w", err)
	}

	// Release the child process so it's not a zombie
	go func() {
		_ = cmd.Wait()
	}()

	log.Info().Int("new_pid", cmd.Process.Pid).Msg("New worker started, exiting old process")

	// Give a moment for the log to flush
	time.Sleep(100 * time.Millisecond)

	// Exit current process - the new one will bind to the port
	os.Exit(0)

	return nil // Never reached
}
