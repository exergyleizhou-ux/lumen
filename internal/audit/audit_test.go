package audit
import ("testing";"time")
func TestTrailRecord(t *testing.T) {
	tr := NewTrail(100)
	e := tr.Record("agent-1", "tool.call", "grep", "success", nil)
	if e.Action != "tool.call" { t.Error("record") }
}
func TestTrailQuery(t *testing.T) {
	tr := NewTrail(100)
	tr.Record("a", "login", "/", "success", nil)
	tr.Record("b", "logout", "/", "success", nil)
	results := tr.Query("a", "", "", time.Time{}, time.Time{})
	if len(results) != 1 { t.Error("query by actor") }
}
func TestTrailVerification(t *testing.T) {
	tr := NewTrail(100)
	tr.Record("x", "do", "r", "ok", nil)
	tr.Record("y", "do", "r", "ok", nil)
	ok, issues := tr.Verify()
	if !ok { t.Errorf("chain broken: %v", issues) }
}
func TestComplianceReport(t *testing.T) {
	tr := NewTrail(100)
	tr.Record("user", "login", "/login", "success", nil)
	tr.Record("user", "upload", "/files", "success", nil)
	tr.Record("user", "delete", "/files", "failure", map[string]any{"reason": "permission"})
	report := GenerateComplianceReport(tr, "day")
	if report.TotalEvents != 3 { t.Error("total events") }
	if report.Failures != 1 { t.Error("failures") }
}
