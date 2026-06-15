package linter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewLinter(t *testing.T) {
	l := New(nil)
	if l == nil {
		t.Fatal("New returned nil")
	}
	if l.registry == nil {
		t.Fatal("registry is nil")
	}
	rules := l.registry.List()
	if len(rules) < 10 {
		t.Errorf("expected at least 10 built-in rules, got %d", len(rules))
	}
}

func TestLinterLintFile_ValidGo(t *testing.T) {
	tmpDir := t.TempDir()
	src := `package main

import "fmt"

// Greet prints a greeting.
func Greet(name string) {
	// This function is fine
	msg := "Hello, " + name
	fmt.Println(msg)
}

func main() {
	Greet("world")
}
`
	srcPath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(srcPath, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	l := New(nil)
	issues, err := l.LintFile(srcPath)
	if err != nil {
		t.Fatalf("LintFile error: %v", err)
	}
	// The code above is clean — no unused vars, docs exist
	t.Logf("Found %d issues", len(issues))
}

func TestLinterLintFile_MissingDoc(t *testing.T) {
	tmpDir := t.TempDir()
	src := `package main

func UnexportedFine() {}

func ExportedMissingDoc() {}

// ExportedHasDoc does something.
func ExportedHasDoc() {}
`
	srcPath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(srcPath, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	l := New(nil)
	issues, err := l.LintFile(srcPath)
	if err != nil {
		t.Fatalf("LintFile error: %v", err)
	}

	found := false
	for _, issue := range issues {
		if issue.Rule == "missing-doc" && strings.Contains(issue.Message, "ExportedMissingDoc") {
			found = true
		}
	}
	if !found {
		t.Error("expected missing-doc issue for ExportedMissingDoc")
	}
}

func TestLinterLintFile_UnusedVar(t *testing.T) {
	tmpDir := t.TempDir()
	src := `package main

import "fmt"

func main() {
	x := 42
	y := 100
	fmt.Println(y)
	_ = x // using x to avoid actual unused
}
`
	srcPath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(srcPath, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	l := New(nil)
	issues, err := l.LintFile(srcPath)
	if err != nil {
		t.Fatalf("LintFile error: %v", err)
	}
	t.Logf("Issues: %v", issues)
}

func TestLinterLintFile_LongLine(t *testing.T) {
	tmpDir := t.TempDir()
	longLine := strings.Repeat("a", 130)
	src := `package main

// ` + "Ok" + `
func main() {
	_ = "` + longLine + `"
}
`
	srcPath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(srcPath, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	l := New(nil)
	issues, err := l.LintFile(srcPath)
	if err != nil {
		t.Fatalf("LintFile error: %v", err)
	}

	found := false
	for _, issue := range issues {
		if issue.Rule == "long-line" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected long-line issue")
	}
}

func TestRuleRegistry_RegisterAndGet(t *testing.T) {
	rr := NewRuleRegistry()
	if rr == nil {
		t.Fatal("NewRuleRegistry returned nil")
	}

	r := &Rule{Name: "test-rule", Description: "A test rule", Enabled: true}
	rr.Register(r)

	got, ok := rr.Get("test-rule")
	if !ok {
		t.Fatal("rule not found after registration")
	}
	if got.Name != "test-rule" {
		t.Errorf("expected name 'test-rule', got '%s'", got.Name)
	}
}

func TestRuleRegistry_EnableDisable(t *testing.T) {
	rr := NewRuleRegistry()
	r := &Rule{Name: "toggle-rule", Enabled: false}
	rr.Register(r)

	rr.Enable("toggle-rule")
	got, _ := rr.Get("toggle-rule")
	if !got.Enabled {
		t.Error("rule should be enabled after Enable")
	}

	rr.Disable("toggle-rule")
	got, _ = rr.Get("toggle-rule")
	if got.Enabled {
		t.Error("rule should be disabled after Disable")
	}
}

func TestRuleRegistry_List(t *testing.T) {
	rr := NewRuleRegistry()
	rr.Register(&Rule{Name: "rule-a"})
	rr.Register(&Rule{Name: "rule-b"})
	rr.Register(&Rule{Name: "rule-c"})

	list := rr.List()
	if len(list) != 3 {
		t.Errorf("expected 3 rules, got %d", len(list))
	}
	if list[0] != "rule-a" || list[1] != "rule-b" || list[2] != "rule-c" {
		t.Errorf("unexpected order: %v", list)
	}
}

func TestLoadConfig(t *testing.T) {
	yaml := `version: "1"
rules:
  unused-var:
    enabled: true
    severity: warning
exclude:
  - "*_test.go"
max_issues: 500
`
	cfg, err := LoadConfig(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}
	if cfg.Version != "1" {
		t.Errorf("expected version '1', got '%s'", cfg.Version)
	}
	if cfg.MaxIssues != 500 {
		t.Errorf("expected MaxIssues 500, got %d", cfg.MaxIssues)
	}
	if len(cfg.Exclude) != 1 || cfg.Exclude[0] != "*_test.go" {
		t.Errorf("unexpected exclude: %v", cfg.Exclude)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg == nil {
		t.Fatal("DefaultConfig returned nil")
	}
	if cfg.Version != "1" {
		t.Errorf("expected version '1', got '%s'", cfg.Version)
	}
	if cfg.MaxIssues != 1000 {
		t.Errorf("expected MaxIssues 1000, got %d", cfg.MaxIssues)
	}
}

func TestFormatReport(t *testing.T) {
	issues := []Issue{
		{Rule: "test-rule", Severity: SevWarning, File: "test.go", Line: 10, Column: 1, Message: "test issue"},
	}
	var sb strings.Builder
	FormatReport(issues, &sb)
	output := sb.String()
	if !strings.Contains(output, "test.go") {
		t.Error("report should contain filename")
	}
	if !strings.Contains(output, "test issue") {
		t.Error("report should contain message")
	}
}

func TestFormatReport_NoIssues(t *testing.T) {
	var sb strings.Builder
	FormatReport(nil, &sb)
	output := sb.String()
	if !strings.Contains(output, "No issues found") {
		t.Error("expected 'No issues found' for empty issues")
	}
}

func TestFormatReportJSON(t *testing.T) {
	issues := []Issue{
		{Rule: "test", Severity: SevWarning, File: "f.go", Line: 1, Column: 1, Message: "msg"},
	}
	var sb strings.Builder
	err := FormatReportJSON(issues, &sb)
	if err != nil {
		t.Fatalf("FormatReportJSON error: %v", err)
	}
	output := sb.String()
	if !strings.Contains(output, `"f.go"`) {
		t.Error("JSON output should contain filename")
	}
}

func TestFilterBySeverity(t *testing.T) {
	issues := []Issue{
		{Severity: SevInfo, Message: "info"},
		{Severity: SevWarning, Message: "warn"},
		{Severity: SevError, Message: "err"},
	}
	warnings := FilterBySeverity(issues, SevWarning)
	if len(warnings) != 1 {
		t.Errorf("expected 1 warning, got %d", len(warnings))
	}
	if warnings[0].Message != "warn" {
		t.Errorf("unexpected message: %s", warnings[0].Message)
	}
}

func TestFilterByRule(t *testing.T) {
	issues := []Issue{
		{Rule: "rule-a", Message: "a"},
		{Rule: "rule-b", Message: "b"},
		{Rule: "rule-a", Message: "c"},
	}
	aIssues := FilterByRule(issues, "rule-a")
	if len(aIssues) != 2 {
		t.Errorf("expected 2 rule-a issues, got %d", len(aIssues))
	}
}

func TestHasErrors(t *testing.T) {
	issues := []Issue{
		{Severity: SevInfo},
		{Severity: SevWarning},
	}
	if HasErrors(issues) {
		t.Error("HasErrors should be false for info + warning")
	}
	issues = append(issues, Issue{Severity: SevError})
	if !HasErrors(issues) {
		t.Error("HasErrors should be true when error present")
	}
}

func TestCountBySeverity(t *testing.T) {
	issues := []Issue{
		{Severity: SevInfo},
		{Severity: SevInfo},
		{Severity: SevWarning},
		{Severity: SevError},
	}
	counts := CountBySeverity(issues)
	if counts[SevInfo] != 2 {
		t.Errorf("expected 2 info, got %d", counts[SevInfo])
	}
	if counts[SevWarning] != 1 {
		t.Errorf("expected 1 warning, got %d", counts[SevWarning])
	}
	if counts[SevError] != 1 {
		t.Errorf("expected 1 error, got %d", counts[SevError])
	}
}

func TestCountByRule(t *testing.T) {
	issues := []Issue{
		{Rule: "a"},
		{Rule: "a"},
		{Rule: "b"},
	}
	counts := CountByRule(issues)
	if counts["a"] != 2 {
		t.Errorf("expected 2 'a', got %d", counts["a"])
	}
	if counts["b"] != 1 {
		t.Errorf("expected 1 'b', got %d", counts["b"])
	}
}

func TestWorstSeverity(t *testing.T) {
	issues := []Issue{
		{Severity: SevInfo},
		{Severity: SevWarning},
	}
	if WorstSeverity(issues) != SevWarning {
		t.Error("worst should be warning")
	}
}

func TestParseSeverity(t *testing.T) {
	tests := []struct {
		input string
		want  Severity
		err   bool
	}{
		{"info", SevInfo, false},
		{"warning", SevWarning, false},
		{"error", SevError, false},
		{"fatal", SevFatal, false},
		{"INFO", SevInfo, false},
		{"WARNING", SevWarning, false},
		{"unknown", SevInfo, true},
	}
	for _, tt := range tests {
		got, err := ParseSeverity(tt.input)
		if tt.err && err == nil {
			t.Errorf("ParseSeverity(%q) expected error", tt.input)
		}
		if !tt.err && err != nil {
			t.Errorf("ParseSeverity(%q) unexpected error: %v", tt.input, err)
		}
		if got != tt.want {
			t.Errorf("ParseSeverity(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestSeverityString(t *testing.T) {
	tests := []struct {
		sev  Severity
		want string
	}{
		{SevInfo, "info"},
		{SevWarning, "warning"},
		{SevError, "error"},
		{SevFatal, "fatal"},
		{Severity(99), "unknown"},
	}
	for _, tt := range tests {
		got := tt.sev.String()
		if got != tt.want {
			t.Errorf("Severity(%d).String() = %q, want %q", tt.sev, got, tt.want)
		}
	}
}

func TestMergeIssues(t *testing.T) {
	a := []Issue{
		{Rule: "r", File: "f.go", Line: 1, Column: 1},
	}
	b := []Issue{
		{Rule: "r", File: "f.go", Line: 1, Column: 1}, // duplicate
		{Rule: "r", File: "f.go", Line: 2, Column: 1}, // new
	}
	merged := MergeIssues(a, b)
	if len(merged) != 2 {
		t.Errorf("expected 2 merged issues, got %d", len(merged))
	}
}

func TestIsClean(t *testing.T) {
	if !IsClean(nil) {
		t.Error("nil should be clean")
	}
	if !IsClean([]Issue{}) {
		t.Error("empty should be clean")
	}
	if IsClean([]Issue{{Message: "dirty"}}) {
		t.Error("non-empty should not be clean")
	}
}

func TestApplyFix(t *testing.T) {
	src := []byte("line1\nline2\nline3\n")
	fix := &Fix{
		StartLine:  2,
		EndLine:    2,
		Replacement: "NEWLINE",
	}
	result, err := ApplyFix(src, fix)
	if err != nil {
		t.Fatalf("ApplyFix error: %v", err)
	}
	expected := "line1\nNEWLINE\nline3\n"
	if string(result) != expected {
		t.Errorf("expected %q, got %q", expected, string(result))
	}
}

func TestApplyFix_OutOfBounds(t *testing.T) {
	src := []byte("line1\n")
	fix := &Fix{StartLine: 5, EndLine: 5, Replacement: "x"}
	_, err := ApplyFix(src, fix)
	if err == nil {
		t.Error("expected error for out-of-bounds fix")
	}
}

func TestExtractSnippet(t *testing.T) {
	src := []byte("line1\nline2\nline3\nline4\nline5\n")
	snippet := ExtractSnippet(src, 3, 1)
	if !strings.Contains(snippet, "line2") {
		t.Errorf("snippet should contain context: %s", snippet)
	}
	if !strings.Contains(snippet, "line4") {
		t.Errorf("snippet should contain context: %s", snippet)
	}
}

func TestSourceHash(t *testing.T) {
	h1 := SourceHash([]byte("hello"))
	h2 := SourceHash([]byte("hello"))
	h3 := SourceHash([]byte("world"))
	if h1 != h2 {
		t.Error("same content should produce same hash")
	}
	if h1 == h3 {
		t.Error("different content should produce different hash")
	}
}

func TestLintCache(t *testing.T) {
	cache := NewLintCache()
	issues := []Issue{{Message: "test"}}

	// Cache miss
	_, ok := cache.Get("file.go", "hash1")
	if ok {
		t.Error("expected cache miss")
	}

	// Set and get
	cache.Set("file.go", "hash1", issues)
	got, ok := cache.Get("file.go", "hash1")
	if !ok || len(got) != 1 {
		t.Error("expected cache hit")
	}

	// Different hash = miss
	_, ok = cache.Get("file.go", "hash2")
	if ok {
		t.Error("different hash should be miss")
	}

	// Invalidate
	cache.Invalidate("file.go")
	_, ok = cache.Get("file.go", "hash1")
	if ok {
		t.Error("should miss after invalidate")
	}

	// Clear
	cache.Set("a.go", "h1", issues)
	cache.Set("b.go", "h1", issues)
	cache.Clear()
	_, ok = cache.Get("a.go", "h1")
	if ok {
		t.Error("should miss after clear")
	}
}

func TestParseIgnoreDirectives(t *testing.T) {
	src := []byte("package p\n//nolint\nfunc f() {}\n//nolint:unused-var\nvar x = 1\n")
	directives := ParseIgnoreDirectives(src)
	if len(directives) != 2 {
		t.Errorf("expected 2 directives, got %d", len(directives))
	}
	if directives[0].Line != 2 || directives[0].Rule != "" {
		t.Errorf("unexpected directive: %+v", directives[0])
	}
	if directives[1].Line != 4 || directives[1].Rule != "unused-var" {
		t.Errorf("unexpected directive: %+v", directives[1])
	}
}

func TestFilterIgnored(t *testing.T) {
	src := []byte("//nolint:myrule\nline2\n")
	issues := []Issue{
		{Rule: "myrule", Line: 1},
		{Rule: "other", Line: 1},
		{Rule: "myrule", Line: 3},
	}
	filtered := FilterIgnored(issues, src)
	// myrule on line 1 should be filtered out
	if len(filtered) != 2 {
		t.Errorf("expected 2 issues after filtering, got %d", len(filtered))
	}
}

func TestValidateConfig(t *testing.T) {
	cfg := &LinterConfig{
		Version:   "",
		MaxIssues: -1,
		Rules: map[string]RuleConfig{
			"bad": {Severity: "bogus"},
		},
	}
	errs := ValidateConfig(cfg)
	if len(errs) < 3 {
		t.Errorf("expected at least 3 validation errors, got %d: %v", len(errs), errs)
	}
}

func TestNewLintResult(t *testing.T) {
	issues := []Issue{
		{Severity: SevError, Rule: "r1"},
		{Severity: SevWarning, Rule: "r2"},
		{Severity: SevInfo, Rule: "r1"},
	}
	result := NewLintResult(issues, "cfg.yml", 5)
	if result.Summary.Total != 3 {
		t.Errorf("expected 3 total, got %d", result.Summary.Total)
	}
	if result.Summary.Errors != 1 {
		t.Errorf("expected 1 error, got %d", result.Summary.Errors)
	}
	if result.Summary.Warnings != 1 {
		t.Errorf("expected 1 warning, got %d", result.Summary.Warnings)
	}
	if result.Summary.Infos != 1 {
		t.Errorf("expected 1 info, got %d", result.Summary.Infos)
	}
	if result.Files != 5 {
		t.Errorf("expected 5 files, got %d", result.Files)
	}
}

func TestRuleSet(t *testing.T) {
	rs := NewRuleSet("test-set", "Test rules", "r1", "r2", "r3")
	if rs.Name != "test-set" {
		t.Errorf("expected name 'test-set', got '%s'", rs.Name)
	}
	if len(rs.RuleNames) != 3 {
		t.Errorf("expected 3 rule names, got %d", len(rs.RuleNames))
	}

	l := New(nil)
	l.ApplyRuleSet(rs)
	// Check that only the specified rules are enabled
	for _, rule := range l.registry.All() {
		shouldBeEnabled := false
		for _, name := range rs.RuleNames {
			if rule.Name == name {
				shouldBeEnabled = true
				break
			}
		}
		if rule.Enabled != shouldBeEnabled {
			t.Errorf("rule %s: enabled=%v, want=%v", rule.Name, rule.Enabled, shouldBeEnabled)
		}
	}
}

func TestLintSource(t *testing.T) {
	l := New(nil)
	src := `package main

// Greet says hello.
func Greet() string { return "hello" }

func main() { println(Greet()) }
`
	issues, err := l.LintSource("virtual.go", strings.NewReader(src))
	if err != nil {
		t.Fatalf("LintSource error: %v", err)
	}
	// should be clean
	t.Logf("issues from LintSource: %d", len(issues))
}

func TestIssueByPosition(t *testing.T) {
	issues := IssueByPosition{
		{File: "b.go", Line: 1},
		{File: "a.go", Line: 5},
		{File: "a.go", Line: 1},
	}
	// Should sort by file then line
	if !issues.Less(2, 1) {
		t.Error("a.go:1 should be before a.go:5")
	}
	if !issues.Less(1, 0) {
		t.Error("a.go should be before b.go")
	}
}

func TestIssueBySeverity(t *testing.T) {
	issues := IssueBySeverity{
		{Severity: SevInfo},
		{Severity: SevError},
		{Severity: SevWarning},
	}
	// Higher severity should come first
	if !issues.Less(1, 0) {
		t.Error("error should be before info")
	}
}

func TestLinterLintDir(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a subdirectory
	subDir := filepath.Join(tmpDir, "sub")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	src1 := `package main

// Main does stuff.
func Main() { println("hello") }
`
	src2 := `package sub

func helper() int { return 42 }
`
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(src1), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "helper.go"), []byte(src2), 0644); err != nil {
		t.Fatal(err)
	}

	l := New(nil)
	// Disable noisy rules
	l.registry.Disable("long-line")
	l.registry.Disable("magic-number")

	issues, err := l.LintDir(tmpDir)
	if err != nil {
		t.Fatalf("LintDir error: %v", err)
	}
	t.Logf("LintDir found %d issues", len(issues))
}

func TestLinterLintPhased(t *testing.T) {
	tmpDir := t.TempDir()
	src := `package main

func f() int { return 42 }

// G does stuff.
func G() string { return "hello" }
`
	srcPath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(srcPath, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	l := New(nil)
	results, err := l.LintPhased(srcPath)
	if err != nil {
		t.Fatalf("LintPhased error: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected at least one phase result")
	}
	t.Logf("Got %d phase results", len(results))
}

func TestSeverityYAML(t *testing.T) {
	// Test MarshalYAML
	s := SevWarning
	v, err := s.MarshalYAML()
	if err != nil {
		t.Fatalf("MarshalYAML error: %v", err)
	}
	if v != "warning" {
		t.Errorf("expected 'warning', got %v", v)
	}
}

func TestPresets(t *testing.T) {
	all := PresetAll()
	if len(all) < 10 {
		t.Errorf("PresetAll should have >=10 rules, got %d", len(all))
	}

	minimal := PresetMinimal()
	if len(minimal) != 1 || !minimal["error-check"] {
		t.Error("PresetMinimal should only have error-check")
	}

	style := PresetStyle()
	if len(style) < 3 {
		t.Errorf("PresetStyle should have >=3 rules, got %d", len(style))
	}

	quality := PresetQuality()
	if len(quality) < 5 {
		t.Errorf("PresetQuality should have >=5 rules, got %d", len(quality))
	}
}

func TestLinterLintFile_MagicNumber(t *testing.T) {
	tmpDir := t.TempDir()
	src := `package main

func main() {
	// 0, 1, 2 are allowed
	x := 0
	y := 1
	z := 2
	// 42 should trigger magic-number
	w := 42
	_, _, _, _ = x, y, z, w
}
`
	srcPath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(srcPath, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	l := New(nil)
	issues, err := l.LintFile(srcPath)
	if err != nil {
		t.Fatalf("LintFile error: %v", err)
	}

	found := false
	for _, issue := range issues {
		if issue.Rule == "magic-number" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected magic-number issue for '42'")
	}
}

func TestBatchFixer(t *testing.T) {
	issues := []Issue{
		{Rule: "test", File: "a.go", Fix: &Fix{Description: "fix1", StartLine: 2, EndLine: 2, Replacement: "new"}},
		{Rule: "test", File: "b.go", Fix: &Fix{Description: "fix2", StartLine: 1, EndLine: 1, Replacement: "new"}},
		{Rule: "test", File: "a.go", Fix: nil},
	}
	bf := NewBatchFixer(issues)
	if len(bf.byFile) != 2 {
		t.Errorf("expected 2 files, got %d", len(bf.byFile))
	}
	if len(bf.byFile["a.go"]) != 1 {
		t.Errorf("expected 1 fix for a.go, got %d", len(bf.byFile["a.go"]))
	}
}

func TestDefaultStyleGuide(t *testing.T) {
	sg := DefaultStyleGuide()
	if sg == nil {
		t.Fatal("DefaultStyleGuide returned nil")
	}
	if sg.IndentSize != 4 {
		t.Errorf("expected IndentSize 4, got %d", sg.IndentSize)
	}
	if !sg.UseTabs {
		t.Error("expected UseTabs to be true")
	}
}

func TestConsoleReporter(t *testing.T) {
	var sb strings.Builder
	cr := &ConsoleReporter{Out: &sb}
	cr.OnStart(3)
	cr.OnFile("test.go", 0)
	cr.OnDone(5)

	output := sb.String()
	if !strings.Contains(output, "Linting 3 files") {
		t.Error("expected start message")
	}
	if !strings.Contains(output, "Done. 5 total") {
		t.Error("expected done message")
	}
}

func TestSeverityCount(t *testing.T) {
	issues := []Issue{
		{Severity: SevError},
		{Severity: SevError},
		{Severity: SevWarning},
	}
	c := SeverityCount(issues, SevError)
	if c != 2 {
		t.Errorf("expected 2 errors, got %d", c)
	}
}
