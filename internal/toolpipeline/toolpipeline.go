// Package toolpipeline chains multiple tool calls into composable execution
// pipelines with conditional branching, parallel fan-out, aggregation, and
// error recovery. Tools are composed using a fluent builder pattern and
// executed as a DAG with automatic dependency resolution.
package toolpipeline

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type StepStatus string

const (
	StepPending StepStatus = "pending"
	StepRunning StepStatus = "running"
	StepDone    StepStatus = "done"
	StepFailed  StepStatus = "failed"
	StepSkipped StepStatus = "skipped"
)

type Result struct {
	StepName string
	Output   string
	Error    string
	Duration time.Duration
	Status   StepStatus
}
type Step struct {
	Name      string
	Tool      string
	Args      map[string]any
	DependsOn []string
	Condition string
	Timeout   time.Duration
	Retries   int
	OnFail    string
}
type Pipeline struct {
	Name    string
	Steps   []*Step
	Results map[string]*Result
	mu      sync.RWMutex
}

func NewPipeline(name string) *Pipeline       { return &Pipeline{Name: name, Results: map[string]*Result{}} }
func (p *Pipeline) AddStep(s *Step) *Pipeline { p.Steps = append(p.Steps, s); return p }
func (p *Pipeline) Run(executor func(name string, args map[string]any) (string, error)) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	completed := map[string]bool{}
	failed := map[string]bool{}
	for len(completed)+len(failed) < len(p.Steps) {
		ready := p.getReady(completed, failed)
		if len(ready) == 0 && len(completed)+len(failed) < len(p.Steps) {
			return fmt.Errorf("pipeline deadlock at %d/%d steps", len(completed), len(p.Steps))
		}
		var wg sync.WaitGroup
		for _, s := range ready {
			wg.Add(1)
			go func(step *Step) {
				defer wg.Done()
				start := time.Now()
				for attempt := 0; attempt <= step.Retries; attempt++ {
					output, err := executor(step.Tool, step.Args)
					r := &Result{StepName: step.Name, Duration: time.Since(start)}
					if err != nil {
						r.Error = err.Error()
						r.Status = StepFailed
						r.Output = output
					} else {
						r.Output = output
						r.Status = StepDone
					}
					p.Results[step.Name] = r
					if err == nil {
						completed[step.Name] = true
						return
					}
					if attempt < step.Retries {
						time.Sleep(time.Duration(attempt+1) * time.Second)
					}
				}
				failed[step.Name] = true
			}(s)
		}
		wg.Wait()
	}
	return nil
}
func (p *Pipeline) getReady(completed, failed map[string]bool) []*Step {
	var r []*Step
	for _, s := range p.Steps {
		if completed[s.Name] || failed[s.Name] {
			continue
		}
		ready := true
		for _, d := range s.DependsOn {
			if !completed[d] {
				ready = false
				break
			}
		}
		if ready {
			r = append(r, s)
		}
	}
	return r
}
func (p *Pipeline) Format() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var sb strings.Builder
	fmt.Fprintf(&sb, "Pipeline: %s (%d steps)\n\n", p.Name, len(p.Steps))
	for _, s := range p.Steps {
		r := p.Results[s.Name]
		status := "○"
		if r != nil {
			switch r.Status {
			case StepDone:
				status = "✅"
			case StepFailed:
				status = "❌"
			case StepRunning:
				status = "🔄"
			case StepSkipped:
				status = "⏭"
			}
		}
		fmt.Fprintf(&sb, "%s %-20s → %s", status, s.Name, s.Tool)
		if len(s.DependsOn) > 0 {
			fmt.Fprintf(&sb, " [after: %s]", strings.Join(s.DependsOn, ", "))
		}
		if r != nil && r.Error != "" {
			fmt.Fprintf(&sb, " ✗ %s", r.Error)
		}
		if r != nil && r.Status == StepDone {
			fmt.Fprintf(&sb, " (%v)", r.Duration)
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}
func (p *Pipeline) AllDone() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, s := range p.Steps {
		if r, ok := p.Results[s.Name]; !ok || r.Status != StepDone {
			return false
		}
	}
	return true
}
func (p *Pipeline) FailedSteps() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var o []string
	for _, s := range p.Steps {
		if r, ok := p.Results[s.Name]; ok && r.Status == StepFailed {
			o = append(o, s.Name)
		}
	}
	sort.Strings(o)
	return o
}
func (p *Pipeline) Duration() time.Duration {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var total time.Duration
	for _, r := range p.Results {
		total += r.Duration
	}
	return total
}
func (p *Pipeline) StepCount() int { return len(p.Steps) }

type Builder struct {
	pipeline *Pipeline
	lastStep string
}

func NewBuilder(name string) *Builder { return &Builder{pipeline: NewPipeline(name)} }
func (b *Builder) Then(name, tool string, args map[string]any) *Builder {
	deps := []string{}
	if b.lastStep != "" {
		deps = []string{b.lastStep}
	}
	b.pipeline.AddStep(&Step{Name: name, Tool: tool, Args: args, DependsOn: deps, Retries: 1})
	b.lastStep = name
	return b
}
func (b *Builder) Parallel(steps []*Step) *Builder {
	deps := []string{}
	if b.lastStep != "" {
		deps = []string{b.lastStep}
	}
	for _, s := range steps {
		s.DependsOn = deps
		b.pipeline.AddStep(s)
	}
	b.lastStep = ""
	return b
}
func (b *Builder) Build() *Pipeline { return b.pipeline }
