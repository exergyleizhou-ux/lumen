package wand

import ("testing")

func TestWandDiagnose(t *testing.T) {
	w := NewWand()
	for _, d := range BuiltinDiagnostics() { w.RegisterDiagnostic(d) }
	issues, err := w.Diagnose()
	if err != nil { t.Fatal(err) }
	if len(issues) != 1 { t.Errorf("expected 1 issue from builtins, got %d", len(issues)) }
}
func TestWandFix(t *testing.T) {
	w := NewWand()
	w.SetAutoApply("medium")
	issues := []Issue{{ID: "fix-001", Severity: "low", FixFn: func() error { return nil }}}
	results := w.Fix(issues)
	if len(results) != 1 || !results[0].Success { t.Error("fix should succeed") }
}
func TestWandFixAll(t *testing.T) {
	w := NewWand()
	w.SetAutoApply("high")
	for _, d := range BuiltinDiagnostics() { w.RegisterDiagnostic(d) }
	issues, results, err := w.FixAll()
	if err != nil { t.Fatal(err) }
	if len(issues) != 1 { t.Error("issues") }
	if len(results) != 0 { t.Error("should not auto-fix medium severity with max high") }
}
func TestFormatIssues(t *testing.T) {
	issues := []Issue{{ID: "i1", Severity: "high", Title: "Test", Suggestion: "Fix it"}}
	s := FormatIssues(issues)
	if s == "" { t.Error("format") }
}
func TestSuggester(t *testing.T) {
	sg := NewSuggester()
	sg.AddPattern("timeout", "Increase the timeout setting in config.yml")
	suggestions := sg.Suggest("connection timeout error")
	if len(suggestions) != 1 { t.Error("suggest") }
}
func TestSummary(t *testing.T) {
	s := Summary([]Issue{{}}, []FixResult{{Success: true}})
	if s == "" { t.Error("summary") }
}
