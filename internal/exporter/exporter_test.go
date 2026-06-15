package exporter

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExportJSON(t *testing.T) {
	dir := t.TempDir()
	data := &SessionData{
		ID: "test", Title: "Test Session", Date: time.Now(),
		Messages: []MessageData{{Role: "user", Content: "hello"}},
		Usage:    UsageData{TotalTokens: 100},
	}
	err := Export(data, FormatJSON, filepath.Join(dir, "test.json"))
	if err != nil {
		t.Fatal(err)
	}
}

func TestExportMarkdown(t *testing.T) {
	dir := t.TempDir()
	data := &SessionData{ID: "md", Title: "MD Test", Date: time.Now(),
		Messages: []MessageData{{Role: "user", Content: "hi"}, {Role: "assistant", Content: "hello"}},
	}
	err := Export(data, FormatMarkdown, filepath.Join(dir, "test.md"))
	if err != nil {
		t.Fatal(err)
	}
}

func TestExportHTML(t *testing.T) {
	dir := t.TempDir()
	data := &SessionData{ID: "html", Title: "HTML Test", Date: time.Now(),
		Messages: []MessageData{{Role: "user", Content: "hi"}},
	}
	err := Export(data, FormatHTML, filepath.Join(dir, "test.html"))
	if err != nil {
		t.Fatal(err)
	}
}

func TestExportCSV(t *testing.T) {
	dir := t.TempDir()
	data := &SessionData{ID: "csv", Title: "CSV Test", Date: time.Now(),
		Messages: []MessageData{{Role: "user", Content: "hello"}},
	}
	err := Export(data, FormatCSV, filepath.Join(dir, "test.csv"))
	if err != nil {
		t.Fatal(err)
	}
}

func TestToJSON(t *testing.T) {
	data := &SessionData{ID: "j", Title: "Test"}
	out, err := ToJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if out == "" {
		t.Error("ToJSON should not be empty")
	}
}

func TestToMarkdown(t *testing.T) {
	data := &SessionData{ID: "m", Title: "Test", Messages: []MessageData{{Role: "user", Content: "hi"}}}
	out := ToMarkdown(data)
	if out == "" {
		t.Error("ToMarkdown should not be empty")
	}
}

func TestExportDiff(t *testing.T) {
	dir := t.TempDir()
	diffs := []DiffData{{FilePath: "test.go", Before: "old", After: "new"}}
	err := ExportDiff(diffs, filepath.Join(dir, "diff.md"))
	if err != nil {
		t.Fatal(err)
	}
}

func TestExportUnknown(t *testing.T) {
	err := Export(&SessionData{}, "unknown", "/tmp/test")
	if err == nil {
		t.Error("unknown format should error")
	}
}
