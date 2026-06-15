// Package monitor provides real-time agent monitoring with Prometheus-style
// metrics export, health dashboards, alerting rules, and trend analysis.
// It tracks agent performance, error rates, latency percentiles, and
// resource utilization across sessions and turns.
package monitor

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

// ── Collector ──────────────────────────────────────────────

type MetricKind int

const (
	MetricCounter MetricKind = iota
	MetricGauge
	MetricHistogram
)

type Metric struct {
	Name   string
	Kind   MetricKind
	Help   string
	Labels map[string]string
}
type CounterVal struct {
	Val    int64
	Labels map[string]string
}
type GaugeVal struct {
	Val    float64
	Labels map[string]string
}
type HistogramVal struct {
	Sum     float64
	Count   int64
	Buckets map[float64]int64
	Labels  map[string]string
}

type Collector struct {
	mu         sync.RWMutex
	metrics    map[string]*Metric
	counters   map[string][]*CounterVal
	gauges     map[string][]*GaugeVal
	histograms map[string][]*HistogramVal
}

func NewCollector() *Collector {
	return &Collector{metrics: map[string]*Metric{}, counters: map[string][]*CounterVal{}, gauges: map[string][]*GaugeVal{}, histograms: map[string][]*HistogramVal{}}
}
func (c *Collector) RegisterCounter(name, help string) {
	c.mu.Lock()
	c.metrics[name] = &Metric{Name: name, Kind: MetricCounter, Help: help}
	c.mu.Unlock()
}
func (c *Collector) Inc(name string, labels map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.counters[name] = append(c.counters[name], &CounterVal{Val: 1, Labels: labels})
}
func (c *Collector) SetGauge(name string, val float64, labels map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gauges[name] = append(c.gauges[name], &GaugeVal{Val: val, Labels: labels})
}
func (c *Collector) Observe(name string, val float64, labels map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.histograms[name] = append(c.histograms[name], &HistogramVal{Sum: val, Count: 1, Labels: labels})
}
func (c *Collector) CounterSum(name string) int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.counterSumLocked(name)
}

// counterSumLocked assumes the caller holds the lock.
func (c *Collector) counterSumLocked(name string) int64 {
	var sum int64
	for _, v := range c.counters[name] {
		sum += v.Val
	}
	return sum
}
func (c *Collector) ExportPrometheus() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var sb strings.Builder
	for _, m := range c.metrics {
		sb.WriteString(fmt.Sprintf("# HELP %s %s\n# TYPE %s %s\n", m.Name, m.Help, m.Name, metricTypeStr(m.Kind)))
	}
	for name, vals := range c.counters {
		for _, v := range vals {
			fmt.Fprintf(&sb, "%s%s %d\n", name, formatLabels(v.Labels), v.Val)
		}
	}
	for name, vals := range c.gauges {
		for _, v := range vals {
			fmt.Fprintf(&sb, "%s%s %f\n", name, formatLabels(v.Labels), v.Val)
		}
	}
	for name, vals := range c.histograms {
		for _, v := range vals {
			fmt.Fprintf(&sb, "%s%s_sum %f\n%s%s_count %d\n", name, formatLabels(v.Labels), v.Sum, name, formatLabels(v.Labels), v.Count)
		}
	}
	return sb.String()
}
func (c *Collector) FormatSummary() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Metrics Summary (%d registered):\n\n", len(c.metrics)))
	names := make([]string, 0, len(c.metrics))
	for n := range c.metrics {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		m := c.metrics[n]
		switch m.Kind {
		case MetricCounter:
			fmt.Fprintf(&sb, "  %-30s = %d\n", n, c.counterSumLocked(n))
		case MetricGauge:
			if vals, ok := c.gauges[n]; ok && len(vals) > 0 {
				fmt.Fprintf(&sb, "  %-30s = %f\n", n, vals[len(vals)-1].Val)
			}
		}
	}
	return sb.String()
}

func metricTypeStr(k MetricKind) string {
	switch k {
	case MetricCounter:
		return "counter"
	case MetricGauge:
		return "gauge"
	case MetricHistogram:
		return "histogram"
	}
	return "untyped"
}
func formatLabels(l map[string]string) string {
	if len(l) == 0 {
		return ""
	}
	var parts []string
	for k, v := range l {
		parts = append(parts, fmt.Sprintf(`%s="%s"`, k, v))
	}
	sort.Strings(parts)
	return "{" + strings.Join(parts, ",") + "}"
}

// ── Dashboard ─────────────────────────────────────────────

type Dashboard struct {
	collector *Collector
	sections  []Section
}
type Section struct {
	Name    string
	Metrics []string
	Refresh time.Duration
}

func NewDashboard(c *Collector) *Dashboard { return &Dashboard{collector: c} }
func (d *Dashboard) AddSection(s Section)  { d.sections = append(d.sections, s) }
func (d *Dashboard) Render() string        { return d.collector.FormatSummary() }

// ── Alerting ──────────────────────────────────────────────

type AlertRule struct {
	Name      string
	Metric    string
	Condition string
	Threshold float64
	Duration  time.Duration
	Message   string
}
type Alert struct {
	Rule    AlertRule
	Value   float64
	FiredAt time.Time
	Active  bool
}
type AlertManager struct {
	mu      sync.Mutex
	rules   []AlertRule
	active  map[string]*Alert
	history []*Alert
}

func NewAlertManager() *AlertManager        { return &AlertManager{active: map[string]*Alert{}} }
func (a *AlertManager) AddRule(r AlertRule) { a.mu.Lock(); a.rules = append(a.rules, r); a.mu.Unlock() }
func (a *AlertManager) Evaluate(collector *Collector) []*Alert {
	a.mu.Lock()
	defer a.mu.Unlock()
	var fired []*Alert
	for _, rule := range a.rules {
		val := float64(collector.CounterSum(rule.Metric))
		triggered := false
		switch rule.Condition {
		case "gt":
			triggered = val > rule.Threshold
		case "lt":
			triggered = val < rule.Threshold
		case "gte":
			triggered = val >= rule.Threshold
		case "lte":
			triggered = val <= rule.Threshold
		}
		if triggered {
			if _, ok := a.active[rule.Name]; !ok {
				alert := &Alert{Rule: rule, Value: val, FiredAt: time.Now(), Active: true}
				a.active[rule.Name] = alert
				a.history = append(a.history, alert)
				fired = append(fired, alert)
			}
		} else {
			delete(a.active, rule.Name)
		}
	}
	return fired
}
func (a *AlertManager) FormatAlerts() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.active) == 0 {
		return "No active alerts.\n"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d active alert(s):\n\n", len(a.active))
	for _, al := range a.active {
		fmt.Fprintf(&sb, "  🔴 %s: %s (value:%.0f, threshold:%.0f)\n", al.Rule.Name, al.Rule.Message, al.Value, al.Rule.Threshold)
	}
	return sb.String()
}

// ── Trend Analyzer ────────────────────────────────────────

type TrendPoint struct {
	Timestamp time.Time
	Value     float64
}
type TrendAnalyzer struct {
	mu        sync.Mutex
	points    map[string][]TrendPoint
	maxPoints int
}

func NewTrendAnalyzer(maxPoints int) *TrendAnalyzer {
	return &TrendAnalyzer{points: map[string][]TrendPoint{}, maxPoints: maxPoints}
}
func (t *TrendAnalyzer) Record(metric string, val float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.points[metric] = append(t.points[metric], TrendPoint{time.Now(), val})
	if len(t.points[metric]) > t.maxPoints {
		t.points[metric] = t.points[metric][len(t.points[metric])-t.maxPoints:]
	}
}
func (t *TrendAnalyzer) Slope(metric string) float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.slopeLocked(metric)
}

// slopeLocked assumes the caller holds the lock.
func (t *TrendAnalyzer) slopeLocked(metric string) float64 {
	pts := t.points[metric]
	if len(pts) < 2 {
		return 0
	}
	n := float64(len(pts))
	sumX, sumY, sumXY, sumX2 := 0.0, 0.0, 0.0, 0.0
	for i, p := range pts {
		x := float64(i)
		sumX += x
		sumY += p.Value
		sumXY += x * p.Value
		sumX2 += x * x
	}
	return (n*sumXY - sumX*sumY) / (n*sumX2 - sumX*sumX)
}
func (t *TrendAnalyzer) FormatTrends() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	var sb strings.Builder
	sb.WriteString("Trend Analysis:\n\n")
	names := make([]string, 0, len(t.points))
	for n := range t.points {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		slope := t.slopeLocked(n)
		icon := "→"
		if slope > 0.1 {
			icon = "↗"
		} else if slope < -0.1 {
			icon = "↘"
		}
		fmt.Fprintf(&sb, "  %s %-25s slope:%.3f\n", icon, n, slope)
	}
	return sb.String()
}

// ── Latency Stats ─────────────────────────────────────────

type LatencyStats struct {
	mu         sync.Mutex
	samples    []float64
	maxSamples int
}

func NewLatencyStats(maxSamples int) *LatencyStats { return &LatencyStats{maxSamples: maxSamples} }
func (l *LatencyStats) Record(d time.Duration) {
	l.mu.Lock()
	l.samples = append(l.samples, float64(d.Milliseconds()))
	if len(l.samples) > l.maxSamples {
		l.samples = l.samples[len(l.samples)-l.maxSamples:]
	}
	l.mu.Unlock()
}
func (l *LatencyStats) Percentile(p float64) float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.percentileLocked(p)
}

// percentileLocked assumes the caller holds the lock.
func (l *LatencyStats) percentileLocked(p float64) float64 {
	if len(l.samples) == 0 {
		return 0
	}
	sorted := make([]float64, len(l.samples))
	copy(sorted, l.samples)
	sort.Float64s(sorted)
	idx := int(math.Ceil(p/100*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	return sorted[idx]
}
func (l *LatencyStats) Avg() float64 { l.mu.Lock(); defer l.mu.Unlock(); return l.avgLocked() }

// avgLocked assumes the caller holds the lock.
func (l *LatencyStats) avgLocked() float64 {
	if len(l.samples) == 0 {
		return 0
	}
	var sum float64
	for _, s := range l.samples {
		sum += s
	}
	return sum / float64(len(l.samples))
}
func (l *LatencyStats) Format() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.samples) == 0 {
		return "No data.\n"
	}
	return fmt.Sprintf("Latency: avg=%.1fms p50=%.1fms p95=%.1fms p99=%.1fms (%d samples)", l.avgLocked(), l.percentileLocked(50), l.percentileLocked(95), l.percentileLocked(99), len(l.samples))
}
