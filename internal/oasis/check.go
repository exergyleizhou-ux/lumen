package oasis

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// ── C2D runtime contract self-check ────────────────────────
//
// The marketplace runner executes an algorithm image with strict isolation
// (`docker run --network none --read-only -v <data>:/data:ro -v <out>:/out`),
// pre-writes the params to /out/input.json, and captures the container's STDOUT
// as /out/output.json — which it then hashes + Ed25519-attests. So a
// contract-compliant algorithm must: read its dataset from /data and params from
// /out/input.json, run with no network and a read-only root, and write a valid
// JSON document to stdout. CheckContract runs the image exactly that way and
// validates the result, so authors catch violations before pushing to Oasis.

// CheckOutput validates an algorithm's output.json against the contract: it must
// be non-empty, valid JSON. For the structured kinds (model/metrics/report) the
// top level must be a JSON object. Returns a list of violations (empty = OK).
func CheckOutput(data []byte, kind string) []string {
	if len(bytes.TrimSpace(data)) == 0 {
		return []string{"output is empty — the algorithm must write a JSON result to stdout (the runner saves it as /out/output.json)"}
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return []string{fmt.Sprintf("output is not valid JSON: %v", err)}
	}
	switch kind {
	case "model", "metrics", "report":
		if _, ok := v.(map[string]any); !ok {
			return []string{fmt.Sprintf("output_kind %q expects a JSON object at the top level", kind)}
		}
	}
	return nil
}

// SandboxRunner runs an algorithm image under the C2D isolation contract and
// returns the captured stdout (the would-be output.json) and combined logs.
// Abstracted so the self-check can be unit-tested without Docker.
type SandboxRunner interface {
	Run(ctx context.Context, image, dataDir, outDir string) (stdout []byte, logs string, err error)
}

// dockerSandbox runs the image exactly as the marketplace runner does.
type dockerSandbox struct{}

func (dockerSandbox) Run(ctx context.Context, image, dataDir, outDir string) ([]byte, string, error) {
	return runDockerSandbox(ctx, image, dataDir, outDir)
}

// runDockerSandbox runs the image with exactly the marketplace runner's
// isolation: no network, read-only root, dataset read-only, /out writable.
func runDockerSandbox(ctx context.Context, image, dataDir, outDir string) ([]byte, string, error) {
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm",
		"--network", "none",
		"--read-only",
		"--tmpfs", "/tmp",
		"-v", dataDir+":/data:ro",
		"-v", outDir+":/out",
		image,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.String(), err
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
// output.json. run is the sandbox (use dockerSandbox{} in production, a fake in
// tests). sampleData seeds /data; kind is the declared output_kind.
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

	stdout, logs, err := run.Run(ctx, image, dataDir, outDir)
	if err != nil {
		return CheckResult{
			Violations: []string{fmt.Sprintf("algorithm did not run cleanly under `--network none --read-only`: %v", err)},
			Logs:       logs,
		}
	}

	// Prefer an explicitly written /out/output.json; fall back to stdout (the
	// runner captures stdout when the container writes nothing to /out).
	out := stdout
	if data, rerr := os.ReadFile(filepath.Join(outDir, "output.json")); rerr == nil && len(bytes.TrimSpace(data)) > 0 {
		out = data
	}

	violations := CheckOutput(out, kind)
	return CheckResult{OK: len(violations) == 0, Violations: violations, Logs: logs}
}
