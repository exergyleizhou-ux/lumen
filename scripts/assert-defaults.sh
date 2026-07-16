#!/usr/bin/env bash
# Structural proof: DeepSeek default + example base_url + bin name lumen.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"

fail() { echo "FAIL: $*" >&2; exit 1; }

MODELS="$ROOT/agent/crates/codegen/xai-grok-models/default_models.json"
BIN_TOML="$ROOT/agent/crates/codegen/xai-grok-pager-bin/Cargo.toml"
EXAMPLE="$ROOT/config/lumen.example.toml"

test -f "$MODELS" || fail "missing $MODELS"
grep -q '"default": "deepseek-chat"' "$MODELS" || fail "default_models.json default is not deepseek-chat"
grep -q '"model": "deepseek-chat"' "$MODELS" || fail "default_models.json missing deepseek-chat model entry"

test -f "$BIN_TOML" || fail "missing pager-bin Cargo.toml"
grep -q 'name = "lumen"' "$BIN_TOML" || fail "binary name is not lumen"
grep -q 'default-run = "lumen"' "$BIN_TOML" || fail "default-run is not lumen"

test -f "$EXAMPLE" || fail "missing config/lumen.example.toml"
grep -q 'base_url = "https://api.deepseek.com/v1"' "$EXAMPLE" || fail "example missing DeepSeek base_url"
grep -q 'default = "deepseek-chat"' "$EXAMPLE" || fail "example missing default deepseek-chat"
grep -q 'auto_update = false' "$EXAMPLE" || fail "example missing auto_update = false"

echo "OK: defaults structural checks pass"
