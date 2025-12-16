#!/bin/bash
# Claude Mnemonic - Remote Installation Script
# Usage: curl -sSL https://raw.githubusercontent.com/lukaszraczylo/claude-mnemonic/main/scripts/install.sh | bash
#
# Or with a specific version:
# curl -sSL https://raw.githubusercontent.com/lukaszraczylo/claude-mnemonic/main/scripts/install.sh | bash -s -- v1.0.0

set -e

# Configuration
GITHUB_REPO="lukaszraczylo/claude-mnemonic"
INSTALL_DIR="$HOME/.claude/plugins/marketplaces/claude-mnemonic"
CACHE_DIR="$HOME/.claude/plugins/cache/claude-mnemonic/claude-mnemonic"
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

info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

success() {
    echo -e "${GREEN}[OK]${NC} $1"
}

warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

error() {
    echo -e "${RED}[ERROR]${NC} $1"
    exit 1
}

# Detect OS and architecture
detect_platform() {
    local os arch

    case "$(uname -s)" in
        Darwin)
            os="darwin"
            ;;
        Linux)
            os="linux"
            ;;
        MINGW*|MSYS*|CYGWIN*)
            os="windows"
            ;;
        *)
            error "Unsupported operating system: $(uname -s)"
            ;;
    esac

    case "$(uname -m)" in
        x86_64|amd64)
            arch="amd64"
            ;;
        arm64|aarch64)
            arch="arm64"
            ;;
        *)
            error "Unsupported architecture: $(uname -m)"
            ;;
    esac

    # Check for unsupported combinations
    if [[ "$os" == "linux" && "$arch" == "arm64" ]]; then
        error "Linux ARM64 is not currently supported due to CGO cross-compilation limitations"
    fi

    echo "${os}_${arch}"
}

# Get the latest release version from GitHub
get_latest_version() {
    local response version curl_opts

    # Use GitHub token if available (higher rate limit)
    curl_opts=(-sS)
    if [[ -n "${GITHUB_TOKEN:-}" ]]; then
        curl_opts+=(-H "Authorization: token ${GITHUB_TOKEN}")
    fi

    # Fetch with error handling
    response=$(curl "${curl_opts[@]}" "https://api.github.com/repos/${GITHUB_REPO}/releases/latest" 2>&1)

    # Check for rate limiting
    if echo "$response" | grep -q "API rate limit exceeded"; then
        echo ""
        error "GitHub API rate limit exceeded.

You have a few options:
  1. Wait ~1 hour for the rate limit to reset
  2. Specify a version manually:
     curl -sSL https://raw.githubusercontent.com/${GITHUB_REPO}/main/scripts/install.sh | bash -s -- v0.6.1
  3. Use a GitHub token (set GITHUB_TOKEN environment variable)
  4. Clone and build from source:
     git clone https://github.com/${GITHUB_REPO}.git
     cd claude-mnemonic && make build && make install"
    fi

    # Check for other API errors
    if echo "$response" | grep -q '"message":'; then
        local msg
        msg=$(echo "$response" | grep '"message":' | sed -E 's/.*"message": *"([^"]+)".*/\1/')
        error "GitHub API error: $msg"
    fi

    # Extract version
    version=$(echo "$response" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')

    if [[ -z "$version" ]]; then
        error "Failed to fetch latest version from GitHub. Response: $response"
    fi

    echo "$version"
}

# Download and extract the release
download_release() {
    local version="$1"
    local platform="$2"
    local tmp_dir

    tmp_dir=$(mktemp -d)
    trap "rm -rf $tmp_dir" EXIT

    # Construct download URL (use .zip for Windows, .tar.gz for others)
    local archive_ext="tar.gz"
    if [[ "$platform" == windows_* ]]; then
        archive_ext="zip"
    fi
    local archive_name="claude-mnemonic_${version#v}_${platform}.${archive_ext}"
    local download_url="https://github.com/${GITHUB_REPO}/releases/download/${version}/${archive_name}"

    info "Downloading ${archive_name}..."

    if ! curl -sSL -o "$tmp_dir/release.${archive_ext}" "$download_url"; then
        error "Failed to download release from: $download_url"
    fi

    info "Extracting archive..."
    if [[ "$archive_ext" == "zip" ]]; then
        if ! unzip -q "$tmp_dir/release.zip" -d "$tmp_dir"; then
            error "Failed to extract archive"
        fi
    else
        if ! tar -xzf "$tmp_dir/release.tar.gz" -C "$tmp_dir"; then
            error "Failed to extract archive"
        fi
    fi

    # Stop existing worker if running
    info "Stopping existing worker (if running)..."
    pkill -9 -f 'claude-mnemonic.*worker' 2>/dev/null || true
    pkill -9 -f '\.claude/plugins/.*/worker' 2>/dev/null || true
    # Kill process on port 37777 (use lsof on macOS, ss/fuser on Linux)
    if command -v lsof &> /dev/null; then
        lsof -ti :37777 | xargs kill -9 2>/dev/null || true
    elif command -v ss &> /dev/null; then
        ss -tlnp 'sport = :37777' 2>/dev/null | awk 'NR>1 {print $6}' | grep -oP 'pid=\K[0-9]+' | xargs -r kill -9 2>/dev/null || true
    elif command -v fuser &> /dev/null; then
        fuser -k 37777/tcp 2>/dev/null || true
    fi
    sleep 1

    # Create installation directories
    info "Installing to ${INSTALL_DIR}..."
    mkdir -p "$INSTALL_DIR/hooks"
    mkdir -p "$INSTALL_DIR/.claude-plugin"

    # Copy binaries
    cp "$tmp_dir/worker" "$INSTALL_DIR/"
    cp "$tmp_dir/mcp-server" "$INSTALL_DIR/"
    cp "$tmp_dir/hooks/"* "$INSTALL_DIR/hooks/"

    # Copy plugin configuration
    cp "$tmp_dir/.claude-plugin/"* "$INSTALL_DIR/.claude-plugin/"

    # Make binaries executable
    chmod +x "$INSTALL_DIR/worker"
    chmod +x "$INSTALL_DIR/mcp-server"
    chmod +x "$INSTALL_DIR/hooks/"*

    success "Binaries installed to ${INSTALL_DIR}"
}

# Register the plugin with Claude Code
register_plugin() {
    local version="$1"
    local timestamp
    timestamp=$(date -u +"%Y-%m-%dT%H:%M:%S.000Z")

    # Ensure directories exist
    mkdir -p "$HOME/.claude/plugins"

    # Clean up old cache versions to prevent stale binaries
    local cache_base
    cache_base=$(dirname "$CACHE_DIR")
    if [[ -d "$cache_base" ]]; then
        info "Cleaning up old cache versions..."
        find "$cache_base" -mindepth 1 -maxdepth 1 -type d ! -name "${version#v}" -exec rm -rf {} \; 2>/dev/null || true
    fi

    mkdir -p "${CACHE_DIR}/${version}"

    # Create JSON files if they don't exist
    [[ ! -f "$PLUGINS_FILE" ]] && echo '{"version": 2, "plugins": {}}' > "$PLUGINS_FILE"
    [[ ! -f "$SETTINGS_FILE" ]] && echo '{}' > "$SETTINGS_FILE"
    [[ ! -f "$MARKETPLACES_FILE" ]] && echo '{}' > "$MARKETPLACES_FILE"

    # Check for jq
    if ! command -v jq &> /dev/null; then
        warn "jq is not installed. Plugin registration requires jq."
        warn "Please install jq: brew install jq (macOS) or apt-get install jq (Linux)"
        warn "Then run: $0 --register-only"
        return 1
    fi

    local cache_path="${CACHE_DIR}/${version}"

    # Copy files to cache directory
    mkdir -p "$cache_path/.claude-plugin"
    mkdir -p "$cache_path/hooks"
    cp -r "$INSTALL_DIR/"* "$cache_path/" 2>/dev/null || true

    # Register in installed_plugins.json
    local plugin_entry
    plugin_entry=$(cat <<EOF
[{
    "scope": "user",
    "installPath": "$cache_path",
    "version": "${version#v}",
    "installedAt": "$timestamp",
    "lastUpdated": "$timestamp",
    "isLocal": true
}]
EOF
)

    jq --arg key "$PLUGIN_KEY" --argjson entry "$plugin_entry" \
        '.plugins[$key] = $entry' "$PLUGINS_FILE" > "${PLUGINS_FILE}.tmp" \
        && mv "${PLUGINS_FILE}.tmp" "$PLUGINS_FILE"

    success "Plugin registered in installed_plugins.json"

    # Enable in settings.json and configure statusline
    local statusline_cmd="$INSTALL_DIR/hooks/statusline"
    local statusline_entry
    statusline_entry=$(cat <<EOF
{
    "type": "command",
    "command": "$statusline_cmd",
    "padding": 0
}
EOF
)

    jq --arg key "$PLUGIN_KEY" --argjson statusline "$statusline_entry" \
        '.enabledPlugins //= {} | .enabledPlugins[$key] = true | .statusLine = $statusline' "$SETTINGS_FILE" > "${SETTINGS_FILE}.tmp" \
        && mv "${SETTINGS_FILE}.tmp" "$SETTINGS_FILE"

    success "Plugin enabled in settings.json"
    success "Statusline configured in settings.json"

    # Register marketplace
    local marketplace_entry
    marketplace_entry=$(cat <<EOF
{
    "source": {
        "source": "directory",
        "path": "$INSTALL_DIR"
    },
    "installLocation": "$INSTALL_DIR",
    "lastUpdated": "$timestamp"
}
EOF
)

    jq --arg key "claude-mnemonic" --argjson entry "$marketplace_entry" \
        '.[$key] = $entry' "$MARKETPLACES_FILE" > "${MARKETPLACES_FILE}.tmp" \
        && mv "${MARKETPLACES_FILE}.tmp" "$MARKETPLACES_FILE"

    success "Marketplace registered in known_marketplaces.json"
}

# Start the worker service
start_worker() {
    local worker_path="$INSTALL_DIR/worker"

    if [[ ! -x "$worker_path" ]]; then
        error "Worker binary not found at $worker_path"
    fi

    info "Starting worker service..."
    nohup "$worker_path" > /tmp/claude-mnemonic-worker.log 2>&1 &

    sleep 2

    if curl -sS http://localhost:37777/health > /dev/null 2>&1; then
        success "Worker started successfully at http://localhost:37777"
    else
        warn "Worker may not have started properly. Check /tmp/claude-mnemonic-worker.log"
    fi
}

# Check optional dependencies for semantic search
check_optional_deps() {
    local missing_deps=()
    local install_hints=""

    # Check for Python 3.13+
    if command -v python3 &> /dev/null; then
        local py_version=$(python3 -c 'import sys; print(f"{sys.version_info.major}.{sys.version_info.minor}")' 2>/dev/null)
        if [[ "$py_version" < "3.13" ]]; then
            missing_deps+=("Python 3.13+ (found $py_version)")
        fi
    else
        missing_deps+=("Python 3.13+")
    fi

    # Check for uvx
    if ! command -v uvx &> /dev/null; then
        missing_deps+=("uvx")
    fi

    if [[ ${#missing_deps[@]} -gt 0 ]]; then
        echo ""
        warn "Optional dependencies missing (needed for semantic search):"
        for dep in "${missing_deps[@]}"; do
            echo "  - $dep"
        done
        echo ""

        # Detect OS and show appropriate install command
        case "$(uname -s)" in
            Darwin)
                info "Install on macOS:"
                echo "  brew install python@3.13"
                echo "  pip3 install uv"
                ;;
            Linux)
                info "Install on Linux:"
                echo "  sudo apt install python3 python3-pip"
                echo "  pip3 install uv"
                ;;
            MINGW*|MSYS*|CYGWIN*)
                info "Install on Windows:"
                echo "  winget install Python.Python.3.13"
                echo "  pip install uv"
                ;;
        esac
        echo ""
        info "Note: Requires Python 3.13+. Most package managers install the latest version."
        echo ""
        info "Semantic search will be disabled until these are installed."
        info "Core functionality (SQLite storage, full-text search) will work."
        echo ""
    else
        success "Optional dependencies found (semantic search enabled)"
    fi
}

# Main installation flow
main() {
    local version="${1:-}"

    echo ""
    echo "╔═══════════════════════════════════════════════════════════╗"
    echo "║           Claude Mnemonic - Installation Script           ║"
    echo "║     Persistent Memory System for Claude Code CLI          ║"
    echo "╚═══════════════════════════════════════════════════════════╝"
    echo ""

    # Check required dependencies
    if ! command -v curl &> /dev/null; then
        error "curl is required but not installed"
    fi

    if ! command -v tar &> /dev/null; then
        error "tar is required but not installed"
    fi

    # Detect platform
    local platform
    platform=$(detect_platform)
    info "Detected platform: $platform"

    # Get version
    if [[ -z "$version" ]]; then
        info "Fetching latest release..."
        version=$(get_latest_version)
    fi
    info "Installing version: $version"

    # Download and install
    download_release "$version" "$platform"

    # Register plugin
    if register_plugin "$version"; then
        success "Plugin registered successfully"
    else
        warn "Plugin registration incomplete - please install jq and run again"
    fi

    # Start worker
    start_worker

    # Check optional dependencies
    check_optional_deps

    echo ""
    echo "╔═══════════════════════════════════════════════════════════╗"
    echo "║                  Installation Complete!                   ║"
    echo "╠═══════════════════════════════════════════════════════════╣"
    echo "║  Dashboard: http://localhost:37777                        ║"
    echo "║  Logs: /tmp/claude-mnemonic-worker.log                    ║"
    echo "║                                                           ║"
    echo "║  Start a new Claude Code CLI session to activate memory.  ║"
    echo "╚═══════════════════════════════════════════════════════════╝"
    echo ""
}

# Handle --register-only flag
if [[ "${1:-}" == "--register-only" ]]; then
    version=$(cat "$INSTALL_DIR/.claude-plugin/plugin.json" 2>/dev/null | grep '"version"' | sed -E 's/.*"([^"]+)".*/\1/' || echo "1.0.0")
    register_plugin "v$version"
    exit 0
fi

# Handle --uninstall flag
if [[ "${1:-}" == "--uninstall" ]]; then
    KEEP_DATA=false
    [[ "${2:-}" == "--keep-data" ]] && KEEP_DATA=true

    echo ""
    echo "╔═══════════════════════════════════════════════════════════╗"
    echo "║         Claude Mnemonic - Uninstallation                  ║"
    echo "╚═══════════════════════════════════════════════════════════╝"
    echo ""

    info "Stopping worker processes..."
    pkill -9 -f 'claude-mnemonic.*worker' 2>/dev/null || true
    pkill -9 -f '\.claude/plugins/.*/worker' 2>/dev/null || true
    # Kill process on port 37777 (use lsof on macOS, ss/fuser on Linux)
    if command -v lsof &> /dev/null; then
        lsof -ti :37777 | xargs kill -9 2>/dev/null || true
    elif command -v ss &> /dev/null; then
        ss -tlnp 'sport = :37777' 2>/dev/null | awk 'NR>1 {print $6}' | grep -oP 'pid=\K[0-9]+' | xargs -r kill -9 2>/dev/null || true
    elif command -v fuser &> /dev/null; then
        fuser -k 37777/tcp 2>/dev/null || true
    fi
    sleep 1

    info "Removing plugin directories..."
    rm -rf "$INSTALL_DIR"
    rm -rf "$CACHE_DIR"
    success "Plugin directories removed"

    # Remove from JSON files (if jq is available)
    if command -v jq &> /dev/null; then
        info "Cleaning up Claude Code configuration..."
        if [[ -f "$PLUGINS_FILE" ]]; then
            jq 'del(.plugins["'"$PLUGIN_KEY"'"])' "$PLUGINS_FILE" > "${PLUGINS_FILE}.tmp" && mv "${PLUGINS_FILE}.tmp" "$PLUGINS_FILE"
        fi
        if [[ -f "$SETTINGS_FILE" ]]; then
            # Remove plugin from enabled plugins and remove statusline if it's ours
            jq 'del(.enabledPlugins["'"$PLUGIN_KEY"'"]) | if .statusLine.command | test("claude-mnemonic") then del(.statusLine) else . end' "$SETTINGS_FILE" > "${SETTINGS_FILE}.tmp" && mv "${SETTINGS_FILE}.tmp" "$SETTINGS_FILE"
        fi
        if [[ -f "$MARKETPLACES_FILE" ]]; then
            jq 'del(.["claude-mnemonic"])' "$MARKETPLACES_FILE" > "${MARKETPLACES_FILE}.tmp" && mv "${MARKETPLACES_FILE}.tmp" "$MARKETPLACES_FILE"
        fi
        success "Configuration cleaned up"
    else
        warn "jq not found - configuration files not cleaned up"
    fi

    # Handle data directory
    DATA_DIR="$HOME/.claude-mnemonic"
    if [[ -d "$DATA_DIR" ]]; then
        if [[ "$KEEP_DATA" == "true" ]]; then
            warn "Keeping data directory: $DATA_DIR"
        else
            info "Removing data directory..."
            rm -rf "$DATA_DIR"
            success "Data directory removed"
        fi
    fi

    echo ""
    success "Claude Mnemonic uninstalled successfully"
    exit 0
fi

main "$@"
