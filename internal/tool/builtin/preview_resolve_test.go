package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"lumen/internal/fileutil"
)

// Preview must resolve the path the same way Execute does (fileutil.ResolvePath),
// so the diff preview isn't dropped and the file it reports as changed matches
// the file Execute mutates — otherwise verify-after-edit is silently skipped.
// (On macOS t.TempDir() lives under /var → /private/var, so the raw vs resolved
// paths genuinely diverge.)
func TestEditFilePreview_ResolvesPathLikeExecute(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(f, []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}
	resolved, err := fileutil.ResolvePath(f)
	if err != nil {
		t.Fatal(err)
	}

	args, _ := json.Marshal(map[string]string{"path": f, "old_string": "world", "new_string": "there"})
	ch, err := (&EditFileTool{}).Preview(context.Background(), args)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if ch.Path != resolved {
		t.Errorf("EditFile Preview path = %q, want resolved %q (must match Execute)", ch.Path, resolved)
	}

	margs, _ := json.Marshal(map[string]any{"path": f, "edits": []map[string]string{{"old_string": "hello", "new_string": "hi"}}})
	mch, err := (&MultiEditTool{}).Preview(context.Background(), margs)
	if err != nil {
		t.Fatalf("multi preview: %v", err)
	}
	if mch.Path != resolved {
		t.Errorf("MultiEdit Preview path = %q, want resolved %q", mch.Path, resolved)
	}
}
