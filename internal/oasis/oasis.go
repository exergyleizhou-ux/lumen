// Package oasis implements the C2D (Compute-to-Data) author toolchain — the
// P4 "Oasis Author CLI". Algorithm developers use `lumen oasis init|validate|
// build|deploy` to scaffold, check, package, and register C2D algorithms with
// the ai-data-marketplace.
//
// Contract: follows the backend compute module schema (Algorithm type) from
// ai-data-marketplace/backend/internal/modules/compute/model.go.
package oasis

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ── Algorithm Manifest (the C2D contract) ─────────────────

// Manifest is the oasis.toml file that every C2D algorithm must have.
// It maps 1:1 to the marketplace's compute.Algo type.
type Manifest struct {
	// Required
	Name     string `json:"name" toml:"name"`
	Runtime  string `json:"runtime" toml:"runtime"` // "docker" | "wasm" | "tee"
	Image    string `json:"image" toml:"image"`
	Digest   string `json:"image_digest" toml:"image_digest"`

	// Optional
	Entrypoint   string `json:"entrypoint" toml:"entrypoint"` // container entrypoint override
	OutputKind   string `json:"output_kind" toml:"output_kind"` // "model" | "metrics" | "report" | "bytes"
	Version      int    `json:"version" toml:"version"`
	SourceRef    string `json:"source_ref" toml:"source_ref"` // git repo URL
	ParamsSchema string `json:"params_schema" toml:"params_schema"` // JSON schema

	// Build
	Builder   string `json:"builder" toml:"builder"` // "docker" (default) | "kaniko" | "buildpacks"
	Dockerfile string `json:"dockerfile" toml:"dockerfile"` // path, default "Dockerfile"
}

// DefaultManifest returns a template manifest for new algorithms.
func DefaultManifest(name string) Manifest {
	return Manifest{
		Name:       name,
		Runtime:    "docker",
		Image:      fmt.Sprintf("registry.example.com/algo/%s:latest", name),
		OutputKind: "model",
		Version:    1,
		Builder:    "docker",
		Dockerfile: "Dockerfile",
		ParamsSchema: `{"n_estimators":{"type":"integer","default":100}}`,
	}
}

// ── Validation ─────────────────────────────────────────────

// validRuntimes is the set of supported compute runtimes.
var validRuntimes = map[string]bool{"docker": true, "wasm": true, "tee": true}
var validOutputKinds = map[string]bool{"model": true, "metrics": true, "report": true, "bytes": true}

// Validate checks a manifest for C2D contract compliance.
func Validate(m Manifest) []string {
	var errs []string
	if m.Name == "" {
		errs = append(errs, "name is required")
	}
	if m.Image == "" {
		errs = append(errs, "image is required")
	}
	if !validRuntimes[m.Runtime] {
		errs = append(errs, fmt.Sprintf("runtime %q must be one of: docker, wasm, tee", m.Runtime))
	}
	if m.Runtime == "" {
		errs = append(errs, "runtime is required")
	}
	if !validOutputKinds[m.OutputKind] {
		errs = append(errs, fmt.Sprintf("output_kind %q must be one of: model, metrics, report, bytes", m.OutputKind))
	}
	if m.Dockerfile == "" {
		m.Dockerfile = "Dockerfile"
	}
	if m.Builder == "" {
		m.Builder = "docker"
	}
	return errs
}

// ── Scaffolding (init) ─────────────────────────────────────

// Scaffold creates a new algorithm directory from the manifest template.
func Scaffold(dir string, m Manifest) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	// Write oasis.toml
	toml := formatTOML(m)
	manifestPath := filepath.Join(dir, "oasis.toml")
	if err := os.WriteFile(manifestPath, []byte(toml), 0644); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}

	// Write Dockerfile template
	dockerfile := fmt.Sprintf(`# Oasis C2D algorithm: %s
FROM golang:1.23-alpine AS builder
WORKDIR /build
COPY . .
RUN go build -o /algo ./cmd/algo

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=builder /algo /algo
ENTRYPOINT ["/algo"]
`, m.Name)
	dockerfilePath := filepath.Join(dir, m.Dockerfile)
	if err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0644); err != nil {
		return fmt.Errorf("write dockerfile: %w", err)
	}

	// Write main.go template
	mainDir := filepath.Join(dir, "cmd", "algo")
	if err := os.MkdirAll(mainDir, 0755); err != nil {
		return fmt.Errorf("mkdir cmd/algo: %w", err)
	}
	mainGo := fmt.Sprintf(`package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// C2D algorithm contract (enforced by the Oasis runner; check with
// 'lumen oasis check'):
//   - the dataset is mounted READ-ONLY at /data
//   - the runner provides params at /out/input.json
//   - write your JSON result to STDOUT — the runner saves it as /out/output.json,
//     then hashes + Ed25519-attests it
//   - the container runs with --network none --read-only (no internet, ro root)
func main() {
	datasetPath := "/data/dataset.csv"
	var params map[string]interface{}
	if data, err := os.ReadFile("/out/input.json"); err == nil {
		var input map[string]interface{}
		if json.Unmarshal(data, &input) == nil {
			if dp, ok := input["dataset_path"].(string); ok && dp != "" {
				datasetPath = dp
			}
			if p, ok := input["params"].(map[string]interface{}); ok {
				params = p
			}
		}
	}
	_ = datasetPath // TODO: read your dataset from here (under /data, read-only)
	_ = params      // TODO: apply params (e.g. n_estimators, learning_rate)

	// TODO: replace with your real result.
	result := map[string]interface{}{
		"algorithm": "%s",
		"status":    "ok",
	}

	// Write the result to stdout — the runner captures it as /out/output.json.
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: encode output: %%v\n", err)
		os.Exit(1)
	}
}
`, m.Name)
	mainGoPath := filepath.Join(mainDir, "main.go")
	if err := os.WriteFile(mainGoPath, []byte(mainGo), 0644); err != nil {
		return fmt.Errorf("write main.go: %w", err)
	}

	// Write .gitignore
	gitignorePath := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte("/algo\n/oasis-lock.json\n"), 0644); err != nil {
		return fmt.Errorf("write .gitignore: %w", err)
	}

	return nil
}

// ── Lockfile ───────────────────────────────────────────────

// Lock holds the build provenance: which image+digest was produced from which source.
type Lock struct {
	Manifest  Manifest `json:"manifest"`
	BuiltAt   string   `json:"built_at"`
	Image     string   `json:"image"`
	Digest    string   `json:"image_digest"`
	SrcHash   string   `json:"source_sha256"`
}

// ComputeSrcHash computes the SHA-256 hash of all Go source files in dir
// (not including vendor/ or hidden directories).
func ComputeSrcHash(dir string) (string, error) {
	h := sha256.New()
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		name := info.Name()
		if info.IsDir() {
			if name == ".git" || name == "vendor" || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		io.Copy(h, f)
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ── Helpers ────────────────────────────────────────────────

func formatTOML(m Manifest) string {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Sprintf("# oasis.toml — %s\n", m.Name)
	}
	var sb strings.Builder
	sb.WriteString("# oasis.toml — C2D algorithm manifest\n")
	sb.WriteString("# See docs/superpowers/specs/P4-oasis-c2d.md\n\n")
	var m2 map[string]interface{}
	json.Unmarshal(b, &m2)
	for k, v := range m2 {
		sb.WriteString(fmt.Sprintf("%s = %v\n", k, formatValue(v)))
	}
	return sb.String()
}

func formatValue(v interface{}) string {
	switch vv := v.(type) {
	case string:
		return fmt.Sprintf("%q", vv)
	case float64:
		if vv == float64(int(vv)) {
			return fmt.Sprintf("%d", int(vv))
		}
		return fmt.Sprintf("%v", vv)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// ParseManifest parses an oasis.toml file into a Manifest.
func ParseManifest(raw string) (Manifest, error) {
	m := DefaultManifest("")
	lines := strings.Split(raw, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		val = strings.Trim(val, "\"")
		switch key {
		case "name":
			m.Name = val
		case "runtime":
			m.Runtime = val
		case "image":
			m.Image = val
		case "image_digest":
			m.Digest = val
		case "entrypoint":
			m.Entrypoint = val
		case "output_kind":
			m.OutputKind = val
		case "version":
			fmt.Sscanf(val, "%d", &m.Version)
		case "source_ref":
			m.SourceRef = val
		case "params_schema":
			m.ParamsSchema = val
		case "builder":
			m.Builder = val
		case "dockerfile":
			m.Dockerfile = val
		}
	}
	return m, nil
}
