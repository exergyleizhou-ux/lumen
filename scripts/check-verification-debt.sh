#!/usr/bin/env bash
# S10 — verification_debt read model.
#
# Parses docs/verification-debt.md and reports the debt ledger: open items
# (with their closing condition) and closed items. Exit 0 when there are no
# open debt items; exit 1 otherwise (fail-closed for gates/CI).
#
# Usage: bash scripts/check-verification-debt.sh [--json]
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DOC="$ROOT/docs/verification-debt.md"

[[ -f "$DOC" ]] || { echo "FAIL: $DOC missing"; exit 1; }

open_count=0
closed_count=0
open_items=()
# Rows look like `| DEBT-001 | src | desc | status | cond |` — the leading
# pipe yields an empty first field, so align the variables accordingly.
while IFS='|' read -r _lead _id _src _desc st _cond; do
  st="$(printf '%s' "$st" | tr -d ' ')"
  case "$st" in
    open|open（缓解）) open_count=$((open_count + 1)); open_items+=("$st|$_id") ;;
    closed|*closed*) closed_count=$((closed_count + 1)) ;;
  esac
done < <(grep '^| DEBT-' "$DOC")

if [[ "${1:-}" == "--json" ]]; then
  python3 - "$DOC" <<'PY'
import json, re, sys
lines = open(sys.argv[1]).read().splitlines()
items = []
for ln in lines:
    m = re.match(r"^\| (DEBT-\d+) \| .*? \| (\S+)(?: \(.*\))? \| (.*?) \|$", ln)
    if not m:
        continue
    items.append({"id": m.group(1), "status": m.group(2), "close_condition": m.group(3)})
print(json.dumps({"schema": "lumen.verification_debt.v1", "items": items,
                  "open": sum(1 for i in items if i["status"] == "open"),
                  "closed": sum(1 for i in items if i["status"] != "open")}, indent=2))
PY
  exit 0
fi

echo "=== verification_debt read model ==="
echo "open:  $open_count"
echo "closed: $closed_count"
if ((open_count > 0)); then
  printf 'open items:\n'
  for item in "${open_items[@]}"; do printf '  - %s\n' "$item"; done
  echo "FAIL: verification debt is not zero"
  exit 1
fi
echo "PASS: verification debt == 0"
