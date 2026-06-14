package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"lumen/internal/event"
	"lumen/internal/provider"
	"lumen/internal/skill"
	"lumen/internal/tool"
)

// SkillSource provides named skills for the run_skill tool. *skill.Store
// satisfies it; tests can supply a fake.
type SkillSource interface {
	Get(name string) (skill.Skill, bool)
	List() []skill.Skill
}

// SubagentDeps holds the environment needed to spawn a sub-agent.
type SubagentDeps struct {
	Prov          provider.Provider
	Pricing       *provider.Pricing
	ParentReg     *tool.Registry
	MaxSteps      int
	ContextWindow int
	Temperature   float64
	Gate          Gate
}

// SkillTool implements the run_skill tool. An inline skill returns its body so
// the model folds the guidance into the current turn; a subagent skill runs in
// an isolated child agent (filtered to the skill's allowed-tools) and returns
// only its final answer.
type SkillTool struct {
	skills SkillSource
	deps   SubagentDeps
}

// NewSkillTool builds the run_skill tool over a skill source and the sub-agent
// environment used by subagent-mode skills.
func NewSkillTool(skills SkillSource, deps SubagentDeps) *SkillTool {
	return &SkillTool{skills: skills, deps: deps}
}

func (t *SkillTool) Name() string   { return "run_skill" }
func (t *SkillTool) ReadOnly() bool { return false } // a subagent skill may write

func (t *SkillTool) Description() string {
	var sb strings.Builder
	sb.WriteString("Invoke a named skill — a reusable playbook. Inline skills fold their guidance into your current turn; subagent skills run in an isolated agent and return only a final answer.\n\nAvailable skills:\n")
	skills := t.skills.List()
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	for _, sk := range skills {
		sb.WriteString("  - ")
		sb.WriteString(sk.Name)
		if sk.Description != "" {
			sb.WriteString(" — ")
			sb.WriteString(sk.Description)
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

func (t *SkillTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"The skill to invoke (see the list in this tool's description)."},"prompt":{"type":"string","description":"For subagent skills: the specific task for the sub-agent. Ignored by inline skills."}},"required":["name"]}`)
}

func (t *SkillTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Name   string `json:"name"`
		Prompt string `json:"prompt"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		return "", fmt.Errorf("name is required")
	}
	sk, ok := t.skills.Get(p.Name)
	if !ok {
		return "", fmt.Errorf("unknown skill %q. Available: %s", p.Name, t.availableNames())
	}
	if sk.RunAs == skill.RunSubagent {
		task := strings.TrimSpace(p.Prompt)
		if task == "" {
			task = "Carry out the skill as described."
		}
		return runSubagent(ctx, t.deps, sk.Body, sk.AllowedTools, task)
	}
	return sk.Body, nil
}

func (t *SkillTool) availableNames() string {
	var names []string
	for _, sk := range t.skills.List() {
		names = append(names, sk.Name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// runSubagent spawns an isolated sub-agent and returns its final answer.
// sysPrompt is the sub-agent's system message; toolWhitelist filters the
// inherited tools (empty = all but meta-tools); task is the user prompt.
func runSubagent(ctx context.Context, d SubagentDeps, sysPrompt string, toolWhitelist []string, task string) (string, error) {
	subReg := filterRegistry(d.ParentReg, toolWhitelist, SubagentMetaTools...)
	maxSteps := d.MaxSteps / 2
	if maxSteps < 5 {
		maxSteps = 5
	}
	subSess := NewSession("")
	subSess.Add(provider.Message{Role: provider.RoleSystem, Content: sysPrompt})

	parentID, parentSink, _, _ := CallContext(ctx)
	sink := event.Discard
	if parentSink != nil {
		sink = subSinkFor(parentID, parentSink)
	}

	subAgent := New(d.Prov, subReg, subSess, Options{
		MaxSteps:      maxSteps,
		Temperature:   d.Temperature,
		Pricing:       d.Pricing,
		ContextWindow: d.ContextWindow,
		Gate:          d.Gate,
		Sink:          sink,
	})
	if err := subAgent.Run(ctx, task); err != nil {
		return "", err
	}
	for i := len(subSess.Messages) - 1; i >= 0; i-- {
		m := subSess.Messages[i]
		if m.Role == provider.RoleAssistant && strings.TrimSpace(m.Content) != "" {
			return m.Content, nil
		}
	}
	return "", fmt.Errorf("sub-agent finished without producing a final answer")
}
