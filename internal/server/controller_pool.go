package server

import (
	"errors"
	"sync"
	"time"

	"lumen/internal/control"
	"lumen/internal/runstate"
	"lumen/internal/workspace"
)

var ErrControllerBusy = errors.New("controller capacity reached")

type controllerKey struct {
	Owner     runstate.Owner
	SessionID string
}
type serverController struct {
	Controller *control.Controller
	Workspace  workspace.Context
	lastUsed   time.Time
	busy       bool
}
type controllerLimits struct{ Global, PerUser, PerWorkspace int }
type serverControllerPool struct {
	mu      sync.Mutex
	limits  controllerLimits
	entries map[controllerKey]*serverController
	factory func() *control.Controller
	now     func() time.Time
}

func newServerControllerPool(limits controllerLimits) *serverControllerPool {
	if limits.Global < 1 {
		limits.Global = 32
	}
	if limits.PerUser < 1 {
		limits.PerUser = 8
	}
	if limits.PerWorkspace < 1 {
		limits.PerWorkspace = 4
	}
	return &serverControllerPool{limits: limits, entries: map[controllerKey]*serverController{}, factory: control.New, now: time.Now}
}

func (p *serverControllerPool) acquire(owner runstate.Owner, sessionID string, ws workspace.Context) (*serverController, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := controllerKey{Owner: owner, SessionID: sessionID}
	if e := p.entries[key]; e != nil {
		if e.busy {
			return nil, ErrControllerBusy
		}
		e.busy = true
		e.lastUsed = p.now()
		return e, nil
	}
	userN, workspaceN := 0, 0
	for k := range p.entries {
		if k.Owner.UserID == owner.UserID {
			userN++
		}
		if k.Owner == owner {
			workspaceN++
		}
	}
	if len(p.entries) >= p.limits.Global || userN >= p.limits.PerUser || workspaceN >= p.limits.PerWorkspace {
		return nil, ErrControllerBusy
	}
	e := &serverController{Controller: p.factory(), Workspace: ws, busy: true, lastUsed: p.now()}
	p.entries[key] = e
	return e, nil
}
func (p *serverControllerPool) release(owner runstate.Owner, sessionID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if e := p.entries[controllerKey{Owner: owner, SessionID: sessionID}]; e != nil {
		e.busy = false
		e.lastUsed = p.now()
	}
}
