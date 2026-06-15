package modelpool

import (
	"context"
	"strings"
	"testing"
)

func TestPoolRegister(t *testing.T) {
	p := NewPool(&RoundRobinStrategy{})
	p.Register(&Info{Name: "gpt4", Model: "gpt-4o", Healthy: true, Capabilities: []Capability{CapCode}})
	m, err := p.Select(context.Background(), nil, "")
	if err != nil { t.Fatalf("select: %v", err) }
	if m.Name != "gpt4" { t.Errorf("expected gpt4, got %s", m.Name) }
}
func TestRoundRobin(t *testing.T) {
	p := NewPool(&RoundRobinStrategy{})
	p.Register(&Info{Name: "a", Model: "a", Healthy: true})
	p.Register(&Info{Name: "b", Model: "b", Healthy: true})
	m1, _ := p.Select(context.Background(), nil, "")
	m2, _ := p.Select(context.Background(), nil, "")
	if m1.Name == m2.Name { t.Error("round robin should cycle") }
}
func TestCostOptimized(t *testing.T) {
	p := NewPool(&CostOptimizedStrategy{})
	p.Register(&Info{Name: "expensive", Model: "pro", Healthy: true, CostPer1K: 0.010})
	p.Register(&Info{Name: "cheap", Model: "flash", Healthy: true, CostPer1K: 0.001})
	m, _ := p.Select(context.Background(), nil, "")
	if m.Name != "cheap" { t.Error("should pick cheapest") }
}
func TestCapabilityFilter(t *testing.T) {
	p := NewPool(&RoundRobinStrategy{})
	p.Register(&Info{Name: "coder", Model: "code-model", Healthy: true, Capabilities: []Capability{CapCode}})
	p.Register(&Info{Name: "viewer", Model: "vision-model", Healthy: true, Capabilities: []Capability{CapVision}})
	m, _ := p.Select(context.Background(), []Capability{CapVision}, "")
	if m.Name != "viewer" { t.Error("should pick vision-capable") }
}
func TestFailureRecording(t *testing.T) {
	p := NewPool(&RoundRobinStrategy{})
	p.Register(&Info{Name: "flaky", Model: "flaky-model", Healthy: true})
	p.RecordFailure("flaky", "timeout"); p.RecordFailure("flaky", "timeout"); p.RecordFailure("flaky", "timeout")
	m, err := p.Select(context.Background(), nil, "")
	if err == nil { t.Errorf("should not select unhealthy model, got %s", m.Name) }
}
func TestBudgetTracker(t *testing.T) {
	b := NewBudgetTracker(1000)
	if !b.Consume(500) { t.Error("should allow within budget") }
	if b.UsedToday() != 500 { t.Error("used mismatch") }
	if b.Consume(600) { t.Error("should reject over budget") }
}
func TestPoolFormatStats(t *testing.T) {
	p := NewPool(&RoundRobinStrategy{})
	p.Register(&Info{Name: "fmt", Model: "test", Healthy: true})
	if s := p.FormatStats(); !strings.Contains(s, "fmt") { t.Error("should contain name") }
}
