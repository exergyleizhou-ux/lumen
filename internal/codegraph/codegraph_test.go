package codegraph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewGraph(t *testing.T) {
	g := NewGraph("/tmp")
	if g == nil {
		t.Fatal("NewGraph returned nil")
	}
	if g.loaded {
		t.Error("new graph should not be loaded")
	}
}

func TestLoadGoFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte(`package main

func main() { println("hi") }

type Config struct {
	Name string
}
`), 0o644)

	g := NewGraph(dir)
	err := g.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	syms := g.FindSymbol("main")
	if len(syms) == 0 {
		t.Error("should find at least one symbol matching 'main'")
	}

	syms2 := g.FindSymbol("Config")
	if len(syms2) == 0 {
		t.Error("should find Config type")
	}
}

func TestLoadSkipTestFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main_test.go"), []byte(`package main
func TestX(t *testing.T) {}`), 0o644)

	g := NewGraph(dir)
	g.Load()
	if len(g.symbols) > 0 {
		t.Error("test files should be skipped")
	}
}

func TestFindSymbol(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "app.go"), []byte(`package app
func Run() error { return nil }
type Server struct { Port int }
`), 0o644)

	g := NewGraph(dir)
	g.Load()

	if syms := g.FindSymbol("Run"); len(syms) == 0 {
		t.Error("should find Run")
	}
	if syms := g.FindSymbol("Server"); len(syms) == 0 {
		t.Error("should find Server")
	}
	if syms := g.FindSymbol("nonexistent"); len(syms) != 0 {
		t.Error("should not find nonexistent")
	}
}

func TestSymbolsByKind(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "types.go"), []byte(`package p
func Hello() {}
type World struct {}
type Runner interface { Run() }
`), 0o644)

	g := NewGraph(dir)
	g.Load()

	funcs := g.SymbolsByKind("func")
	if len(funcs) == 0 {
		t.Error("should find func symbols")
	}

	types := g.SymbolsByKind("type")
	if len(types) < 1 {
		t.Error("should find type symbols")
	}
}

func TestExtractFuncName(t *testing.T) {
	if name := extractFuncName("func Run(ctx context.Context)"); name != "Run" {
		t.Errorf("extract: got %q", name)
	}
	if name := extractFuncName("func (s *Server) Start() error"); name != "Start" {
		t.Errorf("method extract: got %q", name)
	}
}

func TestExtractTypeName(t *testing.T) {
	if name := extractTypeName("type Server struct {"); name != "Server" {
		t.Errorf("extract: got %q", name)
	}
	if name := extractTypeName("type Runner interface {"); name != "Runner" {
		t.Errorf("interface extract: got %q", name)
	}
}

func TestIsExported(t *testing.T) {
	if !isExported("Run") {
		t.Error("Run should be exported")
	}
	if isExported("run") {
		t.Error("run should not be exported")
	}
	if isExported("") {
		t.Error("empty should not be exported")
	}
}

func TestGuruQueryNotInstalled(t *testing.T) {
	_, err := GuruQuery("/tmp", "definition", "main.go:#1")
	if err == nil {
		t.Skip("guru found, but test expects it not installed")
	}
	if !strings.Contains(err.Error(), "not installed") {
		t.Logf("guru error: %v", err)
	}
}
