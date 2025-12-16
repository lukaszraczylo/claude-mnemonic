#!/bin/bash
# Download ONNX Runtime libraries for embedding
# Usage: ./download-onnx-libs.sh [platform] [--force]
# Platform: darwin-arm64, linux-amd64, windows-amd64, or "all" (default)
# Use --force to re-download even if libraries exist

set -e

ONNX_VERSION="1.23.2"
ASSETS_DIR="internal/embedding/assets/lib"
PLATFORM="${1:-all}"
FORCE_DOWNLOAD=false

# Check for --force flag
for arg in "$@"; do
    if [ "$arg" = "--force" ]; then
        FORCE_DOWNLOAD=true
    fi
done

# Auto-detect platform if not specified
if [ "$PLATFORM" = "auto" ]; then
    OS=$(uname -s | tr '[:upper:]' '[:lower:]')
    ARCH=$(uname -m)
    case "$OS" in
        darwin) OS="darwin" ;;
        linux) OS="linux" ;;
        mingw*|msys*|cygwin*) OS="windows" ;;
        *) echo "Unsupported OS: $OS"; exit 1 ;;
    esac
    case "$ARCH" in
        x86_64|amd64) ARCH="amd64" ;;
        arm64|aarch64) ARCH="arm64" ;;
        *) echo "Unsupported arch: $ARCH"; exit 1 ;;
    esac
    PLATFORM="${OS}-${ARCH}"
fi

# Temporary directory for downloads
TEMP_DIR=$(mktemp -d)
trap "rm -rf ${TEMP_DIR}" EXIT

# Get the installed version for a platform
get_installed_version() {
    local plat="$1"
    local version_file="${ASSETS_DIR}/${plat}/.version"
    if [ -f "$version_file" ]; then
        cat "$version_file"
    else
        echo ""
    fi
}

# Write version file after successful download
write_version_file() {
    local plat="$1"
    echo "${ONNX_VERSION}" > "${ASSETS_DIR}/${plat}/.version"
}

# Check if version matches
version_matches() {
    local plat="$1"
    local installed_version
    installed_version=$(get_installed_version "$plat")
    [ "$installed_version" = "$ONNX_VERSION" ]
}

download_darwin_arm64() {
    echo "Downloading darwin-arm64..."
    mkdir -p "${ASSETS_DIR}/darwin-arm64"
    curl -fsSL "https://github.com/microsoft/onnxruntime/releases/download/v${ONNX_VERSION}/onnxruntime-osx-arm64-${ONNX_VERSION}.tgz" -o "${TEMP_DIR}/darwin-arm64.tgz"
    tar -xzf "${TEMP_DIR}/darwin-arm64.tgz" -C "${TEMP_DIR}"
    cp "${TEMP_DIR}/onnxruntime-osx-arm64-${ONNX_VERSION}/lib/libonnxruntime.${ONNX_VERSION}.dylib" "${ASSETS_DIR}/darwin-arm64/libonnxruntime.dylib"
    write_version_file "darwin-arm64"
}

download_linux_amd64() {
    echo "Downloading linux-amd64..."
    mkdir -p "${ASSETS_DIR}/linux-amd64"
    curl -fsSL "https://github.com/microsoft/onnxruntime/releases/download/v${ONNX_VERSION}/onnxruntime-linux-x64-${ONNX_VERSION}.tgz" -o "${TEMP_DIR}/linux-amd64.tgz"
    tar -xzf "${TEMP_DIR}/linux-amd64.tgz" -C "${TEMP_DIR}"
    cp "${TEMP_DIR}/onnxruntime-linux-x64-${ONNX_VERSION}/lib/libonnxruntime.so.${ONNX_VERSION}" "${ASSETS_DIR}/linux-amd64/libonnxruntime.so"
    cp "${TEMP_DIR}/onnxruntime-linux-x64-${ONNX_VERSION}/lib/libonnxruntime_providers_shared.so" "${ASSETS_DIR}/linux-amd64/libonnxruntime_providers_shared.so" 2>/dev/null || true
    write_version_file "linux-amd64"
}

download_linux_arm64() {
    echo "Downloading linux-arm64..."
    mkdir -p "${ASSETS_DIR}/linux-arm64"
    curl -fsSL "https://github.com/microsoft/onnxruntime/releases/download/v${ONNX_VERSION}/onnxruntime-linux-aarch64-${ONNX_VERSION}.tgz" -o "${TEMP_DIR}/linux-arm64.tgz"
    tar -xzf "${TEMP_DIR}/linux-arm64.tgz" -C "${TEMP_DIR}"
    cp "${TEMP_DIR}/onnxruntime-linux-aarch64-${ONNX_VERSION}/lib/libonnxruntime.so.${ONNX_VERSION}" "${ASSETS_DIR}/linux-arm64/libonnxruntime.so"
    cp "${TEMP_DIR}/onnxruntime-linux-aarch64-${ONNX_VERSION}/lib/libonnxruntime_providers_shared.so" "${ASSETS_DIR}/linux-arm64/libonnxruntime_providers_shared.so" 2>/dev/null || true
    write_version_file "linux-arm64"
}

download_windows_amd64() {
    echo "Downloading windows-amd64..."
    mkdir -p "${ASSETS_DIR}/windows-amd64"
    local url="https://github.com/microsoft/onnxruntime/releases/download/v${ONNX_VERSION}/onnxruntime-win-x64-${ONNX_VERSION}.zip"
    echo "URL: $url"
    curl -fsSL --retry 3 "$url" -o "${TEMP_DIR}/windows-amd64.zip"
    echo "Downloaded file size: $(wc -c < "${TEMP_DIR}/windows-amd64.zip") bytes"
    unzip -q "${TEMP_DIR}/windows-amd64.zip" -d "${TEMP_DIR}"
    cp "${TEMP_DIR}/onnxruntime-win-x64-${ONNX_VERSION}/lib/onnxruntime.dll" "${ASSETS_DIR}/windows-amd64/onnxruntime.dll"
    write_version_file "windows-amd64"
}

# Check if library already exists for a platform
lib_exists() {
    local plat="$1"
    case "$plat" in
        darwin-*) [ -f "${ASSETS_DIR}/${plat}/libonnxruntime.dylib" ] ;;
        linux-*) [ -f "${ASSETS_DIR}/${plat}/libonnxruntime.so" ] ;;
        windows-*) [ -f "${ASSETS_DIR}/${plat}/onnxruntime.dll" ] ;;
        *) return 1 ;;
    esac
}

# Download only if not present or version mismatch
download_if_needed() {
    local plat="$1"
    local need_download=false
    local reason=""

    if [ "$FORCE_DOWNLOAD" = true ]; then
        need_download=true
        reason="forced"
    elif ! lib_exists "$plat"; then
        need_download=true
        reason="not found"
    elif ! version_matches "$plat"; then
        local installed_version
        installed_version=$(get_installed_version "$plat")
        need_download=true
        reason="version mismatch (installed: ${installed_version:-unknown}, required: ${ONNX_VERSION})"
    fi

    if [ "$need_download" = true ]; then
        if [ -n "$reason" ] && [ "$reason" != "not found" ]; then
            echo "Re-downloading ${plat}: ${reason}"
        fi
        # Remove old library before downloading
        rm -rf "${ASSETS_DIR}/${plat}"
        case "$plat" in
            darwin-arm64) download_darwin_arm64 ;;
            linux-amd64) download_linux_amd64 ;;
            linux-arm64) download_linux_arm64 ;;
            windows-amd64) download_windows_amd64 ;;
        esac
    else
        echo "Library for ${plat} already exists (v${ONNX_VERSION}), skipping download"
    fi
}

echo "ONNX Runtime v${ONNX_VERSION} - Platform: ${PLATFORM}"

case "$PLATFORM" in
    darwin-arm64|linux-amd64|linux-arm64|windows-amd64)
        download_if_needed "$PLATFORM"
        ;;
    all)
        download_if_needed darwin-arm64
        download_if_needed linux-amd64
        download_if_needed linux-arm64
        download_if_needed windows-amd64
        ;;
    *)
        echo "Unknown platform: $PLATFORM"
        echo "Supported: darwin-arm64, linux-amd64, linux-arm64, windows-amd64, all, auto"
        exit 1
        ;;
esac

echo "Done!"
