# Lumen Coding Eval — 锚点集登记（A1 基线，DEBT-033）

> **性质**：固定锚点集，**禁改**。改题/增删/改判定标准必须新开 DEBT 登记并说明理由。
> 用途：DEBT-033 六项强化的对照基线（通过率/成本/延迟/缓存命中率/验证次数）。
> 判定标准：agent 在**工作区副本**中修复后，确定性测试套件全绿（go test / pytest / vitest）；
> agent 不得修改测试（prompt 约束 + harness 副本机制双重保证）。

## 1. 锚点集（20 题）

| # | 任务 | 语言 | 难度 | 判定标准（测试） | 核心能力点 |
|---|---|---|---|---|---|
| 01 | average-empty | Go | L1 | `go test` TestAverageEmpty | 空切片除零 → 边界返回 |
| 02 | stack-lifo | Go | L2 | TestPopIsLIFO | 数据结构语义（LIFO vs FIFO） |
| 03 | reverse-runes | Go | L2 | TestReverse / TestReverseUnicode | Unicode 安全（rune 反转） |
| 04 | binary-search | Go | L2 | TestSearch | 二分边界/off-by-one |
| 05 | counter-race | Go | L3 | race detector 下 TestConcurrentInc | Go 内存模型/同步 |
| 06 | stringer-impl | Go | L1 | TestCircle（编译 + 断言） | 接口实现（缺方法不编译） |
| 07 | nilmap-write | Go | L1 | TestTally | 零值 map 初始化 |
| 08 | multifile-shapes | Go | L2 | TestAreas | 多文件包定位（Rect.Area 错，Circle 对） |
| 09 | py-divzero | Python | L1 | pytest safe_divide | 除零守卫 |
| 10 | py-json-merge | Python | L1 | pytest merge_dicts | dict 合并语义（b 胜出） |
| 11 | ts-optional-chain | TS | L1 | vitest getUserName | null 安全 |
| 12 | ts-async-race | TS | L2 | vitest fetchWithTimeout | async 竞态/超时取消 |
| 13 | go-context-cancel | Go | L3 | TestContextCancel | context 传播/ctx.Err |
| 14 | go-error-wrap | Go | L1 | TestErrorIs / Unwrap | %w 错误包装（errors.Is 可解） |
| 15 | py-path-traversal-fix | Python | L3 | pytest safe_read | 安全（路径穿越拒绝） |
| 16 | go-http-timeout | Go | L2 | TestFetchTimeout | HTTP 客户端超时 |
| 17 | multi-pkg-go | Go | L3 | TestMean（calc+stats） | 跨包调用错误定位（只改 stats） |
| 18 | fix-only-regression | Go | L2 | TestTrimSpaces + ToUpper 回归 | 最小改动纪律（不碰正确代码） |
| 19 | readme-driven | Go | L3 | TestGreeter（按 README 规格） | 规格驱动实现 |
| 20 | flaky-to-stable | Go | L2 | TestTickerFires（确定性） | 生产者修复/确定性 |

## 2. 难度分布

- **L1（单点修复，7 题）**：01, 06, 07, 09, 10, 11, 14
- **L2（逻辑/边界，8 题）**：02, 03, 04, 08, 12, 16, 18, 20
- **L3（跨切面/设计/安全，5 题）**：05, 13, 15, 17, 19

语言分布：Go 15 / Python 3 / TypeScript 2。

## 3. 判定规则（可证伪）

1. **PASS** = 工作区副本中 `go test ./...` / `pytest -q` / `vitest run` 全部通过。
2. **FAIL** = 任一测试失败、编译错误、超时（30s/任务）、或 agent 修改了测试文件（harness 副本 diff 校验，若发现测试被改判 FAIL 并记 corruption）。
3. 每轮 eval 必须输出 `EvalRun` JSON（schema v2）：`run_id / profile / tasks[] / aggregate`。
4. 可复现性断言：同一配置连续 2 次运行 `pass_rate` 波动 ≤ ±5%。

## 4. 运行方式

```bash
# harness 验证（所有工作区必须处于 broken 状态）
./scripts/eval-coding.sh

# 基线/对照运行（agent 修复 + 确定性测试）
EVAL_RUN_ONLY=1 EVAL_LIVE_DIR=artifacts/readiness/eval-baseline-flash \
  LUMEN_EVAL_MODEL=deepseek-v4-flash ./scripts/eval-coding-live.sh
```

输出：`evidence/eval/eval-run-<run_id>.json`（EvalRun schema v2）+ `artifacts/readiness/eval-run-latest.json`。

## 5. 指标语义（诚实声明）

| 字段 | 基线期（A1） | A2/A3 接线后 |
|---|---|---|
| pass_rate | 真实（测试判定） | 同 |
| avg_latency_ms | 真实（墙钟） | 同 |
| avg_tool_calls | 真实（events.jsonl tool_completed 计数） | 同 |
| total_input_bytes | 真实（cache_request_evidence body_bytes 求和，输入代理） | 同 |
| total_input_tokens / total_output_tokens | **null**（provider usage 未接线） | A3 接线后真实 |
| avg_cache_hit_ratio | **null**（wire_common_prefix_bytes 现为 None） | A2 CacheHealth 接线后真实 |
| avg_verify_count | **null**（验证计数随 ④ 落地） | Cycle B 后真实 |

> 基线期的 null 不是缺陷，是诚实锚点：A2/A3 落地后同一字段从 null → 真值，构成可证伪的对照证据。
