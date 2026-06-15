package adapt

import (
	"testing"
)

func TestRegistry(t *testing.T) {
	r := NewRegistry()
	wr := NewWebhookReceiver("test", ":0")
	r.Register(wr)
	a, ok := r.Get("test")
	if !ok || a.Name() != "test" {
		t.Error("registry")
	}
}
func TestWebhookStats(t *testing.T) {
	wr := NewWebhookReceiver("st", ":0")
	s := wr.Stats()
	if s.Received != 0 {
		t.Error("should be zero")
	}
}
func TestProcessSupervisor(t *testing.T) {
	ps := NewProcessSupervisor("echo", "echo", "hello")
	if ps.Name() != "echo" {
		t.Error("name")
	}
	if ps.Status() == "" {
		t.Error("status")
	}
}
func TestFormatStatus(t *testing.T) {
	r := NewRegistry()
	r.Register(NewWebhookReceiver("wh", ":0"))
	s := r.FormatStatus()
	if s == "" {
		t.Error("format")
	}
}
