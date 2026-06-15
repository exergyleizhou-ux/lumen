package schema

import (
	"testing"
)

func TestRegistryRegister(t *testing.T) {
	r := NewRegistry()
	sv, err := r.Register("user", `{"type":"object","properties":{"name":{"type":"string"}}}`, "json-schema")
	if err != nil || sv.Version != 1 {
		t.Error("register")
	}
	if len(r.Subjects()) != 1 {
		t.Error("subjects")
	}
}
func TestGetLatest(t *testing.T) {
	r := NewRegistry()
	r.Register("evt", `{"type":"object"}`, "json-schema")
	r.Register("evt", `{"type":"object","properties":{"id":{"type":"integer"}}}`, "json-schema")
	latest, ok := r.GetLatest("evt")
	if !ok || latest.Version != 2 {
		t.Error("latest")
	}
}
func TestValidate(t *testing.T) {
	r := NewRegistry()
	r.Register("test", `{"type":"object"}`, "json-schema")
	ok, _ := r.Validate("test", map[string]any{"a": 1})
	if !ok {
		t.Error("should validate")
	}
	ok, _ = r.Validate("nonexistent", nil)
	if ok {
		t.Error("should fail")
	}
}
func TestDetectBreakingChange(t *testing.T) {
	r := NewRegistry()
	issues := r.DetectBreakingChange(`{"properties":{"name":{"type":"string"}}}`, `{"properties":{"name":{"type":"integer"}}}`)
	if len(issues) == 0 {
		t.Error("should detect type change")
	}
}
