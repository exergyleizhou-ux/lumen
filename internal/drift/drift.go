// Package drift provides configuration drift detection: desired state vs actual
// state comparison, diff computation, reconciliation suggestions, and drift
// history with timestamps.
package drift

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// State represents a key-value configuration state.
type State map[string]interface{}

// DeepCopy returns a deep copy of the state.
func (s State) DeepCopy() State {
	cpy := make(State)
	for k, v := range s {
		cpy[k] = deepCopyValue(v)
	}
	return cpy
}

func deepCopyValue(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		cpy := make(map[string]interface{})
		for mk, mv := range val {
			cpy[mk] = deepCopyValue(mv)
		}
		return cpy
	case []interface{}:
		cpy := make([]interface{}, len(val))
		for i, elem := range val {
			cpy[i] = deepCopyValue(elem)
		}
		return cpy
	default:
		return v
	}
}

// ---- Drift Detection Types ----

// DriftType classifies the kind of drift detected.
type DriftType int

const (
	// DriftAdded means a key exists in actual but not desired.
	DriftAdded DriftType = iota
	// DriftRemoved means a key exists in desired but not actual.
	DriftRemoved
	// DriftModified means a key exists in both but values differ.
	DriftModified
	// DriftTypeChanged means the type of the value changed.
	DriftTypeChanged
)

var driftTypeStrings = map[DriftType]string{
	DriftAdded:       "added",
	DriftRemoved:     "removed",
	DriftModified:    "modified",
	DriftTypeChanged: "type_changed",
}

func (dt DriftType) String() string {
	if s, ok := driftTypeStrings[dt]; ok {
		return s
	}
	return "unknown"
}

// DriftEntry represents a single configuration drift.
type DriftEntry struct {
	Key      string      `json:"key"`
	Type     DriftType   `json:"type"`
	Desired  interface{} `json:"desired,omitempty"`
	Actual   interface{} `json:"actual,omitempty"`
	Path     string      `json:"path"`
	Severity string      `json:"severity"` // "critical", "warning", "info"
}

// DriftReport holds the complete drift detection results.
type DriftReport struct {
	ID        string       `json:"id"`
	Timestamp time.Time    `json:"timestamp"`
	Entries   []DriftEntry `json:"entries"`
	Summary   DriftSummary `json:"summary"`
}

// DriftSummary provides aggregate drift information.
type DriftSummary struct {
	Total      int `json:"total"`
	Added      int `json:"added"`
	Removed    int `json:"removed"`
	Modified   int `json:"modified"`
	TypeChange int `json:"type_change"`
	Critical   int `json:"critical"`
	Warnings   int `json:"warnings"`
	Info       int `json:"info"`
}

// ---- Detector ----

// Detector compares desired and actual states to find drift.
type Detector struct {
	mu            sync.RWMutex
	desired       State
	history       []DriftReport
	reconcileFn   func(entry DriftEntry) string
	severityRules []SeverityRule
}

// SeverityRule maps key patterns to severity levels.
type SeverityRule struct {
	Pattern  string // glob or prefix match
	Severity string
}

// NewDetector creates a new drift detector.
func NewDetector(desired State) *Detector {
	return &Detector{
		desired:       desired.DeepCopy(),
		history:       make([]DriftReport, 0),
		severityRules: defaultSeverityRules(),
	}
}

func defaultSeverityRules() []SeverityRule {
	return []SeverityRule{
		{Pattern: "security.*", Severity: "critical"},
		{Pattern: "*.password", Severity: "critical"},
		{Pattern: "*.token", Severity: "critical"},
		{Pattern: "prod.*", Severity: "warning"},
		{Pattern: "*.timeout", Severity: "warning"},
		{Pattern: "logging.*", Severity: "info"},
	}
}

// SetDesired updates the desired state.
func (d *Detector) SetDesired(state State) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.desired = state.DeepCopy()
}

// GetDesired returns a copy of the desired state.
func (d *Detector) GetDesired() State {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.desired.DeepCopy()
}

// SetSeverityRules sets custom severity classification rules.
func (d *Detector) SetSeverityRules(rules []SeverityRule) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.severityRules = rules
}

// OnReconcile sets a callback that generates reconciliation suggestions.
func (d *Detector) OnReconcile(fn func(entry DriftEntry) string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.reconcileFn = fn
}

// Detect compares the actual state against the desired state.
func (d *Detector) Detect(actual State) *DriftReport {
	d.mu.RLock()
	desired := d.desired.DeepCopy()
	d.mu.RUnlock()

	report := &DriftReport{
		ID:        generateReportID(),
		Timestamp: time.Now(),
		Entries:   detectDrift(desired, actual, "", d.severityRules),
	}

	// Compute summary
	for _, entry := range report.Entries {
		report.Summary.Total++
		switch entry.Type {
		case DriftAdded:
			report.Summary.Added++
		case DriftRemoved:
			report.Summary.Removed++
		case DriftModified:
			report.Summary.Modified++
		case DriftTypeChanged:
			report.Summary.TypeChange++
		}
		switch entry.Severity {
		case "critical":
			report.Summary.Critical++
		case "warning":
			report.Summary.Warnings++
		case "info":
			report.Summary.Info++
		}
	}

	d.mu.Lock()
	d.history = append(d.history, *report)
	// Keep only last 100 reports
	if len(d.history) > 100 {
		d.history = d.history[len(d.history)-100:]
	}
	d.mu.Unlock()

	return report
}

func detectDrift(desired, actual State, prefix string, rules []SeverityRule) []DriftEntry {
	var entries []DriftEntry

	allKeys := make(map[string]bool)
	for k := range desired {
		allKeys[k] = true
	}
	for k := range actual {
		allKeys[k] = true
	}

	for key := range allKeys {
		fullPath := key
		if prefix != "" {
			fullPath = prefix + "." + key
		}

		dVal, dOk := desired[key]
		aVal, aOk := actual[key]

		switch {
		case !dOk && aOk:
			entries = append(entries, DriftEntry{
				Key:      key,
				Type:     DriftAdded,
				Actual:   aVal,
				Path:     fullPath,
				Severity: classifySeverity(fullPath, rules),
			})
		case dOk && !aOk:
			entries = append(entries, DriftEntry{
				Key:      key,
				Type:     DriftRemoved,
				Desired:  dVal,
				Path:     fullPath,
				Severity: classifySeverity(fullPath, rules),
			})
		case dOk && aOk:
			dMap, dIsMap := dVal.(map[string]interface{})
			aMap, aIsMap := aVal.(map[string]interface{})

			if dIsMap && aIsMap {
				subEntries := detectDrift(dMap, aMap, fullPath, rules)
				entries = append(entries, subEntries...)
			} else {
				entry := compareValues(key, fullPath, dVal, aVal, rules)
				if entry != nil {
					entries = append(entries, *entry)
				}
			}
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Path < entries[j].Path
	})

	return entries
}

func compareValues(key, path string, desired, actual interface{}, rules []SeverityRule) *DriftEntry {
	dJSON, _ := json.Marshal(desired)
	aJSON, _ := json.Marshal(actual)

	if string(dJSON) == string(aJSON) {
		return nil
	}

	// Check for type change
	dType := typeName(desired)
	aType := typeName(actual)

	if dType != aType {
		return &DriftEntry{
			Key:      key,
			Type:     DriftTypeChanged,
			Desired:  desired,
			Actual:   actual,
			Path:     path,
			Severity: classifySeverity(path, rules),
		}
	}

	return &DriftEntry{
		Key:      key,
		Type:     DriftModified,
		Desired:  desired,
		Actual:   actual,
		Path:     path,
		Severity: classifySeverity(path, rules),
	}
}

func typeName(v interface{}) string {
	switch v.(type) {
	case bool:
		return "bool"
	case float64, float32:
		return "number"
	case int, int64, int32, int16, int8:
		return "number"
	case string:
		return "string"
	case []interface{}:
		return "array"
	case map[string]interface{}:
		return "map"
	case nil:
		return "null"
	default:
		return fmt.Sprintf("%T", v)
	}
}

func classifySeverity(path string, rules []SeverityRule) string {
	for _, rule := range rules {
		if matchPattern(path, rule.Pattern) {
			return rule.Severity
		}
	}
	return "info"
}

func matchPattern(path, pattern string) bool {
	// Simple glob matching: * matches any sequence
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return path == pattern
	}

	// Check prefix
	if !strings.HasPrefix(path, parts[0]) {
		return false
	}
	rest := path[len(parts[0]):]

	for i := 1; i < len(parts)-1; i++ {
		idx := strings.Index(rest, parts[i])
		if idx < 0 {
			return false
		}
		rest = rest[idx+len(parts[i]):]
	}

	// Check suffix
	if !strings.HasSuffix(rest, parts[len(parts)-1]) {
		return false
	}

	return true
}

// ---- Diff Computation ----

// DiffResult represents a computed diff between desired and actual.
type DiffResult struct {
	Path    string      `json:"path"`
	Desired interface{} `json:"desired,omitempty"`
	Actual  interface{} `json:"actual,omitempty"`
	Action  string      `json:"action"` // "set", "delete", "noop"
}

// ComputeDiff produces a diff between desired and actual states.
func ComputeDiff(desired, actual State) []DiffResult {
	var results []DiffResult
	computeDiffRecursive(desired, actual, "", &results)
	sort.Slice(results, func(i, j int) bool {
		return results[i].Path < results[j].Path
	})
	return results
}

func computeDiffRecursive(desired, actual State, prefix string, results *[]DiffResult) {
	allKeys := make(map[string]bool)
	for k := range desired {
		allKeys[k] = true
	}
	for k := range actual {
		allKeys[k] = true
	}

	for key := range allKeys {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}

		dVal, dOk := desired[key]
		aVal, aOk := actual[key]

		switch {
		case !dOk && aOk:
			*results = append(*results, DiffResult{Path: path, Actual: aVal, Action: "delete"})
		case dOk && !aOk:
			*results = append(*results, DiffResult{Path: path, Desired: dVal, Action: "set"})
		case dOk && aOk:
			dMap, dIsMap := dVal.(map[string]interface{})
			aMap, aIsMap := aVal.(map[string]interface{})
			if dIsMap && aIsMap {
				computeDiffRecursive(dMap, aMap, path, results)
			} else {
				dJSON, _ := json.Marshal(dVal)
				aJSON, _ := json.Marshal(aVal)
				if string(dJSON) != string(aJSON) {
					*results = append(*results, DiffResult{
						Path:    path,
						Desired: dVal,
						Actual:  aVal,
						Action:  "set",
					})
				}
			}
		}
	}
}

// ---- Reconciliation ----

// ReconciliationPlan describes actions to reconcile drift.
type ReconciliationPlan struct {
	ID        string            `json:"id"`
	Timestamp time.Time         `json:"timestamp"`
	Actions   []ReconcileAction `json:"actions"`
}

// ReconcileAction is a single step to reconcile drift.
type ReconcileAction struct {
	Path        string      `json:"path"`
	Action      string      `json:"action"` // "set", "delete", "noop"
	Value       interface{} `json:"value,omitempty"`
	Description string      `json:"description"`
	Severity    string      `json:"severity"`
}

// GeneratePlan creates a reconciliation plan from a drift report.
func (d *Detector) GeneratePlan(report *DriftReport) *ReconciliationPlan {
	plan := &ReconciliationPlan{
		ID:        generatePlanID(),
		Timestamp: time.Now(),
		Actions:   make([]ReconcileAction, 0, len(report.Entries)),
	}

	for _, entry := range report.Entries {
		action := ReconcileAction{
			Path:     entry.Path,
			Severity: entry.Severity,
		}

		switch entry.Type {
		case DriftAdded:
			action.Action = "delete"
			action.Description = fmt.Sprintf("Remove unexpected key '%s'", entry.Path)
		case DriftRemoved:
			action.Action = "set"
			action.Value = entry.Desired
			action.Description = fmt.Sprintf("Restore missing key '%s'", entry.Path)
		case DriftModified, DriftTypeChanged:
			action.Action = "set"
			action.Value = entry.Desired
			action.Description = fmt.Sprintf("Reset '%s' to desired value", entry.Path)
		}

		if d.reconcileFn != nil {
			custom := d.reconcileFn(entry)
			if custom != "" {
				action.Description = custom
			}
		}

		plan.Actions = append(plan.Actions, action)
	}

	return plan
}

// ApplyPlan applies a reconciliation plan to a state.
func ApplyPlan(state State, plan *ReconciliationPlan) State {
	result := state.DeepCopy()
	for _, action := range plan.Actions {
		applyAction(result, action)
	}
	return result
}

func applyAction(state State, action ReconcileAction) {
	parts := strings.Split(action.Path, ".")
	if len(parts) == 0 {
		return
	}

	// Navigate to the parent
	current := state
	for i := 0; i < len(parts)-1; i++ {
		next, ok := current[parts[i]]
		if !ok {
			newMap := make(map[string]interface{})
			current[parts[i]] = newMap
			current = newMap
			continue
		}
		nextMap, ok := next.(map[string]interface{})
		if !ok {
			newMap := make(map[string]interface{})
			current[parts[i]] = newMap
			current = newMap
			continue
		}
		current = nextMap
	}

	lastKey := parts[len(parts)-1]
	switch action.Action {
	case "set":
		current[lastKey] = action.Value
	case "delete":
		delete(current, lastKey)
	}
}

// ---- History ----

// History returns the drift history.
func (d *Detector) History() []DriftReport {
	d.mu.RLock()
	defer d.mu.RUnlock()
	result := make([]DriftReport, len(d.history))
	copy(result, d.history)
	return result
}

// HistorySince returns drift reports since a given time.
func (d *Detector) HistorySince(since time.Time) []DriftReport {
	d.mu.RLock()
	defer d.mu.RUnlock()
	var result []DriftReport
	for _, report := range d.history {
		if report.Timestamp.After(since) {
			result = append(result, report)
		}
	}
	return result
}

// LastReport returns the most recent drift report.
func (d *Detector) LastReport() (*DriftReport, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if len(d.history) == 0 {
		return nil, false
	}
	last := d.history[len(d.history)-1]
	return &last, true
}

// ClearHistory removes all drift history.
func (d *Detector) ClearHistory() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.history = make([]DriftReport, 0)
}

// ---- Snapshots ----

// Snapshot captures a point-in-time state.
type Snapshot struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	State     State     `json:"state"`
	Label     string    `json:"label,omitempty"`
}

// SnapshotManager stores and retrieves state snapshots.
type SnapshotManager struct {
	mu        sync.RWMutex
	snapshots []Snapshot
}

// NewSnapshotManager creates a snapshot manager.
func NewSnapshotManager() *SnapshotManager {
	return &SnapshotManager{
		snapshots: make([]Snapshot, 0),
	}
}

// Take creates a snapshot of the current state.
func (sm *SnapshotManager) Take(state State, label string) *Snapshot {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	s := Snapshot{
		ID:        generateSnapshotID(),
		Timestamp: time.Now(),
		State:     state.DeepCopy(),
		Label:     label,
	}
	sm.snapshots = append(sm.snapshots, s)
	return &s
}

// List returns all snapshots.
func (sm *SnapshotManager) List() []Snapshot {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	result := make([]Snapshot, len(sm.snapshots))
	copy(result, sm.snapshots)
	return result
}

// Get returns a snapshot by ID.
func (sm *SnapshotManager) Get(id string) (*Snapshot, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	for _, s := range sm.snapshots {
		if s.ID == id {
			return &s, true
		}
	}
	return nil, false
}

// Latest returns the most recent snapshot.
func (sm *SnapshotManager) Latest() (*Snapshot, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	if len(sm.snapshots) == 0 {
		return nil, false
	}
	latest := sm.snapshots[len(sm.snapshots)-1]
	return &latest, true
}

// CompareSnapshots compares two snapshots and returns drift entries.
func CompareSnapshots(a, b Snapshot) []DriftEntry {
	return detectDrift(a.State, b.State, "", defaultSeverityRules())
}

// ---- Hash utilities ----

// StateHash computes a SHA-256 hash of a state for quick comparison.
func StateHash(state State) string {
	data, _ := json.Marshal(state)
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h)
}

// HasDrift returns true if desired and actual differ.
func HasDrift(desired, actual State) bool {
	return StateHash(desired) != StateHash(actual)
}

// ---- Report formatting ----

// FormatDriftReport returns a human-readable drift report.
func FormatDriftReport(report *DriftReport) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Drift Report [%s] at %s\n", report.ID, report.Timestamp.Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("Total drifts: %d (added: %d, removed: %d, modified: %d, type_change: %d)\n",
		report.Summary.Total, report.Summary.Added, report.Summary.Removed,
		report.Summary.Modified, report.Summary.TypeChange))
	sb.WriteString(fmt.Sprintf("Severity: %d critical, %d warnings, %d info\n",
		report.Summary.Critical, report.Summary.Warnings, report.Summary.Info))

	if len(report.Entries) > 0 {
		sb.WriteString("\nDetails:\n")
		for _, entry := range report.Entries {
			sb.WriteString(fmt.Sprintf("  [%s] %s: %s\n", entry.Severity, entry.Type, entry.Path))
		}
	}

	return sb.String()
}

// FormatReconciliationPlan returns a human-readable plan.
func FormatReconciliationPlan(plan *ReconciliationPlan) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Reconciliation Plan [%s] at %s\n", plan.ID, plan.Timestamp.Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("%d actions:\n", len(plan.Actions)))
	for i, action := range plan.Actions {
		sb.WriteString(fmt.Sprintf("  %d. [%s] %s %s: %s\n",
			i+1, action.Severity, action.Action, action.Path, action.Description))
	}
	return sb.String()
}

// ---- ID generation ----

func generateReportID() string {
	return "drift-" + time.Now().Format("20060102-150405") + "-" + randomSuffix(8)
}

func generatePlanID() string {
	return "plan-" + time.Now().Format("20060102-150405") + "-" + randomSuffix(8)
}

func generateSnapshotID() string {
	return "snap-" + time.Now().Format("20060102-150405") + "-" + randomSuffix(8)
}

func randomSuffix(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte('a' + (time.Now().UnixNano()+int64(i))%26)
	}
	return string(b)
}

// ---- Utilities ----

// MergeStates merges multiple states. Later states override earlier ones.
func MergeStates(states ...State) State {
	result := make(State)
	for _, s := range states {
		for k, v := range s {
			result[k] = deepCopyValue(v)
		}
	}
	return result
}

// Keys returns all top-level keys from a state.
func Keys(state State) []string {
	keys := make([]string, 0, len(state))
	for k := range state {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// GetByPath retrieves a nested value by dot-separated path.
func GetByPath(state State, path string) (interface{}, bool) {
	parts := strings.Split(path, ".")
	current := state
	for i, part := range parts {
		val, ok := current[part]
		if !ok {
			return nil, false
		}
		if i == len(parts)-1 {
			return val, true
		}
		nextMap, ok := val.(map[string]interface{})
		if !ok {
			return nil, false
		}
		current = nextMap
	}
	return nil, false
}

// SetByPath sets a nested value by dot-separated path.
func SetByPath(state State, path string, value interface{}) {
	parts := strings.Split(path, ".")
	current := state
	for i := 0; i < len(parts)-1; i++ {
		next, ok := current[parts[i]]
		if !ok {
			newMap := make(map[string]interface{})
			current[parts[i]] = newMap
			current = newMap
			continue
		}
		nextMap, ok := next.(map[string]interface{})
		if !ok {
			newMap := make(map[string]interface{})
			current[parts[i]] = newMap
			current = newMap
			continue
		}
		current = nextMap
	}
	current[parts[len(parts)-1]] = value
}

// DeleteByPath deletes a nested value by path.
func DeleteByPath(state State, path string) bool {
	parts := strings.Split(path, ".")
	current := state
	for i := 0; i < len(parts)-1; i++ {
		val, ok := current[parts[i]]
		if !ok {
			return false
		}
		nextMap, ok := val.(map[string]interface{})
		if !ok {
			return false
		}
		current = nextMap
	}
	last := parts[len(parts)-1]
	if _, ok := current[last]; ok {
		delete(current, last)
		return true
	}
	return false
}

// Flatten converts a nested state to flat dot-separated keys.
func Flatten(state State) map[string]interface{} {
	result := make(map[string]interface{})
	flattenRecursive(state, "", result)
	return result
}

func flattenRecursive(state State, prefix string, result map[string]interface{}) {
	for k, v := range state {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		switch val := v.(type) {
		case map[string]interface{}:
			flattenRecursive(val, path, result)
		default:
			result[path] = v
		}
	}
}

// Unflatten converts flat dot-separated keys to nested state.
func Unflatten(flat map[string]interface{}) State {
	result := make(State)
	for path, value := range flat {
		SetByPath(result, path, value)
	}
	return result
}
