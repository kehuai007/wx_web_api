#!/usr/bin/env bash
# Build all platform binaries into ./dist.
# Mirrors build.bat (which only builds the Windows .exe) but fans out
# to Windows (current host's GOOS), Linux, and macOS.

set -euo pipefail

# Resolve script directory so the script works regardless of cwd.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

DIST_DIR="$SCRIPT_DIR/dist"
mkdir -p "$DIST_DIR"

echo "Building wx_web_api for all platforms into $DIST_DIR ..."

# Windows: matches dist/wx_web_api.exe produced by build.bat.
GOOS=windows GOARCH=amd64 go build -ldflags "-s -w" -o "$DIST_DIR/wx_web_api.exe" .

# Linux: matches the existing dist/wx_web_api artifact.
GOOS=linux   GOARCH=amd64 go build -ldflags "-s -w" -o "$DIST_DIR/wx_web_api" .

# macOS: additional target requested by the user.
GOOS=darwin  GOARCH=amd64 go build -ldflags "-s -w" -o "$DIST_DIR/wx_web_api-darwin" .

echo "Build complete:"
ls -lh "$DIST_DIR"/wx_web_api "$DIST_DIR"/wx_web_api.exe "$DIST_DIR"/wx_web_api-darwin
