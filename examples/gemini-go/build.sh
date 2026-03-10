#!/usr/bin/env bash

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"

OUTPUT_DIR="$SCRIPT_DIR/dist"
BINARY_NAME="gemini-go"

mkdir -p "$OUTPUT_DIR"

echo "🔥 Building for host platform..."
go build -ldflags="-s -w" -o "$OUTPUT_DIR/$BINARY_NAME" .

echo "✅ Build complete:"
ls -lh "$OUTPUT_DIR/"

echo ""
echo "Usage: GEMINI_API_KEY=your-key ./$OUTPUT_DIR/$BINARY_NAME"
