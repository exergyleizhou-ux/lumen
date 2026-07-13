package lab

import (
	"fmt"
	"strings"

	coreconfig "lumen/internal/config"
	sciconfig "lumen/internal/science/config"
	"lumen/internal/science/proxy"
)

// ScienceProviderConfig resolves a science profile without mutating process state.
func ScienceProviderConfig(cfg sciconfig.File) (coreconfig.ProviderConfig, string, string, error) {
	var out coreconfig.ProviderConfig
	masked, adapter, err := applyScienceProfile(cfg, &out)
	return out, masked, adapter, err
}

// ApplyScienceProfile is retained for status callers. It is pure: credentials
// are returned through ScienceProviderConfig, never installed into os.Environ.
func ApplyScienceProfile(cfg sciconfig.File) (masked string, adapter string, err error) {
	return applyScienceProfile(cfg, nil)
}

func applyScienceProfile(cfg sciconfig.File, out *coreconfig.ProviderConfig) (masked string, adapter string, err error) {
	p := cfg.ActiveProfile()
	key := ""
	templateID := cfg.Provider
	baseURL := ""
	model := ""
	if p != nil {
		key = strings.TrimSpace(p.APIKey)
		templateID = p.TemplateID
		if templateID == "" {
			templateID = cfg.Provider
		}
		baseURL = sciconfig.ResolveProfileBaseURL(*p)
		model = strings.TrimSpace(p.Model)
		masked = sciconfig.MaskKey(key)
	} else if pc, ok := cfg.Providers[cfg.Provider]; ok {
		key = strings.TrimSpace(pc.Key)
		masked = sciconfig.MaskKey(key)
	}
	if key == "" {
		return masked, "", fmt.Errorf("no API key in science config — configure a profile in Bridge")
	}
	tpl, ok := sciconfig.TemplateByID(templateID)
	if !ok {
		if spec, ok2 := proxy.LookupProvider(cfg.Provider); ok2 {
			adapter = spec.Name
			if out != nil {
				*out = coreconfig.ProviderConfig{Name: cfg.Provider, Kind: "openai", APIKey: key, Model: model, BaseURL: baseURL}
			}
			return masked, adapter, nil
		}
		return masked, "", fmt.Errorf("unknown provider template %q", templateID)
	}
	adapter = tpl.Adapter
	if out != nil {
		// The Lab runtime deliberately ships one request-compatible adapter;
		// template-specific endpoint semantics are represented by BaseURL.
		kind := "openai"
		*out = coreconfig.ProviderConfig{Name: templateID, Kind: kind, APIKey: key, BaseURL: baseURL, Model: model}
	}
	return masked, adapter, nil
}

// providerStatus returns masked key info without requiring a valid key.
func providerStatus(cfg sciconfig.File) (masked, adapter string) {
	p := cfg.ActiveProfile()
	if p != nil {
		masked = sciconfig.MaskKey(strings.TrimSpace(p.APIKey))
		if tpl, ok := sciconfig.TemplateByID(p.TemplateID); ok {
			adapter = tpl.Adapter
		}
		return masked, adapter
	}
	if pc, ok := cfg.Providers[cfg.Provider]; ok {
		masked = sciconfig.MaskKey(strings.TrimSpace(pc.Key))
		adapter = cfg.Provider
	}
	return masked, adapter
}
