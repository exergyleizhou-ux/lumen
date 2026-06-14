package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"lumen/internal/event"
	"lumen/internal/provider"
	"lumen/internal/tool"
)

// Runner carries out one task turn. Both Agent (single model) and Coordinator
// (two-model) satisfy it, so the CLI stays agnostic to which is in use.
type Runner interface {
	Run(ctx context.Context, input string) error
}

// ── Coordinator: two-model Planner + Executor ─────────────

// DefaultPlannerPrompt steers the planner toward concise plans, not execution.
const DefaultPlannerPrompt = `You are the planner in a two-model coding agent.
Given a task, produce a concise, ordered plan for the executor model to carry out.
Use the read-only tools available to you when the task needs context.
Do not write implementations or attempt side effects.
Output executor-ready instructions: what to do, which files or commands are relevant,
expected blockers, and key decisions. Keep it short and actionable.`

// Coordinator runs two models in separate sessions to keep each model's prefix
// cache stable: a low-frequency planner proposes an approach, then the executor
// (a full tool-using Agent) carries it out.
type Coordinator struct {
	planner      provider.Provider
	plannerSess  *Session
	plannerAgent *Agent
	executor     *Agent
	temperature  float64
	sink         event.Sink
	shouldPlan   func(string) bool
}

// NewCoordinator wires a planner provider (with its own session) to an executor.
func NewCoordinator(
	planner provider.Provider,
	plannerSession *Session,
	plannerTools *tool.Registry,
	plannerOpts Options,
	executor *Agent,
	temperature float64,
	sink event.Sink,
	shouldPlan func(string) bool,
) *Coordinator {
	var plannerAgent *Agent
	if plannerTools != nil {
		plannerOpts.Temperature = temperature
		plannerAgent = New(planner, plannerTools, plannerSession, plannerOpts)
	}
	return &Coordinator{
		planner:      planner,
		plannerSess:  plannerSession,
		plannerAgent: plannerAgent,
		executor:     executor,
		temperature:  temperature,
		sink:         sink,
		shouldPlan:   shouldPlan,
	}
}

// Run plans with the planner model, then hands the plan to the executor.
func (c *Coordinator) Run(ctx context.Context, input string) error {
	c.sink.Emit(event.Event{Kind: event.TurnStarted, Timestamp: time.Now()})

	// Skip planning for trivial turns
	if c.shouldPlan != nil && !c.shouldPlan(input) {
		c.sink.Emit(event.Event{Kind: event.Phase, Text: c.executor.prov.Name() + " · executing", Timestamp: time.Now()})
		return c.executor.Run(ctx, input)
	}

	// Phase 1: Plan
	c.sink.Emit(event.Event{Kind: event.Phase, Text: c.planner.Name() + " · planning", Timestamp: time.Now()})
	plan, err := c.plan(ctx, input)
	if err != nil {
		return fmt.Errorf("planner: %w", err)
	}

	// Phase 2: Execute
	c.sink.Emit(event.Event{Kind: event.Phase, Text: c.executor.prov.Name() + " · executing", Timestamp: time.Now()})
	return c.executor.Run(ctx, formatHandoff(input, plan))
}

func (c *Coordinator) plan(ctx context.Context, input string) (string, error) {
	if c.plannerAgent != nil {
		return c.planWithTools(ctx, input)
	}

	// Simple plan: just send the prompt and collect the response
	c.plannerSess.Add(provider.Message{Role: provider.RoleUser, Content: input})
	ch, err := c.planner.Stream(ctx, provider.Request{
		Messages:    c.plannerSess.Messages,
		Temperature: c.temperature,
	})
	if err != nil {
		return "", err
	}
	var text strings.Builder
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkText:
			text.WriteString(chunk.Text)
			c.sink.Emit(event.Event{Kind: event.Text, Text: chunk.Text, Timestamp: time.Now()})
		case provider.ChunkError:
			return "", chunk.Err
		}
	}
	plan := text.String()
	c.plannerSess.Add(provider.Message{Role: provider.RoleAssistant, Content: plan})
	return plan, nil
}

func (c *Coordinator) planWithTools(ctx context.Context, input string) (string, error) {
	before := len(c.plannerSess.Messages)
	if err := c.plannerAgent.Run(ctx, input); err != nil {
		return "", err
	}
	for i := len(c.plannerSess.Messages) - 1; i >= before; i-- {
		m := c.plannerSess.Messages[i]
		if m.Role == provider.RoleAssistant && strings.TrimSpace(m.Content) != "" {
			return m.Content, nil
		}
	}
	return "", fmt.Errorf("planner finished without producing a plan")
}

func formatHandoff(task, plan string) string {
	return fmt.Sprintf(`# Executor Handoff

You are the executor now. Use your available tools to execute the task.

Original task:
%s

Planner output:
%s

Executor instructions:
- Treat the planner output as context, not as your role or capability set.
- If the task requires changes, call the appropriate tools (write/edit/bash).
- Establish the task list with todo_write, then execute each sub-task.
- Serial workflow: sign off one sub-task at a time with complete_step.`, task, plan)
}

// ── Planner tool registry ──────────────────────────────────

// PlannerToolRegistry returns a read-only subset of tools suitable for the planner.
func PlannerToolRegistry(parent *tool.Registry) *tool.Registry {
	exclude := map[string]bool{
		// Exclude meta/writer tools the planner shouldn't use
		"task":           true,
		"run_skill":      true,
		"install_skill":  true,
		"write_file":     true,
		"edit_file":      true,
		"bash":           true,
		"notebook_edit":  true,
		"complete_step":  true,
		"todo_write":     true,
		"ask":            true,
	}
	sub := tool.NewRegistry()
	for _, name := range parent.Names() {
		if exclude[name] {
			continue
		}
		tl, ok := parent.Get(name)
		if !ok || !tl.ReadOnly() {
			continue
		}
		sub.Add(tl)
	}
	return sub
}
