// Package drift detects configuration and infrastructure drift between
// desired and actual state. It compares golden files against runtime
// configurations, detects unauthorized changes, and generates remediation
// plans. Used for infrastructure-as-code validation.
package drift

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Detection is one drift finding.
type Detection struct {
	Path       string    `json:"path"`
	Type       string    `json:"type"` // "added", "removed", "modified", "permission"
	Expected   string    `json:"expected"`
	Actual     string    `json:"actual"`
	Severity   string    `json:"severity"`
	DetectedAt time.Time `json:"detected_at"`
}

// Baseline is a snapshot of desired state.
type Baseline struct {
	Name      string            `json:"name"`
	CreatedAt time.Time         `json:"created_at"`
	Files     map[string]string `json:"files"` // path → SHA256
}

// Detector compares current state against a baseline.
type Detector struct {
	mu       sync.Mutex
	baseline *Baseline
	dir      string
}

// NewDetector creates a drift detector.
func NewDetector(dir string) *Detector {
	return &Detector{dir: dir}
}

// CaptureBaseline records the current state as the desired baseline.
func (d *Detector) CaptureBaseline(name string) (*Baseline, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	b := &Baseline{Name: name, CreatedAt: time.Now(), Files: map[string]string{}}
	err := filepath.Walk(d.dir, func(path string, info os.FileInfo, err error) error {
		if err != nil { return nil }
		if info.IsDir() {
			n := info.Name()
			if strings.HasPrefix(n, ".") || n == "vendor" || n == "node_modules" { return filepath.SkipDir }
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil { return nil }
		h := sha256.Sum256(data)
		b.Files[path] = hex.EncodeToString(h[:])
		return nil
	})
	if err != nil { return nil, err }
	d.baseline = b
	return b, nil
}

// Detect finds drift between current state and the baseline.
func (d *Detector) Detect() ([]Detection, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.baseline == nil { return nil, fmt.Errorf("no baseline captured — run CaptureBaseline first") }

	var findings []Detection
	current := map[string]string{}

	filepath.Walk(d.dir, func(path string, info os.FileInfo, err error) error {
		if err != nil { return nil }
		if info.IsDir() {
			n := info.Name()
			if strings.HasPrefix(n, ".") || n == "vendor" || n == "node_modules" { return filepath.SkipDir }
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil { return nil }
		h := sha256.Sum256(data)
		current[path] = hex.EncodeToString(h[:])
		return nil
	})

	// Added or modified files
	for path, hash := range current {
		expected, ok := d.baseline.Files[path]
		if !ok {
			findings = append(findings, Detection{
				Path: path, Type: "added", Actual: hash[:8],
				Severity: "info", DetectedAt: time.Now(),
			})
		} else if hash != expected {
			findings = append(findings, Detection{
				Path: path, Type: "modified", Expected: expected[:8], Actual: hash[:8],
				Severity: "warning", DetectedAt: time.Now(),
			})
		}
	}

	// Removed files
	for path, hash := range d.baseline.Files {
		if _, ok := current[path]; !ok {
			findings = append(findings, Detection{
				Path: path, Type: "removed", Expected: hash[:8],
				Severity: "warning", DetectedAt: time.Now(),
			})
		}
	}

	return findings, nil
}

// FormatDetections formats drift findings for display.
func FormatDetections(findings []Detection) string {
	if len(findings) == 0 {
		return "No drift detected — configuration matches baseline.\n"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Drift Report (%d findings):\n\n", len(findings))
	sort.Slice(findings, func(i, j int) bool { return findings[i].Path < findings[j].Path })

	added, modified, removed := 0, 0, 0
	for _, f := range findings {
		switch f.Type {
		case "added": added++
		case "modified": modified++
		case "removed": removed++
		}
		icon := iconFor(f.Severity)
		fmt.Fprintf(&sb, "%s [%s] %s\n", icon, f.Type, f.Path)
	}
	fmt.Fprintf(&sb, "\nSummary: +%d added ~%d modified -%d removed\n", added, modified, removed)
	return sb.String()
}

func iconFor(s string) string {
	switch s {
	case "warning": return "⚠️"
	case "critical": return "🔴"
	default: return "ℹ️"
	}
}
