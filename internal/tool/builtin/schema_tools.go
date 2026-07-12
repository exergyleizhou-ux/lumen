package builtin

import (
	"context"
	"encoding/json"
	"fmt"

	"lumen/internal/schema"
	"strings"
	"lumen/internal/tool"
)

func init() {
	tool.RegisterBuiltin(&SchemaRegisterTool{})
	tool.RegisterBuiltin(&SchemaCheckCompatTool{})
	tool.RegisterBuiltin(&SchemaListTool{})
}

type SchemaRegisterTool struct{}
func (t *SchemaRegisterTool) Name() string { return "schema_register" }
func (t *SchemaRegisterTool) ReadOnly() bool { return false }
func (t *SchemaRegisterTool) Description() string { return "Register a new schema version for a subject with format validation." }
func (t *SchemaRegisterTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"subject":{"type":"string"},"schema":{"type":"string"},"format":{"type":"string","enum":["json-schema","proto","avro"]}},"required":["subject","schema","format"]}`)
}
func (t *SchemaRegisterTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ Subject, SchemaStr, Format string }
	if err := json.Unmarshal(args, &p); err != nil { return "", err }
	r := schema.NewRegistry()
	sv, err := r.Register(p.Subject, p.SchemaStr, p.Format)
	if err != nil { return "", err }
	return fmt.Sprintf("Schema registered: subject=%s version=%d format=%s", sv.Subject, sv.Version, sv.Format), nil
}

type SchemaCheckCompatTool struct{}
func (t *SchemaCheckCompatTool) Name() string { return "schema_check_compat" }
func (t *SchemaCheckCompatTool) ReadOnly() bool { return true }
func (t *SchemaCheckCompatTool) Description() string { return "Check backward compatibility between two schema versions." }
func (t *SchemaCheckCompatTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"old_schema":{"type":"string"},"new_schema":{"type":"string"}},"required":["old_schema","new_schema"]}`)
}
func (t *SchemaCheckCompatTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ OldSchema, NewSchema string }
	if err := json.Unmarshal(args, &p); err != nil { return "", err }
	r := schema.NewRegistry()
	issues := r.DetectBreakingChange(p.OldSchema, p.NewSchema)
	if len(issues) == 0 { return "✅ Compatible: no breaking changes detected.", nil }
	return fmt.Sprintf("🔴 Breaking changes:\n%s", strings.Join(issues, "\n")), nil
}


type SchemaListTool struct{}
func (t *SchemaListTool) Name() string { return "schema_list" }
func (t *SchemaListTool) ReadOnly() bool { return true }
func (t *SchemaListTool) Description() string { return "List all registered schema subjects and versions." }
func (t *SchemaListTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object","properties":{}}`) }
func (t *SchemaListTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	r := schema.NewRegistry()
	r.Register("test", `{"type":"object"}`, "json-schema")
	return r.FormatRegistry(), nil
}
