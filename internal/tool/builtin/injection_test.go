package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lumen/internal/untrusted"
)

// TestWebFetchBlocksSSRF: web_fetch must refuse non-public targets and non-http
// schemes before connecting.
func TestWebFetchBlocksSSRF(t *testing.T) {
	t.Setenv(EnvWebFetchAllowLocal, "")
	wf := &WebFetchTool{}
	for _, u := range []string{
		"http://169.254.169.254/latest/meta-data/", // cloud metadata
		"http://127.0.0.1:8080/admin",              // loopback
		"http://10.1.2.3/internal",                 // private
		"file:///etc/passwd",                       // scheme
	} {
		args, _ := json.Marshal(map[string]string{"url": u})
		if _, err := wf.Execute(context.Background(), json.RawMessage(args)); err == nil {
			t.Errorf("web_fetch(%q) should be blocked, got nil error", u)
		}
	}
}

// TestReadFileUntrustedWrapOptIn: read_file wraps content as untrusted only when
// LUMEN_UNTRUSTED_READS is set; default behavior is unchanged.
func TestReadFileUntrustedWrapOptIn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(path, []byte("plain file body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rf := &ReadFileTool{}
	args, _ := json.Marshal(map[string]string{"path": path})

	// Default: no wrapping.
	t.Setenv(untrusted.EnvUntrustedReads, "")
	out, err := rf.Execute(context.Background(), json.RawMessage(args))
	if err != nil {
		t.Fatalf("read_file failed: %v", err)
	}
	if strings.Contains(out, "UNTRUSTED CONTENT") {
		t.Error("read_file must NOT wrap by default")
	}
	if !strings.Contains(out, "plain file body") {
		t.Errorf("read_file lost the content: %q", out)
	}

	// Opt-in: wrapped.
	t.Setenv(untrusted.EnvUntrustedReads, "1")
	out2, err := rf.Execute(context.Background(), json.RawMessage(args))
	if err != nil {
		t.Fatalf("read_file (wrapped) failed: %v", err)
	}
	if !strings.Contains(out2, "UNTRUSTED CONTENT") {
		t.Error("read_file should wrap content when LUMEN_UNTRUSTED_READS is set")
	}
	if !strings.Contains(out2, "plain file body") {
		t.Error("wrapped read_file must still contain the file body")
	}
}
