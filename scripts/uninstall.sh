#!/bin/bash
# Claude Mnemonic - Uninstallation Script
# Usage: curl -sSL https://raw.githubusercontent.com/lukaszraczylo/claude-mnemonic/main/scripts/uninstall.sh | bash
#
# Options:
#   --keep-data    Keep the data directory (~/.claude-mnemonic/)
#   --purge        Remove everything including data (default)

set -e

# Configuration
INSTALL_DIR="$HOME/.claude/plugins/marketplaces/claude-mnemonic"
CACHE_DIR="$HOME/.claude/plugins/cache/claude-mnemonic"
DATA_DIR="$HOME/.claude-mnemonic"
PLUGINS_FILE="$HOME/.claude/plugins/installed_plugins.json"
SETTINGS_FILE="$HOME/.claude/settings.json"
MARKETPLACES_FILE="$HOME/.claude/plugins/known_marketplaces.json"
PLUGIN_KEY="claude-mnemonic@claude-mnemonic"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

info() { echo -e "${BLUE}[INFO]${NC} $1"; }
success() { echo -e "${GREEN}[OK]${NC} $1"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }

# Parse arguments
KEEP_DATA=false
for arg in "$@"; do
    case $arg in
        --keep-data)
            KEEP_DATA=true
            ;;
        --purge)
            KEEP_DATA=false
            ;;
    esac
done

echo ""
echo "╔═══════════════════════════════════════════════════════════╗"
echo "║         Claude Mnemonic - Uninstallation Script           ║"
echo "╚═══════════════════════════════════════════════════════════╝"
echo ""

# Stop worker
info "Stopping worker processes..."
pkill -9 -f 'claude-mnemonic.*worker' 2>/dev/null || true
pkill -9 -f '\.claude/plugins/.*/worker' 2>/dev/null || true
lsof -ti :37777 | xargs kill -9 2>/dev/null || true
sleep 1
success "Worker processes stopped"

# Remove plugin directories
info "Removing plugin directories..."
if [[ -d "$INSTALL_DIR" ]]; then
    rm -rf "$INSTALL_DIR"
    success "Removed $INSTALL_DIR"
else
    info "Plugin directory not found (already removed)"
fi

if [[ -d "$CACHE_DIR" ]]; then
    rm -rf "$CACHE_DIR"
    success "Removed $CACHE_DIR"
fi

# Remove from Claude Code configuration (if jq is available)
if command -v jq &> /dev/null; then
    info "Cleaning up Claude Code configuration..."

    if [[ -f "$PLUGINS_FILE" ]]; then
        jq 'del(.plugins["'"$PLUGIN_KEY"'"])' "$PLUGINS_FILE" > "${PLUGINS_FILE}.tmp" && mv "${PLUGINS_FILE}.tmp" "$PLUGINS_FILE"
        success "Removed from installed_plugins.json"
    fi

    if [[ -f "$SETTINGS_FILE" ]]; then
        jq 'del(.enabledPlugins["'"$PLUGIN_KEY"'"])' "$SETTINGS_FILE" > "${SETTINGS_FILE}.tmp" && mv "${SETTINGS_FILE}.tmp" "$SETTINGS_FILE"
        success "Removed from settings.json"
    fi

    if [[ -f "$MARKETPLACES_FILE" ]]; then
        jq 'del(.["claude-mnemonic"])' "$MARKETPLACES_FILE" > "${MARKETPLACES_FILE}.tmp" && mv "${MARKETPLACES_FILE}.tmp" "$MARKETPLACES_FILE"
        success "Removed from known_marketplaces.json"
    fi
else
    warn "jq not found - Claude Code configuration files were not cleaned up"
    warn "You may need to manually remove claude-mnemonic entries from:"
    warn "  - $PLUGINS_FILE"
    warn "  - $SETTINGS_FILE"
    warn "  - $MARKETPLACES_FILE"
fi

# Handle data directory
if [[ -d "$DATA_DIR" ]]; then
    if [[ "$KEEP_DATA" == "true" ]]; then
        warn "Keeping data directory: $DATA_DIR"
        warn "To remove it later, run: rm -rf $DATA_DIR"
    else
        info "Removing data directory..."
        rm -rf "$DATA_DIR"
        success "Removed $DATA_DIR"
    fi
fi

echo ""
echo "╔═══════════════════════════════════════════════════════════╗"
echo "║              Uninstallation Complete!                     ║"
echo "╚═══════════════════════════════════════════════════════════╝"
echo ""

if [[ "$KEEP_DATA" == "true" ]]; then
    echo "  Data preserved at: $DATA_DIR"
    echo "  To reinstall: curl -sSL .../install.sh | bash"
    echo ""
fi

success "Claude Mnemonic has been uninstalled"
