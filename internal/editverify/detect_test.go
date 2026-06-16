package editverify

import (
	"testing"
)

func TestDetect_noChanges(t *testing.T) {
	cfg := DefaultConfig()
	steps := Detect("/project", nil, cfg)

	// Must return build+vet even with no changed files
	if len(steps) < 2 {
		t.Fatalf("got %d steps, want at least 2", len(steps))
	}
	if steps[0].Name != "build" || steps[0].Args[0] != "go" || steps[0].Args[1] != "build" {
		t.Errorf("step[0] = %v, want build", steps[0])
	}
	if steps[1].Name != "vet" || steps[1].Args[0] != "go" || steps[1].Args[1] != "vet" {
		t.Errorf("step[1] = %v, want vet", steps[1])
	}
	// No .go changes → no test steps
	if len(steps) != 2 {
		t.Errorf("got %d steps with no changes, want exactly 2 (build+vet)", len(steps))
	}
}

func TestDetect_singlePkg(t *testing.T) {
	cfg := DefaultConfig()
	changed := []string{"pkg/main.go", "pkg/helper.go", "README.md"}
	steps := Detect("/project", changed, cfg)

	if len(steps) != 3 {
		t.Fatalf("got %d steps, want 3 (build+vet+test)", len(steps))
	}
	if steps[2].Name != "test" {
		t.Errorf("step[2] name = %q, want test", steps[2].Name)
	}
	if len(steps[2].Args) != 3 || steps[2].Args[2] != "./pkg" {
		t.Errorf("step[2].Args = %v, want [go test ./pkg]", steps[2].Args)
	}
}

func TestDetect_multiPkgDedupe(t *testing.T) {
	cfg := DefaultConfig()
	changed := []string{
		"pkg/auth/login.go",
		"pkg/auth/middleware.go",
		"pkg/models/user.go",
		"pkg/auth/extra.go", // same dir as above — should be deduped
	}
	steps := Detect("/project", changed, cfg)

	if len(steps) != 4 {
		t.Fatalf("got %d steps, want 4 (build+vet+test/auth+test/models)", len(steps))
	}
	if steps[2].Name != "test" || steps[3].Name != "test" {
		t.Errorf("steps 2/3 should both be test, got %s/%s", steps[2].Name, steps[3].Name)
	}

	// Ensure dedup: the two test steps must be for different packages
	pkgs := map[string]bool{}
	for _, s := range steps {
		if s.Name == "test" {
			pkgs[s.Args[2]] = true
		}
	}
	if len(pkgs) != 2 {
		t.Errorf("expected 2 distinct test packages, got %v", pkgs)
	}
}

func TestDetect_scopeAll(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Scope = "all"
	changed := []string{"pkg/a.go", "pkg/b.go"}
	steps := Detect("/project", changed, cfg)

	if len(steps) != 3 {
		t.Fatalf("got %d steps, want 3 (build+vet+test/...)", len(steps))
	}
	if steps[2].Args[2] != "./..." {
		t.Errorf("scope=all test step = %v, want ./...", steps[2].Args)
	}
}

func TestDetect_commandOverride(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Command = "golangci-lint run"
	changed := []string{"a.go", "b.go", "c.go"}
	steps := Detect("/project", changed, cfg)

	if len(steps) != 1 {
		t.Fatalf("got %d steps, want 1 (custom)", len(steps))
	}
	if steps[0].Name != "custom" {
		t.Errorf("step[0].Name = %q, want custom", steps[0].Name)
	}
	if steps[0].Args[0] != "sh" || steps[0].Args[1] != "-c" || steps[0].Args[2] != "golangci-lint run" {
		t.Errorf("step[0].Args = %v", steps[0].Args)
	}
}

func TestDetect_nonGoFilesOnly(t *testing.T) {
	cfg := DefaultConfig()
	changed := []string{"README.md", "lumen.toml", "docs/spec.md"}
	steps := Detect("/project", changed, cfg)

	// No .go files → only build+vet
	if len(steps) != 2 {
		t.Errorf("got %d steps for non-.go changes, want 2", len(steps))
	}
}

func TestDetect_runTestsFalse(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RunTests = false
	changed := []string{"pkg/main.go"}
	steps := Detect("/project", changed, cfg)

	if len(steps) != 2 {
		t.Fatalf("got %d steps, want 2 (build+vet only)", len(steps))
	}
	for _, s := range steps {
		if s.Name == "test" {
			t.Fatal("should not have test step when RunTests=false")
		}
	}
}

func TestDetect_rootPkg(t *testing.T) {
	cfg := DefaultConfig()
	changed := []string{"main.go"} // root package
	steps := Detect("/project", changed, cfg)

	if len(steps) != 3 {
		t.Fatalf("got %d steps, want 3", len(steps))
	}
	if steps[2].Args[2] != "." {
		t.Errorf("root pkg test step = %v, want go test .", steps[2].Args)
	}
}
