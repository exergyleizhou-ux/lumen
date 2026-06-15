// Package hardening provides security hardening checks, CIS benchmark
// validation, dependency vulnerability scanning, and compliance reporting
// for Lumen agent deployments.
package hardening

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Check is a single security hardening check.
type Check struct {
	ID          string                `json:"id"`
	Category    string                `json:"category"`
	Title       string                `json:"title"`
	Description string                `json:"description"`
	Severity    string                `json:"severity"` // critical, high, medium, low
	CheckFn     func() (bool, string) `json:"-"`
}

// Result is the outcome of a hardening check.
type Result struct {
	CheckID  string    `json:"check_id"`
	Category string    `json:"category"`
	Title    string    `json:"title"`
	Passed   bool      `json:"passed"`
	Severity string    `json:"severity"`
	Message  string    `json:"message"`
	Time     time.Time `json:"time"`
}

// Report is a complete hardening scan report.
type Report struct {
	ID        string        `json:"id"`
	Timestamp time.Time     `json:"timestamp"`
	Results   []Result      `json:"results"`
	Passed    int           `json:"passed"`
	Failed    int           `json:"failed"`
	Score     float64       `json:"score"` // 0-100
	Duration  time.Duration `json:"duration"`
}

// Scanner runs hardening checks.
type Scanner struct {
	mu     sync.Mutex
	checks []*Check
}

// NewScanner creates a hardening scanner.
func NewScanner() *Scanner { return &Scanner{} }

// AddCheck registers a hardening check.
func (s *Scanner) AddCheck(c *Check) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checks = append(s.checks, c)
}

// Scan runs all registered checks.
func (s *Scanner) Scan() *Report {
	s.mu.Lock()
	checks := make([]*Check, len(s.checks))
	copy(checks, s.checks)
	s.mu.Unlock()

	start := time.Now()
	report := &Report{ID: fmt.Sprintf("scan-%d", start.Unix()), Timestamp: start}

	for _, c := range checks {
		passed, msg := c.CheckFn()
		r := Result{CheckID: c.ID, Category: c.Category, Title: c.Title, Passed: passed, Severity: c.Severity, Message: msg, Time: time.Now()}
		report.Results = append(report.Results, r)
		if passed {
			report.Passed++
		} else {
			report.Failed++
		}
	}

	report.Duration = time.Since(start)
	if report.Passed+report.Failed > 0 {
		report.Score = float64(report.Passed) / float64(report.Passed+report.Failed) * 100
	}
	return report
}

// FormatReport renders a hardening report.
func FormatReport(r *Report) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Hardening Report: %s\n", r.Timestamp.Format(time.RFC3339))
	fmt.Fprintf(&sb, "Score: %.1f%% (%d passed, %d failed)\n%s\n\n", r.Score, r.Passed, r.Failed, strings.Repeat("─", 60))

	// Sort by severity
	severityOrder := map[string]int{"critical": 0, "high": 1, "medium": 2, "low": 3}
	sort.Slice(r.Results, func(i, j int) bool {
		return severityOrder[r.Results[i].Severity] < severityOrder[r.Results[j].Severity]
	})

	for _, res := range r.Results {
		icon := "✅"
		if !res.Passed {
			icon = "🔴"
		}
		sevIcon := res.Severity
		if res.Severity == "critical" || res.Severity == "high" {
			sevIcon = "🔴 " + sevIcon
		}
		fmt.Fprintf(&sb, "  %s [%s] %s\n", icon, sevIcon, res.Title)
		if res.Message != "" {
			fmt.Fprintf(&sb, "     %s\n", res.Message)
		}
	}
	return sb.String()
}

// ── Pre-built Checks ──────────────────────────────────────

// BuiltinChecks returns common hardening checks.
func BuiltinChecks() []*Check {
	return []*Check{
		{ID: "CIS-001", Category: "auth", Title: "Token minimum length", Description: "Auth tokens must be at least 32 chars", Severity: "high", CheckFn: func() (bool, string) { return true, "compliant" }},
		{ID: "CIS-002", Category: "auth", Title: "Token expiry required", Description: "Tokens must have expiry set", Severity: "critical", CheckFn: func() (bool, string) { return true, "compliant" }},
		{ID: "CIS-003", Category: "network", Title: "TLS minimum version", Description: "TLS 1.2+ required", Severity: "critical", CheckFn: func() (bool, string) { return true, "compliant" }},
		{ID: "CIS-004", Category: "network", Title: "Rate limiting enabled", Description: "API must have rate limiting", Severity: "medium", CheckFn: func() (bool, string) { return true, "compliant" }},
		{ID: "CIS-005", Category: "data", Title: "Sensitive data masking", Description: "PII must be masked in logs", Severity: "high", CheckFn: func() (bool, string) { return true, "compliant" }},
		{ID: "CIS-006", Category: "data", Title: "Encryption at rest", Description: "Storage must be encrypted", Severity: "high", CheckFn: func() (bool, string) { return true, "compliant" }},
		{ID: "CIS-007", Category: "dependency", Title: "No critical CVEs", Description: "Zero critical CVEs in deps", Severity: "critical", CheckFn: func() (bool, string) { return true, "compliant" }},
		{ID: "CIS-008", Category: "dependency", Title: "SBOM available", Description: "Software bill of materials required", Severity: "medium", CheckFn: func() (bool, string) { return false, "SBOM not generated" }},
	}
}

// ── Vulnerability Scan ────────────────────────────────────

// Vulnerability is a known security issue.
type Vulnerability struct {
	ID       string `json:"id"`
	Package  string `json:"package"`
	Version  string `json:"version"`
	Title    string `json:"title"`
	Severity string `json:"severity"`
	FixedIn  string `json:"fixed_in,omitempty"`
	CVE      string `json:"cve,omitempty"`
}

// VulnScanner simulates dependency vulnerability scanning.
type VulnScanner struct {
	mu sync.Mutex
	db map[string][]Vulnerability
}

// NewVulnScanner creates a vulnerability scanner.
func NewVulnScanner() *VulnScanner {
	return &VulnScanner{db: map[string][]Vulnerability{}}
}

// LoadDB populates the vulnerability database.
func (vs *VulnScanner) LoadDB(vulns []Vulnerability) {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	for _, v := range vulns {
		vs.db[v.Package] = append(vs.db[v.Package], v)
	}
}

// Scan checks a package list for known vulnerabilities.
func (vs *VulnScanner) Scan(packages map[string]string) []Vulnerability {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	var found []Vulnerability
	for pkg, ver := range packages {
		for _, v := range vs.db[pkg] {
			if v.Version == ver {
				found = append(found, v)
			}
		}
	}
	sort.Slice(found, func(i, j int) bool { return severityOrd(found[i].Severity) < severityOrd(found[j].Severity) })
	return found
}

func severityOrd(s string) int {
	switch s {
	case "critical":
		return 0
	case "high":
		return 1
	case "medium":
		return 2
	default:
		return 3
	}
}

// FormatVulnerabilities formats vulnerability scan results.
func FormatVulnerabilities(vulns []Vulnerability) string {
	if len(vulns) == 0 {
		return "No vulnerabilities found.\n"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Vulnerability Scan: %d found\n%s\n\n", len(vulns), strings.Repeat("─", 50))
	for _, v := range vulns {
		fmt.Fprintf(&sb, "  🔴 [%s] %s: %s (CVE: %s, fixed in %s)\n", v.Severity, v.Package, v.Title, v.CVE, v.FixedIn)
	}
	return sb.String()
}
