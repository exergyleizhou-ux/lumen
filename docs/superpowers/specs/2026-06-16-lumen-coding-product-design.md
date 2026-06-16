# Lumen → 对标 Cursor / Claude Code / Codex 的终极方案

> 2026-06-16 · 终端 TUI 路线 · 分阶段(视觉 → 编程质量 → 广度) · Claude 架构+终审 / DeepSeek 执行机械任务

---

## 0. 诚实的现状(基于代码,不是报告)

不重复 AUDIT-REPORT 的说法。以下是我逐文件读过后的真实判断。

**已经很强(Claude-Code 级,别推倒重来):**
- `internal/agent/agent.go` — 真正的 streaming tool-loop:plan-mode 门控(不破前缀缓存)、storm-breaker 死循环检测、模型压缩+滑窗回退、evidence ledger、checkpoint/rewind、隐藏字符注入防御、并行只读工具、空回答/断流恢复守卫。这是产品的心脏,写得好。
- `internal/provider` + `openai` — OpenAI 兼容流式(SSE)、连接池、静默重试,跑在 DeepSeek 上。
- `internal/tool/builtin` — read/write/edit/multi_edit/glob/grep/bash/web_fetch/todo_write/ask 真接好了;另有 ~90 个扩展工具。
- 权限模式(bypass/plan/default/accept-edits)、SQLite 会话、checkpoint 快照、`lumen chat|run|doctor` 都能跑。二进制实测可运行。

**真正的差距(不是那 184 个引擎包):**
1. **视觉/交互 = 最大短板。** TUI 把 markdown **抹掉**(`stripMD`)而不是渲染;diff 是纯 `+/-` 无语法色;输入是裸 `bufio.Scanner`——无历史/上箭头、无多行、无斜杠菜单、无 @ 补全、无实时状态。`bubbletea`/`lipgloss` 在 go.mod 里却**没被 import**。
2. **编程可靠性。** 缺统一的"验证回路"(build/test/lint 自动跑并回喂)、大仓导航(符号/语义检索)、编辑质量保证(编辑后立即校验)。
3. **少量能力缺口** vs 三大产品:计划 UX、子代理体验、MCP 接入 UX、@ 文件提及、`/` 命令体系、会话恢复 UX。
4. **此前发现并已修复**:19 处重入锁死锁(全仓扫描+ `-race` 验证);`filewatcher` channel 生命周期死锁(待修);AUDIT-REPORT 多处夸大(待改诚实版)。

**结论:** Lumen 不是"又一个库堆",它已有一个像样的编程 agent 内核。要"对标",钱要花在**视觉、可靠性、产品化**上,而不是再加引擎包。

---

## 1. 北极星与衡量标准

**北极星:** 一个 DeepSeek 驱动、终端原生、观感与可靠性都对标 Claude Code/Codex 的编程 agent。

**可量化验收(每阶段结束跑):**
| 维度 | 指标 | 目标 |
|---|---|---|
| 视觉 | markdown/代码块/diff 渲染保真 | 富渲染、语法高亮、行内 diff |
| 交互 | 输入能力 | 历史↑↓、多行、`/`菜单、@文件补全、Esc-Esc 回退 |
| 可靠性 | 编辑后自动验证命中率 | 写代码后自动 build/test 并回喂 |
| 可靠性 | 全量测试 | `go test ./...` 全绿(当前 137 ok / 1 fail) |
| 广度 | 工具/能力对标矩阵 | 见 §5 |
| 工程 | `-race` + `go vet` | 干净 |

---

## 2. 形态决策(已定)

**终端 TUI**,在现有 Go 内核上做到世界级。不做 Cursor 式 GUI(工作量数倍且需新桥接层)。
渲染与输入层的架构要**与事件 sink 解耦**,这样既能服务当前 sink,也能平滑升级到 bubbletea 全程序。

---

## 3. 三阶段路线

### 阶段一:视觉 / 交互(先做)
让 Lumen "看起来和用起来" 像 Claude Code。

- **1a 富渲染器 `internal/render`** 〔Claude 架构〕
  markdown→ANSI(标题/粗斜/列表/引用/表格/链接)+ 代码围栏**语法高亮**;流式安全(增量喂入不破样式)。替换 `stripMD`。
  - DeepSeek 子任务:各语言高亮 token 表(go/js/ts/py/rust/json/bash/sql)、表格对齐、大量 golden 测试夹具。
- **1b 语法高亮 diff** 〔Claude〕
  升级 `renderFileDiff`:+/- 颜色、行内 token 级高亮、`▏` 行号、hunk 折叠。
  - DeepSeek 子任务:颜色常量、宽度截断、测试夹具。
- **1c 可交互输入 `internal/lineedit`** 〔Claude〕
  raw 模式行编辑:历史↑↓持久化、多行(``` 或续行)、`/` 命令菜单+补全、`@` 文件路径补全、Ctrl-C/Ctrl-D 语义、Esc-Esc 回退提示。
  - DeepSeek 子任务:`/` 命令注册表数据、`@` 路径扫描的 glob 辅助、补全排序、按键名常量。
- **1d 实时状态行** 〔Claude〕
  流式 spinner、当前工具、step、elapsed、token/cache/cost 实时表;TurnDone footer 美化。
  - DeepSeek 子任务:spinner 帧、数字千分位/人类可读格式化、颜色主题表。

**阶段一验收:** 一次真实会话里 markdown 富渲染、代码高亮、diff 高亮、↑历史、`/`菜单、`@`补全、实时状态全部可见可用。

### 阶段二:编程效果 / 可靠性
让它"改得对、能自证"。

- **2a 验证回路 `internal/verify`** 〔Claude〕
  写/改代码后自动探测项目类型并跑 build/test/lint(Go 优先:`go build`/`go vet`/`go test`),失败摘要回喂给模型形成自修复闭环。接入 evidence ledger。
  - DeepSeek 子任务:各语言命令探测表(package.json/go.mod/Cargo.toml/pyproject)、输出摘要正则、夹具。
- **2b 大仓导航** 〔Claude〕
  把已有 `codemap`/`lsp_real`/`grep` 升级成 agent 友好的"符号检索 / 调用图 / 定义跳转"工具组,接进工具注册表;ripgrep 风格快速检索。
  - DeepSeek 子任务:工具 schema、参数校验、错误信息、单测。
- **2c 编辑质量护栏** 〔Claude〕
  edit_file 后即时校验(语法/格式),失败即回滚并报错;multi_edit 原子性;大文件分块编辑。
  - DeepSeek 子任务:格式化器调用封装、回滚测试夹具。
- **2d 子代理 / 计划 UX** 〔Claude〕
  完善 `task`(子代理)与 plan-mode 的产出与审批展示。

**阶段二验收:** 给一个真实 bug,Lumen 能定位→改→自动跑测试→看到失败→自修复→最终全绿;`go test ./...` 全绿(含修掉 filewatcher)。

### 阶段三:能力广度 / 产品化
补齐与三大产品的对标缺口。

- **3a `/` 命令体系**(/init /compact /diff /undo /model /mcp /cost /resume…)〔Claude 框架 + DeepSeek 各命令实现〕
- **3b MCP 接入 UX**:基于已有 `mcp_real` 做 `lumen mcp add/list`,工具热注册。〔Claude〕
- **3c 会话恢复 UX**:`/resume`、跨会话记忆。〔Claude + DeepSeek〕
- **3d 配置 & 安装**:`lumen setup` 向导、多 provider 切换(DeepSeek/OpenAI/Anthropic)、模型成本表。〔DeepSeek 为主〕
- **3e 文档诚实化**:重写 AUDIT-REPORT/README 为与代码一致的诚实版,补对标矩阵。〔Claude〕

**阶段三验收:** §5 对标矩阵齐平;文档不再夸大。

---

## 4. 分工协议(Claude / DeepSeek)

- **Claude(我):** 架构、接口设计、并发/状态机/渲染算法等难点、所有 PR 终审、`-race`/集成验证。
- **DeepSeek:** 机械重复——数据表、token 高亮表、格式化封装、单测夹具、各语言命令探测、`/`命令的样板实现。
- **机制:** 每个 DeepSeek 任务我产出**自包含任务卡**(目标 / 输入文件 / 输出文件 / 接口签名 / 验收命令 / 不要碰什么),你喂给 DeepSeek;回来后我终审 + 跑测试 + 合并。任务卡集中放 `docs/tasks/`。

---

## 5. 对标矩阵(目标态)

| 能力 | Lumen 现状 | Claude Code | Cursor | Codex | 目标阶段 |
|---|---|---|---|---|---|
| streaming tool-loop | ✅ 强 | ✅ | ✅ | ✅ | — |
| plan mode | ✅ | ✅ | ⚠️ | ⚠️ | — |
| 富 markdown 渲染 | ❌ 抹掉 | ✅ | ✅ | ✅ | 1a |
| 语法高亮代码/diff | ❌ | ✅ | ✅ | ✅ | 1a/1b |
| 交互输入(历史/多行//菜单/@) | ❌ | ✅ | ✅ | ✅ | 1c |
| 实时状态/成本 | ⚠️ footer | ✅ | ✅ | ✅ | 1d |
| 自动验证回路 | ❌ | ✅ | ✅ | ✅ | 2a |
| 大仓符号导航 | ⚠️ 散件 | ✅ | ✅ | ✅ | 2b |
| 子代理 | ✅ task | ✅ | ⚠️ | ⚠️ | 2d |
| `/` 命令体系 | ⚠️ 少 | ✅ | ✅ | ✅ | 3a |
| MCP | ✅ 客户端 | ✅ | ⚠️ | ❌ | 3b |
| checkpoint/undo | ✅ | ✅ | ✅ | ⚠️ | — |

---

## 6. 风险与原则
- **不破前缀缓存**:任何改动保持 system prompt + 工具 schema 跨轮字节稳定(DeepSeek 缓存依赖)。渲染/输入层改动不得进入发往模型的消息。
- **渲染与 sink 解耦**:`render` 是纯函数库,便于测试与未来 bubbletea 化。
- **TDD + golden 测试**:渲染/高亮/diff 全部 golden 夹具;并发件 `-race`。
- **诚实**:文档与代码一致,不再制造"测试通过造成的虚假安全感"。
