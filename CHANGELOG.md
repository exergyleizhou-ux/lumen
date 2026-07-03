# Changelog

## v1.1.1 — Science GUI hardening + xAI geek UI

- **GUI redesign** — pure black terminal aesthetic (`LUMEN://SCIENCE`), monospace,
  `[OK]`/`[WARN]` badges, telemetry strip, grid/scanline backdrop.
- **Backend hardening** — mutate rate limit, `uptime_ms` + `panel` on `/api/health`,
  immutable asset cache headers, `MaxHeaderBytes`.
- **Stress tests** — concurrent health/config/doctor/SSE + rate-limit verification.

## v1.1.0 — Claude Science bridge

> Feature release. Adds `lumen science` — a native Go integration for [Claude
> Science](https://claude.ai/science) third-party models (DeepSeek / Qwen /
> Moonshot / Zhipu), replacing CSswitch-style Python subprocess bridges with a
> single-binary workflow. Pushing `v1.1.0` runs goreleaser (4 cross-platform
> tarballs + `checksums.txt`).

### Claude Science bridge (`lumen science`)
- **Native Go Anthropic proxy** — DeepSeek passthrough, Qwen/Moonshot/Zhipu
  OpenAI translation, CONNECT fast-fail for Anthropic remote MCP, path-secret
  auth, loopback-only listen.
- **DeepSeek cache boost** — optional `cache_control: ephemeral` on system/tools,
  raw-preserve body patching, `/health` cache stats, `lumen science watch` dashboard.
- **Isolated sandbox launcher** — APFS clone of `bin`/`conda`/`runtime`/`seed-assets`
  from `~/.claude-science`; never touches real instance on port 8765.
- **Virtual OAuth forge** — encrypted sandbox login without real Anthropic credentials.
- **Full research pack** — local bio-tools MCP (87 DB clients, 23 domains, ~247
  tools), ketcher-chemistry, 29 skills, org workspace seeding + bundled MCP
  auto-approve; `research list|verify|reseed`.
- **GUI control panel** — Grok Build-style panel (`lumen science gui`, default
  `:18990`) with REST + SSE, shared manager, config validation, CSswitch import.
- **CSswitch migration** — `lumen science migrate [--force]` from `~/.csswitch`.
- **CLI** — `start|stop|status|doctor|verify|watch|mode|official|proxy|config`.
- **Doctor integration** — `lumen doctor` includes optional science bridge checks.
- **Tests** — proxy, oauth, migrate, research, config, runtime, gui (~34 tests).

### Other fixes
- **Web serve** — fix image-paste race on `/v1/chat`; surface provider errors in UI.

---
*Go 1.23+ · single binary · ports: proxy 18991 · sandbox 8990 · GUI 18990*

## v1.0.0-rc1 — first public release candidate

> Pre-release. The previous published release was `v0.2.0`. This is a **1.0
> candidate**, not GA: the core is solid and well-tested, but the multi-provider
> and coding-quality claims are not yet live-verified at scale (see scope below).
> Pushing `v1.0.0-rc1` runs the goreleaser pipeline (4 cross-platform tarballs +
> `checksums.txt`); GitHub marks it a pre-release.

Lumen is a terminal coding agent (Go). Honest scope: a solid, well-tested
single-path agent that does tool-calling on **OpenAI-compatible** backends
(exercised daily on DeepSeek + local LM Studio) and, as of this release, on
**native Anthropic and Gemini** (wire-format verified against mock servers, not
yet live-burned-in). It is **not** yet a measured rival to Cursor/Claude Code —
the first coding-quality baseline is small (a 6-task baseline; the harness now
ships 8 tasks). See `docs/eval-baseline.md`.

### Measurement (the new spine)
- **`lumen eval`** — coding-quality harness: each task runs through the real
  agent and is scored by `go test`. `--json` / `--repeat` / latency reporting.
- **First local baseline recorded** — `google/gemma-4-12b` via LM Studio: 5/6
  (ceiling 6/6; the one miss was a memory-pressure timeout). `docs/eval-baseline.md`.
- **CI gate** — a scripted-provider fixture drives the eval in CI so a pass-rate
  regression fails the build; protected-test-file edits can't fake a pass.

### Multi-provider tool-calling (3 backend families)
- **OpenAI-compatible** — DeepSeek, OpenAI, Grok, Ollama, Qwen, Moonshot, Zhipu,
  Mimo, and any local LM Studio / vLLM server.
- **Native Anthropic** — sends tool schemas + parses streamed `tool_use`
  (was previously a silent degrade to plain chat). Mock-verified.
- **Native Gemini** — parses `functionCall`, sends structured
  `functionCall`/`functionResponse` history. Mock-verified.
- The Anthropic and Gemini paths are **not yet live-burned-in** (no cloud keys in
  the dev env) — the README and this note say so plainly.

### Local daily-driver
- **Configurable `[agent] turn_timeout`** — the per-turn deadline (was a hardcoded
  5 minutes) so a slow local model's first-turn prefill isn't killed.
- **Context-overflow pre-flight guard** — warns before the first turn when the
  system prompt + tool schemas crowd the configured `context_window`, instead of
  letting the window silently slide (the gemma "greeting instead of editing" trap).
- **`[tools] profile`** — `core` (~42 coding tools) vs `full` (~116) to fit small
  local context windows.

### Safety
- **Real interactive approval** — the permission gate now actually prompts in
  chat (default/accept-edits modes) instead of a hardcoded always-yes; headless
  runs auto-approve (no human to ask) with the guard still enforced.
- **Heuristic bash guard** — blocks exfiltration, sensitive reads, recon,
  destructive ops, download-and-execute, encoded payloads in all modes. (A
  denylist, not a sandbox — see `docs/threat-model.md`.)
- **Write-path guard** — every path-taking writer is checked against sensitive /
  persistence paths even in bypass mode.
- **Audit trail** (hash-chained JSONL), **injection isolation** + SSRF guard for
  web/tool content, and an **opt-in OS sandbox** for bash (Seatbelt/bwrap, default
  off — it would block the agent's own builds).

### Agent core & reliability
- Streaming tool-loop with plan-mode gating, prefix-cache stability, model
  compaction (circuit-breakered), checkpoint/rewind, stream-recovery.
- Verify-after-edit: auto build+vet+test after edits with model self-repair
  (Go / Python ruff+pytest / JS-TS tsc+jest); fault rollback via `git checkout`.
- Session persistence (JSONL, auto-resume), background jobs, sub-agents (`task`).

### Terminal & TUI
- Line editing: full cursor movement incl. wrapped-line cursor, CJK/emoji-safe,
  history, bracketed paste. Bubble Tea multi-panel TUI.

### C2D (Compute-to-Data) author toolchain
- **`lumen oasis init|validate|build|deploy`** — author algorithms for the Oasis
  marketplace; `--network none` sandbox execution + Ed25519 attestation of results.

### Operations & distribution
- **`lumen doctor` / `stats` / `reliability` / `config` / `probe-local`**.
- **Release pipeline** — `LICENSE` (MIT), `VERSION`, `.goreleaser.yaml` (darwin/
  linux × amd64/arm64 + checksums), tag-driven workflow; `lumen version` reports
  the injected version/commit/date. **`install.sh` verifies the tarball checksum**
  before installing (fail-closed on mismatch).
- **CI** — `go build` + `vet` + `test -race` on push, plus the eval gate.
- Dependencies: Go stdlib plus BurntSushi/toml and charmbracelet bubbletea/lipgloss
  (it is *not* zero-dependency).

---
*Go 1.23+ · single binary · honest-status README*
