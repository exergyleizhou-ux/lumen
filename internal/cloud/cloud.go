// Package cloud provides a multi-cloud abstraction layer for deploying
// and managing Lumen agents across AWS, GCP, and Azure. It normalizes
// compute, storage, and networking primitives behind a single interface.
package cloud

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Provider abstracts a cloud provider interface.
type Provider interface {
	Name() string
	Regions() ([]string, error)
	Deploy(config DeployConfig) (*Deployment, error)
	Destroy(id string) error
	Status(id string) (*Deployment, error)
}

// DeployConfig specifies deployment parameters.
type DeployConfig struct {
	Name      string            `json:"name"`
	Region    string            `json:"region"`
	Image     string            `json:"image"`
	Resources ResourceSpec      `json:"resources"`
	Env       map[string]string `json:"env,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// ResourceSpec specifies compute resources.
type ResourceSpec struct {
	CPU    string `json:"cpu"`
	Memory string `json:"memory"`
	Disk   string `json:"disk"`
	GPU    string `json:"gpu,omitempty"`
}

// Deployment is a running cloud deployment.
type Deployment struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Provider  string            `json:"provider"`
	Region    string            `json:"region"`
	Status    string            `json:"status"`
	Endpoint  string            `json:"endpoint,omitempty"`
	Resources ResourceSpec      `json:"resources"`
	CreatedAt time.Time         `json:"created_at"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// MultiCloud manages deployments across providers.
type MultiCloud struct {
	mu          sync.RWMutex
	providers   map[string]Provider
	deployments map[string]*Deployment
}

// NewMultiCloud creates a multi-cloud manager.
func NewMultiCloud() *MultiCloud {
	return &MultiCloud{providers: map[string]Provider{}, deployments: map[string]*Deployment{}}
}

// Register adds a provider.
func (mc *MultiCloud) Register(p Provider) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.providers[p.Name()] = p
}

// Providers returns registered provider names.
func (mc *MultiCloud) Providers() []string {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	var out []string
	for n := range mc.providers {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Deploy launches a deployment on a specific provider.
func (mc *MultiCloud) Deploy(provider string, config DeployConfig) (*Deployment, error) {
	mc.mu.RLock()
	p, ok := mc.providers[provider]
	mc.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("provider %q not found", provider)
	}
	d, err := p.Deploy(config)
	if err != nil {
		return nil, err
	}
	d.Provider = provider
	mc.mu.Lock()
	mc.deployments[d.ID] = d
	mc.mu.Unlock()
	return d, nil
}

// Destroy removes a deployment.
func (mc *MultiCloud) Destroy(id string) error {
	mc.mu.RLock()
	d, ok := mc.deployments[id]
	mc.mu.RUnlock()
	if !ok {
		return fmt.Errorf("deployment %q not found", id)
	}
	mc.mu.RLock()
	p, ok := mc.providers[d.Provider]
	mc.mu.RUnlock()
	if !ok {
		return fmt.Errorf("provider %q not found", d.Provider)
	}
	if err := p.Destroy(id); err != nil {
		return err
	}
	mc.mu.Lock()
	delete(mc.deployments, id)
	mc.mu.Unlock()
	return nil
}

// List returns all deployments.
func (mc *MultiCloud) List() []*Deployment {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	var out []*Deployment
	for _, d := range mc.deployments {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

// Find returns deployments matching a label selector.
func (mc *MultiCloud) Find(labels map[string]string) []*Deployment {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	var out []*Deployment
	for _, d := range mc.deployments {
		match := true
		for k, v := range labels {
			if d.Labels[k] != v {
				match = false
				break
			}
		}
		if match {
			out = append(out, d)
		}
	}
	return out
}

// ── Mock Provider for testing ──────────────────────────────

// MockProvider is an in-memory provider for testing.
type MockProvider struct {
	name        string
	regions     []string
	deployments map[string]*Deployment
	mu          sync.Mutex
	nextID      int64
}

// NewMockProvider creates a mock provider.
func NewMockProvider(name string, regions []string) *MockProvider {
	return &MockProvider{name: name, regions: regions, deployments: map[string]*Deployment{}}
}
func (m *MockProvider) Name() string               { return m.name }
func (m *MockProvider) Regions() ([]string, error) { return m.regions, nil }
func (m *MockProvider) Deploy(config DeployConfig) (*Deployment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	id := fmt.Sprintf("%s-%d", m.name, m.nextID)
	d := &Deployment{ID: id, Name: config.Name, Region: config.Region, Status: "running", Resources: config.Resources, CreatedAt: time.Now(), Labels: config.Labels}
	d.Endpoint = fmt.Sprintf("https://%s.example.com/%s", config.Region, id)
	m.deployments[id] = d
	return d, nil
}
func (m *MockProvider) Destroy(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.deployments[id]; !ok {
		return fmt.Errorf("not found")
	}
	delete(m.deployments, id)
	return nil
}
func (m *MockProvider) Status(id string) (*Deployment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.deployments[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return d, nil
}

// ── Cost Estimator ─────────────────────────────────────────

// Estimation is a cloud cost estimate.
type Estimation struct {
	Provider     string       `json:"provider"`
	Region       string       `json:"region"`
	Resources    ResourceSpec `json:"resources"`
	CPUHour      float64      `json:"cpu_hourly"`
	MemHour      float64      `json:"mem_hourly"`
	StorageHour  float64      `json:"storage_hourly"`
	TotalHourly  float64      `json:"total_hourly"`
	TotalMonthly float64      `json:"total_monthly"`
}

// EstimateCost computes a cost estimate for given resources.
func EstimateCost(provider, region string, res ResourceSpec) *Estimation {
	e := &Estimation{Provider: provider, Region: region, Resources: res}

	// Simplified pricing
	switch {
	case strings.HasPrefix(provider, "aws"):
		e.CPUHour = 0.04
		e.MemHour = 0.005
	case strings.HasPrefix(provider, "gcp"):
		e.CPUHour = 0.035
		e.MemHour = 0.0045
	default:
		e.CPUHour = 0.042
		e.MemHour = 0.0048
	}
	e.StorageHour = 0.001
	e.TotalHourly = e.CPUHour + e.MemHour + e.StorageHour
	e.TotalMonthly = e.TotalHourly * 730 // hours per month
	return e
}

// FormatEstimation formats a cost estimate.
func FormatEstimation(e *Estimation) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Cost Estimate: %s / %s\n%s\n\n", e.Provider, e.Region, strings.Repeat("─", 50))
	fmt.Fprintf(&sb, "  CPU:     $%.4f/hr\n", e.CPUHour)
	fmt.Fprintf(&sb, "  Memory:  $%.4f/hr\n", e.MemHour)
	fmt.Fprintf(&sb, "  Storage: $%.4f/hr\n", e.StorageHour)
	fmt.Fprintf(&sb, "  ──────────────────\n")
	fmt.Fprintf(&sb, "  Hourly:  $%.4f\n", e.TotalHourly)
	fmt.Fprintf(&sb, "  Monthly: $%.2f\n", e.TotalMonthly)
	return sb.String()
}

// ── Resource Optimizer ─────────────────────────────────────

// OptimumSuggestion is a resource optimization suggestion.
type OptimumSuggestion struct {
	Current ResourceSpec `json:"current"`
	Optimal ResourceSpec `json:"optimal"`
	Saving  float64      `json:"monthly_saving"`
	Reason  string       `json:"reason"`
}

// OptimizeResources suggests resource adjustments based on utilization.
func OptimizeResources(current ResourceSpec, cpuUtil, memUtil float64) *OptimumSuggestion {
	if cpuUtil > 80 && memUtil > 80 {
		return nil
	}
	if cpuUtil < 20 && memUtil < 20 {
		return &OptimumSuggestion{
			Current: current,
			Optimal: ResourceSpec{CPU: "0.5", Memory: "512Mi", Disk: current.Disk},
			Saving:  0.6 * EstimateCost("", "", current).TotalMonthly,
			Reason:  "Underutilized — both CPU and memory <20%",
		}
	}
	return nil
}

// ToJSON marshals a deployment to JSON.
func (d *Deployment) ToJSON() ([]byte, error) { return json.MarshalIndent(d, "", "  ") }
