package main

import (
	"fmt"
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
		fmt.Fprintf(os.Stderr, "Usage: lumen oasis <init|validate|build|deploy>\n")
		os.Exit(1)
	}
	sub := args[0]
	rest := args[1:]

	switch sub {
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
		fmt.Fprintf(os.Stderr, "Usage: lumen oasis <init|validate|build|deploy>\n")
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

	// Check cmd/algo/main.go exists
	mainGo := filepath.Join(dir, "cmd", "algo", "main.go")
	if _, err := os.Stat(mainGo); err != nil {
		fmt.Printf("⚠️  cmd/algo/main.go not found — algorithm entrypoint missing\n")
	}

	fmt.Printf("✅ manifest validated: %s v%d (runtime=%s, output=%s)\n",
		m.Name, m.Version, m.Runtime, m.OutputKind)
	fmt.Printf("   Image: %s\n", m.Image)
	fmt.Printf("   Next: lumen oasis build\n")
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
	tag := fmt.Sprintf("%s:%d", m.Image, m.Version)
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
	fmt.Printf("   Next: lumen oasis deploy\n")
}

func deployAlgo(dir string) {
	m, err := loadManifest(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "oasis deploy: %v\n", err)
		os.Exit(1)
	}

	tag := fmt.Sprintf("%s:%d", m.Image, m.Version)
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
	digestCmd := exec.Command("docker", "inspect", "--format={{index .RepoDigests 0}}", tag)
	digestOut, _ := digestCmd.Output()
	digest := strings.TrimSpace(string(digestOut))

	fmt.Printf("✅ deployed %s\n", tag)
	if digest != "" && digest != "<no value>" {
		fmt.Printf("   Digest: %s\n", digest)
	}
	fmt.Printf("   Next: register in marketplace admin panel\n")
	fmt.Printf("   Image: %s\n", m.Image)
	fmt.Printf("   Digest: %s\n", digest)
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
