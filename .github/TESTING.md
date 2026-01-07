# Testing Guide

## Running Tests

### Full Test Suite

```bash
make test
```

This runs all tests with proper build tags (`-tags "fts5"`) and race detection.

### Test Coverage

```bash
make test-coverage
```

Generates coverage reports in `coverage.out` and `coverage.html`.

## Platform-Specific Issues

### macOS ARM64

**Known Issue:** Tests for `internal/vector/hybrid` fail to compile on macOS ARM64 due to CGO linking issues with `sqlite-vec-go-bindings`.

**Symptoms:**
```
ld: symbol(s) not found for architecture arm64
FAIL github.com/lukaszraczylo/claude-mnemonic/internal/vector/hybrid [build failed]
```

**Root Cause:** Conflict between `mattn/go-sqlite3` and `sqlite-vec`'s embedded SQLite when linking test binaries on macOS ARM64.

**Impact:**
- Local development on macOS ARM64 cannot run hybrid package tests
- **Does not affect:** Linux CI, production builds, or runtime functionality
- All other 41 packages test successfully

**Workarounds:**
1. Test other packages individually: `go test ./internal/db/... ./internal/search/...`
2. Use Docker/Linux container for comprehensive local testing
3. Rely on CI for hybrid package validation (recommended)

**CI Status:** Tests pass successfully on Linux (GitHub Actions ubuntu-latest runner).

### Linux / CI

Tests run successfully with:
- `CGO_ENABLED=1`
- `-tags "fts5"` build flag
- ONNX runtime libraries (auto-downloaded by `workflow-prepare.sh`)

### Windows

`workflow-prepare.sh` handles Windows-specific SQLite setup automatically.

## CI Configuration

The CI workflow (`.github/workflows/ci.yaml`) uses the shared-actions workflow with:

```yaml
build-tags: "fts5"  # Required for SQLite FTS5 support
go-version: ">=1.24"
lfs: true
```

The `workflow-prepare.sh` script runs before tests to:
1. Download ONNX runtime libraries for the target platform
2. Set up SQLite headers on Windows
3. Configure CGO linker flags as needed

## Test Organization

- **Unit tests**: Package-level tests in `*_test.go` files
- **Integration tests**: Database and system integration in `*integration_test.go`
- **Benchmarks**: Performance tests in `*_test.go` with `Benchmark*` functions

## Debugging Test Failures

If tests fail in CI but pass locally (or vice versa):

1. Check that build tags are consistent: `-tags "fts5"`
2. Verify CGO is enabled: `CGO_ENABLED=1`
3. Ensure ONNX libraries are present (run `make setup-libs`)
4. Check platform-specific issues (see above)
5. Review `workflow-prepare.sh` for platform-specific setup

## Known Limitations

- **macOS ARM64**: Cannot compile hybrid package tests (see above)
- **Test caching**: Some tests use real databases and may need cache clearing with `go clean -testcache`
- **Race detector**: Increases test time significantly but is required for CI
