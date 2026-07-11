#!/usr/bin/env bash
# Build Lumen Lab Desktop (Tauri) for macOS.
# Prerequisite: Lab must be running at http://127.0.0.1:18992/ first.
# Usage: ./scripts/science/build-desktop-lab.sh
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$REPO_ROOT/desktop/lumen-lab"

echo "[build-desktop] npm install..."
npm ci 2>/dev/null || npm install

echo "[build-desktop] tauri build..."
npm run tauri build 2>&1 | tail -30

echo ""
echo "[build-desktop] artifacts:"
ls -lh src-tauri/target/release/bundle/macos/*.app 2>/dev/null || echo "  (no .app found)"
ls -lh src-tauri/target/release/bundle/dmg/*.dmg 2>/dev/null || echo "  (no .dmg — ad-hoc signing only)"
echo "[build-desktop] done"
