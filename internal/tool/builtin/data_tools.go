package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"lumen/internal/exchange"
	"lumen/internal/tool"
	"lumen/internal/toolkit"
)

func init() {
	tool.RegisterBuiltin(&ConvertCSVToJSONTool{})
	tool.RegisterBuiltin(&ConvertJSONToCSVTool{})
	tool.RegisterBuiltin(&TextSummaryTool{})
	tool.RegisterBuiltin(&JSONGetTool{})
	tool.RegisterBuiltin(&EncodeBase64Tool{})
	tool.RegisterBuiltin(&DecodeBase64Tool{})
	tool.RegisterBuiltin(&EncodeHexTool{})
	tool.RegisterBuiltin(&ComputeHashTool{})
}

// ── convert_csv_to_json ─────────────────────────────────────────────────────

type ConvertCSVToJSONTool struct{}

func (t *ConvertCSVToJSONTool) Name() string   { return "convert_csv_to_json" }
func (t *ConvertCSVToJSONTool) ReadOnly() bool { return true }

func (t *ConvertCSVToJSONTool) Description() string {
	return "Convert CSV data to JSON. Provide CSV text with a header row; returns a JSON array of objects keyed by column headers."
}

func (t *ConvertCSVToJSONTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "csv":{"type":"string","description":"CSV text to convert, with a header row"}
},
"required":["csv"]
}`)
}

func (t *ConvertCSVToJSONTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		CSV string `json:"csv"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.CSV == "" {
		return "", fmt.Errorf("csv is required")
	}

	conv := exchange.NewConverter()
	result, err := conv.CSVToJSON([]byte(p.CSV))
	if err != nil {
		return "", fmt.Errorf("conversion failed: %w", err)
	}
	return string(result), nil
}

// ── convert_json_to_csv ─────────────────────────────────────────────────────

type ConvertJSONToCSVTool struct{}

func (t *ConvertJSONToCSVTool) Name() string   { return "convert_json_to_csv" }
func (t *ConvertJSONToCSVTool) ReadOnly() bool { return true }

func (t *ConvertJSONToCSVTool) Description() string {
	return "Convert JSON data (array of flat objects) to CSV. Provide the JSON text; returns CSV with a header row."
}

func (t *ConvertJSONToCSVTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "json":{"type":"string","description":"JSON array of flat objects to convert to CSV"}
},
"required":["json"]
}`)
}

func (t *ConvertJSONToCSVTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		JSON string `json:"json"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.JSON == "" {
		return "", fmt.Errorf("json is required")
	}

	conv := exchange.NewConverter()
	result, err := conv.JSONToCSV([]byte(p.JSON))
	if err != nil {
		return "", fmt.Errorf("conversion failed: %w", err)
	}
	return string(result), nil
}

// ── text_summary ────────────────────────────────────────────────────────────

type TextSummaryTool struct{}

func (t *TextSummaryTool) Name() string   { return "text_summary" }
func (t *TextSummaryTool) ReadOnly() bool { return true }

func (t *TextSummaryTool) Description() string {
	return "Analyze text and return a summary: character count, word count, line count, unique word count, and top word frequencies."
}

func (t *TextSummaryTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "text":{"type":"string","description":"Text to analyze"}
},
"required":["text"]
}`)
}

func (t *TextSummaryTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}

	st := toolkit.NewSummaryTool()
	stats := st.Analyze(p.Text)
	return st.FormatStats(stats), nil
}

// ── json_get ────────────────────────────────────────────────────────────────

type JSONGetTool struct{}

func (t *JSONGetTool) Name() string   { return "json_get" }
func (t *JSONGetTool) ReadOnly() bool { return true }

func (t *JSONGetTool) Description() string {
	return "Extract a value from a JSON object by dot-separated path (e.g. 'store.book.0.title'). Provide the JSON string and the path."
}

func (t *JSONGetTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "json":{"type":"string","description":"JSON string to query"},
  "path":{"type":"string","description":"Dot-separated path into the JSON object (e.g. store.book.0.title)"}
},
"required":["json","path"]
}`)
}

func (t *JSONGetTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		JSON string `json:"json"`
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.JSON == "" || p.Path == "" {
		return "", fmt.Errorf("json and path are required")
	}

	jt := toolkit.NewJSONTool()
	parsed, err := jt.Parse(p.JSON)
	if err != nil {
		return "", fmt.Errorf("JSON parse error: %w", err)
	}

	val, found := jt.Get(parsed, p.Path)
	if !found {
		return "", fmt.Errorf("path %q not found", p.Path)
	}

	b, _ := json.MarshalIndent(val, "", "  ")
	return string(b), nil
}

// ── encode_base64 ───────────────────────────────────────────────────────────

type EncodeBase64Tool struct{}

func (t *EncodeBase64Tool) Name() string   { return "encode_base64" }
func (t *EncodeBase64Tool) ReadOnly() bool { return true }

func (t *EncodeBase64Tool) Description() string {
	return "Encode a string to Base64."
}

func (t *EncodeBase64Tool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "input":{"type":"string","description":"String to encode"}
},
"required":["input"]
}`)
}

func (t *EncodeBase64Tool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Input string `json:"input"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}

	et := toolkit.NewEncodeTool()
	return et.Base64Encode(p.Input), nil
}

// ── decode_base64 ───────────────────────────────────────────────────────────

type DecodeBase64Tool struct{}

func (t *DecodeBase64Tool) Name() string   { return "decode_base64" }
func (t *DecodeBase64Tool) ReadOnly() bool { return true }

func (t *DecodeBase64Tool) Description() string {
	return "Decode a Base64-encoded string back to its original form."
}

func (t *DecodeBase64Tool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "input":{"type":"string","description":"Base64-encoded string to decode"}
},
"required":["input"]
}`)
}

func (t *DecodeBase64Tool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Input string `json:"input"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}

	et := toolkit.NewEncodeTool()
	result, err := et.Base64Decode(p.Input)
	if err != nil {
		return "", fmt.Errorf("base64 decode failed: %w", err)
	}
	return result, nil
}

// ── encode_hex ──────────────────────────────────────────────────────────────

type EncodeHexTool struct{}

func (t *EncodeHexTool) Name() string   { return "encode_hex" }
func (t *EncodeHexTool) ReadOnly() bool { return true }

func (t *EncodeHexTool) Description() string {
	return "Encode a string to hexadecimal."
}

func (t *EncodeHexTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "input":{"type":"string","description":"String to encode to hex"}
},
"required":["input"]
}`)
}

func (t *EncodeHexTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Input string `json:"input"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}

	et := toolkit.NewEncodeTool()
	return et.HexEncode(p.Input), nil
}

// ── compute_hash ────────────────────────────────────────────────────────────

type ComputeHashTool struct{}

func (t *ComputeHashTool) Name() string   { return "compute_hash" }
func (t *ComputeHashTool) ReadOnly() bool { return true }

func (t *ComputeHashTool) Description() string {
	return "Compute a hash of the input string. Supported algorithms: md5, sha256."
}

func (t *ComputeHashTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "algorithm":{"type":"string","description":"Hash algorithm: 'md5' or 'sha256'"},
  "input":{"type":"string","description":"String to hash"}
},
"required":["algorithm","input"]
}`)
}

func (t *ComputeHashTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Algorithm string `json:"algorithm"`
		Input     string `json:"input"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Algorithm == "" || p.Input == "" {
		return "", fmt.Errorf("algorithm and input are required")
	}

	et := toolkit.NewEncodeTool()
	switch strings.ToLower(p.Algorithm) {
	case "md5":
		return et.MD5(p.Input), nil
	case "sha256":
		return et.SHA256(p.Input), nil
	default:
		return "", fmt.Errorf("unsupported algorithm %q; use 'md5' or 'sha256'", p.Algorithm)
	}
}
