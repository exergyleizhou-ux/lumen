package diag

import (
	"fmt"
	"testing"
	"time"
)

func TestRunProbe(t *testing.T) {
	e := NewEngine()
	e.RegisterProbe(&Probe{Name: "ok", Fn: func() error { return nil }, Timeout: time.Second})
	is := e.RunProbe("ok")
	if is != nil {
		t.Error("should be nil")
	}
}
func TestRunProbeFail(t *testing.T) {
	e := NewEngine()
	e.RegisterProbe(&Probe{Name: "fail", Fn: func() error { return fmt.Errorf("boom") }, Timeout: time.Second})
	is := e.RunProbe("fail")
	if is == nil || is.Severity != SevWarning {
		t.Error("should find issue")
	}
}
func TestSummary(t *testing.T) {
	e := NewEngine()
	e.RegisterProbe(&Probe{Name: "p", Fn: func() error { return fmt.Errorf("err") }, Timeout: time.Second})
	e.RunAll()
	s := e.Summary()
	if s[SevWarning] != 1 {
		t.Error("summary count")
	}
}
func TestFormatReport(t *testing.T) {
	e := NewEngine()
	e.RegisterProbe(&Probe{Name: "ok", Fn: func() error { return nil }, Timeout: time.Second})
	e.RunAll()
	r := e.FormatReport()
	if r == "" {
		t.Error("format")
	}
}
