# Lumen 看板（A / B / C）

> **规则：** 任何状态变化必须在 5 分钟内改本文件 + session todos。  
> 上次刷新：**2026-07-20T06:30Z**（UTC）

---

## 总览

| 线 | 名称 | 完成度 | 状态 | 阻塞 |
|----|------|--------|------|------|
| **A** | 原目标：Expert prove/harden 交付 | **~95%** | 主路径 **已收口** | Windows 真包后置 |
| **B** | 新计划：grok-build 辩证 P0 升级 | **~98%** | **#127 已 merge** | main CI 排队中 |
| **C** | 运维：看板 + 盯梢 + 合入 + 队列 | **进行中** | 盯 main tip CI | 无 |

---

## A — 原目标（Expert 交付）

| 项 | 状态 | 证据 |
|----|------|------|
| Dual / tools / repair / evidence | ✅ | main 已合 #123–#126 |
| CI 绿（main @ #126） | ✅ | run `29718107379` success @ `84df071` |
| macOS 团队包 | ✅ | release `v0.1.221-macos` |
| Windows 真 binary | ⏸ 后置 | 有脚本，本 Mac 无真包 |
| v0.1.222 tag | ⏸ 未做 | B 合入后评估 |

---

## B — grok-build P0

| 项 | 状态 | 细节 |
|----|------|------|
| 选型 / PINNED | ✅ | `agent/UPSTREAM.md` |
| dispatch_locks + cancel 持锁 | ✅ | 已合 main |
| OSC52 kill switch + LUMEN 别名 | ✅ | 已合 main |
| 本地测试 | ✅ | clipboard 11；cancel 竞态；cancel local |
| PR #127 | ✅ **MERGED** | https://github.com/exergyleizhou-ux/lumen/pull/127 |
| PR CI | ✅ **success** | run `29720503701` |
| merge commit | ✅ | `f29bd2e` @ 06:30Z |
| main tip CI | 🔄 **queued** | run `29721885859` |
| P1 cherry | ⬜ 下一批 | summarize / require_sha / auth… |

**B 结论：** 升级 **已进 main**；差 tip CI 绿作最终确认。

---

## C — 运维

| 项 | 状态 |
|----|------|
| 持久看板 `docs/BOARD.md` | ✅ |
| #127 CI 盯梢 → merge | ✅ 完成 |
| main tip CI 盯梢 | 🔄 run `29721885859` queued |
| P1 评估 | ⬜ tip 绿后 |

### C 下一批

1. main tip CI 绿  
2. 本地 `git pull` 对齐 main  
3. 可选 P1 点状 cherry  
4. 需要时 v0.1.222  

---

## 时间线

| 时间 (UTC) | 事件 |
|------------|------|
| ~04:59 | A: main #126 + CI success |
| 06:01 | B: PR #127 + CI 启动 |
| 06:30 | B CI success → **merge #127** → main `f29bd2e`；tip CI queued |

---

## 一句话

**A 收口 · B 已合 main · C 盯 tip CI**
