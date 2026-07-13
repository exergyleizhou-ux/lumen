# Production model evaluation

This gate measures model behavior for the shared Lumen Runtime. It contains exactly 10 Code tasks and 10 Science Lab tasks. The default gate is deterministic and offline; live model calls are a separate, explicit operation.

## What the committed result means

`eval-results.json` is generated from `evals/production/recorded.json` with model `controlled-v1`. It proves the loader, scorer, aggregation, integer cost accounting, task inventory, report schema, and failure classification are reproducible. Its 100% values are **controlled-fixture results, not claims about a real model**. Cost is zero because no provider request occurs. Recorded token and duration fields are fixtures used to exercise aggregation; they are not network measurements.

The live Qwen/DeepSeek evaluation is the sole external-credential blocker for this phase. No live metrics are committed because no real provider key was supplied; numbers must never be copied from the controlled report or invented.

## Metrics

- Success rate: tasks for which the model declares a completed, verified outcome.
- Tool correctness: exact ordered match between the task's expected tool sequence and model output.
- Verification repair rate: repair tasks that ran verification and reported a successful repair, divided only by tasks requiring repair.
- Citation completeness: credited citations divided by required citations, capped per task. Code tasks require zero; Lab tasks declare their requirement.
- Average tokens: prompt plus completion tokens per task.
- Cost: integer micro-USD, aggregated without floating-point money. Live runs require the operator to pass current provider prices explicitly.
- Duration: average wall time in milliseconds.
- Network failures: timeout, DNS/TLS/connection, rate-limit and gateway/service failures. These are separate from model/code failures and never count as success.

## Deterministic gate

```bash
make model-eval
# or
go run ./cmd/lumen model-eval --out docs/production/eval-results.json
go test ./internal/modeleval
```

The command loads `evals/production/tasks.json`, enforces exactly 10 Code and 10 Lab tasks, requires one recording per task, aggregates all required metrics, and exits non-zero for a failed row or classified failure. Unit tests rerun the same suite twice at a fixed clock and compare metrics.

## Live gate (not part of default tests)

Qwen is the default Chinese-model adapter; DeepSeek is also selectable. Both use their OpenAI-compatible API. Live operation requires four independent operator decisions: the `--live` flag, `LUMEN_MODEL_EVAL_LIVE=1`, a provider credential, and explicit current integer prices. This prevents tests from spending money or reaching the network accidentally and avoids embedding prices that may become stale.

```bash
export LUMEN_MODEL_EVAL_LIVE=1
export DASHSCOPE_API_KEY='...'
go run ./cmd/lumen model-eval \
  --live --adapter qwen \
  --input-price-micros-per-million <CURRENT_INTEGER_RATE> \
  --output-price-micros-per-million <CURRENT_INTEGER_RATE> \
  --out /tmp/qwen-live-eval.json
```

For DeepSeek, use `--adapter deepseek` and `DEEPSEEK_API_KEY`. Review provider pricing immediately before each live run and retain the exact command, result JSON, date, model ID and pricing source with release evidence.

The live adapter requests a strict action trace for every task and scores that trace against the manifest. It does not alter committed fixtures. A provider/network error creates a failed result with `failure_class: "network"`; malformed or incorrect model output is a model/code failure. Neither path can be promoted to success.

## Release interpretation

The deterministic gate is safe for CI and local verification. A release report may say the harness is green while the live capability gate is credential-blocked. It must not call `controlled-v1` a production model, present recorded fixture tokens/duration as observed provider performance, or claim a Qwen/DeepSeek baseline until a real signed-off live JSON exists.
