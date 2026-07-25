#!/usr/bin/env bash
# lumen-install.sh — one-click Lumen installer for macOS
#
# Checks prerequisites, builds from source, installs to ~/.lumen/bin,
# configures API key, and verifies the installation.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/exergyleizhou-ux/lumen/main/scripts/lumen-install.sh | bash
#   ./scripts/lumen-install.sh --check        # check prereqs only
#   ./scripts/lumen-install.sh --reinstall    # force reinstall
#   ./scripts/lumen-install.sh --key YOUR_KEY # set API key during install

set -euo pipefail

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BOLD='\033[1m'
NC='\033[0m'

REPO_URL="${LUMEN_REPO:-https://github.com/exergyleizhou-ux/lumen.git}"
INSTALL_DIR="$HOME/.lumen"
BIN_DIR="$INSTALL_DIR/bin"
SRC_DIR="$HOME/code/lumen"
API_KEY="${LUMEN_API_KEY:-}"
ACTION="install"

while [[ $# -gt 0 ]]; do
    case "$1" in
        --check)     ACTION="check"; shift ;;
        --reinstall) ACTION="reinstall"; shift ;;
        --key)       API_KEY="$2"; shift 2 ;;
        --help)      echo "Usage: $0 [--check|--reinstall] [--key KEY]"; exit 0 ;;
        *)           echo "Unknown flag: $1"; exit 1 ;;
    esac
done

log()  { echo -e "${GREEN}✓${NC} $1"; }
warn() { echo -e "${YELLOW}⚠${NC} $1"; }
err()  { echo -e "${RED}✗${NC} $1"; exit 1; }
info() { echo -e "  $1"; }

check_prereqs() {
    local missing=0
    for cmd in rustc cargo git make; do
        if command -v "$cmd" &>/dev/null; then
            info "$cmd: $(command -v $cmd)"
        else
            warn "$cmd: MISSING"
            missing=$((missing + 1))
        fi
    done
    if [[ $missing -gt 0 ]]; then
        err "Install missing prerequisites first:"
        echo "  curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh  # Rust"
        echo "  xcode-select --install  # Git + Make"
    fi
    log "All prerequisites satisfied"
}

install_lumen() {
    mkdir -p "$BIN_DIR"

    if [[ -d "$SRC_DIR" ]] && [[ "$ACTION" != "reinstall" ]]; then
        info "Source already at $SRC_DIR, pulling latest..."
        cd "$SRC_DIR" && git pull --ff-only origin main 2>/dev/null || true
    else
        info "Cloning $REPO_URL..."
        rm -rf "$SRC_DIR"
        git clone "$REPO_URL" "$SRC_DIR"
    fi

    info "Building Lumen (this may take 10-20 minutes on first build)..."
    cd "$SRC_DIR/agent"
    cargo build --release

    local binary="$SRC_DIR/agent/target/release/lumen"
    if [[ ! -f "$binary" ]]; then
        err "Build failed — binary not found at $binary"
    fi

    cp "$binary" "$BIN_DIR/lumen"
    chmod 755 "$BIN_DIR/lumen"

    # Add to PATH if not already
    if ! grep -q "$BIN_DIR" "$HOME/.zshrc" 2>/dev/null && ! grep -q "$BIN_DIR" "$HOME/.bash_profile" 2>/dev/null; then
        echo "export PATH=\"$BIN_DIR:\$PATH\"" >> "$HOME/.zshrc"
        info "Added $BIN_DIR to PATH in ~/.zshrc"
    fi

    log "Lumen installed to $BIN_DIR/lumen"
}

configure_key() {
    local key="${1:-}"
    if [[ -z "$key" ]]; then
        info "No API key provided. Set later with:"
        info "  export DEEPSEEK_API_KEY=your-key-here"
        info "  export KIMI_CODE_API_KEY=your-key-here"
        return
    fi

    # Auto-detect provider from key prefix
    local env_var="DEEPSEEK_API_KEY"
    if [[ "$key" == sk-* ]]; then
        env_var="KIMI_CODE_API_KEY"
    fi

    if ! grep -q "$env_var" "$HOME/.zshrc" 2>/dev/null; then
        echo "export ${env_var}=${key}" >> "$HOME/.zshrc"
        log "API key configured ($env_var)"
    fi
}

verify() {
    export PATH="$BIN_DIR:$PATH"
    if "$BIN_DIR/lumen" --version &>/dev/null; then
        log "Lumen installed successfully: $("$BIN_DIR/lumen" --version)"
    else
        warn "Verification failed. Try: $BIN_DIR/lumen --version"
    fi
}

echo ""
echo -e "${BOLD}══════════════════════════════════════════${NC}"
echo -e "${BOLD}  Lumen macOS Installer${NC}"
echo -e "${BOLD}══════════════════════════════════════════${NC}"
echo ""

check_prereqs

if [[ "$ACTION" == "check" ]]; then
    echo ""
    log "System ready for Lumen installation"
    echo "  Run without --check to install: $0"
    exit 0
fi

install_lumen
configure_key "$API_KEY"
verify

echo ""
log "Installation complete!"
echo "  Restart your terminal or run: source ~/.zshrc"
echo "  Then: lumen"
