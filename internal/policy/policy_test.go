package policy

import (
	"testing"
)

func TestEvaluateAllow(t *testing.T) {
	e := NewEngine()
	e.Register(&Policy{Name: "test", Version: "1.0", Rules: []Rule{{Name: "allow-all", Effect: EffectAllow, Priority: 0, Conditions: []Condition{{Field: "action", Operator: OpExists}}}}})
	d, err := e.Evaluate("test", map[string]any{"action": "read"})
	if err != nil {
		t.Fatal(err)
	}
	if d.Effect != EffectAllow {
		t.Error("should allow")
	}
}
func TestEvaluateDeny(t *testing.T) {
	e := NewEngine()
	e.Register(&Policy{Name: "strict", Version: "1.0", Rules: []Rule{{Name: "deny-rm", Effect: EffectDeny, Priority: 0, Conditions: []Condition{{Field: "action", Operator: OpEqual, Value: "rm"}}}}})
	d, _ := e.Evaluate("strict", map[string]any{"action": "read"})
	if d.Effect != EffectDeny {
		t.Error("should deny — implicit")
	}
}
func TestEvaluateContains(t *testing.T) {
	e := NewEngine()
	e.Register(&Policy{Name: "cont", Version: "1.0", Rules: []Rule{{Name: "r1", Effect: EffectAllow, Priority: 0, Conditions: []Condition{{Field: "tags", Operator: OpContains, Value: "prod"}}}}})
	d, _ := e.Evaluate("cont", map[string]any{"tags": []any{"dev", "prod"}})
	if d.Effect != EffectAllow {
		t.Error("should match contains")
	}
}
func TestEvaluateIn(t *testing.T) {
	e := NewEngine()
	e.Register(&Policy{Name: "in-test", Version: "1.0", Rules: []Rule{{Name: "r1", Effect: EffectAllow, Priority: 0, Conditions: []Condition{{Field: "role", Operator: OpIn, Value: []any{"admin", "editor"}}}}}})
	d, _ := e.Evaluate("in-test", map[string]any{"role": "admin"})
	if d.Effect != EffectAllow {
		t.Error("in")
	}
}
func TestDefaultSecurityPolicy(t *testing.T) {
	e := NewEngine()
	e.Register(DefaultSecurityPolicy())
	d, _ := e.Evaluate("security.default", map[string]any{"action": "exec rm -rf /"})
	if d.Effect != EffectDeny {
		t.Error("should deny shell exec")
	}
}
