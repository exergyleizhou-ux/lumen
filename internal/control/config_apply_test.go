package control

import (
	"testing"

	"lumen/internal/config"
)

// The compaction ratios are parsed from [agent] in lumen.toml and consumed by
// the agent's autoCompact loop. They must reach agent.Options, not be silently
// replaced by hardcoded defaults.
func TestAgentOptionsFromConfigAppliesCompactionRatios(t *testing.T) {
	cfg := &config.File{}
	cfg.Agent.MaxSteps = 20
	cfg.Agent.SoftCompactRatio = 0.6
	cfg.Agent.CompactRatio = 0.85
	opts := agentOptionsFromConfig(cfg)
	if opts.MaxSteps != 20 {
		t.Errorf("MaxSteps not copied: %d", opts.MaxSteps)
	}
	if opts.SoftCompactRatio != 0.6 {
		t.Errorf("SoftCompactRatio not applied: got %v want 0.6", opts.SoftCompactRatio)
	}
	if opts.CompactRatio != 0.85 {
		t.Errorf("CompactRatio not applied: got %v want 0.85", opts.CompactRatio)
	}
}

// default_model should resolve a provider by its Name OR its Model, and signal
// (ok=false) when it matched nothing so the caller can warn instead of silently
// using a different provider.
func TestResolveProvider_MatchesByNameOrModel(t *testing.T) {
	providers := []config.ProviderConfig{
		{Name: "deepseek", Model: "deepseek-chat"},
		{Name: "openai", Model: "gpt-4"},
	}
	if p, ok := resolveProvider(providers, "openai"); !ok || p.Name != "openai" {
		t.Errorf("by name: got %v ok=%v", p, ok)
	}
	if p, ok := resolveProvider(providers, "gpt-4"); !ok || p.Name != "openai" {
		t.Errorf("by model: should match openai via its Model, got %v ok=%v", p, ok)
	}
	if p, ok := resolveProvider(providers, "nope"); ok || p == nil || p.Name != "deepseek" {
		t.Errorf("fallback: want first provider with ok=false, got %v ok=%v", p, ok)
	}
}

// [skills] max_depth controls how deep skill discovery recurses; it must reach
// the skill store instead of falling back to the hardcoded default.
func TestSkillOptionsFromConfigAppliesMaxDepth(t *testing.T) {
	cfg := &config.File{}
	cfg.Skills.MaxDepth = 5
	opts := skillOptionsFromConfig(cfg, "/some/wd")
	if opts.ProjectRoot != "/some/wd" {
		t.Errorf("ProjectRoot not set: %q", opts.ProjectRoot)
	}
	if opts.MaxDepth != 5 {
		t.Errorf("MaxDepth not applied: got %d want 5", opts.MaxDepth)
	}
}
