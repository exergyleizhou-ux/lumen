#!/usr/bin/env bash
# goal:d6aa846b round9 — local curl e2e: temp-dir lumen.toml + bad api_key → /v1/command plan-fail
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="${1:-/var/folders/dn/_prdhdnn5l53lb71bhtx_n5w0000gn/T/grok-goal-d6aa846b7e98/implementer/command-plan-fail-curl.txt}"

cd "$ROOT"
CGO_ENABLED=0 go build -o bin/lumen ./cmd/lumen

TMP=$(mktemp -d)
cleanup() { kill "$PID" 2>/dev/null || true; rm -rf "$TMP"; }
trap cleanup EXIT

cat > "$TMP/lumen.toml" <<'TOML'
default_model = "deepseek"

[[providers]]
name = "deepseek"
kind = "openai"
base_url = "https://api.deepseek.com/v1"
model = "deepseek-chat"
api_key_env = "DEEPSEEK_API_KEY"
TOML

PORT=$((19000 + RANDOM % 500))
cd "$TMP"
LUMEN_DEMO=0 "$ROOT/bin/lumen" serve --addr "127.0.0.1:$PORT" &>/dev/null &
PID=$!
for i in $(seq 1 30); do
  if curl -s -o /dev/null "http://127.0.0.1:$PORT/v1/skills" 2>/dev/null; then break; fi
  sleep 0.2
done

PAYLOAD='{"command":"/workflow auth fail task","api_key":"sk-invalid-test-key","provider":"deepseek"}'
{
  echo "=== LOCAL CURL command plan-fail (temp-dir lumen.toml + bad api_key) ==="
  echo "cwd: $TMP"
  echo "LUMEN_DEMO=0"
  echo "POST http://127.0.0.1:$PORT/v1/command"
  echo "Content-Type: application/json"
  echo ""
  echo "$PAYLOAD"
  echo "---"
  curl -s -w "\nHTTP_STATUS:%{http_code}\n" -X POST "http://127.0.0.1:$PORT/v1/command" \
    -H "Content-Type: application/json" -d "$PAYLOAD"
} | tee "$OUT"

if grep -q 'HTTP_STATUS:400' "$OUT" && ! grep -q 'plan_ready' "$OUT"; then
  echo "PASS: 400 and no plan_ready" >> "$OUT"
else
  echo "FAIL: expected HTTP 400 without plan_ready" >> "$OUT"
  exit 1
fi