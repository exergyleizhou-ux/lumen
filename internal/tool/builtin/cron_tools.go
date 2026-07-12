package builtin

import (
	"context"
	"encoding/json"

	"lumen/internal/cronparser"
	"lumen/internal/tool"
	"time"
)

func init() {
	tool.RegisterBuiltin(&CronParseTool{})
	tool.RegisterBuiltin(&CronNextNTool{})
	tool.RegisterBuiltin(&CronDescribeTool{})
}

type CronParseTool struct{}

func (t *CronParseTool) Name() string   { return "cron_parse" }
func (t *CronParseTool) ReadOnly() bool { return true }
func (t *CronParseTool) Description() string {
	return "Parse a 5-field cron expression and show next 5 execution times."
}
func (t *CronParseTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"expression":{"type":"string","description":"5-field cron: min hour dom month dow"}},"required":["expression"]}`)
}
func (t *CronParseTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ Expression string }
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	return cronparser.FormatSchedule(p.Expression, 5), nil
}

type CronNextNTool struct{}

func (t *CronNextNTool) Name() string   { return "cron_next" }
func (t *CronNextNTool) ReadOnly() bool { return true }
func (t *CronNextNTool) Description() string {
	return "Get the next N execution times for a cron expression from now."
}
func (t *CronNextNTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"expression":{"type":"string"},"count":{"type":"integer","default":5}},"required":["expression"]}`)
}
func (t *CronNextNTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Expression string
		Count      int
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	if p.Count <= 0 {
		p.Count = 5
	}
	e, err := cronparser.Parse(p.Expression)
	if err != nil {
		return "", err
	}
	b, _ := json.MarshalIndent(e.NextN(time.Now(), p.Count), "", "  ")
	return string(b), nil
}

type CronDescribeTool struct{}

func (t *CronDescribeTool) Name() string   { return "cron_describe" }
func (t *CronDescribeTool) ReadOnly() bool { return true }
func (t *CronDescribeTool) Description() string {
	return "Convert a cron expression to human-readable English."
}
func (t *CronDescribeTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"expression":{"type":"string"}},"required":["expression"]}`)
}
func (t *CronDescribeTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ Expression string }
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	e, err := cronparser.Parse(p.Expression)
	if err != nil {
		return "", err
	}
	return e.Describe(), nil
}
