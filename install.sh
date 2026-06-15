#!/usr/bin/env bash
set -euo pipefail

# Lumen Install Script
# One-command install for Lumen agent platform.

LUMEN_VERSION="${LUMEN_VERSION:-latest}"
INSTALL_DIR="${LUMEN_INSTALL_DIR:-$HOME/.lumen}"
BIN_DIR="$INSTALL_DIR/bin"
REPO="exergyleizhou-ux/lumen"

echo "🪄 Lumen Installer"
echo "=================="
echo "Version: $LUMEN_VERSION"
echo "Install dir: $INSTALL_DIR"
echo ""

# Check prerequisites
command -v go >/dev/null 2>&1 || { echo "❌ Go 1.22+ is required. Install from https://go.dev/dl/"; exit 1; }
command -v git >/dev/null 2>&1 || { echo "❌ Git is required."; exit 1; }

# Create directories
mkdir -p "$BIN_DIR"

# Build from source
echo "📦 Building Lumen..."
TMPDIR=$(mktemp -d)
trap 'rm -rf $TMPDIR' EXIT

cd "$TMPDIR"
git clone --depth 1 "https://github.com/$REPO.git" .
GOTOOLCHAIN=local go build -o "$BIN_DIR/lumen" ./cmd/lumen

# Add to PATH
SHELL_PROFILE=""
if [ -f "$HOME/.zshrc" ]; then SHELL_PROFILE="$HOME/.zshrc"
elif [ -f "$HOME/.bashrc" ]; then SHELL_PROFILE="$HOME/.bashrc"
elif [ -f "$HOME/.bash_profile" ]; then SHELL_PROFILE="$HOME/.bash_profile"
fi

if [ -n "$SHELL_PROFILE" ]; then
    if ! grep -q "$BIN_DIR" "$SHELL_PROFILE"; then
        echo "export PATH=\"$BIN_DIR:\$PATH\"" >> "$SHELL_PROFILE"
        echo "✅ Added $BIN_DIR to PATH in $SHELL_PROFILE"
    fi
fi

echo ""
echo "✅ Lumen installed to $BIN_DIR/lumen"
echo ""
echo "Quick start:"
echo "  export OPENAI_API_KEY=sk-..."
echo "  lumen repl"
echo "  lumen serve"
echo "  lumen --help"
