package runtime

import (
	"fmt"
	"strings"

	sciconfig "lumen/internal/science/config"
	"lumen/internal/science/proxy"
)

// ResolvedSpec bundles upstream spec + API key from active profile or legacy config.
type ResolvedSpec struct {
	Spec       proxy.ProviderSpec
	APIKey     string
	Adapter    string
	ProfileID  string
	ProfileName string
}

// ResolveActiveSpec maps active profile (or legacy provider slot) to a proxy spec.
func ResolveActiveSpec(cfg sciconfig.File) (ResolvedSpec, error) {
	if p := cfg.ActiveProfile(); p != nil {
		return resolveProfile(*p)
	}
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if provider == "" {
		provider = sciconfig.DefaultProvider
	}
	spec, ok := proxy.LookupProvider(provider)
	if !ok {
		return ResolvedSpec{}, fmt.Errorf("unsupported provider %q", provider)
	}
	key := cfg.KeyFor(provider)
	if key == "" {
		return ResolvedSpec{}, fmt.Errorf("missing API key for %s", provider)
	}
	return ResolvedSpec{Spec: spec, APIKey: key, Adapter: provider}, nil
}

func resolveProfile(p sciconfig.Profile) (ResolvedSpec, error) {
	tpl, ok := sciconfig.TemplateByID(p.TemplateID)
	if !ok {
		tpl, _ = sciconfig.TemplateByID("custom")
	}
	key := strings.TrimSpace(p.APIKey)
	if key == "" {
		return ResolvedSpec{}, fmt.Errorf("profile %q has no API key", p.Name)
	}
	baseURL := strings.TrimSpace(p.BaseURL)
	if baseURL == "" {
		baseURL = tpl.BaseURL
	}
	adapter := tpl.Adapter
	switch adapter {
	case "relay":
		models := make([]proxy.ModelEntry, 0, len(tpl.BuiltinModels))
		for _, m := range tpl.BuiltinModels {
			models = append(models, proxy.ModelEntry{ID: m, DisplayName: m})
		}
		spec := proxy.RelaySpec(baseURL, key, strings.TrimSpace(p.Model), models)
		return ResolvedSpec{
			Spec: spec, APIKey: key, Adapter: "relay",
			ProfileID: p.ID, ProfileName: p.Name,
		}, nil
	default:
		spec, ok := proxy.LookupProvider(adapter)
		if !ok {
			return ResolvedSpec{}, fmt.Errorf("unknown adapter %q", adapter)
		}
		if baseURL != "" && baseURL != tpl.BaseURL {
			spec.URL = strings.TrimRight(baseURL, "/") + "/v1/messages"
			if spec.Mode == proxy.ModeOpenAI {
				spec.URL = strings.TrimRight(baseURL, "/") + "/chat/completions"
			}
		}
		if m := strings.TrimSpace(p.Model); m != "" {
			spec.DefaultModel = m
		}
		return ResolvedSpec{
			Spec: spec, APIKey: key, Adapter: adapter,
			ProfileID: p.ID, ProfileName: p.Name,
		}, nil
	}
}