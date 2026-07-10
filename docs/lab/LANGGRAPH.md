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

### 1. Create venv and install dependencies

```bash
python3 -m venv ~/.lumen/langgraph-venv
source ~/.lumen/langgraph-venv/bin/activate
pip install langgraph langchain-core
```

### 2. Create the runner script

Save as `~/.lumen/langgraph_runner.py`:

```python
#!/usr/bin/env python3
"""Minimal LangGraph runner for Lumen Lab sidecar."""
import argparse, json, sys

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--project-id", default="")
    parser.add_argument("--prompt", default="")
    args = parser.parse_args()

    try:
        from langgraph.graph import StateGraph
        from typing import TypedDict

        class State(TypedDict):
            prompt: str
            result: str

        def process_node(state: State) -> State:
            return {"result": f"Processed: {state['prompt'][:200]}"}

        graph = StateGraph(State)
        graph.add_node("process", process_node)
        graph.set_entry_point("process")
        graph.set_finish_point("process")
        app = graph.compile()

        result = app.invoke({"prompt": args.prompt})
        print(json.dumps({"ok": True, "result": result.get("result", "")}))
    except Exception as e:
        print(json.dumps({"ok": False, "error": str(e)}))

if __name__ == "__main__":
    main()
```

### 3. Start Lab with LangGraph enabled

```bash
export LUMEN_LANGGRAPH=1
lumen science lab --addr 127.0.0.1:18992 --no-browser
```

### 4. Test

```bash
curl -sS -X POST http://127.0.0.1:18992/api/lab/langgraph/run \
  -H "Content-Type: application/json" \
  -d '{"project_id":"default","prompt":"hello"}' | python3 -m json.tool
```

## Constraints

- Does NOT replace the Go agent/SSE/approval pipeline
- Only runs when `LUMEN_LANGGRAPH=1` and the Python venv is set up
- Call returns a clear error when unavailable (never empty string)
- 120s timeout for subprocess execution
- Lab does not crash when LangGraph is missing
