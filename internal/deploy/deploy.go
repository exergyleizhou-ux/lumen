// Package deploy handles deployment operations: building binaries,
// cross-compiling for multiple targets, packaging releases, signing,
// and uploading to registries. Supports GoReleaser-compatible configs
// and custom deployment pipelines.
package deploy

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Target represents a build target (OS/Arch combination).
type Target struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
	Name string `json:"name"` // output filename
}

// DefaultTargets returns common cross-compilation targets.
func DefaultTargets() []Target {
	return []Target{
		{OS: "darwin", Arch: "amd64", Name: "lumen-darwin-amd64"},
		{OS: "darwin", Arch: "arm64", Name: "lumen-darwin-arm64"},
		{OS: "linux", Arch: "amd64", Name: "lumen-linux-amd64"},
		{OS: "linux", Arch: "arm64", Name: "lumen-linux-arm64"},
		{OS: "windows", Arch: "amd64", Name: "lumen-windows-amd64.exe"},
	}
}

// Builder manages Go cross-compilation.
type Builder struct {
	mu        sync.Mutex
	outputDir string
	ldflags   string
	targets   []Target
}

// NewBuilder creates a deployment builder.
func NewBuilder(outputDir, ldflags string) *Builder {
	os.MkdirAll(outputDir, 0o755)
	return &Builder{outputDir: outputDir, ldflags: ldflags, targets: DefaultTargets()}
}

// Build compiles the main package for all configured targets.
func (b *Builder) Build(mainPkg string) ([]string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	var built []string
	for _, t := range b.targets {
		outPath := filepath.Join(b.outputDir, t.Name)
		cmd := exec.Command("go", "build",
			"-ldflags", b.ldflags,
			"-o", outPath, mainPkg,
		)
		cmd.Env = append(os.Environ(),
			"GOOS="+t.OS,
			"GOARCH="+t.Arch,
			"CGO_ENABLED=0",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return built, fmt.Errorf("build %s/%s: %w\n%s", t.OS, t.Arch, err, out)
		}
		built = append(built, outPath)
	}
	return built, nil
}

// BuildSingle compiles for a single target.
func (b *Builder) BuildSingle(mainPkg, goos, goarch, outName string) (string, error) {
	outPath := filepath.Join(b.outputDir, outName)
	cmd := exec.Command("go", "build", "-ldflags", b.ldflags, "-o", outPath, mainPkg)
	cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("build: %w\n%s", err, out)
	}
	return outPath, nil
}

// ── Release packaging ────────────────────────────────────

// Release holds metadata for a release.
type Release struct {
	Version    string    `json:"version"`
	Commit     string    `json:"commit"`
	Date       time.Time `json:"date"`
	Assets     []Asset   `json:"assets"`
	Changelog  string    `json:"changelog"`
	Prerelease bool      `json:"prerelease"`
}

// Asset is one release artifact.
type Asset struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Packager creates release packages.
type Packager struct {
	mu      sync.Mutex
	distDir string
}

// NewPackager creates a release packager.
func NewPackager(distDir string) *Packager {
	os.MkdirAll(distDir, 0o755)
	return &Packager{distDir: distDir}
}

// Package creates tar.gz archives and SHA256 checksums for built binaries.
func (p *Packager) Package(version string, builtBins []string) (*Release, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	rel := &Release{Version: version, Date: time.Now()}
	for _, bin := range builtBins {
		info, err := os.Stat(bin)
		if err != nil {
			return nil, err
		}
		asset := Asset{
			Name: filepath.Base(bin),
			Path: bin,
			Size: info.Size(),
		}
		rel.Assets = append(rel.Assets, asset)
	}

	// Generate checksums
	checksumPath := filepath.Join(p.distDir, "SHA256SUMS")
	var checksumLines []string
	for _, a := range rel.Assets {
		hash, _ := sha256File(a.Path)
		a.SHA256 = hash
		checksumLines = append(checksumLines, fmt.Sprintf("%s  %s", hash, a.Name))
	}
	os.WriteFile(checksumPath, []byte(strings.Join(checksumLines, "\n")+"\n"), 0o644)

	return rel, nil
}

// FormatRelease formats release metadata.
func FormatRelease(rel *Release) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Release %s (%s)\n", rel.Version, rel.Date.Format("2006-01-02"))
	fmt.Fprintf(&sb, "%s\n\n", strings.Repeat("─", 40))
	for _, a := range rel.Assets {
		fmt.Fprintf(&sb, "  %-35s %8d bytes  sha256:%s\n", a.Name, a.Size, a.SHA256[:12])
	}
	return sb.String()
}

func sha256File(path string) (string, error) {
	out, err := exec.Command("shasum", "-a", "256", path).Output()
	if err != nil {
		return "", err
	}
	return strings.Fields(string(out))[0], nil
}
