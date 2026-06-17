# Lumen 全面评估报告 — 与 Reasonix / Claude Code 对比

## 一、规模

| 指标 | 数值 |
|------|------|
| Go 代码行数 | 98,793 |
| 内部包数 | 184 |
| Agent 可调用工具 | **97 个** |
| 已接线为工具的包 | **30 个** |
| 未接线的包 | **154 个**（其中大部分是基础设施，内部使用） |

## 二、当前状态总评

### 已完全就绪，agent 可用的能力

| 类别 | 工具 | 状态 |
|------|------|------|
| 文件操作 | read_file, write_file, edit_file, glob, grep, ls, notebook_edit, multi_edit, delete_range | ✅ |
| Shell | bash (含后台/超时) | ✅ |
| LSP | lsp_diagnostics, lsp_definition, lsp_hover, lsp_references | ✅ |
| GitHub | create_pr, list_prs, ci_status, create_issue, search_code | ✅ |
| 安全加密 | seal_data, unseal_data, sign_attestation, verify_attestation, hash_chain_append, hash_chain_verify, audit_query | ✅ |
| 图论算法 | graph_bfs, graph_dijkstra, graph_topological_sort, graph_scc | ✅ |
| 拓扑分析 | topology_build_graph, detect_cycles, detect_spof, critical_path | ✅ |
| 数据处理 | run_mapreduce, convert_csv_to_json, convert_json_to_csv, text_summary, json_get, json_diff, jsonpath_query, encode_base64, compute_hash | ✅ |
| LLM 调用 | llm_chat, llm_stream, llm_cost_report | ✅ |
| 诊断监控 | heap_snapshot, goroutine_summary, diagnostic_run, runtime_info | ✅ |
| 配置管理 | config_get, config_set, config_history, env_list | ✅ |
| 蓝图/DI | blueprint_build, blueprint_validate | ✅ |
| 策略引擎 | policy_evaluate, policy_list, policy_audit_log | ✅ |
| 代码地图 | find_symbol, find_callers, find_callees, get_call_graph, detect_circular_deps | ✅ |
| 定时任务 | cron_parse, cron_next, cron_describe | ✅ |
| Schema | schema_register, schema_check_compat, schema_list | ✅ |
| 编排 | run_workflow, create_workflow, list_workflows | ✅ |
| 🆕 Computer Use | screen_capture, click, type_text, key_press, open_app, ui_inspect, computer_status | ✅ |

### 🔴 未接线但代码已就绪的大能力（154个包中值得优先接线的）

| 包名 | 能力 | 代码量 | 优先级 |
|------|------|--------|--------|
| `maestro` | SAGA 工作流引擎 | ~4000行 | 🔴最高 |
| `playbook` | YAML 剧本执行 | ~3000行 | 🔴最高 |
| `knowledge` | RAG 向量搜索 | ~2000行 | 🔴最高 |
| `graphql` | 自建 GraphQL 查询引擎 | ~5000行 | 🔴最高 |
| `compiler` | 字节码编译器+VM | ~3000行 | 🟡高 |
| `observer` | OpenTelemetry 分布式追踪 | ~3000行 | 🟡高 |
| `monitor` | Prometheus 指标 | ~3000行 | 🟡高 |
| `tracer` | 分布式 trace 传播 | ~3000行 | 🟡高 |
| `linter` | 代码 linter 引擎 | ~5000行 | 🟡高 |
| `statechart` | 层次状态机 | ~2000行 | 🟡高 |
| `websocket` | WebSocket Hub | ~4000行 | 🟡高 |
| `broker` | 消息代理 pub/sub | ~3000行 | 🟡高 |
| `shard` | 一致性哈希 | ~2000行 | 🟡高 |
| `vault` | 密钥层级 vault | ~3000行 | 🟡高 |
| `sessiondb` | SQLite 会话 | ~2000行 | 🟡高 |
| `scrape` | HTML 爬取 | ~2000行 | 🟡高 |
| `swizzle` | 数据变形/ETL | ~2000行 | 🟡高 |
| `formatter` | 代码格式化 | ~3000行 | 🟡高 |
| `sourcemap` | Source Map | ~2000行 | 🟢中 |
| `manifest` | 项目清单解析 | ~2000行 | 🟢中 |


## 三、与 Reasonix / Claude Code 真实对比

| 维度 | Lumen | Reasonix | Claude Code |
|------|-------|----------|-------------|
| **代码量** | 🥇 98K | ~25K | ~20K (推测) |
| **内置工具** | 🥇 97 | ~20 | ~30 |
| **多模型** | ✅ OpenAI+Anthropic+DeepSeek | ✅ 多模型 | ❌ 仅 Claude |
| **LSP 集成** | 🟡 gopls CLI 调用 | 🥇 原生 JSON-RPC | 🥇 原生 LSP |
| **MCP 协议** | 🟡 代码完成未接线 | 🟡 部分 | 🥇 完整 MCP |
| **GitHub 集成** | ✅ REST+CLI | ✅ 完整 | ✅ 基础 |
| **Computer Use** | ✅ macOS 原生 | ❌ 无 | 🥇 最佳（浏览器操作） |
| **安全审计链** | 🥇 SHA-256 tamper-proof | ❌ 无 | ❌ 无 |
| **策略引擎** | 🥇 OPA 风格 | ❌ 无 | ❌ 无 |
| **图论/拓扑** | 🥇 BFS/Dijkstra/A*/Tarjan/SCC | ❌ 无 | ❌ 无 |
| **GraphQL** | 🥇 自建引擎 | ❌ 无 | ❌ 无 |
| **MapReduce** | 🥇 内置 | ❌ 无 | ❌ 无 |
| **工作流编排** | 🥇 DAG+SAGA+剧本 | ❌ 无 | ❌ 无 |
| **终端 UI** | 🟡 基础行式 | 🥇 多面板 TUI | 🥇 富交互 |
| **包数量** | 🥇 184 | ~60 | ~40 |
| **第三方依赖** | 🥇 零 | ~5个 | ~10个 |


## 四、质量差距（需要加强）

### 🔴 严重差距

1. **MCP 真接线** — `mcplife/mcp_real.go` 写了完整的 MCP stdio 客户端（520行），但 **没注册为 agent 工具**。agent 不能连 MCP server。解决方案：加 `mcp_list_tools`, `mcp_call_tool`, `mcp_list_resources` 工具。

2. **LSP 深度不行** — 当前通过 `gopls check` 命令行调用，不是真正的 JSON-RPC 连接。`lsp/lsp_real.go` 写了完整的 JSON-RPC 客户端（526行），**但没用到**。应该用 `StartGopls()` 启动长期进程，复用连接。

3. **TUI 太简陋** — 没有文件树、diff 面板、计划审批、子代理追踪。此前写了 `tui/tui_real.go`（910行全功能 UI），**但被删了**换了极简版。应该把它编译回来。

4. **LLM 流式输出** — `modelpool_real.go` 有完整的 OpenAI/Anthropic streaming，**但 agent 没用它**。agent 调用的是 `internal/provider/openai` 直连，没走那个流式+故障转移层。

### 🟡 中等差距

5. **没有计划审批流程** — Claude Code 有 `/plan` 先出计划再执行。Lumen 有 `permission.ModePlan` 但不能先展示计划再批准。

6. **没有自动 compact** — Reasonix 在长对话中自动压缩上下文，Lumen 的 `agent/compact.go` 有代码但似乎在 chat 模式下不触发。

7. **工具输出太长** — agent 调 `ls /tmp` 可以返回 100 行，没有自动截断和摘要。

8. **没有文件变更 diff 预览** — Claude Code 修改文件前会显示 diff。Lumen 的 `diffengine` 有完整实现，但没接到写文件前的预览流程。

### 🟢 次要差距

9. **错误恢复弱** — 模型抛出错误后不会自动重试或切换模型。
10. **记忆系统弱** — 有 `memory` 包但 agent 不用它跨会话记住上下文。
11. **技能系统未接** — 有 101 个 skill 文件但 agent 只列出不主动调用。


## 五、加强路线图（按投入产出比排序）

### 第一优先级（每项 1-2 小时）

| # | 改动 | 效果 |
|---|------|------|
| 1 | **MCP 接线** — 注册 3 个 MCP 工具到 agent | agent 直接连接任意 MCP server |
| 2 | **LSP 长连接** — 用 `lsp_real.go` 的 JSON-RPC 替代 shell 命令 | LSP 响应从 200ms → 10ms，支持增量诊断 |
| 3 | **写文件前 diff 预览** — diffengine 接到 write_file 工具 | 每次修改前看到 diff，按 y 确认 |
| 4 | **工具输出截断** — 超 2KB 自动截断+摘要 | 不再被长输出淹没 |
| 5 | **聊天记录持久化** — 保存对话到 ~/.lumen/history/ | 断线可以继续 |

### 第二优先级（每项 2-4 小时）

| # | 改动 | 效果 |
|---|------|------|
| 6 | **TUI 恢复** — 把 `tui_real.go` 的多面板 UI 编译回来 | 文件树、diff 面板、计划审批、status bar |
| 7 | **LLM 流式+故障转移** — agent 调用 `modelpool_real.go` | 流式打字效果 + 模型挂了自动切备选 |
| 8 | **计划审批流程** — /mode plan 后先展示计划再执行 | 对标 Claude Code 的 plan 模式 |
| 9 | **GraphQL 查询能力** — 接线 graphql 包 | 能查询结构化 API 数据 |
| 10 | **知识库 RAG** — 接线 knowledge 包 | 能对代码库做语义搜索 |

### 第三优先级（每项 4-8 小时）

| # | 改动 | 效果 |
|---|------|------|
| 11 | **经纪人模式** — maestro SAGA + playbook YAML | 复杂多步骤任务自动编排 |
| 12 | **沙盒执行** — sandbox 包接线 | 危险命令在隔离环境跑 |
| 13 | **集群部署** — `lumen serve` 完善 | 多 agent 协同工作 |
| 14 | **浏览器操作** — browser 包接线 | 对标 Claude Code 的浏览器控制 |
| 15 | **会话分析** — observer+tracer 接到反馈循环 | 看到 agent 内部决策链 |


## 六、一句话总结

**Lumen 在"引擎深度"上碾压 Reasonix/Claude Code** — 184 个包、97 个工具、自建 GraphQL/MapReduce/拓扑引擎/密码学套件。但在"使用体验"上明显落后 — TUI 简陋、MCP 未接、流式未通、diff 预览缺失。

差距不在代码，在**集成**。上面 15 项改动，每一项背后都有完整的内部包和测试在等着，只需要写 ~50 行 glue code 把包接到 agent 工具上。
