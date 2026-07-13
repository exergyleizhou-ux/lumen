package lab

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"lumen/internal/config"
	"lumen/internal/event"
	sciconfig "lumen/internal/science/config"
	"lumen/internal/science/lab/project"
)

func TestConfigureDoesNotMutateProcessWorkspaceOrPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("LUMEN_WORKSPACE_ROOT", "sentinel-root")
	t.Setenv("PATH", "/sentinel/bin")
	cfgPath, err := config.UserConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := `default_model = "test"

[[providers]]
name = "test"
kind = "openai"
base_url = "http://127.0.0.1:1/v1"
model = "test"
api_key = "test"
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	sciDir := filepath.Join(home, ".lumen", "science")
	scienceCfg := sciconfig.Default()
	scienceCfg.Providers[sciconfig.DefaultProvider] = sciconfig.ProviderCfg{Key: "science-test"}
	if err := sciconfig.Save(sciDir, scienceCfg); err != nil {
		t.Fatal(err)
	}
	store := project.NewStore(sciDir)
	proj, err := store.Create("Workspace Isolation A", "")
	if err != nil {
		t.Fatal(err)
	}
	projB, err := store.Create("Workspace Isolation B", "")
	if err != nil {
		t.Fatal(err)
	}
	ctrl := NewController(sciDir, nil, store)
	ctrlB := NewController(sciDir, nil, store)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, item := range []struct {
		ctrl *Controller
		slug string
		sess string
	}{{ctrl, proj.Slug, "session-1"}, {ctrlB, projB.Slug, "session-2"}} {
		item := item
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := item.ctrl.Configure(item.slug, item.sess, event.Discard, nil); err != nil {
				errs <- fmt.Errorf("configure %s: %w", item.slug, err)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if got := os.Getenv("LUMEN_WORKSPACE_ROOT"); got != "sentinel-root" {
		t.Fatalf("Configure mutated LUMEN_WORKSPACE_ROOT: %q", got)
	}
	if got := os.Getenv("PATH"); got != "/sentinel/bin" {
		t.Fatalf("Configure mutated PATH: %q", got)
	}
	wantRoot, err := store.WorkspacePath(proj.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if got := ctrl.ctrl.WorkspaceContext().Root; got == "" || got != canonicalPath(t, wantRoot) {
		t.Fatalf("controller workspace=%q want %q", got, canonicalPath(t, wantRoot))
	}
	wantRootB, err := store.WorkspacePath(projB.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if got := ctrlB.ctrl.WorkspaceContext().Root; got == "" || got != canonicalPath(t, wantRootB) {
		t.Fatalf("controller B workspace=%q want %q", got, canonicalPath(t, wantRootB))
	}
}

func canonicalPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}
