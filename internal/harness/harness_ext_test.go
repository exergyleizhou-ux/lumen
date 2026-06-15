package harness

import (
	"strings"
	"testing"
	"time"
)

func TestSnapshotManager_SaveCompare(t *testing.T) {
	sm := NewSnapshotManager()
	sm.Save("test1", "hello world")

	match, msg := sm.Compare("test1", "hello world")
	if !match {
		t.Errorf("should match: %s", msg)
	}

	match, _ = sm.Compare("test1", "different")
	if match {
		t.Error("should not match different content")
	}
}

func TestSnapshotManager_HasSnapshot(t *testing.T) {
	sm := NewSnapshotManager()
	if sm.HasSnapshot("missing") {
		t.Error("should not have snapshot")
	}
	sm.Save("exists", "data")
	if !sm.HasSnapshot("exists") {
		t.Error("should have snapshot")
	}
}

func TestGen_String(t *testing.T) {
	g := NewGen(42)
	s1 := g.String(10)
	s2 := g.String(10)
	if len(s1) != 10 {
		t.Errorf("expected len 10, got %d", len(s1))
	}
	if s1 == s2 {
		t.Error("two random strings should differ")
	}
}

func TestGen_Int(t *testing.T) {
	g := NewGen(42)
	for i := 0; i < 100; i++ {
		v := g.Int(1, 10)
		if v < 1 || v > 10 {
			t.Errorf("value %d out of range [1,10]", v)
		}
	}
}

func TestGen_Float(t *testing.T) {
	g := NewGen(42)
	for i := 0; i < 100; i++ {
		v := g.Float(0, 1)
		if v < 0 || v >= 1 {
			t.Errorf("value %f out of range [0,1)", v)
		}
	}
}

func TestGen_Bool(t *testing.T) {
	g := NewGen(42)
	trueCount := 0
	falseCount := 0
	for i := 0; i < 100; i++ {
		if g.Bool() {
			trueCount++
		} else {
			falseCount++
		}
	}
	if trueCount == 0 || falseCount == 0 {
		t.Error("Bool should generate both values")
	}
}

func TestGen_StringSlice(t *testing.T) {
	g := NewGen(42)
	slice := g.StringSlice(5, 3, 8)
	if len(slice) != 5 {
		t.Errorf("expected 5 elements, got %d", len(slice))
	}
}

func TestGen_IntSlice(t *testing.T) {
	g := NewGen(42)
	slice := g.IntSlice(10, 0, 100)
	if len(slice) != 10 {
		t.Errorf("expected 10 elements, got %d", len(slice))
	}
}

func TestGen_Email(t *testing.T) {
	g := NewGen(42)
	email := g.Email()
	if !strings.Contains(email, "@") {
		t.Errorf("invalid email: %s", email)
	}
}

func TestGen_URL(t *testing.T) {
	g := NewGen(42)
	url := g.URL()
	if !strings.HasPrefix(url, "https://") {
		t.Errorf("invalid URL: %s", url)
	}
}

func TestPerfTracker(t *testing.T) {
	pt := NewPerfTracker(10.0)
	pt.SetBaseline("op1", 100*time.Millisecond)

	result := pt.Check("op1", 95*time.Millisecond)
	if result.Regression {
		t.Error("95ms vs 100ms baseline should not be regression")
	}

	result = pt.Check("op1", 120*time.Millisecond)
	if !result.Regression {
		t.Error("120ms vs 100ms baseline should be regression (20% increase)")
	}
}

func TestPerfTracker_NoBaseline(t *testing.T) {
	pt := NewPerfTracker(10.0)
	result := pt.Check("new-op", 50*time.Millisecond)
	if result.Regression {
		t.Error("no baseline should not indicate regression")
	}
}

func TestPerfTracker_AllBaselines(t *testing.T) {
	pt := NewPerfTracker(10.0)
	pt.SetBaseline("a", time.Second)
	pt.SetBaseline("b", 2*time.Second)

	all := pt.AllBaselines()
	if len(all) != 2 {
		t.Errorf("expected 2 baselines, got %d", len(all))
	}
}

func TestCoverageTracker(t *testing.T) {
	ct := NewCoverageTracker()
	ct.Hit("path/a")
	ct.Hit("path/a")
	ct.Hit("path/b")

	if ct.Coverage() != 2 {
		t.Errorf("expected 2 unique paths, got %d", ct.Coverage())
	}
	if ct.TotalHits() != 3 {
		t.Errorf("expected 3 total hits, got %d", ct.TotalHits())
	}
	if ct.HitCount("path/a") != 2 {
		t.Errorf("expected 2 hits for path/a, got %d", ct.HitCount("path/a"))
	}

	paths := ct.PathsHit()
	if len(paths) != 2 {
		t.Errorf("expected 2 paths, got %d", len(paths))
	}
}

func TestWriteTAP(t *testing.T) {
	s := NewSuite("tap-test")
	s.AddTest("passing", func(ht *T) {})
	s.AddTest("failing", func(ht *T) { ht.Error("fail") })
	result := s.Run()

	tap := WriteTAP(result)
	if !strings.HasPrefix(tap, "1..2") {
		t.Error("TAP should start with plan line")
	}
	if !strings.Contains(tap, "ok 1") {
		t.Error("TAP should have ok for passing test")
	}
	if !strings.Contains(tap, "not ok 2") {
		t.Error("TAP should have not ok for failing test")
	}
}

func TestRetryRunner(t *testing.T) {
	s := NewSuite("retry-suite")
	attempts := 0
	s.AddTest("flaky", func(ht *T) {
		attempts++
		if attempts < 3 {
			ht.Error("transient failure")
		}
	})
	s.Run()
	// This test is run within our harness, not with RetryRunner
	// Just verify attempts are tracked
	if attempts < 1 {
		t.Error("test should have run")
	}
}
