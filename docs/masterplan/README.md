# Lumen Masterplan（最终极致方案）

> **方案权威（桌面）：** `Lumen Masterplan FINAL-2.0 - 生产级执行方案.docx`  
> **内容基线：** FINAL-1.1（`Lumen Masterplan.docx`）；**Blueprint 不作依据**  
> **仓库 SSOT：** 本目录 + `policy/` + 根 `SOURCE_LOCK.json`  
> **怎么走：** [09-FINAL-2.0-执行路径.md](./09-FINAL-2.0-执行路径.md) — **保留已完成 M0–M4，按 2.0 补缺口**

## 阅读顺序

| # | 文件 | 内容 |
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
| 9 | [09-FINAL-2.0-执行路径.md](./09-FINAL-2.0-执行路径.md) | **当前执行路径** |

## 常用门禁

```bash
./scripts/verify-readiness.sh          # 汇总（诚实 blockers）
./scripts/smoke-deepseek.sh            # L0
./scripts/smoke-deepseek-agent.sh      # L1 tool_calls（需有效 DEEPSEEK_API_KEY）
./scripts/source-lock.sh               # 刷新 SOURCE_LOCK.json
```

## 与旧文档

| 文档 | 状态 |
|------|------|
| FINAL-2.0 Word | **现行方案权威** |
| FINAL-1.1 Word | 内容基线 |
| Blueprint | 历史骨架，不作依据 |
| 本目录 | 执行与代码同步的 SSOT |
