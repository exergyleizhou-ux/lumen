# Lumen Science 能力矩阵

> Lumen `v1.3.0-science-lab.1`（2026-07-04）。Page A Bridge（外接稳）+ Page B Lab（自主科研实验室），原生 Go 单二进制。

## Page A · Bridge（外接层）

| 能力 | 说明 |
|------|------|
| Proxy runtime | 原生 Go 单二进制，无 Python 子进程 |
| Multi-profile switch | 切换前上游探活，事务提交，回滚 |
| Relay / model picker | `/v1/models` 双认证 + GUI 模型选择 |
| DSML tool-use shim | `off` / `detect` / `rewrite` + e2e |
| CONNECT fast-fail 401 | Anthropic 远程 MCP 快速失败 |
| Config path isolation | 符号链接/路径守卫 |
| Truthful key save | 401/403 拒绝保存 |
| Desktop app | Tauri + Go GUI (:18990) |
| DeepSeek cache boost | 一等公民 + watch |
| Virtual OAuth | Go forge + launcher 自检修复 |
| science_mode 三态 | hybrid/native/bridge CLI + GUI API |

## Page B · Lab（自主实验室）

| 能力 | 说明 |
|------|------|
| CS bio-tools fleet | 23 域 ~247 工具全舰队连接 |
| Lumen native fleet | 5-ship（PubMed/ChEMBL/GEO/Oasis/C2D） |
| 三栏 UI | 项目列表 + SSE 对话 + 状态/文件/化学/分子面板 |
| provenance.jsonl | MCP 调用 + 文件写入溯源，UI 展示 |
| Research Brief | 四源管线（PubMed/ChEMBL/GEO/Oasis） |
| Ketcher 嵌入 | EPAM 独立版化学编辑器 |
| 3Dmol 查看器 | 蛋白/分子 3D 结构预览 (PDB/SDF) |
| 文件面板 | 文件树 + 内容预览 + 下载 |
| Jupyter | Notebook CRUD + nbconvert 执行 |
| SSH 远程计算 | ~/.ssh/config 解析 + detached job |
| C2D 闭环 | list_algorithms API + publish CLI |
| Hybrid Bridge | A/B 互跳 bridge/open |
| ⌘K 命令面板 | 5 快捷操作 |
| Skills | 29 CS pack + 8 Lumen 拔高 |
| Seed templates | CRISPR/酶工程等 4 示例 |
| Desktop Lab | Tauri .app (:18992, 1280×800) |

## 拔高（vs CS）

| 维度 | CS | Lumen Page B |
|------|-----|-------------|
| 溯源 | 黑盒为主 | provenance.jsonl 可追溯 |
| 审阅 | 人工自查 | integrity-auditor + traceability-review |
| 数据资产 | 公共库为主 | 绿洲已验证数据集 + C2D |
| 模型 | 绑 Anthropic | 国产/第三方直连 |
| 工作台 | CS 官方 UI | 三栏 + 文件预览 + 分子查看 |
| 双模 | 仅 CS | hybrid: B→A 一键复现 |

## 生产硬化（2026-07-09）

| 维度 | 能力 |
|------|------|
| Lab 并发 | 全局 turn 池 4 + 每课题互斥 + controller 池 8 |
| 审批 | 真等待 + `/api/lab/approve` + 10min 超时拒绝 |
| 满载 | `503 Retry-After` / `readyz` |
| Bridge 策略 | force-shell · Kimi thinking · capability_rules on `/health` |
| 运维文档 | `docs/science/OPS-HARDENING.md` |

## 剩余边界

| 项 | 状态 |
|----|------|
| RM-04/06/13 真 OAuth | HUMAN-deferred |
| Eval 全量 model run | 结构闸门已覆盖 |
| Office 全预览 / job harvest | M2+ backlog |

## 本地验证

```bash
bash scripts/science/lab-smoke.sh
bash scripts/science/lab-parity-verify.sh
bash scripts/science/full-verify.sh
```
