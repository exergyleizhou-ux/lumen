package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"lumen/internal/modeleval"
)

func runModelEval(args []string) {
	fs := flag.NewFlagSet("model-eval", flag.ExitOnError)
	tasksPath := fs.String("tasks", "evals/production/tasks.json", "production task manifest")
	recordedPath := fs.String("recorded", "evals/production/recorded.json", "controlled recording")
	live := fs.Bool("live", false, "call a real model (also requires LUMEN_MODEL_EVAL_LIVE=1)")
	adapterName := fs.String("adapter", "qwen", "live Chinese model adapter: qwen or deepseek")
	inputPrice := fs.Int64("input-price-micros-per-million", 0, "provider input price in integer micro-USD per million tokens")
	outputPrice := fs.Int64("output-price-micros-per-million", 0, "provider output price in integer micro-USD per million tokens")
	outPath := fs.String("out", "", "write report to path (stdout when empty)")
	_ = fs.Parse(args)
	tasks, err := modeleval.LoadTasks(*tasksPath)
	if err != nil {
		fatalModelEval(err)
	}
	mode, model := "recorded", "controlled-v1"
	var runner modeleval.Runner
	if *live {
		if os.Getenv("LUMEN_MODEL_EVAL_LIVE") != "1" {
			fatalModelEval(fmt.Errorf("live evaluation disabled: set LUMEN_MODEL_EVAL_LIVE=1 explicitly"))
		}
		if *inputPrice <= 0 || *outputPrice <= 0 {
			fatalModelEval(fmt.Errorf("live evaluation requires explicit positive integer input/output prices"))
		}
		a, e := modeleval.SelectAdapter(*adapterName)
		if e != nil {
			fatalModelEval(e)
		}
		a.InputMicrosPerMTok = *inputPrice
		a.OutputMicrosPerMTok = *outputPrice
		mode = "live"
		model = a.Model
		runner = modeleval.LiveRunner{Adapter: a}
	} else {
		rows, e := modeleval.LoadRecorded(*recordedPath)
		if e != nil {
			fatalModelEval(e)
		}
		runner = modeleval.RecordedRunner{Rows: rows}
	}
	rep, err := modeleval.Evaluate(context.Background(), mode, model, tasks, runner, time.Now())
	if err != nil {
		fatalModelEval(err)
	}
	b, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		fatalModelEval(err)
	}
	b = append(b, '\n')
	if *outPath != "" {
		if err := os.WriteFile(*outPath, b, 0644); err != nil {
			fatalModelEval(err)
		}
	} else {
		_, _ = os.Stdout.Write(b)
	}
	if rep.Metrics.NetworkFailures > 0 || rep.Metrics.CodeFailures > 0 || rep.Metrics.SuccessRate < 1 {
		os.Exit(1)
	}
}

func fatalModelEval(err error) {
	fmt.Fprintln(os.Stderr, "model-eval:", strings.TrimSpace(err.Error()))
	os.Exit(2)
}
