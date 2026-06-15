package builtin

import (
	"context"
	"encoding/json"
	"fmt"

	"lumen/internal/diffengine"
	"lumen/internal/tool"
)

func init() {
	tool.RegisterBuiltin(&ComputeDiffTool{})
	tool.RegisterBuiltin(&JSONDiffTool{})
}

// ── compute_diff ────────────────────────────────────────────────────────────

type ComputeDiffTool struct{}

func (t *ComputeDiffTool) Name() string   { return "compute_diff" }
func (t *ComputeDiffTool) ReadOnly() bool { return true }

func (t *ComputeDiffTool) Description() string {
	return "Compute a unified line diff between two texts. Provide old_text and new_text. Returns added/removed/changed line counts and the full diff."
}

func (t *ComputeDiffTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "old_text":{"type":"string","description":"The original text"},
  "new_text":{"type":"string","description":"The new text to compare against"}
},
"required":["old_text","new_text"]
}`)
}

func (t *ComputeDiffTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		OldText string `json:"old_text"`
		NewText string `json:"new_text"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}

	engine := diffengine.NewEngine()
	result := engine.LineDiff(p.OldText, p.NewText)
	return diffengine.FormatDiff(result), nil
}

// ── json_diff ───────────────────────────────────────────────────────────────

type JSONDiffTool struct{}

func (t *JSONDiffTool) Name() string   { return "json_diff" }
func (t *JSONDiffTool) ReadOnly() bool { return true }

func (t *JSONDiffTool) Description() string {
	return "Compute a structural diff between two JSON documents. Provide old_json and new_json as JSON objects/arrays or strings. Returns changes with paths, added/removed/modified counts."
}

func (t *JSONDiffTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "old_json":{"description":"The original JSON value (object, array, or JSON string)"},
  "new_json":{"description":"The new JSON value to compare against"}
},
"required":["old_json","new_json"]
}`)
}

func (t *JSONDiffTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		OldJSON json.RawMessage `json:"old_json"`
		NewJSON json.RawMessage `json:"new_json"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}

	oldVal := parseJSONValue(p.OldJSON)
	if oldVal == nil && len(p.OldJSON) > 0 {
		return "", fmt.Errorf("invalid old_json")
	}
	newVal := parseJSONValue(p.NewJSON)
	if newVal == nil && len(p.NewJSON) > 0 {
		return "", fmt.Errorf("invalid new_json")
	}

	engine := diffengine.NewEngine()
	result := engine.JSONDiff(oldVal, newVal)
	return diffengine.FormatChanges(result), nil
}

func parseJSONValue(raw json.RawMessage) interface{} {
	if len(raw) == 0 {
		return nil
	}
	// Try direct unmarshal first
	var v interface{}
	if err := json.Unmarshal(raw, &v); err == nil {
		// If it's a string, try parsing it as JSON
		if s, ok := v.(string); ok {
			var nested interface{}
			if json.Unmarshal([]byte(s), &nested) == nil {
				return nested
			}
		}
		return v
	}
	return nil
}
