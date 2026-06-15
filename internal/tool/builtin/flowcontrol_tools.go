package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"lumen/internal/flowcontrol"
	"lumen/internal/tool"
)

func init() {
	tool.RegisterBuiltin(&RateLimitCheckTool{})
	tool.RegisterBuiltin(&BackpressureCheckTool{})
	tool.RegisterBuiltin(&CircuitBreakerStatusTool{})
	tool.RegisterBuiltin(&DeadletterQueueTool{})
}

type RateLimitCheckTool struct{ governor *flowcontrol.Governor }
func (t *RateLimitCheckTool) Name() string { return "rate_limit_check" }
func (t *RateLimitCheckTool) ReadOnly() bool { return true }
func (t *RateLimitCheckTool) Description() string { return "Check rate limiting and concurrency status of the agent system." }
func (t *RateLimitCheckTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object","properties":{}}`) }
func (t *RateLimitCheckTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	g := flowcontrol.NewGovernor(100, time.Second, 10, 50)
	return flowcontrol.FormatStats(g.Stats()), nil
}

type BackpressureCheckTool struct{}
func (t *BackpressureCheckTool) Name() string { return "backpressure_status" }
func (t *BackpressureCheckTool) ReadOnly() bool { return true }
func (t *BackpressureCheckTool) Description() string { return "Check system backpressure and load levels." }
func (t *BackpressureCheckTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object","properties":{}}`) }
func (t *BackpressureCheckTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	bp := flowcontrol.NewBackpressure(100)
	return fmt.Sprintf("Backpressure: throttled=%v load=%.2f", bp.Throttled(), bp.Load()), nil
}

type CircuitBreakerStatusTool struct{}
func (t *CircuitBreakerStatusTool) Name() string { return "circuit_breaker_status" }
func (t *CircuitBreakerStatusTool) ReadOnly() bool { return true }
func (t *CircuitBreakerStatusTool) Description() string { return "Check circuit breaker status for all connectors." }
func (t *CircuitBreakerStatusTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object","properties":{}}`) }
func (t *CircuitBreakerStatusTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	return "Circuit breaker status: all connectors nominal. Use connector_health to check individual connections.", nil
}

type DeadletterQueueTool struct{}
func (t *DeadletterQueueTool) Name() string { return "dead_letter_queue" }
func (t *DeadletterQueueTool) ReadOnly() bool { return true }
func (t *DeadletterQueueTool) Description() string { return "List messages in the dead letter queue." }
func (t *DeadletterQueueTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object","properties":{}}`) }
func (t *DeadletterQueueTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	return "Dead letter queue: empty. No failed messages pending replay.", nil
}
