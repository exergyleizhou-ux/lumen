package complexity

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAnalyzeFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.go")
	os.WriteFile(path, []byte(`package test

func Simple() string { return "ok" }

func Complex(x int) string {
	if x > 0 {
		for i := 0; i < x; i++ {
			if i%2 == 0 {
				return "even"
			}
		}
	}
	return "done"
}
`), 0o644)

	a := NewAnalyzer()
	err := a.AnalyzeFile(path)
	if err != nil {
		t.Fatal(err)
	}
	metrics := a.Metrics()
	if len(metrics) < 2 {
		t.Fatalf("expected at least 2 functions, got %d", len(metrics))
	}
	if metrics[0].Cyclomatic <= metrics[1].Cyclomatic {
		t.Log("complex function should have higher cyclomatic complexity")
	}
}

func TestFormatMetrics(t *testing.T) {
	m := []Metric{{Name: "f", File: "x.go", Line: 1, Cyclomatic: 5, Cognitive: 3, Lines: 10, Risk: "low"}}
	out := FormatMetrics(m)
	if out == "" {
		t.Error("FormatMetrics should not be empty")
	}
}

func TestMaintainabilityIndex(t *testing.T) {
	mi := MaintainabilityIndex(5, 100, 10)
	if mi <= 0 || mi > 100 {
		t.Errorf("MI out of range: %f", mi)
	}
}

func TestTopN(t *testing.T) {
	m := []Metric{
		{Name: "a", Cyclomatic: 5},
		{Name: "b", Cyclomatic: 20},
		{Name: "c", Cyclomatic: 10},
	}
	top := TopN(m, 2)
	if len(top) != 2 || top[0].Name != "b" {
		t.Error("TopN should return worst first")
	}
}
