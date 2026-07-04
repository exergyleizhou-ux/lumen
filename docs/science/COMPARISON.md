# Lumen Science 能力矩阵

> Lumen `v1.2.0-science-beta`（2026-07-04）。原生 Go 单二进制桥接 Claude Science，附带 5-ship MCP、Research Brief、Oasis OAuth。

| 能力 | 说明 |
|------|------|
| Proxy runtime | 原生 Go 单二进制，无 Python 子进程 |
| Multi-profile switch | 切换前上游探活，事务提交，代理重启失败自动回滚 |
| Relay / model picker | `/v1/models` 双认证 + GUI 模型选择 |
| DSML tool-use shim | `off` / `detect` / `rewrite` + e2e 测试 |
| CONNECT fast-fail 401 | Anthropic 远程 MCP 快速失败 |
| Config path isolation | 符号链接/路径守卫，禁止写入真实 Science 目录 |
| Truthful key save | 401/403 拒绝保存；未校验标记 |
| Desktop app | Tauri + Go `lumen science gui` |
| Native MCP fleet | 5 艘（PubMed / ChEMBL / GEO / Oasis / C2D） |
| Research Brief | 四源管线 CLI + API |
| Oasis embed | OAuth + C2D 发布路由 |
| DeepSeek cache boost | 一等公民 + watch 仪表盘 |
| Offline test gate | `test-science-all.sh`（≥120 tests） |
| RM matrix | 18 项文档 + 自动化离线 runner |
| Virtual OAuth | Go forge + launcher intact 自检修复 |
| Branding | Verdant Oasis / editorial GUI |

## 剩余边界

| 项 | 状态 |
|----|------|
| RM-04/06/13 真 OAuth | HUMAN-deferred；`rm-human-oauth.sh` + offline 预检 |
| 日常维护 plist | 未移植（可选） |
| Eval 全量 model run | 结构闸门已覆盖；live model run 按 API key 可选 |

## 本地验证

```bash
bash scripts/science/full-verify.sh
bash scripts/science/rm-offline-auto.sh
```