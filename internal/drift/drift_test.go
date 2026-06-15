package drift

import (
	"strings"
	"testing"
	"time"
)

func TestNewDetector(t *testing.T) {
	desired := State{
		"host": "localhost",
		"port": 8080,
	}
	d := NewDetector(desired)
	if d == nil {
		t.Fatal("NewDetector returned nil")
	}
	got := d.GetDesired()
	if got["host"] != "localhost" {
		t.Errorf("expected host 'localhost', got %v", got["host"])
	}
}

func TestDetect_NoDrift(t *testing.T) {
	desired := State{
		"host": "localhost",
		"port": 8080,
	}
	actual := State{
		"host": "localhost",
		"port": 8080,
	}

	d := NewDetector(desired)
	report := d.Detect(actual)
	if report.Summary.Total != 0 {
		t.Errorf("expected 0 drifts, got %d", report.Summary.Total)
	}
}

func TestDetect_AddedKey(t *testing.T) {
	desired := State{"a": 1}
	actual := State{"a": 1, "b": 2}

	d := NewDetector(desired)
	report := d.Detect(actual)

	if report.Summary.Added != 1 {
		t.Errorf("expected 1 added, got %d", report.Summary.Added)
	}
}

func TestDetect_RemovedKey(t *testing.T) {
	desired := State{"a": 1, "b": 2}
	actual := State{"a": 1}

	d := NewDetector(desired)
	report := d.Detect(actual)

	if report.Summary.Removed != 1 {
		t.Errorf("expected 1 removed, got %d", report.Summary.Removed)
	}
}

func TestDetect_ModifiedKey(t *testing.T) {
	desired := State{"a": "hello"}
	actual := State{"a": "world"}

	d := NewDetector(desired)
	report := d.Detect(actual)

	if report.Summary.Modified != 1 {
		t.Errorf("expected 1 modified, got %d", report.Summary.Modified)
	}
}

func TestDetect_TypeChanged(t *testing.T) {
	desired := State{"a": "string"}
	actual := State{"a": 42}

	d := NewDetector(desired)
	report := d.Detect(actual)

	if report.Summary.TypeChange != 1 {
		t.Errorf("expected 1 type change, got %d", report.Summary.TypeChange)
	}
}

func TestDetect_NestedState(t *testing.T) {
	desired := State{
		"db": map[string]interface{}{
			"host": "localhost",
			"port": 5432,
		},
	}
	actual := State{
		"db": map[string]interface{}{
			"host": "prod-host",
			"port": 5432,
		},
	}

	d := NewDetector(desired)
	report := d.Detect(actual)

	if report.Summary.Modified != 1 {
		t.Errorf("expected 1 modified, got %d", report.Summary.Modified)
	}
}

func TestReconciliationPlan(t *testing.T) {
	desired := State{"key": "desired"}
	actual := State{"key": "actual"}

	d := NewDetector(desired)
	report := d.Detect(actual)
	plan := d.GeneratePlan(report)

	if len(plan.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(plan.Actions))
	}
	if plan.Actions[0].Action != "set" {
		t.Errorf("expected 'set', got '%s'", plan.Actions[0].Action)
	}
}

func TestApplyPlan(t *testing.T) {
	desired := State{"key": "desired"}
	actual := State{"key": "actual"}

	d := NewDetector(desired)
	report := d.Detect(actual)
	plan := d.GeneratePlan(report)

	result := ApplyPlan(actual, plan)
	if result["key"] != "desired" {
		t.Errorf("expected 'desired', got %v", result["key"])
	}
}

func TestApplyPlan_DeleteAdded(t *testing.T) {
	desired := State{}
	actual := State{"extra": "value"}

	d := NewDetector(desired)
	report := d.Detect(actual)
	plan := d.GeneratePlan(report)

	result := ApplyPlan(actual, plan)
	if _, ok := result["extra"]; ok {
		t.Error("'extra' should be deleted")
	}
}

func TestHistory(t *testing.T) {
	desired := State{"a": 1}
	d := NewDetector(desired)

	d.Detect(State{"a": 2})
	d.Detect(State{"a": 3})

	history := d.History()
	if len(history) != 2 {
		t.Errorf("expected 2 history entries, got %d", len(history))
	}
}

func TestHistorySince(t *testing.T) {
	desired := State{"a": 1}
	d := NewDetector(desired)

	d.Detect(State{"a": 2})
	midpoint := time.Now()
	time.Sleep(10 * time.Millisecond)
	d.Detect(State{"a": 3})

	recent := d.HistorySince(midpoint)
	if len(recent) != 1 {
		t.Errorf("expected 1 recent, got %d", len(recent))
	}
}

func TestLastReport(t *testing.T) {
	desired := State{"a": 1}
	d := NewDetector(desired)

	_, ok := d.LastReport()
	if ok {
		t.Error("should not have report yet")
	}

	d.Detect(State{"a": 2})
	report, ok := d.LastReport()
	if !ok {
		t.Error("should have report")
	}
	if report.Summary.Total != 1 {
		t.Errorf("expected 1 drift, got %d", report.Summary.Total)
	}
}

func TestClearHistory(t *testing.T) {
	desired := State{"a": 1}
	d := NewDetector(desired)
	d.Detect(State{"a": 2})
	d.ClearHistory()

	if len(d.History()) != 0 {
		t.Error("history should be empty after clear")
	}
}

func TestComputeDiff(t *testing.T) {
	desired := State{"a": 1, "b": 2}
	actual := State{"a": 1, "b": 3, "c": 4}

	diffs := ComputeDiff(desired, actual)
	if len(diffs) < 1 {
		t.Errorf("expected at least 1 diff, got %d", len(diffs))
	}
}

func TestStateHash(t *testing.T) {
	s1 := State{"a": 1}
	s2 := State{"a": 1}
	s3 := State{"a": 2}

	if StateHash(s1) != StateHash(s2) {
		t.Error("same states should have same hash")
	}
	if StateHash(s1) == StateHash(s3) {
		t.Error("different states should have different hash")
	}
}

func TestHasDrift(t *testing.T) {
	if HasDrift(State{"a": 1}, State{"a": 1}) {
		t.Error("identical states should not have drift")
	}
	if !HasDrift(State{"a": 1}, State{"a": 2}) {
		t.Error("different states should have drift")
	}
}

func TestFormatDriftReport(t *testing.T) {
	desired := State{"key": "desired"}
	actual := State{"key": "actual"}
	d := NewDetector(desired)
	report := d.Detect(actual)

	formatted := FormatDriftReport(report)
	if !strings.Contains(formatted, "Total drifts") {
		t.Error("formatted report should contain summary")
	}
	if !strings.Contains(formatted, "key") {
		t.Error("formatted report should mention key")
	}
}

func TestFormatReconciliationPlan(t *testing.T) {
	desired := State{"key": "desired"}
	actual := State{"key": "actual"}
	d := NewDetector(desired)
	report := d.Detect(actual)
	plan := d.GeneratePlan(report)

	formatted := FormatReconciliationPlan(plan)
	if !strings.Contains(formatted, "actions") {
		t.Error("formatted plan should contain actions count")
	}
}

func TestSnapshotManager(t *testing.T) {
	sm := NewSnapshotManager()
	state := State{"version": 1}

	snap1 := sm.Take(state, "initial")
	snap2 := sm.Take(state, "backup")

	if len(sm.List()) != 2 {
		t.Errorf("expected 2 snapshots, got %d", len(sm.List()))
	}

	got, ok := sm.Get(snap1.ID)
	if !ok || got.Label != "initial" {
		t.Error("should find snapshot by ID")
	}

	latest, ok := sm.Latest()
	if !ok || latest.ID != snap2.ID {
		t.Error("should get latest snapshot")
	}
}

func TestCompareSnapshots(t *testing.T) {
	s1 := Snapshot{State: State{"a": 1}}
	s2 := Snapshot{State: State{"a": 2}}

	entries := CompareSnapshots(s1, s2)
	if len(entries) != 1 {
		t.Errorf("expected 1 drift entry, got %d", len(entries))
	}
}

func TestGetByPath(t *testing.T) {
	state := State{
		"db": map[string]interface{}{
			"host": "localhost",
		},
	}

	v, ok := GetByPath(state, "db.host")
	if !ok || v != "localhost" {
		t.Errorf("expected 'localhost', got %v (ok=%v)", v, ok)
	}

	_, ok = GetByPath(state, "db.missing")
	if ok {
		t.Error("should not find missing key")
	}
}

func TestSetByPath(t *testing.T) {
	state := make(State)
	SetByPath(state, "db.host", "prod-host")

	v, _ := GetByPath(state, "db.host")
	if v != "prod-host" {
		t.Errorf("expected 'prod-host', got %v", v)
	}
}

func TestDeleteByPath(t *testing.T) {
	state := State{
		"db": map[string]interface{}{
			"host": "localhost",
		},
	}

	if !DeleteByPath(state, "db.host") {
		t.Error("delete should succeed")
	}
	if DeleteByPath(state, "db.host") {
		t.Error("second delete should fail")
	}
}

func TestFlatten(t *testing.T) {
	state := State{
		"a": 1,
		"b": map[string]interface{}{
			"c": 2,
		},
	}

	flat := Flatten(state)
	if flat["a"] != 1 {
		t.Errorf("expected a=1, got %v", flat["a"])
	}
	if flat["b.c"] != 2 {
		t.Errorf("expected b.c=2, got %v", flat["b.c"])
	}
}

func TestUnflatten(t *testing.T) {
	flat := map[string]interface{}{
		"a":   1,
		"b.c": 2,
		"b.d": 3,
	}

	state := Unflatten(flat)
	if v, ok := GetByPath(state, "b.c"); !ok || v != 2 {
		t.Errorf("expected b.c=2, got %v", v)
	}
}

func TestMergeStates(t *testing.T) {
	s1 := State{"a": 1, "b": 2}
	s2 := State{"b": 3, "c": 4}

	merged := MergeStates(s1, s2)
	if merged["a"] != 1 {
		t.Error("a should be 1")
	}
	if merged["b"] != 3 {
		t.Error("b should be 3 (overridden)")
	}
	if merged["c"] != 4 {
		t.Error("c should be 4")
	}
}

func TestKeys(t *testing.T) {
	state := State{"z": 1, "a": 2, "m": 3}
	keys := Keys(state)
	if len(keys) != 3 || keys[0] != "a" || keys[2] != "z" {
		t.Errorf("unexpected keys: %v", keys)
	}
}

func TestDeepCopy(t *testing.T) {
	state := State{
		"nested": map[string]interface{}{
			"key": "value",
		},
	}
	cpy := state.DeepCopy()
	cpy["nested"].(map[string]interface{})["key"] = "modified"

	if state["nested"].(map[string]interface{})["key"] != "value" {
		t.Error("deep copy should not affect original")
	}
}

func TestSetDesired(t *testing.T) {
	d := NewDetector(State{"a": 1})
	d.SetDesired(State{"b": 2})

	got := d.GetDesired()
	if _, ok := got["a"]; ok {
		t.Error("old key should be gone")
	}
	if got["b"] != 2 {
		t.Errorf("expected b=2, got %v", got["b"])
	}
}

func TestSeverityClassification(t *testing.T) {
	desired := State{"security.password": "secret"}
	actual := State{"security.password": "changed"}

	d := NewDetector(desired)
	report := d.Detect(actual)

	for _, entry := range report.Entries {
		if strings.Contains(entry.Path, "password") {
			if entry.Severity != "critical" {
				t.Errorf("expected 'critical' for password, got '%s'", entry.Severity)
			}
		}
	}
}

func TestOnReconcile(t *testing.T) {
	desired := State{"key": "desired"}
	d := NewDetector(desired)
	d.OnReconcile(func(entry DriftEntry) string {
		return "custom reconciliation"
	})

	report := d.Detect(State{"key": "actual"})
	plan := d.GeneratePlan(report)

	if plan.Actions[0].Description != "custom reconciliation" {
		t.Errorf("expected custom description, got '%s'", plan.Actions[0].Description)
	}
}
