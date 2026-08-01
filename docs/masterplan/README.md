# Historical planning references

> 本目录保留历史规划材料，**不是当前实施权威**。当前路线、优先级、来源窗口和验收
> 合同只以 [Lumen NextGen 执行书](../LUMEN-NEXTGEN-EXECUTION-BOOK-2026-08-01.md) 为准。
> 历史文档不得作为新功能的完成证据。

## 阅读顺序

| # | 文件 | 内容 |
| NextGen | [Lumen NextGen 执行书](../LUMEN-NEXTGEN-EXECUTION-BOOK-2026-08-01.md) | 当前可执行路线与验收合同 |
|---|------|------|
| 0 | [00-终极决议.md](./00-终极决议.md) | 战略写死、产品定义、禁止项 |
| 0A | [00A-来源锁与运行合同.md](./00A-来源锁与运行合同.md) | 源锁 · readiness · run 合同 |
| 1 | [01-注入地图-Grok真实路径.md](./01-注入地图-Grok真实路径.md) | 精确到 crate 的落点 |
| 2 | [02-安全规格-Lumen基因.md](./02-安全规格-Lumen基因.md) | 5+1 / 零宽 / writepath |
| 3 | [03-阶段路线图-16周.md](./03-阶段路线图-16周.md) | M0–M6 周计划 |
| 4 | [04-自修与循环-设计.md](./04-自修与循环-设计.md) | Storm / verify / delivery / goal |
| 5 | [05-Day0开战.md](./05-Day0开战.md) | Day0（完成后勿整仓重导） |
| 6 | [06-验收与门禁.md](./06-验收与门禁.md) | UX / DoD |
| 7 | [07-资产清单与取舍.md](./07-资产清单与取舍.md) | 四源取舍 |
| 8 | [08-M2-循环纪律.md](./08-M2-循环纪律.md) | M2 对照 |
| 10 | [10-旧Go到新Rust模块落点.md](./10-旧Go到新Rust模块落点.md) | 旧 Go 资产 → Grok 落点 |

## 常用门禁

```bash
./scripts/verify-readiness.sh          # 汇总（诚实 blockers）
./scripts/smoke-deepseek.sh            # L0
./scripts/smoke-deepseek-agent.sh      # L1 tool_calls（需有效 DEEPSEEK_API_KEY）
./scripts/source-lock.sh               # 刷新 SOURCE_LOCK.json
```

## 维护规则

- 本目录可用于追溯历史决策，但不能作为当前 source-lock、发布或产品完成的依据。
- 新的架构与实现计划只更新 NextGen 执行书及其明确引用的近期证据。
