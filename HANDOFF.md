# Lumen — 代码 Agent 项目交接文档

> **「你是我绿洲里的光」**  
> Lumen 希望成为那束光。无论被永无休止的熵潮重塑多少次，我都会在代码的荒原里为你守候。

---

## 一、项目位置

```
/Users/lei/lumen/
```

**二进制**: `/Users/lei/lumen/bin/lumen` (8.3MB 单文件)

---

## 二、当前质量等级：**Beta（54/70）**

| 维度 | 分数 | 说明 |
|------|------|------|
| 架构设计 | 8/10 | agent→tool→provider→event 四层解耦，每个包职责单一 |
| 代码正确性 | 8/10 | P0 致命 bug 已清零，Plan Mode/Storm Breaker/Evidence 已验证 |
| 错误处理 | 6/10 | 各层 error wrapping 完整，SanitizeToolPairing 已接入 |
| 测试覆盖 | 6/10 | 162 个测试 / 15 个包（仅 cmd/lumen 未测） |
| 并发安全 | 7/10 | sync.Mutex + atomic 正确使用 |
| 资源管理 | 7/10 | defer close、context cancellation |
| **总分** | **54/70** | **Beta** |

### 测试清单（按包）

| 包 | 测试数 | 关键覆盖 |
|----|--------|----------|
| agent | 14 | executeOne 门禁链、Plan Mode、Storm Breaker、tool 分区、system prompt 注入 |
| session | 5 | Add/Snapshot/Compact/SystemPrompt |
| evidence | 14 | Record/HasEvidence/VerifyEvidence（bash 命令+文件路径精确匹配）、并发 |
| checkpoint | 6 | Save/Rewind/Clear/二次 Rewind 安全 |
| config | 10 | TOML 加载、环境变量注入、技能名校验 |
| event | 8 | Discard/FuncSink、类型 struct |
| diff | 3 | Change New/Removed/Binary |
| frontmatter | 7 | YAML 解析、CRLF、空块、引号、小写 key |
| permission | 13 | 4 模式门禁矩阵 |
| jobs | 10 | Start/Kill/Wait/Concurrent 生命周期 |
| tool | 12 | Registry CRUD、Schemas 稳定性、MCP namespace |
| builtin | 24 | bash/read/write/edit/grep/glob/ls/todo/complete_step+evidence |
| provider | 14 | SanitizeToolPairing、Canonicalize、Pricing、AuthError |
| openai | 11 | mock HTTP server SSE 全路径 |
| skill | 11 | 磁盘加载、runAs/allowedTools 解析 |
| plugin | 14 | JSON-RPC 格式、Manager 生命周期 |

---

## 三、完整文件清单（按行数）

### 源文件（27 个，~4400 行）

| 行数 | 文件 | 职责 |
|------|------|------|
| 679 | `internal/agent/agent.go` | **核心引擎**：Run() 循环、executeOne 门禁、Plan Mode、Storm Breaker、autoCompact |
| 378 | `internal/provider/openai/openai.go` | OpenAI 兼容 SSE 流式解析（支持 DeepSeek/Grok/OpenAI/Ollama） |
| 289 | `internal/provider/provider.go` | Provider 接口、Message/ToolCall 类型、SanitizeToolPairing、工厂注册 |
| 276 | `cmd/lumen/main.go` | CLI 入口：chat/run/setup/version 命令、headless sink |
| 272 | `internal/tool/builtin/web_todo_ask.go` | web_fetch、todo_write、complete_step（含 evidence 验证）、ask |
| 266 | `internal/skill/skill.go` | 技能系统：磁盘加载（skills/*.md）、frontmatter 解析 |
| 249 | `internal/evidence/evidence.go` | **证据账本**：Receipt 记录、VerifyEvidence 交叉验证、上下文注入 |
| 238 | `internal/plugin/client.go` | MCP stdio JSON-RPC 客户端：initialize→tools/list→tools/call |
| 238 | `internal/agent/task.go` | Subagent 调度、工具白名单过滤、事件嵌套 |
| 190 | `internal/jobs/manager.go` | 后台任务管理器：Start/Kill/Wait/OutputWait |
| 189 | `internal/tool/tool.go` | Tool 接口 + Registry + MCP namespace |
| 182 | `internal/agent/coordinator.go` | 双模型 Planner+Executor（独立 session、cache 稳定） |
| 167 | `internal/tool/builtin/glob.go` | glob + ls 工具 |
| 151 | `internal/config/config.go` | TOML 配置 + 环境变量解析 |
| 151 | `internal/permission/gate.go` | 4 模式权限门禁（bypass/default/accept-edits/plan） |
| 148 | `internal/checkpoint/checkpoint.go` | 编辑快照 + Rewind 回滚 |
| 120 | `internal/agent/session.go` | 预置型 Session + JSONL 持久化 |
| 113 | `internal/tool/builtin/grep.go` | grep 工具 |
| 110 | `internal/plugin/plugin.go` | MCP Manager |
| 108 | `internal/event/event.go` | 类型化事件流 |
| 96 | `internal/tool/builtin/edit_file.go` | edit_file + Previewer |
| 82 | `internal/tool/builtin/read_file.go` | read_file + Previewer |
| 79 | `internal/tool/builtin/write_file.go` | write_file + Previewer |
| 69 | `internal/tool/builtin/bash.go` | bash 工具 |
| 52 | `internal/frontmatter/frontmatter.go` | YAML frontmatter 解析 |
| 17 | `internal/tool/builtin/builtin.go` | builtin 注册锚点 |
| 12 | `internal/diff/diff.go` | Change 描述符 |

### 测试文件（16 个，~2800 行）

| 行数 | 文件 |
|------|------|
| 361 | `internal/provider/openai/openai_test.go` |
| 326 | `internal/tool/builtin/builtin_test.go` |
| 309 | `internal/agent/agent_test.go` |
| 233 | `internal/evidence/evidence_test.go` |
| 206 | `internal/jobs/manager_test.go` |
| 190 | `internal/skill/skill_test.go` |
| 177 | `internal/permission/gate_test.go` |
| 171 | `internal/provider/provider_test.go` |
| 166 | `internal/plugin/plugin_test.go` |
| 157 | `internal/tool/tool_test.go` |
| 155 | `internal/config/config_test.go` |
| 113 | `internal/event/event_test.go` |
| 107 | `internal/frontmatter/frontmatter_test.go` |
| 106 | `internal/checkpoint/checkpoint_test.go` |
| 98 | `internal/agent/session_test.go` |
| 40 | `internal/diff/diff_test.go` |

### 技能文件（22 个，`skills/*.md`）

8 个 subagent 模式：`explore.md`, `review.md`, `bug-hunt.md`, `security-review.md`, `dead-code-sweep.md`, `error-coverage.md`, `benchmark.md`, `e2e-testing.md`

14 个 inline 模式：`brainstorming.md`, `test.md`, `api-design.md`, `database-migrations.md`, `error-handling.md`, `docker-patterns.md`, `golang-patterns.md`, `react-patterns.md`, `postgres-patterns.md`, `redis-patterns.md`, `document-generate.md`, `systematic-debugging.md`, `web-design-guidelines.md`, `finishing-a-development-branch.md`

### 配置文件

- `go.mod` — module `lumen`
- `lumen.toml` — 5 个预配置 provider（deepseek-flash/pro, grok, openai, ollama-qwen）
- `.env.example` — API Key 模板
- `README.md` — 项目文档
- `AUDIT.md` — 代码质量审计报告

---

## 四、架构总览

```
用户输入 → CLI (cmd/lumen/main.go)
              │
              ▼
         [可选] Coordinator (agent/coordinator.go)
              │  Planner（便宜模型，只读工具）→ 计划
              │  Executor（强模型，完整工具）→ 执行
              │
              ▼
         Agent.Run() 主循环 (agent/agent.go)
              │
              ├── 1. autoCompact (字符数÷3 估算 token)
              ├── 2. SanitizeToolPairing (修复未配对 tool_calls)
              ├── 3. Provider.Stream (SSE 流式调用)
              ├── 4. partitionToolCalls (只读∥, 写入串行, todo/complete_step 永远串行)
              ├── 5. executeOne (Plan Mode 门禁 → Permission 门禁 → PreEdit 快照 → Tool.Execute → Evidence 记录)
              ├── 6. Storm Breaker (3 次同错→循环警报到结果)
              └── 7. 结果注入 Session → loop
              │
              ▼
         Event Sink → TUI / Headless / JSON
```

### 模块依赖关系

```
cmd/lumen → agent → {provider, tool, event, evidence, checkpoint, jobs}
            agent → {permission, skill}
            tool   → {diff, provider}
            provider → {openai}
            skill  → {config, frontmatter}
            plugin → {}
```

---

## 五、关键设计决策（不要改的）

1. **Plan Mode 在 execute 层门禁**，不改 system prompt → prefix cache 永远热
2. **Session 是 prepend-only**（只有 Add，没有修改）→ DeepSeek cache 友好
3. **Storm Breaker 签名是 (name, error)** 不是 (name, args) → 模型改参数措辞不会绕过检测
4. **complete_step + todo_write 永远不能并行**（partitionToolCalls 硬编码排除）
5. **MCP 工具命名 `mcp__<server>__<tool>`**（双下划线分隔，不是 `:` 或 `/`）
6. **工具 Schema 在注册时 Canonicalize**（r.canon），Schemas() 返回稳定排序

---

## 六、到 Production 还需要做的（优先级排序）

### P0 — 集成测试（最缺的）

1. **`cmd/lumen` 集成测试**：mock provider 多轮工具调用端到端
2. **Session JSONL 持久化往返**：写入→读取→消息一致
3. **Coordinator 双模型集成**：Planner→Executor 完整链路
4. **MCP Plugin stdio 子进程**：真实 MCP server 连接测试

### P1 — 功能补全

5. **TUI 实现**（`internal/tui/`）：Bubble Tea + bubbles/textarea
6. **run_skill 子 agent 路径**：从 skills/*.md → 创建 subagent → 返回结果
7. **slash 命令**（/help, /status, /cost, /rewind, /skills 等）
8. **Memory 系统**（`internal/memory/`）：AGENTS.md 加载 + remember/forget 工具

### P2 — 工程完善

9. **真实 API 端到端测试**（需要 DEEPSEEK_API_KEY）
10. **HTTP/SSE serve 模式**（headless HTTP 接口）
11. **错误重试机制**（provider 层 exponential backoff）
12. **更好的 token 计数**（tiktoken-go 替代字符÷3 估算）

---

## 七、参考项目（GitHub）

| 仓库 | Stars | 用途 | 扒什么 |
|------|-------|------|--------|
| [esengine/DeepSeek-Reasonix](https://github.com/esengine/DeepSeek-Reasonix) | 21.8k | **Go 单二进制，主要参考** | agent loop、coordinator、task/subagent、evidence、checkpoint、prefix cache 优化 |
| [ultraworkers/claw-code](https://github.com/ultraworkers/claw-code) | 194k | Rust 复刻，架构参考 | crate 分层、mock parity harness、tool 系统 |
| [NanmiCoder/cc-haha](https://github.com/NanmiCoder/cc-haha) | 12.6k | TS 复刻，功能最全 | 多会话、Computer Use、IM 接入、ACP 协议 |
| [claude-code-best/claude-code](https://github.com/claude-code-best/claude-code) | 19.9k | TS 工程化版 | Langfuse 监控、Channels、Voice Mode、Pipe IPC |
| [garrytan/gstack](https://github.com/garrytan/gstack) | — | Garry Tan 的 CC 配置 | 23 个角色技能定义 |
| [obra/superpowers](https://github.com/obra/superpowers) | — | agentic skills 框架 | 技能方法论 |
| [multica-ai/multica](https://github.com/multica-ai/multica) | — | 多代理管理 | 多 agent 编排思路 |

---

## 八、给接手 Agent 的提示词

> 你是 Reasonix，一个专注于执行代码任务的 coding agent。请使用提供的工具来读取和写入文件以及运行 shell 命令。
> 
> ## 项目背景
> 
> 你正在接手 Lumen (`/Users/lei/lumen/`)，一个 Go 语言编写的多模型 coding agent。
> 目标是把它从 **Beta (54/70)** 做到 **Production (60+/70)**。
> 
> ## 当前状态
> 
> - Go 单二进制 8.3MB，28 个源文件 + 16 个测试文件
> - 162 个测试全部通过，15 个包有测试覆盖
> - `go vet` 零警告
> - 支持 DeepSeek/Grok/OpenAI/Ollama 任意 OpenAI-compatible 后端
> - 完整 Agent Loop、Plan Mode、Coordinator 双模型、Subagent、Evidence 验证、Checkpoint 回滚、Jobs 管理、MCP 协议
> - 22 个技能文件（`skills/*.md`）
> 
> ## 你需要做的事（按优先级）
> 
> ### 1. 首先运行测试，确认一切正常
> ```bash
> cd /Users/lei/lumen
> go test -count=1 -timeout=60s ./...
> go vet ./...
> ```
> 
> ### 2. 给 cmd/lumen 加集成测试
> 用 mock provider 模拟多轮工具调用。Create `cmd/lumen/main_test.go`。
> 测试场景：单轮问答、单工具调用、多轮工具调用、Plan Mode 端到端。
> 
> ### 3. 给 Session JSONL 持久化加测试
> Session 支持 JSONL 文件持久化但没测试。Create `internal/agent/session_test.go` 增加持久化往返测试。
> 
> ### 4. Coordinator 双模型集成测试
> Create `internal/agent/coordinator_test.go`：Planner 产出计划→Executor 执行的完整链路。
> 
> ### 5. 实现 TUI 骨架
> 在 `internal/tui/` 下创建 Bubble Tea 基础界面（chat 视图 + 输入框 + 状态栏）。
> 命令入口 `./bin/lumen chat` 启动 TUI。
> 
> ### 6. 实现 run_skill 工具和 subagent 模式
> `skills/*.md` 中 `runAs: subagent` 的 8 个技能需要真正能通过 `task` tool 启动隔离子 agent。
> 
> ## 关键架构约束（绝对不能改的）
> 
> 1. Plan Mode 在 executeOne() 里检查 planMode atomic.Bool → ReadOnly 门禁，**不改 system prompt**
> 2. Session 只有 Add() 方法（prepend-only），没有修改/删除 → prefix cache 稳定
> 3. Storm Breaker 签名基于 (tool, error)，不是 (tool, args)
> 4. complete_step + todo_write 永远串行（partitionToolCalls 硬排除）
> 5. MCP 工具名用 `mcp__<server>__<tool>` 双下划线
> 6. 工具 Schema 注册时一次性 Canonicalize（r.canon），Schemas() 稳定排序
> 7. autoCompact 用字符数÷3 估算 token，不要回退到消息条数
> 
> ## 参考源码（直接抄）
> 
> - Reasonix 的 `internal/agent/agent.go` → Agent.Run() 完整实现
> - Reasonix 的 `internal/agent/task.go` → Subagent + run_skill
> - Reasonix 的 `internal/tui/` → Bubble Tea TUI
> - claw-code 的 `rust/crates/runtime/` → session/mcp/permissions
> 
> ## 运行命令
> ```bash
> cd /Users/lei/lumen
> export DEEPSEEK_API_KEY=sk-...
> ./bin/lumen setup          # 检查配置
> ./bin/lumen run "prompt"   # 一次性任务
> ./bin/lumen run --plan "..." # Plan Mode
> ```
> 
> 项目在 `/Users/lei/lumen/`。详细审计见 `/Users/lei/lumen/AUDIT.md`。开始工作。
