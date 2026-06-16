package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyReplace_Success(t *testing.T) {
	got, err := applyReplace("hello world", "world", "lumen")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "hello lumen" {
		t.Errorf("got %q", got)
	}
}

func TestApplyReplace_NoOp(t *testing.T) {
	_, err := applyReplace("abc", "b", "b")
	if err == nil || !strings.Contains(err.Error(), "no-op") {
		t.Errorf("old==new should be a no-op error, got %v", err)
	}
}

func TestApplyReplace_Ambiguous(t *testing.T) {
	_, err := applyReplace("foo foo", "foo", "bar")
	if err == nil || !strings.Contains(err.Error(), "matches 2 times") {
		t.Errorf("expected ambiguity error, got %v", err)
	}
}

func TestApplyReplace_NotFoundPlain(t *testing.T) {
	_, err := applyReplace("hello", "xyz", "abc")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not-found, got %v", err)
	}
	if strings.Contains(err.Error(), "ignoring whitespace") {
		t.Errorf("plain not-found should not claim a whitespace match: %v", err)
	}
}

func TestApplyReplace_NotFoundWhitespaceHint(t *testing.T) {
	// File uses tab indentation; the edit supplies space indentation. Exact match
	// fails, but normalizing whitespace finds exactly one match → hint.
	content := "\tif x {\n\t\treturn\n\t}\n"
	old := "if x {\n  return\n}"
	_, err := applyReplace(content, old, "if x {\n\treturn // changed\n}")
	if err == nil {
		t.Fatal("expected error (exact match should fail)")
	}
	if !strings.Contains(err.Error(), "ignoring whitespace") {
		t.Errorf("expected whitespace hint, got %v", err)
	}
}

// TestMultiEditAtomicOnFailure verifies a mid-sequence failure leaves the file
// byte-for-byte untouched (no partial write).
func TestMultiEditAtomicOnFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	original := "alpha beta gamma\n"
	os.WriteFile(path, []byte(original), 0o644)

	mt := &MultiEditTool{}
	// edit 0 ok (alpha→A), edit 1 fails (nonexistent), so nothing should persist.
	args := `{"path":"` + path + `","edits":[{"old_string":"alpha","new_string":"A"},{"old_string":"NOPE","new_string":"x"}]}`
	_, err := mt.Execute(context.Background(), json.RawMessage(args))
	if err == nil {
		t.Fatal("expected failure on missing 2nd edit")
	}
	if !strings.Contains(err.Error(), "edit 1") {
		t.Errorf("error should point at edit 1, got %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != original {
		t.Errorf("file must be untouched on failure; got %q", string(data))
	}
}

func TestMultiEditAppliesAllOnSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	os.WriteFile(path, []byte("alpha beta gamma\n"), 0o644)

	mt := &MultiEditTool{}
	args := `{"path":"` + path + `","edits":[{"old_string":"alpha","new_string":"A"},{"old_string":"gamma","new_string":"G"}]}`
	if _, err := mt.Execute(context.Background(), json.RawMessage(args)); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "A beta G\n" {
		t.Errorf("got %q", string(data))
	}
}

func TestEditFileNoOpRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	os.WriteFile(path, []byte("keep me"), 0o644)

	et := &EditFileTool{}
	_, err := et.Execute(context.Background(), json.RawMessage(`{"path":"`+path+`","old_string":"keep","new_string":"keep"}`))
	if err == nil || !strings.Contains(err.Error(), "no-op") {
		t.Errorf("expected no-op rejection, got %v", err)
	}
}
