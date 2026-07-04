# Lumen — 交接文档（2026-07-04 同步）

> **「你是我绿洲里的光」**

**本文件曾长期过时（Beta 54/70、162 tests 等）。以下数字来自 `make facts`，请以此为准。**

## 项目路径

```
/Users/lei/lumen/
```

## 当前实况（`make facts`）

| 指标 | 数值 |
|------|------|
| 非测试 Go LOC | ~53,684 |
| 测试文件 | 252 |
| Go packages | 95 |
| Builtin tools | 117 |
| Model presets | 30 |

## 质量闸门

```bash
make check              # build + vet + test（CI 主闸门）
make goal-all-verify    # TestGoalEvidence + science full-verify + check
make science-full-verify
bash scripts/science/rm-offline-auto.sh
```

- **goal-ci** workflow：`TestGoalEvidence` + eval baseline6（TEST_E2E_SUCCESS 绕过）
- **science-ci**：quick + all + RM offline
- `full-verify.sh` 子步骤失败会 **非零退出**（不再假 PASS）

## Science / 绿洲

- Lumen Science：Go 原生代理，多档 profile 事务切换（relay base_url 可编辑）
- 桌面：`/Applications/Lumen Science.app`（内置 `lumen` CLI）
- 5-ship MCP + Research Brief + Oasis C2D 8 模板

## 存储

| 数据 | 默认 | 可选 |
|------|------|------|
| Science 配置 | `~/.lumen/science/config.json` | — |
| 会话 | JSONL `~/.lumen/history/` | — |
| 审计 | JSONL `~/.lumen/audit.jsonl` | SQLite `LUMEN_SQLITE_STORE=on` → `~/.lumen/lumen.db` |

## 质量闸门（2026-07-04 缺口关闭）

| 项 | 状态 |
|----|------|
| Bash 默认沙箱 | `auto`（有 backend 则隔离；`LUMEN_BASH_SANDBOX=off` 可关） |
| MCP injection | `untrusted.Wrap` 包装全部 MCP 工具返回 |
| SQLite 会话 | `LUMEN_SQLITE_STORE=on` 双写 JSONL + `session_messages`；`MigrateJSONLSessions` |
| Anthropic/Gemini live | `-short` 时 Skip；有 key 时 `TestLiveSmoke*` |
| Eval 闸门 | `evals/tasks` + `baseline6` 结构测试；`goal-all-verify` 清单 ≥14 |
| RM-HUMAN | `rm-offline-auto` 预检 + `rm-human-oauth.sh`；真 OAuth 仍需用户在场 |
| 跨平台发布 | `publish-science-release.sh --dry-run`（darwin arm64 + linux amd64 + checksum） |

剩余诚实边界：真 Anthropic 订阅 OAuth 无法零人工；CI 无证书时不公证；eval 全量 model run 仍 gated（成本）。

## 详细历史

下方旧版章节（包级测试表、P1 TUI 等）**已过时**，仅作考古参考。以 `README.md`、`docs/science/COMPARISON.md`、`CHANGELOG.md` 为准。

---