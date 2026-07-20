# Lumen 看板（A / B / C）

> **更新规则（修订）：**  
> - **只在里程碑更新**（合 PR、CI 终态、用户要进度）  
> - **禁止** 90s/3min 心跳刷屏、后台 monitor 打断会话  
> - 上次刷新：**2026-07-20T07:24Z**（UTC）— 验收后修正 docs CI 表述

---

## 总览

| 线 | 名称 | 完成度 | 状态 |
|----|------|--------|------|
| **A** | Expert prove/harden 交付 | ~95% | **主路径收口**；Windows 真包后置 |
| **B** | grok-build P0 升级 | **100%** | **#127 已合 + code CI 绿** |
| **C** | 运维 / 看板 | 纪律已改 | 不后台盯梢；P1 等你下令 |

---

## A — Expert 交付

| 项 | 状态 |
|----|------|
| Dual / tools / evidence / GitHub | ✅ main |
| main CI @ #126 | ✅ |
| macOS 团队包 v0.1.221-macos | ✅ |
| Windows 真 binary | ⏸ 后置 |
| v0.1.222 | ⏸ 未做 |

---

## B — grok-build P0

| 项 | 状态 |
|----|------|
| dispatch_locks + cancel 持锁 | ✅ main `f29bd2e` |
| OSC52 kill switch + LUMEN 别名 | ✅ |
| PR #127 | ✅ MERGED |
| main **code** CI（`f29bd2e`） | ✅ **success** |
| main **docs** CI（`abcead0` 看板刷新） | ✅ **success** |
| tip docs CI（`6c7a677` 看板定稿） | 可能仍在跑（仅 docs，不挡 B） |

本地验收（2026-07-20）：`clipboard_route_*` 11 passed；`cancel_never_overtakes…` ok。

---

## C — 下一批（等你下令）

1. 可选 P1 cherry：`/summarize`、require_sha、auth recovery…  
2. Windows 真包  
3. v0.1.222  

---

## 一句话

**A 交付基本完 · B 升级已落地 · 看板仅里程碑更新、不打断任务。**
