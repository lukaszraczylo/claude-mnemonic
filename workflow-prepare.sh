#!/bin/bash
# Workflow prepare script for CI
# Called by shared GitHub Actions workflow before build/test steps

set -e

# Detect host OS for platform-specific setup (Windows needs SQLite)
HOST_OS=$(uname -s | tr '[:upper:]' '[:lower:]')

# Determine target platform for ONNX library download
# Use TARGET_GOOS/TARGET_GOARCH from CI matrix if available, otherwise auto-detect
if [ -n "$TARGET_GOOS" ] && [ -n "$TARGET_GOARCH" ]; then
    ONNX_PLATFORM="${TARGET_GOOS}-${TARGET_GOARCH}"
    echo "Target platform from CI matrix: $ONNX_PLATFORM"
else
    ONNX_PLATFORM="auto"
fi

# On Windows, install SQLite development headers for CGO
if [[ "$HOST_OS" == mingw* ]] || [[ "$HOST_OS" == msys* ]] || [[ "$HOST_OS" == cygwin* ]]; then
    echo "Installing SQLite for Windows..."

    # Download SQLite amalgamation and set up for CGO
    SQLITE_VERSION="3470200"
    SQLITE_YEAR="2024"
    SQLITE_DIR="/c/sqlite"
    SQLITE_URL="https://www.sqlite.org/${SQLITE_YEAR}/sqlite-amalgamation-${SQLITE_VERSION}.zip"

    mkdir -p "$SQLITE_DIR"
    curl -sSL "$SQLITE_URL" -o /tmp/sqlite.zip
    unzip -q /tmp/sqlite.zip -d /tmp/
    cp /tmp/sqlite-amalgamation-${SQLITE_VERSION}/* "$SQLITE_DIR/"
    rm -rf /tmp/sqlite.zip /tmp/sqlite-amalgamation-${SQLITE_VERSION}

    # Download Go modules first so we can patch sqlite-vec
    echo "Downloading Go modules..."
    go mod download

    # Find the sqlite-vec module and copy sqlite3.h there
    SQLITE_VEC_PATH=$(go list -m -f '{{.Dir}}' github.com/asg017/sqlite-vec-go-bindings 2>/dev/null || true)
    if [ -n "$SQLITE_VEC_PATH" ] && [ -d "$SQLITE_VEC_PATH/cgo" ]; then
        # Make module writable (it's read-only by default)
        chmod -R u+w "$SQLITE_VEC_PATH"
        cp "$SQLITE_DIR/sqlite3.h" "$SQLITE_VEC_PATH/cgo/"
        cp "$SQLITE_DIR/sqlite3.c" "$SQLITE_VEC_PATH/cgo/"
        echo "SQLite headers copied to $SQLITE_VEC_PATH/cgo/"
    fi

    # Tell linker to allow multiple definitions (both go-sqlite3 and sqlite-vec embed SQLite)
    echo "CGO_LDFLAGS=-Wl,--allow-multiple-definition" >> "$GITHUB_ENV"
    echo "SQLite setup complete"
fi

# Download ONNX runtime libraries for target platform
./scripts/download-onnx-libs.sh "$ONNX_PLATFORM"
