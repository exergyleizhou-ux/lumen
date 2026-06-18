package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"lumen/internal/oasis"
)

// runOasis dispatches `lumen oasis <subcommand>`.
func runOasis(args []string) {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: lumen oasis <init|validate|check|build|deploy>\n")
		os.Exit(1)
	}
	sub := args[0]
	rest := args[1:]

	switch sub {
	case "check":
		dir := "."
		if len(rest) > 0 {
			dir = rest[0]
		}
		checkAlgo(dir)

	case "init":
		if len(rest) < 1 {
			fmt.Fprintf(os.Stderr, "Usage: lumen oasis init <name>\n")
			os.Exit(1)
		}
		name := rest[0]
		dir := name
		if len(rest) > 1 {
			dir = rest[1]
		}
		initAlgo(name, dir)

	case "validate":
		dir := "."
		if len(rest) > 0 {
			dir = rest[0]
		}
		validateAlgo(dir)

	case "build":
		dir := "."
		if len(rest) > 0 {
			dir = rest[0]
		}
		buildAlgo(dir)

	case "deploy":
		dir := "."
		if len(rest) > 0 {
			dir = rest[0]
		}
		deployAlgo(dir)

	default:
		fmt.Fprintf(os.Stderr, "unknown oasis subcommand: %s\n", sub)
		fmt.Fprintf(os.Stderr, "Usage: lumen oasis <init|validate|check|build|deploy>\n")
		os.Exit(1)
	}
}

func initAlgo(name, dir string) {
	m := oasis.DefaultManifest(name)
	if err := oasis.Scaffold(dir, m); err != nil {
		fmt.Fprintf(os.Stderr, "oasis init: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✅ algorithm %q scaffolded at %s\n", name, dir)
	fmt.Printf("   Next: cd %s && lumen oasis build\n", dir)
}

func validateAlgo(dir string) {
	m, err := loadManifest(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "oasis validate: %v\n", err)
		os.Exit(1)
	}

	errs := oasis.Validate(m)
	if len(errs) > 0 {
		fmt.Println("❌ Validation failed:")
		for _, e := range errs {
			fmt.Printf("   - %s\n", e)
		}
		os.Exit(1)
	}

	// Check Dockerfile exists
	dockerfile := filepath.Join(dir, m.Dockerfile)
	if _, err := os.Stat(dockerfile); err != nil {
		fmt.Printf("⚠️  Dockerfile %q not found — will be needed for build\n", m.Dockerfile)
	}

	fmt.Printf("✅ manifest validated: %s v%d (runtime=%s, output=%s)\n",
		m.Name, m.Version, m.Runtime, m.OutputKind)
	fmt.Printf("   Image: %s\n", m.Image)
	fmt.Printf("   Next: lumen oasis build, then 'lumen oasis check' to verify the container contract\n")
}

// checkAlgo runs the C2D runtime contract self-check: it executes the algorithm
// image under the exact marketplace isolation and verifies it produces a valid
// /out/output.bin — so the author catches contract violations before pushing.
func checkAlgo(dir string) {
	m, err := loadManifest(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "oasis check: %v\n", err)
		os.Exit(1)
	}
	if !oasis.DockerAvailable() {
		fmt.Fprintf(os.Stderr, "oasis check: docker not found on PATH — the contract self-check runs the image in a sandbox.\n")
		os.Exit(1)
	}

	tag := oasis.ImageTag(m.Image, m.Version)
	fmt.Printf("🔬 C2D contract self-check on %s\n", tag)
	fmt.Printf("   docker run --network none --read-only -v /data:ro -v /out\n")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	res := oasis.RunContractCheck(ctx, tag, m.OutputKind, nil)

	if res.OK {
		fmt.Printf("✅ contract OK — runs isolated and produced a valid %s /out/output.bin\n", m.OutputKind)
		fmt.Printf("   Next: lumen oasis deploy\n")
		return
	}

	fmt.Println("❌ contract violations:")
	for _, v := range res.Violations {
		fmt.Printf("   - %s\n", v)
	}
	if strings.TrimSpace(res.Logs) != "" {
		fmt.Println("   --- container logs ---")
		for _, line := range strings.Split(strings.TrimRight(res.Logs, "\n"), "\n") {
			fmt.Printf("   %s\n", line)
		}
	}
	os.Exit(1)
}

func buildAlgo(dir string) {
	m, err := loadManifest(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "oasis build: %v\n", err)
		os.Exit(1)
	}

	dockerfile := filepath.Join(dir, m.Dockerfile)
	fmt.Printf("🔨 Building %s…\n", m.Name)

	// Build docker image
	tag := oasis.ImageTag(m.Image, m.Version)
	cmd := exec.Command("docker", "build", "-t", tag, "-f", dockerfile, dir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "oasis build: docker build failed: %v\n", err)
		os.Exit(1)
	}

	// Compute source hash for provenance
	srcHash, err := oasis.ComputeSrcHash(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "oasis build: source hash: %v\n", err)
		os.Exit(1)
	}

	// Write lockfile
	lock := oasis.Lock{
		Manifest: m,
		BuiltAt:  time.Now().UTC().Format(time.RFC3339),
		Image:    m.Image,
		SrcHash:  srcHash,
	}
	lockPath := filepath.Join(dir, "oasis-lock.json")
	lockData, _ := encodeJSON(lock)
	if err := os.WriteFile(lockPath, lockData, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "oasis build: write lockfile: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✅ built %s (source sha256: %s)\n", tag, srcHash[:12])
	fmt.Printf("   Lockfile: %s\n", lockPath)
	fmt.Printf("   Next: lumen oasis check  (verify the C2D contract before deploy)\n")
}

func deployAlgo(dir string) {
	m, err := loadManifest(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "oasis deploy: %v\n", err)
		os.Exit(1)
	}

	tag := oasis.ImageTag(m.Image, m.Version)
	fmt.Printf("📤 Pushing %s…\n", tag)

	// Push docker image
	cmd := exec.Command("docker", "push", tag)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "oasis deploy: docker push failed: %v\n", err)
		fmt.Fprintf(os.Stderr, "   You can push manually: docker push %s\n", tag)
		os.Exit(1)
	}

	// Get image digest
	digest := getImageDigest(tag)

	fmt.Printf("✅ deployed %s\n", tag)
	if digest != "" && digest != "<no value>" {
		fmt.Printf("   Digest: %s\n", digest)
	}

	// ── Conveyor belt: auto-register to marketplace ──
	marketplaceURL := os.Getenv("MARKETPLACE_URL")
	if marketplaceURL == "" {
		marketplaceURL = "http://localhost:8080"
	}
	registerURL := strings.TrimRight(marketplaceURL, "/") + "/api/compute/algorithms"

	payload := fmt.Sprintf(`{"name":%q,"runtime":%q,"image":%q,"image_digest":%q,"entrypoint":%q,"output_kind":%q,"version":%d,"params_schema":%q}`,
		m.Name, m.Runtime, m.Image, digest, m.Entrypoint, m.OutputKind, m.Version, m.ParamsSchema)

	req, _ := http.NewRequest("POST", registerURL, strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	if token := os.Getenv("MARKETPLACE_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("   ⚠️  Marketplace register failed (network): %v\n", err)
		fmt.Printf("   Register manually: POST %s\n", registerURL)
		fmt.Printf("   Payload: %s\n", payload)
	} else {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == 200 || resp.StatusCode == 201 {
			fmt.Printf("   ✅ Registered on marketplace: %s\n", strings.TrimSpace(string(body)))
		} else {
			fmt.Printf("   ⚠️  Marketplace returned %d: %s\n", resp.StatusCode, strings.TrimSpace(string(body)))
		}
	}
}

// ── Helpers ────────────────────────────────────────────────

func getImageDigest(tag string) string {
	digestCmd := exec.Command("docker", "inspect", "--format={{index .RepoDigests 0}}", tag)
	digestOut, _ := digestCmd.Output()
	digest := strings.TrimSpace(string(digestOut))
	// Extract just the sha256:... part
	if idx := strings.Index(digest, "@sha256:"); idx >= 0 {
		digest = digest[idx+1:] // skip the @
	}
	return digest
}

// ── Helpers ────────────────────────────────────────────────

func loadManifest(dir string) (oasis.Manifest, error) {
	path := filepath.Join(dir, "oasis.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		return oasis.Manifest{}, fmt.Errorf("read %s: %w — run 'lumen oasis init <name>' first", path, err)
	}
	return oasis.ParseManifest(string(data))
}

func encodeJSON(v interface{}) ([]byte, error) {
	// Simple indented JSON for lockfiles
	buf := new(strings.Builder)
	fmt.Fprintf(buf, "{\n")
	fmt.Fprintf(buf, `  "manifest": {"name":%q,"runtime":%q}`, v.(oasis.Lock).Manifest.Name, v.(oasis.Lock).Manifest.Runtime)
	// Fallback to proper encoding
	_ = buf
	return []byte(fmt.Sprintf(`{"built_at":"%s","image":"%s","source_sha256":"%s"}`,
		v.(oasis.Lock).BuiltAt, v.(oasis.Lock).Image, v.(oasis.Lock).SrcHash)), nil
}
