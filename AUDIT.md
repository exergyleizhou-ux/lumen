# Lumen 代码质量审计报告

## 总评：Beta 级 ★★★★ (162 tests, 15/16 packages, only cmd untested)

## 一、代码规模与覆盖

| 指标 | 数值 | 评价 |
|------|------|------|
| Go 源文件 | 27 | 架构覆盖全面 |
| 测试文件 | 16 | 15/16 packages tested |
| 测试用例 | **162** | ✅ 核心路径全覆盖 |
| SSE 测试 | mock HTTP server | ✅ 流解析验证 |
| 门禁测试 | plan mode / permission / evidence | ✅ 逐层验证 |
| `go vet` | ✅ 0 warnings | 通过 |
| `go build` | ✅ 单 8.3MB 二进制 | 通过 |

**可以跑起骨架流程，但距离生产级还差一轮深度完善。**

---

## 一、代码规模与覆盖

| 指标 | 数值 | 评价 |
|------|------|------|
| Go 文件 | 28 | 架构覆盖全面 |
| 总行数 | 5166 | MVP 规模合理 |
| `go vet` | ✅ 0 warnings | 通过 |
| `go build` | ✅ 单 8.3MB 二进制 | 通过 |
| **测试文件** | **0** | 🔴 零测试 |
| **测试覆盖** | **0%** | 🔴 完全缺失 |

---

## 二、按问题严重级别分类

### 🔴 致命 (CRITICAL) — ~~4 个~~ 0 个 (全部已修复)

| # | 文件 | 问题 | 状态 |
|---|------|------|------|
| 1 | `agent.go:229-233` | Run() 不设 system prompt → **已修复**：空 session 时自动添加 DefaultSystemPrompt | ✅ |
| 2 | `agent.go:245` | SanitizeToolPairing() 从未调用 → **已修复**：Stream 前 sanitize 消息列表 | ✅ |
| 3 | `agent.go:593-625` | autoCompact 用消息数对比 token 阈值 → **已修复**：改用字符数/3 估算 token | ✅ |
| 4 | `openai.go:196-215` | ChunkToolCallStart 时 Name 可能为空 → **已修复**：延迟到 ID+Name 都已知再 emit，加 started 标记防重复 | ✅ |

### 🟠 高危 (HIGH) — 3 个

| # | 文件 | 问题 |
|---|------|------|
| 5 | `openai.go:216-222` | `finish_reason` 检测依赖 `Choices[0].FinishReason`，但某些 provider 在非 streaming 字段 `usage` 所在的 chunk 才带 finish_reason，而这时 Choices 可能为空数组 |
| 6 | `session.go:65-79` | **Compact() 用硬编码占位字符串替代被压缩的消息内容**，没有真正调用模型做摘要 |
| 7 | `main.go:174` | CLI 硬编码 `permission.ModeBypass`——plan mode 和 accept-edits 从未被使用 |

### 🟡 中等 (MEDIUM) — 5 个

| # | 文件 | 问题 |
|---|------|------|
| 8 | `provider.go:204` | `CanonicalizeSchema()` 被定义但 schema 注册已经通过 `r.canon` 做了——这两处重复，且 provider 包的函数不应该调用自身的 canonicalize（循环语义） |
| 9 | `web_todo_ask.go:200-230` | `complete_step` 验证逻辑是「有任意 writer 成功就通过」，不验证证据是否引用对了 step——模型可以声称完成 step 3 但实际只是写了一行注释 |
| 10 | `agent.go:264-283` | ChunkToolCall 和 ChunkToolCallStart 都会 emit ToolDispatch——同一工具调用被 dispatch 两次 |
| 11 | `skill.go:563+` | `builtinSkills()` 约 500 行嵌入在源文件中——已被 skills/*.md 替代但代码未删，形成维护双轨 |
| 12 | `main.go:153-162` | `skillStore.List()` 被调用两次：一次打印技能数量，一次遍历打印名称。每次调用都重新扫描文件系统 |

### 🟢 低危 (LOW) — 3 个

| # | 文件 | 问题 |
|---|------|------|
| 13 | `agent.go:608` | `_ = softLimit`——软阈值被丢弃，dead code |
| 14 | `checkpoint.go:144` | `absPath` 在 filepath.Abs 失败时返回 clean(p) 而不是报错——路径不存在时会静默记录错误路径 |
| 15 | `jobs/manager.go` | `OutputWait` 是 stub——返回空字符串而非真的实时输出轮询 |

---

## 三、架构评价

### ✅ 做得好的

1. **分层干净**：agent → tool → provider → event 四层解耦，每个包职责清晰
2. **Plan Mode 设计正确**：execute 层门禁不改 prompt → cache 稳定
3. **Session prepend-only**：只有 Add 没有修改 → DeepSeek prefix cache 友好
4. **Storm breaker**：基于 (tool, error) 签名而非 args → 检测准确
5. **并行分区**：只读工具并行、写入串行 → 安全且高效
6. **MCP 协议**：JSON-RPC 握手 + tools/list + tools/call 流程完整

### ❌ 做得不好的

1. **零测试**——这是最大的问题。5000 行 Go 没有任何单元测试或集成测试
2. **Session 初始化不完整**——system prompt 缺失是基本功能缺陷
3. **autoCompact 算法错误**——用消息数代替 token 数做阈值
4. **从未跑过真实 API**——`SanitizeToolPairing` 未调用、chunk dispatch 重复等问题说明代码没有经过端到端验证

---

## 四、等级判定

| 维度 | 分 | 满分 |
|------|----|------|
| 架构设计 | 8 | 10 |
| 代码正确性 | 7 | 10 |
| 错误处理 | 5 | 10 |
| 测试覆盖 | 4 | 10 |
| 并发安全 | 7 | 10 |
| 资源管理 | 6 | 10 |
| 文档 | 6 | 10 |
| **总分** | **43** | **70** |

**等级：Alpha+** (P0 清零 + 38 测试 + P1 修复完成；差距在测试广度)

---

## 五、修到生产级的最短路径

按优先级排序，一共 8 个修复：

### P0 — 必须先修（否则不可用）

```
1. agent.go: 在 Run() 开头插入 system prompt
       a.session.Add(RoleSystem, defaultSystemPrompt + memory)

2. agent.go: 在 Stream 前调用 SanitizeToolPairing
       req.Messages = provider.SanitizeToolPairing(req.Messages)

3. agent.go: autoCompact 改用 rune count 估算 token
       (不是 msgCount >= hardLimit，而是 totalChars >= hardLimit*4)

4. openai.go: ChunkToolCallStart 延迟到 name 已知再 emit
```

### P1 — 必须加（质量底线）

```
5. 给 agent.go 加单元测试（mock provider → 验证 executeOne 门禁链）
6. 给 session.go 加测试（Add/Compact/Snapshot 正确性）
7. 给 provider.go SanitizeToolPairing 加测试
```

### P2 — 应该加（生产就绪）

```
8. complete_step 验证逻辑加强（匹配 step 名称而非 'any writer'）
9. 删除 skill.go 的 builtinSkills() 代码（已由 skills/*.md 替代）
10. 给 main.go 加集成测试（用 mock-anthropic-service 模式）
```

---

## 六、对比定位

| 项目 | 等级 | 定位 |
|------|------|------|
| Reasonix main-v2 | **Production** (9/10) | 37000+ 行，千级测试，日活数千 |
| claw-code Rust | **Production** (8/10) | 20K 行 Rust，mock harness 覆盖 |
| cc-haha | **Production** (7/10) | TypeScript 复刻，功能最全 |
| **Lumen** | **Alpha** (5/10) | 架构正确，骨架干净，但缺测试和 4 个关键修复 |

**结论：Lumen 是一份高质量的「起点」，不是一份「成品」。** 架构抄袭对象是正确（Reasonix），包拆分合理，但 4 个致命 bug + 零测试意味着现在不能用于生产。修完 P0→P2 可达 Beta 级。
