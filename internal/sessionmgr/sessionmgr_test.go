package sessionmgr

import (
	"testing"
)

func TestNewManager(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	sessions, _ := m.List()
	if len(sessions) != 0 {
		t.Error("new manager should have no sessions")
	}
}

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	m, _ := NewManager(dir)

	info := Info{ID: "test-1", Title: "Test Session", Model: "test", Provider: "test"}
	m.Save(info)

	loaded, err := m.Load("test-1")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Title != "Test Session" {
		t.Errorf("title: got %q", loaded.Title)
	}
}

func TestDelete(t *testing.T) {
	dir := t.TempDir()
	m, _ := NewManager(dir)

	m.Save(Info{ID: "delete-me", Title: "temp"})
	m.Delete("delete-me")

	_, err := m.Load("delete-me")
	if err == nil {
		t.Error("should error after delete")
	}
}

func TestNewID(t *testing.T) {
	id := NewID()
	if id == "" {
		t.Error("NewID should not be empty")
	}
}

func TestTitleFromPrompt(t *testing.T) {
	if TitleFromPrompt("hello world") != "hello world" {
		t.Error("short prompt unchanged")
	}
	long := "this is a very long prompt that exceeds sixty characters and should be truncated"
	if len(TitleFromPrompt(long)) > 60 {
		t.Error("long prompt should be truncated")
	}
}

func TestSessionStore(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewSessionStore(dir)

	id := "transcript-1"
	msgs := []map[string]any{{"role": "user", "content": "hello"}}
	s.SaveTranscript(id, msgs)

	loaded, err := s.LoadTranscript(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 {
		t.Errorf("expected 1 message, got %d", len(loaded))
	}
}

func TestFormatList(t *testing.T) {
	sessions := []Info{
		{Title: "test", Provider: "deepseek", Model: "chat", Messages: 10},
	}
	out := FormatList(sessions)
	if out == "" {
		t.Error("FormatList should return non-empty")
	}
}
