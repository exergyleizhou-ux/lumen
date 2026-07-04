#!/usr/bin/env bash
# Attach science CLI + desktop artifacts to an existing GitHub prerelease tag.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"
TAG="${1:-v0.4.0-science-beta.2}"
DIST="$ROOT/dist/science-release"
mkdir -p "$DIST"

echo "▸ building lumen CLI (darwin arm64)"
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o "$DIST/lumen-darwin-arm64" ./cmd/lumen

for mcp in lumen-mcp-pubmed lumen-mcp-oasis lumen-mcp-chembl lumen-mcp-c2d lumen-mcp-geo; do
  echo "▸ $mcp"
  CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o "$DIST/$mcp-darwin-arm64" "./cmd/$mcp"
done

if [[ "$(uname -s)" == "Darwin" ]]; then
  APP="$ROOT/desktop/lumen-science/src-tauri/target/release/bundle/macos/Lumen Science.app"
  DMG="$ROOT/desktop/lumen-science/src-tauri/target/release/bundle/dmg/"*.dmg
  if [[ -d "$APP" ]]; then
    echo "▸ zipping .app"
    (cd "$(dirname "$APP")" && zip -qr "$DIST/Lumen-Science-macos.zip" "$(basename "$APP")")
  fi
  if ls $DMG 1>/dev/null 2>&1; then
    cp $DMG "$DIST/" 2>/dev/null || true
  fi
fi

echo "▸ uploading to $TAG"
gh release upload "$TAG" "$DIST"/* --clobber

echo "✓ publish-science-release done"