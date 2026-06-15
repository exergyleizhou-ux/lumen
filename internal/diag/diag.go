// Package diag provides agent self-diagnostics: health check aggregation,
// dependency status, connectivity probes, and issue classification.
package diag

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Severity is the severity level of a diagnostic issue.
type Severity int

const (
	SevInfo    Severity = 0
	SevWarning Severity = 1
	SevError   Severity = 2
	SevFatal   Severity = 3
)

func (s Severity) String() string {
	switch s {
	case SevInfo:
		return "INFO"
	case SevWarning:
		return "WARN"
	case SevError:
		return "ERROR"
	case SevFatal:
		return "FATAL"
	default:
		return "UNKNOWN"
	}
}

// Issue is a single diagnostic finding.
type Issue struct {
	ID        string    `json:"id"`
	Component string    `json:"component"`
	Title     string    `json:"title"`
	Detail    string    `json:"detail"`
	Severity  Severity  `json:"severity"`
	Timestamp time.Time `json:"timestamp"`
	Resolved  bool      `json:"resolved"`
}

// Probe is a health check function.
type Probe struct {
	Name     string        `json:"name"`
	Fn       func() error  `json:"-"`
	Timeout  time.Duration `json:"timeout"`
	Interval time.Duration `json:"interval"`
}

// Engine runs diagnostics.
type Engine struct {
	mu      sync.Mutex
	probes  map[string]*Probe
	issues  []*Issue
	hist    []*Issue
	maxHist int
	running bool
	stopCh  chan struct{}
}

// NewEngine creates a diagnostic engine.
func NewEngine() *Engine {
	return &Engine{probes: map[string]*Probe{}, maxHist: 500, stopCh: make(chan struct{})}
}

// RegisterProbe adds a health probe.
func (e *Engine) RegisterProbe(probe *Probe) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.probes[probe.Name] = probe
}

// RunProbe executes a single probe and records issues.
func (e *Engine) RunProbe(name string) *Issue {
	e.mu.Lock()
	probe, ok := e.probes[name]
	e.mu.Unlock()
	if !ok {
		return nil
	}

	done := make(chan error, 1)
	go func() {
		done <- probe.Fn()
	}()

	var err error
	select {
	case err = <-done:
	case <-time.After(probe.Timeout):
		err = fmt.Errorf("probe timed out after %v", probe.Timeout)
	}

	if err != nil {
		issue := &Issue{
			ID:        fmt.Sprintf("%s-%d", name, time.Now().UnixNano()),
			Component: name, Title: fmt.Sprintf("probe %s failed", name),
			Detail: err.Error(), Severity: SevWarning, Timestamp: time.Now(),
		}
		e.mu.Lock()
		e.issues = append(e.issues, issue)
		e.hist = append(e.hist, issue)
		if len(e.hist) > e.maxHist {
			e.hist = e.hist[1:]
		}
		e.mu.Unlock()
		return issue
	}

	// Resolve any existing issues for this component
	e.mu.Lock()
	for _, is := range e.issues {
		if is.Component == name && !is.Resolved {
			is.Resolved = true
		}
	}
	e.mu.Unlock()
	return nil
}

// RunAll executes all probes.
func (e *Engine) RunAll() []*Issue {
	e.mu.Lock()
	probes := make([]*Probe, 0, len(e.probes))
	for _, p := range e.probes {
		probes = append(probes, p)
	}
	e.mu.Unlock()

	e.mu.Lock()
	e.issues = nil
	e.mu.Unlock()

	var issues []*Issue
	for _, p := range probes {
		if is := e.RunProbe(p.Name); is != nil {
			issues = append(issues, is)
		}
	}
	return issues
}

// Issues returns active (unresolved) issues.
func (e *Engine) Issues() []*Issue {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []*Issue
	for _, is := range e.issues {
		if !is.Resolved {
			out = append(out, is)
		}
	}
	return out
}

// History returns all recorded issues.
func (e *Engine) History() []*Issue {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]*Issue, len(e.hist))
	copy(out, e.hist)
	return out
}

// Summary returns counts by severity.
func (e *Engine) Summary() map[Severity]int {
	e.mu.Lock()
	defer e.mu.Unlock()
	counts := map[Severity]int{}
	for _, is := range e.issues {
		if !is.Resolved {
			counts[is.Severity]++
		}
	}
	return counts
}

// FormatReport formats a diagnostic report.
func (e *Engine) FormatReport() string {
	var sb strings.Builder
	issues := e.Issues()
	summary := e.Summary()

	fmt.Fprintf(&sb, "Diagnostic Report:\n%s\n\n", strings.Repeat("═", 60))
	fmt.Fprintf(&sb, "  Probes: %d registered\n", len(e.probes))
	fmt.Fprintf(&sb, "  Active Issues: %d\n", len(issues))

	// Severity summary
	sevs := []Severity{SevFatal, SevError, SevWarning, SevInfo}
	for _, sev := range sevs {
		if c, ok := summary[sev]; ok && c > 0 {
			icon := iconForSev(sev)
			fmt.Fprintf(&sb, "    %s %s: %d\n", icon, sev.String(), c)
		}
	}

	if len(issues) > 0 {
		sort.Slice(issues, func(i, j int) bool { return issues[i].Severity > issues[j].Severity })
		fmt.Fprintf(&sb, "\nIssues:\n")
		for _, is := range issues {
			fmt.Fprintf(&sb, "  %s [%s] %s\n", iconForSev(is.Severity), is.Severity.String(), is.Title)
			if is.Detail != "" {
				fmt.Fprintf(&sb, "     %s\n", is.Detail)
			}
		}
	}

	if len(issues) == 0 {
		fmt.Fprintf(&sb, "\n  ✅ All systems healthy.\n")
	}
	return sb.String()
}

func iconForSev(s Severity) string {
	switch s {
	case SevFatal:
		return "💀"
	case SevError:
		return "🔴"
	case SevWarning:
		return "🟡"
	default:
		return "🔵"
	}
}

// ── Connectivity Prober ──────────────────────────────────

// ConnectivityResult is the result of a connectivity check.
type ConnectivityResult struct {
	Target    string        `json:"target"`
	Port      int           `json:"port"`
	Reachable bool          `json:"reachable"`
	Latency   time.Duration `json:"latency"`
	Error     string        `json:"error,omitempty"`
}

// ConnectivityProber checks network connectivity.
type ConnectivityProber struct {
	mu      sync.Mutex
	results []ConnectivityResult
	maxRes  int
}

// NewConnectivityProber creates a connectivity prober.
func NewConnectivityProber() *ConnectivityProber {
	return &ConnectivityProber{maxRes: 200}
}

// CheckResult records a connectivity result.
func (cp *ConnectivityProber) CheckResult(result ConnectivityResult) {
	cp.mu.Lock()
	defer cp.mu.Unlock()
	cp.results = append(cp.results, result)
	if len(cp.results) > cp.maxRes {
		cp.results = cp.results[1:]
	}
}

// Results returns recent connectivity results.
func (cp *ConnectivityProber) Results() []ConnectivityResult {
	cp.mu.Lock()
	defer cp.mu.Unlock()
	out := make([]ConnectivityResult, len(cp.results))
	copy(out, cp.results)
	return out
}

// Reachable returns the count of reachable targets.
func (cp *ConnectivityProber) Reachable() int {
	cp.mu.Lock()
	defer cp.mu.Unlock()
	count := 0
	for _, r := range cp.results {
		if r.Reachable {
			count++
		}
	}
	return count
}

// FormatConnectivity formats connectivity results.
func (cp *ConnectivityProber) FormatConnectivity() string {
	var sb strings.Builder
	results := cp.Results()
	reachable := cp.Reachable()
	fmt.Fprintf(&sb, "Connectivity: %d/%d reachable\n%s\n\n", reachable, len(results), strings.Repeat("─", 50))
	for _, r := range results {
		icon := "✅"
		if !r.Reachable {
			icon = "🔴"
		}
		fmt.Fprintf(&sb, "  %s %s:%-5d %v", icon, r.Target, r.Port, r.Latency)
		if r.Error != "" {
			fmt.Fprintf(&sb, "  %s", r.Error)
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}
