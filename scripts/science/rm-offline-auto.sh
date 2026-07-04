#!/usr/bin/env bash
# Automated offline RM items (no OAuth, no ~/.claude-science writes).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"
export GOTOOLCHAIN=local

echo "▸ RM-offline-auto"

# RM-01 guard
bash scripts/science/real_machine_guard.sh

# RM-02 science-all
bash scripts/test-science-all.sh

# RM-03 gitleaks (science tree)
if command -v gitleaks >/dev/null 2>&1; then
  gitleaks detect --source internal/science --config .gitleaks.toml --redact --no-git
fi

# RM-10 CONNECT 401 (unit via go test)
go test ./internal/science/proxy/... -count=1 -short -timeout 30s -run 'Connect|BlockedConnect' >/dev/null

# RM-15 native fleet (offline unit; live in full-verify)
go test ./internal/science/native/... -count=1 -short -timeout 60s >/dev/null

# RM-16 brief builder (offline)
go test ./internal/science/native/brief/... -count=1 -short -timeout 30s >/dev/null

# RM-17 desktop artifact exists (macOS only)
if [[ "$(uname -s)" == "Darwin" ]]; then
  APP="${LUMEN_DESKTOP_APP:-$ROOT/desktop/lumen-science/src-tauri/target/release/bundle/macos/Lumen Science.app}"
  if [[ ! -d "$APP" ]]; then
    if [[ "${LUMEN_RELEASE_VERIFY:-}" == "1" ]]; then
      echo "FAIL RM-17: desktop .app missing at $APP" >&2
      echo "  build: cd desktop/lumen-science && npm run build" >&2
      exit 1
    fi
    echo "WARN: desktop .app not built — run: cd desktop/lumen-science && npm run build" >&2
  else
    echo "▸ RM-17 artifact: $APP"
  fi
fi

# RM-18 bundle
bash scripts/science/rm-preflight.sh >/dev/null

echo "✓ RM-offline-auto PASS"