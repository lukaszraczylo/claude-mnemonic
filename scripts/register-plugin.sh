#!/bin/bash
# Register claude-mnemonic plugin with Claude Code

set -e

PLUGINS_FILE="$HOME/.claude/plugins/installed_plugins.json"
SETTINGS_FILE="$HOME/.claude/settings.json"
MARKETPLACES_FILE="$HOME/.claude/plugins/known_marketplaces.json"
PLUGIN_KEY="claude-mnemonic@claude-mnemonic"
MARKETPLACE_NAME="claude-mnemonic"
MARKETPLACE_PATH="$HOME/.claude/plugins/marketplaces/claude-mnemonic"

# Get version from git tags (same as Makefile), or use argument if provided
VERSION="${1:-$(git describe --tags --always --dirty 2>/dev/null || echo "dev")}"
CACHE_BASE="$HOME/.claude/plugins/cache/claude-mnemonic/claude-mnemonic"
CACHE_PATH="$CACHE_BASE/$VERSION"
TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%S.000Z")

# Ensure plugins directory exists
mkdir -p "$HOME/.claude/plugins"

# Clean up old cache versions to prevent stale binaries
if [ -d "$CACHE_BASE" ]; then
    echo "Cleaning up old cache versions..."
    find "$CACHE_BASE" -mindepth 1 -maxdepth 1 -type d ! -name "$VERSION" -exec rm -rf {} \; 2>/dev/null || true
fi

# Create installed_plugins.json if it doesn't exist
if [ ! -f "$PLUGINS_FILE" ]; then
    echo '{"version": 2, "plugins": {}}' > "$PLUGINS_FILE"
fi

# Create settings.json if it doesn't exist
if [ ! -f "$SETTINGS_FILE" ]; then
    echo '{}' > "$SETTINGS_FILE"
fi

# Create known_marketplaces.json if it doesn't exist
if [ ! -f "$MARKETPLACES_FILE" ]; then
    echo '{}' > "$MARKETPLACES_FILE"
fi

# Check if jq is available
if command -v jq &> /dev/null; then
    # Ensure cache directory exists and copy plugin files
    mkdir -p "$CACHE_PATH/.claude-plugin"
    mkdir -p "$CACHE_PATH/hooks"
    mkdir -p "$CACHE_PATH/commands"

    # Copy files from marketplace to cache
    cp -r "$MARKETPLACE_PATH/"* "$CACHE_PATH/" 2>/dev/null || true

    # Use jq for proper JSON manipulation
    PLUGIN_ENTRY=$(cat <<EOF
[{
    "scope": "user",
    "installPath": "$CACHE_PATH",
    "version": "$VERSION",
    "installedAt": "$TIMESTAMP",
    "lastUpdated": "$TIMESTAMP",
    "isLocal": true
}]
EOF
)

    # Add or update the plugin entry in installed_plugins.json
    jq --arg key "$PLUGIN_KEY" --argjson entry "$PLUGIN_ENTRY" \
        '.plugins[$key] = $entry' "$PLUGINS_FILE" > "${PLUGINS_FILE}.tmp" \
        && mv "${PLUGINS_FILE}.tmp" "$PLUGINS_FILE"

    echo "Plugin registered in installed_plugins.json"

    # Enable the plugin in settings.json and configure statusline
    # First ensure enabledPlugins object exists, then add our plugin
    STATUSLINE_CMD="$MARKETPLACE_PATH/hooks/statusline"
    STATUSLINE_ENTRY=$(cat <<EOF
{
    "type": "command",
    "command": "$STATUSLINE_CMD",
    "padding": 0
}
EOF
)

    jq --arg key "$PLUGIN_KEY" --argjson statusline "$STATUSLINE_ENTRY" \
        '.enabledPlugins //= {} | .enabledPlugins[$key] = true | .statusLine = $statusline' "$SETTINGS_FILE" > "${SETTINGS_FILE}.tmp" \
        && mv "${SETTINGS_FILE}.tmp" "$SETTINGS_FILE"

    echo "Plugin enabled in settings.json"
    echo "Statusline configured in settings.json"

    # Register the marketplace in known_marketplaces.json
    MARKETPLACE_ENTRY=$(cat <<EOF
{
    "source": {
        "source": "directory",
        "path": "$MARKETPLACE_PATH"
    },
    "installLocation": "$MARKETPLACE_PATH",
    "lastUpdated": "$TIMESTAMP"
}
EOF
)

    jq --arg key "$MARKETPLACE_NAME" --argjson entry "$MARKETPLACE_ENTRY" \
        '.[$key] = $entry' "$MARKETPLACES_FILE" > "${MARKETPLACES_FILE}.tmp" \
        && mv "${MARKETPLACES_FILE}.tmp" "$MARKETPLACES_FILE"

    echo "Marketplace registered in known_marketplaces.json"
    echo "Plugin registered successfully using jq"
else
    echo "ERROR: jq is required for plugin registration"
    echo "Please install jq: brew install jq (macOS) or apt-get install jq (Linux)"
    exit 1
fi
