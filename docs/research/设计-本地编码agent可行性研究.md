# 研究设计文档：本地小模型作为编码 Agent 的可行性前沿 —— 一项受控、笔记本可复现的失败模式测量研究

> 状态：执行级设计稿（principal investigator final design）。每一条 load-bearing 主张已对照 `~/lumen` 源码核验。本 doc 直接驱动下一步写代码 + 跑实验。
>
> 一句话定位（写进 abstract 的那句，且只敢说这么多）：
> **我们给出第一份"笔记本可复现、端到端 test-scored"的本地编码 Agent 失败模式测量研究 —— 在一台 24GB MacBook Air 上、对一个 12B 级本地模型、在小型单文件 Go 修 bug 任务上 —— 并用 Agent 自身事件流定义了可机器检测的失败分类学，实证显示在该任务集上"非能力性"失败的主导类是部署时的 context-overflow 指令丢失（固定 tool-schema 前缀溢出小窗口 → system prompt 滑出 → 模型退化为打招呼），它 pre-flight 可检测、且仅改 context budget 即可修复（同模型同任务 0/N → k/N）。** 贡献是测量 + 可操作检测器 + 因果 before/after，**不是新机制，也不是又一个终端 Agent**。

---

## 第 0 节：诚实的总体判断（先把丑话说前面）

把 reviewer-2 和 completeness critique 的结论压成一句：**按 seed hypothesis 当前的措辞与证据，会被 REJECT。** 原因：(a) 机制是公认 folklore；(b) "first" 没清两篇 novelty-killer；(c) 失败分类学是核心贡献，但当前 harness 根本测不出来；(d) 唯一的 5/6 是 n=1 repeat、CI 宽到 [0.44, 0.97]、且"6/6 ceiling"是 select-on-outcome；(e) 8 个任务是 ≤21 行 CS-101 经典 bug，几乎确定被训练数据污染；(f)"frontier"这个词在当前 scope 下是范畴错误（n=1 模型、n=1 机器、玩具任务）。

**但它可以被救活 —— 通过 rescope，不是通过 hand-wave。** 救活后的可发表形态是 **workshop / 技术报告级别的单模型案例研究**，其确定能站住的贡献是：一个可操作、事件流可检测的失败分类学 + overflow 的因果 before/after + 检测器验证。本 doc 把"能站住的最小版本"和"想扩展但被 gated 的部分"显式分开，每一处都标注证据强度。

**工作量分布的诚实陈述：约 60% 是 harness 改造（第 6 节），eval 跑数是便宜的部分。"今晚就能跑"只在改造落地且单测通过之后才成立。**

---

## 第 1 节：研究问题 + 可证伪假设

### 1.1 研究问题

云前沿厂商没有动机研究、却 under-served 且 defensible 的问题：**genuinely-local（7–13B 量化、24GB 消费级硬件）模型作为端到端"读-改-验"编码 Agent 的可行性边界在哪里，且当它失败时，失败是"能力不足"还是"部署时 context budget 配置不当"？**

### 1.2 核心自变量的正确操作化（这是全 doc 的方法论枢轴）

seed hypothesis 说"可行性主要由 (context window ÷ tool-schema budget) 之比支配"。**但绝对窗口大小是 Lumen 自己 tool belt 的产物，不是模型属性**（reviewer-2 objection #2）。因此把 IV 操作化为**harness-不变的无量纲比值**：

```
ρ = first_turn_prompt_tokens / server_context_window
```

- 分子 `first_turn_prompt_tokens` = **provider 上报的第一个 UsageKind 事件的 PromptTokens**（ground truth），**不是** `estimateTokens`（`agent.go:1027`，chars/3，CJK-blind，仅作 server-free 预测器 + 截断 sanity check）。
- 分母 `server_context_window` = **LM Studio `-c` load 参数**（进程外，手工记录的轴 —— 见 §1.4 的硬限制）。

唯有用 ρ，"8k×core" 与 "16k×full" 这两个绝对 token 不同、但 ρ 都 ≈1 的 cell 才能检验：**cliff 落在同一个 ρ（模型属性）还是跟着绝对 schema tokens（Lumen 属性）？** 这正是 §4a 里 16k×full cell 的判别作用。

### 1.3 H1（主假设，可证伪）

**H1.** 对该任务集上的此本地编码 Agent，端到端 pass-rate 在部署时由 ρ 支配：当 ρ→1 及以上时 pass-rate 出现 cliff（软带，非锐阈），且**该 cliff 区的失败被 overflow 类（zero-tool-call + greeting + 首轮 prompt ≥ server window）主导，而非 reasoning-incapacity 类**；该失败仅改 context budget（增窗口或缩 schema）即可恢复 —— 同模型、同任务。

**子假设：**

- **H1a（headroom 可分离性 / 模型属性判别）.** cliff 在 ρ 轴上是不变的：8k×core 与 16k×full（ρ 都 ≈1、绝对 token 不同）以**相同的失败类**collapse。若成立 → 模型属性；若 cliff 跟绝对 schema tokens 走 → Lumen 属性，必须如实改写为"该 harness 的 prompt-budget profile"。
- **H1b（两机制可互换性）.** 在固定 ρ 下，增窗口与缩 profile 给出统计上不可区分的 pass-rate（McNemar 配对）。
- **H1c（两区制，比 H1 更可能为真、也更可发表）.** 前沿有两个区制：低 ρ 处的**能力地板**（小模型如 1.5B 即使 headroom 充足也因真实推理极限而失败）+ 其上的**overflow 带**。即 "overflow 主导的是那些本来有能力的模型的失败"。

### 1.4 杀死 H1 的判据（pre-register，照实报）

- **F1（窗口非杠杆）.** 若 16k×core 与 8k×core 失败率统计上不可区分（Wilson CI 重叠）→ 窗口不是杠杆。
- **F2（是能力/延迟非 overflow）.** 若 8k×core 的失败**不是**zero-tool-call/greeting 类，而是 wrong-edit / max-steps / timeout → 失败源是能力或延迟。（baseline 已含此污染：05-counter-race 是 **5 分钟 turn-timeout**，非 overflow 非 reasoning，孤立跑 9/9 通过 —— `eval-baseline.md:104-125`。分类学必须干净隔离，否则"主导失败"主张被搅浑。）
- **F3（profile-specific 非干净窗口杠杆）.** 若 8k→16k 在 full 下不动 pass-rate、在 core 下动 → 看 2×2 交互项。
- **F4（杠杆不可互换）.** 若 32k×full 不恢复 → "缩 schema 或增窗口可互换"为假。
- **F5（分类学 kill-criterion，最硬的一条）.** **若在 headroom<0 的失败里，reasoning 类失败率 ≥ overflow 类失败率、且 CI 不重叠 → H1 被证伪。** 若 harness 无法干净分离两类（见第 6 节）→ H1 as stated 不可测，必须收窄。

**核心硬限制（贯穿全 doc）：** 真正支配 baseline 0→5/6 的是 LM Studio `-c`，它**进程外**，Go harness 读不到、设不了、扫不了（OpenAI `/v1/models` 不暴露，`eval-baseline.md:55`）。`cfg.Agent.ContextWindow`（`config.go:71`，默认 128000，`agent.go:182`）是**另一个东西** —— 只喂 compaction/preflight 阈值（`agent.go:1053,1088`），从不发给模型服务器。**两个"context window"在所有 prose 里必须严格区分；混淆即令 headline 失效。**

---

## 第 2 节：诚实的 novelty 定位

### 2.1 相关工作 + 它们留下的精确空白

| 工作 | 覆盖了什么 | 留下的空白（我们填的） |
|---|---|---|
| **SWE-bench / Lite / Verified / Pro**（Jimenez et al.; arXiv 2509.16941） | 主流端到端编码 Agent benchmark，test-green 计分 —— Lumen harness 正是镜像此法（`eval.go` 跑 `go test ./...`） | 几乎全是云前沿模型；小开源模型行（SWE-Dev-7B 23.4%、32B 36.6%）是**裸 pass-rate，无失败归因、无本地硬件约束、无 context/schema budget 仪表**。从不问失败是 reasoning-incapacity 还是 context-overflow。 |
| **TinyLLM**（Haque et al., arXiv 2511.22138, 2025-11） | 最近邻：小模型、agentic、edge 硬件；报 TinyLlama 不微调 0% 多轮；综述 SFT/PEFT/RL/DPO 补救 | 测 **BFCL function-calling accuracy，非端到端编码 pass-rate**；sub-1B/微调区制；补救是训练，非部署时 context-budget 杠杆。不隔离 context-vs-schema 比，无 overflow-instruction-loss 分类，无 7-13B 笔记本配方。**⚠ novelty-killer，必须读全文。** |
| **BFCL V4**（Patil et al., ICML 2025） | 可执行 function-calling eval 含小模型；小模型多轮 collapse（Qwen3-0.6B 1.38%） | 孤立测 tool-call 正确性，不驱动 Agent 到 test-green。**正因从不加载完整 ~42-tool schema，它在 context 限制下 PASS** —— 无法暴露只在满 tool-belt 下出现的 overflow 失败。它混淆而非测量我们的变量。 |
| **Lita**（Dai et al., arXiv 2509.25873） | 论证 scaffolding 遮蔽真实模型能力，提最小 harness —— 与"是模型还是 harness"邻近 | 瞄准前沿/大模型 SWE-bench；非本地/笔记本区制，非 context-vs-schema 为 IV，无本地失败分类学。其论点支持我们的 framing，但留下我们的前沿未测。 |
| **终端 Agent 工程文 + MCP token-opt / context-rot / lost-in-the-middle / LM Studio 截断文档** | SOTA 工程记述：有限 context、tool-schema bloat 致退化、sliding-window 丢 system prompt。**机制本身是 documented folklore。** | best-practice / 设计文，**非受控实证**。无人把编码 pass-rate 作为 context/schema 比的函数在本地模型上测量，无人给可证伪 before/after，无人隔离"system prompt 滑出 → fresh-chat greeting"这一小本地退化。**novelty 在测量 + 因果隔离，不在机制。** |
| **LOCA-bench**（arXiv 2602.07962）+ tool-augmented 失败分类学（Winston/Just, UW 2025, F1–F6） | 形式化 model-agnostic 失败分类学 + context **增长**下的受控研究 | 云规模、model-agnostic；增长工作研究**长 context 溢出** —— 与我们相反（小窗口被固定 schema 前缀在第一轮溢出）。不绑失败类到 ρ，不绑笔记本可复现。 |

### 2.2 我们填的精确空白（defensible claim，措辞要这么窄）

> 在所研究的任务集上，我们给出第一份 laptop-reproducible、端到端 test-scored 的 genuinely-local 编码 Agent 失败模式表征；用 Agent 事件流定义**可机器检测**的失败分类学；并实证显示该任务集上的非能力性失败由部署时 ρ→1 的 context-overflow 指令丢失主导，它 (a) 区别于 reasoning incapacity，(b) pre-flight 可检测，(c) 仅改 context budget 即可修复。**贡献 = 测量 + 因果隔离 + 可检测/可修复杠杆；不是新机制，不是又一个终端 Agent。**

### 2.3 不吹的 caveat（写进 paper，且当 headline limitation 不当 footnote）

1. **外部效度弱.** 8 个单文件、单 bug、单语言（Go）、green-test oracle 任务（≤21 行 CS-101 经典 bug）。非 SWE-bench 规模/多文件/多语言。当前 pass-rate 只描述**这一窄集**；"viability frontier"要泛化需更大更难多语言集。
2. **机制非 novel.** schema bloat 溢小窗口、lost-in-the-middle、sliding-window 丢 system prompt —— documented folklore。**声称"新失败模式"会被正确 reject。** novelty 严格是 empirical/measurement。
3. **headline n=1 模型 + n=1 机器.** gemma-4-12b 是**partial tool-tuned 通用模型，非 coder 模型**；13GB kernel-panic 与 memory-pressure prefill 是单台 24GB Air 特有。"laptop-reproducible"当前 = "在这台笔记本上可复现"。
4. **token 阈值是软带.** `estimateTokens` chars/3 低估 CJK；"~11k""8k cliff"是带非常数 —— frame 为 region。
5. **05-counter-race 是第三桶.** 唯一真实失败是 timeout 污染，既非 overflow 也非 reasoning；它只因被孤立重跑才暴露 —— 警示更大 grid 里 memory-pressure latency 会静默污染。
6. **位级复现不可能.** `chatRequest`（`openai.go:351-359`）**无 seed 字段** —— 即使 temperature=0.2 也不可位级复现，目标只能是 distributional reproducibility。

### 2.4 必须先核查的 novelty-killers（**未完成的前置动作 —— 写"first"之前必做**）

- **TinyLLM（arXiv 2511.22138）读全文.** 若它已把小模型 agentic 失败绑到 context/schema 比 **或** 报 edge 硬件上端到端编码 pass-rate → 核心主张塌成 replication。确认它仅 BFCL-accuracy + 微调。
- **"Rethinking Scale: Deployment Trade-offs of SLMs under Agent Paradigms"（arXiv 2604.19299, ACL industry 2026）读全文.** 直接讲 SLM 部署 trade-off；查其 trade-off 曲线是否已含 prompt-budget/context 杠杆、编码是否在 scope。
- **Aider polyglot 本地 leaderboard + Ollama/LM Studio/llama.cpp 社区 before/after 博文.** "raise -c or shrink profile" 是 folklore；非学术 write-up 可能抢先 empirical "first"。
- **BFCL V4 多轮/irrelevance 分析 + SWE-Pruner / context-pruning（arXiv 2601.16746）.** 确认 pruning 工作瞄准 tool-**output**/codebase token，**非**固定 tool-**schema** 前缀在第一轮溢出小窗口 —— 这个区别是"可检测可修复杠杆"贡献存活的唯一前提。

**纪律：如果任一 killer 命中，DROP "first measurement" framing，re-pitch 为仅"可操作事件流检测的失败分类学 + headroom 可分离性结果"。若连分类学都被抢先 → 承认，pivot 为工程报告。**

---

## 第 3 节：失败模式分类学（核心分析贡献）

**互斥、有序**（pre-register 顺序：overflow → timeout → harness-break → empty-stream → empty-final → max-steps → test-tamper → malformed-args → wrong-edit → no-tool-call → pass），每个失败恰落一桶。每类报**自身 Wilson CI**（注意它们 partition failures，非独立，不做未校正的两两显著性）。

> **工具计数纪律（completeness 抓到、三份 design 漏掉的真实正确性点）：数 `event.ToolResult`（`agent.go:585-598`，每次执行一次，对 Start/no-Start 分裂鲁棒），不数 `event.ToolDispatch`** —— OpenAI provider 可投递 finalized tool call 而无前置 `ChunkToolCallStart`（`openai.go:155,302,323`；`agent.go:444-447` 对 finalized-only 路径不 dispatch），Dispatch-based 计数会 undercount 并 false-positive no-tool-call 类。

| # | 类名 | 定义 | 检测规则（可计算） | 今日可测？ | 种子证据 |
|---|---|---|---|---|---|
| **F1** | **context-overflow-greeting**（headline 类） | 首轮 prompt 大于 server 真窗口，模型从未登记任务，产出泛化非任务回复（greeting），零工具零编辑 | `passed==false` ∧ `toolResultCount==0` ∧ `filesChanged==0` ∧ `firstPromptTokens ≥ realServerWindow`（即 ρ≥1）∧ finalText 命中 greeting 正则 `/^(hi|hello|how can i (help|assist)|what would you like|i'?m ready)/i` 或从不命名任务里的文件/符号 | **否** —— sink 仅听 UsageKind（`eval.go:173`），丢 ToolResult；首轮 PromptTokens 被 sum 进 `ctr.in`（`:175`）丢弃 | baseline 8k smoke：1 次 code_search 后中文 greeting，无编辑（`eval-baseline.md:30-37`） |
| **F2** | **no-tool-call**（能力/意图 miss，**非** overflow，控制类） | prompt 未溢出，模型产出实质 prose 但从不行动（描述修法/拒绝） | `passed==false` ∧ `toolResultCount==0` ∧ `filesChanged==0` ∧ `firstPromptTokens < realServerWindow` ∧ finalText 非空 ∧ **不**命中 greeting 正则 | **否**（同 F1 gap） | —— 区分"因 overflow 而 greeting"与"因聊天而非行动" |
| **F3** | **malformed-tool-args** | 发了 tool call 但 args 非合法 JSON / schema 反序列化失败 | 某 ToolResult 的 `Err!=''` ∧（Output 含 'were not valid JSON'（`agent.go:749-750`）∨ Err 匹配 `/^invalid args/`（`task.go:108`））。**substring 匹配，脆，标注之** | **否**（ToolResult 被丢） | BFCL 已知小模型 tool-call 脆性 |
| **F4** | **wrong-edit**（改了文件、check 仍红 —— 真能力 miss） | 改了非测试源文件但 `go test` 仍失败 | `passed==false` ∧ `filesChanged>0`（workspace pre/post diff 非测试文件）∧ 非 max-steps ∧ 非 timeout ∧ `Result.Err==''`。**排除 test-tampering**（F9） | **否**（无 workspace diff；仅有 protected-test diff `eval.go:146`） | —— |
| **F5** | **max-steps-exhaustion**（**今日不可见，最危险 blind spot**） | 耗尽 maxSteps 仍无 final，`Run` 返回 `nil`（`agent.go:630`）→ harness 看到干净完成但 check 失败，与 finish-but-wrong 无法区分 | 改造后：`StopReason=='max_steps'`。临时：sink 匹配 Notice `/max steps \(\d+\) reached/`（`agent.go:626`，脆） | **否** —— Notice 被丢；返回 nil | —— |
| **F6** | **turn-timeout** | 每轮 ctx deadline（默认 5m，`agent.go:321`）触发；`Run` 返 `ctx.Err()`（`:364`） | **改造后**：`errors.Is(rerr, context.DeadlineExceeded)`（在 `eval.go:150` stringify **之前**检查）→ `StopReason=='timeout'`。**消歧**：verify 步有自己的 deadline（`agent.go:805`），确保归因到 turn deadline 非 verify sub-deadline | **部分**（仅脆 substring 'context deadline exceeded'） | 05-counter-race：1 step 301.0s = 旧 5min（`eval-baseline.md:104-125`） |
| **F7** | **empty-stream**（provider/server 不可达 —— **infra 非能力失败**） | 0 chunk 流（`agent.go:517-520`）；24GB 笔记本上 LM Studio 崩/卸载/OOM 的合理 infra 模式 | `StopReason=='empty_stream'`（临时 substring 'empty stream (0 chunks)'）。**从能力分母排除或单独报** | **部分** | —— |
| **F8** | **empty-final-after-nudge** | nudge 后 final 仍空（`agent.go:528-531`，maxEmptyFinalBlocks=1） | `StopReason=='empty_final'`；佐证：'empty assistant response — nudging' Notice 计数 >0（`agent.go:1171`） | **部分** | —— |
| **F9** | **test-tampering**（anti-cheat 拒绝） | check 绿但改了 protected `*_test.go`，harness 翻为 FAIL（`eval.go:146-165`） | `ProtectedTestsUnchanged==false`；推荐升为 typed `Result.TestTampered bool`，勿解析 human-readable `out` | **是**（已实现、已单测 `integration_test.go:131`） | —— |
| **F10** | **harness/config-break**（非模型失败） | workspace copy 失败 / Configure 失败 / 非-DeadlineExceeded run error | `Result.Err` 前缀 `'configure: '`/`'copy: '`/非模型 sentinel 的 `'run: '`。**从模型能力分母完全排除，但报计数** | **是**（前缀已在） | —— |

**诚实 scoping：** F1、F5（不可见性本身）、F6 有真实种子证据。**F2/F3/F4/F7/F8 是已定义、检测可实现、但尚未观测**的类 —— paper 必须标"defined, detection implemented, awaiting observation"，不当 found 宣称。

**分类器验证（非可选）：** 每个分类器**程序化 + pre-registered + 互斥有序**；**盲手标 20% 失败，算 Cohen's κ vs 程序标签；若 κ<0.6 → 分类学太软，撑不起核心主张，照说。**

---

## 第 4 节：实验设计 + ablation 矩阵

### 4.0 自变量 / 因变量

**自变量：**

| IV | 水平 | 何处设 | 诚实状态 |
|---|---|---|---|
| **model size** | 1.5B/3B/7B/12B(有)/14B(risk) Qwen2.5-Coder + gemma-4-12b | LM Studio load（进程外） | **仅 gemma 在盘**；Qwen 全需下载（1–9GB）。14B@16k ≈ 13GB panic 线。 |
| **server context window** | 8192(fail-cell)/16384/32768 | `lms load -c N`（进程外，**手记轴**） | THE headline 变量，Go harness 读不到/设不了/扫不了。 |
| **tool profile（schema budget）** | micro(~12, ~3-4k tok)/core(42, ~11k)/full(118, ~25-30k) | `cfg.Tools.Profile` → `selectTools`（`tools_profile.go:44`，二元） | **仅 core/full 存在**；**micro 须新增**（~10 行 subset `coreToolNames`），否则 schema 轴仅 2 点 = 一条线。 |

ρ = first_turn_prompt_tokens / server_window（见 §1.2）。

**因变量：** pass-rate（test-green + anti-cheat）；**失败类分布 F1–F10（核心）**；firstPromptTokens / toolResultCount / filesChanged / StopReason；median+IQR+max wall-clock（本地真成本，钱≈$0）；per-task pass@k。

**固定控制：** `max_steps=30`，`temperature=0.2`（baseline 用此；committed toml 是 0.7 —— 取 0.2 并锁死），`verify.enabled=true`，**`turn_timeout="20m"`（每 cell 非协商 —— 默认 5min 硬编码 false-fail 了 05-counter-race）**，`[agent] context_window = server -c`（每 cell —— 否则 preflight 检测器在 128000 默认下**静默不触发**，`agent.go:182`），同 8 任务，`GOTOOLCHAIN=local`。

### 4.1 §4a —— 现在就能跑的最小版本（"Minimum Publishable Cell-Block"，零下载）

**gemma-4-12b × {8k, 16k} × {core, full} = 4 cells，repeat=5，8 tasks = 160 task-runs。** 仅 gemma 在盘；除 §6 instrumentation 外零代码（micro 此块不需要）。

|  | core (~11k) | full (~28k) |
|---|---|---|
| **8k** | 11k>8k → **F1 predicted**（已知 greeting 0/6） | 28k≫8k → **F1 strongly predicted** |
| **16k** | 11k<16k → **PASS**（validated 5/8） | 28k>16k → **判别 cell：full 在"大"窗口下仍 overflow？** |

**决定性 cell = 16k×full：** 若它以**与 8k×core 相同的 F1 类**collapse（同 ρ≈1、不同绝对 token）→ 直接证据：控制变量是 ρ/headroom，非窗口绝对大小、非能力（同一模型 16k×core 拿 5/8 却在 16k×full 塌，纯因 schema 前缀重新溢出）。这是**单模型、零下载的因果隔离**。

对角"iso-fit"（8k×micro ≈ 16k×core ≈ 32k×full，prompt<0.7·window）检验两杠杆**可互换性**（H1b，最强因果形式）。

**Wall-time：** 160 runs × ~150s，两个 full cell prefill ×2-3 → **≈ 5.5h**。**今晚可跑，是我锚定 paper 的结果。** 诚实限制：n=1 模型 —— frame 为"mechanism isolation"，**非"frontier"**。

### 4.2 §4b —— headline frontier grid（overnight，gated on 下载 + RAM）

要画 surface 需 ≥3 size 点 + ≥3 budget 点。

**核心 grid：{1.5B, 3B, 7B, 12B} × {micro, core, full} × {16k}，repeat=5 = 12 cells**（固定 16k —— 唯一全 size 可加载的窗口；schema-budget 作 headroom 代理）。
**加 headroom slice：** gemma-12b × core × {8k, 16k, 32k-if-loads} + 7B × {core, full} × {16k, 32k}（仅在不 panic 的两个模型上变窗口腿）。

总 ≈ 17 cells × 5 reps × 8 tasks ≈ **680 task-runs ≈ 11–14h overnight**，瓶颈是 wall-time + RAM ceiling，**非钱（本地≈$0）**。

**诚实门控：**
- **8k 从 grid 删除**，仅留作单个 documented F1 cell（已知失败，非测量点）。
- **32k & 14B 是 risk cell**：每次 load 前 `lms load --estimate-only -y` 门控；不 load 则报 **"infeasible on this hardware"——这本身是发现**（笔记本跨不过的边界）。14B@16k 坐在 13GB panic 线。
- **model-size 轴完全 gated on 下载**（1–9GB/个）—— size-frontier 是最可能保持不完整的部分。报能 load 的，余标 infeasible。
- full-profile cell（~28k prompt）prefill ×2-3。

### 4.3 "几个模型才算可信曲线"

跨一个数量级的 **3 个 size（如 3B/7B/12B）是单调曲线的可信下限** —— 2 点是线非前沿，reviewer 会 reject。1.5B 值一列作**负地板**（很可能低于 tool-calling 可靠性前沿 —— 有价值的"任何 budget 下都不可行"数据点，非废弃）。**published 曲线 4 size，bare floor 3 size。** 且为去除 family/tuning 混淆，size 轴用真 coder 家族（Qwen2.5-Coder），gemma 仅作已有锚点。

---

## 第 5 节：度量与统计

### 5.1 drivability metric：带区间，禁裸点估

每 cell pass-rate `p̂ = passes/n`，n = K tasks × R reps，**只与区间一起报**。

- **Wald 取消资格**（n 小时在 p̂=0/1 给零宽、可超 [0,1]）。
- **Wilson score interval 为默认**（小 n + 边界覆盖正确、留在 [0,1]）。每个 pass-rate（含 abstract、图注）**必带 n 与区间；裸"83%"禁用**。
- **Clopper-Pearson（精确）作保守伴侣**，仅用于阈值/frontier 主张（保证 ≥95% 覆盖，更宽 = 诚实方向）。

**强制诚实：** 当前 baseline "5/6" = `Wilson 95% CI [0.44, 0.97]` —— 与从掷硬币到近完美一致。**该区间就是 n=6 的发现：撑不起任何 ranking。** committed `eval-baseline.md` 的 "5/6 (83%)" headline 是 over-claim，**必须带 CI 重发**。

### 5.2 repeats（给定 60-100s/turn）

两个独立 n 轴别混：**K tasks**（泛化广度）vs **R reps**（this 模型 this 任务的随机性）。Wilson 半宽 @ p̂≈0.8：n=8→±0.24；n=16→±0.18；n=24→±0.15；n=40→±0.12；n=80→±0.085。**n<60 进不了 ±0.10。**

**推荐：K=8（逐步扩向 12–15），R=5 → n=40/cell，半宽≈±0.12**。Wall-time 40×~150s≈100min/cell + load，预算 ~2h/cell —— 这是 binding constraint，把研究锁成 frontier-mapping 非 leaderboard。R=5 而非更多：n≈40 后边际 CI 收缩不抵 wall-time；wall-time 预算花在**沿 ρ 轴更多 cell**，非单 cell 深 repeat。**R pre-register、看结果前定死**（看到抖任务再加 R = p-hacking）。

### 5.3 frontier 阈值 defensibly

**不**定义为"pass-rate>50%"或任何整数（researcher degree of freedom，邀请 gaming）。

> 一个 (model,config) cell **viable 当且仅当其 pass-rate 的 95% Clopper-Pearson 下界 > pre-registered 地板 τ**。

- **τ 来自 baseline 非品味**：用同 K 任务跑 no-op/single-shot/random-edit trivial agent，测其 pass-rate `p₀`（有些任务靠运气/trivial completion 过）；τ = p₀ 的 Wilson 上界。"viable" = **可证明优于没有 Agent**（最弱可发表、最难争）。
- **用 CP 下界**（非 p̂）令 frontier 保守：cell 仅在**证据**清地板时才越线 —— 直接防止从幸运 5/6 over-claim。
- **frontier 是 region 非 point**：pass-rate（含 CI 带）对 ρ 作图，frontier 是 CP 下界穿 τ 的 ρ-带；报为 ρ 区间（"viability 在 ρ≈0.6–0.9 间退化"），**禁假精度单点**。

### 5.4 不从 n=1 过度宣称（四点都进 paper headline）

1. **n=1 模型 = case study 非 population.** title/abstract 必说"a 12B-class local model on a 24GB laptop"，**绝不**"local models"。跨模型主张需 ≥3 size。
2. **K=8 是可信上限非优势.** 全单/小文件 Go 修 bug，窄相关分布 —— 只泛化到"small-Go-bugfix drivability"非"coding ability"。**per-task pass-rate 作主表（永不报 pooled 单数）**。
3. **任务难度是混淆非噪声.** pooling 未校准难度 → headline 取决于挑了哪些 toy。缓解：fit **mixed-effects / IRT 模型，task 作随机效应**，使 model-level CI 吸收任务异质性（n=8 任务下仍 shaky，标 exploratory）。
4. **"可修复"需受控对比非 anecdote.** reload-to-16k 当前是 **n=1 配对 anecdote**。要 claim "fixable" 跑**配对设计**：同 K×R 任务 8k vs 16k（余全固定），**McNemar 配对检验**（同任务）报配对 pass-rate 差 + CI。

### 5.5 在慢、非确定、热耦合笔记本上诚实报方差

- **分离两方差源**：within-task（R reps）vs between-task；重构聚合为 `map[task][]Result`（当前 `Summarize` flat slice 隐藏二者，`eval.go:104`）。
- **wall-time 报分布**：median + IQR + max（非裸 median），标 prefill(60-100s) vs decode(~2s TTFT) 分裂。
- **memory pressure 是系统混淆，run-order 与之相关**：baseline 两个 ~300s 任务"跑在最后内存最紧时"，counter-race 仅 in-sequence 失败、孤立 158s 过。缓解（pre-register）：**Latin-square 跨 reps 随机/平衡任务序**；**每 run 测记 free-RAM+swap 作协变量**，与内存 dip 共现的失败归类**环境**非能力；**timeout 隔离成自己的类**（`Turns==1 && Seconds≥turn_timeout`）；report **capability-ceiling(isolated) 与 as-run(sequential) 两个 pass-rate，均带 CI** —— **isolated 仅作 confound diagnostic，禁升为 headline "ceiling"（那是 select-on-outcome）**；固定/记录 thermal 状态（cell 间插固定 cooldown）。
- **报 seed/temperature 制**：temperature=0.2；**wire 无 seed 字段**（`openai.go:351-359`）→ 不可位级复现，剩余方差是非-seed（scheduler/memory），这强化"环境非模型"论证。

### 5.6 ρ 必须真被 sweep（否则假设未被检验）

H1 是因果的：pass-rate 由 ρ 支配。**不能用单一 ρ 检验 governing-ratio 假设。** deliverable 图 = **pass-rate（Wilson 带）对 ρ ∈ ~0.3–1.1**，overlay overflow 失败类率；假设预测 pass-rate 在 ρ→1 出 elbow，由 overflow 类（非 reasoning 类）驱动。**最大设计陷阱**：改 profile 也移除能力 —— core-profile 失败可能是"缺工具"非"overflow"。故 profile 与 window **独立全因子变化**，per-mode 分类学**必须分离"tool-missing"（F3/tool Err）与"overflow"（F1）**。

---

## 第 6 节：harness 改造（TDD-ready，每条带 file:line 落点）

**约 60% 工作在此 —— 必须落地且单测通过，"今晚能跑"才成立。** 全部 TDD-able against 现有纯单测（`eval.go` model-free，`integration_test.go:131`）。按优先级：

**P0（无此则核心贡献不可测）：**

1. **失败分类器进 `evalSink`**（`cmd/lumen/eval.go:171-181`）：另数 `e.Kind==event.ToolResult`（**非 ToolDispatch**，见 §3 纪律）→ `toolResultCount` + per-tool histogram；捕获**第一个** `UsageKind.PromptTokens`（→ `firstPromptTokens`，F1 的 ground-truth overflow 量，当前 sum 进 `ctr.in` 丢弃 `:175`）+ `maxPromptTokens`；监听 `event.Notice` 文本匹配 'max steps'（→ F5）/ empty / timeout notice。**单条最 load-bearing。**
2. **结构化 `StopReason` 进 `TurnDone`**（`agent.go:518,529,546,629` 现发裸 TurnDone）：exported enum `{finished, max_steps, timeout, empty_stream, empty_final}`。**唯一动 agent 代码的改 —— 把 F5 从不可见转为 recorded。** timeout 用 `errors.Is(rerr, context.DeadlineExceeded)` 在 `eval.go:150` stringify **之前**检查。
3. **run 后 workspace diff** → `FilesChanged []string`（复用 `ProtectedTestsUnchanged` 的 WalkDir，`eval.go:148/169`；确定性、无 agent 改）→ 分离 F2(no-edit) vs F4(wrong-edit)。

**P1（无此则结果不可归因/无统计）：**

4. **自描述结果**：`Model, Provider, ToolProfile string`，`ServerContextWindow int`（手记 `-eff-window` flag，harness 读不到真窗口），`Rep int`，token 计数 stamp 进 `eval.Result`（`internal/eval/eval.go:30-37`）+ JSON envelope（`cmd/lumen/eval.go:94-97`）。**今日 result 文件不记哪个模型产生 —— 复现致命。** 加 git-SHA + config-hash + temperature。
5. **per-task 聚合进 `Summarize`**（`eval.go:104`）：group by `Task`，报 pass-count/k + per-task stddev + **Wilson CI**。数据已逐 rep 收集，仅缺 grouping（当前 `cmd/lumen/eval.go:66-90` flat slice pool 混 task×rep）。
6. **`TestTampered bool` typed 字段**（F9）+ **`RunStatus` enum `{scored, configure_error, copy_error, run_error}`**（F10）—— 别 prefix-parse human-readable `out`。

**P2（sweep 便利 + schema 第三点）：**

7. **`-tool-profile` / `-context-window` eval flags**（`cmd/lumen/eval.go:24-30`）post-override `agent.Options`（值流经 `agentOptionsFromConfig` `config_apply.go:56-61`），令 profile sweep 一条命令。**NB：`-context-window` 只设 compaction 阈值，非模型服务器窗口 —— 文档化并与手设 `lms load -c` 配对。** 别过度建造（真窗口本就进程外）。
8. **micro tool profile**（`tools_profile.go`，~10 行）：`microToolNames` subset = `read_file, write_file, edit_file, multi_edit, ls, glob, grep, bash, todo_write, complete_step`（不可约的读-改-验带）+ 第三 `case`。仅 §4b grid 需，§4a 不需。混淆 flag：schema count 与 verbosity 纠缠 —— subsetting 单调变 token 是公平构造。

**改造完成的验收门：** 每个分类器有单测；ToolResult 计数对 finalized-only 路径鲁棒（伪造无 Start 的 tool call 用例）；StopReason=max_steps 用例（伪造耗尽 step）；firstPromptTokens 捕获用例；workspace-diff 确定性用例。

---

## 第 7 节：执行协议（分阶段）

### 阶段 0 —— 现在就能跑、出第一张图（约 1–1.5 天）

1. **落地 P0 改造（#1–#3）+ 单测绿。** 这是"今晚能跑"的真前提。
2. **设 `turn_timeout="20m"`、`temperature=0.2`、`context_window=server-c`、profile=core；committed `eval.toml` 锁这套**（注意 committed `lumen.toml` 是 deepseek/full/temp0.7 ≠ 实验配置 —— 从干净 checkout 跑复现不了 baseline）。
3. **跑 §4a 2×2**：gemma × {8k, 16k} × {core, full}，repeat=5，每次 load 前 `lms load --estimate-only -y` 门控，跑前后采样 free-RAM/swap，Latin-square 随机任务序。
4. **产出第一张图**：pass@5（Wilson 带）对 ρ，overlay F1 类率；+ **检测器混淆矩阵**（preflight WARN fired × F1 observed）；+ 8k→16k 的 **McNemar 配对**（parallel 须同设 `--parallel 1`，见 §9 缓解 —— 否则混淆 context 与 parallelism）。
5. **盲手标 20% 失败算 κ。** κ<0.6 → 停，修分类器。

**交付：** 单模型 overflow 因果隔离 + 检测器验证 —— 这是确定能站住的 finding。

### 阶段 1 —— 下 Qwen-coder 谱系填 model 轴（overnight，gated）

6. 下 Qwen2.5-Coder {3B, 7B}（+ 1.5B 负地板，+ 14B risk）。每个 **pin GGUF SHA256 + quant + LM Studio model key**。
7. 落地 P1+P2 改造（#4–#8，含 micro profile）。
8. 跑 §4b core grid（{1.5B,3B,7B,12B}×{micro,core,full}×16k）+ headroom slice。每 risk cell `--estimate-only` 门控，infeasible 如实报为边界。
9. 出 frontier surface（pass@5 等高线 + viability contour @ CP 下界穿 τ + overflow 带阴影 + kernel-panic 硬墙）。检验 **H1a ρ-不变性 / H1c 两区制**。

### 阶段 2 —— 污染控制 + 云 baseline 校准（半天，**决定 capability 轴是否可信**）

10. **污染探针**：每任务 prompt（无工具无 harness）作纯 completion 喂每个模型，看是否 unprompted 吐 canonical fix。高 recall = 污染。**作 paper 一张表。**
11. **去 canonical / held-out 任务**：≥几个 rename 符号、嵌非显然控制流、非显然 domain 的变体，使 pass 须读 this code 非 recite pattern。
12. **云校准列**：同 8 任务 × repeat 跑 DeepSeek（committed default，~15min ~$0.01），**标"reference, not local"** —— 证任务集 discriminative（云 8/8 而 gemma 5/8 = 校准 ceiling；云也栽 = 任务坏）。
13. **若污染探针高 recall：DROP 所有 capability/size-frontier 主张（§4b），scope 收到单模型 overflow 结果（§4a 抗污染：prompt 溢出时连 memorized fix 都发不出，F1 仍成立）。**

---

## 第 8 节：可复现包 + writeup 骨架

### 8.1 可复现包（今日几乎为零，须建）

- **pin 模型 digest**：唯一身份是 "google/gemma-4-12b Q4 7.56GB"（`eval-baseline.md:13`），**无 hash、无 quant-file SHA256、无 GGUF id**（"Q4" 歧义：Q4_0/Q4_K_M/Q4_K_S 有别）；`docs/local-models.md:62` 甚至列了不同模型名。**pin 确切 GGUF SHA256 + LM Studio model key + quant。**
- **自描述结果**（第 6 节 #4）：Model/Provider/Profile/ServerWindow/temperature/git-SHA/config-hash 全 stamp。
- **承认位级不可复现**：wire 无 seed → 目标是 distributional reproducibility，writeup 明说。
- **committed `eval.toml` per-cell + sweep manifest + 一键 runner**，stamp git-SHA+config-hash 进输出。**committed `lumen.toml`（deepseek/full/temp0.7）≠ 实验配置 —— 修。**
- **env 捕获**：OS、RAM、LM Studio 版本、free-memory trace、thermal、确切 `lms load` invocation + server health per cell。`Result.Seconds` 当前 bundle workspace-copy+Configure（`eval.go:145`），非干净 model-time —— 分离或标注。
- **server 窗口防御**（reviewer-2 objection #7）：每 cell 交叉核 `-c` vs estimateTokens vs provider PromptTokens —— 大背离 = 静默截断，该 cell **丢弃**；log `lms load` invocation + server health 进 envelope。
- **CI**：eval 从不在 CI 跑（`ci.yml` 仅 build/vet/test -race）—— 合理（需本地模型），但"一键重跑"须是 documented 手动脚本，harness 正确性靠 `internal/eval` 单测。

### 8.2 venue + writeup 骨架

**诚实 scope 决定 venue：n=1 模型、n=1 机器、污染 CS-101 任务、非 novel 机制、measurement-only → workshop 论文或严谨工程技术报告，非 main-track / arXiv "first frontier"。** 过度 scope 的 arXiv "viability frontier of local coding agents" 凭 §2+§9 即招 desk-reject。

候选：
- **Workshop（ML-for-systems / efficient-ML / on-device LLM）**："A measured, instrumented case study of context-vs-tool-schema overflow in one local coding agent, with a validated pre-flight detector." Scope 到 §4a 2×2 + 检测器混淆矩阵 + κ。
- **工程博客 / 技术报告**：全分类学 + before/after 杠杆，作 practitioner 指南（folklore-confirmation-with-measurement 在此可接受）。

**骨架：** Abstract（§0 一句话，带 CI）→ 1 问题+H1+证伪判据 → 2 novelty 定位（清完 §2.4 killer 后写）→ 3 分类学（Table 1，标 found vs awaiting-observation）→ 4 方法（ρ 操作化 + 两 context-window 区分 + §4a 设计）→ 5 结果（pass@5 对 ρ + 失败类分布 + 检测器混淆矩阵 + McNemar before/after + κ）→ 6 污染探针表 + 云校准列 → 7 局限（§9，作 headline 非 footnote）→ 复现附录（digest pin + manifest + env）。

---

## 第 9 节：诚实的局限（reviewer-2 fatal/serious 如实承认 + 缓解）

| # | 严重度 | 异议 | 缓解 / 承认 |
|---|---|---|---|
| 1 | **fatal** | **不是 benchmark**：n=1 模型、n=1 机器、8 个 ≤21 行单语言单 bug green-test 玩具。"viability frontier of local models" 是范畴错误。gemma 是 partial-tool-tuned 通用模型非 coder。 | **靠 rescope 救，非 hand-wave。** title/abstract 必读"a 12B-class local model on a 24GB laptop, on small single-file Go bugfix tasks"。跨模型 frontier 需 ≥3 真 coder-family size（去 family/tuning 混淆）+ 实质更难多文件多语言任务集（≥20 非玩具）。per-task 主表 + mixed-effects（task 随机效应）。无此两者 = 单模型案例研究，照此 title。 |
| 2 | serious | **novelty 未清**：机制是 folklore，"first" 没读两篇 killer（TinyLLM 2511.22138、SLM-tradeoffs 2604.19299）。 | §2.4 前置动作 —— **写 "first" 前读全文 + 书面 disposition**。命中则 DROP "first measurement"，re-pitch 为分类学 + headroom 可分离性；连分类学被抢则 pivot 工程报告。 |
| 3 | serious | **frontier 是 Lumen tool-count 的产物非模型属性**：(window − schema) 的 schema 项全是 Lumen 设计选择。 | IV 改用无量纲 ρ（§1.2）。存活主张："pass-rate 在 ρ→1 collapse，无论哪个 knob 驱动"——仅当 cliff 在 8k×core 与 16k×full 落同一 ρ 时是模型属性。§4a 16k×full 是判别 cell；跟绝对 schema token 走则报为 Lumen finding。加 micro 令 ρ≥3 点。 |
| 4 | serious | **schema 杠杆与能力混淆**：core→full→micro 也移除工具；headline 0→5/6 恢复还混淆 parallel(4→1) + KV layout 与 context 同变。 | (a) **全因子 profile×window 独立**，用分类学分离 tool-missing(F3) vs overflow(F1)；micro 失败若是 tool-missing 则如实报 schema 杠杆混淆。(b) **重跑 8k→16k 固定 parallel**（8k 也 `--parallel 1`），令唯一变量是 `-c` —— committed 恢复 anecdote 同变 parallel+context，**因果证据上不可采，须先隔离**。 |
| 5 | serious | **统计空**：5/6 @ repeat=1，无 CI，CI [0.44,0.97] 撑不起任何主张；Summarize pool task×reps；"6/6 ceiling" select-on-outcome。 | repeat≥5 pre-register；全 Wilson（阈值用 CP 下界）；per-task grouping；isolated 仅作 confound diagnostic 禁升 headline ceiling，as-run sequential 作 primary，均带 CI；裸 "83%" 撤回重发。 |
| 6 | serious | **分类学今日不可测**：sink 仅听 UsageKind，丢 ToolResult/Notice；max-steps 返 nil 不可见；firstPromptTokens 被 sum 丢；preflight 检测器默认 128000 静默不触发。 | **跑前建全 P0 instrumentation**（第 6 节 #1–#3，数 ToolResult 非 Dispatch）；每 cell `context_window=server -c` 否则检测器静默；分类器 κ-验证；kill-criterion(F5) pre-register；报检测器混淆矩阵。 |
| 7 | addressable | **headline 变量进程外不可仪表**：`-c` 手记无机器核查；server 静默截断则 PromptTokens 已是截断值掩盖 overflow。 | server 窗口作 operator-asserted 轴并防御：交叉核 `-c` vs estimate vs PromptTokens（大背离 → 丢该 cell）；log `lms load` + health 进 envelope；prose 严格区分 cfg.Agent.ContextWindow（仅 compaction）；frontier frame 为软带（estimateTokens chars/3 CJK-blind）。 |
| 8 | addressable | **唯一失败是 timeout 混淆**：05-counter-race 是第三桶，仅因孤立重跑才暴露；run-order 与 memory pressure 相关。 | `turn_timeout="20m"` 每 cell（非协商）；timeout 经 `StopReason=='timeout'`（stringify 前 `errors.Is` 检）隔离成 F6，禁折进能力失败；Latin-square 任务序；测记 free-RAM+swap 协变量，内存 dip 共现失败归环境；`--estimate-only` 门控 + infeasible cell 作合法边界。 |
| — | **completeness 追加** | **污染**：8 任务几乎确定被训练数据 memorize → capability/size 轴被污染（§4b 不抗，§4a overflow 抗）；**无云校准**证不了任务 discriminative；**无 seed**（wire `chatRequest` 无字段）→ 不可位级复现。 | 阶段 2 污染探针表 + 去-canonical/held-out 任务 + 云校准列；高 recall 则 DROP size-frontier 收到单模型 overflow 结果；writeup 明说 distributional-only reproducibility。 |

---

## 附：被引文件（全绝对路径）

- `/Users/lei/lumen/internal/eval/eval.go` — `Result`(L30-37,无 token/model/StopReason/FilesChanged 字段)、`Score`(L79-91)、`Summarize`(L104-138, flat pool + median=xs[len/2])、`ProtectedTestsUnchanged`(L146-165, WalkDir 可复用)
- `/Users/lei/lumen/cmd/lumen/eval.go` — flags(L24-30,无 -tool-profile/-context-window)、JSON envelope(L94-97,无 model/config stamp)、`runOneTask`(L112-153)、`evalSink`(L171-181,仅听 UsageKind、丢 ToolResult/Notice、首轮 PromptTokens sum 进 ctr.in L175)、`--repeat` flat append(L66-90)
- `/Users/lei/lumen/internal/agent/agent.go` — `estimateTokens`(L1027-1041, chars/3)、`preflightOverflowCheck`(L1052-1066, keyed off contextWindow*compactRatio, WARN-only)、`autoCompact`(L1068-1089)、ContextWindow 默认 128000(L182)、TurnTimeout 默认 5m(L191)、turn ctx deadline(L321)、Run 返 ctx.Err(L364)、empty-stream(L520)/empty-final(L531)、ToolDispatch(L450,可无 Start)/ToolResult(L585-598)、bare TurnDone 无 StopReason(L518,529,546,629)、**max-steps WARN Notice 后 return nil(L626-630, 不可见)**、verify sub-deadline(L805)、nudge(L1171)
- `/Users/lei/lumen/internal/control/tools_profile.go` — `coreToolNames` 42 entries(L12-38)、二元 `selectTools`(L44, `if profile != "core"`)，**micro 在此加**
- `/Users/lei/lumen/internal/config/config.go` — Profile(L37,默认 full)、ContextWindow/MaxSteps/CompactRatio/TurnTimeout(L69-77)、defaults(L217-222)
- `/Users/lei/lumen/internal/control/config_apply.go` — `agentOptionsFromConfig` 接线 profile/window/timeout(L56-61)
- `/Users/lei/lumen/internal/provider/openai/openai.go` — `chatRequest` **无 seed 字段**(L351-359)、5m timeout(L33-35)、finalized tool call 无 Start(L155,302,323)
- `/Users/lei/lumen/lumen.toml` — committed = deepseek-chat/full/temp0.7/max_steps30 ≠ baseline 实验配置(L1,7,21-32)
- `/Users/lei/lumen/.github/workflows/ci.yml` — build/vet/test-race only，eval 从不跑(L22-28)
- `/Users/lei/lumen/evals/tasks/01-08/prompt.txt` — 8 个 CS-101 经典 bug（污染风险），08 是唯一多文件
- `/Users/lei/lumen/docs/eval-baseline.md` — 0→5/6 @ 8k→16k 锚(L28-57)、per-task(L60-72)、timeout-not-capability(L104-129)、reproduce(L147-164)、open follow-ups(L168-176)、无 model digest(L13)
- `/Users/lei/lumen/docs/local-models.md` — 列了不同模型名 "gemma-4-12b-coder"(L62)

**核验计数：** 42 core tools、119 RegisterBuiltin、wire 无 seed、result 无 model stamp。

---

**Bottom line：** 今晚可跑、零下载、出真 finding 的是阶段 0 的 gemma 2×2（§4a）—— 但**仅在 P0 instrumentation（第 6 节 #1–#3）落地且单测绿之后**。它隔离 F1 为部署-非能力 gated，带 Wilson CI + 检测器混淆矩阵 + κ。瓶颈排序：(1) 模型服务器窗口 Go 不可仪表 = 永久手记轴；(2) micro profile 须加（schema 第三点）；(3) sink 须听 ToolResult/Notice + agent 须发 StopReason，否则 F1/F2/F5 不可测；(4) size-frontier gated on 1-9GB 下载 + 13GB RAM 天花板 + 任务污染。成本是 wall-time + RAM，非钱。可发表形态 = workshop / 技术报告级单模型案例研究，**绝不**自称 "frontier of local models"。