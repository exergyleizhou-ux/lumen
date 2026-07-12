package builtin

import (
	"context"
	"encoding/json"
	"fmt"

	"lumen/internal/github_ops"
	"lumen/internal/tool"
)

func init() {
	tool.RegisterBuiltin(&GitHubPRCreateTool{})
	tool.RegisterBuiltin(&GitHubPRListTool{})
	tool.RegisterBuiltin(&GitHubCIStatusTool{})
	tool.RegisterBuiltin(&GitHubIssueCreateTool{})
	tool.RegisterBuiltin(&GitHubSearchCodeTool{})
}

type GitHubPRCreateTool struct{ client *github_ops.Client }

func (t *GitHubPRCreateTool) Name() string   { return "github_create_pr" }
func (t *GitHubPRCreateTool) ReadOnly() bool { return false }
func (t *GitHubPRCreateTool) Description() string {
	return "Create a GitHub Pull Request. Requires gh CLI auth or GITHUB_TOKEN."
}
func (t *GitHubPRCreateTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"owner":{"type":"string"},"repo":{"type":"string"},"title":{"type":"string"},"body":{"type":"string"},"head":{"type":"string"},"base":{"type":"string","default":"main"}},"required":["owner","repo","title","head"]}`)
}
func (t *GitHubPRCreateTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ Owner, Repo, Title, Body, Head, Base string }
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	if p.Base == "" {
		p.Base = "main"
	}
	client := github_ops.NewClient(p.Owner, p.Repo)
	pr, err := client.CreatePR(ctx, p.Title, p.Body, p.Head, p.Base)
	if err != nil {
		return "", fmt.Errorf("create PR: %w", err)
	}
	out, _ := json.MarshalIndent(pr, "", "  ")
	return string(out), nil
}

type GitHubPRListTool struct{}

func (t *GitHubPRListTool) Name() string   { return "github_list_prs" }
func (t *GitHubPRListTool) ReadOnly() bool { return true }
func (t *GitHubPRListTool) Description() string {
	return "List open Pull Requests for a GitHub repository."
}
func (t *GitHubPRListTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"owner":{"type":"string"},"repo":{"type":"string"},"state":{"type":"string","default":"open"}},"required":["owner","repo"]}`)
}
func (t *GitHubPRListTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ Owner, Repo, State string }
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	if p.State == "" {
		p.State = "open"
	}
	client := github_ops.NewClient(p.Owner, p.Repo)
	prs, err := client.ListPRs(ctx, p.State)
	if err != nil {
		return "", fmt.Errorf("list PRs: %w", err)
	}
	return github_ops.FormatPRs(prs), nil
}

type GitHubCIStatusTool struct{}

func (t *GitHubCIStatusTool) Name() string   { return "github_ci_status" }
func (t *GitHubCIStatusTool) ReadOnly() bool { return true }
func (t *GitHubCIStatusTool) Description() string {
	return "Check CI/CD status for a commit ref on GitHub."
}
func (t *GitHubCIStatusTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"owner":{"type":"string"},"repo":{"type":"string"},"ref":{"type":"string"}},"required":["owner","repo","ref"]}`)
}
func (t *GitHubCIStatusTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ Owner, Repo, Ref string }
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	client := github_ops.NewClient(p.Owner, p.Repo)
	ci, err := client.GetCIStatus(ctx, p.Ref)
	if err != nil {
		return "", fmt.Errorf("CI status: %w", err)
	}
	return github_ops.FormatCI(ci), nil
}

type GitHubIssueCreateTool struct{}

func (t *GitHubIssueCreateTool) Name() string   { return "github_create_issue" }
func (t *GitHubIssueCreateTool) ReadOnly() bool { return false }
func (t *GitHubIssueCreateTool) Description() string {
	return "Create a GitHub Issue with optional labels."
}
func (t *GitHubIssueCreateTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"owner":{"type":"string"},"repo":{"type":"string"},"title":{"type":"string"},"body":{"type":"string"},"labels":{"type":"array","items":{"type":"string"}}},"required":["owner","repo","title"]}`)
}
func (t *GitHubIssueCreateTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Owner, Repo, Title, Body string
		Labels                   []string
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	client := github_ops.NewClient(p.Owner, p.Repo)
	is, err := client.CreateIssue(ctx, p.Title, p.Body, p.Labels)
	if err != nil {
		return "", fmt.Errorf("create issue: %w", err)
	}
	out, _ := json.MarshalIndent(is, "", "  ")
	return string(out), nil
}

type GitHubSearchCodeTool struct{}

func (t *GitHubSearchCodeTool) Name() string        { return "github_search_code" }
func (t *GitHubSearchCodeTool) ReadOnly() bool      { return true }
func (t *GitHubSearchCodeTool) Description() string { return "Search code across GitHub repositories." }
func (t *GitHubSearchCodeTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"Search query (e.g. 'org:kubernetes filename:go context')"}},"required":["query"]}`)
}
func (t *GitHubSearchCodeTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ Query string }
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	client := github_ops.NewClient("", "")
	results, err := client.SearchCode(ctx, p.Query)
	if err != nil {
		return "", fmt.Errorf("search code: %w", err)
	}
	out, _ := json.MarshalIndent(results, "", "  ")
	return string(out), nil
}
