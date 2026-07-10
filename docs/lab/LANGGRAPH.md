# LangGraph Sidecar for Lumen Lab

Optional Python LangGraph service that runs alongside the Go Lab agent.
It does NOT replace `POST /api/lab/chat`, SSE streaming, or the approval pipeline.

## Architecture

```
Browser LangGraph pane
  POST { project_id, prompt }
       ↓
handleLangGraphRun (api.go)
  · Resolves WorkspacePath(slug) → fills req.Workspace
       ↓
langgraph.Run(ctx, req)
  python runner.py --project-id … --prompt … --workspace <abs|空>
       ↓
StateGraph: inventory → read_context → synthesize (3 nodes, no external LLM)
       ↓
stdout JSON: {"ok":true,"result":"…"}
```

## Runner Graph (3 nodes)

| Node | Description |
|------|-------------|
| **inventory** | Walks workspace directory (max 80 files, depth ≤5), skips binary/media/package dirs. Produces file list + summary. |
| **read_context** | Reads up to 8 highest-scored text files (prioritizes .md, .py, .ipynb, and prompt-matching names). Max 4000 chars/file. Extracts cells from .ipynb. |
| **synthesize** | Heuristic summary (no external LLM): project info, file inventory, code snippets, response to prompt, ≥2 actionable next steps. |

## Env

```bash
LUMEN_LANGGRAPH=1                          # enable
LUMEN_LANGGRAPH_VENV=$HOME/.lumen/langgraph-venv  # Python venv path
LUMEN_LANGGRAPH_SCRIPT=$HOME/.lumen/langgraph_runner.py  # runner script path
```

## Health

`GET /api/lab/health`:
```json
{
  "langgraph": {
    "available": true,
    "hint": "LangGraph 旁路可用"
  }
}
```

## API

### POST /api/lab/langgraph/run

Request:
```json
{
  "project_id": "default",
  "prompt": "分析工作区文件结构并给出建议"
}
```

The API automatically resolves the workspace path from `project_id` (slug).
Clients do not need to pass `workspace` — the Go handler fills it.

Response (success):
```json
{
  "ok": true,
  "result": "## LangGraph 旁路分析\n- 课题: default\n- 工作区: /root/.lumen/...\n- 文件数: 5\n\n## 工作区摘要\n共 5 个文件\n- notes.md\n- script.py\n...\n\n## 建议下一步\n1. ...\n2. ..."
}
```

Example curl:
```bash
curl -sS -X POST http://127.0.0.1:18992/api/lab/langgraph/run \
  -H "Content-Type: application/json" \
  -d '{"project_id":"default","prompt":"分析工作区"}' | python3 -m json.tool
```

Expected: result contains file names from the workspace, and ≥2 suggestions.

## Setup (local dev)

### 1. Create venv and install

```bash
python3 -m venv ~/.lumen/langgraph-venv
source ~/.lumen/langgraph-venv/bin/activate
pip install langgraph langchain-core
```

### 2. Runner script

The canonical runner is at `scripts/science/langgraph_runner.py` in the repo.
Copy it to the expected location:

```bash
cp scripts/science/langgraph_runner.py ~/.lumen/langgraph_runner.py
chmod +x ~/.lumen/langgraph_runner.py
```

### 3. Start Lab

```bash
export LUMEN_LANGGRAPH=1
lumen science lab --addr 127.0.0.1:18992 --no-browser
```

### 4. Direct test (no Lab)

```bash
mkdir -p /tmp/lg-ws && echo "# demo" > /tmp/lg-ws/notes.md
~/.lumen/langgraph-venv/bin/python3 ~/.lumen/langgraph_runner.py \
  --project-id demo --prompt '总结并给建议' --workspace /tmp/lg-ws | python3 -m json.tool
```

Expected output includes `notes.md` in the result and ≥2 suggestions.

## VPS Deployment

### Sync runner only (no Go rebuild)
```bash
scp -i ~/.ssh/oasis_deploy scripts/science/langgraph_runner.py \
  root@118.31.47.129:/root/.lumen/langgraph_runner.py
```

### If Go code changed
```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /tmp/lumen-linux-amd64 ./cmd/lumen
scp -i ~/.ssh/oasis_deploy /tmp/lumen-linux-amd64 root@118.31.47.129:/tmp/lumen.new
ssh -i ~/.ssh/oasis_deploy root@118.31.47.129 \
  'install -m 755 /tmp/lumen.new /usr/local/bin/lumen && systemctl restart lumen-lab'
```

### VPS smoke
```bash
ssh -i ~/.ssh/oasis_deploy root@118.31.47.129 '
  SLUG=$(curl -sS http://127.0.0.1:18992/api/lab/projects | python3 -c "import sys,json;d=json.load(sys.stdin);print(d[0][\"slug\"] if d else \"\")")
  curl -sS -X POST http://127.0.0.1:18992/api/lab/langgraph/run \
    -H "Content-Type: application/json" \
    -d "{\"project_id\":\"$SLUG\",\"prompt\":\"分析工作区\"}" | python3 -m json.tool | head -30
'
```

## Constraints

- Does NOT replace the Go agent/SSE/approval pipeline
- No external LLM calls — purely heuristic analysis
- 120s timeout for subprocess execution
- Lab does not crash when LangGraph is missing
- Runner must be synced to `~/.lumen/langgraph_runner.py` (local) and `/root/.lumen/langgraph_runner.py` (VPS) after changes
