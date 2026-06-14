// Package skill loads invokable playbooks ("skills") from Markdown files.
// A skill is a named, described prompt body the model can invoke via the
// run_skill tool (or the user via "/<name>"): an "inline" skill folds its body
// into the turn as a tool result, a "subagent" skill runs in an isolated child
// loop and returns only its final answer. Project scope wins over global.
package skill

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"lumen/internal/config"
	"lumen/internal/frontmatter"
)

// Scope records where a skill was loaded from.
type Scope string

const (
	ScopeProject Scope = "project"
	ScopeGlobal  Scope = "global"
)

// RunAs selects how an invoked skill executes.
type RunAs string

const (
	RunInline   RunAs = "inline"
	RunSubagent RunAs = "subagent"
)

const (
	SkillsDirname = "skills"
	SkillFile     = "SKILL.md"
)

// Skill is a loaded playbook.
type Skill struct {
	Name         string
	Description  string
	Body         string
	Scope        Scope
	Path         string
	AllowedTools []string
	RunAs        RunAs
	Model        string
	Effort       string
}

// Options configure a Store.
type Options struct {
	HomeDir     string
	ProjectRoot string
	CustomPaths []string
	MaxDepth    int
	// IncludeGlobal opts into scanning the host's global convention dirs
	// (~/.claude/skills, ~/.agents/skills, …). Off by default: lumen is
	// project-scoped, so it does not inherit another agent's global skills
	// unless explicitly asked.
	IncludeGlobal bool
}

// Store resolves skills across the configured roots.
type Store struct {
	homeDir       string
	projectRoot   string
	customPaths   []string
	maxDepth      int
	includeGlobal bool
}

// New builds a Store.
func New(opts Options) *Store {
	home := opts.HomeDir
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = h
		}
	}
	root := opts.ProjectRoot
	if root != "" {
		if abs, err := filepath.Abs(root); err == nil {
			root = abs
		}
	}
	return &Store{
		homeDir:       home,
		projectRoot:   root,
		customPaths:   opts.CustomPaths,
		maxDepth:      normalizeMaxDepth(opts.MaxDepth),
		includeGlobal: opts.IncludeGlobal,
	}
}

// List returns every discoverable skill, deduped by name (project skills/ > convention dirs > global > builtin).
func (s *Store) List() []Skill {
	byName := map[string]Skill{}

	if s.projectRoot != "" {
		// Priority 1: skills/ directory at project root (flat .md files)
		flatSkillsDir := filepath.Join(s.projectRoot, SkillsDirname)
		for _, sk := range s.discoverDir(flatSkillsDir, ScopeProject) {
			if _, dup := byName[sk.Name]; !dup {
				byName[sk.Name] = sk
			}
		}

		// Priority 2: .reasonix/skills, .agents/skills, .agent/skills, .claude/skills
		for _, dir := range config.ConventionDirs {
			skillsDir := filepath.Join(s.projectRoot, dir, SkillsDirname)
			for _, sk := range s.discoverDir(skillsDir, ScopeProject) {
				if _, dup := byName[sk.Name]; !dup {
					byName[sk.Name] = sk
				}
			}
		}
	}

	// Global: same convention dirs under home — only when explicitly opted in.
	if s.includeGlobal {
		for _, dir := range config.ConventionDirs {
			skillsDir := filepath.Join(s.homeDir, dir, SkillsDirname)
			for _, sk := range s.discoverDir(skillsDir, ScopeGlobal) {
				if _, dup := byName[sk.Name]; !dup {
					byName[sk.Name] = sk
				}
			}
		}
	}

	// Custom paths
	for _, p := range s.customPaths {
		for _, sk := range s.discoverDir(p, ScopeProject) {
			if _, dup := byName[sk.Name]; !dup {
				byName[sk.Name] = sk
			}
		}
	}

	out := make([]Skill, 0, len(byName))
	for _, sk := range byName {
		out = append(out, sk)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Get resolves one skill by name.
func (s *Store) Get(name string) (Skill, bool) {
	for _, sk := range s.List() {
		if sk.Name == name {
			return sk, true
		}
	}
	return Skill{}, false
}

func (s *Store) discoverDir(dir string, scope Scope) []Skill {
	var out []Skill
	s.scanDir(dir, scope, 1, map[string]bool{}, &out)
	return out
}

func (s *Store) scanDir(dir string, scope Scope, depth int, seen map[string]bool, out *[]Skill) {
	resolved, err := filepath.EvalSymlinks(dir)
	if err == nil {
		dir = resolved
	}
	if seen[dir] {
		return
	}
	seen[dir] = true

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		full := filepath.Join(dir, name)

		if e.IsDir() {
			if !config.IsValidSkillName(name) {
				continue
			}
			skillFile := filepath.Join(full, SkillFile)
			if _, err := os.Stat(skillFile); err == nil {
				if sk, ok := parseSkill(skillFile, name, scope); ok {
					*out = append(*out, sk)
				}
			}
			if depth < s.maxDepth {
				s.scanDir(full, scope, depth+1, seen, out)
			}
			continue
		}

		if strings.EqualFold(filepath.Ext(name), ".md") && !strings.HasPrefix(name, ".") {
			stem := strings.TrimSuffix(name, filepath.Ext(name))
			if !config.IsValidSkillName(stem) {
				continue
			}
			// A loose .md counts as a skill only if it declares skill
			// frontmatter (a non-empty description). This excludes plain docs
			// like CHANGELOG.md / README.md / OPENCLAW.md that happen to live
			// under a skills root — they are markdown, but not skills.
			if sk, ok := parseSkill(full, stem, scope); ok && sk.Description != "" {
				*out = append(*out, sk)
			}
		}
	}
}

func parseSkill(path, stem string, scope Scope) (Skill, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, false
	}
	content := strings.ReplaceAll(string(b), "\r\n", "\n")
	fm, body := frontmatter.Split(content)

	name := stem
	if v := fm["name"]; v != "" && config.IsValidSkillName(v) {
		name = v
	}
	desc := strings.TrimSpace(fm["description"])

	return Skill{
		Name:         name,
		Description:  desc,
		Body:         strings.TrimSpace(body),
		Scope:        scope,
		Path:         path,
		AllowedTools: parseAllowedTools(fm["allowed-tools"]),
		RunAs:        parseRunAs(fm["runas"], fm["context"], fm["agent"]),
		Model:        strings.TrimSpace(fm["model"]),
		Effort:       strings.TrimSpace(fm["effort"]),
	}, true
}

func parseAllowedTools(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(raw, ",") {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func parseRunAs(runAs, context, agent string) RunAs {
	if strings.TrimSpace(runAs) == "subagent" {
		return RunSubagent
	}
	if strings.EqualFold(strings.TrimSpace(context), "fork") {
		return RunSubagent
	}
	if strings.TrimSpace(agent) != "" {
		return RunSubagent
	}
	return RunInline
}

func normalizeMaxDepth(depth int) int {
	if depth <= 0 {
		return 3
	}
	if depth > 5 {
		return 5
	}
	return depth
}

func IsValidName(name string) bool { return config.IsValidSkillName(name) }
