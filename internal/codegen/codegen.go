// Package codegen generates Go source code from templates: tool stubs,
// test scaffolding, mocks, and CRUD handlers. It parses existing code to
// extract type information and generates idiomatic Go code that matches
// the project's conventions.
package codegen

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// GenFile is one generated file.
type GenFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// Context holds information for code generation.
type Context struct {
	Package string     `json:"package"`
	Types   []TypeInfo `json:"types"`
	Imports []string   `json:"imports"`
	Module  string     `json:"module"`
}

// TypeInfo describes a Go type for code generation.
type TypeInfo struct {
	Name    string       `json:"name"`
	Kind    string       `json:"kind"` // "struct", "interface"
	Fields  []FieldInfo  `json:"fields,omitempty"`
	Methods []MethodInfo `json:"methods,omitempty"`
}

// FieldInfo is one struct field.
type FieldInfo struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Tags string `json:"tags,omitempty"`
}

// MethodInfo is one interface method.
type MethodInfo struct {
	Name    string      `json:"name"`
	Params  []ParamInfo `json:"params"`
	Returns []ParamInfo `json:"returns"`
}

// ParamInfo is a function parameter.
type ParamInfo struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// Generator produces code from templates.
type Generator struct {
	mu     sync.Mutex
	module string
}

// NewGenerator creates a code generator for the given module path.
func NewGenerator(module string) *Generator {
	return &Generator{module: module}
}

// AnalyzeFile parses a Go file and extracts type information.
func (g *Generator) AnalyzeFile(path string) (*Context, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	ctx := &Context{
		Package: node.Name.Name,
		Module:  g.module,
	}

	for _, imp := range node.Imports {
		ctx.Imports = append(ctx.Imports, strings.Trim(imp.Path.Value, `"`))
	}

	return ctx, nil
}

// GenerateToolStub creates a new builtin tool file.
func (g *Generator) GenerateToolStub(name, description string, readOnly bool) *GenFile {
	var sb strings.Builder
	sb.WriteString("package builtin\n\n")
	sb.WriteString("import (\n")
	sb.WriteString("\t\"context\"\n")
	sb.WriteString("\t\"encoding/json\"\n")
	sb.WriteString("\t\"fmt\"\n")
	sb.WriteString("\t\"lumen/internal/tool\"\n")
	sb.WriteString(")\n\n")
	sb.WriteString("func init() {\n")
	sb.WriteString(fmt.Sprintf("\ttool.RegisterBuiltin(&%sTool{})\n", toCamel(name)))
	sb.WriteString("}\n\n")
	sb.WriteString(fmt.Sprintf("type %sTool struct{}\n\n", toCamel(name)))
	sb.WriteString(fmt.Sprintf("func (t *%sTool) Name() string { return %q }\n", toCamel(name), name))
	ro := "false"
	if readOnly {
		ro = "true"
	}
	sb.WriteString(fmt.Sprintf("func (t *%sTool) ReadOnly() bool { return %s }\n\n", toCamel(name), ro))
	sb.WriteString(fmt.Sprintf("func (t *%sTool) Description() string { return %q }\n\n", toCamel(name), description))
	sb.WriteString(fmt.Sprintf("func (t *%sTool) Schema() json.RawMessage {\n", toCamel(name)))
	sb.WriteString(fmt.Sprintf("\treturn json.RawMessage(`{`+\"`\"+`\"type\":\"object\",\"properties\":{}}`+\"`\"+`)\n"))
	sb.WriteString("}\n\n")
	sb.WriteString(fmt.Sprintf("func (t *%sTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {\n", toCamel(name)))
	sb.WriteString("\treturn \"\", fmt.Errorf(\"not yet implemented\")\n")
	sb.WriteString("}\n")
	return &GenFile{Path: name + ".go", Content: sb.String()}
}

// GenerateTestStub creates a test file for a Go source file.
func (g *Generator) GenerateTestStub(sourcePath, pkg string, funcs []string) *GenFile {
	var sb strings.Builder
	fmt.Fprintf(&sb, "package %s\n\nimport \"testing\"\n\n", pkg)
	for _, f := range funcs {
		fmt.Fprintf(&sb, "func Test%s(t *testing.T) {\n", toCamel(f))
		sb.WriteString("\tt.Skip(\"TODO: implement\")\n")
		sb.WriteString("}\n\n")
	}
	testPath := strings.TrimSuffix(filepath.Base(sourcePath), ".go") + "_test.go"
	return &GenFile{Path: testPath, Content: sb.String()}
}

// WriteFile writes a generated file to disk.
func (g *Generator) WriteFile(gf *GenFile, dir string) error {
	os.MkdirAll(dir, 0o755)
	return os.WriteFile(filepath.Join(dir, gf.Path), []byte(gf.Content), 0o644)
}

// FormatFiles returns a list of all generated files.
func (g *Generator) FormatFiles(files []*GenFile) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Generated %d file(s):\n", len(files))
	for _, f := range files {
		fmt.Fprintf(&sb, "  %s (%d bytes)\n", f.Path, len(f.Content))
	}
	return sb.String()
}

func toCamel(s string) string {
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, "")
}

// ── Mock generator ────────────────────────────────────

// Mock generates a mock implementation of an interface.
func (g *Generator) Mock(ifaceName, pkg string, methods []MethodInfo) *GenFile {
	var sb strings.Builder
	fmt.Fprintf(&sb, "package %s\n\n", pkg)
	sb.WriteString("import \"sync\"\n\n")
	fmt.Fprintf(&sb, "type Mock%s struct {\n", ifaceName)
	sb.WriteString("\tmu sync.Mutex\n")
	sb.WriteString("\tcalls map[string]int\n")
	sb.WriteString("}\n\n")
	fmt.Fprintf(&sb, "func NewMock%s() *Mock%s {\n", ifaceName, ifaceName)
	fmt.Fprintf(&sb, "\treturn &Mock%s{calls: map[string]int{}}\n", ifaceName)
	sb.WriteString("}\n\n")
	for _, m := range methods {
		fmt.Fprintf(&sb, "func (m *Mock%s) %s(", ifaceName, m.Name)
		for i, p := range m.Params {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(p.Name + " " + p.Type)
		}
		sb.WriteString(") ")
		for i, r := range m.Returns {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(r.Type)
		}
		sb.WriteString(" {\n")
		fmt.Fprintf(&sb, "\tm.mu.Lock()\n\tm.calls[%q]++\n\tm.mu.Unlock()\n", m.Name)
		sb.WriteString("\treturn ")
		for i, r := range m.Returns {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(zeroValue(r.Type))
		}
		sb.WriteString("\n}\n\n")
	}
	return &GenFile{Path: "mock_" + strings.ToLower(ifaceName) + ".go", Content: sb.String()}
}

func zeroValue(t string) string {
	switch t {
	case "string":
		return `""`
	case "int", "int64":
		return "0"
	case "bool":
		return "false"
	case "error":
		return "nil"
	default:
		if strings.HasPrefix(t, "*") || strings.HasPrefix(t, "[]") {
			return "nil"
		}
		return "nil"
	}
}
