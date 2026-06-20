package agent

import (
	"context"
	"fmt"
	"strings"

	"lumen/internal/event"
	"lumen/internal/provider"
	"lumen/internal/tool"
)

// SpawnConfig configures a one-shot sub-agent. It mirrors the synchronous path
// of TaskTool, factored out so other call sites (e.g. the orchestrator's
// workflow agent) can spawn a real lumen sub-agent without duplicating the loop
// wiring or importing the controller.
type SpawnConfig struct {
	Provider      provider.Provider
	ParentReg     *tool.Registry
	MaxSteps      int
	ContextWindow int
	Temperature   float64
	Pricing       *provider.Pricing
	Gate          Gate
	SysPrompt     string   // defaults to DefaultTaskSystemPrompt
	ExcludeTools  []string // meta-tools to drop; defaults to SubagentMetaTools
}

// Spawn runs a sub-agent on prompt in its own session and returns its final
// answer (the last non-empty assistant message). It is safe to call
// concurrently — each call builds an isolated session and registry view.
func Spawn(ctx context.Context, cfg SpawnConfig, prompt string) (string, error) {
	if strings.TrimSpace(prompt) == "" {
		return "", fmt.Errorf("prompt is required")
	}
	if cfg.Provider == nil {
		return "", fmt.Errorf("spawn: no provider configured")
	}

	sysPrompt := cfg.SysPrompt
	if sysPrompt == "" {
		sysPrompt = DefaultTaskSystemPrompt
	}
	exclude := cfg.ExcludeTools
	if exclude == nil {
		exclude = SubagentMetaTools
	}
	maxSteps := cfg.MaxSteps
	if maxSteps <= 0 {
		maxSteps = 5
	}

	subReg := filterRegistry(cfg.ParentReg, nil, exclude...)

	sess := NewSession("")
	sess.Add(provider.Message{Role: provider.RoleSystem, Content: sysPrompt})

	// Nest sub-agent tool events under the parent call when one is present.
	sink := event.Discard
	if parentID, parentSink, _, ok := CallContext(ctx); ok && parentSink != nil {
		sink = subSinkFor(parentID, parentSink)
	}

	sub := New(cfg.Provider, subReg, sess, Options{
		MaxSteps:      maxSteps,
		Temperature:   cfg.Temperature,
		Pricing:       cfg.Pricing,
		ContextWindow: cfg.ContextWindow,
		Gate:          cfg.Gate,
		Sink:          sink,
	})
	if err := sub.Run(ctx, prompt); err != nil {
		return "", fmt.Errorf("sub-agent: %w", err)
	}

	for i := len(sess.Messages) - 1; i >= 0; i-- {
		m := sess.Messages[i]
		if m.Role == provider.RoleAssistant && strings.TrimSpace(m.Content) != "" {
			return m.Content, nil
		}
	}
	return "", fmt.Errorf("sub-agent finished without producing a final answer")
}
