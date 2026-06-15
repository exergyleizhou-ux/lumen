// Package websearch provides real web search capabilities via Brave Search
// API and Bing Web Search API. The agent can invoke web_search to find
// current information, documentation, and code examples.
// Adapted from Reasonix's retrieval package.
package websearch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Result is one search result.
type Result struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
	PublishedAt string `json:"published_at,omitempty"`
}

// Response wraps a set of search results.
type Response struct {
	Query   string   `json:"query"`
	Results []Result `json:"results"`
	Total   int      `json:"total"`
	Source  string   `json:"source"` // "brave" or "bing"
}

// Engine is a web search engine.
type Engine interface {
	Search(ctx context.Context, query string, maxResults int) (*Response, error)
	Name() string
}

// ── Brave Search ───────────────────────────────────────────

// BraveEngine searches via the Brave Search API.
type BraveEngine struct {
	apiKey string
	client *http.Client
}

// NewBraveEngine creates a Brave Search engine. Uses BRAVE_API_KEY env var.
func NewBraveEngine() *BraveEngine {
	return &BraveEngine{
		apiKey: os.Getenv("BRAVE_API_KEY"),
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (e *BraveEngine) Name() string { return "brave" }

func (e *BraveEngine) Search(ctx context.Context, query string, maxResults int) (*Response, error) {
	if e.apiKey == "" {
		return nil, fmt.Errorf("BRAVE_API_KEY not set")
	}
	if maxResults <= 0 {
		maxResults = 10
	}

	reqURL := fmt.Sprintf("https://api.search.brave.com/res/v1/web/search?q=%s&count=%d",
		url.QueryEscape(query), maxResults)

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("X-Subscription-Token", e.apiKey)

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("brave search: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("brave search HTTP %d: %s", resp.StatusCode, string(body))
	}

	var braveResp struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
				Age         string `json:"age"`
			} `json:"results"`
			Total int `json:"total"`
		} `json:"web"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&braveResp); err != nil {
		return nil, fmt.Errorf("brave decode: %w", err)
	}

	results := make([]Result, len(braveResp.Web.Results))
	for i, r := range braveResp.Web.Results {
		results[i] = Result{
			Title:       r.Title,
			URL:         r.URL,
			Description: r.Description,
			PublishedAt: r.Age,
		}
	}
	return &Response{Query: query, Results: results, Total: braveResp.Web.Total, Source: "brave"}, nil
}

// ── Bing Search ────────────────────────────────────────────

// BingEngine searches via the Bing Web Search API.
type BingEngine struct {
	apiKey string
	client *http.Client
}

// NewBingEngine creates a Bing Search engine. Uses BING_API_KEY env var.
func NewBingEngine() *BingEngine {
	return &BingEngine{
		apiKey: os.Getenv("BING_API_KEY"),
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (e *BingEngine) Name() string { return "bing" }

func (e *BingEngine) Search(ctx context.Context, query string, maxResults int) (*Response, error) {
	if e.apiKey == "" {
		return nil, fmt.Errorf("BING_API_KEY not set")
	}
	if maxResults <= 0 {
		maxResults = 10
	}

	reqURL := fmt.Sprintf("https://api.bing.microsoft.com/v7.0/search?q=%s&count=%d",
		url.QueryEscape(query), maxResults)

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	req.Header.Set("Ocp-Apim-Subscription-Key", e.apiKey)

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bing search: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("bing search HTTP %d: %s", resp.StatusCode, string(body))
	}

	var bingResp struct {
		WebPages struct {
			Value []struct {
				Name       string `json:"name"`
				URL        string `json:"url"`
				Snippet    string `json:"snippet"`
				DatePublished string `json:"datePublished"`
			} `json:"value"`
			TotalEstimatedMatches int `json:"totalEstimatedMatches"`
		} `json:"webPages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&bingResp); err != nil {
		return nil, fmt.Errorf("bing decode: %w", err)
	}

	results := make([]Result, len(bingResp.WebPages.Value))
	for i, r := range bingResp.WebPages.Value {
		results[i] = Result{
			Title:       r.Name,
			URL:         r.URL,
			Description: r.Snippet,
			PublishedAt: r.DatePublished,
		}
	}
	return &Response{
		Query:   query,
		Results: results,
		Total:   bingResp.WebPages.TotalEstimatedMatches,
		Source:  "bing",
	}, nil
}

// ── Auto-detect engine ─────────────────────────────────────

// AutoEngine picks the best available search engine.
func AutoEngine() Engine {
	if os.Getenv("BRAVE_API_KEY") != "" {
		return NewBraveEngine()
	}
	if os.Getenv("BING_API_KEY") != "" {
		return NewBingEngine()
	}
	return nil
}

// Available reports whether any search engine is configured.
func Available() bool {
	return AutoEngine() != nil
}

// ── Result formatting ──────────────────────────────────────

// FormatResults formats search results for the model.
func FormatResults(resp *Response) string {
	if resp == nil || len(resp.Results) == 0 {
		return "No results found."
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Search results for %q (via %s):\n\n", resp.Query, resp.Source)
	for i, r := range resp.Results {
		fmt.Fprintf(&sb, "%d. %s\n", i+1, r.Title)
		fmt.Fprintf(&sb, "   URL: %s\n", r.URL)
		fmt.Fprintf(&sb, "   %s\n", r.Description)
		if r.PublishedAt != "" {
			fmt.Fprintf(&sb, "   Published: %s\n", r.PublishedAt)
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

// FormatResultsCompact returns a compact format for context-limited scenarios.
func FormatResultsCompact(resp *Response) string {
	if resp == nil || len(resp.Results) == 0 {
		return "No results."
	}
	var sb strings.Builder
	for i, r := range resp.Results {
		if i >= 5 {
			break
		}
		fmt.Fprintf(&sb, "- [%s](%s): %s\n", r.Title, r.URL, truncateDesc(r.Description, 120))
	}
	return sb.String()
}

func truncateDesc(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
