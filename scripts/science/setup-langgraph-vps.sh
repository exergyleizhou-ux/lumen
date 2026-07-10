#!/usr/bin/env bash
# Enable LangGraph sidecar on a Lab host (e.g. demo VPS). Does NOT install OnlyOffice.
# Usage (on target as root, from a machine with the runner file):
#   scp scripts/science/langgraph_runner.py root@host:/root/.lumen/
#   ssh root@host 'bash -s' < scripts/science/setup-langgraph-vps.sh
set -euo pipefail

LUMEN_HOME="${LUMEN_HOME:-/root/.lumen}"
VENV="${LUMEN_LANGGRAPH_VENV:-$LUMEN_HOME/langgraph-venv}"
SCRIPT="${LUMEN_LANGGRAPH_SCRIPT:-$LUMEN_HOME/langgraph_runner.py}"
DROPIN_DIR=/etc/systemd/system/lumen-lab.service.d

mkdir -p "$LUMEN_HOME" "$DROPIN_DIR" /etc/lumen

if [[ ! -x "$VENV/bin/python3" ]]; then
  python3 -m venv "$VENV"
fi
"$VENV/bin/python3" -m pip install -U pip setuptools wheel
"$VENV/bin/python3" -m pip install langgraph langchain-core
"$VENV/bin/python3" -c "import langgraph; print('import OK')"

if [[ ! -f "$SCRIPT" ]]; then
  echo "ERROR: missing runner at $SCRIPT — copy scripts/science/langgraph_runner.py there first" >&2
  exit 1
fi
chmod +x "$SCRIPT"
"$VENV/bin/python3" "$SCRIPT" --project-id setup --prompt 'ping' | python3 -m json.tool

cat > "$DROPIN_DIR/langgraph.conf" <<ECONF
[Service]
Environment=LUMEN_LANGGRAPH=1
Environment=LUMEN_LANGGRAPH_VENV=$VENV
Environment=LUMEN_LANGGRAPH_SCRIPT=$SCRIPT
ECONF

cat > /etc/lumen/langgraph.env.example <<'EEX'
LUMEN_LANGGRAPH=1
LUMEN_LANGGRAPH_VENV=/root/.lumen/langgraph-venv
LUMEN_LANGGRAPH_SCRIPT=/root/.lumen/langgraph_runner.py
EEX

systemctl daemon-reload
systemctl restart lumen-lab
sleep 2
systemctl is-active lumen-lab
curl -sS http://127.0.0.1:18992/api/lab/health | python3 -c 'import sys,json; h=json.load(sys.stdin); print(h.get("langgraph"))'
echo "[setup-langgraph-vps] done (OnlyOffice intentionally not installed)"
