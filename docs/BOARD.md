# Lumen 看板（A / B / C）

> **规则：** 任何状态变化必须在 5 分钟内改本文件 + session todos。  
> 上次刷新：**2026-07-20T06:03Z**（UTC）

---

## 总览

| 线 | 名称 | 完成度 | 状态 | 阻塞 |
|----|------|--------|------|------|
| **A** | 原目标：Expert prove/harden 交付 | **~95%** | 主路径 **已收口** | Windows 真包后置 |
| **B** | 新计划：grok-build 辩证 P0 升级 | **~85%** | PR 开了，**等 CI** | CI run 进行中 |
| **C** | 运维：看板纪律 + CI 盯梢 + 合入 + 后续队列 | **进行中** | 本轮新建 | 无 |

---

## A — 原目标（Expert 交付）

| 项 | 状态 | 证据 |
|----|------|------|
| Dual / tools / repair / evidence | ✅ | main 已合 #123–#126 |
| CI 绿（main tip） | ✅ | run `29718107379` success @ `84df071` |
| macOS 团队包 | ✅ | release `v0.1.221-macos` |
| GitHub 同步 | ✅ | origin/main |
| Windows 真 binary | ⏸ 后置 | 有脚本，本 Mac 无真包 |
| v0.1.222 tag | ⏸ 未做 | 等 B 合入后评估 |

**A 结论：** 可当交付完成；尾巴不挡 B/C。

---

## B — grok-build P0（当前主攻）

| 项 | 状态 | 细节 |
|----|------|------|
| 选型 / PINNED 政策 | ✅ | `agent/UPSTREAM.md` |
| dispatch_locks + cancel 持锁 | ✅ | commit `8d468d8` |
| OSC52 kill switch + LUMEN 别名 | ✅ | 同上 |
| 本地测试 | ✅ | clipboard 11；cancel 竞态；cancel local |
| push + PR | ✅ | **[#127](https://github.com/exergyleizhou-ux/lumen/pull/127)** |
| CI | 🔄 **in_progress** | [run 29720503701](https://github.com/exergyleizhou-ux/lumen/actions/runs/29720503701) |
| CI 当前步骤 | 🔄 | `cargo check xai-grok-shell`（protoc/toolchain/cache 已绿） |
| merge → main | ⏳ | CI 绿后立刻合 |
| P1（summarize / require_sha / auth…） | ⬜ | **B 合入后**再开 |

**B 结论：** 代码与 PR 已齐；差 CI 绿 + merge 才算「升级到 Lumen」。

---

## C — 运维计划（你点的「C 计划」）

目标：**看板不睡、CI 不丢、合入有人、队列可见。**

| 项 | 状态 | 动作 |
|----|------|------|
| 持久看板文件 | ✅ 新建 | 本文件 `docs/BOARD.md` |
| Session todos 与文件双写 | 🔄 | 每轮状态变化同步 |
| PR #127 CI 盯梢 | 🔄 | 绿 → merge；红 → 修再推 |
| 合入后刷 main CI | ⬜ | merge 后盯 tip |
| 合入后评估 P1 清单 | ⬜ | 写进本看板「下一批」 |
| Windows 真包 | ⏸ | 仍后置，C 只记位不排期 |

### C 下一批（B merge 后）

1. Merge #127 → 刷 main CI  
2. 可选 P1 cherry：`/summarize`、require_sha、auth recovery（仍拒 catalog/Expert 覆盖）  
3. 需要时再打 v0.1.222  

---

## 本轮时间线（精简）

| 时间 (UTC) | 事件 |
|------------|------|
| ~04:59 | A: main #126 merge，CI 最终 success |
| ~05:50–06:00 | B: 本地测通 + commit `8d468d8` + push |
| 06:01 | B: PR #127 打开，CI 启动 |
| 06:03 | C: 建 `docs/BOARD.md`，看板强制刷新 |

---

## 一句话

- **A 收口** · **B 等 CI** · **C 盯板+盯跑+合入**
