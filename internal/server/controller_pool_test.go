package server

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"lumen/internal/runstate"
	"lumen/internal/workspace"
)

func poolWorkspace(t *testing.T, owner runstate.Owner) workspace.Context {
	t.Helper()
	root := filepath.Join(t.TempDir(), owner.UserID, owner.WorkspaceID)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.NewLocal(owner.WorkspaceID, root, owner.UserID, nil)
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

func TestControllerPoolIdleSessionsReleaseQuota(t *testing.T) {
	now := time.Now()
	p := newServerControllerPool(controllerLimits{Global: 1, PerUser: 1, PerWorkspace: 1})
	p.now = func() time.Time { return now }
	p.idleTTL = time.Minute
	a := runstate.Owner{UserID: "a", WorkspaceID: "w"}
	if _, err := p.acquire(a, "old", poolWorkspace(t, a)); err != nil {
		t.Fatal(err)
	}
	p.release(a, "old")
	if _, err := p.acquire(a, "new", poolWorkspace(t, a)); !errors.Is(err, ErrControllerBusy) {
		t.Fatalf("quota should remain before ttl: %v", err)
	}
	now = now.Add(2 * time.Minute)
	if _, err := p.acquire(a, "new", poolWorkspace(t, a)); err != nil {
		t.Fatalf("expired session did not release quota: %v", err)
	}
}
func TestControllerPoolTenantSessionIsolation(t *testing.T) {
	p := newServerControllerPool(controllerLimits{Global: 4, PerUser: 3, PerWorkspace: 2})
	a := runstate.Owner{UserID: "a", WorkspaceID: "w"}
	b := runstate.Owner{UserID: "b", WorkspaceID: "w"}
	ea, err := p.acquire(a, "same", poolWorkspace(t, a))
	if err != nil {
		t.Fatal(err)
	}
	eb, err := p.acquire(b, "same", poolWorkspace(t, b))
	if err != nil {
		t.Fatal(err)
	}
	if ea.Controller == eb.Controller || ea.Workspace.Root == eb.Workspace.Root {
		t.Fatal("controller or workspace shared across tenants")
	}
}
func TestControllerPoolLimitsFailFast(t *testing.T) {
	p := newServerControllerPool(controllerLimits{Global: 2, PerUser: 1, PerWorkspace: 1})
	a := runstate.Owner{UserID: "a", WorkspaceID: "w"}
	if _, err := p.acquire(a, "1", poolWorkspace(t, a)); err != nil {
		t.Fatal(err)
	}
	if _, err := p.acquire(runstate.Owner{UserID: "a", WorkspaceID: "x"}, "2", poolWorkspace(t, runstate.Owner{UserID: "a", WorkspaceID: "x"})); !errors.Is(err, ErrControllerBusy) {
		t.Fatalf("per-user limit not enforced: %v", err)
	}
}
