package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"lumen/internal/codesearch"
	"lumen/internal/tool"
)

func init() {
	tool.RegisterBuiltin(&CodeSearchTool{})
}

// ── Shared index ─────────────────────────────────────────────

var (
	searchIdx     *codesearch.Index
	searchIdxOnce sync.Once
	searchIdxDir  string
	searchIdxErr  error
)

// SetCodeSearchDir wires the root directory for code indexing.
func SetCodeSearchDir(dir string) {
	searchIdxDir = dir
}

func getIndex() (*codesearch.Index, error) {
	searchIdxOnce.Do(func() {
		if searchIdxDir == "" {
			searchIdxDir, _ = os.Getwd()
		}
		// Narrow to git root (or CWD) to avoid indexing entire home dir
		root := findProjectRoot(searchIdxDir)
		searchIdx = codesearch.New()
		searchIdxErr = searchIdx.IndexDir(root)
	})
	return searchIdx, searchIdxErr
}

func findProjectRoot(start string) string {
	// Walk up to find .git or go.mod
	for dir := start; dir != "/" && dir != "."; {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		if _, err := os.Stat(filepath.Join(dir, "package.json")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return start // fallback to original
}

// ── code_search tool ─────────────────────────────────────────

type CodeSearchTool struct{}

func (t *CodeSearchTool) Name() string        { return "code_search" }
func (t *CodeSearchTool) Description() string { return "Search the codebase with a natural-language query. Returns ranked file:line snippets based on TF-IDF relevance. Use for finding functions, types, patterns, or any code by description." }
func (t *CodeSearchTool) ReadOnly() bool      { return true }

func (t *CodeSearchTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {"type": "string", "description": "Natural language description of the code you're looking for. E.g. 'authentication middleware', 'SQL query builder', 'rate limiter implementation'."},
			"top_k": {"type": "integer", "description": "Number of results to return (default 10, max 20)."}
		},
		"required": ["query"]
	}`)
}

func (t *CodeSearchTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var req struct {
		Query string `json:"query"`
		TopK  int    `json:"top_k"`
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return "", fmt.Errorf("code_search: %w", err)
	}
	if req.Query == "" {
		return "", fmt.Errorf("code_search: query is required")
	}
	if req.TopK <= 0 {
		req.TopK = 10
	}
	if req.TopK > 20 {
		req.TopK = 20
	}

	idx, err := getIndex()
	if err != nil {
		return "", fmt.Errorf("code_search: index error: %w", err)
	}

	results := idx.Search(req.Query, req.TopK)
	if len(results) == 0 {
		return "No results found. Try a different query or broader terms.", nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%d results for \"%s\":\n\n", len(results), req.Query))
	for i, r := range results {
		// Truncate snippet to 300 chars
		snip := r.Snippet
		if len(snip) > 300 {
			snip = cutRunes(snip, 297) + "..."
		}
		// Strip common project prefix for cleaner display
		path := r.Path
		if searchIdxDir != "" && strings.HasPrefix(path, searchIdxDir) {
			path = strings.TrimPrefix(path, searchIdxDir)
			path = strings.TrimPrefix(path, "/")
		}
		// Relativize to CWD
		if wd, err := os.Getwd(); err == nil {
			if rel, err := filepath.Rel(wd, r.Path); err == nil {
				path = rel
			}
		}
		sb.WriteString(fmt.Sprintf("%d. %s:%d (score=%.3f)\n%s\n\n",
			i+1, path, r.Line, r.Score, snip))
	}

	stats := idx.Stats()
	sb.WriteString(fmt.Sprintf("─ indexed %v documents, %v unique tokens\n",
		stats["documents"], stats["tokens"]))
	return sb.String(), nil
}
