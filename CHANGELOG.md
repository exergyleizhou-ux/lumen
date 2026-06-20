# Changelog

## v1.0.0 — pending tag (built, awaiting release)

> The `v1.0.0` tag has **not been pushed** yet — the latest published release is
> `v0.2.0`. This section is the staged release note; pushing `v1.0.0` runs the
> goreleaser pipeline (4 cross-platform tarballs + `checksums.txt`).

Lumen is a terminal coding agent (Go). Honest scope: a solid, well-tested
single-path agent that does tool-calling on **OpenAI-compatible** backends
(exercised daily on DeepSeek + local LM Studio) and, as of this release, on
**native Anthropic and Gemini** (wire-format verified against mock servers, not
yet live-burned-in). It is **not** yet a measured rival to Cursor/Claude Code —
the first coding-quality baseline is small (6 tasks). See `docs/eval-baseline.md`.

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
