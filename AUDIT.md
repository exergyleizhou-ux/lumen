# Lumen 代码质量审计（自动同步指引）

> **2026-07-04 注：** 本文件旧版声称「0 tests / 162 tests」等，与仓库现状矛盾。  
> **请以运行时事实为准，不要手抄数字。**

## 一键刷新

```bash
make facts          # LOC / 测试文件 / packages / tools
make check          # build + vet + 全量 test
make goal-all-verify
```

## 当前快照（2026-07-04）

| 指标 | 值 |
|------|-----|
| 非测试 Go LOC | 53,684 |
| 测试文件 | 252 |
| Go packages | 95 |
| Builtin tools | 117 |
| Science Test* 函数 | ≥120（`test-science-all.sh` 门槛） |

## CI 闸门

| Workflow | 内容 |
|----------|------|
| `ci.yml` | `go test -race` 全仓库 |
| `goal-ci.yml` | `TestGoalEvidence` + `rm-offline-auto` |
| `science-ci.yml` | science quick/all + gitleaks + RM offline |

## 强项

- 单 Go 二进制 agent + Science 原生桥
- verify-after-edit、evidence、checkpoint、plan-mode
- Science：DSML shim、profile 事务切换、缓存 boost + benchmark 测试
- Oasis C2D 闭环 + 5-ship MCP

## 已知缺口

见 `HANDOFF.md` 诚实缺口节、`docs/science/COMPARISON.md`。

---

*旧版详细审计表格已归档删除意图 — 避免误导。需要深度评审请跑 `make goal-all-verify` 并查看 `.goal-verify-scratch/` 日志。*