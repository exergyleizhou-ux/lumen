package server

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
	"lumen/internal/config"
	"lumen/internal/control"
	"lumen/internal/event"
	"lumen/internal/hostedauth"
	"lumen/internal/runstate"
	"lumen/internal/workspace"
)

type requestRuntime struct {
	owner         runstate.Owner
	session       string
	ctrl          *control.Controller
	ws            workspace.Context
	entry         *serverController
	configureTest func()
	provider      *config.ProviderConfig
}

func (s *Server) configureRuntime(rt *requestRuntime, sink event.Sink, cfgPath string) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("configure panic: %v", v)
		}
	}()
	if rt.configureTest != nil {
		rt.configureTest()
	}
	if rt.entry == nil {
		if rt.provider == nil && cfgPath == "" && rt.ctrl.ProviderConfig() != nil {
			rt.ctrl.SetSink(sink)
			return nil
		}
		return rt.ctrl.ConfigureWithOptions(sink, nil, cfgPath, control.ConfigureOptions{Workspace: rt.ws, Provider: rt.provider, ProcessEnvImmutable: true})
	}
	if rt.entry.configured {
		if rt.provider != nil && rt.entry.providerKey != rt.provider.Name+"\x00"+rt.provider.Model {
			return fmt.Errorf("provider/model differs from the session's immutable configuration")
		}
		rt.ctrl.SetSink(sink)
		return nil
	}
	err = rt.ctrl.ConfigureWithOptions(sink, nil, cfgPath, control.ConfigureOptions{Workspace: rt.ws, DataRoot: filepath.Join(rt.ws.Root, ".lumen"), Provider: rt.provider, ProcessEnvImmutable: s.auth != nil})
	if err == nil {
		rt.entry.configured = true
		if rt.provider != nil {
			rt.entry.providerKey = rt.provider.Name + "\x00" + rt.provider.Model
		}
	}
	return err
}

func (s *Server) tenantWorkspace(owner runstate.Owner) (workspace.Context, error) {
	root := s.cfg.HostedWorkspaceRoot
	if root == "" {
		return workspace.Context{}, fmt.Errorf("hosted workspace root unavailable")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return workspace.Context{}, err
	}
	if !safeTenantComponent(owner.UserID) || !safeTenantComponent(owner.WorkspaceID) {
		return workspace.Context{}, fmt.Errorf("invalid tenant identity")
	}
	if err := mkdirTenantAt(root, owner.UserID, owner.WorkspaceID); err != nil {
		return workspace.Context{}, err
	}
	tenantRoot := filepath.Join(root, owner.UserID, owner.WorkspaceID)
	return workspace.NewLocal(owner.WorkspaceID, tenantRoot, owner.UserID, nil)
}

func safeTenantComponent(value string) bool {
	if value == "" || value == "." || value == ".." || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// mkdirTenantAt creates user/workspace below an already-open hosted root.
// Every component is opened with O_NOFOLLOW before the next is created, so a
// symlink swap cannot redirect mkdir outside HOSTED_WORKSPACE_ROOT.
func mkdirTenantAt(root, user, workspaceID string) error {
	rootFD, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open hosted root: %w", err)
	}
	defer unix.Close(rootFD)
	userFD, err := mkdirOpenDirAt(rootFD, user)
	if err != nil {
		return fmt.Errorf("open tenant user: %w", err)
	}
	defer unix.Close(userFD)
	workspaceFD, err := mkdirOpenDirAt(userFD, workspaceID)
	if err != nil {
		return fmt.Errorf("open tenant workspace: %w", err)
	}
	return unix.Close(workspaceFD)
}

func mkdirOpenDirAt(parent int, name string) (int, error) {
	if err := unix.Mkdirat(parent, name, 0700); err != nil && !errors.Is(err, syscall.EEXIST) {
		return -1, err
	}
	return unix.Openat(parent, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
}

func (s *Server) acquireRuntime(r *http.Request) (*requestRuntime, error) {
	owner := ownerFromRequest(r)
	if s.auth == nil {
		wd := s.workspaceRoot()
		ws, _ := workspace.NewLocal("local", wd, "", nil)
		return &requestRuntime{owner: owner, session: "local", ctrl: s.cfg.Ctrl, ws: ws}, nil
	}
	id, ok := hostedauth.FromContext(r.Context())
	if !ok {
		return nil, fmt.Errorf("hosted identity unavailable")
	}
	ws, err := s.tenantWorkspace(owner)
	if err != nil {
		return nil, err
	}
	e, err := s.controllers.acquire(owner, id.SessionID, ws)
	if err != nil {
		return nil, err
	}
	return &requestRuntime{owner: owner, session: id.SessionID, ctrl: e.Controller, ws: ws, entry: e}, nil
}

func (s *Server) releaseRuntime(rt *requestRuntime) {
	if rt != nil && rt.entry != nil {
		s.controllers.release(rt.owner, rt.session)
	}
}

func (s *Server) runtimeOrError(w http.ResponseWriter, r *http.Request) *requestRuntime {
	rt, err := s.acquireRuntime(r)
	if err != nil {
		if err == ErrControllerBusy {
			w.Header().Set("Retry-After", "1")
			jsonErr(w, err.Error(), http.StatusTooManyRequests)
		} else {
			jsonErr(w, err.Error(), http.StatusServiceUnavailable)
		}
		return nil
	}
	return rt
}

func (s *Server) resolveRuntimePath(rt *requestRuntime, rel string, allowMissing bool) (string, error) {
	if rt == nil || rt.ws.Backend == nil {
		return "", fmt.Errorf("workspace unavailable")
	}
	return rt.ws.Backend.Resolve(rel, allowMissing)
}
