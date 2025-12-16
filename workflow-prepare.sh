#!/bin/bash
# Workflow prepare script for CI
# Called by shared GitHub Actions workflow before build/test steps

set -e

# Download ONNX runtime libraries for current platform
./scripts/download-onnx-libs.sh auto
