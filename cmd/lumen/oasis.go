package main

import (
	"bytes"
	"context"
	"encoding/json"
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

// oasisPipeline is set while `oasis publish` runs the build→check→deploy chain,
// so intermediate steps suppress their standalone "Next: ..." hints.
var oasisPipeline bool

// runOasis dispatches `lumen oasis <subcommand>`.
func runOasis(args []string) {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: lumen oasis <init|validate|check|build|deploy|verify|publish>\n")
		os.Exit(1)
	}
	sub := args[0]
	rest := args[1:]

	switch sub {
	case "check":
		dir, dataPath := parseCheckArgs(rest)
		checkAlgo(dir, dataPath)

	case "templates":
		fmt.Println("Available algorithm templates (lumen oasis init <name> --template <key>):")
		for _, t := range oasis.Templates() {
			def := ""
			if t.Key == oasis.DefaultTemplate().Key {
				def = "  (default)"
			}
			fmt.Printf("  %-12s %s%s\n", t.Key, t.Description, def)
		}

	case "init":
		// Manual parse so --template works in ANY position (Go's flag stops at the
		// first positional, which would silently ignore `init <name> --template x`).
		tmplKey, pos := "", []string{}
		for i := 0; i < len(rest); i++ {
			switch {
			case rest[i] == "--template" || rest[i] == "-template":
				if i+1 < len(rest) {
					tmplKey = rest[i+1]
					i++
				}
			case strings.HasPrefix(rest[i], "--template="):
				tmplKey = strings.TrimPrefix(rest[i], "--template=")
			default:
				pos = append(pos, rest[i])
			}
		}
		if len(pos) < 1 {
			fmt.Fprintf(os.Stderr, "Usage: lumen oasis init <name> [dir] [--template <key>]\n")
			os.Exit(1)
		}
		name := pos[0]
		dir := name
		if len(pos) > 1 {
			dir = pos[1]
		}
		initAlgo(name, dir, tmplKey)

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

	case "publish":
		dir := "."
		if len(rest) > 0 {
			dir = rest[0]
		}
		publishAlgo(dir)

	case "verify":
		dir := "."
		if len(rest) > 0 {
			dir = rest[0]
		}
		verifyAlgo(dir)

	default:
		fmt.Fprintf(os.Stderr, "unknown oasis subcommand: %s\n", sub)
		fmt.Fprintf(os.Stderr, "Usage: lumen oasis <init|validate|check|build|deploy|verify|publish>\n")
		os.Exit(1)
	}
}

// verifyAlgo re-checks that the working tree is the exact source recorded in the
// provenance lockfile — i.e. that the locked/deployed image digest still
// corresponds to the code in front of you. Source-only (no docker/registry).
func verifyAlgo(dir string) {
	res, err := oasis.VerifySource(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "oasis verify: %v — run 'lumen oasis build' first\n", err)
		os.Exit(1)
	}
	if res.Digest != "" {
		fmt.Printf("   Locked image digest: %s\n", res.Digest)
	}
	if res.SourceMatch {
		fmt.Printf("✅ source matches the provenance lock (sha256:%s) — this tree built the locked artifact\n", short12(res.CurrentHash))
		return
	}
	fmt.Println("❌ source DRIFTED from the provenance lock:")
	fmt.Printf("   locked : %s\n", res.LockedHash)
	fmt.Printf("   current: %s\n", res.CurrentHash)
	fmt.Println("   the locked/deployed image no longer matches this code — rebuild + redeploy")
	os.Exit(1)
}

func short12(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

func initAlgo(name, dir, tmplKey string) {
	m := oasis.DefaultManifest(name)
	tmpl := oasis.DefaultTemplate()
	if tmplKey != "" {
		t, ok := oasis.TemplateByName(tmplKey)
		if !ok {
			fmt.Fprintf(os.Stderr, "oasis init: unknown template %q — run 'lumen oasis templates'\n", tmplKey)
			os.Exit(1)
		}
		tmpl = t
	}
	if err := oasis.ScaffoldTemplate(dir, m, tmpl); err != nil {
		fmt.Fprintf(os.Stderr, "oasis init: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✅ algorithm %q scaffolded at %s (template: %s)\n", name, dir, tmpl.Key)
	fmt.Printf("   %s\n", tmpl.Description)
	fmt.Printf("   It is a COMPLETE working example — run it as-is, or edit train.py.\n")
	fmt.Printf("   Next: cd %s && lumen oasis build, then 'lumen oasis check'\n", dir)
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
	for _, warn := range oasis.ManifestWarnings(m) {
		fmt.Printf("   ⚠️  %s\n", warn)
	}
	fmt.Printf("   Next: lumen oasis build, then 'lumen oasis check' to verify the container contract\n")
}

// parseCheckArgs parses `oasis check [dir] [--data <file>]`. Like `init`, the
// flag is honored in ANY position (Go's flag pkg stops at the first positional,
// which would silently ignore `check <dir> --data x`).
func parseCheckArgs(rest []string) (dir, dataPath string) {
	dir = "."
	var pos []string
	for i := 0; i < len(rest); i++ {
		switch {
		case rest[i] == "--data" || rest[i] == "-data":
			if i+1 < len(rest) {
				dataPath = rest[i+1]
				i++
			}
		case strings.HasPrefix(rest[i], "--data="):
			dataPath = strings.TrimPrefix(rest[i], "--data=")
		default:
			pos = append(pos, rest[i])
		}
	}
	if len(pos) > 0 {
		dir = pos[0]
	}
	return dir, dataPath
}

// checkAlgo runs the C2D runtime contract self-check: it executes the algorithm
// image under the exact marketplace isolation and verifies it produces a valid
// /out/output.bin — so the author catches contract violations before pushing.
// When dataPath is set, the author's own sample data seeds /data instead of the
// synthetic dataset (closer to real input); the synthetic privacy lints, which
// depend on seeded sentinels, are skipped in that mode.
func checkAlgo(dir, dataPath string) {
	m, err := loadManifest(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "oasis check: %v\n", err)
		os.Exit(1)
	}
	if !oasis.DockerAvailable() {
		fmt.Fprintf(os.Stderr, "oasis check: docker not found on PATH — the contract self-check runs the image in a sandbox.\n")
		os.Exit(1)
	}

	var sampleData []byte
	if dataPath != "" {
		b, rerr := os.ReadFile(dataPath)
		if rerr != nil {
			fmt.Fprintf(os.Stderr, "oasis check: --data %s: %v\n", dataPath, rerr)
			os.Exit(1)
		}
		if len(b) == 0 {
			fmt.Fprintf(os.Stderr, "oasis check: --data %s is empty\n", dataPath)
			os.Exit(1)
		}
		sampleData = b
	}

	tag := oasis.ImageTag(m.Image, m.Version)
	fmt.Printf("🔬 C2D contract self-check on %s\n", tag)
	fmt.Printf("   docker run --network none --read-only -v /data:ro -v /params.json:ro -v /out\n")
	if sampleData != nil {
		fmt.Printf("   Seeding /data with your sample: %s (%d bytes) — synthetic privacy lints skipped\n", dataPath, len(sampleData))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	res := oasis.RunContractCheck(ctx, tag, m.OutputKind, sampleData)

	if res.OK {
		fmt.Printf("✅ contract OK — runs isolated and produced a valid %s /out/output.bin\n", m.OutputKind)
		if !oasisPipeline {
			fmt.Printf("   Next: lumen oasis deploy\n")
		}
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

	// Write the provenance lockfile (complete manifest + source hash; the image
	// digest is pinned in by `deploy` once the registry resolves it).
	lock := oasis.Lock{
		Manifest: m,
		BuiltAt:  time.Now().UTC().Format(time.RFC3339),
		Image:    m.Image,
		SrcHash:  srcHash,
	}
	if err := oasis.WriteLock(dir, lock); err != nil {
		fmt.Fprintf(os.Stderr, "oasis build: write lockfile: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✅ built %s (source sha256: %s)\n", tag, srcHash[:12])
	fmt.Printf("   Lockfile: %s\n", filepath.Join(dir, oasis.LockFile))
	if !oasisPipeline {
		fmt.Printf("   Next: lumen oasis check  (verify the C2D contract before deploy)\n")
	}
}

// publishAlgo is the author one-shot: build → check → deploy. Each step exits
// non-zero on failure, so a broken or contract-violating algorithm is never
// pushed or registered.
func publishAlgo(dir string) {
	oasisPipeline = true
	fmt.Println("📦 oasis publish: build → check → deploy")
	buildAlgo(dir)
	checkAlgo(dir, "")
	deployAlgo(dir)
	fmt.Println("🎉 published — built, contract-verified, pushed, and registered")
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
		// Pin the resolved digest back into the lockfile so the author ends with
		// a complete, re-verifiable provenance record (source hash + exact image).
		if err := oasis.UpdateLockDigest(dir, digest); err != nil {
			fmt.Printf("   ⚠️  could not pin digest into %s: %v\n", oasis.LockFile, err)
		} else {
			fmt.Printf("   Pinned digest into %s\n", oasis.LockFile)
		}
		// Also pin it into the manifest the author commits, so oasis.toml carries
		// the exact deployed image (line-level edit; comments/order preserved).
		if err := oasis.UpdateManifestDigest(dir, digest); err != nil {
			fmt.Printf("   ⚠️  could not pin digest into %s: %v\n", oasis.ManifestFile, err)
		} else {
			fmt.Printf("   Pinned digest into %s\n", oasis.ManifestFile)
		}
	}

	// ── Conveyor belt: auto-register to marketplace ──
	marketplaceURL := os.Getenv("MARKETPLACE_URL")
	if marketplaceURL == "" {
		marketplaceURL = "http://localhost:8080"
	}
	registerURL := oasisRegisterURL(marketplaceURL)

	payload := buildRegisterPayload(m, digest)

	token := os.Getenv("MARKETPLACE_TOKEN")
	req, _ := http.NewRequest("POST", registerURL, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("   ⚠️  Marketplace register failed (network): %v\n", err)
		fmt.Printf("   Register manually: POST %s\n", registerURL)
		fmt.Printf("   Payload: %s\n", payload)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		fmt.Printf("   ⚠️  Marketplace returned %d: %s\n", resp.StatusCode, strings.TrimSpace(string(body)))
		return
	}
	id := algoIDFromResponse(body)
	fmt.Printf("   ✅ Registered on marketplace (pending review): %s\n", id)

	// approve+trust is an ops decision (on L1 the audited code is the privacy
	// boundary), so auto-do it only when explicitly opted in (a dev/demo
	// one-shot). Otherwise leave it pending and print the ops command.
	if id == "" {
		return
	}
	if os.Getenv("MARKETPLACE_TRUST") == "1" {
		if err := reviewAlgo(client, marketplaceURL, id, token); err != nil {
			fmt.Printf("   ⚠️  approve+trust failed: %v\n", err)
		} else {
			fmt.Printf("   ✅ Approved + trusted (L1): %s\n", id)
		}
	} else {
		fmt.Printf("   Next (ops): POST %s {\"status\":\"approved\",\"trusted\":true}\n", oasisReviewURL(marketplaceURL, id))
		fmt.Printf("   or re-run with MARKETPLACE_TRUST=1 to approve+trust now\n")
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

// oasisRegisterURL is the marketplace admin endpoint that registers an algorithm.
func oasisRegisterURL(base string) string {
	return strings.TrimRight(base, "/") + "/api/v1/admin/compute/algorithms"
}

// oasisReviewURL approves/rejects + (un)trusts a registered algorithm (ops only).
func oasisReviewURL(base, id string) string {
	return strings.TrimRight(base, "/") + "/api/v1/admin/compute/algorithms/" + id + "/review"
}

// algoIDFromResponse pulls data.id out of the register response
// ({"code":0,"data":{"id":...}}). Returns "" if the body isn't that shape.
func algoIDFromResponse(body []byte) string {
	var r struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &r) == nil {
		return r.Data.ID
	}
	return ""
}

// reviewAlgo approves + trusts a just-registered algorithm (an ops action).
func reviewAlgo(client *http.Client, base, id, token string) error {
	req, _ := http.NewRequest("POST", oasisReviewURL(base, id), strings.NewReader(`{"status":"approved","trusted":true}`))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

// buildRegisterPayload encodes the marketplace register request. params_schema is
// emitted as a JSON OBJECT (the admin endpoint binds it to map[string]any) — the
// old code sent it as a quoted string, which the API rejected with 400.
func buildRegisterPayload(m oasis.Manifest, digest string) []byte {
	type reg struct {
		Name         string          `json:"name"`
		Runtime      string          `json:"runtime"`
		Image        string          `json:"image"`
		ImageDigest  string          `json:"image_digest"`
		Entrypoint   string          `json:"entrypoint"`
		OutputKind   string          `json:"output_kind"`
		Version      int             `json:"version"`
		SourceRef    string          `json:"source_ref"`
		ParamsSchema json.RawMessage `json:"params_schema,omitempty"`
	}
	r := reg{
		Name: m.Name, Runtime: m.Runtime, Image: m.Image, ImageDigest: digest,
		Entrypoint: m.Entrypoint, OutputKind: m.OutputKind, Version: m.Version, SourceRef: m.SourceRef,
	}
	if s := strings.TrimSpace(m.ParamsSchema); s != "" && json.Valid([]byte(s)) {
		r.ParamsSchema = json.RawMessage(s)
	}
	b, _ := json.Marshal(r)
	return b
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
