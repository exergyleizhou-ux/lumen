package lab

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"lumen/internal/runstate"
	"lumen/internal/science/lab/project"
	labruntime "lumen/internal/science/lab/runtime"
	labworkspace "lumen/internal/science/lab/workspace"
	coreworkspace "lumen/internal/workspace"
)

const EnvHostedWorkspaceRoot = "HOSTED_WORKSPACE_ROOT"

var safeTenantComponent = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type tenantResources struct {
	Owner       runstate.Owner
	Root        string
	Workspace   coreworkspace.Context
	Projects    *project.Store
	Controllers *controllerPool
	lastUsed    time.Time
	busy        int
}

type tenantRegistry struct {
	mu      sync.Mutex
	root    coreworkspace.Context
	guard   *labworkspace.Guard
	fleet   *labruntime.FleetManager
	max     int
	idleTTL time.Duration
	now     func() time.Time
	items   map[runstate.Owner]*tenantResources
	onEvict func(runstate.Owner)
}

func newTenantRegistry(root string, fleet *labruntime.FleetManager, max int, idleTTL time.Duration) (*tenantRegistry, error) {
	if max < 1 {
		return nil, fmt.Errorf("tenant capacity must be positive")
	}
	if idleTTL <= 0 {
		idleTTL = 30 * time.Minute
	}
	fsGuard, err := labworkspace.NewGuard(root)
	if err != nil {
		return nil, err
	}
	guard, err := coreworkspace.NewLocal("hosted", root, "host", nil)
	if err != nil {
		return nil, err
	}
	return &tenantRegistry{root: guard, guard: fsGuard, fleet: fleet, max: max, idleTTL: idleTTL, now: time.Now, items: make(map[runstate.Owner]*tenantResources)}, nil
}

func (r *tenantRegistry) acquire(owner runstate.Owner) (*tenantResources, error) {
	if !owner.Valid() || !safeTenantComponent.MatchString(owner.UserID) || !safeTenantComponent.MatchString(owner.WorkspaceID) {
		return nil, fmt.Errorf("invalid tenant identity")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	if t := r.items[owner]; t != nil {
		t.busy++
		t.lastUsed = now
		return t, nil
	}
	r.cleanupLocked(now)
	if len(r.items) >= r.max {
		return nil, fmt.Errorf("tenant registry capacity reached")
	}
	rel := filepath.Join(owner.UserID, owner.WorkspaceID, "science")
	root, err := r.root.Backend.Resolve(rel, true)
	if err != nil {
		return nil, err
	}
	if err := r.guard.MkdirAll(rel, 0o700); err != nil {
		return nil, err
	}
	ws, err := coreworkspace.NewLocal(owner.WorkspaceID, root, owner.UserID, nil)
	if err != nil {
		return nil, err
	}
	projects := project.NewStore(root)
	t := &tenantResources{Owner: owner, Root: root, Workspace: ws, Projects: projects, Controllers: newControllerPool(root, r.fleet, projects, MaxControllers), lastUsed: now, busy: 1}
	r.items[owner] = t
	return t, nil
}

func (r *tenantRegistry) release(owner runstate.Owner) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t := r.items[owner]; t != nil {
		if t.busy > 0 {
			t.busy--
		}
		t.lastUsed = r.now()
	}
}

func (r *tenantRegistry) cleanupLocked(now time.Time) {
	for owner, t := range r.items {
		if t.busy == 0 && now.Sub(t.lastUsed) >= r.idleTTL {
			r.evictLocked(owner, t)
		}
	}
	for len(r.items) >= r.max {
		var victim runstate.Owner
		var oldest time.Time
		for owner, t := range r.items {
			if t.busy == 0 && (oldest.IsZero() || t.lastUsed.Before(oldest)) {
				victim, oldest = owner, t.lastUsed
			}
		}
		if oldest.IsZero() {
			return
		}
		r.evictLocked(victim, r.items[victim])
	}
}

func (r *tenantRegistry) evictLocked(owner runstate.Owner, t *tenantResources) {
	if t != nil && t.Controllers != nil {
		t.Controllers.close()
	}
	delete(r.items, owner)
	if r.onEvict != nil {
		r.onEvict(owner)
	}
}
