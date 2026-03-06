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

# Helper: safely write JSON via tmp file with validation
# Usage: safe_jq_write <jq_args...> <input_file>
# The last argument is treated as the input file, output goes to input_file.tmp
safe_jq_write() {
    local args=("$@")
    local input_file="${args[-1]}"
    local tmp_file="${input_file}.tmp"

    if jq "${args[@]}" > "$tmp_file"; then
        if jq . "$tmp_file" > /dev/null 2>&1; then
            mv "$tmp_file" "$input_file"
        else
            echo "ERROR: jq produced invalid JSON for $input_file, aborting"
            rm -f "$tmp_file"
            return 1
        fi
    else
        echo "ERROR: jq failed for $input_file, aborting"
        rm -f "$tmp_file"
        return 1
    fi
}

# Check that Claude Code directory exists
if [ ! -d "$HOME/.claude" ]; then
    echo "Warning: $HOME/.claude directory not found. Claude Code may not be installed."
    echo "Continuing anyway, but plugin may not function until Claude Code is installed."
fi

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
    # Validate jq version (1.6+ required for //= operator)
    JQ_VERSION=$(jq --version 2>/dev/null | sed 's/jq-//')
    JQ_MAJOR=$(echo "$JQ_VERSION" | cut -d. -f1)
    JQ_MINOR=$(echo "$JQ_VERSION" | cut -d. -f2)
    if [ -n "$JQ_MAJOR" ] && [ -n "$JQ_MINOR" ]; then
        if [ "$JQ_MAJOR" -lt 1 ] || { [ "$JQ_MAJOR" -eq 1 ] && [ "$JQ_MINOR" -lt 6 ]; }; then
            echo "ERROR: jq 1.6+ is required (found jq-$JQ_VERSION)"
            echo "Please upgrade jq: brew install jq (macOS) or apt-get install jq (Linux)"
            exit 1
        fi
    fi

    # Validate marketplace path exists and contains expected files
    if [ ! -d "$MARKETPLACE_PATH" ]; then
        echo "Warning: Marketplace directory not found at $MARKETPLACE_PATH"
        echo "Plugin files may not be copied to cache correctly."
    fi

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
    safe_jq_write --arg key "$PLUGIN_KEY" --argjson entry "$PLUGIN_ENTRY" \
        '.plugins[$key] = $entry' "$PLUGINS_FILE"

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

    safe_jq_write --arg key "$PLUGIN_KEY" --argjson statusline "$STATUSLINE_ENTRY" \
        '.enabledPlugins //= {} | .enabledPlugins[$key] = true | .statusLine = $statusline' "$SETTINGS_FILE"

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

    safe_jq_write --arg key "$MARKETPLACE_NAME" --argjson entry "$MARKETPLACE_ENTRY" \
        '.[$key] = $entry' "$MARKETPLACES_FILE"

    echo "Marketplace registered in known_marketplaces.json"

    # Register MCP server in settings.json
    MCP_BINARY="$MARKETPLACE_PATH/mcp-server"
    if [ -f "$MCP_BINARY" ]; then
        echo "Registering MCP server in settings.json..."

        # MCP server entry - note the escaped ${CLAUDE_PROJECT}
        MCP_ENTRY=$(cat <<'EOF'
{
    "command": "MCP_BINARY_PLACEHOLDER",
    "args": ["--project", "${CLAUDE_PROJECT}"],
    "env": {}
}
EOF
)
        # Replace placeholder with actual path
        MCP_ENTRY=$(echo "$MCP_ENTRY" | sed "s|MCP_BINARY_PLACEHOLDER|$MCP_BINARY|g")

        # Add or update mcpServers field
        if safe_jq_write --arg key "claude-mnemonic" --argjson entry "$MCP_ENTRY" \
            '.mcpServers //= {} | .mcpServers[$key] = $entry' "$SETTINGS_FILE"; then
            echo "MCP server registered successfully"
        else
            echo "Warning: Failed to register MCP server (jq error)"
        fi
    else
        echo "MCP server binary not found at $MCP_BINARY, skipping MCP registration"
    fi

    echo "Plugin registered successfully using jq"
else
    echo "ERROR: jq is required for plugin registration"
    echo "Please install jq: brew install jq (macOS) or apt-get install jq (Linux)"
    exit 1
fi
