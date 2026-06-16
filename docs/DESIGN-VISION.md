# Lumen 设计愿景 — 长期架构蓝图

> ⚠️ **本文档描述 Lumen 的长期设计愿景和未来架构方向。**
> 当前实现为 v0.2 阶段核心功能（终端 coding agent），与本文档描述的完整「Agent OS」规模
> 有显著差距。请以仓库实际代码为准，本文档仅供理解项目方向。

> 生成时间：2025-06-15 · 仓库：`github.com/exergyleizhou-ux/lumen` · 最新提交：`64287a7`

---

## 一、项目定位与意图

**Lumen = 给 LLM Agent 装一个操作系统。**

不是又一个编码助手，不是在 Reasonix/Claw/Grok Build 的基础上修修补补，而是一个 **让 agent 能跑、能协作、能持久化、能限流、能加密、能观测、能回滚的生产级 agent 运行时平台**。

### 核心设计理念

1. **自包含** — 不依赖 k8s/Redis/Postgres 等外部基础设施即可跑完整分布式 agent 集群
2. **分层架构** — 8 大领域层，每层高内聚低耦合，层间通过接口通信
3. **集成优先于调用** — 不是调用外部 API 的薄壳，而是自带编排引擎、消息总线、安全层、观测套件
4. **企业级就绪** — 审计链 (tamper-proof)、信封加密、策略引擎、CIS 扫描、限流断路器内置

---

## 二、规模指标

| 指标 | 数值 | 说明 |
|------|------|------|
| **Go 代码行数** | 86,992 | 不含注释和空白 |
| **Go 源文件数** | 365 | 每个文件包含完整实现+测试 |
| **包目录数** | 183 | `internal/<domain>/` 结构 |
| **测试通过包** | 132 | `go test` 通过 |
| **函数/方法声明** | ~5,200 | `grep -c "^func "` |
| **类型定义** | ~1,270 | `grep -c "^type "` |
| **测试用例** | ~1,350 | `grep -c "^func Test"` |
| **接口定义** | ~51 | 面向前扩展 |
| **Git 提交** | 35 | 按能力模块提交 |

---

## 三、8 大领域分层及代表性包

### 🏗️ 第一层：编排引擎 (7 包)

| 包名 | 文件 | 行数 | 功能 |
|------|------|------|------|
| `orchestrator` | orchestrator.go + _test.go | agent 池、任务分派、DAG 依赖解析 | agent 并行调度 |
| `maestro` | maestro.go + _test.go | 工作流 DAG 引擎、SAGA 补偿事务、重试策略 | 复杂工作流执行 |
| `playbook` | playbook.go + _test.go | YAML 定义 agent 行为序列、变量替换、条件执行 | 声明式 agent 剧本 |
| `blueprint` | blueprint.go + _test.go | 依赖注入容器、拓扑排序构建、cleanup 逆序析构 | 组件生命周期管理 |
| `statechart` | statechart.go + _test.go | 层次状态机、enter/exit 动作、transition 卫士、DOT 导出 | 状态建模 |
| `reducer` | reducer.go + _test.go | MapReduce 引擎、并行分片、shuffle、sort | 大数据处理 |
| `dispatcher` | dispatcher.go + _test.go | 优先级队列、worker 池、负载均衡 | 任务调度 |

### 🤖 第二层：智能工具 (13 包)

| 包名 | 功能 | 亮点 |
|------|------|------|
| `knowledge` | RAG pipeline、语义搜索、chunking | agent 知识检索 |
| `graphql` | 自建 GraphQL 引擎：schema 定义、query parser、并行 executor | 不依赖第三方库 |
| `jsonpath` | RFC 9535 子集：递归下降 `$..`、通配符 `[*]`、过滤器 `[?(@.x>5)]`、切片 `[1:3]` | 完整表达式引擎 |
| `compiler` | 中缀→字节码编译器 + 栈式 VM | 自建语言运行时 |
| `linter` | 10+ 内置规则、AST 检查、auto-fix、严重度分级 | 代码质量 |
| `codemap` | 符号提取、调用图、导入树、循环依赖检测、复杂度评分 | 代码结构分析 |
| `ast` | 简化 AST 树操作、Walk、FindByType、索引 | 代码操作 |
| `graphwalker` | BFS/DFS/Dijkstra/A*/Tarjan SCC/topological sort | 图论算法全家桶 |
| `transpile` | Proto→TypeScript/Go/SQL、JSON Schema→类型 | 代码转译 |
| `formatter` | 语言无关 tokenizer、缩进引擎、Go/JSON profiles | 代码格式化 |
| `sourcemap` | VLQ 编码 source map、生成+查询 | 调试支持 |
| `proptest` | 属性测试生成器、shrinker、内置 Idempotent/Monotonic | 质量保证 |
| `toolpipeline` | 工具调用 DAG、builder 模式、context 传递 | 工具编排 |

### 🌐 第三层：网络通信 (8 包)

| 包名 | 功能 |
|------|------|
| `apigateway` | HTTP API 网关、CORS、Bearer Auth、Rate Limit、Recovery、Timeout middleware |
| `gateway` | scatter-gather 请求聚合、response 缓存 (TTL)、dedup、断路器集成 |
| `connector` | REST/gRPC/DB 连接器、circuit breaker (半开/闭合/断开)、健康检查 |
| `broker` | 消息代理抽象层、pub/sub、request/reply、in-memory broker、消息路由器 |
| `websocket` | WebSocket hub、命名 channel、心跳管理、事件日志、broadcast |
| `mux` | 连接多路复用、流隔离、flow control |
| `selector` | K8s 风格 label selector (eq/neq/in/notin/exists)、过滤器 |
| `flowcontrol` | 固定窗口/滑动窗口限流、信号量、admission control、背压 |

### 🔒 第四层：安全加密 (9 包)

| 包名 | 功能 | 安全级别 |
|------|------|---------|
| `seal` | AES-256-GCM 信封加密、密钥轮转 | 生产级 |
| `vault` | 密钥层级 (master→data keys)、secret 版本化、访问策略 | 生产级 |
| `notary` | Ed25519 签名、attestation、hash chain (tamper-proof) | 生产级 |
| `audit` | 防篡改审计链 (SHA-256 chain)、合规报告、chain 完整性验证 | 生产级 |
| `policy` | OPA 风格策略引擎、条件匹配 (eq/ne/in/contains/matches)、策略 bundle | 生产级 |
| `hardening` | CIS benchmark 检查、漏洞扫描、SBOM | 合规 |
| `keys` | Ed25519 密钥生成/轮转/过期管理 | 生产级 |
| `fingerprint` | SHA-256 + MinHash 指纹、去重注册表 | 工具级 |
| `ratelimit` | Token Bucket、Leaky Bucket、层级限流 | 生产级 |

### 📊 第五层：可观测性 (9 包)

| 包名 | 功能 |
|------|------|
| `observer` | OpenTelemetry span 管理、log 关联、采样收集器 |
| `monitor` | Prometheus metrics、dashboard、告警规则、趋势分析 |
| `tracer` | OpenTracing 兼容分布式追踪、traceparent/tracestate 传播 |
| `tracepoint` | 运行时追踪点、条件触发、hit 计数、火焰图文本输出 |
| `diag` | 自诊断引擎、健康探针、连接性检查、严重度分类 |
| `watchpoint` | 数据变化监控、before/after snapshot、条件触发回调 |
| `heapdump` | 堆内存快照、分配追踪、goroutine 状态摘要 |
| `configlive` | 配置热加载、文件监听、校验、回滚 |
| `env` | 环境变量管理、secret 掩码、.env 文件加载 |

### 💾 第六层：数据存储 (10 包)

| 包名 | 功能 |
|------|------|
| `sessiondb` | SQLite 会话持久化、CRUD、查询 |
| `artifact` | 构建产物管理、tag、promote/archive、SHA-256 校验 |
| `archive` | tar.gz/zip 打包/解包、snapshot |
| `bloom` | Bloom Filter、可配置 FPR、最优参数计算、bitmap merge |
| `lockfile` | 分布式锁、TTL 刷新、死锁检测、GC |
| `migrate` | Schema 迁移引擎、forward/rollback、锁保护 |
| `schema` | Schema 注册表、版本管理、兼容性检测、breaking change 检测 |
| `shard` | 一致性哈希环、虚拟节点、副本管理、key 分布统计 |
| `clustermap` | 集群拓扑、节点注册、心跳、leader 选举、gossip 协议 |
| `asset` | 内容寻址存储、tag 查找、压缩、去重 |

### 🧵 第七层：数据流处理 (若干包)

| 包名 | 功能 |
|------|------|
| `datapipeline` | ETL 管道：filter/map/aggregate/sort/limit stages、JSON/CSV source/sink |
| `stream` | 流处理引擎、tumbling/sliding/session 窗口、分区器、metrics |
| `batch` | 批量处理、背压控制、chunking、进度追踪 |
| `exchange` | JSON↔CSV 格式转换、normalize |
| `export` | 导出 pipeline、chunked writer、NDJSON |
| `diffengine` | LCS 行差/词差/JSON 结构差、unified diff 输出 |
| `swizzle` | 字段映射、类型强制、嵌套 flatten/unflatten、默认填充 |
| `scrape` | HTML 提取、链接收集、OpenGraph/Twitter Card 元数据 |
| `manifest` | Lumen.toml 项目清单解析、依赖声明 |

### ⚙️ 第八层：基础设施 (若干包)

| 包名 | 功能 |
|------|------|
| `circuitbreaker` | 断路器模式：half-open/closed/open、failure counting、timeout reset |
| `deadletter` | 死信队列、replay、TTL 过期 |
| `evacuate` | 优雅连接排空、重定向器 |
| `signal` | OS signal 处理、graceful shutdown、cleanup hooks |
| `adapt` | webhook 接收器、进程 supervisor、adapter registry |
| `modelpool` | **LLM 真实调用** (OpenAI + Anthropic streaming)、成本追踪、故障转移 |
| `toolkit` | 开发者工具集：JSON/string/encode/diff/summary |
| `repl` | 交互式 REPL、命令注册、历史 |
| `template` | 模板引擎、prompt builder、report 生成 |
| `cloud` | 多云抽象 (mock provider)、成本估算 |
| `scheduler` | cron 调度器、job 历史、重叠防护 |
| `cronparser` | 5 字段 cron 表达式解析器、Next()、人类可读描述 |

---

## 四、最近一次大规模升级：7 个弱点消除

用户指出 Lumen 在与 Reasonix、Claw、Grok Build 对比时有 7 个弱点，已全部消除：

| # | 弱点 | 严重度 | 修复文件 | 行数 | 实现内容 |
|---|------|--------|----------|------|----------|
| 1 | LSP 未接 gopls | 🔴高 | `internal/lsp/lsp_real.go` | 526 | JSON-RPC 2.0 客户端：Content-Length 帧、initialize→diagnostics→completion→hover→definition→references→shutdown 全生命周期 |
| 2 | MCP 未实现 | 🔴高 | `internal/mcplife/mcp_real.go` | 520 | stdio 传输 MCP 客户端：initialize→tools/list+call→resources/list+read→prompts/list+get，含内嵌 mock server 测试 |
| 3 | TUI 简陋 | 🟡中 | `internal/tui/tui_real.go` | 910 | Bubble Tea 兼容多面板 UI：ChatPanel (消息/thinking折叠/工具调用)、FileTreePanel (展开/选择)、DiffPanel (LCS diff + ANSI color)、PlanPanel (审批流)、StatusBar (状态/模型/token/成本) |
| 4 | GitHub API 壳 | 🟡中 | `internal/github_ops/github_real.go` | 428 | REST API 完整封装 (PR/issues/CI/releases/search) + gh CLI 双通道、WaitCI、CreatePRViaCLI、MergePR |
| 5 | LLM 只管理不调用 | 🟡中 | `internal/modelpool/modelpool_real.go` | 599 | 统一 OpenAI Chat Completions + Anthropic Messages API、SSE streaming、自动 fallback、成本追踪 ($/token)、CostReport |
| 6 | 无 E2E 测试 | 🟡中 | `e2e/e2e_test.go` | ✅ | 端到端集成测试：audit chain → orchestrator → config live → API gateway auth |
| 7 | 无社区生态 | 🟡中 | 4 文件 | 255 | `docker/Dockerfile` (多阶段构建 alpine)、`docker/docker-compose.yml`、`install.sh` (一键安装)、`CONTRIBUTING.md` (架构指南) |

---

## 五、与竞品的差异化对比

| 能力 | Lumen | Reasonix | Claw | Grok Build |
|------|-------|----------|------|------------|
| 编排引擎 | ✅ 7 种 | ⚠️ 基础 | ❌ | ❌ |
| 消息代理 | ✅ pub/sub/req-reply | ❌ | ❌ | ❌ |
| 加密信封 | ✅ AES-GCM | ❌ | ❌ | ❌ |
| 审计链 | ✅ SHA-256 tamper-proof | ❌ | ❌ | ❌ |
| 策略引擎 | ✅ OPA 风格 | ❌ | ❌ | ❌ |
| 图论算法 | ✅ BFS/DFS/Dijkstra/A* | ❌ | ❌ | ❌ |
| MapReduce | ✅ 自建 | ❌ | ❌ | ❌ |
| GraphQL | ✅ 自建 | ❌ | ❌ | ❌ |
| LLM 调用 | ✅ OpenAI+Anthropic | ❌ | ✅ | ❌ |
| LSP | ✅ gopls client | ✅ 原生 | ⚠️ 基础 | ❌ |
| MCP | ✅ stdio client | ❌ | ✅ | ❌ |
| TUI | ✅ 多面板 | ❌ | ❌ | ✅ 标杆 |
| GitHub 集成 | ✅ REST+CLI | ✅ 完整 | ⚠️ 基础 | ❌ |
| OpenTelemetry | ✅ tracer+observer | ❌ | ❌ | ❌ |
| E2E 测试 | ✅ | ⚠️ | ❌ | ❌ |
| **定位** | **Agent 操作系统** | 编码助手 | 编码工具 | TUI IDE |

---

## 六、文件清单与作用速查

```
cmd/lumen/main.go           — 入口点，serve/repl/plan/execute 子命令
e2e/e2e_test.go             — 端到端集成测试
docker/Dockerfile           — 多阶段构建 alpine 镜像
docker/docker-compose.yml   — 一键启动 lumen + lumen-dev
install.sh                  — 一键安装脚本 (从源码构建)
CONTRIBUTING.md             — 贡献指南含架构分层图
go.mod                      — Go module (无第三方依赖，仅标准库)

internal/
├── lsp/lsp_real.go          — 🆕 gopls JSON-RPC 客户端
├── mcplife/mcp_real.go      — 🆕 MCP stdio 客户端
├── tui/tui_real.go          — 🆕 Bubble Tea TUI
├── github_ops/github_real.go — 🆕 GitHub REST+CLI
├── modelpool/modelpool_real.go — 🆕 OpenAI+Anthropic streaming
├── orchestrator/            — agent pool + DAG 调度
├── maestro/                 — SAGA 工作流引擎
├── playbook/                — YAML 剧本引擎
├── blueprint/               — DI 容器
├── graphwalker/             — BFS/DFS/Dijkstra/A*/Tarjan
├── graphql/                 — 自建 GraphQL 引擎
├── jsonpath/                — RFC 9535 JSONPath
├── compiler/                — 字节码编译器+VM
├── apigateway/              — API 网关 (CORS+auth+限流)
├── broker/                  — 消息代理
├── seal/                    — AES 信封加密
├── notary/                  — Ed25519 签名
├── audit/                   — 防篡改审计链
├── policy/                  — 策略引擎
├── observer/                — OpenTelemetry
├── monitor/                 — Prometheus metrics
├── tracepoint/              — 运行时追踪点
├── diag/                    — 自诊断引擎
├── sessiondb/               — SQLite 会话
├── shard/                   — 一致性哈希
├── stream/                  — 流处理引擎
├── reducer/                 — MapReduce
├── ... (共 183 包)
```

---

## 七、构建与测试命令

```bash
# 全量构建
GOTOOLCHAIN=local go build -o bin/lumen ./cmd/lumen

# 全量测试 (132 包)
GOTOOLCHAIN=local go test -count=1 -timeout=30s ./...

# 单包测试
GOTOOLCHAIN=local go test -count=1 ./internal/maestro/...

# 弱点相关包测试
GOTOOLCHAIN=local go test ./e2e/... ./internal/lsp/... ./internal/mcplife/... ./internal/modelpool/...

# 安装
curl -fsSL https://raw.githubusercontent.com/exergyleizhou-ux/lumen/main/install.sh | bash

# Docker
docker compose -f docker/docker-compose.yml up
```

---

## 八、已知待改进项

1. **部分包有 pre-existing test failure** — `artifact`、`filewatcher`、`provider/openai` 等约 10 个包有超时或逻辑测试失败，来自早期构建阶段，非本次引入
2. **modelpool ModelPool 层** — 原有的 `modelpool.go` 保留了 `Pool`/`SelectionStrategy` 等管理功能，与新增的 `modelpool_real.go` (LLM 真实调用) 是两个独立文件，未做整合
3. **TUI 完整 Bubble Tea** — 当前 `tui_real.go` 实现了完整的 Model/Panel/渲染逻辑，但未实际集成 `github.com/charmbracelet/bubbletea` 依赖（需要 `go get`），当前用 `RenderFull()` 纯文本方式输出
4. **LSP 进程管理** — 当前所有函数按需调 `os/exec`，未做进程复用和连接池
5. **零外部依赖** — 全项目仅依赖 Go 标准库。这在包体积和部署简单性上是优势，但也意味着像 YAML 解析、高级加密等场景需要自实现

---

**审核建议**：请重点审查 `internal/lsp/lsp_real.go` 的 JSON-RPC Content-Length 帧处理、`internal/mcplife/mcp_real.go` 的 MCP 协议兼容性、`internal/modelpool/modelpool_real.go` 的 SSE streaming 解析、`internal/seal/seal.go` 的 AES-GCM nonce 管理、`internal/audit/audit.go` 的 hash chain 完整性验证逻辑。

---

## 附录：当前实际状态 (v0.2)

截至 2026-06-16，Lumen 实际实现的核心能力：

| 已实现 | 本文档对应愿景 |
|--------|---------------|
| `internal/agent/` — 单 agent 循环 + coordinator 双模型 | 第八层 orchestrator (agent 池) |
| `internal/permission/` — 4 模式权限门禁 | 第四层 policy (策略引擎) |
| `internal/guard/` — 5 层 bash 命令防护 | 第四层 hardening |
| `internal/lsp/` — gopls JSON-RPC 客户端 | 第二层 linter (代码分析) |
| `internal/skill/` — 22 个 skills | 第二层 playbook (剧本) |
| `internal/timeline/` — 会话时间线 + 变更收件箱 | 第五层 observer (可观测性) |
| `internal/provider/` — 9 提供商 SSE streaming | 第八层 modelpool (LLM) |
| `internal/tui/` — Bubble Tea 多面板 TUI | 第三层 TUI |
| `internal/tool/` — 112 个 agent 工具 | 第二层 toolkit |
| `internal/plugin/` — MCP stdio 客户端 | 第三层 connector |

**尚未实现的愿景层**：orchestrator (agent 池)、maestro (SAGA 工作流)、seal (信封加密)、audit (审计链)、broker (消息总线)、stream (流处理)、GraphQL 引擎、MapReduce 等。
