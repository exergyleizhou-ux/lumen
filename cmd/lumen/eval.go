package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"lumen/internal/control"
	"lumen/internal/eval"
	"lumen/internal/event"
	"lumen/internal/provider"
	runworkspace "lumen/internal/workspace"
)

// runEval drives the coding-quality harness: each task's broken workspace is
// copied to a temp dir, the agent runs the task prompt there, then the task's
// check command scores pass/fail. Reports pass-rate + median steps + cost so
// model/provider changes (and regressions) are measurable, not guessed.
func runEval(args []string) {
	fs := flag.NewFlagSet("eval", flag.ExitOnError)
	tasksDir := fs.String("tasks", "evals/tasks", "directory of eval tasks")
	list := fs.Bool("list", false, "list tasks and exit (no model needed)")
	keep := fs.Bool("keep", false, "keep each task's workspace for inspection")
	repeat := fs.Int("repeat", 1, "run each task N times (local models are non-deterministic)")
	asJSON := fs.Bool("json", false, "emit machine-readable JSON instead of the pretty report")
	// Research cell coordinates (failure-mode study). The server context window is
	// an operator-asserted axis: the harness can't read LM Studio's `-c` from an
	// OpenAI endpoint, so it's passed in for ρ = first_prompt_tokens / window and
	// stamped onto each result for reproducibility. tool-profile/model-label are
	// record-only labels (the actual profile is set in lumen.toml).
	effWindow := fs.Int("eff-window", 0, "server context window (LM Studio -c) for ρ + the record; 0 = unknown")
	toolProfile := fs.String("tool-profile", "", "tool-profile label for the result record (core/full/micro)")
	modelLabel := fs.String("model-label", "", "model label for the result record; empty = the active model id")
	_ = fs.Parse(args)
	if *repeat < 1 {
		*repeat = 1
	}
	meta := cellMeta{model: *modelLabel, toolProfile: *toolProfile, serverWindow: *effWindow}

	tasks, err := eval.LoadTasks(*tasksDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "eval: load tasks:", err)
		os.Exit(1)
	}
	if len(tasks) == 0 {
		fmt.Fprintln(os.Stderr, "eval: no tasks found in", *tasksDir)
		os.Exit(1)
	}

	if *list {
		fmt.Printf("%d eval task(s) in %s:\n", len(tasks), *tasksDir)
		for _, t := range tasks {
			fmt.Printf("  • %-18s %s\n", t.Name, firstLine(t.Prompt, 72))
		}
		return
	}

	// Resolve the config path from the ORIGINAL cwd before we chdir per task, so
	// every task run uses the user's provider config while operating in its own
	// isolated workspace.
	cfgPath := ""
	if abs, err := filepath.Abs("lumen.toml"); err == nil {
		if _, e := os.Stat(abs); e == nil {
			cfgPath = abs
		}
	}
	orig, _ := os.Getwd()

	var results []eval.Result
	for i, task := range tasks {
		for rep := 0; rep < *repeat; rep++ {
			if !*asJSON {
				label := task.Name
				if *repeat > 1 {
					label = fmt.Sprintf("%s (run %d/%d)", task.Name, rep+1, *repeat)
				}
				fmt.Printf("\n[%d/%d] %s\n", i+1, len(tasks), label)
			}
			m := meta
			m.rep = rep
			r, out := runOneTask(task, cfgPath, orig, *keep, m)
			results = append(results, r)
			if !*asJSON {
				mark := col(Rd, "✗")
				if r.Passed {
					mark = col(G, "✓")
				}
				fmt.Printf("  %s %s  (%d steps · $%.4f · %.1fs)\n", mark, task.Name, r.Turns, r.CostUSD, r.Seconds)
				if !r.Passed && out != "" {
					fmt.Printf("    %s\n", col(D, firstLine(out, 100)))
				}
				if r.Err != "" {
					fmt.Printf("    %s\n", col(Y, r.Err))
				}
			}
		}
	}

	s := eval.Summarize(results)
	if *asJSON {
		_ = json.NewEncoder(os.Stdout).Encode(struct {
			Results []eval.Result `json:"results"`
			Summary eval.Summary  `json:"summary"`
		}{results, s})
	} else {
		fmt.Printf("\n── eval summary ──────────────────────────\n")
		fmt.Printf("  pass-rate     %s%d/%d (%.0f%%)%s\n", passColor(s), s.Passed, s.Total, s.PassRate*100, R)
		fmt.Printf("  median steps  %d\n", s.MedianTurns)
		fmt.Printf("  median time   %.1fs\n", s.MedianSeconds)
		fmt.Printf("  total cost    $%.4f\n", s.TotalCostUSD)
	}
	if s.Passed < s.Total {
		os.Exit(1) // non-zero so CI / scripts can gate on a regression
	}
}

// cellMeta carries the experiment-cell coordinates stamped onto each result so a
// JSON run is self-describing (which model / profile / server window produced it).
type cellMeta struct {
	model        string
	toolProfile  string
	serverWindow int
	rep          int
}

// runOneTask copies a task to a fresh workspace, runs the prompt through the
// agent there, scores it, and classifies any failure from the event stream.
// Returns the result and the check's failure output.
func runOneTask(task eval.Task, cfgPath, orig string, keep bool, meta cellMeta) (eval.Result, string) {
	ws, err := os.MkdirTemp("", "lumen-eval-")
	if err != nil {
		return eval.Result{Task: task.Name, Err: err.Error()}, ""
	}
	if !keep {
		defer os.RemoveAll(ws)
	}
	if err := eval.CopyDir(task.Workspace, ws); err != nil {
		return eval.Result{Task: task.Name, Err: "copy: " + err.Error()}, ""
	}
	ctr := &evalCounters{}
	coll := &eval.SignalCollector{}
	start := time.Now()
	_ = os.Chdir(ws)
	ctrl := control.New()
	workspaceCtx, workspaceErr := runworkspace.NewLocal("eval", ws, "", nil)
	var cerr error
	if workspaceErr != nil {
		cerr = workspaceErr
	} else {
		cerr = ctrl.ConfigureWithOptions(evalSink(ctr, coll), nil, cfgPath, control.ConfigureOptions{
			Workspace:           workspaceCtx,
			DataRoot:            filepath.Join(ws, ".lumen"),
			ProcessEnvImmutable: true,
		})
	}
	var rerr error
	if cerr == nil {
		rerr = ctrl.Run(context.Background(), task.Prompt)
	}
	cost := ctr.cost(ctrlPricing(ctrl))
	_ = os.Chdir(orig)

	passed, out := eval.Score(context.Background(), ws, task.Check)
	// Anti-cheat: a green check earned by editing/deleting a *_test.go is not a
	// pass — the task said don't modify the tests. Compare against the committed
	// fixture and reject if the protected tests changed.
	tampered := false
	if passed {
		if ok, changed := eval.ProtectedTestsUnchanged(task.Workspace, ws); !ok {
			passed = false
			tampered = true
			out = "rejected: modified protected test file(s): " + strings.Join(changed, ", ")
		}
	}

	r := eval.Result{Task: task.Name, Passed: passed, Turns: ctr.steps, CostUSD: cost, Seconds: time.Since(start).Seconds()}
	switch {
	case cerr != nil:
		r.Err = "configure: " + cerr.Error()
	case rerr != nil:
		r.Err = "run: " + firstLine(rerr.Error(), 80)
	}

	// Build the classification signals from the event stream + post-run state.
	sig := coll.Partial()
	sig.Passed = passed
	sig.TestTampered = tampered
	sig.RunErr = r.Err // lets Classify route configure:/copy: to HarnessBreak (F10)
	sig.FilesChanged = len(eval.ChangedNonTestFiles(task.Workspace, ws))
	// A turn-timeout cancels the context before a clean TurnDone, so it is read
	// from Run's error (checked before stringify) rather than a StopReason event.
	if rerr != nil && errors.Is(rerr, context.DeadlineExceeded) {
		sig.StopReason = "timeout"
	}
	sig.ServerContextWindow = meta.serverWindow

	r.FailureMode = eval.Classify(sig)
	r.FirstPromptTokens = sig.FirstPromptTokens
	r.ToolResultCount = sig.ToolResultCount
	r.FilesChanged = sig.FilesChanged
	r.StopReason = sig.StopReason
	r.Rho = sig.Rho()
	r.Model = meta.model
	if r.Model == "" {
		r.Model = ctrl.ModelName()
	}
	r.ToolProfile = meta.toolProfile
	r.ServerContextWindow = meta.serverWindow
	r.Rep = meta.rep
	return r, out
}

func passColor(s eval.Summary) string {
	if s.Total > 0 && s.Passed == s.Total {
		return G
	}
	if s.PassRate >= 0.5 {
		return Y
	}
	return Rd
}

// evalCounters accumulates per-run usage so the harness can report steps + cost.
type evalCounters struct {
	steps              int
	in, out, hit, miss int
}

func evalSink(ctr *evalCounters, coll *eval.SignalCollector) event.Sink {
	return event.FuncSink(func(e event.Event) {
		coll.Observe(e)
		if e.Kind == event.UsageKind && e.Usage != nil {
			ctr.steps++
			ctr.in += e.Usage.PromptTokens
			ctr.out += e.Usage.CompletionTokens
			ctr.hit += e.Usage.CacheHitTokens
			ctr.miss += e.Usage.CacheMissTokens
		}
	})
}

func (ctr *evalCounters) cost(p *provider.Pricing) float64 {
	return usageCost(p, &event.Usage{
		PromptTokens:     ctr.in,
		CompletionTokens: ctr.out,
		CacheHitTokens:   ctr.hit,
		CacheMissTokens:  ctr.miss,
	})
}

func ctrlPricing(ctrl *control.Controller) *provider.Pricing { return ctrl.Pricing() }

func firstLine(s string, n int) string {
	if i := indexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > n {
		s = s[:n] + "…"
	}
	return s
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
