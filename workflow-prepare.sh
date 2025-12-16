#!/bin/bash
# Workflow prepare script for CI
# Called by shared GitHub Actions workflow before build/test steps

set -e

# Detect OS
OS=$(uname -s | tr '[:upper:]' '[:lower:]')

# On Windows, install SQLite development headers for CGO
if [[ "$OS" == mingw* ]] || [[ "$OS" == msys* ]] || [[ "$OS" == cygwin* ]]; then
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

    echo "SQLite setup complete"
fi

# Download ONNX runtime libraries for current platform
./scripts/download-onnx-libs.sh auto
