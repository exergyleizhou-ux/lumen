package reliability

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGenerate_emptyDir(t *testing.T) {
	dir := t.TempDir()
	r := Generate(dir, 2026, 6)
	if r.Sessions != 0 || r.Turns != 0 {
		t.Errorf("empty dir should produce zero report, got sessions=%d turns=%d", r.Sessions, r.Turns)
	}
	if r.Period != "2026-06" {
		t.Errorf("Period=%q, want 2026-06", r.Period)
	}
}

func TestGenerate_withSessions(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "2026-06-01-test.jsonl"), []byte(
		`{"role":"user","content":"hello world"}
{"role":"assistant","content":"hi there"}
{"role":"user","content":"do stuff"}
{"role":"assistant","content":"verified ✓"}
`), 0644)

	r := Generate(dir, 2026, 6)
	if r.Sessions != 1 {
		t.Errorf("Sessions=%d, want 1", r.Sessions)
	}
	if r.Turns != 2 {
		t.Errorf("Turns=%d, want 2 (user messages)", r.Turns)
	}
	if r.VerifyPasses != 1 {
		t.Errorf("VerifyPasses=%d, want 1", r.VerifyPasses)
	}
}

func TestPrint_nonEmpty(t *testing.T) {
	r := Report{
		Period: "2026-06", Sessions: 5, Turns: 20, TotalTokens: 50000,
		TotalCost: 0.05, VerifyPasses: 18, VerifyFails: 2, Rollbacks: 1,
	}
	out := r.Print()
	if out == "" {
		t.Fatal("Print should return non-empty string")
	}
}

func TestSaveAndLatest(t *testing.T) {
	dir := t.TempDir()
	os.Setenv("HOME", dir)
	defer os.Unsetenv("HOME")

	reportsDir := filepath.Join(dir, ".lumen", "reports")
	os.MkdirAll(reportsDir, 0755)

	r := Report{Period: "2026-06", Sessions: 1}
	path, err := r.Save()
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("report file not created: %v", err)
	}

	latest := Latest()
	if latest != path {
		t.Errorf("Latest=%q, want %q", latest, path)
	}
}
