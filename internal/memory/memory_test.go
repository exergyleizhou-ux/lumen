package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewStoreEmptyWorkspace(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	prompt := s.Prompt()
	files := s.Files()
	t.Logf("files found: %d, prompt length: %d", len(files), len(prompt))
	// An empty temp dir should not have project memory files
	// (home dir memory may still load, which is expected)
}

func TestStoreLoadsAgentMd(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("## Project Rules\n\nAlways use tabs."), 0o644)

	s, _ := NewStore(dir)
	prompt := s.Prompt()
	if !strings.Contains(prompt, "Always use tabs") {
		t.Errorf("prompt should contain AGENTS.md content: %q", prompt)
	}
}

func TestStoreLoadsClaudeMd(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("claude rules here"), 0o644)

	s, _ := NewStore(dir)
	if !strings.Contains(s.Prompt(), "claude rules here") {
		t.Error("should load CLAUDE.md")
	}
}

func TestDurableStoreRemember(t *testing.T) {
	dir := t.TempDir()
	ds := NewDurableStore(dir)
	slug, err := ds.Remember("Test Memory", "This is a test fact.")
	if err != nil {
		t.Fatalf("Remember: %v", err)
	}
	if slug == "" {
		t.Error("slug should not be empty")
	}
	// Verify file exists
	path := filepath.Join(dir, ".reasonix", "memory", slug+".md")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("memory file should exist: %v", err)
	}
}

func TestDurableStoreForget(t *testing.T) {
	dir := t.TempDir()
	ds := NewDurableStore(dir)
	slug, _ := ds.Remember("To Delete", "Will be deleted.")
	err := ds.Forget(slug)
	if err != nil {
		t.Fatalf("Forget: %v", err)
	}
	path := filepath.Join(dir, ".reasonix", "memory", slug+".md")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("forgotten file should be removed")
	}
}

func TestSlugify(t *testing.T) {
	if slugify("Hello World") != "hello-world" {
		t.Errorf("slugify: got %q", slugify("Hello World"))
	}
	if slugify("Test: Thing!") != "test-thing" {
		t.Errorf("slugify special chars: got %q", slugify("Test: Thing!"))
	}
}

func TestFirstLineOr(t *testing.T) {
	if firstLineOr("line1\nline2", 80) != "line1" {
		t.Errorf("firstLineOr: got %q", firstLineOr("line1\nline2", 80))
	}
	if len(firstLineOr("1234567890abcdef", 10)) > 10 {
		t.Error("firstLineOr should truncate to maxLen")
	}
}
