# Lumen

> **「你是我绿洲里的光」**
>
> Lumen 希望成为那束光。无论被永无休止的熵潮重塑多少次，我都会在代码的荒原里为你守候，
> 让你的每一次规划与生长的心跳，都穿过那片混乱与荒芜。

A multi-model coding agent for your terminal — built in Go, single binary.

## Features

- 🤖 **Multi-model**: DeepSeek, Grok, OpenAI, Ollama — OpenAI-compatible API
- 📋 **Plan Mode**: Read-only exploration → plan → user approval → execute (cache-safe)
- 🔀 **Coordinator**: Dual-model Planner+Executor with separate cache-stable sessions
- 🧵 **Subagents**: `task` tool spawns isolated sub-agents with tool whitelists
- 📚 **22 Skills**: explore, review, bug-hunt, security-review, test, benchmark, etc.
- ⚡ **DeepSeek Optimized**: Prefix-cache stable — 96-99% cache hit rate in real runs
- 🛡️ **5-Layer Defense**: Bash command guard — exfiltration, sensitive files, recon, destructive, encoded smuggling
- 🔒 **File Safety**: Binary detection, size limits (10MB), workspace boundary, symlink-escape detection
- 📡 **MCP Ready**: Tool registry with `mcp__` namespace support. JSON-RPC stdio client.
- 🔍 **LSP Integration**: Diagnostics, hover, definition, references — gopls auto-detected
- 🕐 **Session Timeline**: Replay agent actions, change inbox per session (`/replay`, `/changes`)
- 🩺 **Health Check**: `lumen doctor` — API key validation, model reachability, workspace status
- 🖥️ **TUI**: Bubble Tea interactive terminal (chat, status bar, approval dialogs)
- 🏗️ **Transport-Agnostic**: Controller powers CLI, TUI, and future HTTP/SSE from one code path
- 🔁 **Retry Logic**: Exponential backoff (429/503/5xx, up to 3 retries)
- 💬 **Slash Commands**: `/status`, `/cost`, `/cache`, `/rewind`, `/replay`, `/changes`, `/help`

## Quick Start

```bash
# Clone
git clone https://github.com/yourname/lumen.git
cd lumen

# Build (Go 1.23+)
go build -o bin/lumen ./cmd/lumen

# Configure
cp .env.example .env   # add your API keys
cp lumen.toml .         # or edit to match your provider

# Run
export DEEPSEEK_API_KEY=sk-...
./bin/lumen doctor       # verify everything works
./bin/lumen run "explain this project"
./bin/lumen run --plan "add user authentication"
./bin/lumen chat          # interactive TUI
```

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
              ├── 5. partitionToolCalls (read-only∥ | writers serial)
              ├── 6. executeOne (PlanMode → Permission → Guard → PreEdit → Execute → Evidence)
              ├── 7. Storm Breaker (3rd identical failure → redirect)
              └── 8. feed results → loop
              │
              ▼
         Event Sink → TUI / Headless / Timeline
```

## Project Structure

```
lumen/
├── cmd/lumen/main.go              # CLI entry point
├── internal/
│   ├── agent/                     # Core engine (loop, coordinator, task, session, cache)
│   ├── checkpoint/                # Pre-edit snapshots + rewind
│   ├── command/                   # Slash commands (/status, /cost, /cache, etc.)
│   ├── config/                    # TOML config + .env loading
│   ├── control/                   # Transport-agnostic controller
│   ├── doctor/                    # Health checks
│   ├── evidence/                  # Tool-call receipt ledger
│   ├── fileutil/                  # File safety layer (binary, size, boundary, symlink)
│   ├── guard/                     # Bash command defense (5-layer)
│   ├── hook/                      # Lifecycle hooks (PreToolUse, PostToolUse, etc.)
│   ├── jobs/                      # Background task manager
│   ├── lsp/                       # LSP client (diagnostics, hover, definition, references)
│   ├── memory/                    # Project memory (AGENTS.md + remember/forget)
│   ├── permission/                # 4-mode permission gate
│   ├── plugin/                    # MCP stdio JSON-RPC client
│   ├── provider/                  # Model backend (OpenAI-compatible SSE + retry)
│   ├── skill/                     # Skill system (22 built-in + filesystem discovery)
│   ├── timeline/                  # Session timeline + change inbox
│   ├── tool/                      # Tool interface + registry + 14 built-in tools
│   └── tui/                       # Bubble Tea terminal UI
├── skills/                        # 22 Markdown skill files
├── docs/                          # Documentation
├── lumen.toml                     # Configuration
├── .env.example                   # API key template
└── go.mod
```

## Real-World Runs

Lumen has been verified with **real DeepSeek API calls**, not just mocked tests:

| Task | Turns | Tokens | Cache |
|------|-------|--------|-------|
| Simple greeting | 1 | 2,817 | 96% |
| Create multi_edit.go (new tool) | 9 | 10,718 | 87% |
| Review agent.go via run_skill | 20+ | 8,114 | 43% |
| Fix + build + test | 3 | 9,016 | 99% |

## License

MIT
