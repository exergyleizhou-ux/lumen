# Lumen — Claude Code 4.8 后续变更清单（2026-06-14）

> 从 `21b7765`（你最后的 commit）到现在的全部变更。
> 项目位于 `/Users/lei/lumen/`。
> 运行方式：`GOTOOLCHAIN=local GOPROXY=https://goproxy.cn,direct go build -o bin/lumen ./cmd/lumen`
> API key 在 `.env` 中已设置。

---

## 一、你的 5 个 commit（保持不变）

```
6ae0392 fix: stop telling users to create reasonix.toml (it's lumen.toml)
e18140a fix(provider): surface real cache-hit tokens (was structurally always 0)
fb9f4a6 feat(skill): wire run_skill + task so the model can invoke skills
436d060 feat(agent): stamp call context so sub-agent events nest instead of vanishing
21b7765 fix(agent): tell the truth about compaction (sliding window, not summary)
```

## 二、变更文件清单

### 新增包（完整文件）

| 文件 | 行数 | 功能 |
|------|------|------|
| `internal/control/controller.go` | ~250 | **传输无关控制器** — New/Configure/Run/Plan/Chat。拥有 config 解析、provider 创建、tool/skill 装配、session 生命周期、permission 门控、checkpoint+jobs 基础设施。CLI main.go 从 318→170 行。 |
| `internal/agent/cache_shape.go` | 155 | **PrefixShape 缓存诊断** — 每轮计算 prefix hash（system prompt + tool schemas + first user），对比上一轮检测 churn，记录原因到 reasons()。cachedSchemas 只算一次。needsRepair 条件 sanitize。 |
| `internal/agent/compact.go` | 160 | **CompactWithModel** — 用便宜模型做真正的上下文摘要（替代纯滑动窗口）。buildCompactPrompt→Stream→summary→session.Compact。 |
| `internal/command/command.go` | 260 | **Slash 命令** — /status /cost /cache /rewind /replay /changes /skills /help。Registry with aliases。 |
| `internal/timeline/timeline.go` | 240 | **会话时间线 + 变更收件箱** — Recorder 追加 JSONL 到 `.lumen/timeline.jsonl`。RecordEvent 代理事件转换。LoadChanges 按路径分组修改文件。FormatTimeline/FormatChanges。 |
| `internal/doctor/doctor.go` | 210 | **健康检查** — /models API key 验证 + chat probe 回退 + workspace git 检测 + `lumen doctor` CLI 命令。 |
| `internal/tui/tui.go` | 400 | **Bubble Tea TUI** — 3Panel 布局：chatModel（user/assistant/reasoning/tool）、statusModel（provider/model/tokens/cache%）、inputModel（行编辑）。审批对话框（Y/N/Esc）。Slash 命令。Agent Asker 集成。 |
| `internal/fileutil/fileutil.go` | 285 | **文件安全层**（移植自 claw-code file_ops.rs）— IsBinaryFile（NUL 检测）、ValidateReadSize/WriteSize（10MB 限制）、ValidateWorkspaceBoundary、IsSymlinkEscape、SafeReadFile、SafeWriteFile、ResolvePath。LUMEN_WORKSPACE_ROOT 环境变量控制边界强制。 |
| `internal/guard/guard.go` | 244 | **bash 命令安全**（Ember 攻击驱动）— 5 层防御：外泄检测（curl -d @/wget --post-file/nc <）、敏感文件（.env/.ssh/etc/passwd/credentials）、侦查（ps/netstat/lsof/find -exec cat .env）、破坏（rm -rf/mkfs/dd/fork bomb）、编码走私（base64 -d|sh/xxd -r|sh/eval）。StripHiddenChars/ContainsHiddenChars 17 种不可见 Unicode。 |
| `internal/lsp/client.go` | 427 | **LSP 客户端**（移植自 claw-code lsp_client.rs）— stdio JSON-RPC：initialize/DidOpen/diagnostics/hover/definition/references/symbols。publishDiagnostics 通知处理。10s 超时。 |
| `internal/lsp/` | — | 空目录（client.go 已在上方） |
| `internal/hook/hook.go` | 140 | **生命周期钩子** — 5 个环境变量钩子（LUMEN_HOOK_PRE_TOOL/POST_TOOL/POST_LLM/SUBAGENT/PRE_COMPACT）。PreToolUse exit 2 阻断。线程安全。 |
| `internal/memory/memory.go` | 254 | **项目记忆** — Store 加载 REASONIX.md/AGENTS.md/CLAUDE.md（项目根 + 用户 home）。Prompt() 返回字节稳定的前缀。DurableStore（remember→.md 文件 + MEMORY.md 索引、forget→删除 + 索引更新）。 |
| `internal/tool/builtin/lsp_tools.go` | 265 | **4 个 LSP 内置工具** — lsp_diagnostics、lsp_definition、lsp_references、lsp_hover。共享 LSP 客户端（gopls/rust-analyzer/typescript-language-server 自动检测）。 |
| `internal/tool/builtin/multi_edit.go` | 130 | **multi_edit 工具**（DeepSeek 真实 API 运行编写）— path + edits 数组（old_string/new_string），按序执行。自注册 init()。Fileutil 安全层集成。Previewer 接口。 |
| `cmd/lumen/main_test.go` | 25 | **CLI 参数解析测试** — 6 个用例（plain/plan/mode/mid/both/mode-without-value）。 |
| `docs/05-验收角色与评分标准.md` | 142 | **FanBox QA 标准** — 5 个验收角色。第一轮评分 + 反馈。 |

### 修改的已有文件

| 文件 | 改动 |
|------|------|
| `cmd/lumen/main.go` | 318→170 行。使用 control.Controller。`--mode` 标志。parseRunArgs。headless 自动批准。`lumen doctor` + `lumen chat`（TUI）。 |
| `internal/agent/agent.go` | +150 行。Agent 安全网：finalAnswerReady + handleEmptyFinal + handleStreamRecovery。DefaultSystemPrompt。SanitizeToolPairing 条件调用（needsRepair）。cachedSchemas + cacheTracker。LastUsage/SessionCache/CacheReasons 公共访问器。SetSink。agent 输入时 StripHiddenChars。executeOne 注入 Evidence Ledger + Jobs Manager。truncateToolOutput 修复（第二返回值不再为空）。 |
| `internal/agent/coordinator.go` | +45 行。ShouldUsePlanner 启发式（跳过 explain/find/summarize，启用 implement/add/build/refactor/fix + 中文关键词）。 |
| `internal/permission/gate.go` | +12 行。CheckBash 对**所有模式**强制 bash 内容安全检查。导入 guard 包。 |
| `internal/tool/builtin/read_file.go` | 使用 fileutil.SafeReadFile（尺寸+二进制+边界检查）。 |
| `internal/tool/builtin/write_file.go` | 使用 fileutil.SafeWriteFile（尺寸+边界检查）。 |
| `internal/tool/builtin/edit_file.go` | 使用 fileutil.SafeReadFile + fileutil.SafeWriteFile。 |
| `internal/provider/openai/openai.go` | **重试逻辑** — streamWithRetry：指数退避（2s→4s→8s，最多 3 次）。429/503/5xx 可重试。401/403 不重试。 |
| `go.mod` | 添加 bubbletea + lipgloss + 依赖。Go 1.23。 |

### 新增测试文件

| 文件 | 测试数 | 覆盖 |
|------|--------|------|
| `internal/agent/agent_test.go` | 14 | executeOne 门禁链、Plan Mode、Storm Breaker、tool 分区 |
| `internal/agent/session_test.go` | 5 | Add/Snapshot/Compact/SystemPrompt |
| `internal/agent/integration_test.go` | 7 | 多轮工具调用、并行执行、Stor沐Breaker、maxSteps、Evidence 记录 |
| `internal/evidence/evidence_test.go` | 14 | Record/HasEvidence/VerifyEvidence（bash 命令+文件路径精确匹配）、并发 |
| `internal/checkpoint/checkpoint_test.go` | 6 | Save/Rewind/Clear/二次 Rewind |
| `internal/config/config_test.go` | 10 | TOML 加载、环境变量注入、技能名校验 |
| `internal/event/event_test.go` | 8 | Discard/FuncSink、类型 struct |
| `internal/diff/diff_test.go` | 3 | Change New/Removed/Binary |
| `internal/frontmatter/frontmatter_test.go` | 7 | YAML 解析、CRLF、空块、引号、小写 key |
| `internal/permission/gate_test.go` | 13 | 4 模式门禁矩阵 |
| `internal/jobs/manager_test.go` | 10 | Start/Kill/Wait/Concurrent 生命周期 |
| `internal/tool/tool_test.go` | 12 | Registry CRUD、Schemas 稳定性、MCP namespace |
| `internal/tool/builtin/builtin_test.go` | 24 | bash/read/write/edit/grep/glob/ls 实际文件操作 + complete_step 与 evidence 验证 |
| `internal/provider/provider_test.go` | 14 | SanitizeToolPairing、Canonicalize、Pricing、AuthError |
| `internal/provider/openai/openai_test.go` | 11 | mock HTTP server SSE 全路径 |
| `internal/skill/skill_test.go` | 11 | 磁盘加载、runAs/allowedTools 解析 |
| `internal/plugin/plugin_test.go` | 14 | JSON-RPC 格式、Manager 生命周期 |

---

## 三、架构变更

```
新增 14 个包：
  control/   → 传输无关控制器（CLI/TUI/HTTP 共享）
  command/   → Slash 命令系统
  timeline/  → 会话时间线 + 变更收件箱（FanBox 概念）
  doctor/    → 健康检查
  tui/       → Bubble Tea 交互界面
  fileutil/  → 文件操作安全层（claw-code 移植）
  guard/     → bash 命令安全（Ember 攻击驱动）
  lsp/       → LSP 客户端（claw-code 移植）
  hook/      → 生命周期钩子
  memory/    → 项目记忆
  填平空目录：hook/ memory/

Agent Loop 增强：
  ✅ 安全网：finalAnswerReady + handleEmptyFinal + handleStreamRecovery
  ✅ 条件 Sanitize（needsRepair）
  ✅ 缓存 Schema（cachedSchemas，只算一次）
  ✅ PrefixShape 诊断（cache_shape.go）
  ✅ ShouldUsePlanner 启发式
  ✅ CompactWithModel（真正的模型摘要）
  ✅ bash 5 层防御（所有模式强制）
  ✅ 零宽字符 StripHiddenChars
  ✅ Provider 指数退避重试（3 次）

新工具：
  ✅ lsp_diagnostics / lsp_definition / lsp_references / lsp_hover
  ✅ multi_edit（DeepSeek 真实 API 编写）

24 个测试文件，16/16 包通过。
```

---

## 四、生产级差距（还差什么）

| 差距 | 优先级 |
|------|--------|
| **集成测试**：agent/integration_test.go 已添加 7 个测试（多轮工具调用、并行执行、Storm Breaker、Evidence 记录）— ✅ 刚完成 | P0 |
| **LSP 工具实际运行验证**：lsp_tools.go 已注册但未用真实 gopls 验证 | P1 |
| **TUI 真实交互验证**：Bubble Tea TUI 编译通过但未在真实终端运行 | P1 |
| **provider/openai 重试逻辑测试**：streamWithRetry 已实现但未测试 429/503 场景 | P1 |
| **CompactWithModel 测试**：已实现但未验证（需要第二个 provider） | P2 |
| **Hook 系统测试**：hook.go 已实现但无测试 | P2 |
| **Memory Store 测试**：memory.go 已实现但无测试 | P2 |

---

## 五、给你（Claude Code）的工作

```bash
cd /Users/lei/lumen
export DEEPSEEK_API_KEY=sk-...
GOTOOLCHAIN=local go build -o bin/lumen ./cmd/lumen
GOTOOLCHAIN=local go test -count=1 -timeout=30s ./...

# 验证 LSP 工具
./bin/lumen run "用 lsp_diagnostics 检查 internal/agent/agent.go 有没有问题"

# 验证 TUI
./bin/lumen chat

# 验证 Doctor
./bin/lumen doctor

# 补充 P0/P1 差距
```

### 建议优先做的：

1. **跑 `lumen run` 验证 LSP 工具真实可用**（需要 gopls 在 PATH 中）
2. **给 provider 重试逻辑加 mock HTTP 测试**（429/503 场景）
3. **给 guard.go 加单元测试**（244 行，0 测试 — 所有安全关键路径）
4. **审阅 agent.go 集成变更**（cache_shape + 安全网 + 条件 sanitize — 确认没有回退你的修复）
5. **审阅 permission/gate.go** — 我在 Check() 开头加了 bash 内容检查，确认不影响你的设计意图
