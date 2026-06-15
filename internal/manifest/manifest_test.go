package manifest

import (
	"strings"
	"testing"
)

func TestNewManifest(t *testing.T) {
	m := New("myapp")
	if m.Name != "myapp" || m.Version != "0.1.0" {
		t.Fatalf("name=%s version=%s", m.Name, m.Version)
	}
}

func TestAddDependency(t *testing.T) {
	m := New("app")
	m.AddDependency("libfoo", "^1.2.0")
	if len(m.Dependencies) != 1 {
		t.Fatal("expected 1 dep")
	}
	if m.FindDependency("libfoo") == nil {
		t.Fatal("dep not found")
	}
	if m.FindDependency("nonexistent") != nil {
		t.Fatal("should be nil")
	}
}

func TestAddScript(t *testing.T) {
	m := New("app")
	m.AddScript("build", "go build ./...")
	s := m.FindScript("build")
	if s == nil || s.Command != "go build ./..." {
		t.Fatal("script not found")
	}
}

func TestAddEnvironment(t *testing.T) {
	m := New("app")
	env := m.AddEnvironment("prod")
	env.Variables["LOG_LEVEL"] = "info"
	if m.FindEnvironment("prod") == nil {
		t.Fatal("env not found")
	}
	if m.FindEnvironment("prod").Variables["LOG_LEVEL"] != "info" {
		t.Fatal("var not set")
	}
}

const sampleTOML = `
name = "myapp"
version = "0.2.0"
description = "A sample app"
license = "MIT"
repository = "https://github.com/example/myapp"

[dependencies]
libfoo = "^1.2.0"
libbar = ">=2.0.0"

[dev-dependencies]
testlib = "0.9.0"

[scripts]
build = "go build ./..."
test = "go test ./..."
deploy = "./deploy.sh"

[environments.prod]
LOG_LEVEL = "info"
DATABASE_URL = "postgres://..."

[environments.staging]
LOG_LEVEL = "debug"

[build]
command = "go build -o myapp"
output = "myapp"
target_os = "linux"
target_arch = "amd64"
`

func TestParseTOML(t *testing.T) {
	m, err := ParseTOML(sampleTOML)
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "myapp" {
		t.Fatalf("name: %s", m.Name)
	}
	if m.Version != "0.2.0" {
		t.Fatalf("version: %s", m.Version)
	}
	if m.License != "MIT" {
		t.Fatalf("license: %s", m.License)
	}
	if len(m.Dependencies) != 2 {
		t.Fatalf("deps: %d", len(m.Dependencies))
	}
	if len(m.DevDependencies) != 1 {
		t.Fatalf("dev deps: %d", len(m.DevDependencies))
	}
	if len(m.Scripts) != 3 {
		t.Fatalf("scripts: %d", len(m.Scripts))
	}
	if len(m.Environments) != 2 {
		t.Fatalf("envs: %d", len(m.Environments))
	}
	if m.Build == nil || m.Build.Output != "myapp" {
		t.Fatal("build config")
	}
}

func TestFormatManifest(t *testing.T) {
	m, _ := ParseTOML(sampleTOML)
	out := FormatManifest(m, DefaultFormatOptions())
	if !strings.Contains(out, "myapp") {
		t.Fatal("missing name")
	}
	if !strings.Contains(out, "[dependencies]") {
		t.Fatal("missing dependencies section")
	}
	if !strings.Contains(out, "[scripts]") {
		t.Fatal("missing scripts section")
	}
	if !strings.Contains(out, "[environments.prod]") {
		t.Fatal("missing env section")
	}
}

func TestParseFile(t *testing.T) {
	m, err := ParseFile("Lumen.toml", sampleTOML)
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "myapp" {
		t.Fatal("parse failed")
	}

	_, err = ParseFile("Lumen.yaml", "")
	if err == nil {
		t.Fatal("expected error for yaml")
	}
}

func TestVersionConstraint_Exact(t *testing.T) {
	vc, err := ParseVersionConstraint("1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if !vc.SatisfiedBy("1.2.3") {
		t.Fatal("should match")
	}
	if vc.SatisfiedBy("1.2.4") {
		t.Fatal("should not match")
	}
}

func TestVersionConstraint_Caret(t *testing.T) {
	vc, err := ParseVersionConstraint("^1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if !vc.SatisfiedBy("1.2.3") || !vc.SatisfiedBy("1.5.0") {
		t.Fatal("should match")
	}
	if vc.SatisfiedBy("2.0.0") {
		t.Fatal("major bump should not match")
	}
}

func TestVersionConstraint_Tilde(t *testing.T) {
	vc, err := ParseVersionConstraint("~1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if !vc.SatisfiedBy("1.2.3") || !vc.SatisfiedBy("1.2.9") {
		t.Fatal("should match")
	}
}

func TestVersionConstraint_Range(t *testing.T) {
	vc, err := ParseVersionConstraint(">=1.0.0 <2.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if !vc.SatisfiedBy("1.5.0") {
		t.Fatal("should match")
	}
	if vc.SatisfiedBy("2.0.0") {
		t.Fatal("should not match")
	}
	if vc.SatisfiedBy("0.9.0") {
		t.Fatal("should not match")
	}
}

func TestVersionConstraint_Wildcard(t *testing.T) {
	vc, err := ParseVersionConstraint("*")
	if err != nil {
		t.Fatal(err)
	}
	if !vc.SatisfiedBy("999.0.0") {
		t.Fatal("wildcard should match all")
	}
}

func TestVersionConstraint_DashRange(t *testing.T) {
	vc, err := ParseVersionConstraint("1.0.0 - 2.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if !vc.SatisfiedBy("1.0.0") || !vc.SatisfiedBy("2.0.0") {
		t.Fatal("should match endpoints")
	}
	if vc.SatisfiedBy("0.9.0") || vc.SatisfiedBy("2.1.0") {
		t.Fatal("should not match outside range")
	}
}

func TestSortDependencies(t *testing.T) {
	deps := []Dependency{
		{Name: "c", Version: "1.0"},
		{Name: "a", Version: "2.0"},
		{Name: "b", Version: "1.5"},
	}
	SortDependencies(deps)
	if deps[0].Name != "a" || deps[1].Name != "b" || deps[2].Name != "c" {
		t.Fatalf("not sorted: %v", deps)
	}
}

func TestSortScripts(t *testing.T) {
	scripts := []Script{
		{Name: "test"}, {Name: "build"}, {Name: "deploy"},
	}
	SortScripts(scripts)
	if scripts[0].Name != "build" {
		t.Fatalf("not sorted: %v", scripts)
	}
}
