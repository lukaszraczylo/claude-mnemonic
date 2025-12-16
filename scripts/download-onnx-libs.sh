#!/bin/bash
# Download ONNX Runtime libraries for embedding
# Usage: ./download-onnx-libs.sh [platform]
# Platform: darwin-amd64, darwin-arm64, linux-amd64, linux-arm64, or "all" (default)

set -e

ONNX_VERSION="1.19.2"
ASSETS_DIR="internal/embedding/assets/lib"
PLATFORM="${1:-all}"

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

download_darwin_amd64() {
    echo "Downloading darwin-amd64..."
    mkdir -p "${ASSETS_DIR}/darwin-amd64"
    curl -sSL "https://github.com/microsoft/onnxruntime/releases/download/v${ONNX_VERSION}/onnxruntime-osx-x86_64-${ONNX_VERSION}.tgz" -o "${TEMP_DIR}/darwin-amd64.tgz"
    tar -xzf "${TEMP_DIR}/darwin-amd64.tgz" -C "${TEMP_DIR}"
    cp "${TEMP_DIR}/onnxruntime-osx-x86_64-${ONNX_VERSION}/lib/libonnxruntime.${ONNX_VERSION}.dylib" "${ASSETS_DIR}/darwin-amd64/libonnxruntime.dylib"
}

download_darwin_arm64() {
    echo "Downloading darwin-arm64..."
    mkdir -p "${ASSETS_DIR}/darwin-arm64"
    curl -sSL "https://github.com/microsoft/onnxruntime/releases/download/v${ONNX_VERSION}/onnxruntime-osx-arm64-${ONNX_VERSION}.tgz" -o "${TEMP_DIR}/darwin-arm64.tgz"
    tar -xzf "${TEMP_DIR}/darwin-arm64.tgz" -C "${TEMP_DIR}"
    cp "${TEMP_DIR}/onnxruntime-osx-arm64-${ONNX_VERSION}/lib/libonnxruntime.${ONNX_VERSION}.dylib" "${ASSETS_DIR}/darwin-arm64/libonnxruntime.dylib"
}

download_linux_amd64() {
    echo "Downloading linux-amd64..."
    mkdir -p "${ASSETS_DIR}/linux-amd64"
    curl -sSL "https://github.com/microsoft/onnxruntime/releases/download/v${ONNX_VERSION}/onnxruntime-linux-x64-${ONNX_VERSION}.tgz" -o "${TEMP_DIR}/linux-amd64.tgz"
    tar -xzf "${TEMP_DIR}/linux-amd64.tgz" -C "${TEMP_DIR}"
    cp "${TEMP_DIR}/onnxruntime-linux-x64-${ONNX_VERSION}/lib/libonnxruntime.so.${ONNX_VERSION}" "${ASSETS_DIR}/linux-amd64/libonnxruntime.so"
    cp "${TEMP_DIR}/onnxruntime-linux-x64-${ONNX_VERSION}/lib/libonnxruntime_providers_shared.so" "${ASSETS_DIR}/linux-amd64/libonnxruntime_providers_shared.so" 2>/dev/null || true
}

download_linux_arm64() {
    echo "Downloading linux-arm64..."
    mkdir -p "${ASSETS_DIR}/linux-arm64"
    curl -sSL "https://github.com/microsoft/onnxruntime/releases/download/v${ONNX_VERSION}/onnxruntime-linux-aarch64-${ONNX_VERSION}.tgz" -o "${TEMP_DIR}/linux-arm64.tgz"
    tar -xzf "${TEMP_DIR}/linux-arm64.tgz" -C "${TEMP_DIR}"
    cp "${TEMP_DIR}/onnxruntime-linux-aarch64-${ONNX_VERSION}/lib/libonnxruntime.so.${ONNX_VERSION}" "${ASSETS_DIR}/linux-arm64/libonnxruntime.so"
    cp "${TEMP_DIR}/onnxruntime-linux-aarch64-${ONNX_VERSION}/lib/libonnxruntime_providers_shared.so" "${ASSETS_DIR}/linux-arm64/libonnxruntime_providers_shared.so" 2>/dev/null || true
}

download_windows_amd64() {
    echo "Downloading windows-amd64..."
    mkdir -p "${ASSETS_DIR}/windows-amd64"
    curl -sSL "https://github.com/microsoft/onnxruntime/releases/download/v${ONNX_VERSION}/onnxruntime-win-x64-${ONNX_VERSION}.zip" -o "${TEMP_DIR}/windows-amd64.zip"
    unzip -q "${TEMP_DIR}/windows-amd64.zip" -d "${TEMP_DIR}"
    cp "${TEMP_DIR}/onnxruntime-win-x64-${ONNX_VERSION}/lib/onnxruntime.dll" "${ASSETS_DIR}/windows-amd64/onnxruntime.dll"
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

# Download only if not present
download_if_needed() {
    local plat="$1"
    if lib_exists "$plat"; then
        echo "Library for ${plat} already exists, skipping download"
        return 0
    fi
    case "$plat" in
        darwin-amd64) download_darwin_amd64 ;;
        darwin-arm64) download_darwin_arm64 ;;
        linux-amd64) download_linux_amd64 ;;
        linux-arm64) download_linux_arm64 ;;
        windows-amd64) download_windows_amd64 ;;
    esac
}

echo "ONNX Runtime v${ONNX_VERSION} - Platform: ${PLATFORM}"

case "$PLATFORM" in
    darwin-amd64|darwin-arm64|linux-amd64|linux-arm64|windows-amd64)
        download_if_needed "$PLATFORM"
        ;;
    all)
        download_if_needed darwin-amd64
        download_if_needed darwin-arm64
        download_if_needed linux-amd64
        download_if_needed linux-arm64
        download_if_needed windows-amd64
        ;;
    *)
        echo "Unknown platform: $PLATFORM"
        echo "Supported: darwin-amd64, darwin-arm64, linux-amd64, linux-arm64, windows-amd64, all, auto"
        exit 1
        ;;
esac

echo "Done!"
