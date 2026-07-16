#!/usr/bin/env bash
# Install release `lumen` to ~/.local/bin for PATH use.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export PATH="/opt/homebrew/bin:$HOME/.cargo/bin:$HOME/.local/bin:$PATH"
export PROTOC="${PROTOC:-/opt/homebrew/bin/protoc}"

BIN_SRC="$ROOT/agent/target/release/lumen"
DEST_DIR="${LUMEN_INSTALL_DIR:-$HOME/.local/bin}"
DEST="$DEST_DIR/lumen"

if [[ ! -x "$BIN_SRC" ]]; then
  echo "Building release lumen..."
  (cd "$ROOT/agent" && CARGO_BUILD_JOBS="${CARGO_BUILD_JOBS:-2}" cargo build -p xai-grok-pager-bin --release)
fi
test -x "$BIN_SRC"

mkdir -p "$DEST_DIR"
cp -f "$BIN_SRC" "$DEST"
chmod +x "$DEST"

echo "Installed: $DEST"
"$DEST" --version
echo ""
echo "Ensure PATH includes: $DEST_DIR"
echo "Set:  export DEEPSEEK_API_KEY=..."
echo "Then: lumen"
echo ""
echo "Productivity diary template: journal/TEMPLATE-productivity-day.md"
