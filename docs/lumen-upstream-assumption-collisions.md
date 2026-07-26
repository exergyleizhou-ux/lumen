# Lumen 定制层 × 上游假设 的碰撞面

> 2026-07-26 一天之内撞了三次同一模式，因此固化为工程契约。
>
> Lumen 是 grok-build 的 pin 衍生：上游约 135 万行，Lumen 增量约 2.3 万行。
> 增量本身质量不差，**风险几乎全部集中在"Lumen 改了某个上游默认，而上游别处
> 仍按旧默认工作"的交界处**。这类缺陷有一个共同特征：
> **编译通过、单元测试全绿、代码审查看不出、只有真跑才暴露。**

## 已发生的三次（全部为真实缺陷，非理论风险）

### 1. BYOK 默认让 hermetic 测试逃逸到真实 API（最严重）

- **Lumen 改了什么**：`default_models.json` 把默认模型换成 `deepseek-v4-pro`，
  并给它一个 embedded `base_url = https://api.deepseek.com/v1`。
  `agent/config.rs` 的 BYOK 分支据此**跳过** `endpoints.xai_api_base_url`。
- **上游假设什么**：e2e harness（`xai-grok-test-support/leader.rs`）通过
  `GROK_XAI_API_BASE_URL` 把所有推理指向 `MockInferenceServer`。
- **碰撞结果**：leader / R0 全部端到端测试**向真实 api.deepseek.com 发送 prompt**，
  mock server 在旁边空转；测试因线上 401 失败。既是门禁失效，也是测试卫生/隐私问题
  （任何人跑测试都会把测试内容发到真实 API）。
- **修法**：`LUMEN_INFERENCE_BASE_URL` 硬覆盖（连 embedded BYOK 也盖），
  harness 强制注入 + 假 key；三态回归测试（embedded → overridden → restored）。

### 2. edit→verify 多语言激活在产品里根本不存在

- **Lumen 改了什么**：`lumen-verify` 激活 Python/TypeScript 自动验证，5 个测试全绿。
- **调用方假设什么**：`xai-grok-tools/verify_after_edit.rs` 自己又写了一遍
  `extension != "go" → return None`（而且写了两处）。
- **碰撞结果**：功能完整实现、测试全绿、**对用户完全不存在**。
  单元测试测的是 crate 内部，没有任何测试跨越 crate 边界验证"编辑 .py 会触发验证"。
- **发现方式**：dogfood——真的用 lumen 修一个 Python bug，发现什么验证都没跑。
- **修法**：外层只做廉价扩展名预筛，语言/项目标记的权威判定留在 `lumen_verify`；
  两个回归测试（Python 编辑必须到达验证器 / Markdown 编辑必须被早筛掉）。

### 3. 上游权限测试假设 guard 不存在

- **Lumen 加了什么**：`lumen-guard` hard-deny，**所有模式**（含 YOLO/auto）生效，
  例如拒绝写 `/etc/` 下的任何路径。
- **上游测试假设什么**：`auto_mode_edit_fast_path_allows` 用
  `Edit("/etc/hosts")` 断言 auto 模式必须 Allow。
- **碰撞结果**：该测试在装了 Lumen guard 的树上必然失败——它实际测的是 guard，
  不是 auto fast path。
- **修法**：fixture 换成临时目录路径（真正测 fast path），
  **另加**一条断言证明 guard 仍然赢过 auto（把碰撞变成契约）。

## 契约（改动 Lumen 默认时必须走的检查）

1. **改任何上游默认前，先问"上游哪里还按旧默认工作？"**
   grep 该默认对应的 env / 常量 / 注入点在 `crates/` 全树的使用，
   尤其是 `xai-grok-test-support`、`tests/`、`*-harness`。
2. **端到端能力必须有跨 crate 的测试**，不能只测自己 crate 内部。
   第 2 类缺陷的唯一防线就是这条。
3. **hermetic 声明必须可验证**：任何自称隔离的 harness，
   要能证明"网络请求不会离开本机"。BYOK 类默认会绕过按 provider 设计的注入点。
4. **上游 fixture 与 Lumen 安全语义冲突时，改 fixture、不改语义**，
   并补一条断言把 Lumen 的语义钉死（见第 3 类的修法）。
5. **CI 覆盖面即可见性**：这三类里有两类长期存在却无人知道，
   因为 CI 只跑 `expert-gate`。任何"本地能跑的廉价门"都应该进 CI，
   否则红了也没人看见。

## 待排查（同类嫌疑）

- Lumen 改过的其它上游默认：`stream_tool_calls`、`context_window`(1M)、
  `auto_compact` 阈值、`reasoning_effort` 归一化、遥测默认关。
  每一项都应按上面第 1 条 grep 一遍其上游消费者。
- `LUMEN_*` 环境变量全集与上游 `GROK_*` 的语义重叠面。
