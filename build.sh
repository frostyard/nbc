#!/bin/bash
# Build script for nbc

set -e

VERSION=${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo "dev")}
LDFLAGS="-X main.version=$VERSION -s -w"

echo "Building nbc version $VERSION..."

# Build for current platform
go build -ldflags "$LDFLAGS" -o nbc .

echo "Build complete: ./nbc"
echo "Run './nbc --help' to get started"
