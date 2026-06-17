package oasis

import (
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestScaffoldedAlgoBuilds(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not in PATH")
	}
	dir := t.TempDir()
	if err := Scaffold(dir, DefaultManifest("demo")); err != nil {
		t.Fatal(err)
	}
	// The Dockerfile runs `go build ./cmd/algo` in the build context, so the
	// scaffold must produce a buildable module (it was missing go.mod).
	cmd := exec.Command("go", "build", "./cmd/algo")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOFLAGS=-mod=mod")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("scaffolded algorithm should build out of the box: %v\n%s", err, out)
	}
}

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
