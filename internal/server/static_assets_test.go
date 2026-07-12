package server

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestEmbeddedAppJSSyntax(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	cmd := exec.Command(node, "--check", "static/assets/app.js")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("embedded app.js must parse: %v\n%s", err, out)
	}
}

func TestEmbeddedAppJSRunReplayContract(t *testing.T) {
	data, err := os.ReadFile("static/assets/app.js")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, required := range []string{"/v1/runs/", "run_id", "after=", "currentRunSeq"} {
		if !strings.Contains(source, required) {
			t.Errorf("app.js missing run replay contract %q", required)
		}
	}
}
