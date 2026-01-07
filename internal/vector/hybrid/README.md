# Hybrid Vector Storage

LEANN-inspired selective vector storage with hub detection and graph-based search.

## Testing

### macOS ARM64 Known Issue

Tests in this package currently fail to link on macOS ARM64 due to a known issue with `sqlite-vec-go-bindings/cgo`. This is a linking-time issue specific to macOS ARM64 and does not affect:

- Linux CI/CD (tests pass normally)
- Windows builds
- Runtime functionality (only affects test compilation)

The issue is caused by how macOS ARM64 handles CGO linking with multiple SQLite implementations (mattn/go-sqlite3 and sqlite-vec's embedded SQLite).

**Workaround for local development:**
- Run tests for other packages: `go test ./internal/db/... ./internal/search/...`
- The CI on Linux will test this package
- Or use a Linux container for local testing

**Status:** This does not affect production builds or CI/CD pipelines.

## Architecture

This package implements three storage strategies:

1. **StorageAlways** - Store all embeddings (backwards compatible)
2. **StorageHub** - Store only frequently-accessed "hub" embeddings
3. **StorageOnDemand** - Recompute all embeddings during search

See the main documentation for more details on the LEANN algorithm and hybrid storage approach.
