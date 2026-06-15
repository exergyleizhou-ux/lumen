// Package runtime provides runtime information for Lumen agents.
// It exposes Go version, memory statistics, goroutine count, CPU count,
// uptime tracking, and a HealthMonitor with periodic checks.
package runtime

import (
	"context"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// RuntimeInfo — snapshot of the Go runtime.

// RuntimeInfo captures a point-in-time snapshot of the Go runtime.
type RuntimeInfo struct {
	GoVersion    string `json:"go_version"`
	NumCPU       int    `json:"num_cpu"`
	NumGoroutine int    `json:"num_goroutine"`
	NumCgoCall   int64  `json:"num_cgo_call"`
	GoMaxProcs   int    `json:"go_max_procs"`
	GOOS         string `json:"goos"`
	GOARCH       string `json:"goarch"`
	PID          int    `json:"pid"`
	Uptime       string `json:"uptime"`

	MemStats MemStatsSnapshot `json:"mem_stats"`

	// Extensions filled by HealthMonitor
	Checks []CheckResult `json:"checks,omitempty"`
}

// MemStatsSnapshot is a lightweight copy of runtime.MemStats.
type MemStatsSnapshot struct {
	Alloc        uint64 `json:"alloc"`
	TotalAlloc   uint64 `json:"total_alloc"`
	Sys          uint64 `json:"sys"`
	Lookups      uint64 `json:"lookups"`
	Mallocs      uint64 `json:"mallocs"`
	Frees        uint64 `json:"frees"`
	HeapAlloc    uint64 `json:"heap_alloc"`
	HeapSys      uint64 `json:"heap_sys"`
	HeapIdle     uint64 `json:"heap_idle"`
	HeapInuse    uint64 `json:"heap_inuse"`
	HeapReleased uint64 `json:"heap_released"`
	HeapObjects  uint64 `json:"heap_objects"`
	StackInuse   uint64 `json:"stack_inuse"`
	StackSys     uint64 `json:"stack_sys"`
	NumGC        uint32 `json:"num_gc"`
	GCPauseTotal uint64 `json:"gc_pause_total_ns"`
	LastGC       string `json:"last_gc,omitempty"`
}

// ---------------------------------------------------------------------------
// MemStats — wrapper that adds convenience methods.

// MemStats wraps runtime.MemStats with helpers.
type MemStats struct {
	raw runtime.MemStats
	mu  sync.RWMutex
}

// NewMemStats creates a MemStats wrapper.
func NewMemStats() *MemStats { return &MemStats{} }

// Refresh updates the internal snapshot.
func (ms *MemStats) Refresh() {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	runtime.ReadMemStats(&ms.raw)
}

// Snapshot returns a light copy of the current stats.
func (ms *MemStats) Snapshot() MemStatsSnapshot {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	r := ms.raw
	return MemStatsSnapshot{
		Alloc:        r.Alloc,
		TotalAlloc:   r.TotalAlloc,
		Sys:          r.Sys,
		Lookups:      r.Lookups,
		Mallocs:      r.Mallocs,
		Frees:        r.Frees,
		HeapAlloc:    r.HeapAlloc,
		HeapSys:      r.HeapSys,
		HeapIdle:     r.HeapIdle,
		HeapInuse:    r.HeapInuse,
		HeapReleased: r.HeapReleased,
		HeapObjects:  r.HeapObjects,
		StackInuse:   r.StackInuse,
		StackSys:     r.StackSys,
		NumGC:        r.NumGC,
		GCPauseTotal: r.PauseTotalNs,
		LastGC:       fmtDuration(time.Duration(r.LastGC)),
	}
}

// Collect gathers a full RuntimeInfo.
func Collect(uptime time.Duration) RuntimeInfo {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	return RuntimeInfo{
		GoVersion:    runtime.Version(),
		NumCPU:       runtime.NumCPU(),
		NumGoroutine: runtime.NumGoroutine(),
		NumCgoCall:   runtime.NumCgoCall(),
		GoMaxProcs:   runtime.GOMAXPROCS(0),
		GOOS:         runtime.GOOS,
		GOARCH:       runtime.GOARCH,
		Uptime:       uptime.Truncate(time.Second).String(),
		MemStats: MemStatsSnapshot{
			Alloc:        ms.Alloc,
			TotalAlloc:   ms.TotalAlloc,
			Sys:          ms.Sys,
			Lookups:      ms.Lookups,
			Mallocs:      ms.Mallocs,
			Frees:        ms.Frees,
			HeapAlloc:    ms.HeapAlloc,
			HeapSys:      ms.HeapSys,
			HeapIdle:     ms.HeapIdle,
			HeapInuse:    ms.HeapInuse,
			HeapReleased: ms.HeapReleased,
			HeapObjects:  ms.HeapObjects,
			StackInuse:   ms.StackInuse,
			StackSys:     ms.StackSys,
			NumGC:        ms.NumGC,
			GCPauseTotal: ms.PauseTotalNs,
			LastGC:       fmtDuration(time.Duration(ms.LastGC)),
		},
	}
}

// ---------------------------------------------------------------------------
// Uptime tracker.

// Uptime tracks how long the process has been running.
type Uptime struct {
	start time.Time
}

// NewUptime creates an uptime tracker starting now.
func NewUptime() *Uptime { return &Uptime{start: time.Now()} }

// StartAt sets an explicit start time (useful for restoring from a checkpoint).
func (u *Uptime) StartAt(t time.Time) { u.start = t }

// Duration returns elapsed time since start.
func (u *Uptime) Duration() time.Duration { return time.Since(u.start) }

// String formats the uptime as "1h2m3s".
func (u *Uptime) String() string { return u.Duration().Truncate(time.Second).String() }

// Started returns the recorded start time.
func (u *Uptime) Started() time.Time { return u.start }

// ---------------------------------------------------------------------------
// Memory formatting helpers.

// FormatBytesSI returns a human-readable size in SI units (powers of 1000).
func FormatBytesSI(n uint64) string {
	const unit = 1000
	if n < uint64(unit) {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for b := n / unit; b >= unit; b /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "kMGTPE"[exp])
}

// FormatBytesIEC returns a human-readable size in IEC units (powers of 1024).
func FormatBytesIEC(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for b := n / unit; b >= unit; b /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// ---------------------------------------------------------------------------
// Health check interface and monitor.

// Check is a named health probe.
type Check interface {
	Name() string
	Run(ctx context.Context) error
}

// CheckFunc adapts a function to the Check interface.
type CheckFunc struct {
	name string
	fn   func(ctx context.Context) error
}

// NamedCheck creates a Check from a function.
func NamedCheck(name string, fn func(ctx context.Context) error) *CheckFunc {
	return &CheckFunc{name: name, fn: fn}
}

func (c *CheckFunc) Name() string                   { return c.name }
func (c *CheckFunc) Run(ctx context.Context) error   { return c.fn(ctx) }

// CheckResult is the outcome of one health check.
type CheckResult struct {
	Name     string        `json:"name"`
	Passed   bool          `json:"passed"`
	Error    string        `json:"error,omitempty"`
	Duration time.Duration `json:"duration"`
	Time     time.Time     `json:"time"`
}

// ---------------------------------------------------------------------------
// HealthMonitor — runs periodic checks and tracks results.

// HealthMonitor runs registered checks on a periodic ticker and retains the
// most recent result for each.
type HealthMonitor struct {
	checks    []Check
	results   map[string]*CheckResult
	mu        sync.RWMutex
	interval  time.Duration
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	running   bool
	onChange  func(name string, passed bool)
}

// NewHealthMonitor creates a HealthMonitor with a check interval.
func NewHealthMonitor(interval time.Duration) *HealthMonitor {
	return &HealthMonitor{
		interval: interval,
		results:  make(map[string]*CheckResult),
	}
}

// Register adds a health check.
func (hm *HealthMonitor) Register(c Check) {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	hm.checks = append(hm.checks, c)
}

// OnChange registers a callback invoked whenever a check result changes
// (pass→fail or fail→pass).
func (hm *HealthMonitor) OnChange(fn func(name string, passed bool)) {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	hm.onChange = fn
}

// Start begins the periodic check loop.
func (hm *HealthMonitor) Start() {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	if hm.running {
		return
	}
	hm.running = true
	hm.ctx, hm.cancel = context.WithCancel(context.Background())
	hm.wg.Add(1)
	go hm.loop()
}

// Stop shuts down the check loop.
func (hm *HealthMonitor) Stop() {
	hm.mu.Lock()
	if !hm.running {
		hm.mu.Unlock()
		return
	}
	hm.running = false
	hm.cancel()
	hm.mu.Unlock()
	hm.wg.Wait()
}

func (hm *HealthMonitor) loop() {
	defer hm.wg.Done()
	ticker := time.NewTicker(hm.interval)
	defer ticker.Stop()
	// Run immediately on start.
	hm.runChecks()
	for {
		select {
		case <-hm.ctx.Done():
			return
		case <-ticker.C:
			hm.runChecks()
		}
	}
}

func (hm *HealthMonitor) runChecks() {
	hm.mu.RLock()
	checks := make([]Check, len(hm.checks))
	copy(checks, hm.checks)
	onChange := hm.onChange
	hm.mu.RUnlock()

	for _, c := range checks {
		start := time.Now()
		err := c.Run(hm.ctx)
		dur := time.Since(start)
		result := &CheckResult{
			Name:     c.Name(),
			Passed:   err == nil,
			Duration: dur,
			Time:     start,
		}
		if err != nil {
			result.Error = err.Error()
		}

		hm.mu.Lock()
		prev, existed := hm.results[c.Name()]
		hm.results[c.Name()] = result
		hm.mu.Unlock()

		if onChange != nil && (!existed || prev.Passed != result.Passed) {
			onChange(c.Name(), result.Passed)
		}
	}
}

// Results returns a copy of the latest check results.
func (hm *HealthMonitor) Results() []CheckResult {
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	out := make([]CheckResult, 0, len(hm.results))
	for _, r := range hm.results {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// IsHealthy returns true if all registered checks pass.
func (hm *HealthMonitor) IsHealthy() bool {
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	if len(hm.results) == 0 {
		return true // nothing to check → healthy
	}
	for _, r := range hm.results {
		if !r.Passed {
			return false
		}
	}
	return true
}

// Summary returns a compact health status string.
func (hm *HealthMonitor) Summary() string {
	results := hm.Results()
	if len(results) == 0 {
		return "no checks registered"
	}
	pass := 0
	for _, r := range results {
		if r.Passed {
			pass++
		}
	}
	if pass == len(results) {
		return fmt.Sprintf("healthy (%d/%d)", pass, len(results))
	}
	return fmt.Sprintf("unhealthy (%d/%d)", pass, len(results))
}

// ---------------------------------------------------------------------------
// Built-in checks.

// GoroutineCheck reports the goroutine count with a soft threshold.
type GoroutineCheck struct {
	WarnThreshold  int
	CritThreshold  int
}

func (gc *GoroutineCheck) Name() string { return "goroutines" }

func (gc *GoroutineCheck) Run(_ context.Context) error {
	n := runtime.NumGoroutine()
	if gc.CritThreshold > 0 && n > gc.CritThreshold {
		return fmt.Errorf("goroutine count %d exceeds critical threshold %d", n, gc.CritThreshold)
	}
	if gc.WarnThreshold > 0 && n > gc.WarnThreshold {
		return fmt.Errorf("goroutine count %d exceeds warning threshold %d", n, gc.WarnThreshold)
	}
	return nil
}

// MemoryCheck reports heap usage against a threshold.
type MemoryCheck struct {
	MaxHeapMB int
}

func (mc *MemoryCheck) Name() string { return "memory" }

func (mc *MemoryCheck) Run(_ context.Context) error {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	heapMB := ms.HeapAlloc / (1024 * 1024)
	if mc.MaxHeapMB > 0 && int(heapMB) > mc.MaxHeapMB {
		return fmt.Errorf("heap %d MB exceeds limit %d MB", heapMB, mc.MaxHeapMB)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Runtime summary helpers.

// GoroutineProfile returns a breakdown of goroutine states.
func GoroutineProfile() map[string]int {
	// Best-effort: read the profile.
	buf := make([]byte, 64*1024)
	n := runtime.Stack(buf, true)
	lines := strings.Split(string(buf[:n]), "\n")
	counts := map[string]int{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "goroutine ") {
			// goroutine 123 [state]:
			parts := strings.SplitN(line, "[", 2)
			if len(parts) == 2 {
				state := strings.TrimRight(parts[1], "]:")
				state = strings.TrimSpace(state)
				counts[state]++
			}
		}
	}
	return counts
}

// StackTrace collects all goroutine stacks as a string.
func StackTrace() string {
	buf := make([]byte, 256*1024)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			return string(buf[:n])
		}
		buf = make([]byte, 2*len(buf))
	}
}

// ---------------------------------------------------------------------------
// GC helpers.

// ForceGC runs a garbage collection and returns stats before/after.
func ForceGC() (before, after MemStatsSnapshot) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	before = snapshotFrom(&ms)
	runtime.GC()
	runtime.ReadMemStats(&ms)
	after = snapshotFrom(&ms)
	return
}

func snapshotFrom(ms *runtime.MemStats) MemStatsSnapshot {
	return MemStatsSnapshot{
		Alloc:        ms.Alloc,
		TotalAlloc:   ms.TotalAlloc,
		Sys:          ms.Sys,
		Lookups:      ms.Lookups,
		Mallocs:      ms.Mallocs,
		Frees:        ms.Frees,
		HeapAlloc:    ms.HeapAlloc,
		HeapSys:      ms.HeapSys,
		HeapIdle:     ms.HeapIdle,
		HeapInuse:    ms.HeapInuse,
		HeapReleased: ms.HeapReleased,
		HeapObjects:  ms.HeapObjects,
		StackInuse:   ms.StackInuse,
		StackSys:     ms.StackSys,
		NumGC:        ms.NumGC,
		GCPauseTotal: ms.PauseTotalNs,
		LastGC:       fmtDuration(time.Duration(ms.LastGC)),
	}
}

func fmtDuration(d time.Duration) string {
	if d == 0 {
		return ""
	}
	// LastGC is actually a Unix timestamp in nanoseconds since the epoch.
	// We convert it.
	return time.Unix(0, int64(d)).Format(time.RFC3339)
}

// ---------------------------------------------------------------------------
// System info (best-effort).

// SysInfo collects a flat map of system-level metadata.
func SysInfo() map[string]string {
	return map[string]string{
		"go_version":  runtime.Version(),
		"goos":        runtime.GOOS,
		"goarch":      runtime.GOARCH,
		"num_cpu":     fmt.Sprintf("%d", runtime.NumCPU()),
		"go_max_procs": fmt.Sprintf("%d", runtime.GOMAXPROCS(0)),
	}
}

// ---------------------------------------------------------------------------
// CPU profile helpers (simple).

// CPUTimer measures elapsed CPU time for a block of work.
type CPUTimer struct {
	start   time.Time
	elapsed time.Duration
}

// NewCPUTimer starts a CPU timer.
func NewCPUTimer() *CPUTimer { return &CPUTimer{start: time.Now()} }

// Stop returns the elapsed duration.
func (ct *CPUTimer) Stop() time.Duration {
	ct.elapsed = time.Since(ct.start)
	return ct.elapsed
}

// Elapsed returns the current elapsed time without stopping.
func (ct *CPUTimer) Elapsed() time.Duration {
	if ct.elapsed > 0 {
		return ct.elapsed
	}
	return time.Since(ct.start)
}
