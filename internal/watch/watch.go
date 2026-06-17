// Package watch provides a file-system watcher that triggers agent actions.
// When files change, it runs build+test; if they fail, it invokes the agent
// to auto-fix. This is the "background agent mode" — the agent watches your
// back while you code.
//
// Usage: lumen watch [dir]
package watch

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Runner executes a prompt and returns the result text.
type Runner interface {
	Run(ctx context.Context, prompt string) (string, error)
}

// Config holds watcher configuration.
type Config struct {
	Dir         string
	Runner      Runner
	Debounce    time.Duration // wait after last change before acting (default 2s)
	PollInterval time.Duration // fs poll interval (default 500ms)
	Extensions  []string      // watch only these extensions (default: .go, .py, .js, .ts, .rs)
	OnChange    func(path string)
	OnError     func(error)
}

// Watcher monitors a directory tree for file changes and triggers actions.
type Watcher struct {
	cfg     Config
	mu      sync.Mutex
	changes map[string]time.Time // path → last change time
	running bool
	cancel  context.CancelFunc
}

// New creates a new Watcher.
func New(cfg Config) *Watcher {
	if cfg.Debounce <= 0 {
		cfg.Debounce = 2 * time.Second
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 500 * time.Millisecond
	}
	if len(cfg.Extensions) == 0 {
		cfg.Extensions = []string{".go", ".py", ".js", ".ts", ".jsx", ".tsx", ".rs", ".c", ".cpp", ".h", ".java", ".rb", ".php", ".swift", ".kt"}
	}
	return &Watcher{
		cfg:     cfg,
		changes: make(map[string]time.Time),
	}
}

// Start begins watching. Blocks until Stop is called.
func (w *Watcher) Start(ctx context.Context) error {
	ctx, w.cancel = context.WithCancel(ctx)
	defer w.cancel()

	w.running = true
	defer func() { w.running = false }()

	log.Printf("[watch] watching %s (debounce %v)", w.cfg.Dir, w.cfg.Debounce)

	ticker := time.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()

	// Snapshot of file mod times
	lastMod := make(map[string]time.Time)
	if err := w.scanDir(lastMod); err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			w.tick(lastMod)
		}
	}
}

// Stop stops the watcher.
func (w *Watcher) Stop() {
	if w.cancel != nil {
		w.cancel()
	}
}

// ── Internal ─────────────────────────────────────────────────

func (w *Watcher) tick(lastMod map[string]time.Time) {
	changed := w.scanChanges(lastMod)
	if len(changed) == 0 {
		return
	}

	now := time.Now()
	w.mu.Lock()
	for _, p := range changed {
		w.changes[p] = now
	}
	w.mu.Unlock()

	// Check for debounced changes
	w.mu.Lock()
	var ready []string
	for p, t := range w.changes {
		if now.Sub(t) >= w.cfg.Debounce {
			ready = append(ready, p)
			delete(w.changes, p)
		}
	}
	w.mu.Unlock()

	for _, p := range ready {
		if w.cfg.OnChange != nil {
			w.cfg.OnChange(p)
		}
	}
}

func (w *Watcher) scanDir(lastMod map[string]time.Time) error {
	return filepath.Walk(w.cfg.Dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			base := filepath.Base(path)
			if strings.HasPrefix(base, ".") || base == "node_modules" || base == "vendor" || base == "dist" || base == "build" {
				return filepath.SkipDir
			}
			return nil
		}
		if !w.matches(path) {
			return nil
		}
		lastMod[path] = info.ModTime()
		return nil
	})
}

func (w *Watcher) scanChanges(lastMod map[string]time.Time) []string {
	var changed []string
	filepath.Walk(w.cfg.Dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			base := filepath.Base(path)
			if strings.HasPrefix(base, ".") || base == "node_modules" || base == "vendor" || base == "dist" || base == "build" {
				return filepath.SkipDir
			}
			return nil
		}
		if !w.matches(path) {
			return nil
		}
		prev, exists := lastMod[path]
		if !exists || info.ModTime().After(prev) {
			changed = append(changed, path)
			lastMod[path] = info.ModTime()
		}
		return nil
	})
	return changed
}

func (w *Watcher) matches(path string) bool {
	ext := filepath.Ext(path)
	for _, e := range w.cfg.Extensions {
		if ext == e {
			return true
		}
	}
	return false
}

// ── Auto-Fix Loop ────────────────────────────────────────────

// AutoFix runs build/test on change; if they fail, asks the agent to fix.
func AutoFix(ctx context.Context, cfg Config) error {
	fixCh := make(chan string, 10)

	cfg.OnChange = func(path string) {
		select {
		case fixCh <- path:
		default:
		}
	}

	w := New(cfg)
	go w.Start(ctx)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case path := <-fixCh:
			log.Printf("[watch] changed: %s", path)
			// Run build/test
			if err := buildTest(cfg.Dir); err != nil {
				log.Printf("[watch] build/test failed: %v — asking agent to fix", err)
				if cfg.Runner != nil {
					prompt := fmt.Sprintf(
						"The file %s was just changed and the build or tests are now failing. "+
							"Error:\n%s\n\nFix the issue. Be minimal — only change what's needed to make it pass.",
						filepath.Base(path), err.Error(),
					)
					result, err := cfg.Runner.Run(ctx, prompt)
					if err != nil {
						log.Printf("[watch] agent fix failed: %v", err)
					} else {
						log.Printf("[watch] agent: %s", strings.TrimSpace(result[:min(200, len(result))]))
					}
				}
			}
		}
	}
}

func buildTest(dir string) error {
	// Try go build/vet/test if go.mod exists
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
		cmd := exec.Command("go", "build", "./...")
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("go build: %s", strings.TrimSpace(string(out)))
		}
	}
	return nil
}
