package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"lumen/internal/config"
	"lumen/internal/localprobe"
)

// runProbeLocal checks whether local model endpoints can actually drive the
// agent loop — the decisive test being whether the model emits an OpenAI
// tool_call (not just chat prose). It prints a capability matrix the user can
// paste into docs/local-models.md.
//
// This lives outside `lumen doctor` on purpose: doctor is owned by a different
// workstream, and this probe is a self-contained, model-gated check.
//
// Usage:
//
//	lumen probe-local                 # probe every built-in local preset
//	lumen probe-local --base-url URL  # probe one ad-hoc endpoint
//	lumen probe-local --json          # machine-readable output
func runProbeLocal(args []string) {
	fs := flag.NewFlagSet("probe-local", flag.ExitOnError)
	baseURL := fs.String("base-url", "", "probe a single OpenAI-compatible endpoint instead of the built-in local presets")
	model := fs.String("model", "", "served model id (default: auto-discover via /v1/models)")
	apiKey := fs.String("api-key", "", "API key (local endpoints accept any/empty value)")
	asJSON := fs.Bool("json", false, "emit JSON instead of a markdown matrix")
	timeout := fs.Duration("timeout", 90*time.Second, "per-endpoint probe timeout")
	_ = fs.Parse(args)

	var configs []localprobe.Config
	if *baseURL != "" {
		configs = append(configs, localprobe.Config{
			Label:   *baseURL,
			BaseURL: *baseURL,
			Model:   *model,
			APIKey:  *apiKey,
			Timeout: *timeout,
		})
	} else {
		for _, p := range config.LocalPresets() {
			configs = append(configs, localprobe.Config{
				Label:   p.Name,
				BaseURL: p.BaseURL,
				Model:   *model, // empty → auto-discover per endpoint
				APIKey:  *apiKey,
				Timeout: *timeout,
			})
		}
	}

	if len(configs) == 0 {
		fmt.Fprintln(os.Stderr, "probe-local: no local presets to probe")
		os.Exit(1)
	}

	fmt.Fprintln(os.Stderr, "Probing local endpoints (read file → edit one line via tool call)…")
	results := make([]localprobe.Result, 0, len(configs))
	for _, c := range configs {
		fmt.Fprintf(os.Stderr, "  • %s (%s)… ", c.Label, c.BaseURL)
		r := localprobe.Probe(context.Background(), c)
		switch {
		case r.Err != nil:
			fmt.Fprintln(os.Stderr, "unreachable")
		case r.CanToolCall:
			fmt.Fprintf(os.Stderr, "✅ drives agent (%.1f tok/s)\n", r.TokensPerSec)
		default:
			fmt.Fprintln(os.Stderr, "❌ prose only")
		}
		results = append(results, r)
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(results); err != nil {
			fmt.Fprintln(os.Stderr, "probe-local: encode:", err)
			os.Exit(1)
		}
		return
	}

	fmt.Println()
	fmt.Print(localprobe.FormatMarkdown(results))
}
