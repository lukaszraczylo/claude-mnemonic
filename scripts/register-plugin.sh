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
if ! cp -r "$MARKETPLACE_PATH/"* "$CACHE_PATH/" 2>/dev/null; then
    echo "ERROR: Failed to copy plugin files to cache directory"
    exit 1
fi

# Verify critical files exist in cache
for f in worker mcp-server hooks/hooks.json .claude-plugin/plugin.json; do
    if [ ! -f "$CACHE_PATH/$f" ]; then
        echo "WARNING: Expected file $f not found in cache after copy"
    fi
done

# --- JSON registration ---
# Uses jq if available, falls back to python3 for systems without jq.

STATUSLINE_CMD="\${CLAUDE_PLUGIN_ROOT}/hooks/statusline"

# Helper: register plugin using python3 (jq-free fallback)
register_with_python() {
    python3 - "$PLUGINS_FILE" "$SETTINGS_FILE" "$MARKETPLACES_FILE" \
        "$PLUGIN_KEY" "$CACHE_PATH" "$VERSION" "$TIMESTAMP" \
        "$STATUSLINE_CMD" "$MARKETPLACE_NAME" "$MARKETPLACE_PATH" <<'PYEOF'
import json, sys, os

plugins_file, settings_file, marketplaces_file = sys.argv[1], sys.argv[2], sys.argv[3]
plugin_key, cache_path, version, timestamp = sys.argv[4], sys.argv[5], sys.argv[6], sys.argv[7]
statusline_cmd, marketplace_name, marketplace_path = sys.argv[8], sys.argv[9], sys.argv[10]

def load_json(path):
    try:
        with open(path) as f:
            return json.load(f)
    except (json.JSONDecodeError, FileNotFoundError):
        return {}

def save_json(path, data):
    tmp = path + ".tmp"
    with open(tmp, "w") as f:
        json.dump(data, f, indent=2)
        f.write("\n")
    os.replace(tmp, path)

# 1. installed_plugins.json
plugins = load_json(plugins_file)
plugins.setdefault("version", 2)
plugins.setdefault("plugins", {})
plugins["plugins"][plugin_key] = [{
    "scope": "user",
    "installPath": cache_path,
    "version": version,
    "installedAt": timestamp,
    "lastUpdated": timestamp,
    "isLocal": True
}]
save_json(plugins_file, plugins)
print("Plugin registered in installed_plugins.json")

# 2. settings.json
settings = load_json(settings_file)
settings.setdefault("enabledPlugins", {})
settings["enabledPlugins"][plugin_key] = True
settings["statusLine"] = {
    "type": "command",
    "command": statusline_cmd,
    "padding": 0
}
save_json(settings_file, settings)
print("Plugin enabled in settings.json")
print("Statusline configured in settings.json")

# 3. known_marketplaces.json
marketplaces = load_json(marketplaces_file)
marketplaces[marketplace_name] = {
    "source": {"source": "directory", "path": marketplace_path},
    "installLocation": marketplace_path,
    "lastUpdated": timestamp
}
save_json(marketplaces_file, marketplaces)
print("Marketplace registered in known_marketplaces.json")
PYEOF
}

# Detect JSON tool and register
if command -v jq &> /dev/null; then
    # Validate jq version (1.6+ required for //= operator)
    JQ_VERSION=$(jq --version 2>/dev/null | sed 's/jq-//')
    JQ_MAJOR=$(echo "$JQ_VERSION" | cut -d. -f1)
    JQ_MINOR=$(echo "$JQ_VERSION" | cut -d. -f2)
    if [ -n "$JQ_MAJOR" ] && [ -n "$JQ_MINOR" ]; then
        if [ "$JQ_MAJOR" -lt 1 ] || { [ "$JQ_MAJOR" -eq 1 ] && [ "$JQ_MINOR" -lt 6 ]; }; then
            echo "jq $JQ_VERSION found but too old (need 1.6+), trying python3..."
            if command -v python3 &> /dev/null; then
                register_with_python
            else
                echo "ERROR: jq 1.6+ or python3 is required for plugin registration"
                exit 1
            fi
            echo "Plugin registered successfully using python3"
            exit 0
        fi
    fi

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
    echo "Plugin registered successfully using jq"

elif command -v python3 &> /dev/null; then
    register_with_python
    echo "Plugin registered successfully using python3"
else
    echo "ERROR: jq or python3 is required for plugin registration"
    echo "Please install one: brew install jq (macOS) or apt-get install jq (Linux)"
    exit 1
fi
