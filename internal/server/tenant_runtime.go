package server

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"lumen/internal/control"
	"lumen/internal/event"
	"lumen/internal/hostedauth"
	"lumen/internal/runstate"
	"lumen/internal/workspace"
)

type requestRuntime struct {
	owner   runstate.Owner
	session string
	ctrl    *control.Controller
	ws      workspace.Context
	entry   *serverController
}

func (s *Server) configureRuntime(rt *requestRuntime, sink event.Sink, cfgPath string) error {
	if rt.entry == nil {
		return rt.ctrl.Configure(sink, nil, cfgPath)
	}
	if rt.entry.configured {
		rt.ctrl.SetSink(sink)
		return nil
	}
	err := rt.ctrl.ConfigureWithOptions(sink, nil, cfgPath, control.ConfigureOptions{Workspace: rt.ws, DataRoot: filepath.Join(rt.ws.Root, ".lumen")})
	if err == nil {
		rt.entry.configured = true
	}
	return err
}

func (s *Server) tenantWorkspace(owner runstate.Owner) (workspace.Context, error) {
	root := os.Getenv("HOSTED_WORKSPACE_ROOT")
	if root == "" {
		return workspace.Context{}, fmt.Errorf("hosted workspace root unavailable")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return workspace.Context{}, err
	}
	guard, err := workspace.NewLocal("hosted", root, "host", nil)
	if err != nil {
		return workspace.Context{}, err
	}
	tenantRoot, err := guard.Backend.Resolve(filepath.Join(owner.UserID, owner.WorkspaceID), true)
	if err != nil {
		return workspace.Context{}, err
	}
	if err := os.MkdirAll(tenantRoot, 0700); err != nil {
		return workspace.Context{}, err
	}
	return workspace.NewLocal(owner.WorkspaceID, tenantRoot, owner.UserID, nil)
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
