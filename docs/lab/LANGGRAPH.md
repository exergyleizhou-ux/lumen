# LangGraph Sidecar for Lumen Lab

Optional Python LangGraph service that runs alongside the Go Lab agent.
It does NOT replace `POST /api/lab/chat`, SSE streaming, or the approval pipeline.

## Architecture

```
Browser/MCP → POST /api/lab/langgraph/run → Go handler → Python subprocess
                                                           ↓
                                                    langgraph graph
                                                           ↓
                                                     JSON result
```

## Env

```bash
LUMEN_LANGGRAPH=1                          # enable
LUMEN_LANGGRAPH_VENV=$HOME/.lumen/langgraph-venv  # Python venv path (optional)
LUMEN_LANGGRAPH_SCRIPT=$HOME/.lumen/langgraph_runner.py  # runner script (optional)
```

## Health

`GET /api/lab/health`:
```json
{
  "langgraph": {
    "available": false,
    "hint": "设置 LUMEN_LANGGRAPH=1 并安装 langgraph (pip install langgraph langchain-core)"
  }
}
```

When available: `"available": true, "hint": "LangGraph 旁路可用"`.

## API

### POST /api/lab/langgraph/run

Request:
```json
{
  "project_id": "default",
  "prompt": "分析工作区文件结构并给出建议"
}
```

Response (success):
```json
{
  "ok": true,
  "result": "analysis output..."
}
```

Response (not available):
```json
{
  "ok": false,
  "error": "LangGraph 不可用：设置 LUMEN_LANGGRAPH=1 并安装 langgraph..."
}
```

Error is never an empty string.

## Setup (local dev)

### One-shot (recommended)

```bash
# From repo root — installs venv + runner under ~/.lumen
./scripts/science/setup-langgraph.sh

# Start Lab with sidecars auto-detected (LangGraph if ready, OnlyOffice if :8088 up)
./scripts/science/lab-local-with-sidecars.sh
```

### Manual

```bash
python3 -m venv ~/.lumen/langgraph-venv
~/.lumen/langgraph-venv/bin/pip install langgraph langchain-core
cp scripts/science/langgraph_runner.py ~/.lumen/langgraph_runner.py
chmod +x ~/.lumen/langgraph_runner.py

export LUMEN_LANGGRAPH=1
export LUMEN_LANGGRAPH_VENV=$HOME/.lumen/langgraph-venv
export LUMEN_LANGGRAPH_SCRIPT=$HOME/.lumen/langgraph_runner.py
lumen science lab --addr 127.0.0.1:18992 --no-browser
```

Runner source of truth: `scripts/science/langgraph_runner.py` (modern `START`/`END` StateGraph API).

### Test

```bash
# Direct runner (no Lab)
~/.lumen/langgraph-venv/bin/python3 ~/.lumen/langgraph_runner.py \
  --project-id demo --prompt 'hello' | python3 -m json.tool

# Via Lab API
curl -sS -X POST http://127.0.0.1:18992/api/lab/langgraph/run \
  -H "Content-Type: application/json" \
  -d '{"project_id":"default","prompt":"hello"}' | python3 -m json.tool
```

Go tests (integration auto-skips if venv missing):

```bash
go test ./internal/science/lab/langgraph/ -count=1 -v
```

## Constraints

- Does NOT replace the Go agent/SSE/approval pipeline
- Only runs when `LUMEN_LANGGRAPH=1` and the Python venv is set up
- Call returns a clear error when unavailable (never empty string)
- 120s timeout for subprocess execution
- Lab does not crash when LangGraph is missing
