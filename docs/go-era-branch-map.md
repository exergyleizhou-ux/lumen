# Go 时代分支 → Rust 主线待办映射

> 背景：origin 上有约 55 个 2026-06 的 **Go 版 Lumen** 分支（当时代码在 `internal/*.go`，
> 归档 tip 在 `origin/archive/go-main`）。这些分支对现在的 Rust 树**无法 cherry-pick**，
> 但它们承载的问题清单、修复思路与验收标准仍然有效。此表把这些结论固化下来，
> 分支本体随时可以删除而不丢知识。
>
> 状态列：✅ Rust 线已有等价物 / 🔁 值得移植 / ⏸ 低价值或不适用 / 🧪 已在 2026-07-26 批次落地

## 安全 / guard（原 s3-pr1..pr5 + fix/guard-*）

| Go 分支 | 内容 | Rust 现状 |
|---|---|---|
| s3-pr1-threat-model | 威胁模型文档（注入=主威胁、信任边界、G1-G8 缺口表） | 🔁 Rust 线只有 masterplan 02-安全规格；值得写一份 agent 级 threat-model.md |
| s3-pr2-guard-property | guard 不变量 property 测试：`normalize(mutate(s))==normalize(s)`，200 变体×危险种子，verdict 永不翻转 | 🔁 lumen-guard 只有逐条规则单测；property/fuzz 层仍缺（bash.rs ~800 行规则表） |
| s3-pr3-sandbox-runner | Seatbelt/bwrap 命令沙箱 runner | ✅ xai-grok-sandbox（sandbox-exec deny network* 已在 child_net.rs） |
| s3-pr4-audit-jsonl | hash-chain 审计日志 + 持久 JSONL | ✅ 部分等价（unified_log + cache_request_evidence.jsonl）；hash-chain 概念未移植 ⏸ |
| s3-pr5-injection-ssrf | untrusted-wrap + SSRF 防护（解析后 IP 校验、metadata IP 黑名单） | 🔁 web_fetch 的 SSRF 预检查值得对照 xai-grok-tools web_fetch 现状核一遍 |
| fix/guard-{destructive-gaps,pipe-to-shell,strip-hidden-chars,sensitive-write-paths,home-data-dir-rm} | 具体规则缺口（rm 家目录变体、curl\|sh、零宽字符、敏感写路径） | ✅ lumen-guard 已覆盖同类（bash.rs/writepath.rs/hidden.rs）；🧪 2026-07-26 又加了 UNSAFE 旁路审计 |

## TUI / 渲染（fix/tui-*、fix/render-*、fix/lineedit-*、fix/paste-*）

| Go 分支 | 内容 | Rust 现状 |
|---|---|---|
| fix/tui-chat-scroll-and-statusbar-overflow | autoscroll 清空聊天区 + 状态栏换行溢出 | ✅ Grok pager 无此实现层；对应关注点=当前 NextGen 执行书的 PTY 矩阵验收，仍须独立跑全 |
| fix/tui-tool-row-coalesce-and-verify-skip | 工具 dispatch/result 合并成单行 + verify 超时→skip | ✅ pager 已有等价 UX；verify-skip 语义在 lumen-verify runner（missing tool=SKIP） |
| fix/render-{markdown,highlight,underscore-italic}-correctness | 渲染正确性（标题样式、apostrophe 吞字、下划线斜体） | ⏸ pager 的渲染栈完全不同（ratatui + xai-grok-markdown），bug 不可迁移 |
| fix/lineedit-wrapped-cursor | 折行光标数学（真 PTY 验证） | ⏸ pager 自有编辑器；概念保留：光标数学必须真 PTY 验收 |
| fix/paste-{flood-and-silent-turns,lifecycle-asker-goroutine} | 粘贴洪泛防抖 + asker 生命周期 | 🔁 值得在 pager 输入层验证一次大粘贴行为（Gate E 场景） |

## 成本 / token / 压缩（fix/cost-*、fix/token-*、fix/compact*、feat/provider-aware-cost）

| Go 分支 | 内容 | Rust 现状 |
|---|---|---|
| fix/cost-accuracy-cache-aware | 按缓存命中率计费（99% 命中不能按全价算） | ✅ CacheUsageTruth 三态 + provider-reported 才显示（conversation.rs:672-758） |
| fix/token-cost-cumulative-basis | 累计口径统一 | ✅ turn 级打点已有 |
| fix/token-estimate-images-schemas | token 估算计入图片+schema | 🔁 Rust 的 estimate_tokens=bytes/4，CJK 高估 ~3x（cache_shape.rs:91-97）——已列入待修 |
| fix/compact{ion-summary-budget-and-dead-knob,-truncate-rune-safe} | 压缩摘要预算 + CJK 安全截断 | ✅ compaction.rs 是上游成熟实现 + bump_log_rewrite 归因 |
| feat/provider-aware-cost | 按 provider 配置计价 | ✅ 33 模型目录自带 pricing 字段 |

## eval / 质量（feat/eval-*、test/eval-tasks-wellformed、feat/tool-profile-core）

| Go 分支 | 内容 | Rust 现状 |
|---|---|---|
| feat/eval-harness / eval-more-tasks / eval-json-repeat-latency | 破损工作区 eval + --json/--repeat/延迟 | ✅ evals/tasks 01-20 + eval-coding(-live).sh；🔁 缺的是把 eval-live 纳入例行回归（现在证据停在 07-16 的 deepseek-chat，且该别名已退役——需要在 deepseek-v4-pro 上重跑基线） |
| test/eval-tasks-wellformed | 任务集结构 CI 门 | ✅ eval-coding.sh 反向门（BROKEN=OK）已是等价物 |
| feat/tool-profile-core | 42-tool core profile 降 prompt 体积 | ⏸ Grok 工具目录结构不同；概念（工具面裁剪 vs 上下文预算）由 1M ctx + auto_compact 85% 吸收 |

## provider 鲁棒性（fix/anthro-*、fix/gemini-*、fix/stream-recovery-*、fix/default-model-resolution、fix/mcp-*）

| Go 分支 | 内容 | Rust 现状 |
|---|---|---|
| fix/anthro-tool-block-wire-format | Anthropic tool_use 结构化 wire 格式 | ✅ sampler 有独立 Messages 序列化路径（client.rs 三路径采样验证） |
| fix/gemini-block-and-cancel-robustness | 安全拦截/取消不静默 | 🔁 通用原则：provider 异常必须响亮。Rust 侧对非 DeepSeek/xAI provider 的 live 验证=0（BYOK 目录 33 模型大多未 live 烧机）——发布叙事里要如实标注 |
| fix/stream-recovery-preserve-partial | 流恢复保留部分文本 | ✅ 上游 sampler 有 attempt/recovery 机制（turn.rs attempts 打点） |
| fix/default-model-resolution | 按名或 model 字段解析默认 | ✅ cli_models.rs:182-247 |
| fix/mcp-client-registry-race-and-leak | MCP 注册表并发竞态+泄漏 | ✅ 上游 xai-grok-mcp 成熟；⏸ |

## parity 门待办（parity-run.sh 自评部分的硬化）

`policy/CC_PARITY.md` 41 行与 `policy/parity_scenarios.json` 12 场景目前是自报状态。
硬化路径：给每个场景补一个可执行断言（现有真实断言=guard/discipline cargo test + 6 条结构 grep）。
优先绑定：storm-breaker 注入（S07）、delivery 提醒去重（S08）、goal 门（S09）—— 2026-07-26 批次已为这三者补了 crate 级测试，可直接引用测试名。

## 处置建议

- 分支本体：保留 `origin/archive/go-main` 一个即可，其余 Go 分支可在 GitHub 上批量删除（内容已被本表吸收；tag v1.x 资产不受影响）。
- 本表由核心线维护；science 线相关分支（codex/science-fusion-full 等）不在此表范围。
