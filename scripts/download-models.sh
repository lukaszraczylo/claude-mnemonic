#!/bin/bash
# Download ONNX models from Hugging Face for local development and CI.
# Usage: ./scripts/download-models.sh [--force]
#
# Downloads models to internal/*/assets/ for go:embed and to testdata/models/
# for Go tests (CLAUDE_MNEMONIC_MODEL_DIR points there).

set -e

ASSETS_EMB="internal/embedding/assets"
ASSETS_RERANK="internal/reranking/assets"
TESTDATA="testdata/models"
FORCE_DOWNLOAD=false

for arg in "$@"; do
	if [ "$arg" = "--force" ]; then
		FORCE_DOWNLOAD=true
	fi
done

download_if_needed() {
	local url="$1"
	local dest="$2"
	local name="$3"
	local expected_sha="$4"

	if [ "$FORCE_DOWNLOAD" = false ] && [ -f "$dest" ]; then
		local actual_sha
		actual_sha=$(shasum -a 256 "$dest" | awk '{print $1}')
		if [ "$actual_sha" = "$expected_sha" ]; then
			echo "[skip] $name"
			return
		fi
		echo "[mismatch] $name checksum mismatch, re-downloading"
	fi

	echo "[download] $name ($(basename "$url"))"
	curl -fsSL --retry 3 --retry-delay 2 "$url" -o "$dest"
	echo "[ok] $name"
}

echo "=== Downloading models from Hugging Face ==="

# BGE-small-en-v1.5 embedding model (~127 MB)
download_if_needed \
	"https://huggingface.co/BAAI/bge-small-en-v1.5/resolve/main/onnx/model.onnx" \
	"${ASSETS_EMB}/model.onnx" \
	"embedding (bge-small-en-v1.5)" \
	"828e1496d7fabb79cfa4dcd84fa38625c0d3d21da474a00f08db0f559940cf35"

# MS-MARCO MiniLM-L6-v2 cross-encoder model (~91 MB)
download_if_needed \
	"https://huggingface.co/cross-encoder/ms-marco-MiniLM-L6-v2/resolve/main/onnx/model.onnx" \
	"${ASSETS_RERANK}/model.onnx" \
	"cross-encoder (ms-marco-MiniLM-L6-v2)" \
	"5d3e70fd0c9ff14b9b5169a51e957b7a9c74897afd0a35ce4bd318150c1d4d4a"

echo ""
echo "=== Staging models for tests ==="
mkdir -p "${TESTDATA}"
cp "${ASSETS_EMB}/model.onnx" "${TESTDATA}/bge-small-en-v1.5-model.onnx"
cp "${ASSETS_RERANK}/model.onnx" "${TESTDATA}/ms-marco-MiniLM-L6-v2-model.onnx"
echo "[ok] Test models staged to ${TESTDATA}/"

echo "Done!"
