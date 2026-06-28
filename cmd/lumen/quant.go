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
	case "data":
		dir, _ := parseQuantRunArgs(args[1:])
		quantData(dir)
	case "backtest":
		dir, mode := parseQuantRunArgs(args[1:])
		quantBacktest(dir, mode)
	case "verify":
		dir, mode := parseQuantRunArgs(args[1:])
		quantVerify(dir, mode)
	case "keygen":
		dir, _ := parseQuantRunArgs(args[1:])
		quantKeygen(quantKeyPath(args[1:]))
		_ = dir
	case "attest":
		dir, mode := parseQuantRunArgs(args[1:])
		quantAttest(dir, mode, quantKeyPath(args[1:]))
	case "verify-attestation":
		dir, _ := parseQuantRunArgs(args[1:])
		quantVerifyAttestation(dir)
	default:
		fmt.Fprintf(os.Stderr, "unknown quant subcommand: %s\n", args[0])
		quantUsage()
		os.Exit(1)
	}
}

func quantUsage() {
	fmt.Fprintln(os.Stderr, "Usage: lumen quant <init|data|backtest|verify|keygen|attest|verify-attestation>")
	fmt.Fprintln(os.Stderr, "  init <name> [dir]      scaffold a strategy package")
	fmt.Fprintln(os.Stderr, "  data [dir]             fetch the manifest's universe -> data.csv (akshare)")
	fmt.Fprintln(os.Stderr, "  backtest [dir]         run the pinned backtest -> quant-cert.json")
	fmt.Fprintln(os.Stderr, "  verify [dir]           re-run and confirm the cert reproduces")
	fmt.Fprintln(os.Stderr, "  keygen                 create/show the verifier's Ed25519 identity")
	fmt.Fprintln(os.Stderr, "  attest [dir]           verifier role: re-run, then sign the cert (B1)")
	fmt.Fprintln(os.Stderr, "  verify-attestation [dir]  check a signed attestation offline")
	fmt.Fprintln(os.Stderr, "\nFlags: --sandbox <docker|local> (default docker), --key <path>")
}

// quantKeyPath extracts --key <path> (or --key=path), defaulting to the user
// config dir.
func quantKeyPath(rest []string) string {
	for i := 0; i < len(rest); i++ {
		if rest[i] == "--key" && i+1 < len(rest) {
			return rest[i+1]
		}
		if strings.HasPrefix(rest[i], "--key=") {
			return strings.TrimPrefix(rest[i], "--key=")
		}
	}
	d, err := os.UserConfigDir()
	if err != nil {
		return "quant-verifier.json"
	}
	return filepath.Join(d, "lumen", "quant-verifier.json")
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
		case a == "--key" && i+1 < len(rest):
			i++ // value belongs to --key; don't treat it as dir
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

func quantData(dir string) {
	lock, err := quant.RunDataFetch(dir, "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "quant data: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("📥 fetched %s [%s..%s] from %s\n", lock.Universe, lock.Start, lock.End, lock.Source)
	fmt.Printf("   data.csv pinned · sha256 %s\n", short12(lock.FileSHA256))
	fmt.Printf("   Next: lumen quant backtest %s\n", dir)
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

func quantKeygen(keyPath string) {
	kp, err := quant.LoadOrCreateVerifierKey(keyPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "quant keygen: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("🔑 verifier identity %s\n", kp.KeyID())
	fmt.Printf("   key file: %s\n", keyPath)
	fmt.Printf("   share this key id with buyers so they can trust your attestations.\n")
}

func quantAttest(dir string, mode quant.SandboxMode, keyPath string) {
	kp, err := quant.LoadOrCreateVerifierKey(keyPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "quant attest: %v\n", err)
		os.Exit(1)
	}
	att, err := quant.Attest(dir, kp, quant.BacktestOptions{Mode: mode})
	if err != nil {
		fmt.Fprintf(os.Stderr, "quant attest: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("📜 attested %s — re-ran and signed by verifier %s\n", att.CertID, att.VerifierKeyID)
	printMetrics(att.Metrics)
	fmt.Printf("   wrote %s · anyone can check it: lumen quant verify-attestation %s\n", quant.AttestFile, dir)
}

func quantVerifyAttestation(dir string) {
	res, err := quant.CheckAttestation(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "quant verify-attestation: %v\n", err)
		os.Exit(1)
	}
	mark := func(ok bool) string {
		if ok {
			return "✓"
		}
		return "✗"
	}
	fmt.Printf("🔏 checking attestation for %s\n", res.CertID)
	fmt.Printf("   %s verifier signature valid\n", mark(res.SignatureValid))
	if res.CertPresent {
		fmt.Printf("   %s attestation matches the local certificate\n", mark(res.MatchesCert))
		fmt.Printf("   %s source unchanged since attestation\n", mark(res.SourceMatch))
	}
	if res.OK() {
		fmt.Printf("✅ ATTESTATION VALID — verifier vouches this backtest was independently reproduced\n")
		return
	}
	fmt.Fprintln(os.Stderr, "❌ ATTESTATION INVALID — do not trust these numbers")
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
