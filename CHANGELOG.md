# Changelog

## v1.0.0 (2026-06-17) — SpaceX Production Grade

### Core

- **verify-after-edit** — automatic build+vet+test after every file edit, with model self-repair (up to 3 cycles). Supports Go, Python (ruff+pytest), JavaScript/TypeScript (tsc+jest).
- **LSP diagnostics** — gopls check results collected after build passes, fed to model via FormatFeedback.
- **Fault rollback** — same file failing verify 2+ consecutive times triggers automatic `git checkout` restore.
- **Multi-model** — 9 providers, 26 presets. DeepSeek optimized (96-99% cache hit).
- **4 permission modes** — bypass / plan / default / accept-edits, with 5-layer bash command guard.

### Input & Terminal

- **Line editing** — full cursor movement (← → Home End), CJK/emoji-safe, mouse scrollback, text selection. 47 tests.
- **Keyboard shortcuts** — Ctrl+W (delete word), Ctrl+K (kill to end), ESC (clear buffer), Tab (command completion), Ctrl+A/Ctrl+E (home/end).
- **Slash commands** — `/cost`, `/cache`, `/rewind`, `/replay`, `/changes`, `/retry`, `/undo`, `/help`, `/stats`, `/reliability`.
- **Multi-line input** — warp-free, no ghost text on multi-row terminals.
- **Scrollback preserved** — `\x1b[K` per-line clearing, never destroys terminal history.

### Agent Reliability

- **Session persistence** — JSONL history files, auto-resume on restart (`📂 resumed: 168 messages`).
- **Context compaction** — model-based auto-compact with circuit breaker (3 consecutive → disabled).
- **Per-turn timeout** — 5-minute hard limit, Ctrl+C cancels within the same turn.
- **Session timeline** — every turn, tool call, and file change recorded. `/replay` to rewatch, `/changes` for diff inbox.
- **Sub-agents** — `task` tool spawns isolated agents with tool whitelists.
- **Background jobs** — `bash` and `task` support `run_in_background` with `bash_output`, `wait`, `kill_shell`.
- **Monthly reliability reports** — `lumen reliability` generates per-month crash/verify/rollback/token/cost metrics.

### C2D (Compute-to-Data) — SpaceX Factory→Shelf

- **`lumen oasis init|validate|build|deploy`** — full algorithm author toolchain.
- **Marketplace compute module** — `POST /api/v1/compute/algorithms` → `POST /compute/jobs` → Worker polls pending → DockerRunner executes with `--network none` isolation → Ed25519 attestation → result persistence.
- **Conveyor belt** — `lumen oasis deploy` auto-registers algorithm on marketplace API.
- **Regression fixtures** — every successful C2D run becomes a permanent regression test in `RegressionStore`.
- **Real C2D flight** — linear-model container executed with `--network none`, `--read-only`, producing verified output (mae=0.2571).

### TUI

- **Bubble Tea multi-panel** — chat (60%), plan+diff (40%), persistent status bar.
- **Verify indicator** — `⟳ verifying…` / `✓ verified` / `✗ detail` in status bar.
- **Event bridge** — agent events stream to TUI via `tuiSink`, text typed in TUI flows to controller.
- **8 TUI tests** — running, ok, fail, empty, truncated, status-msg independence.

### Operations

- **`lumen doctor`** — 8 health checks: config, provider reachability, git, go version, gopls, verify config, workspace.
- **`lumen stats`** — per-session message/turn/token/line table with totals.
- **`lumen reliability`** — monthly crash/verify/rollback report.
- **`lumen config`** — current configuration display (model, providers, key sources, permissions).
- **Binary** — 11MB single Go binary, zero runtime dependencies.
- **CI** — GitHub Actions: go build + vet + test -race on push.

### Security

- **Key rotation** — leaked API key purged from shell config files. Key stored as env var only.
- **5-layer bash guard** — exfiltration, sensitive reads, recon, destructive, encoded payloads blocked in all modes.
- **File safety** — binary detection, 10MB size limit, workspace boundary enforcement.

### Engineering

- **Package count** — 54 packages (down from 192 in earlier releases).
- **Test coverage** — 51+ test packages passing (editverify: 47, lineedit: 44, agent: 3 fault rollback, tui: 8, oasis: 9, reliability: 4, marketplace compute: 14).
- **Race detector** — all tests pass with `-race`.
- **Vet** — zero warnings across all packages.

---
*Built with Go 1.23+ · single binary · zero runtime deps*
