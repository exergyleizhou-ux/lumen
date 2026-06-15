package drift

import (
	"strings"
	"testing"
	"time"
)

func TestReconcileEngine_DetectAndPlan(t *testing.T) {
	d := NewDetector(State{"key": "desired"})
	re := NewReconcileEngine(d)
	re.SetRequireApproval(false)

	plan, err := re.DetectAndPlan(State{"key": "actual"})
	if err != nil {
		t.Fatalf("DetectAndPlan error: %v", err)
	}
	if plan == nil {
		t.Error("expected a plan when drift exists")
	}
}

func TestReconcileEngine_NoDrift(t *testing.T) {
	d := NewDetector(State{"key": "value"})
	re := NewReconcileEngine(d)

	plan, err := re.DetectAndPlan(State{"key": "value"})
	if err != nil {
		t.Fatalf("DetectAndPlan error: %v", err)
	}
	if plan != nil {
		t.Error("expected nil plan when no drift")
	}
}

func TestReconcileEngine_ApproveReject(t *testing.T) {
	d := NewDetector(State{"key": "desired"})
	re := NewReconcileEngine(d)
	re.SetRequireApproval(true)

	plan, _ := re.DetectAndPlan(State{"key": "actual"})
	if len(re.PendingPlans()) != 1 {
		t.Error("expected 1 pending plan")
	}

	if !re.ApprovePlan(plan.ID) {
		t.Error("approve should succeed")
	}
	if len(re.PendingPlans()) != 0 {
		t.Error("no pending plans after approval")
	}
	if len(re.AppliedPlans()) != 1 {
		t.Error("1 applied plan after approval")
	}
}

func TestReconcileEngine_Reject(t *testing.T) {
	d := NewDetector(State{"key": "desired"})
	re := NewReconcileEngine(d)

	plan, _ := re.DetectAndPlan(State{"key": "actual"})
	if !re.RejectPlan(plan.ID) {
		t.Error("reject should succeed")
	}
	if len(re.PendingPlans()) != 0 {
		t.Error("no pending plans after reject")
	}
	if len(re.AppliedPlans()) != 0 {
		t.Error("0 applied plans after reject")
	}
}

func TestReconcileEngine_ApplyAndReconcile(t *testing.T) {
	d := NewDetector(State{"key": "desired"})
	re := NewReconcileEngine(d)
	re.SetRequireApproval(false)

	state := State{"key": "actual"}
	plan, err := re.ApplyAndReconcile(state, &state)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if plan == nil {
		t.Fatal("expected a plan")
	}
	if state["key"] != "desired" {
		t.Errorf("expected 'desired', got %v", state["key"])
	}
}

func TestConfigVersionManager_SaveAndGet(t *testing.T) {
	cvm := NewConfigVersionManager()
	state := State{"version": 1}
	v := cvm.Save(state, "author", "initial commit")

	if v.Version != 1 {
		t.Errorf("expected version 1, got %d", v.Version)
	}

	got, ok := cvm.Get(1)
	if !ok || got.Hash != v.Hash {
		t.Error("should get saved version")
	}
}

func TestConfigVersionManager_Rollback(t *testing.T) {
	cvm := NewConfigVersionManager()
	cvm.Save(State{"key": "v1"}, "a", "first")
	cvm.Save(State{"key": "v2"}, "a", "second")

	v, err := cvm.Rollback(1)
	if err != nil {
		t.Fatalf("rollback error: %v", err)
	}
	if v.State["key"] != "v1" {
		t.Errorf("expected v1, got %v", v.State["key"])
	}
}

func TestConfigVersionManager_Diff(t *testing.T) {
	cvm := NewConfigVersionManager()
	cvm.Save(State{"a": 1}, "a", "v1")
	cvm.Save(State{"a": 2}, "a", "v2")

	entries, err := cvm.Diff(1, 2)
	if err != nil {
		t.Fatalf("diff error: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 diff entry, got %d", len(entries))
	}
}

func TestConfigVersionManager_InvalidVersion(t *testing.T) {
	cvm := NewConfigVersionManager()
	_, ok := cvm.Get(99)
	if ok {
		t.Error("should not get nonexistent version")
	}
	_, err := cvm.Rollback(99)
	if err == nil {
		t.Error("should error on rollback to nonexistent version")
	}
}

func TestConfigVersionManager_Latest(t *testing.T) {
	cvm := NewConfigVersionManager()
	_, ok := cvm.Latest()
	if ok {
		t.Error("no latest for empty manager")
	}

	cvm.Save(State{"a": 1}, "a", "msg")
	v, ok := cvm.Latest()
	if !ok || v.Version != 1 {
		t.Error("should get latest version 1")
	}
}

func TestDetailedDiff(t *testing.T) {
	desired := State{"a": 1, "b": 2}
	actual := State{"a": 1, "c": 3}

	details := DetailedDiff(desired, actual)
	if len(details) != 2 {
		t.Errorf("expected 2 diff details, got %d: %v", len(details), details)
	}
}

func TestDiffSummaryString(t *testing.T) {
	details := []DiffDetail{
		{Path: "a", ChangeType: "add"},
		{Path: "b", ChangeType: "remove"},
		{Path: "c", ChangeType: "change"},
	}
	s := DiffSummaryString(details)
	if !strings.Contains(s, "+1") || !strings.Contains(s, "-1") || !strings.Contains(s, "~1") {
		t.Errorf("unexpected summary: %s", s)
	}
}

func TestApplyConfigPatch(t *testing.T) {
	state := State{"a": 1}
	patch := &ConfigPatch{
		Operations: []PatchOperation{
			{Op: "replace", Path: "a", Value: 2},
			{Op: "add", Path: "b", Value: 3},
		},
	}
	result := ApplyConfigPatch(state, patch)
	if result["a"] != 2 || result["b"] != 3 {
		t.Errorf("patch not applied correctly: %v", result)
	}
}

func TestCreateConfigPatch(t *testing.T) {
	details := []DiffDetail{
		{Path: "x", ChangeType: "add", NewValue: "42"},
		{Path: "y", ChangeType: "remove"},
		{Path: "z", ChangeType: "change", NewValue: "99"},
	}
	patch := CreateConfigPatch(details)
	if len(patch.Operations) != 3 {
		t.Errorf("expected 3 operations, got %d", len(patch.Operations))
	}
}

func TestDriftTrendAnalyzer(t *testing.T) {
	dta := NewDriftTrendAnalyzer()

	r1 := &DriftReport{Timestamp: driftTime(1), Summary: DriftSummary{Total: 5, Critical: 1}}
	r2 := &DriftReport{Timestamp: driftTime(2), Summary: DriftSummary{Total: 10, Critical: 3}}

	dta.Record(r1)
	dta.Record(r2)

	if len(dta.Points()) != 2 {
		t.Errorf("expected 2 points, got %d", len(dta.Points()))
	}

	if !dta.IsIncreasing() {
		t.Error("trend should be increasing")
	}

	max, ok := dta.MaxDrift()
	if !ok || max.Total != 10 {
		t.Errorf("expected max total 10, got %d", max.Total)
	}
}

func TestDriftTrendAnalyzer_Empty(t *testing.T) {
	dta := NewDriftTrendAnalyzer()
	if dta.IsIncreasing() {
		t.Error("empty analyzer should not be increasing")
	}
	_, ok := dta.MaxDrift()
	if ok {
		t.Error("empty analyzer should not have max")
	}
}

func driftTime(n int) time.Time {
	return time.Date(2024, 1, n, 0, 0, 0, 0, time.UTC)
}

func TestValidateState(t *testing.T) {
	state := State{
		"good":    "value",
		" bad":    "value",
		"":        "empty key",
		"nil_val": nil,
	}
	issues := ValidateState(state)
	if len(issues) < 2 {
		t.Errorf("expected at least 2 issues, got %d: %v", len(issues), issues)
	}
}

func TestBatchDetect(t *testing.T) {
	pairs := []struct{ Desired, Actual State }{
		{State{"a": 1}, State{"a": 2}},
		{State{"b": 1}, State{"b": 1}},
	}
	reports := BatchDetect(pairs)
	if len(reports) != 2 {
		t.Fatalf("expected 2 reports, got %d", len(reports))
	}
	if reports[0].Summary.Total != 1 {
		t.Errorf("first report should have 1 drift")
	}
	if reports[1].Summary.Total != 0 {
		t.Errorf("second report should have 0 drift")
	}
}

func TestMergeReports(t *testing.T) {
	r1 := &DriftReport{Entries: []DriftEntry{{Key: "a"}}, Summary: DriftSummary{Total: 1, Modified: 1}}
	r2 := &DriftReport{Entries: []DriftEntry{{Key: "b"}}, Summary: DriftSummary{Total: 1, Added: 1}}

	merged := MergeReports([]*DriftReport{r1, r2})
	if merged.Summary.Total != 2 {
		t.Errorf("expected total 2, got %d", merged.Summary.Total)
	}
	if len(merged.Entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(merged.Entries))
	}
}
