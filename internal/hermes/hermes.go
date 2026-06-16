// Package hermes — goal-driven autonomous agent. Given a high-level goal,
// breaks it into sub-tasks, executes each with auto-retry, learns from
// telemetry and feedback, and evolves behavior over time. Runs to completion
// without human supervision.
package hermes

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ── Goal ──────────────────────────────────────────────────

type Goal struct {
	ID          string    `json:"id"`
	Statement   string    `json:"statement"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
	Learnings   []string  `json:"learnings"`
}

// ── Knowledge Base ─────────────────────────────────────────

type Pattern struct {
	ID           string    `json:"id"`
	Category     string    `json:"category"`
	Condition    string    `json:"condition"`
	Action       string    `json:"action"`
	Source       string    `json:"source"`
	SuccessCount int       `json:"success_count"`
	FailCount    int       `json:"fail_count"`
	CreatedAt    time.Time `json:"created_at"`
}

type KnowledgeBase struct {
	dir      string
	Patterns []Pattern `json:"patterns"`
}

func NewKnowledgeBase() *KnowledgeBase {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".lumen", "knowledge")
	os.MkdirAll(dir, 0700)
	kb := &KnowledgeBase{dir: dir}
	kb.load()
	return kb
}

func (kb *KnowledgeBase) load() {
	data, err := os.ReadFile(filepath.Join(kb.dir, "patterns.json"))
	if err != nil {
		kb.Patterns = builtinPatterns()
		return
	}
	json.Unmarshal(data, &kb.Patterns)
	if len(kb.Patterns) == 0 {
		kb.Patterns = builtinPatterns()
	}
}

func (kb *KnowledgeBase) save() {
	data, _ := json.MarshalIndent(kb, "", "  ")
	os.WriteFile(filepath.Join(kb.dir, "patterns.json"), data, 0600)
}

func (kb *KnowledgeBase) Learn(category, condition, action, source string) {
	for i, p := range kb.Patterns {
		if p.Condition == condition && p.Category == category {
			kb.Patterns[i].SuccessCount++
			kb.save()
			return
		}
	}
	kb.Patterns = append(kb.Patterns, Pattern{
		ID:           fmt.Sprintf("pat-%d", len(kb.Patterns)+1),
		Category:     category,
		Condition:    condition,
		Action:       action,
		Source:       source,
		SuccessCount: 1,
		CreatedAt:    time.Now(),
	})
	kb.save()
}

func (kb *KnowledgeBase) Match(text string) []Pattern {
	var matches []Pattern
	for _, p := range kb.Patterns {
		if strings.Contains(strings.ToLower(text), strings.ToLower(p.Condition)) {
			matches = append(matches, p)
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].Category < matches[j].Category })
	return matches
}

func builtinPatterns() []Pattern {
	return []Pattern{
		{Category: "avoid", Condition: "rm -rf", Action: "Never run rm -rf without confirmation", Source: "builtin"},
		{Category: "avoid", Condition: "DROP TABLE", Action: "Ask before destructive SQL", Source: "builtin"},
		{Category: "prefer", Condition: "read file", Action: "Use read_file tool instead of bash cat", Source: "builtin"},
		{Category: "note", Condition: "gopls v0.12", Action: "gopls v0.16.2 is stable; v0.12.4 crashes on Go 1.23+", Source: "telemetry"},
		{Category: "fix", Condition: "broken pipe", Action: "Re-run the failing tool — transient gopls connection error", Source: "telemetry"},
	}
}
