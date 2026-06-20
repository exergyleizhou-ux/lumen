package control

import (
	"strings"
	"time"

	"lumen/internal/agent"
	"lumen/internal/config"
	"lumen/internal/provider"
	"lumen/internal/skill"
)

// parseTurnTimeout converts an [agent] turn_timeout duration string to a
// duration, falling back to 5m for empty/unparseable values (config.Load already
// rejects bad values from a file; this guards the programmatic &config.File{} path
// so the per-turn deadline is never accidentally zero/disabled).
func parseTurnTimeout(s string) time.Duration {
	if d, err := time.ParseDuration(s); err == nil && d > 0 {
		return d
	}
	return 5 * time.Minute
}

// isLoopbackURL reports whether a provider base_url points at an on-machine
// (loopback) endpoint — i.e. a local model. Used to give local backends routing
// priority (cheap + fast) over cloud ones.
func isLoopbackURL(baseURL string) bool {
	host := baseURL
	if i := strings.Index(host, "://"); i >= 0 {
		host = host[i+3:]
	}
	return strings.HasPrefix(host, "localhost") ||
		strings.HasPrefix(host, "127.0.0.1") ||
		strings.HasPrefix(host, "[::1]")
}

// pricingFromConfig maps an optional [providers.pricing] block to a
// provider.Pricing, or nil when unset (the caller then falls back to the
// built-in default rate). Keeps config decoupled from the provider package.
func pricingFromConfig(pc *config.PricingConfig) *provider.Pricing {
	if pc == nil {
		return nil
	}
	return &provider.Pricing{
		Input:    pc.Input,
		Output:   pc.Output,
		CacheHit: pc.CacheHit,
		Currency: pc.Currency,
	}
}

// agentOptionsFromConfig builds the config-derived agent options. Runtime-only
// fields (Sink, Gate, Asker, MemoryPrompt) are filled in by the caller.
func agentOptionsFromConfig(cfg *config.File) agent.Options {
	return agent.Options{
		MaxSteps:         cfg.Agent.MaxSteps,
		Temperature:      cfg.Agent.Temperature,
		ContextWindow:    cfg.Agent.ContextWindow,
		SoftCompactRatio: cfg.Agent.SoftCompactRatio,
		CompactRatio:     cfg.Agent.CompactRatio,
		TurnTimeout:      parseTurnTimeout(cfg.Agent.TurnTimeout),
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
