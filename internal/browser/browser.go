// Package browser provides headless browser integration via Chrome DevTools
// Protocol. It enables web page content extraction, screenshots, and JS
// execution. Requires chromium or google-chrome in PATH.
// Adapted from claw-code's Chrome Use feature.
package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Client controls a headless Chrome instance via CDP.
type Client struct {
	mu       sync.Mutex
	debugURL string
}

// NewClient attempts to launch headless Chrome via CDP.
func NewClient() (*Client, error) {
	chrome := findChrome()
	if chrome == "" {
		return nil, fmt.Errorf("chrome/chromium not found in PATH")
	}

	cmd := exec.Command(chrome,
		"--headless=new", "--disable-gpu", "--no-sandbox",
		"--disable-dev-shm-usage", "--remote-debugging-port=0",
		"--remote-debugging-address=127.0.0.1",
		"--user-data-dir=/tmp/lumen-chrome", "about:blank",
	)
	cmd.Start()
	time.Sleep(500 * time.Millisecond)
	return &Client{debugURL: "http://127.0.0.1:9222"}, nil
}

func ExistingClient(url string) *Client { return &Client{debugURL: url} }

func findChrome() string {
	for _, c := range []string{"chromium", "google-chrome", "google-chrome-stable"} {
		if p, err := exec.LookPath(c); err == nil {
			return p
		}
	}
	return ""
}

func (c *Client) Navigate(ctx context.Context, pageURL string) error {
	return c.send(ctx, "Page.navigate", map[string]any{"url": pageURL}, nil)
}

func (c *Client) ExtractText(ctx context.Context) (string, error) {
	var result struct {
		Result struct{ Value string `json:"value"` } `json:"result"`
	}
	if err := c.send(ctx, "Runtime.evaluate", map[string]any{
		"expression": "document.body.innerText", "returnByValue": true,
	}, &result); err != nil {
		return "", err
	}
	return result.Result.Value, nil
}

func (c *Client) GetTitle(ctx context.Context) (string, error) {
	var result struct {
		Result struct{ Value string `json:"value"` } `json:"result"`
	}
	if err := c.send(ctx, "Runtime.evaluate", map[string]any{
		"expression": "document.title", "returnByValue": true,
	}, &result); err != nil {
		return "", err
	}
	return result.Result.Value, nil
}

func (c *Client) ExecuteJS(ctx context.Context, js string) (string, error) {
	var result struct {
		Result struct{ Value json.RawMessage `json:"value"` } `json:"result"`
	}
	if err := c.send(ctx, "Runtime.evaluate", map[string]any{
		"expression": js, "returnByValue": true,
	}, &result); err != nil {
		return "", err
	}
	return string(result.Result.Value), nil
}

type cdpRequest struct {
	ID     int            `json:"id"`
	Method string         `json:"method"`
	Params map[string]any `json:"params,omitempty"`
}

type cdpResponse struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (c *Client) send(ctx context.Context, method string, params map[string]any, result any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	req := cdpRequest{ID: 1, Method: method, Params: params}
	body, _ := json.Marshal(req)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(c.debugURL, "/")+"/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("cdp request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("cdp call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("cdp HTTP %d: %s", resp.StatusCode, b)
	}
	var cdpResp cdpResponse
	if err := json.NewDecoder(resp.Body).Decode(&cdpResp); err != nil {
		return fmt.Errorf("cdp decode: %w", err)
	}
	if cdpResp.Error != nil {
		return fmt.Errorf("cdp error: %s", cdpResp.Error.Message)
	}
	if result != nil {
		return json.Unmarshal(cdpResp.Result, result)
	}
	return nil
}

// ── Fetch helpers ──────────────────────────────────────────

func FetchText(ctx context.Context, pageURL string) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	req.Header.Set("User-Agent", "Lumen/1.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	return stripHTML(string(body)), nil
}

func stripHTML(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
		} else if r == '>' {
			inTag = false
		} else if !inTag {
			b.WriteRune(r)
		}
	}
	var out []string
	for _, line := range strings.Split(b.String(), "\n") {
		if t := strings.TrimSpace(line); t != "" {
			out = append(out, t)
		}
	}
	return strings.Join(out, "\n")
}

func Available() bool { return findChrome() != "" }
