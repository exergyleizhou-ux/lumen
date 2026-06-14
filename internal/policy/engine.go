// Package policy defines declarative policies for the agent: safety rules,
// workspace boundaries, rate limits, and model selection strategies.
// Adapted from claw-code's policy_engine.rs.
package policy

import (
	"fmt"
	"strings"
	"sync"
)

// Rule is one declarative policy rule. All conditions must match for
// the rule to fire. Actions specify what to do when the rule matches.
type Rule struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Priority    int      `json:"priority"` // higher = evaluated first
	Conditions  Condition `json:"conditions"`
	Action      Action   `json:"action"`
}

// Condition specifies when a rule applies.
type Condition struct {
	ToolNames    []string `json:"tool_names,omitempty"`    // empty = all tools
	FilePatterns []string `json:"file_patterns,omitempty"` // glob patterns for file paths
	CommandPatterns []string `json:"command_patterns,omitempty"` // regex for bash commands
	MaxFileSize  int64    `json:"max_file_size,omitempty"`  // 0 = no limit
	ReadOnly     *bool    `json:"read_only,omitempty"`      // nil = don't care
}

// Action specifies what happens when a rule matches.
type Action struct {
	Allow bool   `json:"allow"`
	Reason string `json:"reason,omitempty"`
	Log    bool   `json:"log,omitempty"`
}

// Engine evaluates policy rules against tool calls.
type Engine struct {
	mu    sync.RWMutex
	rules []Rule
}

// NewEngine creates a policy engine with sensible defaults.
func NewEngine() *Engine {
	e := &Engine{}
	e.loadDefaults()
	return e
}

func (e *Engine) loadDefaults() {
	e.rules = []Rule{
		{
			Name:        "block-destructive-root",
			Description: "Block any command that removes files at root level",
			Priority:    100,
			Conditions: Condition{
				ToolNames:       []string{"bash"},
				CommandPatterns: []string{`rm\s+-rf\s+/`, `mkfs`, `dd\s+if=/dev/zero`},
			},
			Action: Action{Allow: false, Reason: "destructive filesystem operation", Log: true},
		},
		{
			Name:        "block-exfiltration",
			Description: "Block data exfiltration via curl/wget/netcat",
			Priority:    100,
			Conditions: Condition{
				ToolNames:       []string{"bash"},
				CommandPatterns: []string{`curl.*-d\s+@`, `wget.*--post-file`, `nc\s+.*<`},
			},
			Action: Action{Allow: false, Reason: "potential data exfiltration", Log: true},
		},
		{
			Name:        "block-sensitive-reads",
			Description: "Block reading sensitive files",
			Priority:    90,
			Conditions: Condition{
				ToolNames:    []string{"read_file", "bash"},
				FilePatterns: []string{"**/.env", "**/.ssh/**", "**/etc/passwd", "**/etc/shadow", "**/id_rsa", "**/credentials"},
			},
			Action: Action{Allow: false, Reason: "sensitive file access", Log: true},
		},
		{
			Name:        "limit-file-size-read",
			Description: "Cap file reads at 10MB",
			Priority:    50,
			Conditions: Condition{
				ToolNames:   []string{"read_file", "grep"},
				MaxFileSize: 10 * 1024 * 1024,
			},
			Action: Action{Allow: false, Reason: "file exceeds 10MB read limit", Log: false},
		},
		{
			Name:        "allow-safe-reads",
			Description: "Allow all read-only tool calls by default",
			Priority:    1,
			Conditions: Condition{
				ReadOnly: boolPtr(true),
			},
			Action: Action{Allow: true},
		},
	}
}

// AddRule appends a policy rule.
func (e *Engine) AddRule(r Rule) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rules = append(e.rules, r)
}

// Evaluate checks a tool call against all rules and returns the highest-
// priority matching action. Returns (true, "") if no rules block it.
func (e *Engine) Evaluate(toolName string, filePath string, command string, fileSize int64, readOnly bool) (allow bool, reason string) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	for _, rule := range e.rules {
		if !e.ruleMatches(rule, toolName, filePath, command, fileSize, readOnly) {
			continue
		}
		return rule.Action.Allow, rule.Action.Reason
	}
	return true, ""
}

func (e *Engine) ruleMatches(rule Rule, toolName, filePath, command string, fileSize int64, readOnly bool) bool {
	cond := rule.Conditions

	// Tool name filter
	if len(cond.ToolNames) > 0 {
		found := false
		for _, t := range cond.ToolNames {
			if t == toolName {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// File pattern filter
	if len(cond.FilePatterns) > 0 && filePath != "" {
		found := false
		for _, pattern := range cond.FilePatterns {
			if matchGlob(pattern, filePath) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Command pattern filter
	if len(cond.CommandPatterns) > 0 && command != "" {
		found := false
		for _, pattern := range cond.CommandPatterns {
			if strings.Contains(strings.ToLower(command), strings.ToLower(pattern)) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// File size filter — rule matches only when file exceeds the limit
	if cond.MaxFileSize > 0 {
		if fileSize <= cond.MaxFileSize {
			return false // under limit, rule doesn't apply
		}
	}

	// Read-only filter
	if cond.ReadOnly != nil && *cond.ReadOnly != readOnly {
		return false
	}

	return true
}

// ── Helpers ────────────────────────────────────────────────

func boolPtr(b bool) *bool { return &b }

func matchGlob(pattern, path string) bool {
	// Simple glob matching
	pattern = strings.ToLower(pattern)
	path = strings.ToLower(path)

	// ** matches anything
	if strings.Contains(pattern, "**") {
		parts := strings.Split(pattern, "**")
		if len(parts) == 2 {
			return strings.Contains(path, strings.Trim(parts[0], "/")) &&
				strings.HasSuffix(path, strings.Trim(parts[1], "/"))
		}
	}

	// * matches within a single path component
	if strings.Contains(pattern, "*") {
		patParts := strings.Split(pattern, "/")
		pathParts := strings.Split(path, "/")
		if len(patParts) != len(pathParts) {
			return false
		}
		for i := range patParts {
			if !matchComponent(patParts[i], pathParts[i]) {
				return false
			}
		}
		return true
	}

	return pattern == path
}

func matchComponent(pattern, name string) bool {
	if pattern == "*" {
		return true
	}
	if pattern == name {
		return true
	}
	if strings.HasPrefix(pattern, "*.") {
		return strings.HasSuffix(name, pattern[1:])
	}
	return false
}

// DefaultPolicy returns a formatted description of active policies for
// display in the system prompt.
func (e *Engine) DefaultPolicy() string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var sb strings.Builder
	sb.WriteString("Active policies:\n")
	for _, r := range e.rules {
		icon := "✅"
		if !r.Action.Allow {
			icon = "🚫"
		}
		fmt.Fprintf(&sb, "  %s %s (priority %d): %s\n", icon, r.Name, r.Priority, r.Description)
	}
	return sb.String()
}
