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

Most coding agents trust the LLM unconditionally. **Lumen doesn't.** It puts you in control with
four permission modes, a 5-layer bash command guard, and file-system safety boundaries — while
still letting you switch between DeepSeek, Grok, OpenAI, and Ollama in a single session.

| | Reasonix | Claude Code | **Lumen** |
|---|---|---|---|
| Security guard | Basic | None | **5-layer bash + file safety** |
| Permission modes | Plan only | Plan + Auto | **4 modes: bypass / plan / default / accept-edits** |
| Multi-model | DeepSeek-focused | Anthropic only | **9 providers, 26 models** |
| Session replay | No | No | **Timeline + /replay + change inbox** |
| Sub-agents | Limited | Yes | **Whitelist + isolated sessions** |
| Single binary | ❌ (Node.js) | ❌ (Node.js) | **✅ 9.9 MB, no runtime** |

---

## Screenshots

**Banner + Model List** — 26 presets across 9 providers, color-coded by provider:

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
- 🤖 **Multi-Model** — DeepSeek (96-99% cache hit), Grok, OpenAI, Anthropic, Ollama, Gemini,
  Moonshot, Qwen, Zhipu, Mimo. 26 presets across 9 providers.
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
- 📦 **Single Binary** — 9.9 MB, zero runtime dependencies. `curl | sh` install.

---

## Quick Start

**Install a release binary** (no Go toolchain needed) — pick your platform from the
[latest release](https://github.com/exergyleizhou-ux/lumen/releases/latest):

```bash
# macOS arm64 example — swap the asset for your OS/arch
curl -L https://github.com/exergyleizhou-ux/lumen/releases/latest/download/lumen_1.0.0_darwin_arm64.tar.gz | tar xz
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

*For other providers (OpenAI, Anthropic, Grok, Ollama…), add more `[[providers]]` entries. See `internal/config/model_presets.go` for the full 26-model preset list. Prerequisites: Go 1.23+, optional `gopls` for LSP diagnostics.*

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

## Real-World Runs

Verified with real DeepSeek API calls (not mocked):

| Task | Turns | Tokens | Cache Hit |
|------|-------|--------|-----------|
| Simple greeting | 1 | 2,817 | 96% |
| Create multi_edit.go (new tool) | 9 | 10,718 | 87% |
| Review agent.go via run_skill | 20+ | 8,114 | 43% |
| Fix + build + test | 3 | 9,016 | 99% |
| Git tools + session resume | 2 | 14,000 | 97% |
| Heart drawing (python + JS + HTML) | 13 | 17,000 | 99% |

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

*Built with Go · ~38k lines · 54 packages · 120 tools · 26 models · 9 providers · zero runtime deps*
