// Package poller implements a resource poller with periodic polling, exponential
// back-off, change detection, diff-based notification, and polling history.
package poller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"
)

// --- Change and Diff ---

// ChangeType indicates the nature of a detected change.
type ChangeType int

const (
	ChangeNone ChangeType = iota
	ChangeCreated
	ChangeUpdated
	ChangeDeleted
)

var changeTypeNames = map[ChangeType]string{
	ChangeNone:    "none",
	ChangeCreated: "created",
	ChangeUpdated: "updated",
	ChangeDeleted: "deleted",
}

func (ct ChangeType) String() string {
	if n, ok := changeTypeNames[ct]; ok {
		return n
	}
	return "unknown"
}

// DiffEntry describes one changed field or section.
type DiffEntry struct {
	Path     string      `json:"path"` // JSON path or field name.
	OldValue interface{} `json:"old_value,omitempty"`
	NewValue interface{} `json:"new_value,omitempty"`
	Type     ChangeType  `json:"type"`
}

// Change represents a detected change between two poll snapshots.
type Change struct {
	ResourceID string      `json:"resource_id"`
	Timestamp  time.Time   `json:"timestamp"`
	Type       ChangeType  `json:"type"`
	Diff       []DiffEntry `json:"diff"`
	OldDigest  string      `json:"old_digest"`
	NewDigest  string      `json:"new_digest"`
}

// --- Resource ---

// Resource represents a polled resource with its current state.
type Resource struct {
	ID        string          `json:"id"`
	URL       string          `json:"url"`
	Data      json.RawMessage `json:"data"`
	Digest    string          `json:"digest"`
	FetchedAt time.Time       `json:"fetched_at"`
	Attempts  int             `json:"attempts"`
	Errors    int             `json:"errors"`
}

// ComputeDigest updates the digest from the current data.
func (r *Resource) ComputeDigest() {
	h := sha256.Sum256(r.Data)
	r.Digest = hex.EncodeToString(h[:])
}

// --- Poll Config ---

// Config configures a poller.
type Config struct {
	Interval          time.Duration // Base poll interval.
	MaxInterval       time.Duration // Maximum back-off interval.
	BackoffMultiplier float64       // Exponential back-off multiplier (>1).
	JitterFactor      float64       // Random jitter factor (0-1).
	MaxRetries        int           // Max consecutive errors before giving up.
	Timeout           time.Duration // Per-poll timeout.
	HistorySize       int           // Max change history entries.
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		Interval:          30 * time.Second,
		MaxInterval:       5 * time.Minute,
		BackoffMultiplier: 1.5,
		JitterFactor:      0.1,
		MaxRetries:        10,
		Timeout:           15 * time.Second,
		HistorySize:       1000,
	}
}

// --- Poller ---

// FetchFunc is the user-provided function to fetch a resource.
type FetchFunc func(ctx context.Context, resourceID, url string) (json.RawMessage, error)

// NotifyFunc is called when a change is detected.
type NotifyFunc func(change Change)

// Poller periodically polls resources and detects changes.
type Poller struct {
	mu          sync.RWMutex
	config      Config
	resources   map[string]*Resource
	history     []Change
	fetchFn     FetchFunc
	notifyFn    NotifyFunc
	stopCh      chan struct{}
	running     bool
	pollCount   int64
	changeCount int64
	errorCount  int64
}

// New creates a new Poller.
func New(cfg Config, fetch FetchFunc, notify NotifyFunc) *Poller {
	if cfg.Interval <= 0 {
		cfg.Interval = 30 * time.Second
	}
	if cfg.MaxInterval <= 0 {
		cfg.MaxInterval = 5 * time.Minute
	}
	if cfg.BackoffMultiplier <= 1 {
		cfg.BackoffMultiplier = 1.5
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 10
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 15 * time.Second
	}
	if cfg.HistorySize <= 0 {
		cfg.HistorySize = 1000
	}
	return &Poller{
		config:    cfg,
		resources: make(map[string]*Resource),
		fetchFn:   fetch,
		notifyFn:  notify,
	}
}

// AddResource registers a resource for polling.
func (p *Poller) AddResource(id, url string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, exists := p.resources[id]; exists {
		return
	}
	p.resources[id] = &Resource{
		ID:  id,
		URL: url,
	}
}

// RemoveResource unregisters a resource.
func (p *Poller) RemoveResource(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.resources, id)
}

// GetResource returns a resource by ID.
func (p *Poller) GetResource(id string) *Resource {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.resources[id]
}

// ListResources returns all registered resources.
func (p *Poller) ListResources() []*Resource {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]*Resource, 0, len(p.resources))
	for _, r := range p.resources {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Start begins the poll loop.
func (p *Poller) Start() {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return
	}
	p.running = true
	p.stopCh = make(chan struct{})
	p.mu.Unlock()

	go p.loop()
}

// Stop halts the poll loop.
func (p *Poller) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.running {
		return
	}
	p.running = false
	close(p.stopCh)
}

// Running returns whether the poller is active.
func (p *Poller) Running() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.running
}

func (p *Poller) loop() {
	ticker := time.NewTicker(p.config.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.pollAll()
		}
	}
}

func (p *Poller) pollAll() {
	p.mu.RLock()
	ids := make([]string, 0, len(p.resources))
	for id := range p.resources {
		ids = append(ids, id)
	}
	p.mu.RUnlock()

	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(resID string) {
			defer wg.Done()
			p.pollOne(resID)
		}(id)
	}
	wg.Wait()
}

func (p *Poller) pollOne(resourceID string) {
	p.mu.RLock()
	r, ok := p.resources[resourceID]
	p.mu.RUnlock()
	if !ok {
		return
	}

	p.pollCount++

	ctx, cancel := context.WithTimeout(context.Background(), p.config.Timeout)
	defer cancel()

	newData, err := p.fetchFn(ctx, resourceID, r.URL)
	if err != nil {
		p.mu.Lock()
		if res, ok2 := p.resources[resourceID]; ok2 {
			res.Errors++
			res.Attempts++
		}
		p.errorCount++
		p.mu.Unlock()
		return
	}

	// Compute new digest.
	newDigest := computeDigest(newData)

	p.mu.Lock()
	defer p.mu.Unlock()

	r, ok = p.resources[resourceID]
	if !ok {
		return
	}

	oldDigest := r.Digest
	oldData := r.Data

	if oldDigest == "" {
		// First fetch.
		r.Data = newData
		r.Digest = newDigest
		r.FetchedAt = time.Now()
		r.Attempts++
		r.Errors = 0
		// Don't report as a change.
		return
	}

	if oldDigest == newDigest {
		// No change; reset back-off.
		r.Attempts = 0
		r.Errors = 0
		return
	}

	// Change detected.
	r.Data = newData
	r.Digest = newDigest
	r.FetchedAt = time.Now()
	r.Attempts = 0
	r.Errors = 0

	changeType := ChangeUpdated
	if oldData == nil {
		changeType = ChangeCreated
	}

	diff := computeDiff(oldData, newData)
	change := Change{
		ResourceID: resourceID,
		Timestamp:  time.Now(),
		Type:       changeType,
		Diff:       diff,
		OldDigest:  oldDigest,
		NewDigest:  newDigest,
	}

	p.addHistory(change)
	p.changeCount++

	if p.notifyFn != nil {
		p.notifyFn(change)
	}
}

func (p *Poller) addHistory(c Change) {
	p.history = append(p.history, c)
	if len(p.history) > p.config.HistorySize {
		p.history = p.history[len(p.history)-p.config.HistorySize:]
	}
}

// PollNow forces an immediate poll of all resources.
func (p *Poller) PollNow() {
	p.pollAll()
}

// PollOne forces an immediate poll of a single resource.
func (p *Poller) PollOne(resourceID string) error {
	p.mu.RLock()
	_, ok := p.resources[resourceID]
	p.mu.RUnlock()
	if !ok {
		return fmt.Errorf("resource %q not found", resourceID)
	}
	p.pollOne(resourceID)
	return nil
}

// History returns recent changes, most recent first.
func (p *Poller) History() []Change {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]Change, len(p.history))
	copy(out, p.history)
	// Reverse: most recent first.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// HistoryFor returns changes for a specific resource.
func (p *Poller) HistoryFor(resourceID string) []Change {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]Change, 0)
	for _, c := range p.history {
		if c.ResourceID == resourceID {
			out = append(out, c)
		}
	}
	return out
}

// Stats returns poller statistics.
func (p *Poller) Stats() map[string]int64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return map[string]int64{
		"resources":    int64(len(p.resources)),
		"poll_count":   p.pollCount,
		"change_count": p.changeCount,
		"error_count":  p.errorCount,
		"history_size": int64(len(p.history)),
	}
}

// --- Back-off calculation ---

// Backoff computes the next poll interval using exponential back-off with jitter.
func Backoff(base time.Duration, attempt int, maxDuration time.Duration, multiplier float64, jitter float64) time.Duration {
	if attempt <= 0 {
		return base
	}
	f := float64(base) * math.Pow(multiplier, float64(attempt))
	if f > float64(maxDuration) {
		f = float64(maxDuration)
	}
	// Add jitter.
	if jitter > 0 {
		f = f * (1 + jitter*(2*float64(time.Now().UnixNano()%1000)/1000-1))
	}
	d := time.Duration(f)
	if d > maxDuration {
		d = maxDuration
	}
	return d
}

// --- Diff computation ---

func computeDigest(data json.RawMessage) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func computeDiff(oldData, newData json.RawMessage) []DiffEntry {
	if oldData == nil {
		return []DiffEntry{{Path: "/", NewValue: string(newData), Type: ChangeCreated}}
	}
	if string(oldData) == string(newData) {
		return nil
	}

	// Try to diff as JSON objects.
	var oldObj, newObj map[string]json.RawMessage
	if json.Unmarshal(oldData, &oldObj) == nil && json.Unmarshal(newData, &newObj) == nil {
		return diffObjects(oldObj, newObj, "")
	}

	// Try to diff as JSON arrays.
	var oldArr, newArr []json.RawMessage
	if json.Unmarshal(oldData, &oldArr) == nil && json.Unmarshal(newData, &newArr) == nil {
		return diffArrays(oldArr, newArr, "")
	}

	// Simple string diff.
	return []DiffEntry{{Path: "/", OldValue: string(oldData), NewValue: string(newData), Type: ChangeUpdated}}
}

func diffObjects(oldObj, newObj map[string]json.RawMessage, prefix string) []DiffEntry {
	var diffs []DiffEntry
	allKeys := make(map[string]bool)
	for k := range oldObj {
		allKeys[k] = true
	}
	for k := range newObj {
		allKeys[k] = true
	}
	for k := range allKeys {
		path := prefix + "/" + k
		oldV, oldOk := oldObj[k]
		newV, newOk := newObj[k]
		if !oldOk {
			diffs = append(diffs, DiffEntry{Path: path, NewValue: rawToInterface(newV), Type: ChangeCreated})
		} else if !newOk {
			diffs = append(diffs, DiffEntry{Path: path, OldValue: rawToInterface(oldV), Type: ChangeDeleted})
		} else if string(oldV) != string(newV) {
			// Recurse.
			var subOld, subNew map[string]json.RawMessage
			if json.Unmarshal(oldV, &subOld) == nil && json.Unmarshal(newV, &subNew) == nil {
				diffs = append(diffs, diffObjects(subOld, subNew, path)...)
			} else {
				diffs = append(diffs, DiffEntry{Path: path, OldValue: rawToInterface(oldV), NewValue: rawToInterface(newV), Type: ChangeUpdated})
			}
		}
	}
	return diffs
}

func diffArrays(oldArr, newArr []json.RawMessage, prefix string) []DiffEntry {
	var diffs []DiffEntry
	maxLen := len(oldArr)
	if len(newArr) > maxLen {
		maxLen = len(newArr)
	}
	for i := 0; i < maxLen; i++ {
		path := fmt.Sprintf("%s[%d]", prefix, i)
		if i >= len(oldArr) {
			diffs = append(diffs, DiffEntry{Path: path, NewValue: rawToInterface(newArr[i]), Type: ChangeCreated})
		} else if i >= len(newArr) {
			diffs = append(diffs, DiffEntry{Path: path, OldValue: rawToInterface(oldArr[i]), Type: ChangeDeleted})
		} else if string(oldArr[i]) != string(newArr[i]) {
			diffs = append(diffs, DiffEntry{Path: path, OldValue: rawToInterface(oldArr[i]), NewValue: rawToInterface(newArr[i]), Type: ChangeUpdated})
		}
	}
	return diffs
}

func rawToInterface(raw json.RawMessage) interface{} {
	if raw == nil {
		return nil
	}
	var v interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	return v
}

// FormatHistory returns a human-readable change history.
func (p *Poller) FormatHistory() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	s := fmt.Sprintf("Poll History: %d changes\n", len(p.history))
	for i := len(p.history) - 1; i >= 0 && i >= len(p.history)-20; i-- {
		c := p.history[i]
		s += fmt.Sprintf("  %s %s %s (diffs=%d)\n",
			c.Timestamp.Format(time.RFC3339), c.ResourceID, c.Type, len(c.Diff))
	}
	return s
}

// --- Poll Group ---

// PollGroup polls multiple resources as a group and aggregates results.
type PollGroup struct {
	ID        string
	resources []string
	poller    *Poller
}

// NewPollGroup creates a poll group.
func NewPollGroup(id string, poller *Poller, resourceIDs []string) *PollGroup {
	return &PollGroup{ID: id, poller: poller, resources: resourceIDs}
}

// PollNow polls all resources in the group.
func (pg *PollGroup) PollNow() {
	for _, id := range pg.resources {
		pg.poller.PollOne(id)
	}
}

// HistoryForGroup returns combined history for all resources in the group.
func (pg *PollGroup) HistoryForGroup() []Change {
	seen := make(map[string]bool)
	var combined []Change
	for _, id := range pg.resources {
		for _, c := range pg.poller.HistoryFor(id) {
			key := c.ResourceID + c.Timestamp.String()
			if !seen[key] {
				seen[key] = true
				combined = append(combined, c)
			}
		}
	}
	sort.Slice(combined, func(i, j int) bool { return combined[i].Timestamp.Before(combined[j].Timestamp) })
	return combined
}

// --- Alert / Threshold ---

// Threshold defines a condition that triggers an alert.
type Threshold struct {
	ResourceID string      `json:"resource_id"`
	Field      string      `json:"field"`
	Op         string      `json:"op"` // "gt", "lt", "eq", "changed".
	Value      interface{} `json:"value"`
}

// Alert is raised when a threshold is breached.
type Alert struct {
	ID        string    `json:"id"`
	Threshold Threshold `json:"threshold"`
	Message   string    `json:"message"`
	FiredAt   time.Time `json:"fired_at"`
	Acked     bool      `json:"acked"`
}

// AlertManager evaluates thresholds and tracks alerts.
type AlertManager struct {
	mu         sync.Mutex
	thresholds []Threshold
	alerts     []Alert
	history    map[string]json.RawMessage // resourceID -> last known value.
	poller     *Poller
}

// NewAlertManager creates an alert manager.
func NewAlertManager(poller *Poller) *AlertManager {
	return &AlertManager{poller: poller, history: make(map[string]json.RawMessage)}
}

// AddThreshold adds a threshold rule.
func (am *AlertManager) AddThreshold(t Threshold) {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.thresholds = append(am.thresholds, t)
}

// Evaluate checks all thresholds against the latest poll data.
func (am *AlertManager) Evaluate() []Alert {
	am.mu.Lock()
	defer am.mu.Unlock()
	var fired []Alert
	for _, t := range am.thresholds {
		res := am.poller.GetResource(t.ResourceID)
		if res == nil {
			continue
		}
		prev, hasPrev := am.history[t.ResourceID]
		am.history[t.ResourceID] = res.Data
		if t.Op == "changed" && hasPrev && string(prev) != string(res.Data) {
			a := Alert{ID: generateAlertID(), Threshold: t, Message: fmt.Sprintf("%s changed", t.ResourceID), FiredAt: time.Now()}
			am.alerts = append(am.alerts, a)
			fired = append(fired, a)
		}
	}
	return fired
}

// Alerts returns all alerts.
func (am *AlertManager) Alerts() []Alert {
	am.mu.Lock()
	defer am.mu.Unlock()
	out := make([]Alert, len(am.alerts))
	copy(out, am.alerts)
	return out
}

// AckAlert acknowledges an alert.
func (am *AlertManager) AckAlert(id string) bool {
	am.mu.Lock()
	defer am.mu.Unlock()
	for i, a := range am.alerts {
		if a.ID == id {
			am.alerts[i].Acked = true
			return true
		}
	}
	return false
}

var alertIDCounter int64

func generateAlertID() string { alertIDCounter++; return fmt.Sprintf("alert-%d", alertIDCounter) }

// --- Webhook Notifier ---

// WebhookNotifier sends change notifications to a webhook URL.
type WebhookNotifier struct {
	URL     string
	Headers map[string]string
	client  interface{} // In production, *http.Client.
}

// NewWebhookNotifier creates a webhook notifier.
func NewWebhookNotifier(url string, headers map[string]string) *WebhookNotifier {
	return &WebhookNotifier{URL: url, Headers: headers}
}

// Notify sends a change to the webhook (simplified stub).
func (wn *WebhookNotifier) Notify(change Change) error {
	// In production: POST change JSON to wn.URL with wn.Headers.
	_ = change
	return nil
}

// --- Poll Scheduler ---

// PollScheduler manages polling at specific times (cron-like).
type PollSchedule struct {
	CronExpr    string   `json:"cron_expr"`
	ResourceIDs []string `json:"resource_ids"`
}

// ScheduledPoller combines a Poller with cron-based scheduling.
type ScheduledPoller struct {
	*Poller
	schedules []PollSchedule
	loc       *time.Location
}

// NewScheduledPoller creates a scheduled poller.
func NewScheduledPoller(cfg Config, fetch FetchFunc, notify NotifyFunc, loc *time.Location) *ScheduledPoller {
	return &ScheduledPoller{Poller: New(cfg, fetch, notify), loc: loc}
}

// AddSchedule adds a cron-based poll schedule.
func (sp *ScheduledPoller) AddSchedule(sched PollSchedule) {
	sp.schedules = append(sp.schedules, sched)
}
