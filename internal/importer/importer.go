// Package importer installs skills and MCP servers from URLs, local
// files/folders, .mcp.json configurations, executables, and package
// names. Adapted from Reasonix's installsource package.
package importer

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Source describes what to install.
type Source struct {
	URL      string `json:"url,omitempty"`
	Path     string `json:"path,omitempty"`
	Package  string `json:"package,omitempty"`
	Skill    string `json:"skill,omitempty"`
}

// Kind categorizes an import source.
type Kind string

const (
	KindURL      Kind = "url"
	KindFile     Kind = "file"
	KindFolder   Kind = "folder"
	KindMCPJson  Kind = "mcp_json"
	KindExec     Kind = "executable"
	KindPackage  Kind = "package"
	KindSkill    Kind = "skill"
)

// ── Skill Import ────────────────────────────────────────────

// ImportSkill installs a skill from a URL, local path, or package.
// Skills are Markdown files placed in the skills/ directory.
func ImportSkill(source Source, skillDir string) (string, error) {
	if skillDir == "" {
		skillDir = "skills"
	}
	os.MkdirAll(skillDir, 0o755)

	switch {
	case source.Skill != "":
		return importSkillByName(source.Skill, skillDir)
	case source.URL != "":
		return importSkillFromURL(source.URL, skillDir)
	case source.Path != "":
		return importSkillFromPath(source.Path, skillDir)
	default:
		return "", fmt.Errorf("no skill source specified (url, path, or name)")
	}
}

func importSkillByName(name, dir string) (string, error) {
	// Try known skill registries
	endpoints := []string{
		fmt.Sprintf("https://raw.githubusercontent.com/obra/superpowers/main/skills/%s/SKILL.md", name),
		fmt.Sprintf("https://raw.githubusercontent.com/anthropics/skills/main/%s/SKILL.md", name),
	}

	for _, url := range endpoints {
		if path, err := importSkillFromURL(url, dir); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("skill %q not found in known registries", name)
}

func importSkillFromURL(url, dir string) (string, error) {
	data, err := fetchURL(url)
	if err != nil {
		return "", err
	}
	name := skillNameFromURL(url)
	path := filepath.Join(dir, name+".md")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write skill: %w", err)
	}
	return path, nil
}

func importSkillFromPath(localPath, dir string) (string, error) {
	data, err := os.ReadFile(localPath)
	if err != nil {
		return "", fmt.Errorf("read: %w", err)
	}
	name := skillNameFromPath(localPath)
	path := filepath.Join(dir, name+".md")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write: %w", err)
	}
	return path, nil
}

func skillNameFromURL(url string) string {
	base := filepath.Base(url)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func skillNameFromPath(p string) string {
	base := filepath.Base(p)
	if base == "SKILL.md" {
		return filepath.Base(filepath.Dir(p))
	}
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// ── MCP Server Import ──────────────────────────────────────

// ImportMCP installs an MCP server from a source. Returns the tool name prefix.
func ImportMCP(source Source, mcpDir string) (string, error) {
	if mcpDir == "" {
		mcpDir = ".lumen/mcp"
	}
	os.MkdirAll(mcpDir, 0o755)

	switch {
	case source.Package != "":
		return source.Package, installPackage(source.Package)
	case source.URL != "":
		return installMCPFromURL(source.URL, mcpDir)
	case source.Path != "" && strings.HasSuffix(source.Path, ".mcp.json"):
		return installMCPFromJSON(source.Path)
	case source.Path != "":
		return installMCPFromExecutable(source.Path, mcpDir)
	default:
		return "", fmt.Errorf("no MCP source specified")
	}
}

func installPackage(name string) error {
	// Try npm install
	if _, err := exec.LookPath("npm"); err == nil {
		return exec.Command("npm", "install", "-g", name).Run()
	}
	// Try pip install
	if _, err := exec.LookPath("pip"); err == nil {
		return exec.Command("pip", "install", name).Run()
	}
	return fmt.Errorf("no package manager found (npm or pip)")
}

func installMCPFromURL(url, dir string) (string, error) {
	return "", fmt.Errorf("URL-based MCP install not yet implemented — clone manually to %s", dir)
}

func installMCPFromJSON(path string) (string, error) {
	return "", fmt.Errorf("MCP JSON config parsing not yet implemented")
}

func installMCPFromExecutable(path, dir string) (string, error) {
	name := filepath.Base(path)
	dest := filepath.Join(dir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(dest, data, 0o755); err != nil {
		return "", err
	}
	return name, nil
}

// ── Discover installed ─────────────────────────────────────

// ListSkills returns all installed skill names.
func ListSkills(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, strings.TrimSuffix(e.Name(), ".md"))
		}
	}
	return names, nil
}

// ListMCP returns installed MCP server names.
func ListMCP(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

// ── Uninstall ──────────────────────────────────────────────

// UninstallSkill removes a skill by name.
func UninstallSkill(name, dir string) error {
	path := filepath.Join(dir, name+".md")
	return os.Remove(path)
}

// ── HTTP helper ─────────────────────────────────────────────

func fetchURL(url string) ([]byte, error) {
	cmd := exec.Command("curl", "-sSL", url)
	return cmd.Output()
}
