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

// skillOptionsFromConfig builds the skill-store options from config.
func skillOptionsFromConfig(cfg *config.File, projectRoot string) skill.Options {
	return skill.Options{
		ProjectRoot: projectRoot,
		MaxDepth:    cfg.Skills.MaxDepth,
	}
}
