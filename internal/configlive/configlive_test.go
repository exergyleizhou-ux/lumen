package configlive

import (
	"testing"
)

func TestStoreSet(t *testing.T) {
	s := NewStore()
	s.Set("key", "val", "test")
	v, ok := s.Get("key")
	if !ok || v != "val" {
		t.Error("set/get")
	}
}
func TestStoreGetString(t *testing.T) {
	s := NewStore()
	s.Set("str", "hello", "test")
	if s.GetString("str") != "hello" {
		t.Error("getstring")
	}
}
func TestStoreHistory(t *testing.T) {
	s := NewStore()
	s.Set("k", "a", "")
	s.Set("k", "b", "")
	h := s.History(10)
	if len(h) != 2 {
		t.Error("history")
	}
}
func TestStoreWatch(t *testing.T) {
	s := NewStore()
	called := false
	s.Watch("k", func(k string, o, n any) { called = true })
	s.Set("k", "v", "")
	if !called {
		t.Error("watch")
	}
}
func TestRollback(t *testing.T) {
	s := NewStore()
	s.Set("k", "first", "")
	s.Set("k", "second", "")
	s.Rollback("k")
	v, _ := s.Get("k")
	if v != "first" {
		t.Error("rollback")
	}
}
