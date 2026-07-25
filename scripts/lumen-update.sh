#!/usr/bin/env bash
# lumen-update.sh — check for and install Lumen updates from GitHub Releases
#
# Usage:
#   ./scripts/lumen-update.sh              # check + install latest
#   ./scripts/lumen-update.sh --check      # dry-run: just check
#   ./scripts/lumen-update.sh --force VER  # install specific version

set -euo pipefail

REPO="exergyleizhou-ux/lumen"
API="https://api.github.com/repos/${REPO}/releases/latest"
INSTALL_DIR="$HOME/.lumen"
BIN_DIR="$INSTALL_DIR/bin"
BACKUP_DIR="$INSTALL_DIR/backups"

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

MODE="install"
FORCE_VER=""

while [[ $# -gt 0 ]]; do
    case "$1" in
        --check) MODE="check"; shift ;;
        --force) MODE="force"; FORCE_VER="$2"; shift 2 ;;
        *)       echo "Usage: $0 [--check|--force VER]"; exit 1 ;;
    esac
done

get_latest() {
    curl -fsSL "$API" 2>/dev/null | python3 -c "
import json,sys
data=json.load(sys.stdin)
print(data.get('tag_name','').lstrip('v'))
" 2>/dev/null || echo "0.0.0"
}

get_current() {
    if [[ -x "$BIN_DIR/lumen" ]]; then
        "$BIN_DIR/lumen" --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1 || echo "0.0.0"
    else
        echo "0.0.0"
    fi
}

LATEST=$(get_latest)
CURRENT=$(get_current)

echo ""
echo "  Current: v${CURRENT}"
echo "  Latest:  v${LATEST}"

if [[ "$CURRENT" == "$LATEST" ]] && [[ "$MODE" != "force" ]]; then
    echo -e "  ${GREEN}✓ Already up to date${NC}"
    exit 0
fi

install_version() {
    local ver="${1:-$LATEST}"
    local url="https://github.com/${REPO}/releases/download/v${ver}/lumen-macos-aarch64.tar.gz"
    local tmp="/tmp/lumen-update-$$.tar.gz"

    echo ""
    echo "  Downloading v${ver}..."
    if ! curl -fsSL -o "$tmp" "$url"; then
        echo -e "  ${RED}✗ Download failed${NC} (binary release may not exist for v${ver})"
        echo "  Build from source: cd ~/code/lumen/agent && cargo build --release"
        exit 1
    fi

    mkdir -p "$BACKUP_DIR"
    if [[ -f "$BIN_DIR/lumen" ]]; then
        cp "$BIN_DIR/lumen" "$BACKUP_DIR/lumen.v${CURRENT}" 2>/dev/null || true
        echo "  Backed up v${CURRENT} to $BACKUP_DIR/"
    fi

    tar xzf "$tmp" -C "$BIN_DIR/"
    rm -f "$tmp"
    chmod 755 "$BIN_DIR/lumen"

    echo -e "  ${GREEN}✓ Installed v${ver}${NC}"
    echo "  Restart Lumen to use the new version."
}

if [[ "$MODE" == "check" ]]; then
    echo ""
    echo -e "  ${YELLOW}Update available: v${CURRENT} → v${LATEST}${NC}"
    echo "  Run without --check to install: $0"
    exit 1
elif [[ "$MODE" == "force" ]]; then
    install_version "$FORCE_VER"
else
    install_version "$LATEST"
fi
