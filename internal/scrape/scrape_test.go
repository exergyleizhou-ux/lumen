package scrape

import (
	"testing"
)

func TestExtractTitle(t *testing.T) {
	e := NewExtractor()
	p := e.Extract("<html><head><title>Test</title></head><body>Content</body></html>", "http://example.com")
	if p.Title != "Test" {
		t.Error("title")
	}
}
func TestExtractLinks(t *testing.T) {
	e := NewExtractor()
	p := e.Extract(`<a href="/about">About</a>`, "http://example.com")
	if len(p.Links) == 0 {
		t.Error("links")
	}
}
func TestExtractMeta(t *testing.T) {
	e := NewExtractor()
	p := e.Extract(`<meta name="description" content="A page">`, "")
	if p.Description != "A page" {
		t.Error("meta")
	}
}
