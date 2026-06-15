package watchpoint

import (
	"testing"
)

func TestSetGet(t *testing.T) {
	w := NewWatcher()
	w.Set("key", "val")
	v, ok := w.GetValue("key")
	if !ok || v != "val" {
		t.Error("get")
	}
}
func TestDelete(t *testing.T) {
	w := NewWatcher()
	w.Set("x", 1)
	w.Delete("x")
	if _, ok := w.GetValue("x"); ok {
		t.Error("should be gone")
	}
}
func TestOnChange(t *testing.T) {
	w := NewWatcher()
	called := false
	w.OnChange("k", func(c Change) { called = true })
	w.Set("k", "new")
	if !called {
		t.Error("callback")
	}
}
func TestPaths(t *testing.T) {
	w := NewWatcher()
	w.Set("a", 1)
	w.Set("b", 2)
	if len(w.Paths()) != 2 {
		t.Error("paths")
	}
}
func TestLog(t *testing.T) {
	w := NewWatcher()
	w.Set("c", 3)
	w.Set("c", 4)
	if len(w.Log()) != 2 {
		t.Error("log")
	}
}
