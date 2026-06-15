package flowcontrol

import (
	"testing"
	"time"
)

func TestFixedWindow(t *testing.T) {
	fw := NewFixedWindow(3, 100*time.Millisecond)
	if !fw.Allow() || !fw.Allow() || !fw.Allow() {
		t.Error("first 3")
	}
	if fw.Allow() {
		t.Error("should block 4th")
	}
}
func TestSlidingWindow(t *testing.T) {
	sw := NewSlidingWindow(2, 100*time.Millisecond)
	if !sw.Allow() || !sw.Allow() {
		t.Error("first 2")
	}
	if sw.Allow() {
		t.Error("should block 3rd")
	}
}
func TestSemaphore(t *testing.T) {
	s := NewSemaphore(2)
	if !s.TryAcquire() || !s.TryAcquire() {
		t.Error("acquire")
	}
	if s.TryAcquire() {
		t.Error("should block")
	}
	s.Release()
	if s.TryAcquire() {
		t.Log("acquired after release")
	} else {
		t.Error("should acquire after release")
	}
}
func TestGovernor(t *testing.T) {
	g := NewGovernor(10, time.Second, 5, 100)
	done := make(chan bool)
	ok := g.TryProcess(func() { done <- true })
	if !ok {
		t.Error("should allow")
	}
	select {
	case <-done:
		t.Log("processed")
	case <-time.After(time.Second):
		t.Error("timeout")
	}
}
func TestBackpressure(t *testing.T) {
	bp := NewBackpressure(5)
	bp.SetDepth(10)
	if !bp.Throttled() {
		t.Error("should throttle")
	}
}
