# Lumen 项目文件索引 — 供 Claude Code 审计使用
# 本地路径: /Users/lei/lumen

================================================================================
一、配置文件 (项目根目录)
================================================================================
/Users/lei/lumen/go.mod                         — Go module 声明
/Users/lei/lumen/go.sum                         — 依赖校验
/Users/lei/lumen/lumen.toml                     — 当前运行配置 (DeepSeek API)

================================================================================
二、入口 & UI 层
================================================================================
/Users/lei/lumen/cmd/lumen/main.go              — 主入口: main(), chat/run/setup/doctor 子命令, 事件 sink, 权限解析
/Users/lei/lumen/cmd/lumen/terminal.go          — 终端 UI: termSink(), runChatUI(), 颜色方案, diff 预览渲染, 步骤编号, footer 统计
/Users/lei/lumen/cmd/lumen/main_test.go         — main 测试

================================================================================
三、元文档 (根目录 .md)
================================================================================
/Users/lei/lumen/ASSESSMENT.md                  — 全面评估报告 (98K行/184包/103工具 vs Reasonix vs ClaudeCode)
/Users/lei/lumen/AUDIT-REPORT.md                — 终极审计报告 (致Claude4.8, 分层/对比/改进项)
/Users/lei/lumen/AUDIT.md                       — 审计记录
/Users/lei/lumen/HANDOFF.md                     — 交接文档
/Users/lei/lumen/HANDOFF-V2.md                  — 交接文档V2
/Users/lei/lumen/ROADMAP-13.md                  — 13轮冲刺路线图
/Users/lei/lumen/README.md                      — 项目说明
/Users/lei/lumen/CONTRIBUTING.md                — 贡献指南
/Users/lei/lumen/install.sh                     — 一键安装脚本

================================================================================
四、部署
================================================================================
/Users/lei/lumen/docker/Dockerfile              — 多阶段 alpine 镜像
/Users/lei/lumen/docker/docker-compose.yml      — Docker Compose

================================================================================
五、端到端测试
================================================================================
/Users/lei/lumen/e2e/e2e_test.go                — E2E 集成测试 (pipeline/orchestrator/config/gateway)

================================================================================
六、Agent 核心 (internal/agent/) — 12 文件
================================================================================
/Users/lei/lumen/internal/agent/agent.go         — 核心循环: Run(), 流式, 工具执行, 上下文压缩, 风暴断路器
/Users/lei/lumen/internal/agent/agent_test.go    — Agent 测试
/Users/lei/lumen/internal/agent/compact.go       — 自动压缩/滑动窗口
/Users/lei/lumen/internal/agent/coordinator.go   — 双模型协调器 (planner+executor)
/Users/lei/lumen/internal/agent/coordinator_test.go
/Users/lei/lumen/internal/agent/task.go          — 子代理工具 (task_tool)
/Users/lei/lumen/internal/agent/task_test.go
/Users/lei/lumen/internal/agent/skill_tool.go    — 技能调用工具 (run_skill)
/Users/lei/lumen/internal/agent/skill_tool_test.go
/Users/lei/lumen/internal/agent/cache_shape.go   — 前缀缓存形状
/Users/lei/lumen/internal/agent/cache_test.go
/Users/lei/lumen/internal/agent/cache.go         — 前缀缓存管理

================================================================================
七、Controller 控制层
================================================================================
/Users/lei/lumen/internal/control/controller.go  — Controller: Configure(), Run(), Plan(), LLM故障转移, 权限模式

================================================================================
八、Provider 层
================================================================================
/Users/lei/lumen/internal/provider/provider.go           — Provider 接口 + ToolSchema 定义
/Users/lei/lumen/internal/provider/provider_test.go
/Users/lei/lumen/internal/provider/openai/openai.go      — OpenAI 兼容实现: 流式SSE, HTTP连接池, 静默重试
/Users/lei/lumen/internal/provider/openai/openai_test.go

================================================================================
九、工具注册 (internal/tool/) — 31 文件, ~103 工具
================================================================================
/Users/lei/lumen/internal/tool/tool.go                   — Tool 接口, Registry, RegisterBuiltin, Schemas
/Users/lei/lumen/internal/tool/tool_test.go
/Users/lei/lumen/internal/tool/builtin/bash.go           — bash 工具
/Users/lei/lumen/internal/tool/builtin/builtin.go        — 共享导入
/Users/lei/lumen/internal/tool/builtin/builtin_test.go
/Users/lei/lumen/internal/tool/builtin/read_file.go      — read_file
/Users/lei/lumen/internal/tool/builtin/write_file.go     — write_file
/Users/lei/lumen/internal/tool/builtin/edit_file.go      — edit_file + diff Preview
/Users/lei/lumen/internal/tool/builtin/multi_edit.go     — multi_edit
/Users/lei/lumen/internal/tool/builtin/notebook_edit.go  — notebook_edit
/Users/lei/lumen/internal/tool/builtin/glob.go           — glob
/Users/lei/lumen/internal/tool/builtin/grep.go           — grep
/Users/lei/lumen/internal/tool/builtin/web_todo_ask.go   — web_fetch, todo_write, ask
/Users/lei/lumen/internal/tool/builtin/lsp_real_tools.go — LSP 5工具: diagnostics/completion/hover/definition/references (gopls→go vet→go build 三级回退)
/Users/lei/lumen/internal/tool/builtin/mcp_real_tools.go — MCP 5工具: connect/list_tools/call_tool/list_resources/list_prompts
/Users/lei/lumen/internal/tool/builtin/github_tools.go   — GitHub 5工具
/Users/lei/lumen/internal/tool/builtin/computeruse_tools.go — ComputerUse 7工具 (screen_capture/click/type_text/key_press/open_app/ui_inspect/computer_status)
/Users/lei/lumen/internal/tool/builtin/modelpool_tools.go — LLM 3工具 (llm_chat/llm_stream/llm_cost)
/Users/lei/lumen/internal/tool/builtin/graph_tools.go    — 图论 4工具 (bfs/dijkstra/topo_sort/scc)
/Users/lei/lumen/internal/tool/builtin/topology_tools.go — 拓扑 4工具
/Users/lei/lumen/internal/tool/builtin/security_tools.go — 安全 8工具 (seal/unseal/sign/verify/audit/hash_chain)
/Users/lei/lumen/internal/tool/builtin/data_tools.go     — 数据 10工具 (csv/json/text_summary/encode/hash)
/Users/lei/lumen/internal/tool/builtin/config_tools.go   — 配置 6工具 (config_get/set/history/env/blueprint)
/Users/lei/lumen/internal/tool/builtin/monitor_tools.go  — 诊断 5工具 (heap_snapshot/goroutine/diagnostics/runtime_info)
/Users/lei/lumen/internal/tool/builtin/cron_tools.go     — Cron 3工具
/Users/lei/lumen/internal/tool/builtin/schema_tools.go   — Schema 3工具
/Users/lei/lumen/internal/tool/builtin/policy_tools.go   — 策略 3工具
/Users/lei/lumen/internal/tool/builtin/orchestrator_tools.go — 编排 3工具
/Users/lei/lumen/internal/tool/builtin/codemap_tools.go  — CodeMap 6工具
/Users/lei/lumen/internal/tool/builtin/diff_tools.go     — Diff 2工具
/Users/lei/lumen/internal/tool/builtin/jsonpath_tool.go  — JSONPath 1工具
/Users/lei/lumen/internal/tool/builtin/stream_tools.go   — MapReduce + Stream 2工具

================================================================================
十、大型引擎包 (按功能分类)
================================================================================
[编排引擎]
/Users/lei/lumen/internal/maestro/maestro.go        — DAG 工作流引擎 + SAGA 补偿
/Users/lei/lumen/internal/playbook/playbook.go      — YAML 剧本执行器
/Users/lei/lumen/internal/blueprint/blueprint.go    — 依赖注入容器
/Users/lei/lumen/internal/statechart/statechart.go  — 层次状态机
/Users/lei/lumen/internal/orchestrator/orchestrator.go — Agent 池 + 任务调度
/Users/lei/lumen/internal/dispatcher/dispatcher.go  — 优先级队列 task dispatcher
/Users/lei/lumen/internal/reducer/reducer.go        — MapReduce 引擎 (并行分片+shuffle+sort)
/Users/lei/lumen/internal/taskengine/taskengine.go  — 任务引擎

[智能引擎]
/Users/lei/lumen/internal/graphql/graphql.go        — 自建 GraphQL 引擎 (schema + parser + executor)
/Users/lei/lumen/internal/jsonpath/jsonpath.go      — RFC 9535 JSONPath (递归 + 通配 + 过滤 + 切片)
/Users/lei/lumen/internal/compiler/compiler.go      — 字节码编译器 + 栈式 VM
/Users/lei/lumen/internal/linter/linter.go          — 代码 Linter 引擎 (10+ 规则)
/Users/lei/lumen/internal/codemap/codemap.go        — 代码结构分析 + 调用图
/Users/lei/lumen/internal/ast/ast.go                — AST 树操作
/Users/lei/lumen/internal/graphwalker/graphwalker.go — BFS/DFS/Dijkstra/A*/Tarjan SCC
/Users/lei/lumen/internal/transpile/transpile.go    — Proto→TS/Go/SQL 转译
/Users/lei/lumen/internal/formatter/formatter.go    — 代码格式化引擎
/Users/lei/lumen/internal/sourcemap/sourcemap.go    — VLQ 编码 source map
/Users/lei/lumen/internal/knowledge/knowledge.go    — RAG 向量搜索

[网络安全]
/Users/lei/lumen/internal/apigateway/apigateway.go  — HTTP API Gateway (CORS+Auth+RateLimit+Recovery+Timeout)
/Users/lei/lumen/internal/gateway/gateway.go        — Scatter-gather 请求聚合
/Users/lei/lumen/internal/connector/connector.go    — REST/gRPC/DB 连接器 + 断路器
/Users/lei/lumen/internal/broker/broker.go          — 消息代理 (pub/sub/request-reply)
/Users/lei/lumen/internal/websocket/websocket.go    — WebSocket Hub (channels + 心跳)
/Users/lei/lumen/internal/mux/mux.go                — 连接多路复用
/Users/lei/lumen/internal/selector/selector.go      — K8s 风格 label selector
/Users/lei/lumen/internal/circuitbreaker/circuitbreaker.go — 断路器模式

[安全加密]
/Users/lei/lumen/internal/seal/seal.go              — AES-256-GCM 信封加密 + 密钥轮转
/Users/lei/lumen/internal/vault/vault.go            — 密钥层级 vault
/Users/lei/lumen/internal/notary/notary.go          — Ed25519 签名 + Hash Chain
/Users/lei/lumen/internal/audit/audit.go            — 防篡改审计链 + 合规报告
/Users/lei/lumen/internal/policy/policy.go          — OPA 风格策略引擎
/Users/lei/lumen/internal/hardening/hardening.go    — CIS 合规扫描
/Users/lei/lumen/internal/keys/keys.go              — 密钥生成/轮转/过期
/Users/lei/lumen/internal/fingerprint/fingerprint.go — SHA-256 + MinHash 指纹
/Users/lei/lumen/internal/guard/guard.go            — 危险命令防护
/Users/lei/lumen/internal/permission/permission.go   — 权限门控 (bypass/default/plan/accept-edits)

[可观测性]
/Users/lei/lumen/internal/observer/observer.go      — OpenTelemetry spans
/Users/lei/lumen/internal/monitor/monitor.go        — Prometheus metrics + 告警
/Users/lei/lumen/internal/tracer/tracer.go          — 分布式追踪 (traceparent/tracestate)
/Users/lei/lumen/internal/tracepoint/tracepoint.go  — 运行时追踪点 + 火焰图
/Users/lei/lumen/internal/diag/diag.go              — 自诊断引擎 + 健康探针
/Users/lei/lumen/internal/watchpoint/watchpoint.go  — 数据变化监控 + before/after
/Users/lei/lumen/internal/heapdump/heapdump.go      — 堆内存快照 + 分配追踪 + goroutine 摘要
/Users/lei/lumen/internal/configlive/configlive.go  — 配置热加载 + 回滚
/Users/lei/lumen/internal/env/env.go                — 环境变量 + .env
/Users/lei/lumen/internal/config/config.go          — TOML 配置解析
/Users/lei/lumen/internal/telemetry/telemetry.go    — 遥测
/Users/lei/lumen/internal/metrics/metrics.go        — 指标收集

[数据存储]
/Users/lei/lumen/internal/sessiondb/sessiondb.go    — SQLite 会话持久化
/Users/lei/lumen/internal/artifact/artifact.go      — 构建产物管理 (tag + promote + archive)
/Users/lei/lumen/internal/archive/archive.go        — tar.gz/zip 打包/解包
/Users/lei/lumen/internal/bloom/bloom.go            — Bloom Filter
/Users/lei/lumen/internal/lockfile/lockfile.go      — 分布式锁 (TTL + 死锁检测)
/Users/lei/lumen/internal/migrate/migrate.go        — Schema 迁移引擎 (up/down)
/Users/lei/lumen/internal/schema/schema.go           — Schema 注册表 + 兼容性检测
/Users/lei/lumen/internal/shard/shard.go            — 一致性哈希 + 虚拟节点
/Users/lei/lumen/internal/clustermap/clustermap.go  — 集群拓扑 + leader 选举 + gossip
/Users/lei/lumen/internal/asset/asset.go            — 内容寻址存储
/Users/lei/lumen/internal/deadletter/deadletter.go  — 死信队列
/Users/lei/lumen/internal/checkpoint/checkpoint.go  — 文件变更快照

[数据流]
/Users/lei/lumen/internal/datapipeline/datapipeline.go — ETL 管道 (filter/map/aggregate/sort/limit)
/Users/lei/lumen/internal/stream/stream.go          — 流处理 (窗口+分区+聚合)
/Users/lei/lumen/internal/batch/batch.go            — 批量处理 (背压+chunk)
/Users/lei/lumen/internal/exchange/exchange.go      — JSON↔CSV 转换
/Users/lei/lumen/internal/export/export.go          — 导出管道 (NDJSON+chunked)
/Users/lei/lumen/internal/diffengine/diffengine.go  — LCS diff 引擎 (行/词/JSON)
/Users/lei/lumen/internal/diff/diff.go              — diff 类型定义
/Users/lei/lumen/internal/swizzle/swizzle.go        — 数据变形 (flatten/unflatten/coerce)
/Users/lei/lumen/internal/scrape/scrape.go          — HTML 解析 + OpenGraph
/Users/lei/lumen/internal/frontmatter/frontmatter.go — Markdown frontmatter

[基础设施]
/Users/lei/lumen/internal/signal/signal.go          — OS signal 处理 + graceful shutdown
/Users/lei/lumen/internal/adapt/adapt.go            — webhook 接收器 + 进程 supervisor
/Users/lei/lumen/internal/extender/extender.go      — 插件扩展系统 + 依赖解析
/Users/lei/lumen/internal/hotplug/hotplug.go        — 热插拔插件
/Users/lei/lumen/internal/toolkit/toolkit.go        — 开发者工具集
/Users/lei/lumen/internal/modelpool/modelpool.go     — 模型池 (原)
/Users/lei/lumen/internal/modelpool/modelpool_real.go — LLM 真实调用 (OpenAI+Anthropic streaming + 成本追踪 + 故障转移)
/Users/lei/lumen/internal/repl/repl.go              — 交互式 REPL
/Users/lei/lumen/internal/template/template.go      — 模板引擎 + Prompt Builder
/Users/lei/lumen/internal/cloud/cloud.go            — 多云抽象 + 成本估算
/Users/lei/lumen/internal/evacuate/evacuate.go      — 优雅连接排空
/Users/lei/lumen/internal/cron/cron.go              — Cron 表达式引擎
/Users/lei/lumen/internal/cronparser/cronparser.go  — Cron 解析器 + 人类可读描述
/Users/lei/lumen/internal/scheduler/scheduler.go    — 任务调度器
/Users/lei/lumen/internal/loadgen/loadgen.go        — 负载生成 + 延迟分布
/Users/lei/lumen/internal/poller/poller.go          — 资源轮询器
/Users/lei/lumen/internal/snapshot/snapshot.go      — 目录快照
/Users/lei/lumen/internal/manifest/manifest.go      — 项目清单解析
/Users/lei/lumen/internal/packager/packager.go      — 打包 (tar/zip)
/Users/lei/lumen/internal/registry/registry.go      — 注册表

[LSP & MCP 真实客户端]
/Users/lei/lumen/internal/lsp/lsp.go                — LSP 类型定义 (原)
/Users/lei/lumen/internal/lsp/lsp_real.go           — 完整 gopls JSON-RPC 2.0 客户端 (526行)
/Users/lei/lumen/internal/lsp/lsp_real_test.go
/Users/lei/lumen/internal/lsp/client.go             — LSP 客户端辅助
/Users/lei/lumen/internal/mcplife/mcp_real.go       — 完整 MCP stdio 传输客户端 (520行)
/Users/lei/lumen/internal/mcplife/mcp_real_test.go
/Users/lei/lumen/internal/mcplife/manager.go        — MCP 管理器

[Computer Use]
/Users/lei/lumen/internal/computeruse/computeruse.go — macOS 屏幕/鼠标/键盘/App 控制 (screencapture + osascript)

[命令行工具]
/Users/lei/lumen/internal/cliutils/cliutils.go      — spinner/progress bar/ANSI/table writer
/Users/lei/lumen/internal/terminal/terminal.go      — 终端工具

[GitHub API]
/Users/lei/lumen/internal/github_ops/github_real.go — GitHub REST + gh CLI 双通道 (PR/issue/CI/release/search)

[事件系统]
/Users/lei/lumen/internal/event/event.go            — Event 类型定义 (Kind/Sink/Event)
/Users/lei/lumen/internal/event/event_test.go
/Users/lei/lumen/internal/eventbus/eventbus.go      — 事件总线
/Users/lei/lumen/internal/evidence/evidence.go      — 证据收集
/Users/lei/lumen/internal/timeline/timeline.go      — 时间线记录

================================================================================
十一、Skill 技能系统
================================================================================
/Users/lei/lumen/internal/skill/skill.go            — Skill 加载/注册/列表
/Users/lei/lumen/internal/skill/skill_test.go
/Users/lei/lumen/skills/                            — 101 个 .md 技能文件

================================================================================
十二、关键指标
================================================================================
Go 代码总行数: 98,793 行
内部包数: 184
Agent 可调用工具: 103 个
Go 源文件: ~365 个
当前 Provider: DeepSeek (https://api.deepseek.com/v1)
默认模式: bypass (全权限)
gopls 版本: v0.16.2 (已安装)
单文件编译: ~23MB
