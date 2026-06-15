package shard

import (
	"testing"
)

func TestRingGetNode(t *testing.T) {
	r := NewRing(128)
	r.AddNode(&Node{ID: "n1", Address: "10.0.0.1:8080", Weight: 1, Healthy: true})
	r.AddNode(&Node{ID: "n2", Address: "10.0.0.2:8080", Weight: 1, Healthy: true})
	n := r.GetNode("my-key")
	if n == nil {
		t.Error("should get node")
	}
}
func TestReplicas(t *testing.T) {
	r := NewRing(64)
	r.AddNode(&Node{ID: "a", Weight: 1, Healthy: true})
	r.AddNode(&Node{ID: "b", Weight: 1, Healthy: true})
	reps := r.GetReplicas("key", 2)
	if len(reps) != 2 {
		t.Error("replica count")
	}
}
func TestMarkUnhealthy(t *testing.T) {
	r := NewRing(64)
	r.AddNode(&Node{ID: "x", Weight: 1, Healthy: true})
	r.MarkHealthy("x", false)
	if r.RingSize() != 0 {
		t.Error("should be empty ring")
	}
}
func TestFormatRing(t *testing.T) {
	r := NewRing(16)
	r.AddNode(&Node{ID: "n1", Weight: 1, Healthy: true})
	if r.FormatRing() == "" {
		t.Error("format")
	}
}
