package deadcode

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.go")
	os.WriteFile(path, []byte(`package test

func Used() string { return "ok" }

func Unused() string { return "unused" }

type Config struct { Name string }
`), 0o644)

	s := NewScanner()
	s.ScanFile(path)
	findings := s.Findings()
	t.Logf("found %d findings", len(findings))
}

func TestFormatFindings(t *testing.T) {
	out := FormatFindings(nil)
	if out == "" {
		t.Error("FormatFindings nil should return non-empty")
	}
	out = FormatFindings([]Finding{{Name: "deadFunc", Kind: "func", File: "test.go", Line: 1}})
	if out == "" {
		t.Error("FormatFindings should return non-empty")
	}
}

func TestExtractFnName(t *testing.T) {
	if extractFnName("func Run()") != "Run" {
		t.Error("func Run")
	}
	if extractFnName("func (s *Server) Start() error") != "Start" {
		t.Error("method")
	}
}

func TestExtractTypeName(t *testing.T) {
	if extractTypeName("type Config struct {") != "Config" {
		t.Error("type struct")
	}
	if extractTypeName("type Runner interface {") != "Runner" {
		t.Error("type interface")
	}
}
