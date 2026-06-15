// Package release generates release notes from git history and publishes
// GitHub Releases. Adapted from Reasonix's release management tooling.
package release

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Notes represents generated release notes.
type Notes struct {
	Version     string    `json:"version"`
	Title       string    `json:"title"`
	Date        time.Time `json:"date"`
	Sections    []Section `json:"sections"`
	TotalCommits int      `json:"total_commits"`
	RawMarkdown string    `json:"raw_markdown"`
}

// Section is one category of changes.
type Section struct {
	Title  string   `json:"title"`
	Emoji  string   `json:"emoji"`
	Items  []string `json:"items"`
}

// Generator creates release notes from git history.
type Generator struct {
	repoPath string
	sinceRef string // e.g., "v0.1.0"
}

// NewGenerator creates a release notes generator.
func NewGenerator(repoPath, sinceRef string) *Generator {
	return &Generator{repoPath: repoPath, sinceRef: sinceRef}
}

// Generate produces release notes from commits since the last tag.
func (g *Generator) Generate(ctx context.Context, version string) (*Notes, error) {
	commits, err := g.getCommits(ctx)
	if err != nil {
		return nil, err
	}

	notes := &Notes{
		Version: version,
		Date:    time.Now(),
	}

	// Categorize commits
	categories := map[string]*Section{
		"feat":     {Title: "Features", Emoji: "🚀"},
		"fix":      {Title: "Bug Fixes", Emoji: "🐛"},
		"perf":     {Title: "Performance", Emoji: "⚡"},
		"refactor": {Title: "Refactoring", Emoji: "♻️"},
		"test":     {Title: "Tests", Emoji: "✅"},
		"docs":     {Title: "Documentation", Emoji: "📝"},
		"chore":    {Title: "Chores", Emoji: "🔧"},
		"security": {Title: "Security", Emoji: "🔒"},
	}

	var uncategorized Section
	uncategorized.Title = "Other Changes"
	uncategorized.Emoji = "📦"

	for _, c := range commits {
		conventional, description := parseConventionalCommit(c)
		if sec, ok := categories[conventional]; ok {
			sec.Items = append(sec.Items, description)
		} else {
			uncategorized.Items = append(uncategorized.Items, c)
		}
	}

	for _, sec := range categories {
		if len(sec.Items) > 0 {
			notes.Sections = append(notes.Sections, *sec)
			notes.TotalCommits += len(sec.Items)
		}
	}
	if len(uncategorized.Items) > 0 {
		notes.Sections = append(notes.Sections, uncategorized)
		notes.TotalCommits += len(uncategorized.Items)
	}

	notes.RawMarkdown = notes.Format()
	return notes, nil
}

func (g *Generator) getCommits(ctx context.Context) ([]string, error) {
	args := []string{"log", "--oneline", "--no-merges"}
	if g.sinceRef != "" {
		args = append(args, g.sinceRef+"..HEAD")
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	if g.repoPath != "" {
		cmd.Dir = g.repoPath
	}
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var commits []string
	for _, line := range lines {
		if line != "" {
			commits = append(commits, line)
		}
	}
	return commits, nil
}

func parseConventionalCommit(commit string) (string, string) {
	// Strip hash prefix
	parts := strings.SplitN(commit, " ", 2)
	if len(parts) < 2 {
		return "", commit
	}
	msg := parts[1]

	// Check for conventional commit prefix
	lower := strings.ToLower(msg)
	for _, prefix := range []string{"feat", "fix", "perf", "refactor", "test", "docs", "chore", "security"} {
		if strings.HasPrefix(lower, prefix+":") || strings.HasPrefix(lower, prefix+"(") {
			return prefix, msg
		}
	}
	return "", commit
}

// Format renders release notes as Markdown.
func (n *Notes) Format() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "## %s\n\n", n.Title)
	fmt.Fprintf(&sb, "**%s** — %d commits\n\n", n.Date.Format("2006-01-02"), n.TotalCommits)

	for _, sec := range n.Sections {
		fmt.Fprintf(&sb, "### %s %s\n\n", sec.Emoji, sec.Title)
		for _, item := range sec.Items {
			fmt.Fprintf(&sb, "- %s\n", item)
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

// ── GitHub Release ──────────────────────────────────────────

// Publisher creates GitHub Releases.
type Publisher struct {
	owner string
	repo  string
}

// NewPublisher creates a release publisher.
func NewPublisher(owner, repo string) *Publisher {
	return &Publisher{owner: owner, repo: repo}
}

// Publish creates a GitHub Release with the given notes.
func (p *Publisher) Publish(ctx context.Context, version string, notes *Notes, prerelease bool) error {
	if _, err := exec.LookPath("gh"); err != nil {
		return fmt.Errorf("gh CLI not installed")
	}

	args := []string{"release", "create", version,
		"--title", fmt.Sprintf("%s — %s", version, notes.Title),
		"--notes", notes.RawMarkdown,
	}
	if prerelease {
		args = append(args, "--prerelease")
	}
	if p.owner != "" && p.repo != "" {
		args = append(args, "--repo", p.owner+"/"+p.repo)
	}

	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// GetLatestTag returns the most recent git tag.
func GetLatestTag(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "describe", "--tags", "--abbrev=0").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// SuggestNextVersion suggests the next version number based on commits.
func SuggestNextVersion(ctx context.Context, currentVersion string) string {
	commits, _ := exec.CommandContext(ctx, "git", "log", "--oneline", currentVersion+"..HEAD").Output()
	msg := strings.ToLower(string(commits))

	// Bump patch by default
	parts := strings.Split(strings.TrimPrefix(currentVersion, "v"), ".")
	if len(parts) != 3 {
		return "v0.1.0"
	}
	major, minor, patch := parts[0], parts[1], parts[2]
	patchNum := 0
	fmt.Sscanf(patch, "%d", &patchNum)

	if strings.Contains(msg, "breaking change") || strings.Contains(msg, "breaking:") {
		return fmt.Sprintf("v%s.%d.0", major, mustAtoi(minor)+1)
	}
	if strings.Contains(msg, "feat:") || strings.Contains(msg, "feat(") {
		return fmt.Sprintf("v%s.%d.0", major, mustAtoi(minor)+1)
	}
	return fmt.Sprintf("v%s.%s.%d", major, minor, patchNum+1)
}

func mustAtoi(s string) int {
	var n int
	fmt.Sscanf(s, "%d", &n)
	return n
}

// ── CHANGELOG management ────────────────────────────────────

// UpdateChangelog prepends new release notes to CHANGELOG.md.
func UpdateChangelog(repoPath string, notes *Notes) error {
	path := repoPath + "/CHANGELOG.md"
	existing := ""
	if data, err := os.ReadFile(path); err == nil {
		existing = string(data)
	}

	newContent := notes.RawMarkdown + "\n\n" + existing
	return os.WriteFile(path, []byte(newContent), 0o644)
}
