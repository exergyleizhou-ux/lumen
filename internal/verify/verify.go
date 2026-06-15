// Package verify provides verification checks for agent outputs:
// schema validation, integrity checks, output sanitization, and
// conformance testing against expected behaviors.
package verify

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Check struct {
	Name     string
	Category string
	Fn       func(input any) (bool, string)
}
type Result struct {
	CheckName string
	Passed    bool
	Message   string
	Duration  time.Duration
	Timestamp time.Time
}
type Verifier struct {
	mu      sync.Mutex
	checks  []Check
	results []Result
	maxRes  int
}

func NewVerifier() *Verifier { return &Verifier{maxRes: 1000} }
func (v *Verifier) AddCheck(c Check) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.checks = append(v.checks, c)
}
func (v *Verifier) Verify(input any) []Result {
	v.mu.Lock()
	checks := make([]Check, len(v.checks))
	copy(checks, v.checks)
	v.mu.Unlock()
	var results []Result
	for _, c := range checks {
		start := time.Now()
		passed, msg := c.Fn(input)
		results = append(results, Result{CheckName: c.Name, Passed: passed, Message: msg, Duration: time.Since(start), Timestamp: time.Now()})
	}
	v.mu.Lock()
	v.results = append(v.results, results...)
	if len(v.results) > v.maxRes {
		v.results = v.results[1:]
	}
	v.mu.Unlock()
	return results
}
func (v *Verifier) AllPassed(input any) bool {
	for _, r := range v.Verify(input) {
		if !r.Passed {
			return false
		}
	}
	return true
}
func (v *Verifier) FormatResults() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(v.results) == 0 {
		return "No verification results.\n"
	}
	var sb strings.Builder
	passed := 0
	for _, r := range v.results {
		if r.Passed {
			passed++
		}
	}
	fmt.Fprintf(&sb, "Verification: %d/%d passed\n%s\n\n", passed, len(v.results), strings.Repeat("─", 40))
	for _, r := range v.results {
		icon := "✅"
		if !r.Passed {
			icon = "🔴"
		}
		fmt.Fprintf(&sb, "  %s %-25s %v %s\n", icon, r.CheckName, r.Duration, r.Message)
	}
	return sb.String()
}

func BuiltinChecks() []Check {
	return []Check{
		{Name: "no-empty-string", Category: "sanitization", Fn: func(input any) (bool, string) {
			s, ok := input.(string)
			if !ok {
				return true, "not a string"
			}
			if strings.TrimSpace(s) == "" {
				return false, "empty string"
			}
			return true, "ok"
		}},
		{Name: "no-html-injection", Category: "security", Fn: func(input any) (bool, string) {
			s, ok := input.(string)
			if !ok {
				return true, "not a string"
			}
			dangerous := []string{"<script>", "javascript:", "onerror="}
			for _, d := range dangerous {
				if strings.Contains(strings.ToLower(s), d) {
					return false, fmt.Sprintf("contains %q", d)
				}
			}
			return true, "ok"
		}},
		{Name: "valid-json", Category: "format", Fn: func(input any) (bool, string) {
			s, ok := input.(string)
			if !ok {
				return true, "not a string"
			}
			var v any
			if err := json.Unmarshal([]byte(s), &v); err != nil {
				return false, err.Error()
			}
			return true, "ok"
		}},
		{Name: "max-length-1mb", Category: "size", Fn: func(input any) (bool, string) {
			s, ok := input.(string)
			if !ok {
				return true, "not a string"
			}
			if len(s) > 1024*1024 {
				return false, "exceeds 1MB"
			}
			return true, "ok"
		}},
		{Name: "no-path-traversal", Category: "security", Fn: func(input any) (bool, string) {
			s, ok := input.(string)
			if !ok {
				return true, "not a string"
			}
			if strings.Contains(s, "../") || strings.Contains(s, "..\\") {
				return false, "path traversal detected"
			}
			return true, "ok"
		}},
	}
}

// ── Integrity Verifier ────────────────────────────────────

type IntegrityResult struct {
	File     string
	Expected string
	Actual   string
	Match    bool
}
type IntegrityVerifier struct {
	mu        sync.Mutex
	checksums map[string]string
}

func NewIntegrityVerifier() *IntegrityVerifier {
	return &IntegrityVerifier{checksums: map[string]string{}}
}
func (iv *IntegrityVerifier) Register(file, checksum string) {
	iv.mu.Lock()
	defer iv.mu.Unlock()
	iv.checksums[file] = checksum
}
func (iv *IntegrityVerifier) Check(file string, data []byte) *IntegrityResult {
	h := sha256.Sum256(data)
	actual := hex.EncodeToString(h[:])
	iv.mu.Lock()
	expected, ok := iv.checksums[file]
	iv.mu.Unlock()
	match := ok && expected == actual
	return &IntegrityResult{File: file, Expected: expected, Actual: actual, Match: match}
}
func (iv *IntegrityVerifier) CheckAll(files map[string][]byte) []IntegrityResult {
	var out []IntegrityResult
	for f, d := range files {
		out = append(out, *iv.Check(f, d))
	}
	return out
}
func FormatIntegrityResults(results []IntegrityResult) string {
	if len(results) == 0 {
		return "No integrity checks.\n"
	}
	var sb strings.Builder
	passed := 0
	for _, r := range results {
		if r.Match {
			passed++
		}
	}
	fmt.Fprintf(&sb, "Integrity: %d/%d matched\n%s\n\n", passed, len(results), strings.Repeat("─", 50))
	for _, r := range results {
		icon := "✅"
		if !r.Match {
			icon = "🔴"
		}
		fmt.Fprintf(&sb, "  %s %s expected=%s actual=%s\n", icon, r.File, r.Expected[:16], r.Actual[:16])
	}
	return sb.String()
}

// ── Regex Validator ──────────────────────────────────────

type RegexValidator struct {
	mu    sync.Mutex
	rules map[string]*regexp.Regexp
}

func NewRegexValidator() *RegexValidator { return &RegexValidator{rules: map[string]*regexp.Regexp{}} }
func (rv *RegexValidator) AddRule(name, pattern string) error {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return err
	}
	rv.mu.Lock()
	defer rv.mu.Unlock()
	rv.rules[name] = re
	return nil
}
func (rv *RegexValidator) Validate(input string) map[string]bool {
	rv.mu.Lock()
	defer rv.mu.Unlock()
	results := map[string]bool{}
	for name, re := range rv.rules {
		results[name] = re.MatchString(input)
	}
	return results
}
