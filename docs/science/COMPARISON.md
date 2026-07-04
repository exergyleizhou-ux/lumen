# Lumen Science vs CSswitch — Dimension Comparison

> Lumen `v1.2.0-science-beta` vs CSswitch `v0.3.1` (2026-07-04).
> Lumen preserves **5-ship native MCP**, **Research Brief 4-source**, and **Oasis OAuth** — CSswitch does not ship these.

| Dimension | CSswitch | Lumen Science | Verdict |
|-----------|----------|---------------|---------|
| Proxy runtime | Python subprocess | Native Go single binary | **Lumen** |
| Multi-profile switch | Probe + transactional commit | Same + rollback on proxy restart fail | **Parity** |
| Relay / model picker | `/v1/models` dual auth | Same + GUI picker | **Parity** |
| DSML tool-use shim | off/detect/rewrite | Same + e2e tests | **Parity** |
| CONNECT fast-fail 401 | Yes | Yes + e2e | **Parity** |
| Config path isolation | Symlink guards | `AssertConfigDirIsolated` + tests | **Parity** |
| Truthful key save | 401/403 reject | Same on profile POST/PUT | **Parity** |
| Desktop app | Tauri + Python proxy | Tauri + Go `lumen science gui` | **Lumen** |
| Node.js runtime | Removed (Rust oauth forge) | Never required | **Lumen** |
| Native MCP fleet | None | 5 shipped (PubMed/ChEMBL/GEO/Oasis/C2D) | **Lumen** |
| Research Brief | None | 4-source pipeline CLI + API | **Lumen** |
| Oasis embed | None | OAuth + C2D publish routes | **Lumen** |
| DeepSeek cache boost | Limited | First-class + watch dashboard | **Lumen** |
| Offline test gate | run_all.sh | `test-science-all.sh` (≥120 tests) | **Lumen** |
| RM matrix | Manual docs | 18-item doc + automated offline runner | **Lumen** |
| Virtual OAuth idempotency | Rust forge + org stickiness | Go forge + launcher intact checks | **Parity** (different impl) |
| Branding | Terracotta | Verdant Oasis / editorial GUI | Different (functional parity) |

## Honest gaps (Lumen)

| Gap | Status |
|-----|--------|
| RM-04/06/13 OAuth manual matrix | User-present only — documented |
| CSswitch daily maintenance plist | Not ported (optional) |
| SQLite config | CSwitch deferred; Lumen **audit MVP** (`LUMEN_SQLITE_STORE=on`, dual-write JSONL) |

## Verify locally

```bash
bash scripts/science/full-verify.sh
bash scripts/science/rm-offline-auto.sh
```