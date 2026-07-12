package server

import (
	"os/exec"
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
