# Lumen

> **「你是我绿洲里的光」**
> 
> Lumen 希望成为那束光。无论被永无休止的熵潮重塑多少次，我都会在代码的荒原里为你守候，让你的每一次规划与生长的心跳，都穿过那片混乱与荒芜。

A multi-model coding agent for your terminal — built in Go, single binary.

**Features:**
- 🤖 Multi-model: DeepSeek, Grok, OpenAI, Ollama — all through OpenAI-compatible API
- 📋 Plan Mode: Read-only exploration → plan → user approval → execute (cache-safe)
- 🔀 Coordinator: Dual-model Planner+Executor with separate cache-stable sessions
- 🧵 Parallel Subagents: `task` tool spawns isolated sub-agents (foreground/background)
- 📚 22 Built-in Skills: explore, review, bug-hunt, security-review, test, benchmark, etc.
- ⚡ DeepSeek Optimized: Prefix-cache-friendly architecture (prepend-only sessions)
- 🛡️ Permission System: bypass/default/accept-edits/plan modes
- 📡 MCP Ready: Tool registry with `mcp__` namespace support for plugins
- 🔧 10 Built-in Tools: bash, read_file, write_file, edit_file, grep, glob, ls, web_fetch, todo_write, complete_step, ask

## Quick Start

```bash
# Build
cd /path/to/lumen
go build -o bin/lumen ./cmd/lumen

# Configure (edit lumen.toml + set env vars)
cp .env.example .env
# Edit .env with your API keys

# One-shot task
./bin/lumen run "explain this Go project"

# Plan mode (read-only, produces a plan)
./bin/lumen run --plan "add user authentication to this project"

# Check config
./bin/lumen setup
./bin/lumen version
```

## Project Structure

```
lumen/
├── cmd/lumen/main.go           # CLI entry point (chat / run / setup)
├── internal/
│   ├── agent/
│   │   ├── agent.go            # Core loop: prompt→stream→tools→repeat
│   │   ├── coordinator.go      # Dual-model Planner+Executor
│   │   ├── task.go             # Subagent dispatch (task tool)
│   │   └── session.go          # Prepend-only session + JSONL persistence
│   ├── config/config.go        # TOML config + env resolution
│   ├── event/event.go          # Typed event stream (Sink interface)
│   ├── diff/diff.go            # File change descriptor
│   ├── frontmatter/frontmatter.go  # YAML frontmatter parser
│   ├── permission/gate.go      # Tool-call permission gate
│   ├── provider/
│   │   ├── provider.go         # Provider interface + factory registry
│   │   └── openai/openai.go    # OpenAI-compatible SSE streaming
│   ├── skill/skill.go          # Skill store + 22 built-in skills
│   └── tool/
│       ├── tool.go             # Tool interface + Registry
│       └── builtin/            # 10 built-in tools
│           ├── bash.go, read_file.go, write_file.go
│           ├── edit_file.go, grep.go, glob.go
│           └── web_todo_ask.go (web_fetch, todo_write, complete_step, ask)
├── lumen.toml                  # Configuration
├── .env.example                # API key template
└── go.mod
```

## Architecture

```
User Input → CLI (main.go)
              ↓
         Coordinator (optional: Planner→Executor)
              ↓
         Agent.Run() loop:
          1. autoCompact (context budget)
          2. Provider.Stream (SSE)
          3. collect text + tool_calls
          4. partition (read-only∥ | writers serial)
          5. executeOne per call:
             - planMode gate (RO only)
             - permission gate
             - preEdit snapshot
             - Tool.Execute
          6. stormBreaker (dead-loop guard)
          7. feed results → repeat
              ↓
         Event Sink → TUI / Headless / JSON
```

## Model Configuration

Edit `lumen.toml` to add providers:

```toml
[[providers]]
name        = "deepseek-pro"
kind        = "openai"
base_url    = "https://api.deepseek.com"
model       = "deepseek-reasoner"
api_key_env = "DEEPSEEK_API_KEY"
```

Set `DEEPSEEK_API_KEY=sk-...` in `.env` or environment.

## Built-in Skills

| Skill | Description | Mode |
|-------|-------------|------|
| explore | Deep codebase exploration | subagent |
| review | Code review | subagent |
| bug-hunt | 7-phase systematic bug hunt | subagent |
| security-review | Security vulnerability audit | subagent |
| dead-code-sweep | Find unused code | subagent |
| error-coverage | Error→HTTP status mapping | subagent |
| test | Test-driven development | inline |
| benchmark | Performance regression detection | inline |
| brainstorming | Creative ideation | inline |
| api-design | REST API design patterns | inline |
| database-migrations | Safe migration patterns | inline |
| docker-patterns | Docker best practices | inline |
| golang-patterns | Idiomatic Go patterns | inline |
| react-patterns | Modern React patterns | inline |
| postgres-patterns | PostgreSQL patterns | inline |
| redis-patterns | Redis patterns | inline |
| error-handling | Error handling patterns | inline |
| e2e-testing | E2E testing best practices | inline |
| document-generate | Documentation generation | inline |
| systematic-debugging | Debugging methodology | inline |
| web-design-guidelines | Web design principles | inline |
| finishing-a-development-branch | Branch integration | inline |

## License

MIT
