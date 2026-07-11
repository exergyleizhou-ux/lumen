#!/usr/bin/env bash
# Versioned Lab deploy to the demo VPS (linux/amd64).
# Usage: ./scripts/science/deploy-lab.sh
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$REPO_ROOT"
export PATH="${HOME}/.local/bin:${PATH}"

HOST="${LUMEN_DEPLOY_HOST:-118.31.47.129}"
KEY="${LUMEN_DEPLOY_KEY:-$HOME/.ssh/oasis_deploy}"
VER="$(tr -d ' \n' < VERSION)"
OUT="/tmp/lumen-linux-amd64-$$"

echo "[deploy-lab] version=$VER host=$HOST"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
  -ldflags "-X main.version=${VER}" \
  -o "$OUT" ./cmd/lumen

scp -i "$KEY" -o ConnectTimeout=15 "$OUT" "root@${HOST}:/tmp/lumen.new"
# Keep runner in sync if present
if [[ -f scripts/science/langgraph_runner.py ]]; then
  scp -i "$KEY" -o ConnectTimeout=15 scripts/science/langgraph_runner.py \
    "root@${HOST}:/root/.lumen/langgraph_runner.py" || true
fi

ssh -i "$KEY" -o ConnectTimeout=15 "root@${HOST}" bash -s <<EOF
set -e
install -m 755 /tmp/lumen.new /usr/local/bin/lumen
# Ensure LangGraph sidecar flags (idempotent drop-in)
mkdir -p /etc/systemd/system/lumen-lab.service.d
if [[ ! -f /etc/systemd/system/lumen-lab.service.d/langgraph.conf ]]; then
  cat > /etc/systemd/system/lumen-lab.service.d/langgraph.conf <<'ECONF'
[Service]
Environment=LUMEN_LANGGRAPH=1
Environment=LUMEN_LANGGRAPH_VENV=/root/.lumen/langgraph-venv
Environment=LUMEN_LANGGRAPH_SCRIPT=/root/.lumen/langgraph_runner.py
ECONF
fi
systemctl daemon-reload
systemctl restart lumen-lab
sleep 2
systemctl is-active lumen-lab
curl -sS http://127.0.0.1:18992/api/lab/health | python3 -c 'import sys,json;h=json.load(sys.stdin);print("version",h.get("version"));print("onlyoffice",h.get("onlyoffice"));print("langgraph",h.get("langgraph"));print("jupyter",h.get("jupyter"))'
EOF

rm -f "$OUT"
echo "[deploy-lab] done"
# Optional public smoke
if [[ -x "$REPO_ROOT/scripts/science/lab-product-smoke.sh" ]]; then
  echo "[deploy-lab] running product smoke..."
  "$REPO_ROOT/scripts/science/lab-product-smoke.sh" "https://demo.oasisdata2026.xyz/lumen-lab"
fi
