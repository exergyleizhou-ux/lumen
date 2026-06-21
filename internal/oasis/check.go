package oasis

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// sentinelDataset is the synthetic dataset the contract self-check seeds. The
// numeric columns (v, w, x) exercise the algorithm; the non-numeric _sentinel
// column carries a unique recognizable token per row. An aggregates-only C2D
// algorithm never emits those tokens, so echoing them back reveals a raw-row leak.
func sentinelDataset() ([]byte, []string) {
	var b strings.Builder
	b.WriteString("v,w,x,_sentinel\n")
	var sentinels []string
	for i := 0; i < 30; i++ {
		s := fmt.Sprintf("ZqLeakTok%04d", i)
		sentinels = append(sentinels, s)
		fmt.Fprintf(&b, "%d,%d,%d,%s\n", i+1, (i*7)%50+1, (i*3)%40+2, s)
	}
	return []byte(b.String()), sentinels
}

// leakCount returns how many per-row sentinel tokens appear verbatim in the
// output (a zip of model.json + metrics.json).
func leakCount(outputBin []byte, sentinels []string) int {
	zr, err := zip.NewReader(bytes.NewReader(outputBin), int64(len(outputBin)))
	if err != nil {
		return 0
	}
	var text strings.Builder
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			continue
		}
		bs, _ := io.ReadAll(rc)
		rc.Close()
		text.Write(bs)
	}
	s := text.String()
	n := 0
	for _, sen := range sentinels {
		if strings.Contains(s, sen) {
			n++
		}
	}
	return n
}

// ── C2D runtime contract self-check ────────────────────────
//
// The marketplace runner executes an algorithm image with strict isolation
// (`docker run --network none --read-only -v <data>:/data:ro -v <params>:/params.json:ro -v <out>:/out`),
// mounts the params at /params.json, and reads **/out/output.bin** as the single
// result object — which it then hashes + Ed25519-attests. So a contract-compliant
// algorithm must: read its dataset from /data and params from /params.json, run
// with no network and a read-only root, and write its result to /out/output.bin.
// For a "model" output_kind that file is a zip of model.json (+ metrics.json).
// CheckContract runs the image exactly that way and validates the result, so
// authors catch violations before pushing to Oasis.

// CheckOutput validates the bytes of /out/output.bin against the contract: it
// must be non-empty (the runner reads exactly this file). For output_kind
// "model" it must be a zip archive containing model.json. Returns violations
// (empty = OK).
func CheckOutput(data []byte, kind string) []string {
	if len(data) == 0 {
		return []string{"output is empty — the algorithm must write its result to /out/output.bin (the runner reads exactly that file)"}
	}
	if kind == "model" {
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return []string{`output_kind "model" expects /out/output.bin to be a zip of model.json (+ metrics.json), but it is not a valid zip`}
		}
		has := map[string]bool{}
		for _, f := range zr.File {
			has[f.Name] = true
		}
		if !has["model.json"] {
			return []string{`model output.bin (zip) must contain model.json`}
		}
	}
	return nil
}

// SandboxRunner runs an algorithm image under the C2D isolation contract; the
// algorithm writes its result to <outDir>/output.bin. Abstracted so the
// self-check can be unit-tested without Docker.
type SandboxRunner interface {
	Run(ctx context.Context, image, dataDir, paramsFile, outDir string) (logs string, err error)
}

// dockerSandbox runs the image exactly as the marketplace runner does.
type dockerSandbox struct{}

func (dockerSandbox) Run(ctx context.Context, image, dataDir, paramsFile, outDir string) (string, error) {
	return runDockerSandbox(ctx, image, dataDir, paramsFile, outDir)
}

// runDockerSandbox runs the image with exactly the marketplace runner's
// isolation: no network, read-only root, dataset read-only, /out writable.
func runDockerSandbox(ctx context.Context, image, dataDir, paramsFile, outDir string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm",
		"--network", "none",
		"--read-only",
		"--tmpfs", "/tmp",
		"-v", dataDir+":/data:ro",
		"-v", paramsFile+":/params.json:ro",
		"-v", outDir+":/out",
		image,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stderr.String(), err
}

// CheckResult is the outcome of a contract self-check.
type CheckResult struct {
	OK         bool
	Violations []string
	Logs       string
}

// DockerAvailable reports whether the docker CLI is on PATH.
func DockerAvailable() bool {
	_, err := exec.LookPath("docker")
	return err == nil
}

// RunContractCheck runs the contract self-check against the real Docker sandbox.
func RunContractCheck(ctx context.Context, image, kind string, sampleData []byte) CheckResult {
	return CheckContract(ctx, dockerSandbox{}, image, kind, sampleData)
}

// CheckContract runs the algorithm image under the real C2D isolation against a
// synthetic dataset and validates that it produces a contract-compliant
// /out/output.bin. run is the sandbox (use dockerSandbox{} in production, a fake
// in tests). sampleData seeds /data; kind is the declared output_kind.
func CheckContract(ctx context.Context, run SandboxRunner, image, kind string, sampleData []byte) CheckResult {
	dataDir, err := os.MkdirTemp("", "oasis-data-*")
	if err != nil {
		return CheckResult{Violations: []string{fmt.Sprintf("could not create scratch data dir: %v", err)}}
	}
	defer os.RemoveAll(dataDir)
	outDir, err := os.MkdirTemp("", "oasis-out-*")
	if err != nil {
		return CheckResult{Violations: []string{fmt.Sprintf("could not create scratch out dir: %v", err)}}
	}
	defer os.RemoveAll(outDir)

	// Seed a sample dataset (/data) and the params the runner mounts (/params.json).
	// With no author-supplied data, seed the sentinel dataset so we can also run the
	// privacy leak lint below.
	var sentinels []string
	if len(sampleData) == 0 {
		sampleData, sentinels = sentinelDataset()
	}
	if err := os.WriteFile(filepath.Join(dataDir, "dataset.csv"), sampleData, 0o644); err != nil {
		return CheckResult{Violations: []string{fmt.Sprintf("could not seed dataset: %v", err)}}
	}
	paramsDir, err := os.MkdirTemp("", "oasis-params-*")
	if err != nil {
		return CheckResult{Violations: []string{fmt.Sprintf("could not create scratch params dir: %v", err)}}
	}
	defer os.RemoveAll(paramsDir)
	paramsFile := filepath.Join(paramsDir, "params.json")
	if err := os.WriteFile(paramsFile, []byte(`{}`), 0o644); err != nil {
		return CheckResult{Violations: []string{fmt.Sprintf("could not seed params.json: %v", err)}}
	}

	logs, err := run.Run(ctx, image, dataDir, paramsFile, outDir)
	if err != nil {
		return CheckResult{
			Violations: []string{fmt.Sprintf("algorithm did not run cleanly under `--network none --read-only`: %v", err)},
			Logs:       logs,
		}
	}

	// The runner reads exactly /out/output.bin — so do we.
	out, rerr := os.ReadFile(filepath.Join(outDir, "output.bin"))
	if rerr != nil {
		return CheckResult{
			Violations: []string{"the algorithm did not write /out/output.bin — that is the single result file the runner reads"},
			Logs:       logs,
		}
	}

	violations := CheckOutput(out, kind)
	// Privacy lint: on the synthetic dataset, an output that echoes a majority of
	// the per-row sentinel tokens is dumping raw rows, not emitting aggregates.
	if len(sentinels) > 0 {
		if n := leakCount(out, sentinels); n*2 > len(sentinels) {
			violations = append(violations, fmt.Sprintf(
				"possible PRIVACY LEAK: the output echoes %d/%d seeded per-row values — a C2D algorithm must emit only aggregates, never raw rows",
				n, len(sentinels)))
		}
	}
	return CheckResult{OK: len(violations) == 0, Violations: violations, Logs: logs}
}
