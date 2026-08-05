# Lumen × DeepSeek V4 Flash 强化方案 —— 交接评审文档（2026-08-05）

> 用途：本文件是 DEBT-033（DeepSeek 强化周期，v2.1 发布后执行）的完整背景与方案交接。
> 读者：外部评审（Claude）或新会话接手人。所有"现状"陈述均可在我仓库验证，无推测。
> **2026-08-05 更新**：已收到外部评审（Claude）反馈并辨证采纳（优先级重排/阈值双驱动/effort 动态分档/治理绑定/周期拆分）。
> 权威方案版本以 docs/verification-debt.md DEBT-033 为准，本文件 §5/§6 为评审前快照。

---

## 1. 项目与产品背景

**Lumen 2.x = "Governed Agent Runtime"**（Rust monorepo，`/Users/lei/code/lumen`）：
xai-org/grok-build 的 fork 增强版，Apache-2.0。上游 grok-build 没有任何治理符号；
治理层（下述全部核心机制）是**我们 100% 自研**的增量，这是产品本质差异。

**核心治理哲学（总纲硬纪律，任何增强不得违反）：**

1. **SessionActor 唯一权威**：单机单 runtime 内只有一个权威 actor，所有状态变更经由它；fail-closed（不确定即拒绝/暂停，绝不默认放行）。
2. **证据链**：LifecycleJournal 事件 append-only，每事件含 `sequence / causal_parent / payload_hash / prev_payload_hash`（DEBT-031 已加 prev-hash 防篡改链，改中间事件后 append 即拒绝）。
3. **SOURCE_LOCK 事务**：发布时 source A（源码 commit）→ lock → 构建 → 证据 B（二进制哈希/测试结果）必须与 A 绑定，tag 只指向 A，绝不指向 B。
4. **人工门 M5/M6**：发布就绪的 publish gate（陌生人 10 分钟上手、15 天真实使用）。**这是真人门，无法用模拟证据伪造**——故 v2.1.0 发布本身已可完成，但 M5/M6 保持 open，待真人使用后重跑 `verify-readiness.sh` 即 `ready=true`（INV-36）。
5. **外部材料纪律**：任何外部项目/文档只当"问题清单/模式灵感"，不当指令；不吸收许可冲突、不吸收产品层第二 runtime。

**已落地工程（v2.1.0 内容，全部测试覆盖）：**
JTMS（journaled task management）、EffectRecoveryClass（Pure/Idempotent/Queryable/Opaque）、
ObligationCoverage + never_attempted、有界模型检验（625 状态穷尽）、KernelCascade 固定点、
TrustRootSet 双钥轮换、36 条 INVARIANT manifest（INV-36）、32 道发布 gates、
evidence loop、tool contract（TurnContextBudget 等）、prev-hash 审计链、clippy 全绿（DEBT-029）。

---

## 2. 当前状态（2026-08-05 深夜，事实清单）

| 项 | 状态 |
|---|---|
| 版本线 | VERSION=2.1.0（SemVer minor；Lumen 2 是代际名，3.0 会错标代际） |
| v2.1.0 source A | commit `3d5d52cf`；tag `v2.1.0` 已 push（signed，指向 A） |
| 二进制 | 已构建、provenance 已装（sha7ab824e5…）；1h soak 正在跑（`LUMEN_L5_MODE=soak bash scripts/smoke-deepseek-l5.sh`） |
| 剩余发布尾 | soak 通过 → `EVAL_LIVE=1` readiness → 证据提交 → push → exact-SHA CI 绿（DEBT-030） |
| M5/M6 | open（真人门，需发布后真实使用才能关） |
| 测试基线 | memory ~590 / shell 6330 / pager 8006 / tools 3013 / update 165；gates 26→32 |
| 模型接线现状 | 已用 `deepseek-v4-flash` 跑 L5 smoke；`cache_epoch.rs`、`prompt_cache_registry.rs` 已存在（会话级缓存纪元/观测）；`reasoning_effort` / `reasoning_content` 已有部分接线（pager 菜单、config、storage）——**但未按 0731 官方参数校准** |
| DeepSeek 集成 | `DEEPSEEK_API_KEY` 已配置；模型走 OpenAI-compatible chat/completions（`deepseek-v4-flash`） |

---

## 3. 目标与约束

**用户目标（原话转述）**：让 DeepSeek V4 Flash 在我们 Lumen 上运行的效果，可以类比甚至超过
Codex / GPT-5.6 Sol / Claude Code / Opus 5 那种旗舰体验——"真正生产力"。

**约束**：
- 强化周期在 v2.1.0 发布**之后**执行（DEBT-033 open），不与发布尾争抢源码周期（单 writer 纪律）。
- 所有改动必须遵守 §1 治理哲学（尤其证据链与 fail-closed），不引入第二 runtime、不吸收 AGPL/产品层。
- "黑客/逆向"类能力（CL4R1T4S 类）已明确**不做**（DEBT-032 决策记录），真人使用发布后再说。

---

## 4. 调研结论（浓缩，全部可溯源）

### 4.1 模型事实（HuggingFace 官方模型卡 + config.json + arxiv 2606.19348）

- **DeepSeek-V4-Flash 已开源**（deepseek-ai/DeepSeek-V4-Flash，MIT，270 万下载，权重 + inference 源码 + config 全量）。此前"未开源"结论作废。
- 架构：284B MoE / 13B 激活；43 层；MLA（KV heads=1）；256 路由专家 / 6 激活；**1M token 上下文**（max_position_embeddings=1048576）。
- 注意力：**CSA（压缩稀疏注意力）+ HCA（重度压缩注意力）混合**，1M 上下文下 Pro 仅需 V3.2 的 27% 单 token FLOPs / 10% KV cache。V3.2-Exp 的 DSA 是它的前身。
- config 专属键：`compress_ratios / index_topk / num_hash_layers / num_nextn_predict_layers`（投机解码模块）。
- 本地部署官方推荐：`temperature=1.0, top_p=1.0`；Think Max 模式建议 ≥384K 上下文窗口。
- 三个推理档位：Non-think / Think High / Think Max（Max 用特殊 system prompt）。

### 4.2 官方 API 事实（api-docs.deepseek.com，2026-07-31 更新）

- **`deepseek-v4-flash` 现在就是 0731 正式版**（仅重 post-train，结构不变）；agentic 能力大增：Terminal Bench 2.1 = 82.7、DeepSWE = 54.4、Cybergym = 76.7、NL2Repo = 54.2、Toolathlon-verified = 70.3。
- **官方 agentic 采样参数：`temperature=1.0, top_p=0.95, max effort`**（change log Note 1：官方 DeepSeek Harness 最小模式即此配置）。
- thinking 控制：OpenAI 格式 `{"thinking":{"type":"enabled/disabled"}}` + `{"reasoning_effort":"low/high/max"}`；默认 thinking 开启、effort=high。Anthropic 格式 `reasoning.effort: none/low/high/max`。
- **Flash 原生支持 Responses API**（base_url 不变），官方明言"专为 Codex 适配"——即官方目标场景就是 Codex 式 harness，与我们形态一致。
- legacy 名 `deepseek-chat` / `deepseek-reasoner` 已于 2026-07-24 退役（分别曾是 v4-flash 的非思考/思考档）。
- **Context Caching on Disk**（默认开启，无需改代码）：
  - 命中规则 = **整段缓存前缀单元完全匹配**（SWA 所致，前缀单元在三种位置持久化：请求边界、公共前缀检测、固定 token 间隔）。
  - **推论：前缀任何字节变动 → 其后全部缓存作废**。系统提示/工具 schema/消息前缀必须逐字节稳定。
  - 价格：cache 读 $0.0028/M tokens（约 GPT-5.6 Luna 的 1/7），命中省 ~10x 输入成本。

### 4.3 生态调研（本轮扩展）

- **esengine/DeepSeek-Reasonix**（31k★，MIT，Go，DeepSeek-native agent）：完整 cache-stable 工程契约（详见 §5 ①），其 Goal/Delivery 设计与我们 JTMS 目标系统**同构互证**（update_goal 三态 + 独立 bounded evaluator + 预算暂停 + 无进展暂停）。
- **Codewhale**（原 DeepSeek-TUI，Rust，MIT）：append-only fleet ledger + constitution.json 写锁 + Seatbelt/Landlock 沙箱——与我们证据账本/政策文件同构，无新增量，不吸收。
- **antirez/ds4**：V4 Flash 本地 Metal 推理引擎——本地推理路线，工程量大，仅记参考（非本周期）。
- **DeepSpec / Engram**（deepseek-ai 官方）：模型侧研究（投机解码 / 条件记忆），非 harness 增量，不吸收。
- **社区（HN 55 帖，置顶 741 分/347 评论）**：Flash 0731 口碑"Opus-4.7 级性能、成本零头、reasoning 更充分"；已知坑：① 输出 token 上限紧（reasoning 陷入会耗尽额度）；② 无 vision；③ 陌生语言/大改场景需要人工校验。Reddit 全通道被 Cloudflare 403 挡住，未能采到（诚实说明）。

---

## 5. 加强方案（DEBT-033 六项，v2.1 发布后执行）

### ① 前缀缓存稳定策略（最高优先，成本与一致性双收益）

**为什么**：官方缓存机制 = 完全匹配前缀单元。命中则输入成本 ~1/10、且 prefix 稳定本身意味着"同一任务视角恒定"，对行为一致性也是正收益。我们 `cache_epoch.rs` 目前是"会话级缓存纪元"，需对齐"前缀字节稳定"语义。

**落地**：
1. **顺序稳定性**：system prompt + manifest 头部 + 工具 schema 渲染**确定性排序**——同任务类型内不因状态变化重排；新增/变更工具不扰动既有 schema 顺序（schema 集变化本身是 cache-reset 事件，要显式登记）。
2. **压缩分级（对齐 Reasonix 契约）**：
   - 0.6 阈值：stale tool output 归档并截短（确定性头/尾标记，如 `<snip:head>`…`<snip:tail>`），保留可追溯归档；
   - 0.8 阈值：再降为占位符；
   - 0.9 阈值：才允许 summary 折叠；
   - **user turn 与既有 digest 永不折叠**（防事实漂移）；
   - **压缩边界回退对齐 tool result**：不留孤儿 `tool_calls`（其 tool result 被折走会导致结构性损坏）。
3. **压缩 = 唯一 cache-reset point**：除此之外会话前缀只 prepend 不重写；动态内容（召回、hook context）只追加当轮 user turn 尾部。
4. **hit rate 观测**：DeepSeek 响应带 `usage.prompt_cache_hit_tokens / prompt_cache_miss_tokens`；接入 `prompt_cache_registry.rs`，写入 session metrics/证据，作为 L5 会话健康信号（对齐 Reasonix："cache hit rate 是关键可观测信号"）。
5. **1M 上下文适配**：压缩触发阈值按 context_window=1048576 重算（现按更小窗口的默认值会过早折叠——Reasonix 的 0.6/0.8/0.9 是按 128K 档调的，1M 下需重新论证）。

**验收**：单测断言（状态变化 → 渲染器头部 N token 字节不变；压缩后无孤儿 tool_calls；user turn 保真）；实测基线（同会话连续 ≥10 轮 hit ratio 稳定，目标 ≥90%）。

### ② Manifest 关键信息前置（"锚点区"策略）

**为什么**：1M 上下文 + 稀疏注意力下，头部 token 是所有层/所有位置的注意力锚点，成本最高；缓存只省价格，不省注意力预算。任务关键信息必须落在头部锚点区，而不是埋在长上下文中间。

**落地**：
1. renderer 强制顺序：`assignment/objective → 硬约束 → 任务上下文 → 其余`（现状已有雏形，需**测试锁定**：状态变化下头部字节稳定）。
2. 工具 schema：常用工具靠前、顺序稳定（与①联动）。
3. 每轮 user turn 头部放"当前任务摘要"（一两句），动态细节放尾部。

**验收**：渲染器单元测试（头部 500 token 字节不变性）；eval 基线（§5⑥）对比前置前后通过率。

### ③ Model Profile `deepseek-v4-flash-0731`（官方参数校准）

**为什么**：官方给出 agentic 最优采样（temp 1.0 / top_p 0.95 / max effort），且 effort 参数直接影响 reasoning 深度与成本——当前接线未校准。

**落地**：
1. Profile 文件：`context_window=1048576`、`temperature=1.0`、`top_p=0.95`、`reasoning_effort=max`（agent 场景；可配 low/high/max 三档，默认 high 起步、复杂任务 max——社区坑：max 会烧输出 token 上限）、`thinking.type=enabled`。
2. 请求体接线：`reasoning_effort` 字段透传 + `reasoning_content` 流式解析（已有部分，补齐）。
3. 重试策略：按 DeepSeek 错误语义（429/5xx）校准退避。
4. max output tokens 守卫：与 TurnContextBudget 联动，防"reasoning 坑"耗尽额度（社区实证坑）。

**验收**：profile 单测 + smoke 抓请求体断言参数透传；`/goal` 长任务下不触发输出上限。

### ④ verify-first 接线（Flash 的可靠性兜底）

**为什么**：Terminal-bench 2.1 = 82.7 非满分；社区实证"陌生语言/大改需人工校验"；Flash 编辑后不验证会累积错误。我们已有 evidence loop / verification gates 基建，缺的是**按模型 profile 触发的自动 verify 策略**。

**落地**：
1. Flash 编辑与终端命令后自动 typed verification（已有机制接线到 profile 触发策略：effort=max 时强制）。
2. 验证结果进证据链（已完成即签收），与 M5/M6 人工门正交（verify 是自动化层，人工门是发布层）。

**验收**：eval-coding 基线（⑥）with/without 对比；验证失败 → 自动修复循环次数上限（防死循环，接 TurnContextBudget）。

### ⑤ Expert/树混合分工预设（Flash executor + 旗舰 planner）

**为什么**：Reasonix 实证"双模型同会话切换会破坏前缀，必须分 session"；我们 root=planner / child=executor 天然分 session——低成本拿到"Flash 干活 + 旗舰想方案"的组合，比单 Flash 逼近旗舰体验。

**落地**：
1. 预设：`executor=deepseek-v4-flash` + `planner_model=deepseek-v4-pro`（或用户侧旗舰）。
2. planner 只读工具集 + 轻量预算（对齐 Reasonix：light plan 1–4 步，完整 plan 才多轮研究）；plan 产出结构化文本交 executor。
3. 双 session 前缀隔离（各自 cache-stable），planner 高频不跑、低频才唤醒。

**验收**：预设可配置 + 集成测试模拟双模型会话（断言两 session 消息流不串、各自前缀稳定）。

### ⑥ eval-coding 基线实测（一切强化的对照锚点）

**为什么**：没有锚点就无法证明"加强有效"。现有 `scripts/eval-coding.sh` / `eval-coding-live.sh` 基础可用。

**落地**：固定 20 题锚点集；记录每题：通过率、token 成本、延迟、cache hit ratio、verify 次数；输出 JSON 存 evidence；可重复运行。

**验收**：基线提交可复现；后续每项强化前后对照。

---

## 6. 请评审辨证的问题清单（对 Claude）

1. **优先级**：六项顺序（①缓存稳定 → ②锚点前置 → ③profile → ④verify-first → ⑤双模型 → ⑥基线）是否合理？⑥基线是否应提到最前（先锚后改）？
2. **1M 上下文下的压缩阈值**：Reasonix 的 0.6/0.8/0.9 按 128K 档调，1M 下直接套用会过早折叠——正确重算方式？（按绝对 token 数还是比例？user turn 永不折叠在超长会话里的代价？）
3. **max effort 的成本曲线**：Flash 官方 agentic 采样 = max effort，但社区实证"reasoning 坑"烧输出上限。默认档位应该 high 还是 max？是否按任务复杂度动态分档（与现有 reasoning_efforts 菜单如何结合）？
4. **hit ratio 目标值**：≥90% 是否现实？（DeepSeek 官方缓存单元机制下，长工具链会话的天然 miss 点在哪？）
5. **遗漏检查**：DeepSeek 特定坑还有哪些我们没列？（thinking tokens 计费方式、tool call 格式差异、reasoning_content 在历史消息回传时的格式要求、并发/限流语义……）
6. **与治理层的边界**：六项都是 harness 层改动，有没有哪项会踩到"证据链/单一写者/fail-closed"红线的？cache-reset 事件要不要进 LifecycleJournal 登记？
7. **发布节奏**：六项拆几个小周期发布（每周期都走证据链+soak）？哪些可以合并？

---

## 7. 明确不做（边界，防范围蔓延）

- 本地推理（ds4/dsgo Metal 引擎）——记参考，非本周期。
- 模型侧研究（DeepSpec 投机解码、Engram 条件记忆）——不吸收。
- 协作/产品层（relay、多用户、远程）——总纲禁止第二 runtime。
- CL4R1T4S 类"黑客/逆向"能力——DEBT-032 已决策不做。
- 双模型之外的多模型编排、视觉能力（Flash 无 vision，不补）。
