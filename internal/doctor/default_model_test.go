package doctor

import (
	"testing"

	"lumen/internal/config"
)

func defaultModelResult(r *Report) (Result, bool) {
	for _, res := range r.Results {
		if res.Name == "default_model" {
			return res, true
		}
	}
	return Result{}, false
}

func TestCheckDefaultModel_OkOnMatchByModel(t *testing.T) {
	cfg := &config.File{
		DefaultModel: "deepseek-chat",
		Providers:    []config.ProviderConfig{{Name: "deepseek", Model: "deepseek-chat"}},
	}
	r := &Report{AllOk: true}
	r.checkDefaultModel(cfg)
	res, ok := defaultModelResult(r)
	if !ok || res.Status != "ok" {
		t.Errorf("matching default_model (by model) should be ok, got %+v ok=%v", res, ok)
	}
}

func TestCheckDefaultModel_WarnsOnMismatch(t *testing.T) {
	cfg := &config.File{
		DefaultModel: "nonexistent-model",
		Providers:    []config.ProviderConfig{{Name: "deepseek", Model: "deepseek-chat"}},
	}
	r := &Report{AllOk: true}
	r.checkDefaultModel(cfg)
	res, ok := defaultModelResult(r)
	if !ok || res.Status != "warn" {
		t.Errorf("mismatched default_model should warn, got %+v ok=%v", res, ok)
	}
}
