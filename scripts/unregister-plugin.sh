#!/bin/bash
# Unregister claude-mnemonic plugin from Claude Code

set -e

PLUGINS_FILE="$HOME/.claude/plugins/installed_plugins.json"
SETTINGS_FILE="$HOME/.claude/settings.json"
MARKETPLACES_FILE="$HOME/.claude/plugins/known_marketplaces.json"
CACHE_DIR="$HOME/.claude/plugins/cache/claude-mnemonic"
PLUGIN_KEY="claude-mnemonic@claude-mnemonic"
MARKETPLACE_NAME="claude-mnemonic"

# Check if jq is available
if ! command -v jq &> /dev/null; then
    echo "Warning: jq not found, please manually remove plugin entries from:"
    echo "  - $PLUGINS_FILE (remove $PLUGIN_KEY)"
    echo "  - $SETTINGS_FILE (remove from enabledPlugins and statusLine)"
    echo "  - $MARKETPLACES_FILE (remove $MARKETPLACE_NAME)"
    echo "  - $CACHE_DIR (remove directory)"
    echo "  - $HOME/.claude-mnemonic (remove data directory)"
    exit 1
fi

# Remove from installed_plugins.json
if [ -f "$PLUGINS_FILE" ]; then
    jq --arg key "$PLUGIN_KEY" 'del(.plugins[$key])' "$PLUGINS_FILE" > "${PLUGINS_FILE}.tmp" \
        && mv "${PLUGINS_FILE}.tmp" "$PLUGINS_FILE"
    echo "Plugin removed from installed_plugins.json"
else
    echo "No plugins file found, skipping"
fi

# Remove from settings.json (enabledPlugins and statusLine if it points to our plugin)
if [ -f "$SETTINGS_FILE" ]; then
    # Remove from enabledPlugins and clear statusLine if it references our plugin
    jq --arg key "$PLUGIN_KEY" '
        del(.enabledPlugins[$key]) |
        if .statusLine.command and (.statusLine.command | contains("claude-mnemonic")) then
            del(.statusLine)
        else
            .
        end
    ' "$SETTINGS_FILE" > "${SETTINGS_FILE}.tmp" \
        && mv "${SETTINGS_FILE}.tmp" "$SETTINGS_FILE"
    echo "Plugin removed from settings.json"
fi

# Remove from known_marketplaces.json
if [ -f "$MARKETPLACES_FILE" ]; then
    jq --arg key "$MARKETPLACE_NAME" 'del(.[$key])' "$MARKETPLACES_FILE" > "${MARKETPLACES_FILE}.tmp" \
        && mv "${MARKETPLACES_FILE}.tmp" "$MARKETPLACES_FILE"
    echo "Marketplace removed from known_marketplaces.json"
fi

# Remove cache directory
if [ -d "$CACHE_DIR" ]; then
    rm -rf "$CACHE_DIR"
    echo "Cache directory removed"
fi

# Remove data directory (database, embeddings, etc.)
DATA_DIR="$HOME/.claude-mnemonic"
if [ -d "$DATA_DIR" ]; then
    rm -rf "$DATA_DIR"
    echo "Data directory removed ($DATA_DIR)"
fi

echo "Plugin unregistered successfully"
