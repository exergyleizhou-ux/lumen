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
	cmd := exec.CommandContext(ctx, step.Args[0], step.Args[1:]...)
	cmd.Dir = step.Dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOFLAGS=-mod=mod")
	out, err := cmd.CombinedOutput()
	return string(out), err == nil
}

// Verifier runs a verification plan against a project root.
type Verifier struct {
	root string
	cfg  Config
	run  Runner
}

// New returns a Verifier that shells out via the real toolchain.
func New(root string, cfg Config) *Verifier {
	return &Verifier{root: root, cfg: cfg, run: execRunner{}}
}

// Verify builds the plan for the changed files and runs each step in order,
// stopping at the first failure and returning its parsed diagnostics.
//
// TODO(claude): wire in Detect (D-V1) and Parse (D-V2) once they land:
//
//	for _, step := range Detect(v.root, changed, v.cfg) {
//	    out, ok := v.run.Run(ctx, step)
//	    if !ok {
//	        s := step
//	        return Result{OK: false, Failed: &s, Diagnostics: Parse(s, out), Output: truncate(out)}
//	    }
//	}
//	return Result{OK: true}
//
// Stubbed to OK until the pure helpers exist so the package compiles standalone
// and DeepSeek's D-V1/D-V2/D-V3 cards are unblocked.
func (v *Verifier) Verify(ctx context.Context, changed []string) Result {
	return Result{OK: true}
}

// truncate caps raw output retained for feedback.
func truncate(s string) string {
	if len(s) <= maxOutputBytes {
		return s
	}
	return s[:maxOutputBytes] + "\n…(truncated)"
}
