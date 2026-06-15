// Package circuitbreaker implements the circuit breaker pattern with half-open/closed/open
// states, failure counting, timeout-based reset, per-endpoint tracking, and metrics.
package circuitbreaker

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// State represents the current state of a circuit breaker.
type State int32

const (
	StateClosed   State = iota // Normal operation; requests flow through.
	StateOpen                   // Failing; requests are rejected immediately.
	StateHalfOpen               // Probing; a limited number of requests are allowed.
)

var stateNames = map[State]string{
	StateClosed:   "closed",
	StateOpen:     "open",
	StateHalfOpen: "half-open",
}

func (s State) String() string {
	if n, ok := stateNames[s]; ok {
		return n
	}
	return fmt.Sprintf("unknown(%d)", s)
}

// Config holds the parameters for a circuit breaker.
type Config struct {
	FailureThreshold  int           // Consecutive failures to trip to Open.
	SuccessThreshold  int           // Consecutive successes in HalfOpen to reset to Closed.
	Timeout           time.Duration // How long to stay Open before transitioning to HalfOpen.
	HalfOpenMaxReqs   int           // Max requests allowed in HalfOpen state.
	WindowDuration    time.Duration // Rolling window for failure-rate calculation.
	FailureRateLimit  float64       // Max failure rate (0-1) before tripping, alternative to threshold.
	RequestVolumeMin  int           // Min requests in window before failure-rate evaluation.
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		FailureThreshold: 5,
		SuccessThreshold: 3,
		Timeout:          30 * time.Second,
		HalfOpenMaxReqs:  3,
		WindowDuration:   60 * time.Second,
		FailureRateLimit: 0.5,
		RequestVolumeMin: 10,
	}
}

// Metrics tracks circuit breaker statistics.
type Metrics struct {
	TotalRequests   int64 // Total requests seen.
	TotalSuccesses  int64 // Total successful requests.
	TotalFailures   int64 // Total failed requests.
	TotalRejects    int64 // Requests rejected due to open circuit.
	StateTransitions int64 // Number of state changes.
	ShortCircuits   int64 // Requests short-circuited.
	LastFailureTime int64 // Unix nano of last failure.
	LastSuccessTime int64 // Unix nano of last success.
}

// Snapshot returns a copy of the current metrics.
func (m *Metrics) Snapshot() Metrics {
	return Metrics{
		TotalRequests:    atomic.LoadInt64(&m.TotalRequests),
		TotalSuccesses:   atomic.LoadInt64(&m.TotalSuccesses),
		TotalFailures:    atomic.LoadInt64(&m.TotalFailures),
		TotalRejects:     atomic.LoadInt64(&m.TotalRejects),
		StateTransitions: atomic.LoadInt64(&m.StateTransitions),
		ShortCircuits:    atomic.LoadInt64(&m.ShortCircuits),
		LastFailureTime:  atomic.LoadInt64(&m.LastFailureTime),
		LastSuccessTime:  atomic.LoadInt64(&m.LastSuccessTime),
	}
}

// bucket holds counts for a time window bucket.
type bucket struct {
	successes int64
	failures  int64
}

// CircuitBreaker implements the circuit breaker pattern for a single endpoint.
type CircuitBreaker struct {
	mu               sync.RWMutex
	config           Config
	state            State
	failCount        int
	successCount     int
	lastFailureTime  time.Time
	lastSuccessTime  time.Time
	openedAt         time.Time
	metrics          Metrics
	buckets          []bucket
	bucketIdx        int
	bucketSize       time.Duration
	lastBucketRotate time.Time
	name             string
}

// New creates a new CircuitBreaker with the given name and config.
func New(name string, cfg Config) *CircuitBreaker {
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = 5
	}
	if cfg.SuccessThreshold <= 0 {
		cfg.SuccessThreshold = 3
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.HalfOpenMaxReqs <= 0 {
		cfg.HalfOpenMaxReqs = 3
	}
	if cfg.WindowDuration <= 0 {
		cfg.WindowDuration = 60 * time.Second
	}
	numBuckets := 10
	bs := cfg.WindowDuration / time.Duration(numBuckets)
	if bs < time.Second {
		bs = time.Second
		numBuckets = int(cfg.WindowDuration / bs)
		if numBuckets < 1 {
			numBuckets = 1
			bs = cfg.WindowDuration
		}
	}
	return &CircuitBreaker{
		config:           cfg,
		state:            StateClosed,
		buckets:          make([]bucket, numBuckets),
		bucketSize:       bs,
		lastBucketRotate: time.Now(),
		name:             name,
	}
}

// Name returns the circuit breaker's name.
func (cb *CircuitBreaker) Name() string { return cb.name }

// State returns the current state.
func (cb *CircuitBreaker) State() State {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// setState transitions to a new state, recording metrics. Caller must hold cb.mu.
func (cb *CircuitBreaker) setState(s State) {
	if cb.state == s {
		return
	}
	cb.state = s
	atomic.AddInt64(&cb.metrics.StateTransitions, 1)
	switch s {
	case StateOpen:
		cb.openedAt = time.Now()
	case StateHalfOpen:
		cb.failCount = 0
		cb.successCount = 0
	case StateClosed:
		cb.failCount = 0
		cb.successCount = 0
	}
}

// rotateBuckets advances the time-window bucket.
func (cb *CircuitBreaker) rotateBuckets() {
	now := time.Now()
	elapsed := now.Sub(cb.lastBucketRotate)
	steps := int(elapsed / cb.bucketSize)
	if steps <= 0 {
		return
	}
	for i := 0; i < steps && i < len(cb.buckets); i++ {
		cb.bucketIdx = (cb.bucketIdx + 1) % len(cb.buckets)
		cb.buckets[cb.bucketIdx] = bucket{}
	}
	cb.lastBucketRotate = now
}

// windowStats returns total successes and failures in the current window.
func (cb *CircuitBreaker) windowStats() (successes, failures int64) {
	for _, b := range cb.buckets {
		successes += atomic.LoadInt64(&b.successes)
		failures += atomic.LoadInt64(&b.failures)
	}
	return
}

// Allow checks whether a request should be permitted. Returns nil if allowed,
// or an error explaining why it was rejected.
func (cb *CircuitBreaker) Allow() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	atomic.AddInt64(&cb.metrics.TotalRequests, 1)

	switch cb.state {
	case StateClosed:
		// Evaluate failure rate before allowing.
		if cb.config.RequestVolumeMin > 0 {
			cb.rotateBuckets()
			s, f := cb.windowStats()
			total := s + f
			if total >= int64(cb.config.RequestVolumeMin) {
				rate := float64(f) / float64(total)
				if rate >= cb.config.FailureRateLimit {
					cb.setState(StateOpen)
					atomic.AddInt64(&cb.metrics.ShortCircuits, 1)
					return fmt.Errorf("circuit breaker %q is open (failure rate %.2f >= %.2f)", cb.name, rate, cb.config.FailureRateLimit)
				}
			}
		}
		return nil

	case StateOpen:
		if time.Since(cb.openedAt) >= cb.config.Timeout {
			cb.setState(StateHalfOpen)
			return nil // Allow as probe.
		}
		atomic.AddInt64(&cb.metrics.TotalRejects, 1)
		atomic.AddInt64(&cb.metrics.ShortCircuits, 1)
		return fmt.Errorf("circuit breaker %q is open", cb.name)

	case StateHalfOpen:
		// In half-open, allow up to HalfOpenMaxReqs concurrent probes.
		// We track via success/fail counts as a simple proxy.
		if cb.failCount+cb.successCount < cb.config.HalfOpenMaxReqs {
			return nil
		}
		atomic.AddInt64(&cb.metrics.TotalRejects, 1)
		return fmt.Errorf("circuit breaker %q is half-open, probing limit reached", cb.name)

	default:
		return fmt.Errorf("circuit breaker %q in unknown state", cb.name)
	}
}

// Success records a successful request.
func (cb *CircuitBreaker) Success() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	atomic.AddInt64(&cb.metrics.TotalSuccesses, 1)
	now := time.Now()
	atomic.StoreInt64(&cb.metrics.LastSuccessTime, now.UnixNano())
	cb.lastSuccessTime = now

	// Record in bucket.
	cb.rotateBuckets()
	atomic.AddInt64(&cb.buckets[cb.bucketIdx].successes, 1)

	switch cb.state {
	case StateHalfOpen:
		cb.successCount++
		if cb.successCount >= cb.config.SuccessThreshold {
			cb.setState(StateClosed)
		}
	case StateClosed:
		cb.failCount = 0 // Reset consecutive fail count on success.
	}
}

// Failure records a failed request.
func (cb *CircuitBreaker) Failure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	atomic.AddInt64(&cb.metrics.TotalFailures, 1)
	now := time.Now()
	atomic.StoreInt64(&cb.metrics.LastFailureTime, now.UnixNano())
	cb.lastFailureTime = now

	// Record in bucket.
	cb.rotateBuckets()
	atomic.AddInt64(&cb.buckets[cb.bucketIdx].failures, 1)

	switch cb.state {
	case StateHalfOpen:
		cb.failCount++
		cb.setState(StateOpen) // Any failure in half-open re-tripps.
	case StateClosed:
		cb.failCount++
		if cb.failCount >= cb.config.FailureThreshold {
			cb.setState(StateOpen)
		}
	}
}

// Metrics returns a snapshot of the current metrics.
func (cb *CircuitBreaker) Metrics() Metrics {
	return cb.metrics.Snapshot()
}

// Reset returns the circuit breaker to its initial closed state.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.state = StateClosed
	cb.failCount = 0
	cb.successCount = 0
	cb.buckets = make([]bucket, len(cb.buckets))
	cb.metrics = Metrics{}
}

// FormatStatus returns a human-readable status string.
func (cb *CircuitBreaker) FormatStatus() string {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	m := cb.metrics.Snapshot()
	return fmt.Sprintf(
		"[%s] state=%s failures=%d/%d successes=%d rejects=%d transitions=%d",
		cb.name,
		cb.state,
		cb.failCount,
		cb.config.FailureThreshold,
		cb.successCount,
		m.TotalRejects,
		m.StateTransitions,
	)
}

// --- Per-endpoint tracking ---

// EndpointKey identifies a specific endpoint.
type EndpointKey struct {
	Service string
	Method  string
	Path    string
}

// String returns a compact representation.
func (k EndpointKey) String() string {
	return fmt.Sprintf("%s:%s:%s", k.Service, k.Method, k.Path)
}

// EndpointBreaker wraps a CircuitBreaker with endpoint identification.
type EndpointBreaker struct {
	Key     EndpointKey
	Breaker *CircuitBreaker
}

// BreakerRegistry manages circuit breakers per endpoint.
type BreakerRegistry struct {
	mu       sync.RWMutex
	breakers map[string]*EndpointBreaker
	config   Config
}

// NewRegistry creates a new BreakerRegistry with the default config.
func NewRegistry(cfg Config) *BreakerRegistry {
	if cfg.FailureThreshold <= 0 {
		cfg = DefaultConfig()
	}
	return &BreakerRegistry{
		breakers: make(map[string]*EndpointBreaker),
		config:   cfg,
	}
}

// GetOrCreate returns the breaker for the given key, creating one if needed.
func (r *BreakerRegistry) GetOrCreate(key EndpointKey) *EndpointBreaker {
	r.mu.Lock()
	defer r.mu.Unlock()

	k := key.String()
	if eb, ok := r.breakers[k]; ok {
		return eb
	}
	eb := &EndpointBreaker{
		Key:     key,
		Breaker: New(k, r.config),
	}
	r.breakers[k] = eb
	return eb
}

// Get returns the breaker for the key, or nil.
func (r *BreakerRegistry) Get(key EndpointKey) *EndpointBreaker {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.breakers[key.String()]
}

// Remove deletes the breaker for the given key.
func (r *BreakerRegistry) Remove(key EndpointKey) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.breakers, key.String())
}

// List returns all registered breakers.
func (r *BreakerRegistry) List() []*EndpointBreaker {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*EndpointBreaker, 0, len(r.breakers))
	for _, eb := range r.breakers {
		out = append(out, eb)
	}
	return out
}

// FormatAll returns a status string for every registered breaker.
func (r *BreakerRegistry) FormatAll() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	res := ""
	for _, eb := range r.breakers {
		res += eb.Breaker.FormatStatus() + "\n"
	}
	return res
}

// Ensure interface compliance.
var _ fmt.Stringer = State(0)

// --- Event Callbacks ---

// EventType describes a circuit breaker event.
type EventType int

const (
	EventStateChange EventType = iota
	EventTrip
	EventReset
	EventProbeSuccess
	EventProbeFailure
)

var eventTypeNames = map[EventType]string{
	EventStateChange:   "state-change",
	EventTrip:          "trip",
	EventReset:         "reset",
	EventProbeSuccess:  "probe-success",
	EventProbeFailure:  "probe-failure",
}

func (et EventType) String() string {
	if n, ok := eventTypeNames[et]; ok { return n }
	return "unknown"
}

// EventListener is called on circuit breaker events.
type EventListener func(event EventType, breaker *CircuitBreaker, meta map[string]interface{})

// ObservableBreaker wraps CircuitBreaker with event listeners.
type ObservableBreaker struct {
	*CircuitBreaker
	listeners []EventListener
	muListen  sync.RWMutex
}

// NewObservable creates an observable circuit breaker.
func NewObservable(name string, cfg Config) *ObservableBreaker {
	return &ObservableBreaker{CircuitBreaker: New(name, cfg)}
}

// On registers an event listener.
func (ob *ObservableBreaker) On(listener EventListener) {
	ob.muListen.Lock()
	defer ob.muListen.Unlock()
	ob.listeners = append(ob.listeners, listener)
}

func (ob *ObservableBreaker) emit(event EventType, meta map[string]interface{}) {
	ob.muListen.RLock()
	defer ob.muListen.RUnlock()
	for _, l := range ob.listeners {
		l(event, ob.CircuitBreaker, meta)
	}
}

// Allow checks and emits events.
func (ob *ObservableBreaker) Allow() error {
	err := ob.CircuitBreaker.Allow()
	if err != nil {
		ob.emit(EventTrip, map[string]interface{}{"error": err.Error()})
	}
	return err
}

// Success records success and emits.
func (ob *ObservableBreaker) Success() {
	oldState := ob.CircuitBreaker.State()
	ob.CircuitBreaker.Success()
	newState := ob.CircuitBreaker.State()
	if oldState != newState && newState == StateClosed {
		ob.emit(EventReset, map[string]interface{}{"from": oldState.String(), "to": newState.String()})
	}
	if oldState == StateHalfOpen {
		ob.emit(EventProbeSuccess, nil)
	}
}

// Failure records failure and emits.
func (ob *ObservableBreaker) Failure() {
	oldState := ob.CircuitBreaker.State()
	ob.CircuitBreaker.Failure()
	newState := ob.CircuitBreaker.State()
	if oldState != newState {
		ob.emit(EventStateChange, map[string]interface{}{"from": oldState.String(), "to": newState.String()})
	}
	if oldState == StateHalfOpen {
		ob.emit(EventProbeFailure, nil)
	}
}

// --- Dynamic Configuration ---

// DynamicConfig allows runtime reconfiguration.
type DynamicConfig struct {
	cb *CircuitBreaker
}

// NewDynamicConfig wraps a breaker for dynamic reconfiguration.
func NewDynamicConfig(cb *CircuitBreaker) *DynamicConfig { return &DynamicConfig{cb: cb} }

// SetFailureThreshold updates the failure threshold at runtime.
func (dc *DynamicConfig) SetFailureThreshold(n int) {
	dc.cb.mu.Lock()
	defer dc.cb.mu.Unlock()
	if n > 0 { dc.cb.config.FailureThreshold = n }
}

// SetSuccessThreshold updates the success threshold at runtime.
func (dc *DynamicConfig) SetSuccessThreshold(n int) {
	dc.cb.mu.Lock()
	defer dc.cb.mu.Unlock()
	if n > 0 { dc.cb.config.SuccessThreshold = n }
}

// SetTimeout updates the open-state timeout.
func (dc *DynamicConfig) SetTimeout(d time.Duration) {
	dc.cb.mu.Lock()
	defer dc.cb.mu.Unlock()
	if d > 0 { dc.cb.config.Timeout = d }
}

// ForceState manually sets the breaker state (for testing/admin).
func (dc *DynamicConfig) ForceState(s State) {
	dc.cb.mu.Lock()
	defer dc.cb.mu.Unlock()
	dc.cb.state = s
	dc.cb.failCount = 0
	dc.cb.successCount = 0
	atomic.AddInt64(&dc.cb.metrics.StateTransitions, 1)
}

// --- Multi-Region Support ---

// RegionBreaker tracks a breaker per region for failover scenarios.
type RegionBreaker struct {
	mu       sync.RWMutex
	breakers map[string]*CircuitBreaker // region -> breaker
	config   Config
}

// NewRegionBreaker creates a multi-region breaker manager.
func NewRegionBreaker(cfg Config) *RegionBreaker {
	return &RegionBreaker{breakers: make(map[string]*CircuitBreaker), config: cfg}
}

// Get returns the breaker for a region, creating if needed.
func (rb *RegionBreaker) Get(region string) *CircuitBreaker {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	if cb, ok := rb.breakers[region]; ok { return cb }
	cb := New("region-"+region, rb.config)
	rb.breakers[region] = cb
	return cb
}

// HealthyRegions returns regions whose breakers are not open.
func (rb *RegionBreaker) HealthyRegions() []string {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	var out []string
	for region, cb := range rb.breakers {
		if cb.State() != StateOpen { out = append(out, region) }
	}
	return out
}

// AllOpen returns true if all regional breakers are open.
func (rb *RegionBreaker) AllOpen() bool {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	if len(rb.breakers) == 0 { return false }
	for _, cb := range rb.breakers {
		if cb.State() != StateOpen { return false }
	}
	return true
}

// --- Advisory Policy ---

// Advisory tracks per-breaker health advice.
type Advisory struct {
	mu    sync.RWMutex
	notes map[string][]string // breaker name -> advisories
}

// NewAdvisory creates an advisory tracker.
func NewAdvisory() *Advisory { return &Advisory{notes: make(map[string][]string)} }

// AddNote adds an advisory note for a breaker.
func (a *Advisory) AddNote(breakerName, note string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.notes[breakerName] = append(a.notes[breakerName], note)
}

// GetNotes returns all notes for a breaker.
func (a *Advisory) GetNotes(breakerName string) []string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]string, len(a.notes[breakerName]))
	copy(out, a.notes[breakerName])
	return out
}

// FormatAdvisory returns a summary of all advisories.
func (a *Advisory) FormatAdvisory() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	s := "Advisories:\n"
	for name, notes := range a.notes {
		s += fmt.Sprintf("  %s: %d notes\n", name, len(notes))
		for _, n := range notes {
			s += fmt.Sprintf("    - %s\n", n)
		}
	}
	return s
}

// --- Batch Operations ---

// BatchChecker allows checking multiple breakers atomically.
type BatchChecker struct {
	breakers []*CircuitBreaker
}

// NewBatchChecker creates a batch checker.
func NewBatchChecker(breakers ...*CircuitBreaker) *BatchChecker {
	return &BatchChecker{breakers: breakers}
}

// AllowAll returns nil only if all breakers allow. Returns first error.
func (bc *BatchChecker) AllowAll() error {
	for _, cb := range bc.breakers {
		if err := cb.Allow(); err != nil { return err }
	}
	return nil
}

// SuccessAll records success on all breakers.
func (bc *BatchChecker) SuccessAll() {
	for _, cb := range bc.breakers { cb.Success() }
}

// FailureAll records failure on all breakers.
func (bc *BatchChecker) FailureAll() {
	for _, cb := range bc.breakers { cb.Failure() }
}
