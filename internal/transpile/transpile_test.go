package transpile

import (
	"strings"
	"testing"
)

func TestProtoToTS(t *testing.T) {
	tr := NewTranspiler()
	msg := ProtoMessage{Name: "User", Fields: []ProtoField{{Name: "id", Type: "int32", Number: 1}, {Name: "name", Type: "string", Number: 2, Optional: true}}}
	result := tr.ProtoToTypeScript(msg)
	if !strings.Contains(result, "export interface User") {
		t.Error("interface")
	}
	if !strings.Contains(result, "id") {
		t.Error("id field")
	}
}
func TestProtoToGo(t *testing.T) {
	tr := NewTranspiler()
	msg := ProtoMessage{Name: "Item", Fields: []ProtoField{{Name: "value", Type: "double", Number: 1}}}
	result := tr.ProtoToGo(msg)
	if !strings.Contains(result, "type Item struct") {
		t.Error("struct")
	}
}
func TestProtoToSQL(t *testing.T) {
	tr := NewTranspiler()
	msg := ProtoMessage{Name: "Product", Fields: []ProtoField{{Name: "price", Type: "double", Number: 1}, {Name: "tags", Type: "string", Number: 2, Repeated: true}}}
	result := tr.ProtoToSQL(msg)
	if !strings.Contains(result, "CREATE TABLE") {
		t.Error("ddl")
	}
}
func TestJSONSchema(t *testing.T) {
	tr := NewTranspiler()
	s := map[string]any{"title": "Config", "properties": map[string]any{"enabled": map[string]any{"type": "boolean"}}}
	result := tr.JSONSchemaToTypes(s, FormatTypeScript)
	if !strings.Contains(result, "enabled") {
		t.Error("ts gen")
	}
}
func TestHelpers(t *testing.T) {
	if toCamel("user_id") != "userId" {
		t.Error("camel")
	}
	if toPascal("user_name") != "UserName" {
		t.Error("pascal")
	}
}
