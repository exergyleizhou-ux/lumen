// Package harness - extension: test data generators, fuzzy testing support,
// snapshot testing, coverage tracking, performance regression detection.
package harness

import (
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"
)

// ---- Snapshot Testing ----

// SnapshotManager stores and compares test snapshots.
type SnapshotManager struct {
	mu        sync.Mutex
	snapshots map[string]string
}

// NewSnapshotManager creates a snapshot manager.
func NewSnapshotManager() *SnapshotManager {
	return &SnapshotManager{
		snapshots: make(map[string]string),
	}
}

// Save stores a snapshot for a test.
func (sm *SnapshotManager) Save(testName, content string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.snapshots[testName] = content
}

// Compare checks current output against a saved snapshot.
func (sm *SnapshotManager) Compare(testName, current string) (bool, string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	saved, ok := sm.snapshots[testName]
	if !ok {
		return false, "no snapshot found"
	}
	if saved == current {
		return true, ""
	}
	return false, fmt.Sprintf("snapshot mismatch:\n--- saved\n+++ current\n-%s\n+%s", saved, current)
}

// HasSnapshot returns true if a snapshot exists.
func (sm *SnapshotManager) HasSnapshot(testName string) bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	_, ok := sm.snapshots[testName]
	return ok
}

// ---- Test Data Generators ----

// Gen generates random test data.
type Gen struct {
	rng *rand.Rand
}

// NewGen creates a data generator with an optional seed.
func NewGen(seed int64) *Gen {
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	return &Gen{rng: rand.New(rand.NewSource(seed))}
}

// String generates a random string of given length.
func (g *Gen) String(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[g.rng.Intn(len(charset))]
	}
	return string(b)
}

// Int generates a random int in [min, max].
func (g *Gen) Int(min, max int) int {
	if min >= max {
		return min
	}
	return min + g.rng.Intn(max-min+1)
}

// Float generates a random float64 in [min, max).
func (g *Gen) Float(min, max float64) float64 {
	return min + g.rng.Float64()*(max-min)
}

// Bool generates a random bool.
func (g *Gen) Bool() bool {
	return g.rng.Intn(2) == 1
}

// StringSlice generates a slice of random strings.
func (g *Gen) StringSlice(count, minLen, maxLen int) []string {
	result := make([]string, count)
	for i := range result {
		length := g.Int(minLen, maxLen)
		result[i] = g.String(length)
	}
	return result
}

// IntSlice generates a slice of random ints.
func (g *Gen) IntSlice(count, min, max int) []int {
	result := make([]int, count)
	for i := range result {
		result[i] = g.Int(min, max)
	}
	return result
}

// Email generates a random email address.
func (g *Gen) Email() string {
	return g.String(8) + "@" + g.String(5) + ".com"
}

// URL generates a random URL.
func (g *Gen) URL() string {
	return "https://" + g.String(10) + ".com/" + g.String(6)
}

// ---- Performance Regression Detection ----

// PerfResult holds a performance measurement.
type PerfResult struct {
	Name       string
	Duration   time.Duration
	Baseline   time.Duration
	Regression bool
	PctChange  float64
}

// PerfTracker tracks performance across test runs.
type PerfTracker struct {
	mu        sync.Mutex
	baselines map[string]time.Duration
	threshold float64 // pct change to flag as regression
}

// NewPerfTracker creates a performance tracker.
func NewPerfTracker(thresholdPct float64) *PerfTracker {
	if thresholdPct <= 0 {
		thresholdPct = 10.0
	}
	return &PerfTracker{
		baselines: make(map[string]time.Duration),
		threshold: thresholdPct,
	}
}

// SetBaseline records a baseline duration.
func (pt *PerfTracker) SetBaseline(name string, d time.Duration) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.baselines[name] = d
}

// Check compares a new measurement against the baseline.
func (pt *PerfTracker) Check(name string, d time.Duration) *PerfResult {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	baseline, ok := pt.baselines[name]
	result := &PerfResult{
		Name:     name,
		Duration: d,
		Baseline: baseline,
	}

	if !ok {
		return result
	}

	if baseline > 0 {
		result.PctChange = (float64(d) - float64(baseline)) / float64(baseline) * 100
	}
	if result.PctChange > pt.threshold {
		result.Regression = true
	}

	return result
}

// AllBaselines returns all baselines.
func (pt *PerfTracker) AllBaselines() map[string]time.Duration {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	result := make(map[string]time.Duration, len(pt.baselines))
	for k, v := range pt.baselines {
		result[k] = v
	}
	return result
}

// ---- Coverage Tracker ----

// CoverageTracker tracks which branches/paths were exercised.
type CoverageTracker struct {
	mu    sync.Mutex
	hits  map[string]int
	total int
}

// NewCoverageTracker creates a coverage tracker.
func NewCoverageTracker() *CoverageTracker {
	return &CoverageTracker{
		hits: make(map[string]int),
	}
}

// Hit records that a path was exercised.
func (ct *CoverageTracker) Hit(path string) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	ct.hits[path]++
	ct.total++
}

// Coverage returns the number of unique paths hit.
func (ct *CoverageTracker) Coverage() int {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	return len(ct.hits)
}

// TotalHits returns total hit count.
func (ct *CoverageTracker) TotalHits() int {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	return ct.total
}

// PathsHit returns all paths that were hit.
func (ct *CoverageTracker) PathsHit() []string {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	paths := make([]string, 0, len(ct.hits))
	for p := range ct.hits {
		paths = append(paths, p)
	}
	return paths
}

// HitCount returns the hit count for a specific path.
func (ct *CoverageTracker) HitCount(path string) int {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	return ct.hits[path]
}

// ---- TAP (Test Anything Protocol) output ----

// WriteTAP writes TAP-formatted output.
func WriteTAP(result *SuiteResult) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("1..%d\n", result.Total))
	for i, tr := range result.Tests {
		status := "ok"
		if tr.Status != StatusPassed {
			status = "not ok"
		}
		directive := ""
		if tr.Status == StatusSkipped {
			directive = " # SKIP " + tr.Message
		}
		sb.WriteString(fmt.Sprintf("%s %d - %s%s\n", status, i+1, tr.Name, directive))
	}
	return sb.String()
}

// ---- Retry Logic for Flaky Tests ----

// RetryConfig configures test retries.
type RetryConfig struct {
	MaxRetries int
	Backoff    time.Duration
}

// RetryRunner wraps a test with retry logic.
func RetryRunner(t *T, config RetryConfig, fn func(*T)) {
	for attempt := 0; attempt <= config.MaxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(config.Backoff * time.Duration(attempt))
		}

		subT := &T{
			name:      fmt.Sprintf("%s/attempt-%d", t.name, attempt+1),
			suite:     t.suite,
			startTime: time.Now(),
			timeout:   t.timeout,
		}

		success := runTest(subT, fn)
		if success && !subT.Failed() {
			return
		}
	}
	t.Errorf("test failed after %d retries", config.MaxRetries)
}
