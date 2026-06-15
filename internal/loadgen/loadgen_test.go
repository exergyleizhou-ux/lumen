package loadgen

import (
	"fmt"
	"strings"
	"testing"
)

func TestRunBasic(t *testing.T) {
	r := NewRunner()
	rep, _ := r.Run(Config{Name: "test", Concurrency: 2, TotalRequests: 10, TargetFn: func(id int64) error { return nil }})
	if rep.Success != 10 { t.Error("all should succeed") }
}
func TestRunFailures(t *testing.T) {
	r := NewRunner()
	rep, _ := r.Run(Config{Name: "fail", Concurrency: 1, TotalRequests: 5, TargetFn: func(id int64) error { return fmt.Errorf("boom") }})
	if rep.Failed != 5 { t.Error("all should fail") }
}
func TestFormatReport(t *testing.T) {
	r := NewRunner()
	rep, _ := r.Run(Config{Name: "fmt", Concurrency: 1, TotalRequests: 3, TargetFn: func(id int64) error { return nil }})
	s := FormatReport(rep)
	if !strings.Contains(s, "3") { t.Error("format") }
}
