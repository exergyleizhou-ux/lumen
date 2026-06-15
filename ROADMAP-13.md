# Lumen 13轮冲刺计划：31,285 → 70,000 行

## 路线总览

| 轮次 | 能力维度 | 行数增量 | 累积 | 包数 |
|------|---------|---------|------|------|
| R1 | 压力测试 + 基准 | +3,200 | 34,500 | +4 |
| R2 | 数据管道 + 迁移 | +3,000 | 37,500 | +3 |
| R3 | 拓扑 + 孤儿检测 + 云集成 | +3,100 | 40,600 | +3 |
| R4 | Maestro全编排 + 剧本引擎 | +3,300 | 43,900 | +3 |
| R5 | 安全加固 + 渗透测试 | +2,900 | 46,800 | +3 |
| R6 | 工具包 + 魔杖(Wand) | +3,200 | 50,000 | +3 |
| R7 | 观测器 + 日志系统 | +2,800 | 52,800 | +3 |
| R8 | WebSocket + 实时通信 | +2,900 | 55,700 | +3 |
| R9 | 测试补齐: 10个零测试包全补 | +2,800 | 58,500 | +0 |
| R10 | 对标补齐: LSP验证 + MCP接线 | +3,000 | 61,500 | +2 |
| R11 | Grok Build级TUI完善 | +3,100 | 64,600 | +1 |
| R12 | 生产化: 性能+分布式+文档 | +3,000 | 67,600 | +2 |
| R13 | 最终集成: 全模块接线+端到端 | +2,600 | 70,200 | +0 |

## 环环相扣依赖链

```
R1 压力 + 基准
  ↓ (为 R4 提供性能基线)
R2 数据管道 + 迁移
  ↓ (为 R3 提供数据流基础)
R3 拓扑 + 孤儿 + 云
  ↓ (为 R4 提供服务发现)
R4 Maestro + 剧本
  ↓ (为 R7 提供编排事件)
R5 安全加固
  ↓ (为 R11 提供安全审计)
R6 工具包 + Wand
  ↓ (为 R10 提供 LSP 包装)
R7 观测器 + 日志
  ↓ (为 R8 提供事件流)
R8 WebSocket 实时
  ↓ (为 R11 TUI 提供实时更新)
R9 测试补齐
  ↓ (为 R13 端到端提供覆盖)
R10 LSP + MCP 接线
  ↓ (为 R13 提供完整工具链)
R11 TUI 完善
  ↓ (为 R13 提供用户体验)
R12 生产化
  ↓
R13 最终集成
```

## 每轮详细组成

### R1: 压力测试 + 基准工具 (3,200行, 4包)
- `loadgen/`: 负载生成器 — 并发请求生成、吞吐量测量、延迟分布
- `internal/tool/builtin/benchmark_tool.go`: 基准工具 — agent可调用benchmark
- `internal/observer/metrics_exporter.go`: Prometheus导出器
- `internal/observer/sample_collector.go`: 采样收集器

### R2: 数据管道 + 迁移 (3,000行, 3包)
- `datapipeline/`: ETL管道 — 提取、转换、加载 agent输出
- `migrate/`: schema版本迁移 + 数据迁移引擎
- `internal/exporter/`: 扩展导出器(Avro/Parquet/Protobuf)

### R3: 拓扑 + 孤儿 + 云 (3,100行, 3包)
- `topology/`: 服务拓扑发现 + 调用图构建
- `orphan/`: 孤儿资源检测 + 自动清理
- `cloud/`: 多云抽象层(AWS/GCP/Azure provider)

### R4: Maestro + 剧本 (3,300行, 3包)
- `maestro/`: 全编排引擎 — 跨agent工作流、条件分支、循环、SAGA模式
- `playbook/`: 剧本引擎 — YAML定义的agent行为序列、变量替换
- `internal/agent/maestro_integration.go`: agent集成

### R5: 安全加固 (2,900行, 3包)
- `hardening/`: CIS基准检查、漏洞扫描、依赖审计
- `internal/guard/`: 扩展guard (新增20个攻击模式)
- `toolkit/`: 安全工具包 — JWT分析、证书检查、加密审计

### R6: 工具包 + Wand (3,200行, 3包)
- `toolkit/`: 通用开发者工具 — JSON/YAML/TOML/XML处理器、base64/hex、jq
- `wand/`: 魔法棒 — 一键修复、自动诊断、智能建议引擎
- `internal/tool/builtin/`: 新工具注册

### R7: 观测器 + 日志 (2,800行, 3包)
- `observer/`: OpenTelemetry追踪、span导出、分布式上下文传播
- `internal/logger/`: 日志扩展 — 日志轮转、级别热切换、采样
- `internal/telemetry/`: 扩展遥测 — 会话跟踪、用户行为分析

### R8: WebSocket + 实时 (2,900行, 3包)
- `websocket/`: WebSocket服务器 — 实时推送agent事件
- `internal/serve/`: HTTP serve WebSocket升级
- `internal/tui/`: TUI WebSocket客户端接收实时事件

### R9: 测试补齐 (2,800行, 0新包)
- 给10个零测试包补测试：control, github_ops, importer, lsp, mcplife, release, serve, tui, loadgen, cloud
- 每个包3-5个测试用例
- 目标：从48个测试包→58个

### R10: LSP + MCP 真实接线 (3,000行, 2包)
- LSP验证：真实gopls端到端测试 + 修复
- MCP接线：连接真实MCP server + 测试
- `internal/lsp/`: 补充server管理
- `internal/mcplife/`: 补充测试

### R11: Grok Build级TUI (3,100行, 1包)
- `internal/tui/`: 完整重写 — 文件浏览器、实时Diff面板、Plan审批可视化、子代理追踪、思考块折叠/展开
- Bubble Tea组件化重构

### R12: 生产化 (3,000行, 2包)
- 性能优化、分布式会话、Dockerfile、CI/CD配置
- `internal/profiler/`: 扩展 — pprof集成、火焰图生成
- `internal/sessiondb/`: 分布式会话(Redis backend)

### R13: 最终集成 (2,600行, 0新包)
- 全模块接线到agent loop
- 端到端集成测试
- 文档生成
- Release准备

---

**开始执行 R1。**
