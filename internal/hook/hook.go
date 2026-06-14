// Package hook fires user-configured shell hooks around tool calls and
// agent lifecycle events. Configure via environment variables:
//
//	LUMEN_HOOK_PRE_TOOL   — command run before each tool call (exit 2 to block)
//	LUMEN_HOOK_POST_TOOL  — command run after each tool call
//	LUMEN_HOOK_POST_LLM   — command run after each model completion
//	LUMEN_HOOK_SUBAGENT   — command run when a sub-agent finishes
//	LUMEN_HOOK_PRE_COMPACT — command run before context compaction
package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// Registry holds configured hooks and implements agent.ToolHooks.
type Registry struct {
	PreToolUseCmd   string
	PostToolUseCmd  string
	PostLLMCallCmd  string
	SubagentStopCmd string
	PreCompactCmd   string
	mu              sync.Mutex
}

// NewRegistry creates a hook registry from environment variables.
func NewRegistry() *Registry {
	return &Registry{
		PreToolUseCmd:   os.Getenv("LUMEN_HOOK_PRE_TOOL"),
		PostToolUseCmd:  os.Getenv("LUMEN_HOOK_POST_TOOL"),
		PostLLMCallCmd:  os.Getenv("LUMEN_HOOK_POST_LLM"),
		SubagentStopCmd: os.Getenv("LUMEN_HOOK_SUBAGENT"),
		PreCompactCmd:   os.Getenv("LUMEN_HOOK_PRE_COMPACT"),
	}
}

// HasAny reports whether any hooks are configured.
func (r *Registry) HasAny() bool {
	return r.PreToolUseCmd != "" || r.PostToolUseCmd != "" ||
		r.PostLLMCallCmd != "" || r.SubagentStopCmd != "" ||
		r.PreCompactCmd != ""
}

// PreToolUse fires before a tool call. Exit code 2 blocks the call.
func (r *Registry) PreToolUse(ctx context.Context, name string, args json.RawMessage) (block bool, message string) {
	if r.PreToolUseCmd == "" {
		return false, ""
	}
	return r.run(ctx, r.PreToolUseCmd, name, args, nil)
}

// PostToolUse fires after a tool call (cannot block).
func (r *Registry) PostToolUse(ctx context.Context, name string, args json.RawMessage, result string) {
	if r.PostToolUseCmd == "" {
		return
	}
	r.run(ctx, r.PostToolUseCmd, name, args, map[string]string{"LUMEN_TOOL_RESULT": result})
}

// PostLLMCall fires after each model turn completes.
func (r *Registry) PostLLMCall(ctx context.Context, reasoning string, turn int) string {
	if r.PostLLMCallCmd == "" {
		return reasoning
	}
	env := map[string]string{
		"LUMEN_TURN":      fmt.Sprintf("%d", turn),
		"LUMEN_REASONING": reasoning,
	}
	_, output := r.run(ctx, r.PostLLMCallCmd, "", nil, env)
	if output == "" {
		return reasoning
	}
	return strings.TrimSpace(output)
}

// HasPostLLMCall reports whether a post-LLM hook is configured.
func (r *Registry) HasPostLLMCall() bool { return r.PostLLMCallCmd != "" }

// SubagentStop fires when a foreground sub-agent finishes.
func (r *Registry) SubagentStop(ctx context.Context, last string) {
	if r.SubagentStopCmd == "" {
		return
	}
	r.run(ctx, r.SubagentStopCmd, "", nil, map[string]string{"LUMEN_SUBAGENT_LAST": last})
}

// PreCompact fires before context compaction.
func (r *Registry) PreCompact(ctx context.Context, trigger string) string {
	if r.PreCompactCmd == "" {
		return ""
	}
	_, output := r.run(ctx, r.PreCompactCmd, "", nil, map[string]string{"LUMEN_COMPACT_TRIGGER": trigger})
	return strings.TrimSpace(output)
}

func (r *Registry) run(ctx context.Context, command, toolName string, args json.RawMessage, extraEnv map[string]string) (block bool, output string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	if toolName != "" {
		cmd.Env = append(cmd.Environ(), "LUMEN_TOOL_NAME="+toolName)
	}
	for k, v := range extraEnv {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	if args != nil {
		cmd.Stdin = bytes.NewReader(args)
	} else {
		cmd.Stdin = bytes.NewReader(nil)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output = strings.TrimSpace(stdout.String())
	if stderr.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += strings.TrimSpace(stderr.String())
	}
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 2 {
			return true, output
		}
		if output == "" {
			return false, fmt.Sprintf("hook error: %v", err)
		}
	}
	return false, output
}
