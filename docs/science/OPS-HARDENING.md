# Lumen Science / Lab — 国家级可用运维硬化手册

> 目标：Bridge（Page A）+ Lab（Page B）在长时间科研负载下稳定、可观测、可恢复。  
> 配套实现：`internal/science/lab/{pool,limits,approval}.go` · `proxy/{catalog,policy}.go`

## 1. 进程与端口

| 服务 | 默认地址 | 命令 |
|------|----------|------|
| Bridge GUI | `127.0.0.1:18990` | `lumen science gui` |
| Bridge proxy | `127.0.0.1:18991` | 由 start/gui 拉起 |
| CS sandbox | `127.0.0.1:8990` | 隔离 HOME，永不碰 `8765` |
| Lab | `127.0.0.1:18992` | `lumen science lab` |
| Lab HTTPS | `port+3` | 自签，embed 可选 |

**铁律**：真实 `~/.claude-science` 与端口 `8765` 禁止写入/占用。

## 2. Lab 抗压能力（已实现）

| 机制 | 默认 | 行为 |
|------|------|------|
| 全局并发 turn | 4 | 超额 → `503` + `Retry-After` |
| 每课题互斥 | 1 turn | 同 project 并发 → `409` |
| Controller 池 | 8 | LRU 淘汰空闲课题 |
| 审批等待 | 10 min | 超时自动拒绝，释放 goroutine |
| Turn 超时 | 5 min | context cancel |
| Chat body | 8 MiB | 长 prompt；其它 POST 1 MiB |
| Panic 恢复 | middleware + turn | 不拖垮进程 |
| 就绪探针 | `/api/lab/readyz` | 满载 503 |
| 容量指标 | `/api/lab/health` → `capacity` | turns_active / controllers_busy |

## 3. Bridge 抗压能力（已实现）

| 机制 | 说明 |
|------|------|
| 协议指纹重启 | profile 的 adapter/api_format/model 变则重启代理 |
| force-shell | Science 选择器稳定显示真模型名 |
| thinking policy | Kimi enabled + 去强制 tool_choice |
| capability_rules | `/health` 返回命中规则 id |
| DSML shim | off/detect/rewrite |
| 上游超时 | Upstream client 300s；CONNECT 快失败 401 |

## 4. Oasis 生产反代

```
/api/lab/*        → host:18992
/lumen-lab/*      → host:18992
/lumen-science/*  → host:18990
/api/* (其它)     → marketplace backend
```

**禁止**把 `/api/lab/*` 交给 marketplace Go backend（历史 404 根因）。

## 5. 日常闸门

```bash
cd /Users/lei/lumen
make science-check          # fmt + vet + quick tests
bash scripts/science/lab-stress.sh
bash scripts/science/lab-smoke.sh
bash scripts/science/full-verify.sh   # 发布前
```

## 6. 现场验收清单（真科研场景）

1. **长会话**：同一课题连续 20+ 轮，无泄漏、无卡死  
2. **双课题**：两个浏览器 tab 各开一课题并行（不超过并发上限）  
3. **审批**：Agent 模式写文件 → 卡审批 → 允许/拒绝均正确  
4. **审批超时**：不点按钮超过 10 min → 自动拒绝，可发新消息  
5. **满载**：5 路并发 chat → 第 5 路 503，前 4 路正常结束  
6. **Bridge 切换**：Kimi ↔ DeepSeek profile 事务切换，Science 顶部显示真模型名  
7. **Oasis embed**：`/workspace/lumen-lab` 先 health 再 iframe；Lab 宕机有诚实错误页  

## 7. 故障速查

| 症状 | 查 |
|------|-----|
| Lab 404 on demo | Caddy 是否把 `/api/lab` 拆出 marketplace |
| 一直「繁忙」 | `capacity.turns_active` 是否泄漏；重启 lab |
| 审批无反应 | 是否 Agent 模式；浏览器是否打到 `/api/lab/approve` |
| Science 卡死 | DSML rewrite；`capability_rules`；proxy log |
| 模型选不中 | force-shell / profile.model 是否空 |

## 8. 后续仍可拔高（未承诺本期）

- Office 全预览、job harvest globs  
- catalog 全量 JSON 嵌文件  
- 多机 Lab 水平扩展（当前单机进程内池）  
- 正式 Apple 公证桌面包  
