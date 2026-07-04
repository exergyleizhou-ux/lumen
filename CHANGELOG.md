# Changelog

## v1.2.0-science-beta.4 — /goal verification gates + SQLite audit MVP

> Prerelease. Master `make goal-all-verify` orchestrator, goal CI workflow, cache
> benchmark gate, SQLite dual-write audit store, and honest audit doc refresh.

### Added
- `make goal-all-verify` — `TestGoalEvidence` + `science full-verify` + `make check`.
- `.github/workflows/goal-ci.yml` — goal evidence + RM offline on push.
- `internal/lumenstore` — optional SQLite store (`LUMEN_SQLITE_STORE=on` → `~/.lumen/lumen.db`).
- `internal/science/proxy/cache_benchmark_test.go` — warm-cache hit-rate gate (≥85%).

### Fixed
- `scripts/science/full-verify.sh` — exits non-zero on any gate failure.
- `profile_switch` tests — use editable `custom` template for mock upstream.
- `Validate` — allow legacy `provider: relay` for schema v2 profile configs.
- `goal_evidence_test` — `LUMEN_REPO_ROOT` instead of hardcoded path.

## v1.2.0-science-beta.3 — RM manual automation

> Prerelease. Automates RM-04..17 in guard HOME via `rm-manual-auto.sh`; fixes
> `IsLoginIntact` for `SCIENCE_REAL_HOME`; mock-upstream fallback when DeepSeek key invalid.

### Added
- `cmd/lumen-science-rm` — orchestrates RM-04..14 in isolated guard HOME.
- `scripts/science/rm-manual-auto.sh` — full real-machine matrix (native, brief, desktop, core).
- `gui.Server.Handler()` — embeddable handler chain for RM relay API checks.
- `SCIENCE_REAL_HOME` read-only asset clone in guard runs.

### Fixed
- `oauth.IsLoginIntact` — correct sandbox root vs auth dir (RM-04/13 self-heal).
- RM-05 mock upstream when live DeepSeek key rejected.
- RM-11/12 quit/official semantics without hanging `claude-science stop`.

## v1.2.0-science-beta.2 — Ultimate science elevation

> Prerelease. Full verification orchestrator, CI gates, comparison doc, automated offline RM
> runner, GUI profile verification badges, release asset pipeline (CLI + MCP + desktop).

### Added
- `scripts/science/full-verify.sh` — one-command all gates (quick, all, RM, desktop, native).
- `scripts/science/rm-offline-auto.sh` — automated offline RM-01/02/03/10/15/16/17/18.
- `scripts/science/publish-science-release.sh` — attach CLI + 5 MCP + desktop zip to GitHub release.
- `.github/workflows/science-ci.yml` — science quick + all on every science-path push.
- `docs/science/COMPARISON.md` — Lumen Science capability matrix.
- `make science-test-all`, `make science-full-verify` targets.
- GUI profile verified badge + hint line; quit-proxy API test.

### Graduated
- RM-desktop → Acceptance `.app` documented in `desktop/lumen-science/README.md`.

## v1.2.0-science-beta.1 — Science bridge parity + science-beta

> Prerelease. Closes remaining Claude Science bridge gaps while preserving
> Lumen-only strengths (5-ship MCP fleet, Research Brief, Oasis OAuth).
> Manual RM steps (RM-04/06/13) still required — see `docs/science/REAL_MACHINE_TEST.md`.

### Bridge parity
- **Multi-profile transactional switch** — upstream `/v1/messages` probe before commit; rollback on failure.
- **Relay provider** — dual auth, `/v1/models` discovery, GUI model picker.
- **DSML tool-use shim** — `off` / `detect` / `rewrite` (default `off`); e2e tests.
- **Config path isolation** — symlink/canonicalize guards reject writes under real Science home.
- **Truthful-save** — reject 401/403 keys on profile create/update; unverified otherwise.
- **gitleaks + science-check** — Makefile target, workflow, findings docs.

### Deliverables
- `scripts/test-science-all.sh` — unified offline gate (≥120 tests).
- `scripts/science/real_machine_guard.sh`, `rm-preflight.sh`, RM 18-item matrix doc.
- `desktop/lumen-science/` — Tauri Acceptance app (`com.lumen.science.acceptance`).

### Unchanged Lumen strengths
- 5-ship native MCP fleet, Research Brief 4-source, Oasis embed, Go single-stack proxy.

## v1.1.2 — Science panel Oasis design + hardening

> Patch release. Science bridge unchanged at API level; focuses on control
> panel UX (Verdant Oasis design system) and GUI backend resilience.

### Science GUI
- **Oasis-aligned UI** — paper/ink palette, Instrument Serif + Geist fonts
  (embedded), HeroPlate-style runtime flow, pill tabs, editorial footer.
- **Third-party branding removed from GUI** — panel is Lumen-native;
  `lumen science migrate` CLI retained for legacy config import.

### Backend
- Mutate rate limiting, richer `/api/health`, static asset cache headers.
- Concurrent stress tests for health/config/doctor/SSE endpoints.

### Upgrade
- Replace binary; restart `lumen science gui`. Hard-refresh browser (Cmd+Shift+R).
- Ports unchanged: GUI 18990 · proxy 18991 · sandbox 8990.

## v1.1.1 — Science GUI hardening + xAI geek UI (intermediate)

- **GUI redesign** — pure black terminal aesthetic (`LUMEN://SCIENCE`), monospace,
  `[OK]`/`[WARN]` badges, telemetry strip, grid/scanline backdrop.
- **Backend hardening** — mutate rate limit, `uptime_ms` + `panel` on `/api/health`,
  immutable asset cache headers, `MaxHeaderBytes`.
- **Stress tests** — concurrent health/config/doctor/SSE + rate-limit verification.

## v1.1.0 — Claude Science bridge

> Feature release. Adds `lumen science` — a native Go integration for [Claude
> Science](https://claude.ai/science) third-party models (DeepSeek / Qwen /
> Moonshot / Zhipu), replacing Python subprocess bridges with a
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
  `:18990`) with REST + SSE, shared manager, config validation, legacy config import.
- **Legacy migration** — `lumen science migrate [--force]` imports prior bridge settings.
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
