#!/usr/bin/env bash
# Unified science offline gate (all packages + DSML e2e + science-check).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
export GOTOOLCHAIN=local

echo "▶ science-all: quick"
bash scripts/test-science-quick.sh

echo "▶ science-all: DSML e2e + proxy connect/auth"
go test ./internal/science/proxy/... -count=1 -short -timeout 60s -run 'E2E|DSML'

echo "▶ science-all: science-check"
make science-check

echo "▶ science-all: controlled native integration"
go test -tags=integration -p=2 -count=1 -timeout 180s ./internal/science/native ./internal/science/lab/runtime

echo "▶ science-all: test count gate"
COUNT=$(grep -r '^func Test' internal/science --include='*_test.go' | wc -l | tr -d ' ')
if [ "$COUNT" -lt 120 ]; then
  echo "FAIL: need >=120 Test functions, got $COUNT" >&2
  exit 1
fi
echo "  test functions: $COUNT"

if command -v gitleaks >/dev/null 2>&1; then
  echo "▶ science-all: gitleaks"
  gitleaks detect --source "$ROOT/internal/science" --config "$ROOT/.gitleaks.toml" --redact --no-git --verbose
fi

echo "✓ science-all passed"
