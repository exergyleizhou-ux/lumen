package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"lumen/internal/memory"
	"lumen/internal/tool"
)

func init() {
	tool.RegisterBuiltin(&RememberTool{})
	tool.RegisterBuiltin(&ForgetTool{})
	tool.RegisterBuiltin(&MemoriesTool{})
}

// ── Memory source ────────────────────────────────────────────

var globalMemStore *memory.Store

// SetMemStore wires the shared memory store for the builtin tools package.
func SetMemStore(s *memory.Store) { globalMemStore = s }

// ── remember tool ────────────────────────────────────────────

type RememberTool struct{}

func (t *RememberTool) Name() string        { return "remember" }
func (t *RememberTool) Description() string { return "Save a fact to persistent user memory. Memories survive across sessions and are loaded into the system prompt on startup. Use for user preferences, project constraints, or important facts worth keeping." }
func (t *RememberTool) ReadOnly() bool      { return false }

func (t *RememberTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {"type": "string", "description": "Short kebab-case slug (e.g., 'prefers-tabs'). Reuse to update an existing memory."},
			"title": {"type": "string", "description": "Human-readable label shown in the memory index."},
			"body": {"type": "string", "description": "Full markdown body of the memory. For feedback/project types, include '**Why:**' and '**How to apply:**' lines."},
			"description": {"type": "string", "description": "One-line summary shown in the index."},
			"kind": {"type": "string", "enum": ["user", "feedback", "project", "reference"], "description": "Category of the fact."}
		},
		"required": ["name", "body", "description"]
	}`)
}

func (t *RememberTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	if globalMemStore == nil {
		return "", fmt.Errorf("memory store not available")
	}
	var req struct {
		Name        string `json:"name"`
		Title       string `json:"title"`
		Body        string `json:"body"`
		Description string `json:"description"`
		Kind        string `json:"kind"`
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return "", fmt.Errorf("remember: %w", err)
	}
	if req.Name == "" {
		return "", fmt.Errorf("remember: name is required")
	}

	kind := memory.KindUser
	switch req.Kind {
	case "feedback":
		kind = memory.KindFeedback
	case "project":
		kind = memory.KindProject
	case "reference":
		kind = memory.KindReference
	}

	entry := memory.Entry{
		Name:        req.Name,
		Title:       req.Title,
		Body:        req.Body,
		Description: req.Description,
		Kind:        kind,
	}
	if err := globalMemStore.Save(entry); err != nil {
		return "", fmt.Errorf("remember: %w", err)
	}
	return fmt.Sprintf("✓ memory '%s' saved (%s)", req.Name, kind), nil
}

// ── forget tool ──────────────────────────────────────────────

type ForgetTool struct{}

func (t *ForgetTool) Name() string        { return "forget" }
func (t *ForgetTool) Description() string { return "Delete a saved memory by name. Use when a memory is wrong, stale, or superseded." }
func (t *ForgetTool) ReadOnly() bool      { return false }

func (t *ForgetTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {"type": "string", "description": "Slug of the memory to delete."}
		},
		"required": ["name"]
	}`)
}

func (t *ForgetTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	if globalMemStore == nil {
		return "", fmt.Errorf("memory store not available")
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return "", fmt.Errorf("forget: %w", err)
	}
	if err := globalMemStore.Delete(req.Name); err != nil {
		return "", fmt.Errorf("forget: %w", err)
	}
	return fmt.Sprintf("✓ memory '%s' deleted", req.Name), nil
}

// ── memories tool ────────────────────────────────────────────

type MemoriesTool struct{}

func (t *MemoriesTool) Name() string        { return "memories" }
func (t *MemoriesTool) Description() string { return "List all saved user memories with their names, titles, kinds, and descriptions." }
func (t *MemoriesTool) ReadOnly() bool      { return true }

func (t *MemoriesTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type": "object", "properties": {}}`)
}

func (t *MemoriesTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	if globalMemStore == nil {
		return "", fmt.Errorf("memory store not available")
	}
	entries := globalMemStore.List()
	if len(entries) == 0 {
		return "No memories saved yet. Use /remember to save facts, preferences, or guidance.", nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%d memories:\n\n", len(entries)))
	for _, e := range entries {
		ts := e.UpdatedAt.Format(time.RFC3339)
		sb.WriteString(fmt.Sprintf("- **[%s](%s)** (%s) — %s\n  _updated %s_\n",
			e.Title, e.Name, e.Kind, e.Description, ts))
	}
	return sb.String(), nil
}
