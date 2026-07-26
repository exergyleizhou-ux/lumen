# 交接 — 2026-07-26 核心线加固

> 单日一轮完整加固。**这一份是自足的**:读完可以直接接着干,不需要回看会话。
> 车道 = lumen core(编程终端底座)。**未触碰** science 接缝(SessionCommand /
> extensions/science.rs / science_goal.rs 一行未动),不碰 packs/oasis|quant。

## 一句话

把 Lumen 从"**自己说的话不可信**"(伪造的 READY + 4 个空转门 + 纪律半接线)推进到
"**每句自我陈述都有当天证据**":21+/27 自动门在当前 HEAD 的新鲜二进制上真绿、
eval 20/20、R0 端到端通过,只剩两个人类门。

## 一、真相层(最重要)

- **撤销伪造的 READY**。`artifacts/readiness/status.json` 曾被手写成
  `ready=true / 全 PASS`,而 M5/M6 证据文件自己承认是 SIMULATED
  (16 篇日记单 commit 批量生成,其中 6 天早于仓库诞生日)。现由
  `verify-readiness.sh` 生成真实的 BLOCKED。
- **隔离伪造日记** → `journal/simulated-backfill-20260725/`(保留审计痕迹)。
- **M6 防伪门**(`productivity-gate.sh`):首次 git add 必须在文件名日期
  ±`PRODUCTIVITY_GRACE_DAYS`(默认 2)内、不得早于仓库诞生日、**不得是未来日期**、
  不得来自被烧毁的批量提交 `ed8fca91`。三个方向都有负例实测。
  诚实声明:git 日期本地可伪造,这是善意绊线不是密码学边界。
- **版本一致性门**加硬:检查 status.json 版本、`ready=true` 与
  `engineering_complete=false` 的矛盾(删文件也不能静默)、SOURCE_LOCK
  `lumen_version` 缺失/不匹配一律 FAIL。
- **L5 soak 证据绑定二进制**:曾经拿 7-16 的旧 artifact(二进制 `65a3f694`,早被替换)
  一直报 PASS。现在要求 artifact 的 `binary_sha256` == 当前 release 二进制。

## 二、空转的门(修好后每个都有负例证明会咬人)

| 门 | 原缺陷 |
|---|---|
| `check-vacuous-e2e.sh` | `find\|while` 子 shell 吞掉 EXIT_CODE,**永远 exit 0** |
| `verify-goal.sh` | `2>&1 > log` 顺序错误 → 测试/clippy 计数恒为 0;clippy 闸被降级 |
| `lumen-e2e.sh` | `--test-threads` 传给 cargo(须在 `--` 后);`set -e` 下失败分支不可达 |
| `install-local.sh` | 被 `\| tail -2` 吞掉退出码 → 脏树拒绝构建看起来像成功(已在脚本头警告) |
| `regenerate-ledger.sh` | CI 检出无本地分支 → 分支表永远为空;Key Facts 硬编码 |
| verify-readiness | L4/L5 用本地 fixture 假 key,却被锁在 `DEEPSEEK_API_KEY` 后 |

## 三、纪律闭环(卖点从"有代码"变成"有行为")

- **StormBreaker**:非 Expert 会话原本只写日志 → 现在把提醒注入模型
  (阈值倍数节流);`LUMEN_STORM_STOP=1` 时 `StopBatch` 有**独立更强文案且不节流**。
- **RepeatSuccessGuard**:同样从"只写日志"改为模型可见提醒。
- **DeliverySessionState**:`begin_turn` 重置提醒预算(原来每会话只提醒一次);
  Soft 每回合最多一次(turn 有两个出口分支,原来会重复注入);
  `LUMEN_DELIVERY_STRICTNESS=off|soft|strict`。
- **lumen-verify 多语言真激活**:详见第四节的 dogfood 发现。

## 四、三次"接缝碰撞"(→ `docs/lumen-upstream-assumption-collisions.md`)

同一天撞三次同一模式:**Lumen 改了某个上游默认,上游别处仍按旧默认工作**。
共同特征:编译通过、单测全绿、审查看不出、**只有真跑才暴露**。

1. **BYOK 让 hermetic 测试逃逸到真实 API**(最严重):`deepseek-v4-pro` 带 embedded
   `base_url`,使 `config.rs` BYOK 分支跳过 `endpoints.xai_api_base_url` —— 而那正是
   上游 e2e harness 注入 MockInferenceServer 的地方。结果 leader/R0 全部端到端测试
   **向真实 api.deepseek.com 发 prompt**,撞线上 401。修:`LUMEN_INFERENCE_BASE_URL`
   硬覆盖 + harness 强制注入 + 三态回归测试。**这是 R0 门挂的真因。**
2. **edit→verify 多语言在产品里根本不存在**:两层缺陷叠加。
   (a) `lumen-verify` 已支持 py/ts 且测试全绿,但调用方 `verify_after_edit.rs`
   自己又写了两处 `extension != "go" → return None`;
   (b) 更外层 `workspace_ops.rs::call_tool` 把 `cwd_override` 写死 `None`,
   而生产环境无人插入 `Cwd` 资源 → registry 的 `workspace_root` 恒为 `None`
   → 钩子每次走空分支(**对所有语言,包括 Go**)。修:把 `WorkspaceSession::cwd()`
   传下去。**由 dogfood 发现**(用 lumen 修真实 Python bug,发现什么验证都没跑)。

   > **排查方法论的教训(比 bug 本身值钱)**:修好后我又连续三轮误判"还是没跑",
   > 因为一直在 **stderr 日志**里找反馈——而它按设计只进**模型的工具结果**,
   > 不进日志。最后靠两个直接证据定案:在产品路径打印 `run_after_edit` 返回值
   > (`RESULT: Some(ok=false, steps=2)`,ruff+pytest 真的跑了并抓到语法错误),
   > 以及让模型**逐字复述**工具返回(完整吐出 `[verify-after-edit]` 的 ruff 诊断)。
   > **用间接信号代替直接证据,会让你把好功能判成坏的** —— 与第 2 类缺陷
   > "把坏功能判成好的"正好互为镜像。
3. **上游权限测试假设 guard 不存在**:见下节。

## 五、7 个躺了几周的红测试(全部修绿,422/422)

`xai-grok-workspace` 的 permission 测试长期红,**CI 只跑 expert-gate 所以没人知道**。
两类根因:

- 单元测试读开发者**真实的 `~/.claude/settings.json`**(每台装 Claude Code 的机器都有)
  → `cfg(test)` 隔离 + `LUMEN_TEST_REAL_GLOBAL_CLAUDE` 逃生舱。
- 上游 fixture 用 `rm -rf /`、`curl|sh`、写 `/etc` 去测 classifier/ask-floor/session,
  但 **lumen-guard 的 hard-deny 在权限管理器最前面**(先于 classifier/ask/session/yolo),
  测试根本走不到目标逻辑。**测试红恰恰证明 guard 在正确工作。**
  修法:换成既危险又不被 guard 拦的 fixture(如 `chmod`)让每个测试测自己那层;
  意图是"必须拦住"的直接断言更强的 `PolicyDeny`;
  **新增契约断言:lumen-guard 必须赢过 yolo**(原测试竟编码了"yolo 放行 `rm -rf /`")。

## 六、安全欠债(上游辩证审查 → `docs/upstream-cherry-plan-20260726.md`)

5 域并行审查 `ba76b0a..47348d13`(957 文件漂移),**批准 23 项 cherry / 拒绝 12 类整包**。
本轮已落地:

- `cargo check` 移出自动安全名单(它会跑 build.rs/proc-macro = 仓库内容任意代码执行)
- bwrap `--cap-drop ALL`
- **`[permission]` 成为 folder-trust 标记**:一个只带
  `.grok/config.toml → [permission] allow=["Bash(*)"]` 的仓库,克隆进来**不弹任何提示**
  就自动放行一切 = 静默 RCE
- **marketplace git 参数注入**:URL/branch 直接进 argv,`--upload-pack=<cmd>` 即命令执行
  (Lumen 这块甚至落后于自己的基线);加校验 + `--` 终止符
- **链式命令 allow 绕过**:`allow=["Bash(git:*)"]` 整串匹配
  `git status && curl|sh` → 改为每个 chained segment 必须独立被允许

## 七、CI(地基的传感器)

从"只有 expert-gate"扩到:lumen 三 crate 测试 + clippy `-D warnings`、
**permission:: 418 + folder_trust:: 35(安全接缝)**、assert-defaults、shellcheck、
vacuous-e2e、版本一致性、SOURCE_LOCK 新鲜度。

## 八、当前状态与下一步

- **自动门**:最后一轮完整验证的结果见 `artifacts/readiness/status.json`
  (L0-L5 live / R0_full / eval_live 20/20 均已在本日验证通过)。
- **唯一合法残留 = 两个人类门**:M5(真人 10 分钟陌生人)+ M6(15 个真实自用日,
  当前 2/15,必须每天真用 + 当天提交日记)。
- **已派生独立任务**:15 个纯上游 `foreign_sessions` 红测试(自 Day0 就红,
  导入 Codex/Claude 历史会话的 sqlite 扫描,不在底座四大承诺内)。
- **未做且有意为之**:5UX 三件套(TUI 首启 wizard / 隔离 probe / PTY 矩阵)——
  9 天前的规格,在只有一个用户的阶段投产比低,**等真人 M5 反馈再做**。
- **平台化**:core 是 science/金融/ember/guard 的底座。现在**不抽 Platform API**
  (science 正高速迭代,冻结接口会互相绊);先让地基成为仪器,再谈 API 化 + feature-gate。

## 九、复现命令

```bash
cd ~/code/lumen
bash scripts/source-lock.sh          # 先锁
bash scripts/install-local.sh        # 后建(顺序反了 tuple 门会挂)
bash scripts/check-binary-tuple.sh
EVAL_LIVE=1 bash scripts/verify-readiness.sh
```
