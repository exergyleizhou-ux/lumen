package hardening

import (
	"testing"
)

func TestScannerBuiltin(t *testing.T) {
	s := NewScanner()
	for _, c := range BuiltinChecks() {
		s.AddCheck(c)
	}
	report := s.Scan()
	if report.Passed+report.Failed != len(BuiltinChecks()) {
		t.Error("check count")
	}
	if report.Score > 100 {
		t.Error("score")
	}
}
func TestFormatReport(t *testing.T) {
	s := NewScanner()
	for _, c := range BuiltinChecks() {
		s.AddCheck(c)
	}
	r := s.Scan()
	formatted := FormatReport(r)
	if formatted == "" {
		t.Error("format")
	}
}
func TestVulnScanner(t *testing.T) {
	vs := NewVulnScanner()
	vs.LoadDB([]Vulnerability{
		{ID: "CVE-2024-0001", Package: "libfoo", Version: "1.0", Title: "Critical bug", Severity: "critical", CVE: "CVE-2024-0001"},
	})
	found := vs.Scan(map[string]string{"libfoo": "1.0", "libbar": "2.0"})
	if len(found) != 1 {
		t.Error("should find 1 vuln")
	}
	if found[0].Package != "libfoo" {
		t.Error("pkg")
	}
}
func TestFormatVulns(t *testing.T) {
	s := FormatVulnerabilities([]Vulnerability{{Package: "pkg", Title: "bug", Severity: "high"}})
	if s == "" {
		t.Error("format")
	}
}
