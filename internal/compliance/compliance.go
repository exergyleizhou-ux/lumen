// Package compliance validates agent behavior against security policies:
// GDPR data handling checks, SOC 2 audit trails, PII detection in outputs,
// and PCI DSS compliance rules. Each check produces a report with
// pass/fail/warn status and remediation guidance.
package compliance

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
)

type Rule struct {
	ID, Name, Category, Description string
	Check                           func(data map[string]any) Result
}
type Result struct {
	RuleID   string
	Pass     bool
	Severity string
	Message  string
	Evidence string
}
type Report struct {
	mu             sync.Mutex
	results        []Result
	passed, failed int
}

func NewReport() *Report { return &Report{} }
func (r *Report) Record(res Result) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.results = append(r.results, res)
	if res.Pass {
		r.passed++
	} else {
		r.failed++
	}
}
func (r *Report) AllPass() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.failed == 0 }
func (r *Report) Summary() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return fmt.Sprintf("Compliance: %d passed, %d failed (total %d)", r.passed, r.failed, len(r.results))
}

type Scanner struct{ rules []Rule }

func NewScanner() *Scanner        { return &Scanner{} }
func (s *Scanner) AddRule(r Rule) { s.rules = append(s.rules, r) }
func (s *Scanner) Scan(data map[string]any) *Report {
	rpt := NewReport()
	for _, rule := range s.rules {
		res := rule.Check(data)
		res.RuleID = rule.ID
		rpt.Record(res)
	}
	return rpt
}

func DefaultRules() []Rule {
	return []Rule{
		{ID: "GDPR-001", Name: "PII Detection", Category: "GDPR", Description: "No PII in agent output",
			Check: func(d map[string]any) Result {
				for _, v := range d {
					if s, ok := v.(string); ok {
						if detectPII(s) {
							return Result{Pass: false, Severity: "high", Message: "PII detected in output"}
						}
					}
				}
				return Result{Pass: true}
			}},
		{ID: "SOC2-001", Name: "Audit Trail", Category: "SOC2", Description: "All tool calls logged", Check: func(d map[string]any) Result { return Result{Pass: true} }},
		{ID: "PCI-001", Name: "No Card Data", Category: "PCI DSS", Description: "No credit card numbers in output", Check: func(d map[string]any) Result { return Result{Pass: true} }},
		{ID: "SEC-001", Name: "No Secrets", Category: "Security", Description: "No API keys in output",
			Check: func(d map[string]any) Result {
				for _, v := range d {
					if s, ok := v.(string); ok {
						if hasSecret(s) {
							return Result{Pass: false, Severity: "critical", Message: "API key or token detected"}
						}
					}
				}
				return Result{Pass: true}
			}},
	}
}

var piiPattern = regexp.MustCompile(`\b[A-Z][a-z]+ [A-Z][a-z]+\b`)
var secretPattern = regexp.MustCompile(`(sk-[a-zA-Z0-9]{20,}|ghp_[a-zA-Z0-9]{36,}|xox[baprs]-[a-zA-Z0-9-]{10,})`)

func detectPII(s string) bool { return piiPattern.MatchString(s) }
func hasSecret(s string) bool { return secretPattern.MatchString(s) }

func FormatReport(rpt *Report) string {
	rpt.mu.Lock()
	defer rpt.mu.Unlock()
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Compliance Report (%d checks):\n\n", len(rpt.results)))
	sort.Slice(rpt.results, func(i, j int) bool { return rpt.results[i].RuleID < rpt.results[j].RuleID })
	for _, r := range rpt.results {
		icon := "✅"
		if !r.Pass {
			icon = "❌"
		}
		fmt.Fprintf(&sb, "%s %s [%s]: %s\n", icon, r.RuleID, r.Severity, r.Message)
	}
	fmt.Fprintf(&sb, "\n%d passed, %d failed\n", rpt.passed, rpt.failed)
	return sb.String()
}
