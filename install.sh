#!/usr/bin/env bash
set -euo pipefail

# Lumen installer. Prefers a prebuilt release binary (no Go toolchain needed);
# falls back to building from source if no matching release asset is published.

LUMEN_VERSION="${LUMEN_VERSION:-latest}"
INSTALL_DIR="${LUMEN_INSTALL_DIR:-$HOME/.lumen}"
BIN_DIR="$INSTALL_DIR/bin"
REPO="exergyleizhou-ux/lumen"

echo "🪄 Lumen Installer"
echo "=================="
mkdir -p "$BIN_DIR"

# Map uname → goreleaser asset naming (darwin/linux × amd64/arm64).
os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
esac

build_from_source() {
  echo "🔧 Building from source (requires Go 1.23+ and git)…"
  command -v go  >/dev/null 2>&1 || { echo "❌ Go 1.23+ required: https://go.dev/dl/"; exit 1; }
  command -v git >/dev/null 2>&1 || { echo "❌ git required."; exit 1; }
  tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
  git clone --depth 1 "https://github.com/$REPO.git" "$tmp"
  ( cd "$tmp" && GOTOOLCHAIN=local go build -o "$BIN_DIR/lumen" ./cmd/lumen )
}

install_binary() {
  # Resolve the release tag (latest or pinned) and download the tarball.
  local tag="$LUMEN_VERSION"
  if [ "$tag" = "latest" ]; then
    tag="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" 2>/dev/null \
      | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1)"
  fi
  [ -n "$tag" ] || return 1
  local ver="${tag#v}"
  local url="https://github.com/$REPO/releases/download/${tag}/lumen_${ver}_${os}_${arch}.tar.gz"
  echo "⬇️  Downloading $url"
  local tmp; tmp="$(mktemp -d)"
  curl -fsSL "$url" -o "$tmp/lumen.tar.gz" 2>/dev/null || { rm -rf "$tmp"; return 1; }
  tar -xzf "$tmp/lumen.tar.gz" -C "$tmp" 2>/dev/null || { rm -rf "$tmp"; return 1; }
  [ -f "$tmp/lumen" ] || { rm -rf "$tmp"; return 1; }
  install -m 0755 "$tmp/lumen" "$BIN_DIR/lumen"
  rm -rf "$tmp"
}

if install_binary; then
  echo "✅ Installed release binary."
else
  echo "ℹ️  No matching release asset (or download failed) — falling back to source."
  build_from_source
fi

# Add to PATH (idempotent).
profile=""
for f in "$HOME/.zshrc" "$HOME/.bashrc" "$HOME/.bash_profile"; do
  [ -f "$f" ] && { profile="$f"; break; }
done
if [ -n "$profile" ] && ! grep -q "$BIN_DIR" "$profile"; then
  echo "export PATH=\"$BIN_DIR:\$PATH\"" >> "$profile"
  echo "✅ Added $BIN_DIR to PATH in $profile (restart your shell or 'source $profile')"
fi

echo ""
echo "✅ Lumen installed: $BIN_DIR/lumen"
"$BIN_DIR/lumen" version || true
echo ""
echo "Quick start:"
echo "  export DEEPSEEK_API_KEY=sk-...   # or point base_url at a local model"
echo "  lumen doctor"
echo "  lumen run \"explain this project\""
echo "  lumen chat"
