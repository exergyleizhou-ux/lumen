package builtin

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// End-to-end: a command run through the bash tool must NOT be able to read a
// secret-named env var out of the agent's environment, while non-secret vars
// (HOME) still work.
func TestBashToolScrubsSecretsFromChild(t *testing.T) {
	os.Setenv("LUMEN_TEST_SECRET_KEY", "leak-me-12345")
	defer os.Unsetenv("LUMEN_TEST_SECRET_KEY")
	bt := &BashTool{}
	out, _ := bt.Execute(context.Background(),
		json.RawMessage(`{"command":"echo SECRET=[$LUMEN_TEST_SECRET_KEY] HOME=[$HOME]"}`))
	if strings.Contains(out, "leak-me-12345") {
		t.Errorf("secret env var leaked to the bash child: %q", out)
	}
	if !strings.Contains(out, "SECRET=[]") {
		t.Errorf("expected the secret to be empty in the child, got %q", out)
	}
	if strings.Contains(out, "HOME=[]") {
		t.Errorf("non-secret HOME should still be present in the child, got %q", out)
	}
}

func TestScrubSecrets(t *testing.T) {
	in := []string{
		"DEEPSEEK_API_KEY=sk-secret",
		"OPENAI_API_KEY=sk-x",
		"GITHUB_TOKEN=ghp_x",
		"AWS_SECRET_ACCESS_KEY=abc",
		"DB_PASSWORD=hunter2",
		"GH_PAT=pat_x",
		"PATH=/usr/bin:/bin",
		"HOME=/home/u",
		"GOFLAGS=-mod=mod",
		"LANG=en_US.UTF-8",
		"MALFORMED_NO_EQUALS",
	}
	got := strings.Join(scrubSecrets(in), "\n")

	for _, leaked := range []string{"DEEPSEEK_API_KEY", "OPENAI_API_KEY", "GITHUB_TOKEN", "AWS_SECRET_ACCESS_KEY", "DB_PASSWORD", "GH_PAT"} {
		if strings.Contains(got, leaked) {
			t.Errorf("secret var %q must be scrubbed from the child env, but survived:\n%s", leaked, got)
		}
	}
	for _, kept := range []string{"PATH=/usr/bin", "HOME=/home/u", "GOFLAGS=-mod=mod", "LANG=en_US.UTF-8", "MALFORMED_NO_EQUALS"} {
		if !strings.Contains(got, kept) {
			t.Errorf("non-secret var %q must be preserved, but was dropped:\n%s", kept, got)
		}
	}
}
