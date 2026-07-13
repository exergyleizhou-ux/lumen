#!/usr/bin/env bash
set -euo pipefail

: "${WORKBENCH_TOKEN_FILE:?path to a freshly minted Workbench JWT file}"
: "${LUMEN_CODE_URL:?Code proxy URL, for example http://127.0.0.1:19080}"
: "${LUMEN_LAB_URL:?Lab proxy URL, for example http://127.0.0.1:19410}"
: "${WORKBENCH_PARENT_ORIGIN:?exact configured parent origin}"

token="$(<"$WORKBENCH_TOKEN_FILE")"
[[ -n "$token" ]] || { echo "empty token file" >&2; exit 2; }
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

expect_status() {
  local want="$1" out="$2"
  shift 2
  local got
  got="$(curl --silent --show-error --output "$out" --write-out '%{http_code}' "$@")"
  [[ "$got" == "$want" ]] || { echo "expected HTTP $want, got $got" >&2; cat "$out" >&2; exit 1; }
}

expect_status 200 "$tmp/health" "$LUMEN_CODE_URL/healthz"
expect_status 200 "$tmp/code-ready" "$LUMEN_CODE_URL/readyz"
expect_status 401 "$tmp/code-unauth" "$LUMEN_CODE_URL/v1/status"
expect_status 200 "$tmp/code-auth" -H "Authorization: Bearer $token" "$LUMEN_CODE_URL/v1/status"
expect_status 200 "$tmp/lab-ready" "$LUMEN_LAB_URL/api/lab/readyz"
expect_status 401 "$tmp/lab-unauth" "$LUMEN_LAB_URL/api/lab/projects"
expect_status 200 "$tmp/lab-auth" -H "Authorization: Bearer $token" "$LUMEN_LAB_URL/api/lab/projects"

curl --silent --show-error --no-buffer --max-time 30 \
  --dump-header "$tmp/sse.headers" --output "$tmp/sse.body" \
  -H "Authorization: Bearer $token" \
  -H "Origin: $WORKBENCH_PARENT_ORIGIN" \
  -H 'Content-Type: application/json' \
  --data '{"prompt":"hosted deployment smoke","mode":"default"}' \
  "$LUMEN_CODE_URL/v1/chat" || true

grep -qi '^Content-Type: text/event-stream' "$tmp/sse.headers"
grep -Fqi "Access-Control-Allow-Origin: $WORKBENCH_PARENT_ORIGIN" "$tmp/sse.headers"
grep -q '"event_id"' "$tmp/sse.body"
grep -q '"kind":"stream_done"' "$tmp/sse.body"
echo "hosted smoke PASS"
