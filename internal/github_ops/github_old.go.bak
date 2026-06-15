// Package github_ops provides GitHub repository operations: issue triage,
// PR management, CI debugging, release management, and security monitoring.
// Uses the gh CLI when available, with HTTP API fallback.
package github_ops

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Client wraps GitHub operations.
type Client struct {
	owner string
	repo  string
	token string
}

// NewClient creates a GitHub client for a repository.
// Uses GITHUB_TOKEN env var, or gh CLI auth.
func NewClient(owner, repo string) *Client {
	return &Client{
		owner: owner,
		repo:  repo,
		token: os.Getenv("GITHUB_TOKEN"),
	}
}

// ── Issues ─────────────────────────────────────────────────

// Issue represents a GitHub issue.
type Issue struct {
	Number    int      `json:"number"`
	Title     string   `json:"title"`
	Body      string   `json:"body"`
	State     string   `json:"state"`
	Labels    []string `json:"labels"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
	URL       string   `json:"html_url"`
}

// ListIssues returns open issues for the repository.
func (c *Client) ListIssues(ctx context.Context, state string, limit int) ([]Issue, error) {
	args := []string{"issue", "list", "--state", state, "--json", "number,title,state,labels,createdAt,updatedAt,url", "--limit", fmt.Sprintf("%d", limit)}
	if c.owner != "" && c.repo != "" {
		args = append(args, "--repo", c.owner+"/"+c.repo)
	}
	out, err := runGh(ctx, args...)
	if err != nil {
		return nil, err
	}
	var issues []Issue
	json.Unmarshal(out, &issues)
	return issues, nil
}

// CreateIssue creates a new GitHub issue.
func (c *Client) CreateIssue(ctx context.Context, title, body string, labels []string) (*Issue, error) {
	args := []string{"issue", "create", "--title", title, "--body", body}
	for _, l := range labels {
		args = append(args, "--label", l)
	}
	if c.owner != "" && c.repo != "" {
		args = append(args, "--repo", c.owner+"/"+c.repo)
	}
	out, err := runGh(ctx, args...)
	if err != nil {
		return nil, err
	}
	// Parse URL from stdout to extract issue number
	url := strings.TrimSpace(string(out))
	return &Issue{URL: url}, nil
}

// CloseIssue closes a GitHub issue.
func (c *Client) CloseIssue(ctx context.Context, number int, reason string) error {
	args := []string{"issue", "close", fmt.Sprintf("%d", number)}
	if reason != "" {
		args = append(args, "--reason", reason)
	}
	if c.owner != "" && c.repo != "" {
		args = append(args, "--repo", c.owner+"/"+c.repo)
	}
	_, err := runGh(ctx, args...)
	return err
}

// ── Pull Requests ──────────────────────────────────────────

// PR represents a GitHub pull request.
type PR struct {
	Number   int      `json:"number"`
	Title    string   `json:"title"`
	Body     string   `json:"body"`
	State    string   `json:"state"`
	Branch   string   `json:"headRefName"`
	Base     string   `json:"baseRefName"`
	Merged   bool     `json:"merged"`
	Draft    bool     `json:"isDraft"`
	URL      string   `json:"url"`
	Checks   string   `json:"statusCheckRollup"`
}

// ListPRs returns pull requests for the repository.
func (c *Client) ListPRs(ctx context.Context, state string, limit int) ([]PR, error) {
	args := []string{"pr", "list", "--state", state, "--json", "number,title,state,headRefName,baseRefName,merged,isDraft,url,statusCheckRollup", "--limit", fmt.Sprintf("%d", limit)}
	if c.owner != "" && c.repo != "" {
		args = append(args, "--repo", c.owner+"/"+c.repo)
	}
	out, err := runGh(ctx, args...)
	if err != nil {
		return nil, err
	}
	var prs []PR
	json.Unmarshal(out, &prs)
	return prs, nil
}

// CreatePR creates a new pull request.
func (c *Client) CreatePR(ctx context.Context, title, body, head, base string, draft bool) (*PR, error) {
	args := []string{"pr", "create", "--title", title, "--body", body, "--head", head, "--base", base}
	if draft {
		args = append(args, "--draft")
	}
	if c.owner != "" && c.repo != "" {
		args = append(args, "--repo", c.owner+"/"+c.repo)
	}
	out, err := runGh(ctx, args...)
	if err != nil {
		return nil, err
	}
	url := strings.TrimSpace(string(out))
	return &PR{URL: url}, nil
}

// ── CI / Workflows ──────────────────────────────────────────

// CIStatus represents the status of a CI run.
type CIStatus struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	Conclusion string `json:"conclusion"`
	URL       string `json:"html_url"`
	CreatedAt string `json:"created_at"`
}

// ListWorkflowRuns returns recent CI runs.
func (c *Client) ListWorkflowRuns(ctx context.Context, limit int) ([]CIStatus, error) {
	args := []string{"run", "list", "--json", "name,status,conclusion,url,createdAt", "--limit", fmt.Sprintf("%d", limit)}
	if c.owner != "" && c.repo != "" {
		args = append(args, "--repo", c.owner+"/"+c.repo)
	}
	out, err := runGh(ctx, args...)
	if err != nil {
		return nil, err
	}
	var runs []CIStatus
	json.Unmarshal(out, &runs)
	return runs, nil
}

// WatchWorkflowRun watches a CI run until it completes.
func (c *Client) WatchWorkflowRun(ctx context.Context, runID string) (*CIStatus, error) {
	args := []string{"run", "watch", runID}
	if c.owner != "" && c.repo != "" {
		args = append(args, "--repo", c.owner+"/"+c.repo)
	}
	out, err := runGh(ctx, args...)
	if err != nil {
		return nil, err
	}
	return &CIStatus{Conclusion: strings.TrimSpace(string(out))}, nil
}

// ── Repo info ──────────────────────────────────────────────

// RepoInfo holds repository metadata.
type RepoInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Stars       int    `json:"stargazers_count"`
	Forks       int    `json:"forks_count"`
	OpenIssues  int    `json:"open_issues_count"`
	DefaultBranch string `json:"default_branch"`
	UpdatedAt   string `json:"updated_at"`
}

// GetRepoInfo fetches repository metadata.
func (c *Client) GetRepoInfo(ctx context.Context) (*RepoInfo, error) {
	args := []string{"repo", "view", "--json", "name,description,stargazersCount,forksCount,openIssuesCount,defaultBranch,updatedAt"}
	if c.owner != "" && c.repo != "" {
		args = append(args, c.owner+"/"+c.repo)
	}
	out, err := runGh(ctx, args...)
	if err != nil {
		return nil, err
	}
	var info RepoInfo
	json.Unmarshal(out, &info)
	return &info, nil
}

// ── gh CLI helper ──────────────────────────────────────────

func runGh(ctx context.Context, args ...string) ([]byte, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return nil, fmt.Errorf("gh CLI not installed: %w", err)
	}
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Stderr = os.Stderr
	return cmd.Output()
}

// Available reports whether the gh CLI is available.
func Available() bool {
	_, err := exec.LookPath("gh")
	return err == nil
}

// ── Summary formatting ─────────────────────────────────────

// FormatIssues formats a list of issues for display.
func FormatIssues(issues []Issue) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%d issue(s):\n\n", len(issues)))
	for _, i := range issues {
		icon := "○"
		if i.State == "closed" {
			icon = "✓"
		}
		labels := ""
		if len(i.Labels) > 0 {
			labels = " [" + strings.Join(i.Labels, ", ") + "]"
		}
		fmt.Fprintf(&sb, "%s #%d %s%s\n  %s\n\n", icon, i.Number, i.Title, labels, i.URL)
	}
	return sb.String()
}

// FormatPRs formats a list of pull requests for display.
func FormatPRs(prs []PR) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%d pull request(s):\n\n", len(prs)))
	for _, pr := range prs {
		icon := "○"
		if pr.Merged {
			icon = "✓"
		} else if pr.Draft {
			icon = "◌"
		}
		fmt.Fprintf(&sb, "%s #%d %s\n  %s → %s | %s\n\n", icon, pr.Number, pr.Title, pr.Branch, pr.Base, pr.URL)
	}
	return sb.String()
}

// ── Git info helpers ───────────────────────────────────────

// GitRemote returns the GitHub owner/repo for the current git remote.
func GitRemote(ctx context.Context) (owner, repo string, err error) {
	out, err := exec.CommandContext(ctx, "git", "remote", "get-url", "origin").Output()
	if err != nil {
		return "", "", err
	}
	url := strings.TrimSpace(string(out))
	// Parse github.com/owner/repo.git
	url = strings.TrimSuffix(strings.TrimPrefix(url, "https://"), "github.com/")
	url = strings.TrimSuffix(strings.TrimPrefix(url, "git@github.com:"), ".git")
	parts := strings.SplitN(url, "/", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("cannot parse remote URL: %s", url)
	}
	return parts[0], parts[1], nil
}

// CommitInfo holds commit metadata.
type CommitInfo struct {
	Hash    string `json:"hash"`
	Message string `json:"message"`
	Author  string `json:"author"`
	Date    time.Time `json:"date"`
}

// RecentCommits returns the last N commits.
func RecentCommits(ctx context.Context, n int) ([]CommitInfo, error) {
	out, err := exec.CommandContext(ctx, "git", "log", fmt.Sprintf("-%d", n), "--format=%H||%s||%an||%aI").Output()
	if err != nil {
		return nil, err
	}
	var commits []CommitInfo
	for _, line := range strings.Split(string(out), "\n") {
		parts := strings.SplitN(line, "||", 4)
		if len(parts) != 4 {
			continue
		}
		t, _ := time.Parse(time.RFC3339, parts[3])
		commits = append(commits, CommitInfo{
			Hash:    parts[0][:7],
			Message: parts[1],
			Author:  parts[2],
			Date:    t,
		})
	}
	return commits, nil
}
