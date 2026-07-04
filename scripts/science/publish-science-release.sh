#!/usr/bin/env bash
# Build science CLI + MCP fleet + desktop artifacts; upload or dry-run with checksums.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"
TAG="${1:-v0.4.0-science-beta.2}"
DRY_RUN=0
if [[ "${1:-}" == "--dry-run" ]]; then
  DRY_RUN=1
  TAG="${2:-v0.4.0-science-beta.2}"
fi
DIST="$ROOT/dist/science-release"
MANIFEST="$DIST/MANIFEST.sha256"
mkdir -p "$DIST"

build_cli() {
  local goos="$1" goarch="$2"
  local out="$DIST/lumen-${goos}-${goarch}"
  echo "▸ lumen ${goos}/${goarch}"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -o "$out" ./cmd/lumen
}

build_cli darwin arm64
build_cli linux amd64

for mcp in lumen-mcp-pubmed lumen-mcp-oasis lumen-mcp-chembl lumen-mcp-c2d lumen-mcp-geo; do
  echo "▸ $mcp darwin/arm64"
  CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o "$DIST/$mcp-darwin-arm64" "./cmd/$mcp"
  echo "▸ $mcp linux/amd64"
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o "$DIST/$mcp-linux-amd64" "./cmd/$mcp"
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

echo "▸ checksums"
(
  cd "$DIST"
  shasum -a 256 lumen-* lumen-mcp-* Lumen-Science-macos.zip 2>/dev/null || true
) > "$MANIFEST"

echo "▸ release artifacts:"
ls -la "$DIST"

if [[ "$DRY_RUN" -eq 1 ]]; then
  echo "✓ publish-science-release dry-run done (no upload)"
  exit 0
fi

echo "▸ uploading to $TAG"
gh release upload "$TAG" "$DIST"/* --clobber
echo "✓ publish-science-release done"