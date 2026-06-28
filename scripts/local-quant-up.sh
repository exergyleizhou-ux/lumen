#!/usr/bin/env bash
# local-quant-up.sh — dry-run (or run for real) the local-compute -> lumen -> quant loop.
#
#   MODE=sim ./scripts/local-quant-up.sh                       # drive lumen with the exo simulator
#   MODE=exo EXO_HOST=192.168.1.50 ./scripts/local-quant-up.sh # once the cluster is up
#
# Same script, one switch. It (1) ensures an endpoint, (2) probes that the
# endpoint can drive the agent, (3) lets lumen run the quant pipeline against it,
# (4) prints the verified VQ certificate. Build lumen first:
#   go build -o lumen ./cmd/lumen && export PATH="$PWD:$PATH"   (or set LUMEN_BIN)
set -euo pipefail

LUMEN="${LUMEN_BIN:-lumen}"
if ! command -v "$LUMEN" >/dev/null 2>&1 && [ ! -x "$LUMEN" ]; then
  echo "lumen not found — build it: go build -o lumen ./cmd/lumen && export PATH=\"\$PWD:\$PATH\"" >&2
  exit 1
fi
# Make a binary named `lumen` resolvable for the agent's bash subprocess.
LUMEN_DIR="$(cd "$(dirname "$(command -v "$LUMEN" 2>/dev/null || echo "$LUMEN")")" && pwd)"
export PATH="$LUMEN_DIR:$PATH"
command -v lumen >/dev/null 2>&1 || ln -sf "$(command -v "$LUMEN" || echo "$LUMEN")" "$LUMEN_DIR/lumen"

SIM="${EXO_SIM:-$(dirname "$0")/exo_sim.py}"
MODE="${MODE:-sim}"
PORT="${EXO_PORT:-52415}"
WORK="$(mktemp -d)"
cleanup() { [ -n "${SIM_PID:-}" ] && kill "$SIM_PID" 2>/dev/null || true; }
trap cleanup EXIT

if [ "$MODE" = "sim" ]; then
  echo "▶ starting exo simulator on :$PORT (no GPU needed)"
  python3 "$SIM" "$PORT" & SIM_PID=$!
  sleep 1
  BASE="http://127.0.0.1:$PORT/v1"
else
  BASE="http://${EXO_HOST:?set EXO_HOST to the cluster head node}:$PORT/v1"
  echo "▶ targeting real exo at $BASE"
fi

echo "▶ [1/3] probe: can the endpoint drive the agent?"
lumen probe-local --base-url "$BASE" --timeout 60s

echo "▶ [2/3] lumen (on the endpoint) runs the quant pipeline"
cat > "$WORK/lumen.toml" <<EOF
default_model = "exo"
[tools]
profile = "core"
[[providers]]
name = "exo"
kind = "openai"
base_url = "$BASE"
model = "local-model"
api_key_env = "EXO_API_KEY"
EOF
( cd "$WORK" && EXO_API_KEY=sk-local lumen run --mode bypass \
    "Use the lumen quant toolchain to scaffold a strategy, backtest it, and verify the certificate." )

echo "▶ [3/3] result"
if [ -f "$WORK/simstrat/quant-cert.json" ]; then
  python3 - "$WORK/simstrat/quant-cert.json" <<'PY'
import json, sys
c = json.load(open(sys.argv[1]))
print("  certificate:", c["cert_id"])
print("  metrics    :", {k: round(v, 4) for k, v in c["metrics"].items()})
print("  data hash  :", c["data_sha256"][:16], " equity hash:", c["equity_curve_sha256"][:16])
PY
  echo "✅ full loop worked: local endpoint -> lumen -> quant -> verified VQ cert"
else
  echo "⚠ no certificate produced — see lumen output above" >&2; exit 1
fi
