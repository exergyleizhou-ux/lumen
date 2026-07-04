# Lumen Science Lab — API & Directory Reference

> v1.3.0-science-lab.1 · Page B autonomous laboratory on :18992

## Ports

| Port | Service | Command |
|------|---------|---------|
| 18990 | Bridge GUI (Page A) | `lumen science gui` |
| 18991 | Bridge proxy | `lumen science proxy` |
| 8990 | Sandbox | (auto-started by bridge) |
| 18992 | Lab (Page B) | `lumen science lab` |

## API

Base: `http://127.0.0.1:18992`

### Health
```
GET /api/lab/health
→ { status, port, science_mode, research_pack, fleet, provider }
```

### Projects
```
GET    /api/lab/projects                  → [...Project]
POST   /api/lab/projects                  → Project  { title, template? }
GET    /api/lab/projects/:slug            → { project, sessions }
POST   /api/lab/projects/:slug/sessions   → Session   { title? }
```

### Chat
```
POST /api/lab/chat  { project_id, prompt, mode? }
→ SSE: turn_started, text, tool, approval_request, error, turn_done, done
```

### Skills
```
GET /api/lab/skills?project_id=  → { skills: [...], count }
```

### Files
```
GET /api/lab/files?project_id=           → file tree
GET /api/lab/files?project_id=&path=     → file content
GET /api/lab/files/download?project_id=&path= → download
```

### Brief
```
POST /api/lab/brief  { project_id, topic }  → { path, markdown }
```

### Artifacts
```
GET /api/lab/artifacts?project_id=  → artifact list
```

### Provenance
```
GET /api/lab/provenance?project_id=&path=  → { records, count }
```

### Compute
```
GET    /api/lab/compute/ssh-hosts         → ~/.ssh/config hosts
GET    /api/lab/compute/jobs?project_id=  → job list
POST   /api/lab/compute/jobs?project_id=  → Submit  { host, command }
GET    /api/lab/compute/jobs/:id?project_id= → job status
```

### C2D
```
POST /api/lab/c2d/algorithms  { dataset_id } → list_algorithms
```

### Bridge
```
POST /api/lab/bridge/open  → { bridge_url, sandbox_url }
```

### Notebooks
```
GET    /api/lab/notebooks?project_id=           → list
GET    /api/lab/notebooks/cells/:name?project_id= → cells + markdown
POST   /api/lab/notebooks/create?project_id=    → { name, source? }
POST   /api/lab/notebooks/cell/:name?project_id= → append cell
POST   /api/lab/notebooks/execute/:name?project_id= → execute nbconvert
```

## Directory Layout

```
~/.lumen/science/
├── config.json                    # profiles, science_mode, oasis token
├── sandbox/                       # CS sandbox clone
│   └── home/.claude-science/
│       ├── runtime/               # CS research pack (read-only)
│       ├── bin/, conda/           # cloned runtimes
│       └── seed-assets/           # example projects
├── skills/                        # user + Lumen elevation skills
├── lab/
│   └── projects/
│       └── {slug}/
│           ├── project.json
│           ├── provenance.jsonl
│           ├── workspace/
│           │   ├── data/
│           │   ├── figures/
│           │   ├── reports/
│           │   └── notebooks/
│           ├── sessions/
│           └── .lumen/
│               ├── skills/
│               └── compute/
└── logs/
```

## CLI

```
lumen science lab [--port N] [--no-browser]   启动实验室 (:18992)
lumen science publish [--dataset ID]          发布 C2D Agent
lumen science mode hybrid|native|bridge       双页模式
lumen science research verify                 parity 门禁
```

## Verification

```bash
bash scripts/science/lab-smoke.sh              # 离线冒烟
bash scripts/science/lab-parity-verify.sh       # CS parity 门禁
bash scripts/science/full-verify.sh             # 全量回归
```
