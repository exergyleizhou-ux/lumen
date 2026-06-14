package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"lumen/internal/skill"
)

type fakeSkillSource struct{ skills []skill.Skill }

func (f *fakeSkillSource) Get(name string) (skill.Skill, bool) {
	for _, s := range f.skills {
		if s.Name == name {
			return s, true
		}
	}
	return skill.Skill{}, false
}
func (f *fakeSkillSource) List() []skill.Skill { return f.skills }

func TestSkillToolInlineReturnsBody(t *testing.T) {
	src := &fakeSkillSource{skills: []skill.Skill{
		{Name: "brainstorm", Description: "ideate", Body: "BODY: think first", RunAs: skill.RunInline},
	}}
	tl := NewSkillTool(src, SubagentDeps{})
	out, err := tl.Execute(context.Background(), json.RawMessage(`{"name":"brainstorm"}`))
	if err != nil {
		t.Fatal(err)
	}
	if out != "BODY: think first" {
		t.Errorf("inline skill should return its body, got %q", out)
	}
}

func TestSkillToolUnknownSkillErrors(t *testing.T) {
	src := &fakeSkillSource{skills: []skill.Skill{{Name: "alpha", RunAs: skill.RunInline}}}
	tl := NewSkillTool(src, SubagentDeps{})
	_, err := tl.Execute(context.Background(), json.RawMessage(`{"name":"missing"}`))
	if err == nil {
		t.Fatal("unknown skill should error")
	}
	if !strings.Contains(err.Error(), "alpha") {
		t.Errorf("error should list available skills, got %v", err)
	}
}

func TestSkillToolSubagentReturnsFinalAnswer(t *testing.T) {
	src := &fakeSkillSource{skills: []skill.Skill{
		{Name: "explore", Body: "You are an explorer.", RunAs: skill.RunSubagent, AllowedTools: []string{"read_test"}},
	}}
	deps := SubagentDeps{
		Prov:          &mockProvider{name: "test"},
		ParentReg:     testRegistry(),
		MaxSteps:      6,
		ContextWindow: 1000,
	}
	tl := NewSkillTool(src, deps)
	out, err := tl.Execute(context.Background(), json.RawMessage(`{"name":"explore","prompt":"map the code"}`))
	if err != nil {
		t.Fatal(err)
	}
	if out != "ok" {
		t.Errorf("subagent skill should return the sub-agent's final answer, got %q", out)
	}
}

func TestSkillToolDescriptionListsCatalog(t *testing.T) {
	src := &fakeSkillSource{skills: []skill.Skill{
		{Name: "review", Description: "review code carefully", RunAs: skill.RunInline},
	}}
	tl := NewSkillTool(src, SubagentDeps{})
	d := tl.Description()
	if !strings.Contains(d, "review") || !strings.Contains(d, "review code carefully") {
		t.Errorf("description should list the skill catalog, got:\n%s", d)
	}
}
