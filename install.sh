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

# sha256_of prints the SHA-256 of a file using whichever tool is present
# (sha256sum on Linux, shasum on macOS), or nothing if neither exists.
sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

# verify_checksum checks $1 (a downloaded file) named $2 against the release's
# checksums.txt for tag $3, downloading it into $4. Mismatch → hard abort.
# Missing checksums.txt or no sha256 tool → warn and proceed.
verify_checksum() {
  local file="$1" asset="$2" tag="$3" tmp="$4"
  local sums_url="https://github.com/$REPO/releases/download/${tag}/checksums.txt"
  if ! curl -fsSL "$sums_url" -o "$tmp/checksums.txt" 2>/dev/null; then
    echo "⚠️  no checksums.txt for $tag — cannot verify integrity, proceeding."
    return 0
  fi
  local want got
  want="$(awk -v a="$asset" '$2==a {print $1}' "$tmp/checksums.txt" | head -1)"
  got="$(sha256_of "$file")"
  if [ -z "$want" ]; then
    echo "⚠️  no checksum entry for $asset — proceeding without verification."
    return 0
  fi
  if [ -z "$got" ]; then
    echo "⚠️  no sha256 tool (sha256sum/shasum) — proceeding without verification."
    return 0
  fi
  if [ "$want" != "$got" ]; then
    echo "❌ checksum MISMATCH for $asset — aborting (possible tampering or corrupt download)."
    echo "   expected: $want"
    echo "   actual:   $got"
    rm -rf "$tmp"
    exit 1
  fi
  echo "🔒 checksum verified ($asset)"
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
  local asset="lumen_${ver}_${os}_${arch}.tar.gz"
  local url="https://github.com/$REPO/releases/download/${tag}/${asset}"
  echo "⬇️  Downloading $url"
  local tmp; tmp="$(mktemp -d)"
  curl -fsSL "$url" -o "$tmp/lumen.tar.gz" 2>/dev/null || { rm -rf "$tmp"; return 1; }
  # Supply-chain integrity: verify the tarball against the release checksums.txt
  # BEFORE extracting/installing. A mismatch aborts hard (possible tampering);
  # an absent checksums.txt warns but proceeds (older releases).
  verify_checksum "$tmp/lumen.tar.gz" "$asset" "$tag" "$tmp"
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
