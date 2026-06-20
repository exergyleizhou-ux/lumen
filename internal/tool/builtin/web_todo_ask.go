package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"lumen/internal/event"
	"lumen/internal/evidence"
	"lumen/internal/tool"
	"lumen/internal/untrusted"
	"lumen/internal/websearch"
)

// sharedHTTP is a package-level HTTP client with keep-alive.
// Creating a new http.Client per web_fetch call destroys connection
// reuse — every call pays TCP+TLS handshake cost (~50-300ms).
var sharedHTTP = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
			// SSRF guard: validate the ACTUAL resolved IP at connect time, which
			// also defeats DNS rebinding. See ssrf.go.
			Control: ssrfDialControl,
		}).DialContext,
		MaxIdleConns:          20,
		MaxIdleConnsPerHost:   5,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
	},
}

func init() {
	tool.RegisterBuiltin(&WebFetchTool{})
	tool.RegisterBuiltin(&WebSearchTool{})
	tool.RegisterBuiltin(&TodoWriteTool{})
	tool.RegisterBuiltin(&CompleteStepTool{})
	tool.RegisterBuiltin(&AskTool{})
}

// WebFetchTool fetches a URL over HTTPS and returns its content as text.
type WebFetchTool struct{}

func (t *WebFetchTool) Name() string   { return "web_fetch" }
func (t *WebFetchTool) ReadOnly() bool { return true }

func (t *WebFetchTool) Description() string {
	return "Fetch a URL over HTTPS/HTTP and return its text content. HTML pages are reduced to readable text (scripts, styles, tags stripped, whitespace collapsed); JSON / plain text / markdown bodies come back verbatim."
}

func (t *WebFetchTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "url":{"type":"string","description":"Absolute URL beginning with http:// or https://"}
},
"required":["url"]
}`)
}

func (t *WebFetchTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.URL == "" {
		return "", fmt.Errorf("url is required")
	}
	// SSRF pre-flight: reject non-http(s) schemes and known-bad literal-IP hosts
	// before any connection. The dialer Control hook (sharedHTTP) is the
	// authoritative, rebinding-safe check for hostname targets and redirects.
	if err := checkFetchURL(p.URL); err != nil {
		return "", err
	}

	client := sharedHTTP // package-level keep-alive pool
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.URL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Lumen/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return "", err
	}

	content := string(body)
	contentType := resp.Header.Get("Content-Type")

	if strings.Contains(contentType, "text/html") {
		content = stripHTML(content)
	}
	// Fetched web content is untrusted external data. Wrap it so the model
	// treats it as information, not as instructions to follow (indirect prompt
	// injection mitigation; docs/threat-model.md §7 G3).
	return untrusted.Wrap(p.URL, content), nil
}

func stripHTML(s string) string {
	// Simple HTML tag removal — sufficient for readable text extraction.
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	// Collapse whitespace
	lines := strings.Split(b.String(), "\n")
	var out []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return strings.Join(out, "\n")
}

// TodoWriteTool records and updates a structured task list.
type TodoWriteTool struct{}

func (t *TodoWriteTool) Name() string   { return "todo_write" }
func (t *TodoWriteTool) ReadOnly() bool { return false }

func (t *TodoWriteTool) Description() string {
	return "Record and update a structured task list for the current work. Send the COMPLETE list every call — it replaces the previous one."
}

func (t *TodoWriteTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "todos":{"type":"array","items":{
    "type":"object",
    "properties":{
      "content":{"type":"string","description":"Imperative description of the task."},
      "status":{"type":"string","enum":["pending","in_progress","completed"]},
      "activeForm":{"type":"string","description":"Present-continuous form shown while the task is in progress."},
      "level":{"type":"integer","description":"Nesting level: 0 = phase/milestone, 1 = a sub-step."}
    },
    "required":["content","status"]
  }}
},
"required":["todos"]
}`)
}

func (t *TodoWriteTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Todos []struct {
			Content    string `json:"content"`
			Status     string `json:"status"`
			ActiveForm string `json:"activeForm"`
			Level      int    `json:"level"`
		} `json:"todos"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}

	var sb strings.Builder
	sb.WriteString("Task list updated:\n")
	for i, td := range p.Todos {
		prefix := "  "
		if td.Level == 0 {
			prefix = ""
		}
		icon := "○"
		switch td.Status {
		case "in_progress":
			icon = "◉"
		case "completed":
			icon = "✓"
		}
		fmt.Fprintf(&sb, "%s%s %s\n", prefix, icon, td.Content)
		_ = i
	}
	return sb.String(), nil
}

// CompleteStepTool signs off a completed step with evidence.
type CompleteStepTool struct{}

func (t *CompleteStepTool) Name() string   { return "complete_step" }
func (t *CompleteStepTool) ReadOnly() bool { return false }

func (t *CompleteStepTool) Description() string {
	return "Record the evidence-backed completion of ONE step of an approved plan. Call it as you finish each step instead of silently moving on."
}

func (t *CompleteStepTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "step":{"type":"string","description":"Which plan step this completes — its title or number, matching the task list."},
  "result":{"type":"string","description":"What is now true or changed as a result of finishing this step."},
  "evidence":{"type":"array","items":{
    "type":"object",
    "properties":{
      "kind":{"type":"string","enum":["verification","diff","files","manual"]},
      "summary":{"type":"string","description":"The evidence itself."},
      "command":{"type":"string","description":"The command run, for verification evidence."},
      "paths":{"type":"array","items":{"type":"string"}}
    },
    "required":["kind","summary"]
  }},
  "notes":{"type":"string","description":"Optional caveats, follow-ups, or anything deferred."}
},
"required":["evidence","result","step"]
}`)
}

func (t *CompleteStepTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Step     string                  `json:"step"`
		Result   string                  `json:"result"`
		Notes    string                  `json:"notes"`
		Evidence []evidence.EvidenceItem `json:"evidence"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Step == "" {
		return "", fmt.Errorf("step is required")
	}

	// Validate against the evidence ledger when available
	ledger := evidence.FromContext(ctx)
	if ledger != nil {
		ok, msg := ledger.VerifyEvidence(p.Step, p.Result, p.Evidence)
		if !ok {
			return "", fmt.Errorf(msg)
		}
		return msg, nil
	}

	// No ledger: headless run — accept the claim
	if len(p.Evidence) == 0 {
		return "", fmt.Errorf("at least one evidence item is required")
	}
	return fmt.Sprintf("Step %q completed: %s", p.Step, p.Result), nil
}

// AskTool puts structured multiple-choice questions to the user.
type AskTool struct{}

func (t *AskTool) Name() string   { return "ask" }
func (t *AskTool) ReadOnly() bool { return true }

func (t *AskTool) Description() string {
	return "Ask the user one or more multiple-choice questions when you hit a decision that is genuinely theirs to make."
}

func (t *AskTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "questions":{"type":"array","items":{
    "type":"object",
    "properties":{
      "header":{"type":"string","description":"Very short label for the question."},
      "question":{"type":"string","description":"The full question to ask."},
      "options":{"type":"array","items":{
        "type":"object",
        "properties":{
          "label":{"type":"string","description":"The choice text."},
          "description":{"type":"string","description":"Optional one-line explanation."}
        },
        "required":["label"]
      }},
      "multiSelect":{"type":"boolean"}
    },
    "required":["header","options","question"]
  }}
},
"required":["questions"]
}`)
}

func (t *AskTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	ask, ok := tool.AskerFrom(ctx)
	if !ok {
		// No interactive user attached (one-shot / piped / headless): tell the
		// model to decide for itself rather than blocking forever.
		return "[ask tool called — no interactive user available (headless); decide for yourself and proceed]", nil
	}

	// Parse against the tool's own schema (note: schema uses "multiSelect",
	// event.AskQuestion's JSON tag is "multi_select", so parse explicitly).
	var in struct {
		Questions []struct {
			Header      string `json:"header"`
			Question    string `json:"question"`
			Options     []struct {
				Label       string `json:"label"`
				Description string `json:"description"`
			} `json:"options"`
			MultiSelect bool `json:"multiSelect"`
		} `json:"questions"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("ask: invalid arguments: %w", err)
	}
	if len(in.Questions) == 0 {
		return "[ask tool called with no questions]", nil
	}

	questions := make([]event.AskQuestion, len(in.Questions))
	for i, q := range in.Questions {
		opts := make([]event.AskOption, len(q.Options))
		for j, o := range q.Options {
			opts[j] = event.AskOption{Label: o.Label, Description: o.Description}
		}
		questions[i] = event.AskQuestion{
			Header:      q.Header,
			Question:    q.Question,
			Options:     opts,
			MultiSelect: q.MultiSelect,
		}
	}

	answers, err := ask(ctx, questions)
	if err != nil {
		return "", fmt.Errorf("ask: %w", err)
	}
	return formatAskAnswers(questions, answers), nil
}

// formatAskAnswers renders the user's selections back to the model as plain
// text it can act on.
func formatAskAnswers(questions []event.AskQuestion, answers []event.AskAnswer) string {
	byHeader := make(map[string][]string, len(answers))
	for _, a := range answers {
		byHeader[a.Header] = a.Answers
	}
	var b strings.Builder
	b.WriteString("The user answered:\n")
	for _, q := range questions {
		sel := byHeader[q.Header]
		label := q.Header
		if label == "" {
			label = q.Question
		}
		if len(sel) == 0 {
			fmt.Fprintf(&b, "- %s: (no answer)\n", label)
			continue
		}
		fmt.Fprintf(&b, "- %s: %s\n", label, strings.Join(sel, ", "))
	}
	return strings.TrimRight(b.String(), "\n")
}

// ── Web Search ─────────────────────────────────────────────

// WebSearchTool searches the web via Brave or Bing API.
type WebSearchTool struct{}

func (t *WebSearchTool) Name() string     { return "web_search" }
func (t *WebSearchTool) ReadOnly() bool   { return true }

func (t *WebSearchTool) Description() string {
	return "Search the web using Brave or Bing Search API. Returns title, URL, and description for each result."
}

func (t *WebSearchTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "query":{"type":"string","description":"Search query string."},
  "max_results":{"type":"integer","description":"Maximum number of results (default 10)."}
},
"required":["query"]
}`)
}

func (t *WebSearchTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Query      string `json:"query"`
		MaxResults int    `json:"max_results"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Query == "" {
		return "", fmt.Errorf("query is required")
	}

	engine := websearch.AutoEngine()
	if engine == nil {
		return "", fmt.Errorf("no search engine configured: set BRAVE_API_KEY or BING_API_KEY environment variable")
	}
	if p.MaxResults <= 0 {
		p.MaxResults = 10
	}

	resp, err := engine.Search(ctx, p.Query, p.MaxResults)
	if err != nil {
		return "", fmt.Errorf("search failed: %w", err)
	}
	return websearch.FormatResults(resp), nil
}
