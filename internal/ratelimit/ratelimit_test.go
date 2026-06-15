package ratelimit

import (
	"testing"
)

func TestTokenBucket(t *testing.T) {
	tb := NewTokenBucket(100, 10)
	for i := 0; i < 10; i++ {
		if !tb.Allow() {
			t.Error("should allow")
		}
	}
	if tb.Allow() {
		t.Error("should deny")
	}
}
func TestLeakyBucket(t *testing.T) {
	lb := NewLeakyBucket(10, 1)
	if !lb.Add(5) {
		t.Error("should allow 5")
	}
	if lb.Add(6) {
		t.Error("should deny 6")
	}
}
func TestHierarchical(t *testing.T) {
	root := NewHierarchicalLimiter("root", 100)
	child := root.AddChild("api", 50)
	for i := 0; i < 50; i++ {
		if !child.Allow() {
			t.Error("child should allow")
		}
	}
}
