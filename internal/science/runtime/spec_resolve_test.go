package runtime

import (
	"testing"

	sciconfig "lumen/internal/science/config"
)

func TestResolveActiveSpecLegacy(t *testing.T) {
	cfg := sciconfig.Default()
	cfg.Provider = "deepseek"
	cfg.Providers = map[string]sciconfig.ProviderCfg{"deepseek": {Key: "sk-x"}}
	res, err := ResolveActiveSpec(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if res.APIKey != "sk-x" || res.Adapter != "deepseek" {
		t.Fatalf("%+v", res)
	}
}

func TestResolveActiveSpecProfileRelay(t *testing.T) {
	cfg := sciconfig.Default()
	cfg.SchemaVersion = sciconfig.CurrentSchemaVersion
	p := sciconfig.Profile{ID: "r1", Name: "R", TemplateID: "custom", BaseURL: "https://x.com", APIKey: "k"}
	cfg.Profiles = []sciconfig.Profile{p}
	cfg.ActiveProfileID = "r1"
	res, err := ResolveActiveSpec(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if res.Adapter != "relay" {
		t.Fatalf("%+v", res)
	}
}

func TestResolveActiveSpecMissingKey(t *testing.T) {
	cfg := sciconfig.Default()
	cfg.Provider = "deepseek"
	_, err := ResolveActiveSpec(cfg)
	if err == nil {
		t.Fatal("expected missing key")
	}
}
