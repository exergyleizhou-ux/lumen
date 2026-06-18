package editverify

// JS/TS verify steps must be gated on the project's local toolchain, never
// scheduled blindly (npx would auto-fetch from the network).

import (
	"os"
	"path/filepath"
	"testing"
)

// withNodeBin creates a stub project-local tool at root/node_modules/.bin/<tool>
// so the detector resolves it without a real npm install (the registry is
// unreachable here anyway). Existence is all the detector checks.
func withNodeBin(t *testing.T, root, tool string) {
	t.Helper()
	dir := filepath.Join(root, "node_modules", ".bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, tool), "#!/bin/sh\nexit 0\n")
}

// Typecheck must be scheduled only when TypeScript is installed locally AND a
// tsconfig.json exists — otherwise `npx tsc` would auto-download from the network
// (or fail in a plain-JS repo), a false verify failure. It must run the resolved
// binary, never `npx` (which triggers the auto-fetch).
func TestDetect_ts_typecheckGatedOnToolchain(t *testing.T) {
	bare := t.TempDir()
	for _, s := range Detect(bare, []string{"src/app.ts"}, DefaultConfig()) {
		if s.Name == "typecheck" {
			t.Errorf("no TS toolchain/config → must not schedule typecheck, got %v", s.Args)
		}
	}

	proj := t.TempDir()
	withNodeBin(t, proj, "tsc")
	mustWrite(t, filepath.Join(proj, "tsconfig.json"), "{}\n")
	steps := Detect(proj, []string{"src/app.ts"}, DefaultConfig())
	found := false
	for _, s := range steps {
		if s.Name == "typecheck" {
			found = true
			if len(s.Args) == 0 || s.Args[0] == "npx" {
				t.Errorf("typecheck must use the resolved tsc binary, not npx: %v", s.Args)
			}
		}
	}
	if !found {
		t.Errorf("tsc + tsconfig present → expected typecheck step, got %v", stepNames(steps))
	}
}

// jest must be scheduled only when installed locally — never via npx auto-fetch.
func TestDetect_js_jestGatedOnInstall(t *testing.T) {
	bare := t.TempDir()
	for _, s := range Detect(bare, []string{"src/app.js"}, DefaultConfig()) {
		if s.Name == "test" {
			t.Errorf("jest not installed → must not schedule a jest step, got %v", s.Args)
		}
	}

	proj := t.TempDir()
	withNodeBin(t, proj, "jest")
	steps := Detect(proj, []string{"src/app.js"}, DefaultConfig())
	found := false
	for _, s := range steps {
		if s.Name == "test" {
			found = true
			if len(s.Args) == 0 || s.Args[0] == "npx" {
				t.Errorf("jest step must use the resolved binary, not npx: %v", s.Args)
			}
		}
	}
	if !found {
		t.Errorf("jest installed → expected a jest step, got %v", stepNames(steps))
	}
}
