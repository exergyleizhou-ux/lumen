// Package permission implements the tool-call permission gate. It can run in
// several modes: "bypass" (allow all), "default" (deny dangerous, prompt for
// others on interactive runs, allow in headless), "plan" (read-only).
package permission

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"lumen/internal/guard"
)

// Mode controls the permission gate behavior.
type Mode string

const (
	ModeBypass      Mode = "bypass"
	ModeDefault     Mode = "default"
	ModeAcceptEdits Mode = "accept-edits"
	ModePlan        Mode = "plan"
)

// AlwaysAllowTools are tools that are always safe to run without prompting.
var AlwaysAllowTools = map[string]bool{
	"read_file":       true,
	"grep":            true,
	"glob":            true,
	"ls":              true,
	"lsp_hover":       true,
	"lsp_definition":  true,
	"lsp_references":  true,
	"lsp_diagnostics": true,
	"web_fetch":       true,
	"web_search":      true,
	"ask":             true,
}

// DangerousTools are tools that always require confirmation in default mode.
var DangerousTools = map[string]bool{
	"bash": true,
}

// Gate implements agent.Gate with mode-based decision logic.
type Gate struct {
	mode  Mode
	asker func(ctx context.Context, toolName string, args json.RawMessage) (bool, error)
}

// NewGate creates a permission gate in the given mode.
func NewGate(mode Mode, asker func(ctx context.Context, toolName string, args json.RawMessage) (bool, error)) *Gate {
	return &Gate{mode: mode, asker: asker}
}

// Check implements agent.Gate.
func (g *Gate) Check(ctx context.Context, toolName string, args json.RawMessage, readOnly bool) (allow bool, reason string, err error) {
	// ── Bash content inspection (fires for ALL modes) ──────
	if toolName == "bash" {
		var p struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(args, &p); err == nil && p.Command != "" {
			if r := guard.CheckBash(p.Command); !r.Safe {
				return false, "blocked: " + r.Reason, nil
			}
		}
	}

	switch g.mode {
	case ModeBypass:
		return true, "", nil
	case ModePlan:
		if readOnly {
			return true, "", nil
		}
		return false, "plan mode is read-only — approve the plan first, or switch to accept-edits mode", nil
	case ModeAcceptEdits:
		// Allow all tools except dangerous ones
		if DangerousTools[toolName] {
			if g.asker != nil {
				allow, err := g.asker(ctx, toolName, args)
				return allow, "user denied: " + toolName, err
			}
			return false, "tool " + toolName + " requires approval in accept-edits mode", nil
		}
		return true, "", nil
	default: // ModeDefault
		// Always allow safe read-only tools
		if AlwaysAllowTools[toolName] {
			return true, "", nil
		}
		// Dangerous tools always ask
		if DangerousTools[toolName] {
			if g.asker != nil {
				allow, err := g.asker(ctx, toolName, args)
				if !allow {
					return false, "user denied: " + toolName, err
				}
				return true, "", nil
			}
			return false, "tool " + toolName + " requires interactive approval in default mode", nil
		}
		// Writer tools: ask when interactive, allow in headless
		if !readOnly {
			if g.asker != nil {
				allow, err := g.asker(ctx, toolName, args)
				if !allow {
					return false, "user denied: " + toolName, err
				}
				return true, "", nil
			}
			// Headless: allow non-dangerous writer tools
			return true, "", nil
		}
		return true, "", nil
	}
}

// ParseMode converts a config string to a Mode.
func ParseMode(s string) Mode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "bypass":
		return ModeBypass
	case "accept-edits", "accept_edits":
		return ModeAcceptEdits
	case "plan":
		return ModePlan
	default:
		return ModeDefault
	}
}

// SummarizeArgs produces a one-line summary of tool arguments for display
// in approval prompts.
func SummarizeArgs(toolName string, args json.RawMessage) string {
	switch toolName {
	case "bash":
		var p struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(args, &p); err == nil && p.Command != "" {
			return fmt.Sprintf("bash: %s", p.Command)
		}
		return "bash"
	case "write_file":
		var p struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(args, &p); err == nil && p.Path != "" {
			return fmt.Sprintf("write %s", p.Path)
		}
		return "write_file"
	case "edit_file":
		var p struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(args, &p); err == nil && p.Path != "" {
			return fmt.Sprintf("edit %s", p.Path)
		}
		return "edit_file"
	default:
		return toolName
	}
}
