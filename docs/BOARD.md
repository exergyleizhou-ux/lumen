# Lumen 看板（核心线）

> **更新规则：** 只在里程碑更新。无心跳 monitor。
> 上次刷新：**2026-07-26**（core-hardening 批次）

---

## 总览

| 线 | 名称 | 状态 |
|----|------|------|
| **A** | Expert prove/harden 交付 | ✅ 收口（含 Windows，见下） |
| **B** | grok-build cherry P0+P1 | ✅ #127 + #128 已合 main；上游后续 sync 待下一轮巡检 |
| **C** | 发版 / 运维 | v0.1.250；Windows MSVC 构建已验证（推翻旧"跳过"记录） |
| **D** | Track A/B 审计 | ✅ B0–B4 五份审计 + track-a-evidence 已入 docs/ |
| **E** | 真相层与门禁加固 | ✅ 2026-07-26 批次（本次） |

---

## A — Expert 交付

| 项 | 状态 |
|----|------|
| Dual / tools / evidence / GitHub | ✅ main |
| macOS v0.1.221-macos | ✅ |
| Windows 真 binary | ✅ `17d023d` MSVC VERIFIED，138 核心测试过（`outputs/evidence/windows-build-17d023d.json`；证据锚在 0.1.222，0.1.250 需重跑） |

## B — 上游 cherry

| 项 | 状态 |
|----|------|
| P0 dispatch_locks + OSC52 | ✅ #127（`f29b2e2`） |
| P1 `/summarize` alias + marketplace `require_sha` | ✅ #128 |
| P1 auth recovery | SKIP（与 pin 一致） |
| 基线之后的上游 sync（≥5 次未审） | ⏳ 待下一轮辩证吸收（起手式见 `agent/UPSTREAM.md`） |

## C — 发版 / 运维

| 项 | 状态 |
|----|------|
| VERSION | **0.1.250** |
| readiness | **BLOCKED（诚实）**：M5 真人 10 分钟 + M6 15 真实自用日未过；伪造的 READY 已于 2026-07-26 撤销并加防伪门 |
| L1–L5 / R0 / eval-live 证据 | ⚠️ 锚在 0.1.220-alpha.4 时代二进制，0.1.250 发布前须整轮重跑 |
| Homebrew / Winget | formula revision 修为真实 SHA；winget sha 待发布资产 |

## 审计 B0–B4（2026-07-25）

session-actor-authority / persistence-restart-cancel / truth-snapshot-wiring / goal-acp / expert-e2-e3 — 全部 VERIFIED，见 `docs/*-audit-b*.md`。

---

## 一句话

工程门可自动化的全绿路径已铺好；**唯一合法残留是真人 M5 + 15 个真实自用日**，从今天开始每天一篇真日记。
