// Package editverify implements the verify-after-edit self-repair loop: after a
// writer-tool batch, it runs build/vet/test, captures structured diagnostics
// from any failure, and the agent loop feeds those back to the model for
// self-repair. See docs/superpowers/specs/2026-06-17-lumen-verify-after-edit-design.md.
//
// This file is the Claude-owned skeleton: the shared types, default config, the
// command runner, and the Verifier orchestrator. The pure helpers Detect
// (detect.go), Parse (parse.go), and ConfigFromTOML (config.go) are implemented
// separately (DeepSeek cards D-V1/D-V2/D-V3) against the signatures here.
//
// (Distinct from the unrelated, currently-unused internal/verify package, which
// does output schema/integrity checks — flagged for C-6 review.)
package editverify

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// maxOutputBytes caps how much raw command output we retain/feed back.
const maxOutputBytes = 8 * 1024

// Step is one executable verification command.
type Step struct {
	Name string   // "build" | "vet" | "test" | "custom"
	Dir  string   // working directory (usually the project root)
	Args []string // e.g. ["go", "build", "./..."]
}

// Diagnostic is one structured finding parsed from a step's output.
type Diagnostic struct {
	File string // relative to project root
	Line int
	Col  int
	Msg  string
	Sev  string // "error" | "warning"
}

// Result is the outcome of one Verify call.
type Result struct {
	OK          bool
	Failed      *Step        // first failing step (nil when OK)
	Diagnostics []Diagnostic // parsed from the failing step's output
	Output      string       // raw output of the failing step (truncated)
	LSPDiags    []Diagnostic // LSP diagnostics from gopls (even when build passes)
}

// Config controls verification behavior; loaded from lumen.toml [verify].
type Config struct {
	Enabled         bool
	Command         string // override; empty = auto-detect
	Scope           string // "changed-pkg" (default) | "all"
	RunTests        bool
	MaxRepairCycles int
}

// DefaultConfig returns the built-in defaults: verification on, auto-detected
// Go commands, changed-package test scope, up to 3 self-repair cycles.
func DefaultConfig() Config {
	return Config{
		Enabled:         true,
		Command:         "",
		Scope:           "changed-pkg",
		RunTests:        true,
		MaxRepairCycles: 3,
	}
}

// Runner executes a single Step and reports its combined output and whether it
// succeeded (exit code 0). Abstracted so the orchestrator can be tested with a
// fake runner instead of shelling out.
type Runner interface {
	Run(ctx context.Context, step Step) (output string, ok bool)
}

// execRunner runs steps as real subprocesses with the project's toolchain env.
type execRunner struct{}

func (execRunner) Run(ctx context.Context, step Step) (string, bool) {
	if len(step.Args) == 0 {
		return "", true
	}
	// If the tool isn't installed, SKIP the step (inconclusive → treated as ok)
	// rather than reporting a failure: a missing linter/test runner must not
	// trigger a bogus self-repair cycle. This keeps multi-language verify safe in
	// a partial toolchain. Mirrors collectLSPDiags skipping an absent gopls.
	if _, err := exec.LookPath(step.Args[0]); err != nil {
		return "", true
	}
	cmd := exec.CommandContext(ctx, step.Args[0], step.Args[1:]...)
	cmd.Dir = step.Dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOFLAGS=-mod=mod")
	out, err := cmd.CombinedOutput()
	return string(out), err == nil
}

// Verifier runs a verification plan against a project root.
type Verifier struct {
	root     string
	cfg      Config
	run      Runner
	lspDiags []Diagnostic // collected across steps, returned in final result
}

// New returns a Verifier that shells out via the real toolchain.
func New(root string, cfg Config) *Verifier {
	return &Verifier{root: root, cfg: cfg, run: execRunner{}}
}

// Verify builds the plan for the changed files (Detect) and runs each step in
// order, stopping at the first failure and returning its parsed diagnostics
// (Parse). Returns OK when every step succeeds.
//
// Even when build/vet/test all pass, it also collects LSP diagnostics from gopls
// for every changed .go file — the model sees compiler-level errors/warnings
// that tools like gopls surface before the builds break (P3 wire-in).
func (v *Verifier) Verify(ctx context.Context, changed []string) Result {
	rel := v.sameModulePaths(relativizePaths(v.root, changed))
	for _, step := range Detect(v.root, rel, v.cfg) {
		out, ok := v.run.Run(ctx, step)
		if !ok {
			s := step
			return Result{
				OK:          false,
				Failed:      &s,
				Diagnostics: Parse(s, out),
				Output:      truncate(out),
			}
		}
	}

	// Build/vet/test all passed — collect LSP diagnostics for changed .go files.
	// These are informational — the model can see issues gopls would flag even
	// though the code compiled. Pass empty changed list → skip (too expensive).
	res := Result{OK: true}
	if len(rel) > 0 {
		v.collectLSPDiags(ctx, rel)
		res.LSPDiags = v.lspDiags
	}
	return res
}

// sameModulePaths filters the (root-relative) changed paths down to those that
// can become a valid `go test ./<dir>` target for root's module. It drops .go
// files whose directory: escapes root (".." / absolute), no longer exists
// (deleted), or belongs to a NESTED module (a go.mod between root and the file).
// Non-.go paths pass through (Detect ignores them for the test step). This keeps
// monorepo/nested-module/deleted-file changes from producing a spurious test
// failure that the model would then try to "fix".
func (v *Verifier) sameModulePaths(rel []string) []string {
	out := make([]string, 0, len(rel))
	for _, p := range rel {
		if !strings.HasSuffix(p, ".go") {
			out = append(out, p)
			continue
		}
		if filepath.IsAbs(p) || strings.HasPrefix(p, "..") {
			continue // escapes root
		}
		dir := filepath.Dir(p)
		if fi, err := os.Stat(filepath.Join(v.root, dir)); err != nil || !fi.IsDir() {
			continue // dir gone (e.g. file deleted)
		}
		if v.hasNestedGoMod(dir) {
			continue // different module — can't `go test ./dir` from root
		}
		out = append(out, p)
	}
	return out
}

// hasNestedGoMod reports whether any directory from dir up to (but excluding)
// root contains a go.mod — i.e. dir lives in a nested module, not root's.
func (v *Verifier) hasNestedGoMod(dir string) bool {
	for cur := dir; cur != "." && cur != "" && cur != string(filepath.Separator); cur = filepath.Dir(cur) {
		if _, err := os.Stat(filepath.Join(v.root, cur, "go.mod")); err == nil {
			return true
		}
	}
	return false
}

// relativizePaths converts absolute changed-file paths to paths relative to root
// so Detect can derive `./<pkg>` test targets. Paths already relative (or not
// under root) are passed through unchanged.
func relativizePaths(root string, paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if filepath.IsAbs(p) {
			if r, err := filepath.Rel(root, p); err == nil {
				out = append(out, r)
				continue
			}
		}
		out = append(out, p)
	}
	return out
}

// truncate caps raw output retained for feedback.
func truncate(s string) string {
	if len(s) <= maxOutputBytes {
		return s
	}
	return s[:maxOutputBytes] + "\n…(truncated)"
}

// ── LSP diagnostics (P3) ──────────────────────────────────

// collectLSPDiags runs gopls check on each changed .go file and appends
// findings to the Verifier's in-progress Result. This feeds gopls diagnostics
// into the verify-after-edit loop so the model can see compiler-level warnings
// even when go build passes.
func (v *Verifier) collectLSPDiags(ctx context.Context, rel []string) {
	gopls, err := exec.LookPath("gopls")
	if err != nil {
		return // gopls not installed — skip
	}
	for _, f := range rel {
		if !strings.HasSuffix(f, ".go") {
			continue
		}
		abs := filepath.Join(v.root, f)
		if _, err := os.Stat(abs); err != nil {
			continue // file doesn't exist (was deleted)
		}
		// gopls check <file> — fast, per-file, no server needed
		c := exec.CommandContext(ctx, gopls, "check", abs)
		out, _ := c.CombinedOutput()
		if len(out) == 0 {
			continue
		}
		// Parse gopls check output: each line is file.go:line:col: msg
		for _, raw := range strings.Split(string(out), "\n") {
			line := strings.TrimSpace(raw)
			if line == "" {
				continue
			}
			if m := standardDiag.FindStringSubmatch(line); m != nil {
				d := Diagnostic{
					File: m[1],
					Msg:  strings.TrimSpace(m[4]),
					Sev:  "warning", // gopls issues are warnings unless build fails
				}
				if n, err := parseNum(m[2]); err == nil {
					d.Line = n
				}
				if m[3] != "" {
					if n, err := parseNum(m[3]); err == nil {
						d.Col = n
					}
				}
				// gopls check uses absolute paths — relativize back
				if filepath.IsAbs(d.File) {
					if relPath, err := filepath.Rel(v.root, d.File); err == nil {
						d.File = relPath
					}
				}
				v.lspDiags = append(v.lspDiags, d)
			}
		}
	}
}
