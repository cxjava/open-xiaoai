#!/usr/bin/env bash

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"

OUTPUT_DIR="$SCRIPT_DIR/dist"
BINARY_NAME="open-xiaoai-client"

mkdir -p "$OUTPUT_DIR"

echo "🔥 Building for host platform..."
go build -ldflags="-s -w" -o "$OUTPUT_DIR/$BINARY_NAME" ./cmd/client/

echo "🔥 Cross-compiling for ARM (小爱音箱 Pro)..."
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 \
    go build -ldflags="-s -w" -o "$OUTPUT_DIR/${BINARY_NAME}-arm7" ./cmd/client/

echo "✅ Build complete:"
ls -lh "$OUTPUT_DIR/"
