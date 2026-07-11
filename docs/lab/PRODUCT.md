# Lumen Science Lab — Product Status

**Version**: see repo `VERSION` / `GET /api/lab/health` → `version`  
**Demo**: https://demo.oasisdata2026.xyz/lumen-lab/

## Product definition (100% software surface)

Single-tenant research workbench on **Go Lab API + static SPA**:

| Pillar | Status |
|--------|--------|
| Agent chat (SSE, approval) | ✅ |
| Projects / sessions / files / artifacts / provenance | ✅ |
| Jupyter execute | ✅ |
| Ketcher same-origin + molecules | ✅ |
| Compute hosts/jobs | ✅ |
| Research Pack + Fleet health | ✅ |
| OnlyOffice view/edit + callback (when DS configured) | ✅ code |
| LangGraph sidecar (heuristic + optional LLM) | ✅ |
| Server-side LangGraph history | ✅ |
| Browser history export/import/notes | ✅ |
| Desktop Tauri shell | ✅ buildable |
| Versioned deploy + product smoke | ✅ scripts |

## Infrastructure limits (not software bugs)

| Constraint | Implication |
|------------|-------------|
| Demo VPS ~3.4 GiB RAM | **Do not** run OnlyOffice Document Server on it |
| OnlyOffice image ~3–4 GB | Run DS on a ≥4 GiB host or laptop; set `LUMEN_ONLYOFFICE_URL` |
| Apple Developer cert | Desktop DMG notarization requires a paid identity |
| Multi-tenant SaaS / HA | Out of scope for this product line |

## Ops

```bash
# Deploy Lab to demo VPS
./scripts/science/deploy-lab.sh

# Product smoke (public or local)
./scripts/science/lab-product-smoke.sh https://demo.oasisdata2026.xyz/lumen-lab
./scripts/science/lab-l3-smoke.sh http://127.0.0.1:18992

# Backup local science tree
./scripts/science/lab-backup.sh

# Local sidecars (LangGraph venv + OnlyOffice if :8088 up)
./scripts/science/lab-local-with-sidecars.sh
```

## LangGraph LLM

When the Lab applies the science profile, keys land in env (`DEEPSEEK_API_KEY`, etc.).  
The Python runner then synthesizes with an OpenAI-compatible chat API unless
`LUMEN_LANGGRAPH_LLM=0`. Health reports `langgraph.llm: true|false`.

## OnlyOffice production pattern

1. Run Document Server on a capable host (not the 3.4 GiB VPS).  
2. Point Lab: `LUMEN_ONLYOFFICE_URL=http://ds-host:8088`.  
3. Optional: `LUMEN_ONLYOFFICE_CALLBACK_TOKEN=…` (session endpoint mints tokenized callback URLs).  
4. Lab on `0.0.0.0` when DS is in Docker Desktop and must call back via `host.docker.internal`.

## Desktop

```bash
cd desktop/lumen-lab
npm install
# terminal A: lumen science lab --addr 127.0.0.1:18992
npm run tauri dev   # or npm run tauri build
```

Window defaults to Lab `:18992`. Signing/notarization is optional for internal builds.
