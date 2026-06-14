package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"lumen/internal/evidence"
	"lumen/internal/tool"
)

func init() {
	tool.RegisterBuiltin(&WebFetchTool{})
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

	client := &http.Client{Timeout: 30 * time.Second}
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
	return content, nil
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
	// In headless mode or when no asker is wired, return a "decide for yourself" result.
	// The agent detects the asker from the context.
	return "[ask tool called — in headless mode, decide for yourself and proceed]", nil
}
