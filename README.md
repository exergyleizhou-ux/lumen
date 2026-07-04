# Lumen

> **「你是我绿洲里的光」**
>
> A terminal coding agent where **security is not an afterthought.** Multi-model, plan-mode,
> fine-grained permissions, and full session observability — in a single Go binary.

[![Go](https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/license-MIT-green)](./LICENSE)
[![Binary](https://img.shields.io/badge/binary-9.9MB-blue)]()

---

## Why Lumen

Lumen's wedge is **the agent that fixes its own mistakes**: after it edits code it
automatically runs your build/vet/tests, reads the failures, and self-repairs (up to 3
cycles) before handing back — so you review working changes, not broken ones. Around that
it's a control-first terminal agent: four permission modes, a heuristic dangerous-command
guard (a denylist, not a sandbox), file-system safety boundaries, and a single Go binary you
can point at any OpenAI-compatible model — cloud or local (LM Studio / Ollama / vLLM).

**Honest status:** the coding agent does tool-calling on all three backend families —
**OpenAI-compatible** (DeepSeek, LM Studio, Ollama, vLLM, OpenAI, Grok, Qwen, Moonshot, Zhipu),
**native Anthropic**, and **native Gemini**. The OpenAI path is exercised daily; the native
Anthropic and Gemini paths are **wire-format verified against mock servers but not yet
live-burned-in** (no cloud keys in the dev env). Primarily exercised on DeepSeek; a first local
coding-quality baseline exists (`docs/eval-baseline.md`: gemma-4-12b 5/6), but it is small (6
tasks) — treat the points below as *mechanics*, not measured superiority. Solo
project, no third-party users yet.

---

## Screenshots

**Banner + Model List** — 29 presets across 9 providers, color-coded by provider:

```
  🪄  LUMEN  ·  deepseek/deepseek-chat  ·  🛡 default

  🧠 openai               🔍 deepseek            ⚡ xai
    ▸ gpt-4o                ▸ deepseek-chat        ▸ grok-3
    ▸ gpt-4o-mini           ▸ deepseek-reasoner    ▸ grok-3-mini
    ▸ o4-mini

  🏛 anthropic             🌙 moonshot             🐉 qwen
    ▸ claude-sonnet-4       ▸ kimi-k2              ▸ qwen-max
    ▸ claude-opus-4         ▸ moonshot-v1          ▸ qwen-coder
```

**Task Execution** — bash auto-approved, output preview inline:

```
▸ [🛡 default] run: python3 -c "print(42)"

  ⏳ …                                    ← instant spinner feedback

   1. ⚡ bash ✓  42                       ← first-line output preview

  · 📊 14k  ♻ 99%  💰 $0.0038  ⚙ 1st · turn #1
```

**Permission Modes** — switch on the fly, mode always visible in prompt:

```
▸ [🛡 default]      ← initial mode
▸ [🔒 plan]         ← /mode plan
▸ [🔓 bypass]       ← /mode bypass
```

**Health Check** — `lumen doctor` validates everything:

```
✅ config: ./lumen.toml
✅ provider: deepseek — 2 models reachable
✅ workspace: /Users/lei
✅ git: /usr/bin/git
✅ go: go version go1.23.4 darwin/arm64
⚠️  gopls: gopls not found — install with …
✅ verify: enabled scope=changed-pkg tests=on max_repair=3
All checks passed.
```

**Slash Commands** — help, model switch, workflow, undo, skills:

```
/wizard         ✨ AI interviews you, then builds
/workflow <t>   📋 plan → review → execute
/ultra <t>      ⚡ plan → auto-execute
/undo           ↩ undo last file edits
/models         🗂️  list 26 models
/model <name>   🔄 switch model
/mode           🔓🔒🛡 permission modes
/history        📜 recent messages
/<skill>        🎯 invoke skill (explore, review, test, …)
```

---

## Features

### Core Differentiators

- 🛡️ **5-Layer Bash Guard** — Blocks exfiltration, sensitive file reads, recon, destructive ops,
  and encoded payloads *before* execution. Works even in bypass mode.
- 🔓 **4 Permission Modes** — `bypass` (full auto) / `plan` (read-only) / `default` (prompt for
  dangerous) / `accept-edits` (auto-approve writes, ask for bash). Inspired by Claude Code.
- 🤖 **Multi-Model** — three backend families with tool-calling: **OpenAI-compatible** (DeepSeek
  96-99% cache hit, OpenAI, Grok, Ollama, Qwen, Moonshot, Zhipu, Mimo, + any local LM Studio /
  vLLM server), **native Anthropic**, and **native Gemini** (the latter two mock-verified, not yet
  live-burned-in). 29 presets.
- 📋 **Plan Mode** — Read-only exploration → structured plan → review → execute. Cache-stable
  sessions prevent token waste on plan revision.
- ✅ **Verify-after-edit** — After editing code, Lumen auto-runs the project's build/lint/test
  (Go: `build`/`vet`/`test` · Python: `ruff`/`pytest` · JS-TS: `tsc`/`jest`), detected per
  changed file, and feeds any failures back to the model to **self-repair** — up to a
  configurable cycle cap, then hands back to you. Activates in any Go/JS/Python project;
  uninstalled tools are skipped (never a false failure). Tune via `[verify]` in `lumen.toml`.

### Advanced

- 🔀 **Coordinator** — Dual-model Planner+Executor. Separate cache-stable sessions — the Planner
  never sees execution artifacts, so its cache stays warm.
- 🧵 **Sub-Agents** — `task` tool spawns isolated agents with tool whitelists. Each has its own
  session, gate, and max-steps limit.
- 📚 **22 Skills** — `explore`, `review`, `bug-hunt`, `security-review`, `test`, `benchmark`,
  `api-design`, `brainstorming`, `e2e-testing`, and more.
- 🕐 **Session Timeline** — Every turn, tool call, and file change recorded. `/replay` to
  rewatch, `/changes` for the diff inbox.
- 🔍 **LSP Integration** — Real `gopls` diagnostics, hover, definition, references. Not a mock.

### Engineering

- 🏗️ **Transport-Agnostic** — Controller powers CLI chat, one-shot `run`, Bubble Tea TUI, and
  future HTTP/SSE from one code path.
- 🖥️ **Bubble Tea TUI** — Multi-panel layout: chat (60%), plan+diff (40%), persistent status bar.
  Keyboard navigation, thinking-block folding, spinner animation.
- 🔁 **Retry Logic** — Exponential backoff on 429/503/5xx, up to 3 retries per call.
- 📡 **MCP Ready** — `mcp__` namespace support, JSON-RPC stdio client.
- 📦 **Single Binary** — ~13 MB, zero runtime dependencies. `install.sh` fetches a release binary (or builds from source if none is published yet).

---

## Quick Start

**Install a release binary** (no Go toolchain needed) — pick your platform from the
[latest release](https://github.com/exergyleizhou-ux/lumen/releases/latest):

```bash
# macOS arm64 example — swap the asset for your OS/arch
curl -L https://github.com/exergyleizhou-ux/lumen/releases/latest/download/lumen_1.1.2_darwin_arm64.tar.gz | tar xz
./lumen version
```

**…or build from source** (≤ 30 seconds):

```bash
git clone https://github.com/exergyleizhou-ux/lumen.git
cd lumen && go build -o bin/lumen ./cmd/lumen

# Create minimal config (one provider is enough)
cat > lumen.toml << 'TOML'
default_model = "deepseek-chat"

[[providers]]
name = "deepseek"
kind = "openai"
base_url = "https://api.deepseek.com/v1"
model = "deepseek-chat"
api_key = "sk-your-deepseek-key"
TOML

# Or keep the key out of the file:
#   export DEEPSEEK_API_KEY=sk-...
# (lumen reads API keys from env vars — omit api_key from the file)

# Verify your setup
./bin/lumen doctor

# Start coding
./bin/lumen run "explain this project"        # one-shot
./bin/lumen run --plan "add OAuth"            # plan mode
./bin/lumen chat                               # line-mode REPL
./bin/lumen tui                                # multi-panel Bubble Tea TUI
```

*For other providers (OpenAI, Anthropic, Grok, Ollama…), add more `[[providers]]` entries. See `internal/config/model_presets.go` for the full 29-model preset list. Prerequisites: Go 1.23+, optional `gopls` for LSP diagnostics.*

---

## Architecture

```
User Input → CLI (cmd/lumen/main.go)
              │
              ▼
         Controller (control/) — transport-agnostic
              │
              ▼
         Agent.Run() loop (agent/)
              │
              ├── 1. autoCompact (char-based token estimate)
              ├── 2. PrefixShape check (cache churn detection)
              ├── 3. Conditional Sanitize (needsRepair)
              ├── 4. Provider.Stream (SSE with retry)
              ├── 5. partitionToolCalls (read-only || writer serial)
              ├── 6. executeOne (PlanMode → Permission → Guard → PreEdit → Execute → Evidence)
              ├── 7. Storm Breaker (3rd identical failure → redirect)
              └── 8. feed results → loop
              │
              ▼
         Event Sink → TUI / Headless / Timeline
```

## Permission Modes

| Mode | Read | Write | Bash | Use Case |
|------|------|-------|------|----------|
| `bypass` | ✅ | ✅ | ✅ | Full autonomy, trusted tasks |
| `plan` | ✅ | ❌ | ❌ | Exploration, code review, research |
| `default` | ✅ | ✅ (auto) | ⚠️ auto-approve | Daily coding with guard |
| `accept-edits` | ✅ | ✅ | ⚠️ prompt | Safe editing, manual bash |

*5-layer bash guard runs in **all** modes — even bypass blocks `rm -rf /`.*

## Project Structure

```
lumen/
├── cmd/lumen/main.go              # CLI entry point
├── internal/
│   ├── agent/                     # Core engine (loop, coordinator, task, session, cache)
│   ├── checkpoint/                # Pre-edit snapshots + rewind
│   ├── command/                   # Slash commands
│   ├── config/                    # TOML config + .env loading
│   ├── control/                   # Transport-agnostic controller
│   ├── doctor/                    # Health checks
│   ├── evidence/                  # Tool-call receipt ledger
│   ├── fileutil/                  # File safety (binary, size, boundary, symlink)
│   ├── guard/                     # Bash command defense (5-layer)
│   ├── hook/                      # Lifecycle hooks
│   ├── jobs/                      # Background task manager
│   ├── lsp/                       # LSP client (gopls)
│   ├── memory/                    # Project memory (AGENTS.md)
│   ├── permission/                # 4-mode permission gate
│   ├── plugin/                    # MCP stdio JSON-RPC client
│   ├── provider/                  # Model backend (SSE + retry)
│   ├── skill/                     # 22 skills
│   ├── timeline/                  # Session timeline + change inbox
│   ├── tool/                      # Tool interface + registry
│   └── tui/                       # Bubble Tea multi-panel TUI
├── skills/                        # 22 Markdown skill files
├── docs/                          # Documentation
├── lumen.toml                     # Configuration
├── .env.example                   # API key template
└── go.mod
```

## Evaluation

Coding quality is measured, not asserted. `lumen eval` runs a fixed set of real tasks
(`evals/tasks/` — each a broken project + a deterministic check) end-to-end through the
configured model and reports **pass-rate / median steps / cost**:

```bash
lumen eval --list            # list tasks (no model needed)
lumen eval                   # run them through your configured model → pass-rate
make eval
```

It scores by compiling + running each task's tests, so it works against any model — cloud
or local (point `base_url` at LM Studio/Ollama and compare). A published baseline pass-rate
is pending a model run; the harness itself (loader/scorer/aggregator) is unit-tested in CI.

## Real-World Runs

Real DeepSeek API calls (not mocked). **These are cost/efficiency numbers — turns, tokens,
cache-hit — not a coding-quality benchmark** (that's `lumen eval`, above).

| Task | Turns | Tokens | Cache Hit |
|------|-------|--------|-----------|
| Simple greeting | 1 | 2,817 | 96% |
| Create multi_edit.go (new tool) | 9 | 10,718 | 87% |
| Review agent.go via run_skill | 20+ | 8,114 | 43% |
| Fix + build + test | 3 | 9,016 | 99% |
| Git tools + session resume | 2 | 14,000 | 97% |
| Heart drawing (python + JS + HTML) | 13 | 17,000 | 99% |

## C2D Author Toolchain (`lumen oasis`)

Beyond general coding, Lumen ships the official **author toolchain for the Oasis
compute-to-data (C2D) marketplace** — write a privacy-preserving algorithm, gate
it locally against the *exact* marketplace sandbox, and ship it. Buyers run it on
data they can't see and get an aggregates-only result plus a re-verifiable cert.

```bash
lumen oasis templates                    # stats · histogram · quantiles · correlation · groupby (k-anon) · linreg · logreg · dp-stats (ε-DP)
lumen oasis init myalgo --template stats # scaffold a complete, runnable algorithm
lumen oasis check .                       # run it in the real --network=none sandbox
lumen oasis verify .                      # source ⇄ provenance lockfile match
lumen oasis publish .                     # build → check → deploy + register
```

Verified end-to-end on a live marketplace: a generated algorithm produced a real
result certificate whose output re-hash and image digest match the author's
lockfile exactly. See **[the quickstart](docs/教程-用-lumen-oasis-写C2D算法.md)** and the
[integration proof](docs/记录-C2D作者闭环-真实环境整合证明.md). No general terminal agent
(Aider/Cursor/Claude Code) has this — it's Lumen's vertical edge.

## Claude Science Bridge (`lumen science`)

Lumen ships a **native Go** bridge for [Claude Science](https://claude.ai/science) — no Python
subprocess. Multi-profile switch, relay model picker, DSML shim, and config guards, plus
Lumen-only **5-ship MCP fleet**, **Research Brief**, and **Oasis** embed.
See [`docs/science/COMPARISON.md`](docs/science/COMPARISON.md).

```bash
lumen science start                    # proxy + sandbox + browser (one click)
lumen science gui                      # control panel (:18990) — profiles, relay, DSML
lumen science status                   # proxy / sandbox / cache hit rate
lumen science watch                    # live DeepSeek prefix-cache dashboard
lumen science doctor                   # read-only diagnostics
lumen science migrate [--force]        # import legacy bridge config
lumen science native verify --live     # 5-ship MCP fleet (PubMed/ChEMBL/GEO/Oasis/C2D)
lumen science brief "aspirin"          # 4-source Research Brief
lumen science research verify          # audit bundled MCP + skills + DB clients
lumen science config set-key deepseek sk-...
make science-full-verify               # all offline gates (tests, RM, native, desktop)
make goal-all-verify                   # goal evidence + science full-verify + make check
```

**Desktop (macOS)** — `desktop/lumen-science/` Tauri Acceptance app (`com.lumen.science.acceptance`);
embeds loopback GUI; quit stops proxy, keeps sandbox.

**Profiles** — named configs with upstream probe before switch; bad keys rejected (401/403);
relay templates support `/api/relay/models` model discovery in GUI.

**DSML shim** — `off` (default) / `detect` / `rewrite` for tool-use leak mitigation; persisted in config.

**Cache boost** — optional `cache_control: ephemeral` on system/tools blocks pushes DeepSeek
prefix-cache hit rates toward the high 90s on long Science sessions (`lumen science config
set-cache-boost on`).

**Research pack** — on first `start`, Lumen clones `bin`, `conda`, `runtime`, and `seed-assets`
from your real `~/.claude-science` install into an isolated APFS sandbox. Local `bio-tools` MCP
replaces Anthropic-hosted remote MCP servers (blocked at the proxy). Org workspaces and bundled
MCP auto-approve are seeded automatically.

**Ports** — proxy `18991`, sandbox `8990`, GUI `18990`. Real Science on `8765` is never touched.

**Stress-resistant design** — native Go proxy (no Python), shared GUI manager for coherent
start/stop, port-kill fallback when stopping orphaned listeners, config validation (reserved
ports, distinct proxy/sandbox), panic recovery + request size limits on the panel, SSE reconnect
with exponential backoff, and DeepSeek cache-boost for 90%+ prefix-cache hit rates on long
sessions.

Config: `~/.lumen/science/config.json` (mode `0600`). Keys also inherit from `lumen.toml`
providers when present.

## Roadmap

- [ ] **v0.3.0** — VSCode extension, websocket serve, config wizard
- [ ] **v0.4.0** — Browser remote (Chrome automation), vector memory (RAG)
- [ ] **v1.0.0** — Agent OS runtime, orchestrator, workflow engine

## Contributing

Bug reports, feature requests, and PRs welcome.
Open an issue before large changes to discuss direction.

## Releases

Tagged releases publish cross-platform binaries (macOS/Linux, amd64/arm64) via
[goreleaser](https://goreleaser.com). To cut one: bump `VERSION`, tag it
`vX.Y.Z` (must match `VERSION`), and push — the [Release workflow](.github/workflows/release.yml)
builds and uploads the archives + checksums. `lumen version` reports the build.

## License

[MIT](./LICENSE) © 2026 exergyleizhou-ux

---

*Built with Go · ~53k non-test LOC · 95 packages · 252 test files · 117 builtin tools · 30 model presets. Run `make facts` for live counts. Optional SQLite audit store: `LUMEN_SQLITE_STORE=on`.*
