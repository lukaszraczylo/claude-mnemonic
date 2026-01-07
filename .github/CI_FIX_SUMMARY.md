# CI Test Failure Fix Summary

## Problem

Tests were failing in GitHub Actions for PR #20 because the `go-pr.yaml` shared workflow didn't support:
1. CGO_ENABLED=1 (required for sqlite-vec-go-bindings)
2. Build tags `-tags "fts5"` (required for SQLite FTS5 support)

## Root Cause

The hybrid vector storage feature in PR #20 depends on:
- `github.com/asg017/sqlite-vec-go-bindings/cgo` - requires CGO
- SQLite with FTS5 support - requires `-tags "fts5"` build flag

The shared workflow was running `go test` without these requirements.

## Solution

### 1. Updated shared-actions (commit 8f7f235)

**`.github/actions/go-test/action.yml`**
- Added `build-tags` input parameter
- Modified test command to use tags when provided

**`.github/workflows/go-pr.yaml`**
- Added `build-tags` input parameter
- Set `CGO_ENABLED: 1` in test job
- Pass tags to test command

**`.github/workflows/go-release-cgo.yaml`**
- Pass `build-tags: "fts5"` to go-test action

### 2. Updated claude-mnemonic (commit 90ab909)

**`.github/workflows/ci.yaml`**
- Pass `build-tags: "fts5"` to shared workflow

## What Was Already Working

The `workflow-prepare.sh` script already handled:
- Downloading ONNX runtime libraries
- Setting up SQLite on Windows for CGO

## Testing Status

✅ **Linux CI** - Should now pass (ubuntu-latest in GitHub Actions)
⚠️  **macOS Local** - Still has linking issues (macOS-specific sqlite-vec-go-bindings problem)

The macOS local testing issue is unrelated to CI and is caused by how sqlite-vec-go-bindings links on macOS ARM64 with Homebrew Go. This doesn't affect CI since it runs on Linux.

## Verification

The next CI run for PR #20 should pass. The workflow will:
1. Run `workflow-prepare.sh` to download ONNX libs
2. Run `go test -tags "fts5" -race -coverprofile=coverage.out -covermode=atomic ./...` with CGO_ENABLED=1
3. All packages including `internal/vector/hybrid` should compile and test successfully

## References

- PR #20: https://github.com/lukaszraczylo/claude-mnemonic/pull/20
- Failed CI run: https://github.com/lukaszraczylo/claude-mnemonic/actions/runs/20795930707/job/59729327008
- shared-actions fix: https://github.com/lukaszraczylo/shared-actions/commit/8f7f235
- claude-mnemonic fix: https://github.com/lukaszraczylo/claude-mnemonic/commit/90ab909
