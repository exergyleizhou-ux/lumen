# Lumen

Terminal coding agent monorepo: **Grok Build** fork as `agent/`, plus Masterplan docs and packs.

## Build requirements

```bash
# Required for proto codegen crates
export PROTOC="${PROTOC:-/opt/homebrew/bin/protoc}"
# Install once if missing:
#   brew install protobuf
```

## Build release `lumen`

```bash
export PROTOC="${PROTOC:-/opt/homebrew/bin/protoc}"
cd agent
cargo build -p xai-grok-pager-bin --release
./target/release/lumen --version
./target/release/lumen --help
```

## Defaults (Lumen product)

- Default model: **deepseek-chat** (`agent/crates/codegen/xai-grok-models/default_models.json`)
- Example endpoint wiring: `config/lumen.example.toml` (`base_url = https://api.deepseek.com/v1`)
- Mixpanel telemetry: **off** by default
- Auto-update to x.ai: **off** by default

## Scripts

| Script | Purpose |
|--------|---------|
| `scripts/verify-day0.sh` | Layout + git foundation gates |
| `scripts/assert-defaults.sh` | DeepSeek default + bin name + example base_url |
| `scripts/smoke-deepseek.sh` | Live DeepSeek smoke **or** honest SKIP without key |

```bash
./scripts/verify-day0.sh
./scripts/assert-defaults.sh
./scripts/smoke-deepseek.sh   # set DEEPSEEK_API_KEY for live call
```

## Plan

See `docs/masterplan/` for the full execution plan. M0 = shippable local body; M1+ security/verticals follow.
