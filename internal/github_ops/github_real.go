// Package github_ops provides REAL GitHub API operations via the `gh` CLI
// and direct REST API. Supports PR creation, issue management, CI status,
// release creation, code search, and repository management.
package github_ops

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ── GitHub Client ──────────────────────────────────────────

// Client wraps gh CLI and REST API access.
type Client struct {
	mu      sync.Mutex
	token   string
	owner   string
	repo    string
	baseURL string
	http    *http.Client
}

// NewClient creates a GitHub client using gh CLI auth or a token.
func NewClient(owner, repo string) *Client {
	// Try to get token from gh CLI or environment
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		// Try gh auth token
		out, err := exec.Command("gh", "auth", "token").Output()
		if err == nil {
			token = strings.TrimSpace(string(out))
		}
	}
	return &Client{
		token:   token,
		owner:   owner,
		repo:    repo,
		baseURL: "https://api.github.com",
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// SetToken overrides the auth token.
func (c *Client) SetToken(token string) { c.mu.Lock(); defer c.mu.Unlock(); c.token = token }

// IsAuthenticated checks if we have credentials.
func (c *Client) IsAuthenticated() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.token != "" }

// ── REST Helpers ───────────────────────────────────────────

func (c *Client) get(ctx context.Context, path string) ([]byte, error) {
	return c.request(ctx, "GET", path, nil)
}

func (c *Client) post(ctx context.Context, path string, body any) ([]byte, error) {
	b, _ := json.Marshal(body)
	return c.request(ctx, "POST", path, b)
}

func (c *Client) patch(ctx context.Context, path string, body any) ([]byte, error) {
	b, _ := json.Marshal(body)
	return c.request(ctx, "PATCH", path, b)
}

func (c *Client) put(ctx context.Context, path string, body any) ([]byte, error) {
	b, _ := json.Marshal(body)
	return c.request(ctx, "PUT", path, b)
}

func (c *Client) delete(ctx context.Context, path string) error {
	_, err := c.request(ctx, "DELETE", path, nil)
	return err
}

func (c *Client) request(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	c.mu.Lock()
	token := c.token
	c.mu.Unlock()

	url := c.baseURL + path
	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return respBody, fmt.Errorf("GitHub API %s %s: %d — %s", method, path, resp.StatusCode, string(respBody))
	}
	return respBody, nil
}

// ── Pull Requests ──────────────────────────────────────────

// PRInfo holds PR metadata.
type PRInfo struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	State     string    `json:"state"`
	URL       string    `json:"html_url"`
	User      string    `json:"user"`
	Branch    string    `json:"head_ref"`
	Base      string    `json:"base_ref"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	MergedAt  time.Time `json:"merged_at,omitempty"`
	Labels    []string  `json:"labels"`
}

// CreatePR creates a new pull request.
func (c *Client) CreatePR(ctx context.Context, title, body, head, base string) (*PRInfo, error) {
	resp, err := c.post(ctx, fmt.Sprintf("/repos/%s/%s/pulls", c.owner, c.repo), map[string]any{
		"title": title, "body": body, "head": head, "base": base,
	})
	if err != nil {
		return nil, err
	}
	return parsePR(resp)
}

// GetPR retrieves a pull request by number.
func (c *Client) GetPR(ctx context.Context, number int) (*PRInfo, error) {
	resp, err := c.get(ctx, fmt.Sprintf("/repos/%s/%s/pulls/%d", c.owner, c.repo, number))
	if err != nil {
		return nil, err
	}
	return parsePR(resp)
}

// ListPRs lists pull requests with optional state filter.
func (c *Client) ListPRs(ctx context.Context, state string) ([]PRInfo, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls?state=%s&per_page=30", c.owner, c.repo, state)
	if state == "" {
		path = fmt.Sprintf("/repos/%s/%s/pulls?per_page=30", c.owner, c.repo)
	}
	resp, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}
	var prs []PRInfo
	if err := json.Unmarshal(resp, &prs); err != nil {
		return nil, err
	}
	// Fix: parse nested user field
	var raw []map[string]any
	json.Unmarshal(resp, &raw)
	for i := range prs {
		if u, ok := raw[i]["user"].(map[string]any); ok {
			prs[i].User, _ = u["login"].(string)
		}
		if h, ok := raw[i]["head"].(map[string]any); ok {
			prs[i].Branch, _ = h["ref"].(string)
		}
		if b, ok := raw[i]["base"].(map[string]any); ok {
			prs[i].Base, _ = b["ref"].(string)
		}
		var labels []string
		if lbls, ok := raw[i]["labels"].([]any); ok {
			for _, l := range lbls {
				if lm, ok := l.(map[string]any); ok {
					labels = append(labels, lm["name"].(string))
				}
			}
		}
		prs[i].Labels = labels
	}
	return prs, nil
}

func parsePR(data []byte) (*PRInfo, error) {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	pr := &PRInfo{
		Number: int(raw["number"].(float64)),
		Title:  raw["title"].(string),
		State:  raw["state"].(string),
		URL:    raw["html_url"].(string),
	}
	if u, ok := raw["user"].(map[string]any); ok {
		pr.User, _ = u["login"].(string)
	}
	if h, ok := raw["head"].(map[string]any); ok {
		pr.Branch, _ = h["ref"].(string)
	}
	if b, ok := raw["base"].(map[string]any); ok {
		pr.Base, _ = b["ref"].(string)
	}
	if body, ok := raw["body"].(string); ok {
		pr.Body = body
	}
	return pr, nil
}

// ── Issues ─────────────────────────────────────────────────

// IssueInfo holds issue metadata.
type IssueInfo struct {
	Number int      `json:"number"`
	Title  string   `json:"title"`
	Body   string   `json:"body"`
	State  string   `json:"state"`
	URL    string   `json:"html_url"`
	Labels []string `json:"labels"`
	User   string   `json:"user"`
}

// CreateIssue creates a new issue.
func (c *Client) CreateIssue(ctx context.Context, title, body string, labels []string) (*IssueInfo, error) {
	resp, err := c.post(ctx, fmt.Sprintf("/repos/%s/%s/issues", c.owner, c.repo), map[string]any{
		"title": title, "body": body, "labels": labels,
	})
	if err != nil {
		return nil, err
	}
	var issue IssueInfo
	json.Unmarshal(resp, &issue)
	var raw map[string]any
	json.Unmarshal(resp, &raw)
	if u, ok := raw["user"].(map[string]any); ok {
		issue.User, _ = u["login"].(string)
	}
	issue.Number = int(raw["number"].(float64))
	var lbls []string
	if rawLbls, ok := raw["labels"].([]any); ok {
		for _, l := range rawLbls {
			if lm, ok := l.(map[string]any); ok {
				lbls = append(lbls, lm["name"].(string))
			}
		}
	}
	issue.Labels = lbls
	return &issue, nil
}

// ListIssues lists issues.
func (c *Client) ListIssues(ctx context.Context, state string, labels []string) ([]IssueInfo, error) {
	path := fmt.Sprintf("/repos/%s/%s/issues?state=%s&per_page=30", c.owner, c.repo, state)
	if len(labels) > 0 {
		path += "&labels=" + strings.Join(labels, ",")
	}
	resp, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}
	var issues []IssueInfo
	json.Unmarshal(resp, &issues)
	return issues, nil
}

// ── CI / Actions ───────────────────────────────────────────

// CIStatus is the status of CI checks.
type CIStatus struct {
	State      string     `json:"state"` // success, failure, pending
	TotalCount int        `json:"total_count"`
	Checks     []CheckRun `json:"checks"`
}

// CheckRun is a single CI check.
type CheckRun struct {
	Name        string    `json:"name"`
	Status      string    `json:"status"`
	Conclusion  string    `json:"conclusion"`
	URL         string    `json:"html_url"`
	CompletedAt time.Time `json:"completed_at"`
}

// GetCIStatus gets CI status for a ref.
func (c *Client) GetCIStatus(ctx context.Context, ref string) (*CIStatus, error) {
	path := fmt.Sprintf("/repos/%s/%s/commits/%s/check-runs", c.owner, c.repo, ref)
	resp, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}

	var raw struct {
		TotalCount int `json:"total_count"`
		CheckRuns  []struct {
			Name        string     `json:"name"`
			Status      string     `json:"status"`
			Conclusion  string     `json:"conclusion"`
			HTMLURL     string     `json:"html_url"`
			CompletedAt *time.Time `json:"completed_at"`
		} `json:"check_runs"`
	}
	if err := json.Unmarshal(resp, &raw); err != nil {
		return nil, err
	}

	ci := &CIStatus{TotalCount: raw.TotalCount, State: "pending"}
	successCount := 0
	for _, cr := range raw.CheckRuns {
		check := CheckRun{Name: cr.Name, Status: cr.Status, Conclusion: cr.Conclusion, URL: cr.HTMLURL}
		if cr.CompletedAt != nil {
			check.CompletedAt = *cr.CompletedAt
		}
		ci.Checks = append(ci.Checks, check)
		if cr.Conclusion == "success" {
			successCount++
		}
	}
	if successCount == len(ci.Checks) && len(ci.Checks) > 0 {
		ci.State = "success"
	} else if successCount < len(ci.Checks) {
		ci.State = "failure"
	}
	return ci, nil
}

// WaitCI blocks until CI completes or times out.
func (c *Client) WaitCI(ctx context.Context, ref string, timeout time.Duration) (*CIStatus, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ci, err := c.GetCIStatus(ctx, ref)
		if err != nil {
			return nil, err
		}
		if ci.State == "success" || ci.State == "failure" {
			return ci, nil
		}
		select {
		case <-ctx.Done():
			return ci, ctx.Err()
		case <-time.After(15 * time.Second):
		}
	}
	return nil, fmt.Errorf("timeout waiting for CI after %v", timeout)
}

// ── Releases ────────────────────────────────────────────────

// ReleaseInfo holds release metadata.
type ReleaseInfo struct {
	ID        int64     `json:"id"`
	TagName   string    `json:"tag_name"`
	Name      string    `json:"name"`
	Body      string    `json:"body"`
	URL       string    `json:"html_url"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateRelease creates a GitHub release.
func (c *Client) CreateRelease(ctx context.Context, tagName, name, body string) (*ReleaseInfo, error) {
	resp, err := c.post(ctx, fmt.Sprintf("/repos/%s/%s/releases", c.owner, c.repo), map[string]any{
		"tag_name": tagName, "name": name, "body": body,
	})
	if err != nil {
		return nil, err
	}
	var rel ReleaseInfo
	json.Unmarshal(resp, &rel)
	return &rel, nil
}

// ── Search ──────────────────────────────────────────────────

// CodeSearchResult is a single code search hit.
type CodeSearchResult struct {
	Repo      string   `json:"repository"`
	Path      string   `json:"path"`
	URL       string   `json:"html_url"`
	Fragments []string `json:"text_matches"`
}

// SearchCode searches code across GitHub.
func (c *Client) SearchCode(ctx context.Context, query string) ([]CodeSearchResult, error) {
	path := fmt.Sprintf("/search/code?q=%s", query)
	resp, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Items []struct {
			Repository struct {
				FullName string `json:"full_name"`
			} `json:"repository"`
			Path        string `json:"path"`
			HTMLURL     string `json:"html_url"`
			TextMatches []struct {
				Fragment string `json:"fragment"`
			} `json:"text_matches"`
		} `json:"items"`
	}
	json.Unmarshal(resp, &raw)
	var results []CodeSearchResult
	for _, item := range raw.Items {
		var fragments []string
		for _, tm := range item.TextMatches {
			fragments = append(fragments, tm.Fragment)
		}
		results = append(results, CodeSearchResult{Repo: item.Repository.FullName, Path: item.Path, URL: item.HTMLURL, Fragments: fragments})
	}
	return results, nil
}

// ── gh CLI Wrappers ────────────────────────────────────────

// RunGH runs `gh` CLI and returns output.
func RunGH(args ...string) (string, error) {
	cmd := exec.Command("gh", args...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// CreatePRViaCLI creates a PR using gh CLI.
func (c *Client) CreatePRViaCLI(title, body, head, base string) (*PRInfo, error) {
	args := []string{"pr", "create", "--title", title, "--body", body, "--head", head, "--base", base}
	output, err := RunGH(args...)
	if err != nil {
		return nil, fmt.Errorf("gh pr create: %w: %s", err, output)
	}
	// Parse URL from output: https://github.com/owner/repo/pull/N
	parts := strings.Split(output, "/")
	if len(parts) > 0 {
		numStr := parts[len(parts)-1]
		number := 0
		fmt.Sscanf(numStr, "%d", &number)
		return &PRInfo{Number: number, Title: title, Body: body, URL: output, Branch: head, Base: base, State: "open"}, nil
	}
	return &PRInfo{Title: title, URL: output, State: "open"}, nil
}

// MergePR merges a PR via gh CLI.
func (c *Client) MergePR(number int, method string) error {
	if method == "" {
		method = "squash"
	}
	_, err := RunGH("pr", "merge", fmt.Sprintf("%d", number), "--"+method)
	return err
}

// ── Formatters ──────────────────────────────────────────────

// FormatPRs formats a list of PRs.
func FormatPRs(prs []PRInfo) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Pull Requests (%d):\n%s\n\n", len(prs), strings.Repeat("─", 70))
	for _, pr := range prs {
		icon := "🟢"
		switch pr.State {
		case "closed":
			icon = "🔴"
		case "merged":
			icon = "🟣"
		}
		fmt.Fprintf(&sb, "%s #%d %s [%s]\n", icon, pr.Number, pr.Title, pr.State)
		fmt.Fprintf(&sb, "   %s → %s  by %s  %s\n", pr.Branch, pr.Base, pr.User, pr.URL)
	}
	return sb.String()
}

// FormatCI formats CI status.
func FormatCI(ci *CIStatus) string {
	var sb strings.Builder
	icon := "🟡"
	if ci.State == "success" {
		icon = "✅"
	} else if ci.State == "failure" {
		icon = "🔴"
	}
	fmt.Fprintf(&sb, "CI: %s %s (%d checks):\n%s\n\n", icon, ci.State, ci.TotalCount, strings.Repeat("─", 60))
	for _, c := range ci.Checks {
		checkIcon := "🟡"
		if c.Conclusion == "success" {
			checkIcon = "✅"
		} else if c.Conclusion == "failure" {
			checkIcon = "🔴"
		}
		fmt.Fprintf(&sb, "  %s %-30s %-10s %v\n", checkIcon, c.Name, c.Conclusion, c.CompletedAt.Format("15:04:05"))
	}
	return sb.String()
}

// FormatIssues formats issues.
func FormatIssues(issues []IssueInfo) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Issues (%d):\n%s\n\n", len(issues), strings.Repeat("─", 60))
	for _, is := range issues {
		icon := "🟢"
		if is.State == "closed" {
			icon = "⚫"
		}
		tagStr := ""
		if len(is.Labels) > 0 {
			tagStr = " [" + strings.Join(is.Labels, ", ") + "]"
		}
		fmt.Fprintf(&sb, "%s #%d %s%s @%s\n", icon, is.Number, is.Title, tagStr, is.User)
	}
	return sb.String()
}
