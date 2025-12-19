#!/bin/bash
# Download BGE-small-en-v1.5 model for embedding
# Usage: ./download-bge-model.sh [--force]
# Use --force to re-download even if files exist

set -e

MODEL_NAME="bge-small-en-v1.5"
MODEL_REPO="BAAI/bge-small-en-v1.5"
ASSETS_DIR="internal/embedding/assets"
VERSION_FILE="${ASSETS_DIR}/.model_version"
FORCE_DOWNLOAD=false

# Check for --force flag
for arg in "$@"; do
    if [ "$arg" = "--force" ]; then
        FORCE_DOWNLOAD=true
    fi
done

# Temporary directory for downloads
TEMP_DIR=$(mktemp -d)
trap "rm -rf ${TEMP_DIR}" EXIT

# Check if model already exists
model_exists() {
    [ -f "${ASSETS_DIR}/model.onnx" ] && [ -f "${ASSETS_DIR}/tokenizer.json" ]
}

# Get installed version
get_installed_version() {
    if [ -f "$VERSION_FILE" ]; then
        cat "$VERSION_FILE"
    else
        echo ""
    fi
}

# Write version file
write_version_file() {
    echo "${MODEL_NAME}" > "$VERSION_FILE"
}

download_model() {
    echo "Downloading ${MODEL_NAME} from Hugging Face..."

    # Create assets directory
    mkdir -p "${ASSETS_DIR}"

    # Download ONNX model
    # BGE models have ONNX exports available in the repo
    echo "Downloading ONNX model..."
    curl -fsSL \
        "https://huggingface.co/${MODEL_REPO}/resolve/main/onnx/model.onnx" \
        -o "${TEMP_DIR}/model.onnx"

    # Download tokenizer.json
    echo "Downloading tokenizer..."
    curl -fsSL \
        "https://huggingface.co/${MODEL_REPO}/resolve/main/tokenizer.json" \
        -o "${TEMP_DIR}/tokenizer.json"

    # Verify files exist and have content
    if [ ! -s "${TEMP_DIR}/model.onnx" ]; then
        echo "Error: Failed to download model.onnx or file is empty"
        exit 1
    fi

    if [ ! -s "${TEMP_DIR}/tokenizer.json" ]; then
        echo "Error: Failed to download tokenizer.json or file is empty"
        exit 1
    fi

    # Move to assets directory (backup old files first)
    if [ -f "${ASSETS_DIR}/model.onnx" ]; then
        mv "${ASSETS_DIR}/model.onnx" "${ASSETS_DIR}/model.onnx.bak"
    fi
    if [ -f "${ASSETS_DIR}/tokenizer.json" ]; then
        mv "${ASSETS_DIR}/tokenizer.json" "${ASSETS_DIR}/tokenizer.json.bak"
    fi

    mv "${TEMP_DIR}/model.onnx" "${ASSETS_DIR}/model.onnx"
    mv "${TEMP_DIR}/tokenizer.json" "${ASSETS_DIR}/tokenizer.json"

    # Remove backups on success
    rm -f "${ASSETS_DIR}/model.onnx.bak" "${ASSETS_DIR}/tokenizer.json.bak"

    # Write version file
    write_version_file

    echo "Model size: $(du -h "${ASSETS_DIR}/model.onnx" | cut -f1)"
    echo "Tokenizer size: $(du -h "${ASSETS_DIR}/tokenizer.json" | cut -f1)"
}

echo "BGE Model Downloader - ${MODEL_NAME}"
echo "=================================="

need_download=false
reason=""

if [ "$FORCE_DOWNLOAD" = true ]; then
    need_download=true
    reason="forced"
elif ! model_exists; then
    need_download=true
    reason="not found"
elif [ "$(get_installed_version)" != "${MODEL_NAME}" ]; then
    need_download=true
    reason="version mismatch (installed: $(get_installed_version), required: ${MODEL_NAME})"
fi

if [ "$need_download" = true ]; then
    if [ -n "$reason" ] && [ "$reason" != "not found" ]; then
        echo "Re-downloading: ${reason}"
    fi
    download_model
    echo "Done! ${MODEL_NAME} installed successfully."
else
    echo "Model ${MODEL_NAME} already exists, skipping download."
    echo "Use --force to re-download."
fi
