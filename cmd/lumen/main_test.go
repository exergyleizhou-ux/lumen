package main

import "testing"

func TestParseRunArgs(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		wantPlan   bool
		wantMode   string
		wantPrompt string
	}{
		{"plain prompt", []string{"fix", "the", "bug"}, false, "", "fix the bug"},
		{"plan flag first", []string{"--plan", "look", "around"}, true, "", "look around"},
		{"mode flag first", []string{"--mode", "accept-edits", "do", "it"}, false, "accept-edits", "do it"},
		{"mode flag mid", []string{"do", "--mode", "plan", "it"}, false, "plan", "do it"},
		{"both flags", []string{"--plan", "--mode", "bypass", "go"}, true, "bypass", "go"},
		{"mode without value", []string{"--mode"}, false, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			plan, mode, prompt := parseRunArgs(c.args)
			if plan != c.wantPlan || mode != c.wantMode || prompt != c.wantPrompt {
				t.Errorf("parseRunArgs(%v) = (%v, %q, %q), want (%v, %q, %q)",
					c.args, plan, mode, prompt, c.wantPlan, c.wantMode, c.wantPrompt)
			}
		})
	}
}
