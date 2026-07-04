# Lumen Science — Real-Machine Test Matrix (18 items)

> **Iron law:** Agent/automation MUST NOT execute OAuth login or mutate `~/.claude-science`.
> Run manual items with a human present. Automated preflight: `bash scripts/science/rm-preflight.sh`.

## Environment

| Variable | Purpose |
|----------|---------|
| `GUARD_HOME` | Isolated HOME for guard runs (temp dir) |
| `LUMEN_SCIENCE_DIR` | Config under guard HOME |
| `SCIENCE_BIN` | Path to real `claude-science` CLI |
| `RM_BASELINE_8765_PID` | PID listening on 8765 before test (invariant) |

## Automated (offline)

| ID | Check | Command |
|----|-------|---------|
| RM-01 | Guard preflight | `bash scripts/science/real_machine_guard.sh` |
| RM-02 | Unified science tests | `bash scripts/test-science-all.sh` |
| RM-03 | Gitleaks zero findings | `gitleaks detect --source . --config .gitleaks.toml --redact` |
| RM-18 | RM preflight bundle | `bash scripts/science/rm-preflight.sh` |

## Manual (user-present)

| ID | Scenario | Pass criteria |
|----|----------|---------------|
| RM-04 | Virtual OAuth sandbox login | Science sandbox opens; no write to `~/.claude-science` |
| RM-05 | Third-party proxy chat round-trip | User message → model reply via Lumen proxy |
| RM-06 | Profile switch with bad key | Switch rejected; prior profile still active |
| RM-07 | Profile switch with good key | Switch commits; proxy restarts; chat works |
| RM-08 | Relay model picker | `/api/relay/models` populates GUI; selected model used |
| RM-09 | DSML shim rewrite | Leaked DSML block rewritten when `tooluse_shim=rewrite` |
| RM-10 | CONNECT fast-fail | `CONNECT claude.ai:443` returns 401 via proxy |
| RM-11 | Quit semantics | Desktop/GUI quit stops proxy, sandbox keeps running |
| RM-12 | Official mode | Opens real Claude Science; Lumen proxy stopped |
| RM-13 | OAuth token refresh in sandbox | Sandbox session survives without touching real home |
| RM-14 | 8765 PID invariant | Real Science on 8765 unchanged after all steps |
| RM-15 | 5-ship native MCP fleet | `lumen science native verify --live` → 5/5 PASS |
| RM-16 | Research Brief 4-source | `lumen science brief "aspirin"` shows PubMed/ChEMBL/GEO/Oasis |
| RM-17 | Acceptance desktop app | Launch `.app`; `curl http://127.0.0.1:18990/api/health` → `{"status":"ok"}` |

## Desktop artifact path

After `cd desktop/lumen-science && npm run tauri build`:

```
desktop/lumen-science/src-tauri/target/release/bundle/macos/Lumen Science.app
```

Bundle identifier: `com.lumen.science.acceptance`

## RM-04 / RM-06 / RM-13 notes

These require interactive OAuth in the sandbox HOME (`~/.lumen/science/sandbox/home` under guard).
Document results in `docs/science/findings/` with timestamps; do not store raw tokens.