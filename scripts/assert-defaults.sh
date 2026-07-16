#!/usr/bin/env bash
# Structural proof: DeepSeek-preferred default + multi-provider catalog + lumen bin.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"

fail() { echo "FAIL: $*" >&2; exit 1; }

MODELS="$ROOT/agent/crates/codegen/xai-grok-models/default_models.json"
BIN_TOML="$ROOT/agent/crates/codegen/xai-grok-pager-bin/Cargo.toml"
EXAMPLE="$ROOT/config/lumen.example.toml"

test -f "$MODELS" || fail "missing $MODELS"
grep -q '"default": "deepseek-chat"' "$MODELS" || fail "default_models.json default is not deepseek-chat"
grep -q '"model": "deepseek-chat"' "$MODELS" || fail "default_models.json missing deepseek-chat model entry"
# BYOK must be embedded so isolated GROK_HOME works without xAI login.
grep -q '"base_url": "https://api.deepseek.com/v1"' "$MODELS" || fail "default_models.json missing DeepSeek base_url"
grep -q '"env_key": "DEEPSEEK_API_KEY"' "$MODELS" || fail "default_models.json missing env_key DEEPSEEK_API_KEY"
grep -q '"byok": true' "$MODELS" || fail "default_models.json missing byok true"
# Multi-provider catalog (DeepSeek preferred, not exclusive)
for id in openai-gpt4o claude-sonnet xai-grok glm-4 qwen-plus mimo ollama; do
  grep -q "\"id\": \"$id\"" "$MODELS" || fail "default_models.json missing provider id=$id"
done
grep -q '"api_backend": "messages"' "$MODELS" || fail "default_models.json missing Anthropic messages backend"
grep -q '11434' "$MODELS" || fail "default_models.json missing Ollama base_url port"

test -f "$BIN_TOML" || fail "missing pager-bin Cargo.toml"
grep -q 'name = "lumen"' "$BIN_TOML" || fail "binary name is not lumen"
grep -q 'default-run = "lumen"' "$BIN_TOML" || fail "default-run is not lumen"

test -f "$EXAMPLE" || fail "missing config/lumen.example.toml"
grep -q 'base_url = "https://api.deepseek.com/v1"' "$EXAMPLE" || fail "example missing DeepSeek base_url"
grep -q 'default = "deepseek-chat"' "$EXAMPLE" || fail "example missing default deepseek-chat"
grep -q 'auto_update = false' "$EXAMPLE" || fail "example missing auto_update = false"
grep -q '\[model.openai-gpt4o\]' "$EXAMPLE" || fail "example missing OpenAI preset"
grep -q '\[model.claude-sonnet\]' "$EXAMPLE" || fail "example missing Claude preset"
grep -q '\[model.ollama\]' "$EXAMPLE" || fail "example missing Ollama preset"

# Registry + update crate must agree: auto_update default false.
REG="$ROOT/agent/crates/codegen/xai-grok-pager/src/settings/registry.rs"
DEFS="$ROOT/agent/crates/codegen/xai-grok-pager/src/settings/defs.rs"
UPD="$ROOT/agent/crates/codegen/xai-grok-update/src/auto_update.rs"
grep -q 'auto_update.unwrap_or(false)' "$REG" || fail "registry auto_update must unwrap_or(false)"
grep -q 'SettingKind::Bool { default: false }' "$DEFS" || fail "defs auto_update default must be false"
grep -q 'configured.unwrap_or(false)' "$UPD" || fail "effective_auto_update must unwrap_or(false)"

echo "OK: defaults structural checks pass (DeepSeek BYOK + auto_update=false)"
