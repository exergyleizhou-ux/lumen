package control

import (
	"lumen/internal/agent"
	"lumen/internal/config"
	"lumen/internal/skill"
)

// agentOptionsFromConfig builds the config-derived agent options. Runtime-only
// fields (Sink, Gate, Asker, MemoryPrompt) are filled in by the caller.
func agentOptionsFromConfig(cfg *config.File) agent.Options {
	return agent.Options{
		MaxSteps:         cfg.Agent.MaxSteps,
		Temperature:      cfg.Agent.Temperature,
		ContextWindow:    cfg.Agent.ContextWindow,
		SoftCompactRatio: cfg.Agent.SoftCompactRatio,
		CompactRatio:     cfg.Agent.CompactRatio,
	}
}

// resolveProvider picks the provider matching defaultModel by either its Name
// or its Model, falling back to the first provider. The bool is false when it
// fell back (a configured default_model that matched nothing) so the caller can
// warn instead of silently using a different provider.
func resolveProvider(providers []config.ProviderConfig, defaultModel string) (*config.ProviderConfig, bool) {
	for i := range providers {
		if providers[i].Name == defaultModel || providers[i].Model == defaultModel {
			return &providers[i], true
		}
	}
	if len(providers) > 0 {
		return &providers[0], false
	}
	return nil, false
}

// skillOptionsFromConfig builds the skill-store options from config.
func skillOptionsFromConfig(cfg *config.File, projectRoot string) skill.Options {
	return skill.Options{
		ProjectRoot: projectRoot,
		MaxDepth:    cfg.Skills.MaxDepth,
	}
}
