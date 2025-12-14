#!/bin/bash
# Unregister claude-mnemonic plugin from Claude Code

set -e

PLUGINS_FILE="$HOME/.claude/plugins/installed_plugins.json"
PLUGIN_KEY="claude-mnemonic@claude-mnemonic"

if [ ! -f "$PLUGINS_FILE" ]; then
    echo "No plugins file found, nothing to unregister"
    exit 0
fi

# Check if jq is available
if command -v jq &> /dev/null; then
    # Use jq to remove the plugin entry
    jq --arg key "$PLUGIN_KEY" 'del(.plugins[$key])' "$PLUGINS_FILE" > "${PLUGINS_FILE}.tmp" \
        && mv "${PLUGINS_FILE}.tmp" "$PLUGINS_FILE"
    echo "Plugin unregistered successfully"
else
    echo "Warning: jq not found, please manually remove $PLUGIN_KEY from $PLUGINS_FILE"
fi
