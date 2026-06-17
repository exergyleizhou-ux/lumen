package oasis

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// ── C2D runtime contract self-check ────────────────────────
//
// The marketplace runner executes an algorithm image with strict isolation
// (`docker run --network none --read-only -v <data>:/data:ro -v <out>:/out`),
// pre-writes the params to /out/input.json, and reads **/out/output.bin** as the
// single result object — which it then hashes + Ed25519-attests. So a
// contract-compliant algorithm must: read its dataset from /data and params from
// /out/input.json, run with no network and a read-only root, and write its result
// to /out/output.bin. For a "model" output_kind that file is a zip of model.json
// (+ metrics.json). CheckContract runs the image exactly that way and validates
// the result, so authors catch violations before pushing to Oasis.

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
	Run(ctx context.Context, image, dataDir, outDir string) (logs string, err error)
}

// dockerSandbox runs the image exactly as the marketplace runner does.
type dockerSandbox struct{}

func (dockerSandbox) Run(ctx context.Context, image, dataDir, outDir string) (string, error) {
	return runDockerSandbox(ctx, image, dataDir, outDir)
}

// runDockerSandbox runs the image with exactly the marketplace runner's
// isolation: no network, read-only root, dataset read-only, /out writable.
func runDockerSandbox(ctx context.Context, image, dataDir, outDir string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm",
		"--network", "none",
		"--read-only",
		"--tmpfs", "/tmp",
		"-v", dataDir+":/data:ro",
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

	// Seed a sample dataset and the params the runner provides via /out/input.json.
	if len(sampleData) == 0 {
		sampleData = []byte("col_a,col_b\n1,2\n3,4\n")
	}
	if err := os.WriteFile(filepath.Join(dataDir, "dataset.csv"), sampleData, 0o644); err != nil {
		return CheckResult{Violations: []string{fmt.Sprintf("could not seed dataset: %v", err)}}
	}
	if err := os.WriteFile(filepath.Join(outDir, "input.json"), []byte(`{"dataset_path":"/data/dataset.csv","params":{}}`), 0o644); err != nil {
		return CheckResult{Violations: []string{fmt.Sprintf("could not seed input.json: %v", err)}}
	}

	logs, err := run.Run(ctx, image, dataDir, outDir)
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
	return CheckResult{OK: len(violations) == 0, Violations: violations, Logs: logs}
}
