#!/usr/bin/env bash
set -euo pipefail

BINARY="tokenomics"
BUILD_DIR="./bin"

echo "Building ${BINARY}..."
mkdir -p "${BUILD_DIR}"
go build -o "${BUILD_DIR}/${BINARY}" .
echo "Built: ${BUILD_DIR}/${BINARY}"
