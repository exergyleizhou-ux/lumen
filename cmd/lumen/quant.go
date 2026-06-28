package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"lumen/internal/quant"
)

// runQuant dispatches `lumen quant <init|backtest|verify>` — the verifiable
// A-shares backtest toolchain (sibling to `lumen oasis`).
func runQuant(args []string) {
	if len(args) < 1 {
		quantUsage()
		os.Exit(1)
	}
	switch args[0] {
	case "init":
		rest := args[1:]
		if len(rest) < 1 {
			fmt.Fprintln(os.Stderr, "Usage: lumen quant init <name> [dir]")
			os.Exit(1)
		}
		name := rest[0]
		dir := name
		if len(rest) >= 2 {
			dir = rest[1]
		}
		quantInit(name, dir)
	case "backtest":
		dir, mode := parseQuantRunArgs(args[1:])
		quantBacktest(dir, mode)
	case "verify":
		dir, mode := parseQuantRunArgs(args[1:])
		quantVerify(dir, mode)
	default:
		fmt.Fprintf(os.Stderr, "unknown quant subcommand: %s\n", args[0])
		quantUsage()
		os.Exit(1)
	}
}

func quantUsage() {
	fmt.Fprintln(os.Stderr, "Usage: lumen quant <init|backtest|verify>")
	fmt.Fprintln(os.Stderr, "  init <name> [dir]      scaffold a strategy package")
	fmt.Fprintln(os.Stderr, "  backtest [dir]         run the pinned backtest -> quant-cert.json")
	fmt.Fprintln(os.Stderr, "  verify [dir]           re-run and confirm the cert reproduces")
	fmt.Fprintln(os.Stderr, "\nFlags: --sandbox <docker|local> (default docker)")
}

// parseQuantRunArgs parses `[dir] [--sandbox docker|local]` in any position.
func parseQuantRunArgs(rest []string) (dir string, mode quant.SandboxMode) {
	dir = "."
	mode = quant.SandboxDocker
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		switch {
		case a == "--sandbox" && i+1 < len(rest):
			mode = quant.SandboxMode(rest[i+1])
			i++
		case strings.HasPrefix(a, "--sandbox="):
			mode = quant.SandboxMode(strings.TrimPrefix(a, "--sandbox="))
		case !strings.HasPrefix(a, "--"):
			dir = a
		}
	}
	return dir, mode
}

func quantInit(name, dir string) {
	if _, err := os.Stat(filepath.Join(dir, quant.ManifestFile)); err == nil {
		fmt.Fprintf(os.Stderr, "quant init: %s already exists in %s\n", quant.ManifestFile, dir)
		os.Exit(1)
	}
	if err := quant.ScaffoldStrategy(dir, quant.DefaultManifest(name)); err != nil {
		fmt.Fprintf(os.Stderr, "quant init: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✅ scaffolded strategy %q in %s\n", name, dir)
	fmt.Printf("   Edit strategy.py, then: cd %s && lumen quant backtest .\n", dir)
}

func quantBacktest(dir string, mode quant.SandboxMode) {
	cert, err := quant.RunBacktest(dir, quant.BacktestOptions{Mode: mode})
	if err != nil {
		fmt.Fprintf(os.Stderr, "quant backtest: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("📊 backtest complete — certificate %s\n", cert.ID)
	printMetrics(cert.Metrics)
	fmt.Printf("   data %s · equity %s\n", short12(cert.DataHash), short12(cert.EquityCurveHash))
	fmt.Printf("   Next: lumen quant verify %s\n", dir)
}

func quantVerify(dir string, mode quant.SandboxMode) {
	v, err := quant.VerifyBacktest(dir, quant.BacktestOptions{Mode: mode})
	if err != nil {
		fmt.Fprintf(os.Stderr, "quant verify: %v\n", err)
		os.Exit(1)
	}
	mark := func(ok bool) string {
		if ok {
			return "✓"
		}
		return "✗"
	}
	fmt.Printf("🔎 verifying %s\n", v.CertID)
	fmt.Printf("   %s source matches lock\n", mark(v.SourceMatch))
	fmt.Printf("   %s certificate self-consistent\n", mark(v.CertSelfValid))
	fmt.Printf("   %s backtest reproduces (equity %s)\n", mark(v.Reproduces), short12(v.CurrentEquity))
	if v.OK() {
		fmt.Printf("✅ %s VERIFIED — the backtest is reproducible and untampered\n", v.CertID)
		return
	}
	fmt.Fprintln(os.Stderr, "❌ verification FAILED — the cert does not match this strategy/data")
	os.Exit(1)
}

func printMetrics(m map[string]float64) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%.4f", k, m[k]))
	}
	fmt.Printf("   %s\n", strings.Join(parts, "  "))
}
