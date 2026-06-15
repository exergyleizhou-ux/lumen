// Package workspace manages workspace detection and project analysis.
package workspace

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type Type string

const (
	TypeUnknown Type = "unknown"
	TypeGo      Type = "go"
	TypeRust    Type = "rust"
	TypeNode    Type = "node"
	TypePython  Type = "python"
	TypeWeb     Type = "web"
	TypeJava    Type = "java"
)

type Info struct {
	Root       string   `json:"root"`
	Type       Type     `json:"type"`
	GitRepo    bool     `json:"git_repo"`
	Languages  []string `json:"languages"`
	BuildTools []string `json:"build_tools,omitempty"`
	Files      int      `json:"files"`
	Dirs       int      `json:"dirs"`
}

type Detector struct {
	mu   sync.Mutex
	root string
	info *Info
}

func NewDetector(root string) *Detector { return &Detector{root: root} }

func (d *Detector) Detect() (*Info, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.info != nil { return d.info, nil }

	root := d.root
	if root == "" { root, _ = os.Getwd() }

	info := &Info{Root: root, Languages: []string{}, BuildTools: []string{}}
	if _, err := os.Stat(filepath.Join(root, ".git")); err == nil { info.GitRepo = true }

	signatures := map[string]Type{"go.mod": TypeGo, "Cargo.toml": TypeRust, "package.json": TypeNode, "requirements.txt": TypePython, "pyproject.toml": TypePython, "pom.xml": TypeJava, "index.html": TypeWeb}
	for f, t := range signatures {
		if _, err := os.Stat(filepath.Join(root, f)); err == nil {
			if info.Type == TypeUnknown { info.Type = t }
			info.Languages = append(info.Languages, string(t))
		}
	}

	tools := map[string]string{"Makefile": "make", "Dockerfile": "docker", "docker-compose.yml": "docker-compose", ".github/workflows": "github-actions"}
	for f, t := range tools {
		if _, err := os.Stat(filepath.Join(root, f)); err == nil { info.BuildTools = append(info.BuildTools, t) }
	}

	filepath.Walk(root, func(path string, fi os.FileInfo, err error) error {
		if err != nil { return nil }
		if fi.IsDir() {
			n := fi.Name()
			if strings.HasPrefix(n, ".") || n == "node_modules" || n == "vendor" { return filepath.SkipDir }
			info.Dirs++; return nil
		}
		info.Files++; return nil
	})
	d.info = info
	return info, nil
}

func (i *Info) Format() string {
	var sb strings.Builder
	sb.WriteString("Workspace\n─────────\n")
	fmt.Fprintf(&sb, "Root: %s\nType: %s\n", i.Root, i.Type)
	if i.GitRepo { sb.WriteString("Git: ✓\n") }
	if len(i.Languages) > 0 { fmt.Fprintf(&sb, "Languages: %s\n", strings.Join(i.Languages, ", ")) }
	if len(i.BuildTools) > 0 { fmt.Fprintf(&sb, "Build tools: %s\n", strings.Join(i.BuildTools, ", ")) }
	fmt.Fprintf(&sb, "Files: %d, Dirs: %d\n", i.Files, i.Dirs)
	return sb.String()
}

type GitInfo struct {
	Branch         string `json:"branch"`
	Remote         string `json:"remote"`
	ModifiedFiles  int    `json:"modified_files"`
	StagedFiles    int    `json:"staged_files"`
	UntrackedFiles int    `json:"untracked_files"`
}

func GitStatus(root string) (*GitInfo, error) {
	info := &GitInfo{}
	if data, err := runGit(root, "rev-parse", "--abbrev-ref", "HEAD"); err == nil { info.Branch = strings.TrimSpace(data) }
	if data, err := runGit(root, "remote", "get-url", "origin"); err == nil { info.Remote = strings.TrimSpace(data) }
	if data, err := runGit(root, "diff", "--name-only"); err == nil { info.ModifiedFiles = countLines(data) }
	if data, err := runGit(root, "diff", "--cached", "--name-only"); err == nil { info.StagedFiles = countLines(data) }
	if data, err := runGit(root, "ls-files", "--others", "--exclude-standard"); err == nil { info.UntrackedFiles = countLines(data) }
	return info, nil
}

func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return string(out), err
}

func countLines(s string) int {
	if strings.TrimSpace(s) == "" { return 0 }
	return len(strings.Split(strings.TrimSpace(s), "\n"))
}

type LanguageCounts map[string]int

func CountLanguages(root string) (LanguageCounts, error) {
	counts := LanguageCounts{}
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() { return nil }
		n := info.Name()
		if strings.HasPrefix(n, ".") || n == "node_modules" { return nil }
		counts[detectLang(filepath.Ext(path), path)]++
		return nil
	})
	return counts, nil
}

func detectLang(ext, path string) string {
	m := map[string]string{".go":"Go",".rs":"Rust",".py":"Python",".js":"JavaScript",".ts":"TypeScript",".java":"Java",".rb":"Ruby",".swift":"Swift",".c":"C",".cpp":"C++",".md":"Markdown",".json":"JSON",".yaml":"YAML",".yml":"YAML",".html":"HTML",".css":"CSS",".sql":"SQL",".sh":"Shell"}
	if l, ok := m[strings.ToLower(ext)]; ok { return l }
	return "Other"
}

func FormatLanguageCounts(counts LanguageCounts) string {
	var sb strings.Builder
	sb.WriteString("Language distribution:\n")
	type lc struct{ lang string; count int }
	var items []lc
	for l, c := range counts { items = append(items, lc{l, c}) }
	sort.Slice(items, func(i, j int) bool { return items[i].count > items[j].count })
	for _, it := range items { fmt.Fprintf(&sb, "  %-15s %d files\n", it.lang, it.count) }
	return sb.String()
}
