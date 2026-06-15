package builtin

import (
	"context"
	"encoding/json"
	"fmt"

	"lumen/internal/jsonpath"
	"lumen/internal/tool"
)

func init() {
	tool.RegisterBuiltin(&JSONPathTool{})
}

// JSONPathTool evaluates JSONPath expressions against JSON data.
type JSONPathTool struct{}

func (t *JSONPathTool) Name() string   { return "jsonpath_query" }
func (t *JSONPathTool) ReadOnly() bool { return true }

func (t *JSONPathTool) Description() string {
	return "Evaluate a JSONPath expression against JSON data. Provide the JSON data (as a JSON object or string) and a JSONPath expression like $.store.book[?(@.price<10)].title. Returns matched nodes."
}

func (t *JSONPathTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "data":{"description":"JSON data to query. Can be a JSON object, array, or JSON string"},
  "expression":{"type":"string","description":"JSONPath expression (e.g. $.store.book[*].author, $.store.book[?(@.price<10)].title)"}
},
"required":["data","expression"]
}`)
}

func (t *JSONPathTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Data       json.RawMessage `json:"data"`
		Expression string          `json:"expression"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Expression == "" {
		return "", fmt.Errorf("expression is required")
	}

	// Parse the data: it may be an object/array already, or a JSON string
	var data interface{}
	if len(p.Data) > 0 {
		switch p.Data[0] {
		case '{', '[':
			if err := json.Unmarshal(p.Data, &data); err != nil {
				return "", fmt.Errorf("invalid JSON data: %w", err)
			}
		case '"':
			var s string
			if err := json.Unmarshal(p.Data, &s); err != nil {
				return "", fmt.Errorf("invalid JSON string data: %w", err)
			}
			if err := json.Unmarshal([]byte(s), &data); err != nil {
				return "", fmt.Errorf("data string is not valid JSON: %w", err)
			}
		default:
			return "", fmt.Errorf("data must be a JSON object/array or a JSON-encoded string")
		}
	} else {
		return "", fmt.Errorf("data is required")
	}

	path, err := jsonpath.Parse(p.Expression)
	if err != nil {
		return "", fmt.Errorf("jsonpath parse error: %w", err)
	}

	results, err := path.Evaluate(data)
	if err != nil {
		return "", fmt.Errorf("jsonpath evaluate error: %w", err)
	}

	out := map[string]interface{}{
		"expression": jsonpath.FormatPath(path),
		"count":      len(results),
		"matches":    results,
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return string(b), nil
}
