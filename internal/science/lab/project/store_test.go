package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSlugFromTitle(t *testing.T) {
	if got := SlugFromTitle("EGFR 抑制剂"); got != "egfr" {
		t.Fatalf("slug = %q", got)
	}
}

func TestCreateProject(t *testing.T) {
	dir := t.TempDir()
	sci := filepath.Join(dir, "science")
	store := NewStore(sci)
	p, err := store.Create("Aspirin Study", "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Slug == "" {
		t.Fatal("empty slug")
	}
	ws, err := store.WorkspacePath(p.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(ws); err != nil || !st.IsDir() {
		t.Fatalf("workspace missing: %v", err)
	}
}
