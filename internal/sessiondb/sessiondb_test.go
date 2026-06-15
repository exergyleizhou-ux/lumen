package sessiondb

import (
	"strings"
	"testing"
)

func TestMemoryStoreCreate(t *testing.T) {
	s := NewMemoryStore()
	s.CreateSession(&SessionRecord{ID: "t1", Title: "Test", Status: "active"})
	got, _ := s.GetSession("t1")
	if got.Title != "Test" {
		t.Error("title")
	}
}
func TestMemoryStoreList(t *testing.T) {
	s := NewMemoryStore()
	s.CreateSession(&SessionRecord{ID: "a", Title: "A", Status: "active"})
	s.CreateSession(&SessionRecord{ID: "b", Title: "B", Status: "completed"})
	all, _ := s.ListSessions(0, 10, "")
	if len(all) != 2 {
		t.Error("count")
	}
}
func TestMemoryStoreMessages(t *testing.T) {
	s := NewMemoryStore()
	s.CreateSession(&SessionRecord{ID: "s1", Title: "Chat"})
	s.AddMessage(&MessageRecord{SessionID: "s1", Role: "user", Content: "hi", Turn: 1})
	msgs, _ := s.GetMessages("s1")
	if len(msgs) != 1 {
		t.Error("msg count")
	}
}
func TestMemoryStoreCount(t *testing.T) {
	s := NewMemoryStore()
	s.CreateSession(&SessionRecord{ID: "1", Title: "a"})
	s.CreateSession(&SessionRecord{ID: "2", Title: "b"})
	if s.Count() != 2 {
		t.Error("count")
	}
}
func TestFormatSessions(t *testing.T) {
	o := FormatSessions([]*SessionRecord{{Title: "test", Provider: "ds", Model: "chat", MessageCount: 10}})
	if !strings.Contains(o, "test") {
		t.Error("format")
	}
}
