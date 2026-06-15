// Package drift - additional extension: state reconciliation engine,
// configuration versioning, rollback support, diff visualization.
package drift

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// ---- Reconcile Engine ----

// ReconcileEngine automates drift reconciliation with approval workflow.
type ReconcileEngine struct {
	detector      *Detector
	pendingPlans  []*ReconciliationPlan
	appliedPlans  []*ReconciliationPlan
	requireApproval bool
}

// NewReconcileEngine creates a reconciliation engine.
func NewReconcileEngine(detector *Detector) *ReconcileEngine {
	return &ReconcileEngine{
		detector:       detector,
		pendingPlans:   make([]*ReconciliationPlan, 0),
		appliedPlans:   make([]*ReconciliationPlan, 0),
		requireApproval: true,
	}
}

// SetRequireApproval controls whether plans need approval before application.
func (re *ReconcileEngine) SetRequireApproval(v bool) {
	re.requireApproval = v
}

// DetectAndPlan detects drift and generates a reconciliation plan.
func (re *ReconcileEngine) DetectAndPlan(actual State) (*ReconciliationPlan, error) {
	report := re.detector.Detect(actual)
	if report.Summary.Total == 0 {
		return nil, nil // no drift
	}
	plan := re.detector.GeneratePlan(report)
	re.pendingPlans = append(re.pendingPlans, plan)
	return plan, nil
}

// ApprovePlan approves a pending plan by ID.
func (re *ReconcileEngine) ApprovePlan(planID string) bool {
	for i, p := range re.pendingPlans {
		if p.ID == planID {
			re.pendingPlans = append(re.pendingPlans[:i], re.pendingPlans[i+1:]...)
			re.appliedPlans = append(re.appliedPlans, p)
			return true
		}
	}
	return false
}

// RejectPlan rejects a pending plan.
func (re *ReconcileEngine) RejectPlan(planID string) bool {
	for i, p := range re.pendingPlans {
		if p.ID == planID {
			re.pendingPlans = append(re.pendingPlans[:i], re.pendingPlans[i+1:]...)
			return true
		}
	}
	return false
}

// PendingPlans returns all pending plans.
func (re *ReconcileEngine) PendingPlans() []*ReconciliationPlan {
	return re.pendingPlans
}

// AppliedPlans returns all applied plans.
func (re *ReconcileEngine) AppliedPlans() []*ReconciliationPlan {
	return re.appliedPlans
}

// ApplyAndReconcile detects, plans, and applies reconciliation.
func (re *ReconcileEngine) ApplyAndReconcile(actual State, state *State) (*ReconciliationPlan, error) {
	plan, err := re.DetectAndPlan(actual)
	if err != nil {
		return nil, err
	}
	if plan == nil {
		return nil, nil
	}
	if re.requireApproval {
		return plan, nil // requires approval
	}
	*state = ApplyPlan(*state, plan)
	re.appliedPlans = append(re.appliedPlans, plan)
	return plan, nil
}

// ---- Configuration Versioning ----

// ConfigVersion represents a versioned configuration state.
type ConfigVersion struct {
	Version   int       `json:"version"`
	State     State     `json:"state"`
	Timestamp time.Time `json:"timestamp"`
	Author    string    `json:"author,omitempty"`
	Message   string    `json:"message,omitempty"`
	Hash      string    `json:"hash"`
}

// ConfigVersionManager manages versioned configurations.
type ConfigVersionManager struct {
	versions []ConfigVersion
	current  int
}

// NewConfigVersionManager creates a version manager.
func NewConfigVersionManager() *ConfigVersionManager {
	return &ConfigVersionManager{
		versions: make([]ConfigVersion, 0),
		current:  -1,
	}
}

// Save saves a new version of the state.
func (cvm *ConfigVersionManager) Save(state State, author, message string) *ConfigVersion {
	v := ConfigVersion{
		Version:   len(cvm.versions) + 1,
		State:     state.DeepCopy(),
		Timestamp: time.Now(),
		Author:    author,
		Message:   message,
		Hash:      StateHash(state),
	}
	cvm.versions = append(cvm.versions, v)
	cvm.current = len(cvm.versions) - 1
	return &v
}

// Current returns the current version.
func (cvm *ConfigVersionManager) Current() (*ConfigVersion, bool) {
	if cvm.current < 0 || cvm.current >= len(cvm.versions) {
		return nil, false
	}
	v := cvm.versions[cvm.current]
	return &v, true
}

// Get returns a specific version by number.
func (cvm *ConfigVersionManager) Get(version int) (*ConfigVersion, bool) {
	if version < 1 || version > len(cvm.versions) {
		return nil, false
	}
	v := cvm.versions[version-1]
	return &v, true
}

// List returns all versions.
func (cvm *ConfigVersionManager) List() []ConfigVersion {
	result := make([]ConfigVersion, len(cvm.versions))
	copy(result, cvm.versions)
	return result
}

// Diff computes the drift between two versions.
func (cvm *ConfigVersionManager) Diff(fromVersion, toVersion int) ([]DriftEntry, error) {
	from, ok := cvm.Get(fromVersion)
	if !ok {
		return nil, fmt.Errorf("version %d not found", fromVersion)
	}
	to, ok := cvm.Get(toVersion)
	if !ok {
		return nil, fmt.Errorf("version %d not found", toVersion)
	}
	return detectDrift(from.State, to.State, "", defaultSeverityRules()), nil
}

// Rollback rolls back to a previous version.
func (cvm *ConfigVersionManager) Rollback(version int) (*ConfigVersion, error) {
	v, ok := cvm.Get(version)
	if !ok {
		return nil, fmt.Errorf("version %d not found", version)
	}
	cvm.current = version - 1
	return v, nil
}

// Latest returns the latest version.
func (cvm *ConfigVersionManager) Latest() (*ConfigVersion, bool) {
	if len(cvm.versions) == 0 {
		return nil, false
	}
	v := cvm.versions[len(cvm.versions)-1]
	return &v, true
}

// ---- Advanced Diff Operations ----

// DiffDetail provides detailed diff information for display.
type DiffDetail struct {
	Path       string `json:"path"`
	OldValue   string `json:"old_value,omitempty"`
	NewValue   string `json:"new_value,omitempty"`
	ChangeType string `json:"change_type"` // "add", "remove", "change"
}

// DetailedDiff computes a detailed diff suitable for display.
func DetailedDiff(desired, actual State) []DiffDetail {
	flatD := Flatten(desired)
	flatA := Flatten(actual)

	var details []DiffDetail

	for k, dVal := range flatD {
		aVal, ok := flatA[k]
		if !ok {
			details = append(details, DiffDetail{
				Path:       k,
				OldValue:   fmt.Sprintf("%v", dVal),
				NewValue:   "",
				ChangeType: "remove",
			})
		} else if fmt.Sprintf("%v", dVal) != fmt.Sprintf("%v", aVal) {
			details = append(details, DiffDetail{
				Path:       k,
				OldValue:   fmt.Sprintf("%v", dVal),
				NewValue:   fmt.Sprintf("%v", aVal),
				ChangeType: "change",
			})
		}
	}

	for k, aVal := range flatA {
		if _, ok := flatD[k]; !ok {
			details = append(details, DiffDetail{
				Path:       k,
				OldValue:   "",
				NewValue:   fmt.Sprintf("%v", aVal),
				ChangeType: "add",
			})
		}
	}

	sort.Slice(details, func(i, j int) bool {
		return details[i].Path < details[j].Path
	})

	return details
}

// DiffSummaryString produces a single-line summary of the diff.
func DiffSummaryString(details []DiffDetail) string {
	adds, rems, changes := 0, 0, 0
	for _, d := range details {
		switch d.ChangeType {
		case "add":
			adds++
		case "remove":
			rems++
		case "change":
			changes++
		}
	}
	return fmt.Sprintf("+%d -%d ~%d", adds, rems, changes)
}

// ---- Patch-like Operations ----

// ConfigPatch represents a set of changes to apply.
type ConfigPatch struct {
	Operations []PatchOperation `json:"operations"`
}

// PatchOperation is a single patch operation.
type PatchOperation struct {
	Op    string      `json:"op"`    // "add", "remove", "replace"
	Path  string      `json:"path"`  // dot-separated path
	Value interface{} `json:"value,omitempty"`
}

// ApplyConfigPatch applies a config patch to a state.
func ApplyConfigPatch(state State, patch *ConfigPatch) State {
	result := state.DeepCopy()
	for _, op := range patch.Operations {
		switch op.Op {
		case "add", "replace":
			SetByPath(result, op.Path, op.Value)
		case "remove":
			DeleteByPath(result, op.Path)
		}
	}
	return result
}

// CreateConfigPatch creates a patch from diff details.
func CreateConfigPatch(details []DiffDetail) *ConfigPatch {
	patch := &ConfigPatch{}
	for _, d := range details {
		switch d.ChangeType {
		case "add":
			patch.Operations = append(patch.Operations, PatchOperation{
				Op: "add", Path: d.Path, Value: d.NewValue,
			})
		case "remove":
			patch.Operations = append(patch.Operations, PatchOperation{
				Op: "remove", Path: d.Path,
			})
		case "change":
			patch.Operations = append(patch.Operations, PatchOperation{
				Op: "replace", Path: d.Path, Value: d.NewValue,
			})
		}
	}
	return patch
}

// ---- Drift Trend Analysis ----

// TrendPoint represents a drift measurement at a point in time.
type TrendPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Total     int       `json:"total"`
	Critical  int       `json:"critical"`
}

// DriftTrendAnalyzer tracks drift over time.
type DriftTrendAnalyzer struct {
	points []TrendPoint
}

// NewDriftTrendAnalyzer creates a trend analyzer.
func NewDriftTrendAnalyzer() *DriftTrendAnalyzer {
	return &DriftTrendAnalyzer{
		points: make([]TrendPoint, 0),
	}
}

// Record adds a trend point.
func (dta *DriftTrendAnalyzer) Record(report *DriftReport) {
	dta.points = append(dta.points, TrendPoint{
		Timestamp: report.Timestamp,
		Total:     report.Summary.Total,
		Critical:  report.Summary.Critical,
	})
}

// Points returns all recorded trend points.
func (dta *DriftTrendAnalyzer) Points() []TrendPoint {
	result := make([]TrendPoint, len(dta.points))
	copy(result, dta.points)
	return result
}

// IsIncreasing returns true if drift is trending upward.
func (dta *DriftTrendAnalyzer) IsIncreasing() bool {
	if len(dta.points) < 2 {
		return false
	}
	// Simple: compare first half average vs second half average
	mid := len(dta.points) / 2
	firstHalf := avgTotal(dta.points[:mid])
	secondHalf := avgTotal(dta.points[mid:])
	return secondHalf > firstHalf
}

func avgTotal(points []TrendPoint) float64 {
	if len(points) == 0 {
		return 0
	}
	var sum int
	for _, p := range points {
		sum += p.Total
	}
	return float64(sum) / float64(len(points))
}

// MaxDrift returns the maximum drift observed.
func (dta *DriftTrendAnalyzer) MaxDrift() (TrendPoint, bool) {
	if len(dta.points) == 0 {
		return TrendPoint{}, false
	}
	max := dta.points[0]
	for _, p := range dta.points[1:] {
		if p.Total > max.Total {
			max = p
		}
	}
	return max, true
}

// ---- Validation of Desired State ----

// ValidateState checks if a state is well-formed.
func ValidateState(state State) []string {
	var issues []string
	for key, value := range state {
		if strings.TrimSpace(key) == "" {
			issues = append(issues, "empty key found")
		}
		if key != strings.TrimSpace(key) {
			issues = append(issues, fmt.Sprintf("key '%s' has leading/trailing whitespace", key))
		}
		if value == nil {
			issues = append(issues, fmt.Sprintf("key '%s' has nil value", key))
		}
	}
	return issues
}

// ---- Batch Drift Detection ----

// BatchDetect runs drift detection across multiple desired/actual pairs.
func BatchDetect(pairs []struct{ Desired, Actual State }) []*DriftReport {
	reports := make([]*DriftReport, len(pairs))
	for i, pair := range pairs {
		d := NewDetector(pair.Desired)
		reports[i] = d.Detect(pair.Actual)
	}
	return reports
}

// MergeReports combines multiple drift reports into one.
func MergeReports(reports []*DriftReport) *DriftReport {
	merged := &DriftReport{
		ID:        generateReportID(),
		Timestamp: time.Now(),
		Entries:   make([]DriftEntry, 0),
	}

	for _, report := range reports {
		merged.Entries = append(merged.Entries, report.Entries...)
		merged.Summary.Total += report.Summary.Total
		merged.Summary.Added += report.Summary.Added
		merged.Summary.Removed += report.Summary.Removed
		merged.Summary.Modified += report.Summary.Modified
		merged.Summary.TypeChange += report.Summary.TypeChange
		merged.Summary.Critical += report.Summary.Critical
		merged.Summary.Warnings += report.Summary.Warnings
		merged.Summary.Info += report.Summary.Info
	}

	return merged
}
