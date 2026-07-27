# Provider 熔断与降级 — 设计(待实施)

> 状态:**设计完成,未实施**。这是核心线目前唯一能直接改善"日常不卡住"的能力项。
> 写成设计而不是当场动手,是因为它改的是 provider 推理主路径,值得在清醒的
> 一次专门会话里做,而不是在收尾时顺手。

## 一、为什么要做(证据)

- `agent/crates/codegen/xai-grok-models/default_models.json` 有 **33 个模型预设**,
  但只有 DeepSeek 与 xAI 被 live 验证过。其余全部是"结构支持、未实测"。
- **"模型挂了怎么办"目前没有产品级答案**。唯一相关的东西是
  `scripts/lumen-model-fallback.sh` —— 一个读 `~/.lumen/science/proxy.log` 的外挂
  脚本,而核心 agent 直连 DeepSeek 时**根本不写那个日志**,并且它只"建议"切换,
  不执行。等于没有。
- 与此同时,`agent/crates/common/xai-circuit-breaker` 是一个 **1,190 行、约 70 个
  测试**的成熟熔断器,却**只被文件上传路径使用**
  (`xai-file-utils/storage_client.rs`)。推理路径一次都没用到。

一句话:**轮子是现成的、测试是齐的,只是没装到会抛锚的那个轮轴上。**

## 二、现成可复用的接口

```rust
pub use breaker::CircuitBreaker;
pub use registry::CircuitBreakerRegistry;   // 按 key 分池 → 天然 per-provider
pub use config::{BreakerConfig, parse_failure_codes};
pub use state::{BreakerOpen, BreakerState, Outcome};
pub use retry_policy::{Disposition, RetryPolicy};
pub use observer::{NoopObserver, Observer};  // → 接 truth bar / 遥测
pub use clock::{Clock, MockClock, SystemClock};  // → 确定性测试,无需 sleep
```

`storage_client.rs` 已经示范了正确用法,照抄即可:

```rust
if self.breaker.check().is_err() { /* 短路,不发请求 */ }
// ... 发请求 ...
self.breaker.record(Outcome::Success | Outcome::Failure);
```

## 三、设计

### 3.1 熔断域 = `(provider, base_url)`

不是按模型。同一 provider 的多个模型共享后端,一个挂了通常全挂;而 base_url 不同
即视为不同后端(这与 `prompt_cache_registry` 已确立的缓存/安全域定义一致)。
用 `CircuitBreakerRegistry` 以此为 key。

### 3.2 接入点:`xai-grok-sampler/src/client.rs`

- **前置**:发请求前 `check()`。若开路,**不发请求**,直接返回一个可识别的
  `BreakerOpen` 错误,交给降级链。
- **事后**:按状态码记录 —— `5xx` / 连接失败 / 超时 → `Outcome::Failure`;
  `2xx` → `Success`。
- **不计入熔断**:`401/403`(凭据问题,换后端也没用,只会把好后端也熔了)、
  `400`(请求本身错)。这一点 storage_client 已有先例(403 触发归因但不计数)。

### 3.3 降级链

配置形如 `[providers.failover] chain = ["deepseek-v4-pro", "deepseek-v4-flash", ...]`。
规则:

1. **只在"尚未产出任何输出块"时降级。** 一旦开始流式输出就不再切换 ——
   这与 `modelpool::RoutingProvider` 当年定下的 no-replay 原则一致:半截切换会
   重放已发出的内容,比失败更糟。
2. 每次降级发一个 **模型可见** 的系统提醒(`<provider-failover>`),说明从哪个
   模型降到哪个、原因是什么。模型需要知道它换了执行器 —— 这与今天接通的
   storm-breaker / delivery 提醒走同一条路径(turn-tail user 消息,**绝不进
   system prefix**,以保 DeepSeek 前缀缓存稳定)。
3. 链尾仍失败 → 如实报错,**不静默**。

### 3.4 可见性

- `Observer` 实现写入现有的 `unified_log`,并更新 TruthSnapshot,使 truth bar
  能显示"当前运行在降级执行器上"。
- **禁止**在降级后仍显示原模型名 —— 那就是新的一种说谎,与本仓 2026-07-26
  整批工作的方向相反。

## 四、验收

1. `MockClock` 驱动的单测:N 次失败后开路、半开、恢复闭合。
2. httptest mock:主 provider 返回 503 → 自动降级到次选并完成 turn;
   模型上下文里出现 `<provider-failover>`。
3. **无重放**:主 provider 在已输出若干 chunk 后断开 → **不得**降级重发,
   错误如实上报。
4. `401` 不触发熔断(负例):凭据错误不应把一个健康后端熔掉。
5. dogfood 新增一个任务:主 provider 指向必然失败的地址,断言任务仍然完成且
   `discipline`/`failover` 提醒出现 —— 与 d09/d10/d11 同一套仪器。

## 五、明确不做

- **不做跨 provider 的自动"找最便宜/最快"路由**。那是另一个产品,而且会让
  "我在用哪个模型"变得不可预测。降级只在失败时发生,且必须可见。
- **不动 `modelpool`**。它已有 latency-aware 路由与 no-replay failover 的实现,
  但当前未接入主路径;本设计走 sampler 层,先解决"挂了怎么办",不引入路由策略。

## 六、工作量

约 1–2 天:熔断接入(半天)+ 降级链与提醒(半天)+ 五项验收(半天)。
