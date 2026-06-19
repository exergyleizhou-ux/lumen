package control

import (
	"os"
	"path/filepath"
	"testing"

	"lumen/internal/agent"
	"lumen/internal/editverify"
	"lumen/internal/tool"
)

// setupEditVerify must activate the verify-after-edit loop in ANY recognized
// project (Go / JS-TS / Python), not just Go, and stay inert when verification
// is disabled or the dir isn't a recognized project. Regression guard for the
// old go.mod-only gate that left the self-repair loop dead in Python/JS repos.
func TestSetupEditVerify_ActivatesByProjectType(t *testing.T) {
	cases := []struct {
		name    string
		marker  string
		enabled bool
		want    bool
	}{
		{"go module", "go.mod", true, true},
		{"js package", "package.json", true, true},
		{"python project", "pyproject.toml", true, true},
		{"unrecognized dir", "", true, false},
		{"disabled config in go project", "go.mod", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.marker != "" {
				if err := os.WriteFile(filepath.Join(dir, tc.marker), []byte("x"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			ag := agent.New(&errProvider{name: "test"}, tool.NewRegistry(), agent.NewSession(""), agent.Options{})
			c := &Controller{ag: ag}
			cfg := editverify.DefaultConfig()
			cfg.Enabled = tc.enabled
			if got := c.setupEditVerify(dir, cfg); got != tc.want {
				t.Errorf("setupEditVerify(%s) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}
