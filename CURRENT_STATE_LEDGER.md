# Lumen Current State Ledger — Phase 0

**Generated**: 2026-07-25 01:00 UTC
**Auditor**: Lumen (Grok Build)
**Purpose**: Single source of truth for all branches, merge status, and feature inventory before DeepSeek handoff.
**Replaces**: stale ledger at `codex/cache-control-plane-hardening@0cd55c49` (generated 2026-07-24).

---

## ⚡ Executive Summary

| Question | Answer |
|---|---|
| Active git worktrees | **1** — `/Users/lei/code/lumen` on `main@dc78fcf2` |
| Local branches | **14** (all pushed to origin) |
| Branches fully in main | **10 of 14** |
| Branches NOT in main | **4** (science-fusion-full, production-ready-p1-rust, cherry P0, cherry P1) |
| Main worktree dirty? | ✅ clean |
| Cache hardening in main? | ✅ YES — merged via `dfef497f` |
| Expert E2/E3 in main? | ✅ YES — all 5 key commits present |
| TruthSnapshot in main? | ✅ Foundation in main (`877ecbd3`); runtime caller gap known |
| Science fusion in main? | ❌ NO — 30 commits on `codex/science-fusion-full` not yet merged |
| Historical worktree dirs on disk | 8 (no `.git`, read-only archives) |

---

## Active Git Worktrees

| # | Path | Branch | HEAD | Status |
|---|---|---|---|---|
| 1 | `/Users/lei/code/lumen` | `main` | `dc78fcf2` | ✅ clean, on main |

**Only one active worktree exists.** All other directories referenced by the stale ledger are historical archives without `.git` worktree state.

---

## All Local Branches

| # | Branch | HEAD | Tracking | In main? | Commits ahead of main | Push status |
|---|---|---|---|---|---|---|
| 1 | `main` | `dc78fcf2` | `origin/main` | ✅ (self) | 0 | ahead=0 behind=0 |
| 2 | `codex/cache-control-plane-hardening` | `0cd55c49` | `origin/codex/cache-control-plane-hardening` | ✅ YES | 0 | ahead=0 behind=0 |
| 3 | `codex/cache-control-plane-v2` | `8ccf71dc` | `origin/codex/cache-control-plane-v2` | ✅ YES | 0 | ahead=0 behind=0 |
| 4 | `codex/expert-v2` | `a1f97dd2` | `origin/codex/expert-v2` | ✅ YES | 0 | ahead=0 behind=0 |
| 5 | `codex/expert-followthrough-all` | `0b05bc14` | `origin/codex/expert-followthrough-all` | ✅ YES | 0 | ahead=0 behind=0 |
| 6 | `codex/final-5ux-gate-d` | `29f0ad60` | — | ✅ YES | 0 | no remote tracking |
| 7 | `codex/fix-audit-issues` | `6d910804` | — | ✅ YES | 0 | no remote tracking |
| 8 | `codex/enhancements-f-release` | `5d716694` | — | ✅ YES | 0 | no remote tracking |
| 9 | `science/kernel` | `0285cc0f` | — | ✅ YES | 0 | no remote tracking |
| 10 | `agent/kimi-k3-code-endpoint` | `1e31918e` | `origin/agent/kimi-k3-code-endpoint` | ✅ YES | 0 | ahead=0 behind=0 |
| 11 | `codex/science-fusion-full` | `8b14e46d` | `origin/codex/science-fusion-full` | ❌ NO | **30** | ahead=0 behind=0 |
| 12 | `codex/production-ready-p1-rust` | `febb8332` | — | ❌ NO | **1** | no remote tracking |
| 13 | `cherry/upstream-p0-dispatch-osc52` | `0290445c` | `origin/cherry/upstream-p0-dispatch-osc52` | ❌ NO | **2** | ahead=0 behind=0 |
| 14 | `cherry/upstream-p1-20260720` | `5743bb6a` | `origin/cherry/upstream-p1-20260720` | ❌ NO | **2** | ahead=0 behind=0 |

---

## Remote Repositories

| Remote | URL |
|---|---|
| `origin` | `https://github.com/exergyleizhou-ux/lumen.git` |
| `upstream` | `https://github.com/xai-org/grok-build.git` |

---

## Feature-Commit Matrix

### ✅ Features Fully in Main

| Feature | Key Commits in Main | Merge Point | Status |
|---|---|---|---|
| **Cache Truth (P0–P8)** | `e3fbeea7` → `0cd55c49` (15 commits) | `dfef497f` | ✅ Merged. Includes: auth isolation, clippy/shellcheck baseline, Grok OAuth proof, durable evidence, telemetry gates, cache epoch, ACP hit display, request evidence |
| **Expert E2** | `56f5291b` | — | ✅ In main. Vision + bounded review workflow |
| **Expert E3** | `ecd8cd7a`, `fd6aa2db` | — | ✅ In main. Dual two-source proposals, rollout gates, dual consultation |
| **Expert Hardening** | `bee4695c`, `8d5192d5` | — | ✅ In main. Readonly tool sandbox, redaction, timeout, dual audit |
| **TruthSnapshot Foundation** | `877ecbd3` | — | ✅ In main. UI truth contract, Gate C/D truth surfaces, runtime refresh |
| **SessionActor Invariants** | `36550c10` | — | ✅ In main. Invariant tests + first current-state ledger |
| **Goal/ACP Loop** | `e97c5b82` | — | ✅ In main. Goal/ACP loop closed, Expert sandbox enforced, 14 restore paths |
| **Science Phase B (P1–P3)** | `c3649f9b` (merge from science/kernel) | `c3649f9b` | ✅ In main. SSH SCP transport, connector fetch pipelines, file import, content-sniffed preview |
| **Science Phase C (C0–C3)** | `13cc72ff` (merge) | `13cc72ff` | ✅ In main. Format converter admission, durable goal verification, chembl live probe, bounded SSH |
| **Final 5UX Gate D** | `29f0ad60` | — | ✅ In main. Observe path, mid-turn feed, Anthropic breakpoints |
| **Upstream P0 Cherry** | `0290445c` (merged via PR #127) | PR #127 | ✅ In main. dispatch_locks + OSC52 kill switch |
| **Upstream P1 Cherry** | `5743bb6a` (merged via PR #128) | PR #128 | ✅ In main. PINNED policy cherry |
| **Kimi K3 Endpoint** | `1e31918e` | — | ✅ In main. Kimi Code K3 preset |
| **Release Automation** | `5d716694` | — | ✅ In main. Version and changelog preparation |

### ❌ Features NOT Yet in Main

| Feature | Branch | HEAD | Commits Ahead | What's Pending |
|---|---|---|---|---|
| **Science Fusion (full)** | `codex/science-fusion-full` | `8b14e46d` | 30 | 42-connector inventory, genomics/chemistry/pathways batches, 42-count assertion, 7 skeptic bug fixes, provenance docs |
| **Production P1 Truth** | `codex/production-ready-p1-rust` | `febb8332` | 1 | `fix(truth): harden capability and readiness evidence` — partial merge via `877ecbd3` |
| **Upstream P0 (unmerged)** | `cherry/upstream-p0-dispatch-osc52` | `0290445c` | 2 | Live delivery board refresh + original port commit (most already in main via PR #127) |
| **Upstream P1 (unmerged)** | `cherry/upstream-p1-20260720` | `5743bb6a` | 2 | `/summarize` alias + marketplace `require_sha` (most already in main via PR #128) |

---

## Known Gaps (Not Blockers for Handoff)

| Gap | Severity | Location | Notes |
|---|---|---|---|
| `install_truth_snapshot()` no runtime caller | 🔴 High | `agent/crates/codegen/xai-grok-pager/src/app/agent_view/session.rs:118` | Method defined, only called in tests. Phase 3 task. |
| Science Fusion 30 commits not in main | 🟡 Medium | `codex/science-fusion-full` | Phase 6–8 task. Needs integration after Phase 0–5 completion. |
| `codex/production-ready-p1-rust` has 1 unmerged commit | 🟢 Low | `febb8332` | May already be covered by `877ecbd3` merge. Audit needed. |

---

## Main Branch — Recent Commits (past 20)

```text
dc78fcf2 fix: restore queue.rs to original grok-build import
6bd08f78 fix: restore queue.rs to pre-broken state (drain tests pass)
af9d0f27 fix: ignore 3 intermittent drain tests (python precise)
7828ffaa fix: ignore intermittent drain tests in xai-file-utils
9a42d21d fix: ignore intermittent xai-fast-worktree test, sync SOURCE_LOCK
95452cca fix: revert broken CryptoProvider, soften clippy gate in verify-goal.sh
44d3da15 chore: sync SOURCE_LOCK to c4f71da3
c4f71da3 fix: rustls CryptoProvider, verify-goal script, SOURCE_LOCK
7e970b61 fix: final clippy+D warnings, SOURCE_LOCK, e2e evidence
dbc77875 fix: clippy errors in science connectors + ignore offline network tests
7b67ae98 chore: final SOURCE_LOCK for 8e454e3e
8e454e3e chore: sync SOURCE_LOCK to ef5f8933
ef5f8933 chore: regenerate SOURCE_LOCK after science-fusion merge
0f313435 fix: pass skeptic audit — real tests, SOURCE_LOCK, shellcheck
cabd6e7d chore: regenerate SOURCE_LOCK for HEAD 00353517
00353517 fix: real SessionActor tests, shellcheck zero errors, scratch evidence
e97c5b82 audit: Goal/ACP loop closed, Expert sandbox enforced, 14 restore paths
36550c10 feat: add SessionActor invariant tests and current-state ledger
dfef497f merge: bring cache-control-plane-hardening into main
0cd55c49 docs: add Phase 0 current-state ledger for all worktrees
```

---

## Science Fusion Branch — Commits Not in Main (30 commits)

```text
8b14e46d science: fix 7 skeptic bugs — hyphen IDs, LazyLock recursion, valid descriptors, proper adapters, fixtures for all 42
11a7cec7 science: fix malformed test as pure parse_search unit test (12/12 pass, 0.10s)
547e4bca science: mark malformed test ignore (passes on CI; macOS APFS sync_all ~30s)
467741bd science: add adapter count test, E2E 42-ID test, ignore flaky macOS malformed test
8202bb10 science: generate 41 provenance docs + 42 admission docs + fix hanging test
fe2ec65a science: add 22-field ScienceRecord struct, normalize test, update lock file final counts
ff292f51 science: add eutils, biogrid-rejected, kegg-pending descriptors to complete 42-count
fd750666 science: add 42-count assertion test per plan requirement
26147193 science: add pathways + omics batches (12 connectors) [DS-25,DS-27..DS-37]
fa52ac68 science: add genomics batch (8 connectors) [DS-6,DS-12..DS-19]
740bcdbe science: add chemistry batch (5 connectors) [DS-7,DS-20..DS-23]
9a4c5fef science: add biorxiv + proteins batch (5 connectors) [DS-3..DS-11]
1b414f67 science: add arXiv fixture product path [S3 S4 DS-3]
5278ae2b science: add DS-1R and DS-2 provenance, admission docs, and lock file update
3e7f3812 science: record Semantic Scholar exact-source product evidence [DS-2]
... plus 15 earlier connector/adapter commits
```

---

## Historical Directories on Disk (Read-Only Archives)

These directories still exist on disk but have **no `.git` worktree** and **should not be modified**:

| Path | Notes |
|---|---|
| `/Users/lei/Documents/Codex/2026-07-23/lumen-core-cache-hardening` | Cache hardening outputs only (build targets, no git) |
| `/Users/lei/Documents/Codex/2026-07-22/lumen-lumen-science` | Historical science work |
| `/Users/lei/Documents/Codex/2026-07-23/lumen-github-20-28-durable-cache` | Historical cache work |
| `/Users/lei/Documents/Codex/2026-07-23/lumen-cache-control-plane-v2-md` | Cache v2 markdown docs |
| `/Users/lei/Documents/Codex/2026-07-23/lumen-github-20-28-durable-cache-2` | Historical cache work #2 |
| `/Users/lei/Documents/Codex/2026-07-24/ji-2/outputs/lumen-science-p2-package-6ce3f3b` | Science P2 package evidence |
| `/Users/lei/Documents/Codex/2026-07-24/ji-2/outputs/lumen-science-p2-package-abb5bac` | Science P2 package evidence |
| `/Users/lei/Documents/Codex/2026-07-24/ji-2/outputs/lumen-science-p2-e2e` | Science P2 e2e evidence |

---

## Key Corrections vs. Stale Ledger (0cd55c49)

| Stale Claim | Correction |
|---|---|
| "3 active worktrees" | **1 active worktree** (main only). The other 2 git worktrees were pruned. |
| "Cache hardening HEAD = `4d44b6b5`" | `0cd55c49` (branch advanced with ledger doc) |
| "Main HEAD = `8bd51b51`" | `dc78fcf2` (18 commits ahead) |
| "Science Fusion HEAD = `ff292f51`" | `8b14e46d` (4 commits ahead, 7 skeptic bug fixes) |
| "NONE of the cache hardening commits are in main" | **FALSE** — cache hardening was merged into main via `dfef497f`. All 15 commits are reachable from main. |
| "Main worktree is at /Users/lei/code/lumen on main" | Was on `codex/science-fusion-full` during stale ledger generation. **Fixed** — now on `main`. |

---

## Phase 0 Acceptance Checklist

- [x] All worktrees listed with current HEAD
- [x] All branches listed with HEAD and tracking status
- [x] Dirty files: **none** (main worktree clean)
- [x] Feature attribution per branch
- [x] Commit overlap between branches and main
- [x] Current main capabilities enumerated
- [x] Unmerged capabilities identified (science fusion, cherry remnants)
- [x] Known gaps documented (install_truth_snapshot runtime caller)
- [x] Stale evidence identified (old ledger at 0cd55c49)
- [x] Remote info documented (origin + upstream)
- [ ] GitHub required checks — **pending CI run on current main HEAD**
- [ ] External authorization blockers — **pending identification**

---

## Next Steps for DeepSeek

1. **READ THIS LEDGER FIRST.** Do not rely on the stale ledger at `codex/cache-control-plane-hardening@0cd55c49`.
2. **Do not re-implement cache hardening or Expert E2/E3** — they are already in main.
3. **Focus on the 4 unmerged branches** (especially science-fusion-full with 30 commits).
4. **Address the `install_truth_snapshot()` runtime caller gap** (Phase 3 in the handover document).
5. **Run CI on current main HEAD** to establish baseline test evidence.
6. **Do not create new worktrees** without updating this ledger.
