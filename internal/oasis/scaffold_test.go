package oasis

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScaffoldGeneratesContractAccurateGo(t *testing.T) {
	dir := t.TempDir()
	if err := Scaffold(dir, DefaultManifest("demo")); err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join(dir, "cmd", "algo", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	// Must be valid Go (the old template had unquoted struct tags and didn't compile).
	if _, err := parser.ParseFile(token.NewFileSet(), "main.go", src, parser.AllErrors); err != nil {
		t.Fatalf("scaffolded main.go is not valid Go: %v", err)
	}
	// Must follow the real C2D contract, not the stdin/stdout shape.
	s := string(src)
	if !strings.Contains(s, "/out/input.json") {
		t.Error("template should read params from /out/input.json (the runner provides them there)")
	}
	if strings.Contains(s, "os.Stdin") {
		t.Error("template must not read params from stdin — that is not the C2D contract")
	}
	if !strings.Contains(s, "os.Stdout") {
		t.Error("template should write the result to stdout (the runner captures it as output.json)")
	}
}
