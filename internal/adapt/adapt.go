// Package adapt provides adapter patterns for integrating Lumen agents
// with external systems: HTTP webhook receiver, gRPC client adapter,
// WebSocket relay, file system watcher bridge, and process supervisor.
package adapt

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Adapter is a named integration adapter.
type Adapter interface {
	Name() string
	Start(ctx context.Context) error
	Stop() error
	Status() string
}

// WebhookReceiver listens for incoming HTTP webhooks.
type WebhookReceiver struct {
	mu       sync.Mutex
	name     string
	addr     string
	server   *http.Server
	handlers map[string]func(payload []byte) error
	stats    *WebhookStats
}

// WebhookStats tracks webhook metrics.
type WebhookStats struct {
	Received  int64
	Succeeded int64
	Failed    int64
}

// NewWebhookReceiver creates a webhook receiver.
func NewWebhookReceiver(name, addr string) *WebhookReceiver {
	return &WebhookReceiver{
		name: name, addr: addr,
		handlers: map[string]func(payload []byte) error{},
		stats:    &WebhookStats{},
	}
}

func (wr *WebhookReceiver) Name() string { return wr.name }

// Handle registers a webhook path handler.
func (wr *WebhookReceiver) Handle(path string, handler func(payload []byte) error) {
	wr.mu.Lock()
	defer wr.mu.Unlock()
	wr.handlers[path] = handler
}

func (wr *WebhookReceiver) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		wr.mu.Lock()
		handler, ok := wr.handlers[r.URL.Path]
		wr.mu.Unlock()
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		atomic.AddInt64(&wr.stats.Received, 1)
		var payload []byte
		if r.Body != nil {
			payload, _ = io.ReadAll(r.Body)
			r.Body.Close()
		}
		if err := handler(payload); err != nil {
			atomic.AddInt64(&wr.stats.Failed, 1)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		atomic.AddInt64(&wr.stats.Succeeded, 1)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	})
	wr.mu.Lock()
	wr.server = &http.Server{Addr: wr.addr, Handler: mux}
	wr.mu.Unlock()
	go func() {
		if err := wr.server.ListenAndServe(); err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "webhook %s: %v\n", wr.name, err)
		}
	}()
	return nil
}

func (wr *WebhookReceiver) Stop() error {
	wr.mu.Lock()
	srv := wr.server
	wr.mu.Unlock()
	if srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(ctx)
	}
	return nil
}

func (wr *WebhookReceiver) Status() string {
	r, s, f := atomic.LoadInt64(&wr.stats.Received), atomic.LoadInt64(&wr.stats.Succeeded), atomic.LoadInt64(&wr.stats.Failed)
	return fmt.Sprintf("webhook receiver %s: received=%d succeeded=%d failed=%d", wr.name, r, s, f)
}

// Stats returns webhook statistics.
func (wr *WebhookReceiver) Stats() *WebhookStats {
	return &WebhookStats{
		Received:  atomic.LoadInt64(&wr.stats.Received),
		Succeeded: atomic.LoadInt64(&wr.stats.Succeeded),
		Failed:    atomic.LoadInt64(&wr.stats.Failed),
	}
}

// ── Process Supervisor ────────────────────────────────────

// ProcessSupervisor manages an external process lifecycle.
type ProcessSupervisor struct {
	mu       sync.Mutex
	name     string
	command  []string
	env      []string
	cmd      *exec.Cmd
	running  bool
	restarts int
	lastExit time.Time
}

// NewProcessSupervisor creates a process supervisor.
func NewProcessSupervisor(name string, command ...string) *ProcessSupervisor {
	return &ProcessSupervisor{name: name, command: command}
}

func (ps *ProcessSupervisor) Name() string { return ps.name }

// SetEnv sets environment variables for the process.
func (ps *ProcessSupervisor) SetEnv(env []string) { ps.mu.Lock(); defer ps.mu.Unlock(); ps.env = env }

func (ps *ProcessSupervisor) Start(ctx context.Context) error {
	ps.mu.Lock()
	ps.running = true
	ps.mu.Unlock()

	go func() {
		for ps.isRunning() {
			cmd := exec.CommandContext(ctx, ps.command[0], ps.command[1:]...)
			cmd.Env = ps.env
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			ps.mu.Lock()
			ps.cmd = cmd
			ps.mu.Unlock()

			if err := cmd.Start(); err != nil {
				fmt.Fprintf(os.Stderr, "supervisor %s: start failed: %v\n", ps.name, err)
				return
			}
			err := cmd.Wait()
			ps.mu.Lock()
			ps.lastExit = time.Now()
			ps.restarts++
			ps.mu.Unlock()

			if err != nil {
				fmt.Fprintf(os.Stderr, "supervisor %s: exited with %v, restarting...\n", ps.name, err)
				time.Sleep(time.Second)
			} else {
				break
			}
		}
	}()
	return nil
}

func (ps *ProcessSupervisor) Stop() error {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.running = false
	if ps.cmd != nil && ps.cmd.Process != nil {
		return ps.cmd.Process.Signal(os.Interrupt)
	}
	return nil
}

func (ps *ProcessSupervisor) Status() string {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if ps.running {
		return fmt.Sprintf("process %s: running (restarts: %d)", ps.name, ps.restarts)
	}
	return fmt.Sprintf("process %s: stopped (last exit: %v)", ps.name, ps.lastExit)
}

func (ps *ProcessSupervisor) isRunning() bool {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.running
}

// ── Adapter Registry ──────────────────────────────────────

// Registry manages all adapters.
type Registry struct {
	mu       sync.Mutex
	adapters map[string]Adapter
}

// NewRegistry creates an adapter registry.
func NewRegistry() *Registry {
	return &Registry{adapters: map[string]Adapter{}}
}

// Register adds an adapter.
func (r *Registry) Register(a Adapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters[a.Name()] = a
}

// Get retrieves an adapter by name.
func (r *Registry) Get(name string) (Adapter, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.adapters[name]
	return a, ok
}

// StartAll starts all registered adapters.
func (r *Registry) StartAll(ctx context.Context) error {
	r.mu.Lock()
	adapters := make([]Adapter, 0, len(r.adapters))
	for _, a := range r.adapters {
		adapters = append(adapters, a)
	}
	r.mu.Unlock()
	for _, a := range adapters {
		if err := a.Start(ctx); err != nil {
			return fmt.Errorf("adapter %s: %w", a.Name(), err)
		}
	}
	return nil
}

// StopAll stops all registered adapters.
func (r *Registry) StopAll() error {
	r.mu.Lock()
	adapters := make([]Adapter, 0, len(r.adapters))
	for _, a := range r.adapters {
		adapters = append(adapters, a)
	}
	r.mu.Unlock()
	var errs []error
	for _, a := range adapters {
		if err := a.Stop(); err != nil {
			errs = append(errs, fmt.Errorf("adapter %s: %w", a.Name(), err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("stop errors: %v", errs)
	}
	return nil
}

// Status returns status of all adapters.
func (r *Registry) Status() map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[string]string{}
	for name, a := range r.adapters {
		out[name] = a.Status()
	}
	return out
}

// FormatStatus formats adapter statuses.
func (r *Registry) FormatStatus() string {
	var sb strings.Builder
	statuses := r.Status()
	keys := make([]string, 0, len(statuses))
	for k := range statuses {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Fprintf(&sb, "Adapter Status (%d):\n%s\n\n", len(statuses), strings.Repeat("─", 50))
	for _, k := range keys {
		fmt.Fprintf(&sb, "  %s\n", statuses[k])
	}
	return sb.String()
}

// ── Helpers ────────────────────────────────────────────────
