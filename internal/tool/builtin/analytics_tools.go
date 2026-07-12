// analytics_tools.go — Telemetry and feedback tools for the agent.
package builtin

import (
	"context"
	"encoding/json"
	"fmt"

	"lumen/internal/telemetry"
	"lumen/internal/tool"
)

func init() {
	tool.RegisterBuiltin(&FeedbackTool{})
	tool.RegisterBuiltin(&AnalyticsTool{})
	tool.RegisterBuiltin(&TelemetryStatusTool{})
}

type FeedbackTool struct{ store *telemetry.FeedbackStore }
func (t *FeedbackTool) Name() string     { return "feedback" }
func (t *FeedbackTool) ReadOnly() bool   { return false }
func (t *FeedbackTool) Description() string {
	return "Submit user feedback: type can be 'thumbs_up', 'thumbs_down', 'bug', 'feature', or 'text'."
}
func (t *FeedbackTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"type":{"type":"string","enum":["thumbs_up","thumbs_down","bug","feature","text"]},"message":{"type":"string"},"context":{"type":"string","description":"What were you doing when this happened?"}},"required":["type","message"]}`)
}
func (t *FeedbackTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ Type, Message, Context string }
	if err := json.Unmarshal(args, &p); err != nil { return "", err }
	fs := telemetry.NewFeedbackStore()
	fe, err := fs.Submit(p.Type, p.Message, p.Context, "")
	if err != nil { return "", fmt.Errorf("failed to submit feedback: %w", err) }
	return fmt.Sprintf("Feedback submitted. ID: %s. Thank you!", fe.ID), nil
}

type AnalyticsTool struct{}
func (t *AnalyticsTool) Name() string     { return "analytics" }
func (t *AnalyticsTool) ReadOnly() bool   { return true }
func (t *AnalyticsTool) Description() string {
	return "Run usage analytics — shows health score, tool usage, error rates, model stats, and improvement recommendations."
}
func (t *AnalyticsTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object","properties":{"period":{"type":"string","enum":["day","week","month"]}},"required":[]}`) }
func (t *AnalyticsTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ Period string }
	json.Unmarshal(args, &p)
	if p.Period == "" { p.Period = "week" }

	a := telemetry.NewAnalyzer()
	report := a.Analyze(p.Period)
	return telemetry.FormatReport(report), nil
}

type TelemetryStatusTool struct{}
func (t *TelemetryStatusTool) Name() string     { return "telemetry_status" }
func (t *TelemetryStatusTool) ReadOnly() bool   { return true }
func (t *TelemetryStatusTool) Description() string {
	return "Show telemetry collection status and recent event counts."
}
func (t *TelemetryStatusTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object","properties":{}}`) }
func (t *TelemetryStatusTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	c := telemetry.NewCollector()
	defer c.Close()

	events := c.Tail(50)
	byType := map[telemetry.EventType]int{}
	for _, e := range events { byType[e.Type]++ }

	return fmt.Sprintf(
		"Telemetry: active\nEvents this session: %d\nRecent (last 50): tool_calls=%d errors=%d model_calls=%d feedback=%d\n",
		c.Count(), byType[telemetry.EventToolCall], byType[telemetry.EventToolError],
		byType[telemetry.EventModelCall], byType[telemetry.EventFeedback]), nil
}
