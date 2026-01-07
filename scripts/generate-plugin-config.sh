#!/bin/bash
# Generate plugin configuration files with version substitution
# Called from .goreleaser.yaml before hooks

set -e

# Get version from GoReleaser environment variable
if [ -n "$GORELEASER_CURRENT_TAG" ]; then
    VERSION="${GORELEASER_CURRENT_TAG#v}"
    echo "Using version from GORELEASER_CURRENT_TAG: $VERSION"
else
    # Fallback: Use latest git tag instead of 0.0.0-dev
    # This prevents version mismatch when Claude installs from GitHub
    LATEST_TAG=$(git tag --sort=-v:refname | head -1 || echo "v0.0.0-dev")
    if [ -z "$LATEST_TAG" ]; then
        LATEST_TAG="v0.0.0-dev"
    fi
    VERSION="${LATEST_TAG#v}"
    echo "GORELEASER_CURRENT_TAG not set, using latest git tag: $VERSION"
fi

# Source and destination directories
TEMPLATE_DIR="plugin/.claude-plugin"
OUTPUT_DIR=".claude-plugin"

# Create output directory
mkdir -p "$OUTPUT_DIR"

# Generate plugin.json
if [ -f "$TEMPLATE_DIR/plugin.json.tpl" ]; then
    sed "s/{{ .Version }}/$VERSION/g; s/{{.Version}}/$VERSION/g" \
        "$TEMPLATE_DIR/plugin.json.tpl" > "$OUTPUT_DIR/plugin.json"
    echo "Generated $OUTPUT_DIR/plugin.json"
else
    echo "ERROR: Template file not found: $TEMPLATE_DIR/plugin.json.tpl"
    exit 1
fi

# Generate marketplace.json
if [ -f "$TEMPLATE_DIR/marketplace.json.tpl" ]; then
    sed "s/{{ .Version }}/$VERSION/g; s/{{.Version}}/$VERSION/g" \
        "$TEMPLATE_DIR/marketplace.json.tpl" > "$OUTPUT_DIR/marketplace.json"
    echo "Generated $OUTPUT_DIR/marketplace.json"
else
    echo "ERROR: Template file not found: $TEMPLATE_DIR/marketplace.json.tpl"
    exit 1
fi

echo "Plugin config files generated successfully with version $VERSION"
