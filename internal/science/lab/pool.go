package lab

import (
	"fmt"
	"sync"
	"time"

	"lumen/internal/config"
	"lumen/internal/science/lab/project"
	labruntime "lumen/internal/science/lab/runtime"
)

// turnPool limits concurrent chat turns (anti-meltdown under stress).
type turnPool struct {
	sem chan struct{}
}

func newTurnPool(n int) *turnPool {
	if n < 1 {
		n = 1
	}
	return &turnPool{sem: make(chan struct{}, n)}
}

func (p *turnPool) tryAcquire() bool {
	select {
	case p.sem <- struct{}{}:
		return true
	default:
		return false
	}
}

func (p *turnPool) release() {
	select {
	case <-p.sem:
	default:
	}
}

func (p *turnPool) active() int {
	return len(p.sem)
}

func (p *turnPool) capacity() int {
	return cap(p.sem)
}

// controllerPool keeps one Controller per project slug (isolation + reuse).
type controllerPool struct {
	mu       sync.Mutex
	sciDir   string
	fleet    *labruntime.FleetManager
	projects *project.Store
	provider *config.ProviderConfig
	basePATH string
	items    map[string]*poolEntry
	max      int
}

func (p *controllerPool) setPlatformProvider(pc *config.ProviderConfig, basePATH string) {
	if pc != nil {
		copy := *pc
		p.provider = &copy
	}
	p.basePATH = basePATH
}

type poolEntry struct {
	ctrl     *Controller
	lastUsed time.Time
	busy     bool
}

func newControllerPool(sciDir string, fleet *labruntime.FleetManager, projects *project.Store, max int) *controllerPool {
	if max < 1 {
		max = MaxControllers
	}
	return &controllerPool{
		sciDir:   sciDir,
		fleet:    fleet,
		projects: projects,
		items:    make(map[string]*poolEntry),
		max:      max,
	}
}

// acquire returns a free controller for slug, or error if project is already mid-turn.
func (p *controllerPool) acquire(slug string) (*Controller, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if e, ok := p.items[slug]; ok {
		if e.busy {
			return nil, fmt.Errorf("project %q is already running a turn", slug)
		}
		e.busy = true
		e.lastUsed = time.Now()
		return e.ctrl, nil
	}
	// Evict idle LRU if full
	for len(p.items) >= p.max {
		var oldestKey string
		var oldestTime time.Time
		for k, e := range p.items {
			if e.busy {
				continue
			}
			if oldestKey == "" || e.lastUsed.Before(oldestTime) {
				oldestKey = k
				oldestTime = e.lastUsed
			}
		}
		if oldestKey == "" {
			return nil, fmt.Errorf("controller pool full (%d busy)", p.max)
		}
		p.items[oldestKey].ctrl.Close()
		delete(p.items, oldestKey)
	}
	c := newControllerWithPlatformProvider(p.sciDir, p.fleet, p.projects, p.provider, p.basePATH)
	p.items[slug] = &poolEntry{ctrl: c, lastUsed: time.Now(), busy: true}
	return c, nil
}

func (p *controllerPool) release(slug string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if e, ok := p.items[slug]; ok {
		e.busy = false
		e.lastUsed = time.Now()
	}
}

// discard removes a poisoned controller (for example, after Configure timed
// out). The identity check prevents an old goroutine from deleting a newer
// replacement for the same project.
func (p *controllerPool) discard(slug string, ctrl *Controller) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if e := p.items[slug]; e != nil && e.ctrl == ctrl {
		delete(p.items, slug)
		go e.ctrl.Close()
	}
}

func (p *controllerPool) close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for key, e := range p.items {
		e.ctrl.Close()
		delete(p.items, key)
	}
}

func (p *controllerPool) stats() (total, busy int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	total = len(p.items)
	for _, e := range p.items {
		if e.busy {
			busy++
		}
	}
	return total, busy
}
