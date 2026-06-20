package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"lumen/internal/control"
	"lumen/internal/eval"
	"lumen/internal/event"
	"lumen/internal/provider"
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
	_ = fs.Parse(args)

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
		fmt.Printf("\n[%d/%d] %s\n", i+1, len(tasks), task.Name)
		ws, err := os.MkdirTemp("", "lumen-eval-")
		if err != nil {
			results = append(results, eval.Result{Task: task.Name, Err: err.Error()})
			continue
		}
		if !*keep {
			defer os.RemoveAll(ws)
		}
		if err := eval.CopyDir(task.Workspace, ws); err != nil {
			results = append(results, eval.Result{Task: task.Name, Err: "copy: " + err.Error()})
			continue
		}

		ctr := &evalCounters{}
		start := time.Now()
		_ = os.Chdir(ws)
		ctrl := control.New()
		cerr := ctrl.Configure(evalSink(ctr), nil, cfgPath)
		var rerr error
		if cerr == nil {
			rerr = ctrl.Run(context.Background(), task.Prompt)
		}
		cost := ctr.cost(ctrlPricing(ctrl))
		_ = os.Chdir(orig)

		passed, out := eval.Score(context.Background(), ws, task.Check)
		r := eval.Result{Task: task.Name, Passed: passed, Turns: ctr.steps, CostUSD: cost, Seconds: time.Since(start).Seconds()}
		switch {
		case cerr != nil:
			r.Err = "configure: " + cerr.Error()
		case rerr != nil:
			r.Err = "run: " + firstLine(rerr.Error(), 80)
		}
		results = append(results, r)

		mark := col(Rd, "✗")
		if passed {
			mark = col(G, "✓")
		}
		fmt.Printf("  %s %s  (%d steps · $%.4f · %.1fs)\n", mark, task.Name, r.Turns, r.CostUSD, r.Seconds)
		if !passed && out != "" {
			fmt.Printf("    %s\n", col(D, firstLine(out, 100)))
		}
		if r.Err != "" {
			fmt.Printf("    %s\n", col(Y, r.Err))
		}
	}

	s := eval.Summarize(results)
	fmt.Printf("\n── eval summary ──────────────────────────\n")
	fmt.Printf("  pass-rate    %s%d/%d (%.0f%%)%s\n", passColor(s), s.Passed, s.Total, s.PassRate*100, R)
	fmt.Printf("  median steps %d\n", s.MedianTurns)
	fmt.Printf("  total cost   $%.4f\n", s.TotalCostUSD)
	if s.Passed < s.Total {
		os.Exit(1) // non-zero so CI / scripts can gate on a regression
	}
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

func evalSink(ctr *evalCounters) event.Sink {
	return event.FuncSink(func(e event.Event) {
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
