# Lumen Current State Ledger — Phase 0

**Generated**: 2026-07-24
**Auditor**: Grok Build (Lumen)
**Purpose**: Single source of truth for all worktrees, branches, and feature status before integration.

---

## Active Worktrees

| # | Path | Branch | HEAD | Status | Purpose |
|---|---|---|---|---|---|
| 1 | `/Users/lei/Documents/Codex/2026-07-23/lumen-core-cache-hardening/work/lumen-core-cache-hardening` | `codex/cache-control-plane-hardening` | `4d44b6b5` | ✅ clean | Cache truth hardening |
| 2 | `/Users/lei/Documents/Codex/2026-07-24/ji-3/work/lumen-science-fusion` | `codex/science-fusion-full` | `ff292f51` | ✅ clean | Science connector fusion |
| 3 | `/Users/lei/code/lumen` | `main` | `8bd51b51` | ⚠️ 6 modified + 5 untracked | Main worktree (Science docs in progress) |

## Historical Worktrees (read-only, do not modify)

| # | Path | Branch | HEAD | Notes |
|---|---|---|---|---|
| 4 | `.../2026-07-17/ni-z/work/lumen-codex` | `codex/final-5ux-gate-d` | `29f0ad60` | ⚠️ Disk check: `find` hangs on `.jj` directory |
| 5 | `.../2026-07-18/ji-x-4/work/lumen-enhancements-f-release` | `codex/enhancements-f-release` | `5d716694` | — |
| 6 | `.../2026-07-18/lumen-production-ready-p0-p1-rust/work/lumen-rust-p1` | `codex/production-ready-p1-rust` | `febb8332` | — |
| 7 | `.../2026-07-18/main-31b7609-codex-fix-audit-issues/work/lumen-audit` | `codex/fix-audit-issues` | `6d910804` | — |
| 8 | `.../2026-07-23/ji-2/work/lumen-cache-control-plane-v2` | `codex/cache-control-plane-v2` | `8ccf71dc` | Base branch for cache hardening |

---

## Feature-Commit Matrix

| Feature | Branch | Key Commits | In main? | Dirty? | Test Evidence | Needs Port? |
|---|---|---|---|---|---|---|
| **Cache truth** | `codex/cache-control-plane-hardening` | `e3fbeea7`→`4d44b6b5` (4 commits) | ❌ | ✅ clean | 5684p/0f/13i, Clippy 0, shellcheck 4/4, CI passed | **Yes** — needs merge to main |
| **Cache base** | `codex/cache-control-plane-v2` | `8ccf71dc`→`f3efd009` (8 commits) | ❌ | ✅ clean | Pre-hardening cache infrastructure | Already in hardening branch |
| **Science Fusion** | `codex/science-fusion-full` | `3e7f3812`→`ff292f51` (10 commits) | ❌ (Phase C merged separately) | ✅ clean | 42 connectors, offline fixtures | Partially merged via `13cc72ff` |
| **Goal/Expert E2/E3** | main | `56f5291b`, `ecd8cd7a`, `fd6aa2db`, `bee4695c`, `8d5192d5` | ✅ | ⚠️ main has dirty files | Needs re-run on current HEAD | Already in main — audit only |
| **TruthSnapshot** | main | `877ecbd3` nearby | ✅ | — | `install_truth_snapshot()` lacks runtime caller | Audit + wire runtime caller |
| **Windows package** | workflow | In `.github/workflows/windows-team-build.yml` | — | — | No current native artifact | Build on exact SHA |

---

## Commit Overlap: Cache Branch vs Main

```
Cache hardening:
  4d44b6b5 fix(test): harden auth test isolation
  f57de18f chore(clippy,shellcheck): clear strict baseline
  b46245b8 chore(clippy): clear strict shell lint baseline
  e3fbeea7 test(shell): isolate integration homes and await e2e helpers
  f3efd009 fix(auth): sync Grok Build OAuth scope contract  ← last in base branch
  ...
  8ccf71dc feat(cache): prove durable provider cache truth  ← base: cache-control-plane-v2

Main:
  8bd51b51 docs: chronicle full worktree_pool flake evidence
  ...
  13cc72ff merge: phase C science work (C0-C3 plus post-C extensions)
  ...

❌ NONE of the cache hardening commits are in main.
```

---

## Current Main Capabilities (from commit log)

| Capability | Evidence Commit | Status |
|---|---|---|
| E2 Expert | `56f5291b` | In main — needs audit |
| E3 Expert | `ecd8cd7a` | In main — needs audit |
| Expert hardening | `bee4695c`, `8d5192d5` | In main — needs audit |
| Phase C Science (C0-C3) | `13cc72ff` (merge) | In main |
| Format converter audit | `73d46a41` | In main |
| Durable goal verification | `8453adfb` | In main |
| Worktree pool flake fix | `8bd51b51` | In main |

---

## Capabilities NOT in Main

| Capability | Branch | Status |
|---|---|---|
| Provider cache truth (full) | `codex/cache-control-plane-hardening` | P0–P8 complete, needs merge |
| 42 Science connectors (full set) | `codex/science-fusion-full` | Partially in main via Phase C merge |
| Windows native binary | workflow only | Needs build verification |
| SOURCE_LOCK (current) | main | Stale (`9d5d9f22` vs current `8bd51b51`) |

---

## Blocker Register

| Blocker | Type | Resolution |
|---|---|---|
| Grok live proof | External | HTTP 402 — no account balance |
| Kimi cache truth | External | API has no cache usage fields |
| M5 10-min stranger test | Human | Requires independent tester |
| M6 15-day self-use | Human | Requires real usage over time |
| Windows code signing | Decision | Unsigned vs signed release contract |
| SOURCE_LOCK stale | Internal | Must regenerate after all code complete |
| Cache branch unmerged | Internal | Awaiting integration validation |

---

## Stale Evidence

| Evidence | Why Stale | Action |
|---|---|---|
| `SOURCE_LOCK.json` lock HEAD `9d5d9f22` | Current main is `8bd51b51` | Regenerate after freeze |
| Old `f3efd009` cache evidence | Moved to `4d44b6b5` | Use new HEAD evidence |
| Old `3e7f3812` Science evidence | Moved to `ff292f51` | Re-verify on current HEAD |
| Any "40% complete" claim | Now 100% on cache branch | Update all references |
