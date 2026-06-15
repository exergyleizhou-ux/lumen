package codemap

import (
	"testing"
)

func TestAddSymbol(t *testing.T) {
	m := NewMap()
	m.AddSymbol(&Symbol{Name: "Foo", Kind: "func", Package: "pkg", File: "pkg/file.go", Line: 10, Exported: true})
	if len(m.Symbols()) != 1 {
		t.Error("symbol count")
	}
}
func TestCalleesCallers(t *testing.T) {
	m := NewMap()
	m.AddCall("main", "foo", "main.go")
	m.AddCall("main", "bar", "main.go")
	callees := m.Callees("main")
	if len(callees) != 2 {
		t.Error("callees")
	}
}
func TestCircularDeps(t *testing.T) {
	m := NewMap()
	m.AddImport("a", "b")
	m.AddImport("b", "a")
	cycles := m.CircularDeps()
	if len(cycles) == 0 {
		t.Error("should detect cycle")
	}
}
func TestFormatCallGraph(t *testing.T) {
	m := NewMap()
	m.AddCall("A", "B", "file.go")
	s := m.FormatCallGraph()
	if s == "" {
		t.Error("format")
	}
}
