package editverify

import (
	"regexp"
	"strings"
)

// standardDiag matches Go-style diagnostics: file.go:LINE:COL: message or file.go:LINE: message.
// Group 1=file, 2=line, 3=col (optional), 4=msg.
// Examples:
//
//	main.go:42:5: undefined: foo
//	main.go:10: cannot use x
var standardDiag = regexp.MustCompile(`^(\S+\.\w+):(\d+)(?::(\d+))?:\s*(.+)$`)

// testFail matches --- FAIL: TestName
var testFail = regexp.MustCompile(`^--- FAIL:\s+(\S+)`)

// panicLine matches "panic: " at the start of a line.
var panicLine = regexp.MustCompile(`^panic:\s*(.+)`)

// tscDiag matches TypeScript compiler diagnostics: file.ts(LINE,COL): message.
// Group 1=file, 2=line, 3=col, 4=msg. tsc uses a paren format the Go-style
// file:line:col matcher misses. Example:
//
//	src/app.ts(3,5): error TS2304: Cannot find name 'foo'.
var tscDiag = regexp.MustCompile(`^(\S+\.[jt]sx?)\((\d+),(\d+)\):\s*(.+)$`)

// Parse extracts structured Diagnostics from the raw output of one Step.
//
// Rules by step.Name:
//
//	"build"  → severity "error"; standard file:line:col:msg matches
//	"vet"    → severity "warning"; standard file:line:col:msg matches
//	"test"   → severity "error" for test failures / panics;
//	           also standard file_test.go:line: matches
//	"custom" → same as build (severity "error")
//
// Returns an empty slice (not nil) when no structured diagnostics are found.
func Parse(step Step, output string) []Diagnostic {
	if output == "" {
		return nil
	}

	sev := "error"
	if step.Name == "vet" {
		sev = "warning"
	}

	var diags []Diagnostic

	// For test output: capture FAIL lines, panics, and standard file:line matches.
	// Process every line — multiple diagnostics per output are normal.
	lines := strings.Split(output, "\n")
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}

		// --- FAIL: TestName
		if m := testFail.FindStringSubmatch(line); m != nil {
			diags = append(diags, Diagnostic{
				Msg: "FAIL: " + m[1],
				Sev: "error",
			})
			continue
		}

		// panic: ...
		if m := panicLine.FindStringSubmatch(line); m != nil {
			diags = append(diags, Diagnostic{
				Msg: "panic: " + m[1],
				Sev: "error",
			})
			continue
		}

		// tsc: file.ts(LINE,COL): message
		if m := tscDiag.FindStringSubmatch(line); m != nil {
			diags = append(diags, Diagnostic{
				File: m[1],
				Line: parseInt(m[2]),
				Col:  parseInt(m[3]),
				Msg:  strings.TrimSpace(m[4]),
				Sev:  sev,
			})
			continue
		}

		// Standard file.go:LINE:COL: message
		if m := standardDiag.FindStringSubmatch(line); m != nil {
			d := Diagnostic{
				File: m[1],
				Msg:  strings.TrimSpace(m[4]),
				Sev:  sev,
			}
			// line
			if _, err := parseNum(m[2]); err == nil {
				d.Line = int(parseInt(m[2]))
			}
			// optional col
			if m[3] != "" {
				if _, err := parseNum(m[3]); err == nil {
					d.Col = int(parseInt(m[3]))
				}
			}
			diags = append(diags, d)
		}
	}

	return diags
}

// parseNum checks whether s is a valid non-negative integer.
func parseNum(s string) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errBadNum
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

func parseInt(s string) int {
	n, _ := parseNum(s)
	return n
}

var errBadNum = errType{}

type errType struct{}

func (errType) Error() string { return "not a number" }
