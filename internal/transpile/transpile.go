// Package transpile provides code transpilation utilities for common
// format conversions: Protobuf ↔ TypeScript, JSON Schema ↔ Go types,
// and SQL DDL generation from schema definitions.
package transpile

import (
	"fmt"
	"sort"
	"strings"
)

// Format is a target output format.
type Format string

const (
	FormatTypeScript Format = "typescript"
	FormatGo         Format = "go"
	FormatSQL        Format = "sql"
	FormatJSONSchema Format = "json-schema"
)

// ProtoMessage represents a simplified protobuf message definition.
type ProtoMessage struct {
	Name   string       `json:"name"`
	Fields []ProtoField `json:"fields"`
	Enums  []ProtoEnum  `json:"enums,omitempty"`
}

// ProtoField is one field in a protobuf message.
type ProtoField struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Number   int    `json:"number"`
	Repeated bool   `json:"repeated,omitempty"`
	Optional bool   `json:"optional,omitempty"`
}

// ProtoEnum is an enum definition.
type ProtoEnum struct {
	Name   string         `json:"name"`
	Values map[string]int `json:"values"`
}

// Transpiler converts between formats.
type Transpiler struct{}

// NewTranspiler creates a transpiler.
func NewTranspiler() *Transpiler { return &Transpiler{} }

// ProtoToTypeScript converts a proto message to TypeScript interface.
func (tr *Transpiler) ProtoToTypeScript(msg ProtoMessage) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("// Generated from %s.proto\n", msg.Name))
	sb.WriteString(fmt.Sprintf("export interface %s {\n", msg.Name))

	// Enums first
	for _, e := range msg.Enums {
		sb.WriteString(fmt.Sprintf("  export enum %s {\n", e.Name))
		var keys []string
		for k := range e.Values {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			sb.WriteString(fmt.Sprintf("    %s = %d,\n", k, e.Values[k]))
		}
		sb.WriteString("  }\n")
	}

	for _, f := range msg.Fields {
		tsType := mapProtoToTS(f.Type)
		if f.Repeated {
			tsType += "[]"
		}
		if f.Optional {
			tsType += " | undefined"
		}
		sb.WriteString(fmt.Sprintf("  %s: %s;\n", toCamel(f.Name), tsType))
	}
	sb.WriteString("}\n")
	return sb.String()
}

// ProtoToGo converts a proto message to Go struct.
func (tr *Transpiler) ProtoToGo(msg ProtoMessage) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("// Code generated from proto. DO NOT EDIT.\n\n"))
	sb.WriteString(fmt.Sprintf("type %s struct {\n", msg.Name))
	for _, f := range msg.Fields {
		goType := mapProtoToGo(f.Type)
		if f.Repeated {
			goType = "[]" + goType
		}
		jsonTag := fmt.Sprintf("`json:\"%s,omitempty\"`", toSnake(f.Name))
		sb.WriteString(fmt.Sprintf("\t%s %s %s\n", toPascal(f.Name), goType, jsonTag))
	}
	sb.WriteString("}\n")
	return sb.String()
}

// ProtoToSQL converts proto message to CREATE TABLE DDL.
func (tr *Transpiler) ProtoToSQL(msg ProtoMessage) string {
	var sb strings.Builder
	tableName := toSnake(msg.Name)
	sb.WriteString(fmt.Sprintf("-- Generated DDL for %s\n", tableName))
	sb.WriteString(fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n", tableName))
	sb.WriteString("  id SERIAL PRIMARY KEY,\n")
	for _, f := range msg.Fields {
		sqlType := mapProtoToSQL(f.Type)
		if f.Repeated {
			sqlType = "JSONB"
		} // Repeated fields → JSONB array
		colName := toSnake(f.Name)
		nullable := "NOT NULL"
		if f.Optional {
			nullable = ""
		}
		sb.WriteString(fmt.Sprintf("  %s %s %s,\n", colName, sqlType, nullable))
	}
	sb.WriteString(fmt.Sprintf("  created_at TIMESTAMP DEFAULT NOW(),\n"))
	sb.WriteString(fmt.Sprintf("  updated_at TIMESTAMP DEFAULT NOW()\n"))
	sb.WriteString(");\n")
	return sb.String()
}

// JSONSchemaToTypes converts JSON Schema to language types.
func (tr *Transpiler) JSONSchemaToTypes(schema map[string]any, format Format) string {
	var sb strings.Builder
	title, _ := schema["title"].(string)
	if title == "" {
		title = "Unknown"
	}
	props, _ := schema["properties"].(map[string]any)

	switch format {
	case FormatTypeScript:
		sb.WriteString(fmt.Sprintf("export interface %s {\n", toPascal(title)))
		for name, prop := range props {
			pm, _ := prop.(map[string]any)
			tsType := jsonSchemaToTS(pm)
			sb.WriteString(fmt.Sprintf("  %s: %s;\n", toCamel(name), tsType))
		}
		sb.WriteString("}\n")
	case FormatGo:
		sb.WriteString(fmt.Sprintf("type %s struct {\n", toPascal(title)))
		for name, prop := range props {
			pm, _ := prop.(map[string]any)
			goType := jsonSchemaToGo(pm)
			sb.WriteString(fmt.Sprintf("\t%s %s `json:\"%s\"`\n", toPascal(name), goType, toSnake(name)))
		}
		sb.WriteString("}\n")
	}
	return sb.String()
}

// ── Type Mappings ────────────────────────────────────────

func mapProtoToTS(pt string) string {
	switch pt {
	case "string":
		return "string"
	case "int32", "int64", "uint32", "uint64", "sint32", "sint64", "fixed32", "fixed64", "sfixed32", "sfixed64":
		return "number"
	case "bool":
		return "boolean"
	case "double", "float":
		return "number"
	case "bytes":
		return "Uint8Array"
	default:
		return pt
	}
}

func mapProtoToGo(pt string) string {
	switch pt {
	case "string":
		return "string"
	case "int32":
		return "int32"
	case "int64":
		return "int64"
	case "uint32":
		return "uint32"
	case "uint64":
		return "uint64"
	case "bool":
		return "bool"
	case "double":
		return "float64"
	case "float":
		return "float32"
	case "bytes":
		return "[]byte"
	default:
		return "interface{}"
	}
}

func mapProtoToSQL(pt string) string {
	switch pt {
	case "string":
		return "TEXT"
	case "int32", "int64", "uint32", "uint64":
		return "BIGINT"
	case "bool":
		return "BOOLEAN"
	case "double", "float":
		return "DOUBLE PRECISION"
	case "bytes":
		return "BYTEA"
	default:
		return "JSONB"
	}
}

func jsonSchemaToTS(pm map[string]any) string {
	typ, _ := pm["type"].(string)
	switch typ {
	case "string":
		return "string"
	case "number", "integer":
		return "number"
	case "boolean":
		return "boolean"
	case "array":
		return "any[]"
	case "object":
		return "Record<string, any>"
	default:
		return "any"
	}
}

func jsonSchemaToGo(pm map[string]any) string {
	typ, _ := pm["type"].(string)
	switch typ {
	case "string":
		return "string"
	case "number":
		return "float64"
	case "integer":
		return "int"
	case "boolean":
		return "bool"
	default:
		return "interface{}"
	}
}

func toCamel(s string) string {
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if i == 0 {
			parts[i] = strings.ToLower(p)
		} else {
			parts[i] = strings.Title(p)
		}
	}
	return strings.Join(parts, "")
}

func toPascal(s string) string {
	parts := strings.Split(s, "_")
	for i, p := range parts {
		parts[i] = strings.Title(p)
	}
	return strings.Join(parts, "")
}

func toSnake(s string) string {
	var result []byte
	for i, c := range s {
		if c >= 'A' && c <= 'Z' {
			if i > 0 {
				result = append(result, '_')
			}
			result = append(result, byte(c+32))
		} else {
			result = append(result, byte(c))
		}
	}
	return string(result)
}
