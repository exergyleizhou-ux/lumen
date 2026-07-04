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

- Lumen Science：Go 原生代理，CSswitch **v0.3.1** parity（含 relay base_url 可编辑）
- 桌面：`/Applications/Lumen Science.app`（内置 `lumen` CLI）
- 5-ship MCP + Research Brief + Oasis C2D 8 模板

## 存储

| 数据 | 默认 | 可选 |
|------|------|------|
| Science 配置 | `~/.lumen/science/config.json` | — |
| 会话 | JSONL `~/.lumen/history/` | — |
| 审计 | JSONL `~/.lumen/audit.jsonl` | SQLite `LUMEN_SQLITE_STORE=on` → `~/.lumen/lumen.db` |

## 诚实缺口（仍未「极致」）

- Anthropic/Gemini 未 live-burned-in
- eval 仅 6 任务 baseline，非大规模质量证明
- OAuth RM-HUMAN 需用户在场
- bash 默认未沙箱；MCP injection 包装不完整
- SQLite 仅 MVP（审计双写 + session_meta 表），会话主体仍 JSONL

## 详细历史

下方旧版章节（包级测试表、P1 TUI 等）**已过时**，仅作考古参考。以 `README.md`、`docs/science/COMPARISON.md`、`CHANGELOG.md` 为准。

---