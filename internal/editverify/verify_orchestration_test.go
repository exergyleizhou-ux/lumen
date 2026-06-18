package editverify

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// scriptedRunner records the steps it runs and fails the named step.
type scriptedRunner struct {
	failStep string // step Name to fail (others succeed)
	output   string // output returned for the failing step
	ran      []string
}

func (r *scriptedRunner) Run(ctx context.Context, step Step) (string, bool) {
	r.ran = append(r.ran, step.Name)
	if step.Name == r.failStep {
		return r.output, false
	}
	return "", true
}

func TestVerify_AllPass(t *testing.T) {
	// Real root with the changed file's package dir present, so the same-module
	// filter keeps it and a test step is produced.
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "go.mod"), "module testmod\n\ngo 1.21\n")
	os.MkdirAll(filepath.Join(root, "foo"), 0o755)
	mustWrite(t, filepath.Join(root, "foo", "bar.go"), "package foo\n")

	r := &scriptedRunner{failStep: ""}
	v := New(root, DefaultConfig())
	v.run = r
	res := v.Verify(context.Background(), []string{"foo/bar.go"})
	if !res.OK {
		t.Fatalf("expected OK, got %+v", res)
	}
	// build, vet, then test for the changed pkg
	want := []string{"build", "vet", "test"}
	if len(r.ran) != len(want) {
		t.Fatalf("ran %v, want %v", r.ran, want)
	}
}

func TestVerify_StopsAtFirstFailure(t *testing.T) {
	// Real Go-module root with the changed file present, so the same-module
	// filter keeps it and goSteps produces the build step the runner fails.
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "go.mod"), "module testmod\n\ngo 1.21\n")
	os.MkdirAll(filepath.Join(root, "foo"), 0o755)
	mustWrite(t, filepath.Join(root, "foo", "bar.go"), "package foo\n")

	r := &scriptedRunner{failStep: "build", output: "foo/bar.go:3:5: undefined: x"}
	v := New(root, DefaultConfig())
	v.run = r
	res := v.Verify(context.Background(), []string{"foo/bar.go"})
	if res.OK {
		t.Fatal("expected failure")
	}
	if res.Failed == nil || res.Failed.Name != "build" {
		t.Fatalf("expected failed step 'build', got %+v", res.Failed)
	}
	// vet/test must NOT run after build fails
	if len(r.ran) != 1 || r.ran[0] != "build" {
		t.Errorf("should stop after build, ran %v", r.ran)
	}
	// Parse should have been wired in
	if len(res.Diagnostics) != 1 || res.Diagnostics[0].Msg != "undefined: x" {
		t.Errorf("diagnostics not parsed: %+v", res.Diagnostics)
	}
}

// TestVerify_RealToolchain is the end-to-end acceptance: a broken Go module
// fails the build step with diagnostics; once fixed, Verify passes.
func TestVerify_RealToolchain(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not in PATH")
	}
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "go.mod"), "module testmod\n\ngo 1.21\n")
	bad := "package main\n\nfunc main() {\n\tx := undefinedSymbol()\n\t_ = x\n}\n"
	mustWrite(t, filepath.Join(dir, "main.go"), bad)

	v := New(dir, DefaultConfig())
	res := v.Verify(context.Background(), []string{filepath.Join(dir, "main.go")})
	if res.OK {
		t.Fatal("broken module should fail verification")
	}
	if res.Failed == nil || res.Failed.Name != "build" {
		t.Fatalf("expected build failure, got %+v", res.Failed)
	}
	if len(res.Diagnostics) == 0 {
		t.Errorf("expected build diagnostics, got none; output=%q", res.Output)
	}

	// Fix it → should pass.
	good := "package main\n\nfunc main() {\n\t_ = 1\n}\n"
	mustWrite(t, filepath.Join(dir, "main.go"), good)
	res = v.Verify(context.Background(), []string{filepath.Join(dir, "main.go")})
	if !res.OK {
		t.Fatalf("fixed module should pass, got %+v (output=%q)", res.Failed, res.Output)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
