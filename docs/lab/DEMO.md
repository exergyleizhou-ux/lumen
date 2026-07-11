# Lumen Science Lab — 5 分钟演示剧本

**公网 Demo**：https://demo.oasisdata2026.xyz/lumen-lab/  
**版本**：见页面「状态」或 `GET /api/lab/health` → `version`  
**定位**：Lumen 体系内的科研工作台（Go Agent 主路径），**不是** OpenClaudeScience / InternAgentS 的克隆。

---

## 开场白（30 秒）

> 这是 **Lumen Science Lab**：课题工作区 + 可审批的 Agent + Jupyter + 同域化学编辑 + Research Pack/舰队，跑在我们自己的 Go 栈上，公网可演示。  
> 和 InternAgentS（OCS）不同：他们是 DeepAgents/LangGraph 当主 Agent + Next 工作台；我们是 **Lumen 主 Agent**，LangGraph 只是可选旁路。

---

## 演示路径（约 5 分钟）

### 1. 打开与健康（30s）

1. 浏览器打开 https://demo.oasisdata2026.xyz/lumen-lab/  
2. 右侧 **状态** 页确认：
   - Lab 在线、模型已配置  
   - **同域 Ketcher** ✓  
   - **Jupyter** 可用  
   - **Research Pack / Fleet** 有连接  
   - **LangGraph** 可用（可有 LLM）  
   - **OnlyOffice**：小 VPS 上多为「未配置」——**诚实**，真编辑需外挂 Document Server  

### 2. 课题与对话（1–1.5 min）

1. 左侧 **新建课题**（或选已有）  
2. 点欢迎区/快捷 chip，例如「检查工作区」或「PubMed 文献检索」  
3. 说明：消息走 **Go Agent + SSE**，工具调用可走 **审批**（Agent 模式）  

### 3. 文件工作区（45s）

1. 右侧 **文件**：浏览/上传/预览 Markdown  
2. 可选：新建 `notes/demo.md`，在对话里 `@notes/demo.md` 引用  

### 4. Jupyter（45s）

1. **Notebook** 面板 → 新建 notebook → 加 cell `print(1+1)` → 执行  
2. 强调：**服务端真实执行**，不是纯前端玩具  

### 5. 化学（45s）

1. **化学 / Ketcher**：同域打开编辑器  
2. 可选：导出 MOL → **分子** 3D 查看  

### 6. LangGraph 旁路（45s）

1. **LangGraph** 页：提示词「总结工作区并给两条建议」→ **运行**  
2. 点结果里的路径可打开文件；**存笔记** 写入 `artifacts/`  
3. 说明：这是 **旁路**，不替代主对话 Agent  

### 7. 收尾（15s）

> 生产形态：单租户、本地/VPS 可控数据；Office 真编辑需外挂 DS；桌面 Tauri 可包一层壳。  
> 一键验收：`./scripts/science/lab-product-smoke.sh https://demo.oasisdata2026.xyz/lumen-lab`

---

## 不要演示 / 不要夸大

| 不要说 | 原因 |
|--------|------|
| 「和 OCS 一模一样」 | 架构不同（见 `docs/lab/POSITIONING.md`） |
| 「VPS 上已开 OnlyOffice 真编辑」 | 3.4 GiB 故意不装 DS |
| 「LangGraph 就是主 Agent」 | 主路径是 Go Lumen |

---

## 本机 5 分钟（开发者）

```bash
export PATH="$HOME/.local/bin:$PATH"
# 可选 sidecar
./scripts/science/lab-local-with-sidecars.sh
# 浏览器
open http://127.0.0.1:18992/
# 门禁
./scripts/science/lab-product-smoke.sh http://127.0.0.1:18992
```
