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
	"strconv"
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
		Image:        fmt.Sprintf("registry.example.com/algo/%s", name),
		OutputKind:   "model",
		Version:      1,
		Builder:      "docker",
		Dockerfile:   "Dockerfile",
		ParamsSchema: `{}`,
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
	dockerfile := `FROM python:3.11-slim
COPY train.py /app/train.py
WORKDIR /app
USER 65534:65534
ENTRYPOINT ["python", "/app/train.py"]
`
	dockerfilePath := filepath.Join(dir, m.Dockerfile)
	if err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0644); err != nil {
		return fmt.Errorf("write dockerfile: %w", err)
	}

	// Write train.py — pure-Python-stdlib C2D algorithm skeleton
	trainPy := fmt.Sprintf(`#!/usr/bin/env python3
"""C2D algorithm: %s — pure-Python-stdlib skeleton.

Container contract: read /data (the dataset, read-only), write /out/output.bin
(a zip of model.json + metrics.json). Paths overridable via env for testing.
Optional /params.json for hyper-parameters.
"""
import csv
import io
import json
import os
import sys
import zipfile

DATA_DIR = os.environ.get("VO_DATA_DIR", "/data")
OUT_DIR = os.environ.get("VO_OUT_DIR", "/out")
PARAMS_FILE = os.environ.get("VO_PARAMS", "/params.json")


def log(stage, **kw):
    """Structured progress log — counts/metrics only, never raw data."""
    print(json.dumps({"stage": stage, **kw}), flush=True)


def die(reason, code=2):
    log("error", reason=reason)
    sys.exit(code)


def load_params():
    if os.path.exists(PARAMS_FILE):
        try:
            with open(PARAMS_FILE) as f:
                return json.load(f) or {}
        except (OSError, ValueError):
            return {}
    return {}


def find_input():
    if not os.path.isdir(DATA_DIR):
        die("no_data_dir")
    names = sorted(os.listdir(DATA_DIR))
    for n in names:
        if n.lower().endswith((".csv", ".tsv")):
            return os.path.join(DATA_DIR, n)
    if names:
        return os.path.join(DATA_DIR, names[0])
    die("no_input_file")


def read_dataset(path):
    """Read a CSV or TSV into a list of dicts using only the stdlib."""
    sep = "\t" if path.lower().endswith(".tsv") else ","
    with open(path, newline="") as f:
        reader = csv.DictReader(f, delimiter=sep)
        rows = list(reader)
    return rows


def write_output(model, metrics):
    os.makedirs(OUT_DIR, exist_ok=True)
    buf = io.BytesIO()
    with zipfile.ZipFile(buf, "w", zipfile.ZIP_DEFLATED) as z:
        z.writestr("model.json", json.dumps(model))
        z.writestr("metrics.json", json.dumps(metrics))
    with open(os.path.join(OUT_DIR, "output.bin"), "wb") as f:
        f.write(buf.getvalue())


def main():
    params = load_params()
    inp = find_input()
    rows = read_dataset(inp)
    log("loaded", rows=len(rows))

    # TODO: compute your model from rows + params here (aggregates only).
    model = {"format": "vo-model-1"}
    metrics = {"status": "ok"}

    write_output(model, metrics)
    log("done")


if __name__ == "__main__":
    main()
`, m.Name)
	trainPyPath := filepath.Join(dir, "train.py")
	if err := os.WriteFile(trainPyPath, []byte(trainPy), 0644); err != nil {
		return fmt.Errorf("write train.py: %w", err)
	}

	// Write .gitignore
	gitignorePath := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte("__pycache__/\n*.pyc\n/oasis-lock.json\n"), 0644); err != nil {
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

// ComputeSrcHash computes the SHA-256 hash of all regular files in dir
// (not including hidden dirs, vendor, __pycache__, *.pyc, or oasis-lock.json).
func ComputeSrcHash(dir string) (string, error) {
	h := sha256.New()
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		name := info.Name()
		if info.IsDir() {
			if name == ".git" || name == "vendor" || name == "__pycache__" || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if name == "oasis-lock.json" || strings.HasSuffix(name, ".pyc") {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		fh := sha256.New()
		_, cerr := io.Copy(fh, f)
		f.Close()
		if cerr != nil {
			return cerr
		}
		// Fixed-width per-file record: sha256(rel) || sha256(content). Unframed
		// concatenation would let distinct trees alias to the same digest.
		relSum := sha256.Sum256([]byte(rel))
		h.Write(relSum[:])
		h.Write(fh.Sum(nil))
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ImageTag returns the docker image reference for build/push/check.
// If the image is already tagged (the final path segment after the last "/"
// contains ":"), it is returned unchanged. Otherwise it appends :version.
// This avoids the invalid double-tag like "repo:1:1" when the image already
// carries a tag, while correctly handling registry ports like "127.0.0.1:5000/repo".
func ImageTag(image string, version int) string {
	lastSlash := strings.LastIndex(image, "/")
	lastSegment := image[lastSlash+1:]
	if strings.Contains(lastSegment, ":") {
		return image
	}
	return fmt.Sprintf("%s:%d", image, version)
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
		if uq, uerr := strconv.Unquote(val); uerr == nil {
			val = uq // honors escaped JSON (e.g. params_schema's \" → ")
		} else {
			val = strings.Trim(val, "\"")
		}
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
