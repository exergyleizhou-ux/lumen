// Package selector provides Kubernetes-style label selectors for filtering
// and matching resources. Supports equality, set-based, and composite
// selectors for querying agent resources, deployments, and sessions.
package selector

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

type Operator string

const (
	OpEq        Operator = "="
	OpNeq       Operator = "!="
	OpIn        Operator = "in"
	OpNotIn     Operator = "notin"
	OpExists    Operator = "exists"
	OpNotExists Operator = "notexists"
)

type Requirement struct {
	Key    string
	Op     Operator
	Values []string
}
type Selector struct{ requirements []Requirement }

func Parse(s string) (*Selector, error) {
	parts := strings.Split(s, ",")
	var reqs []Requirement
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		for _, op := range []string{"!=", "=", " notin ", " in "} {
			if strings.Contains(p, op) {
				kv := strings.SplitN(p, op, 2)
				key := strings.TrimSpace(kv[0])
				vals := strings.Split(strings.TrimSpace(kv[1]), ",")
				for i := range vals {
					vals[i] = strings.TrimSpace(vals[i])
				}
				switch op {
				case "=":
					reqs = append(reqs, Requirement{Key: key, Op: OpEq, Values: vals})
				case "!=":
					reqs = append(reqs, Requirement{Key: key, Op: OpNeq, Values: vals})
				case " in ":
					reqs = append(reqs, Requirement{Key: key, Op: OpIn, Values: vals})
				case " notin ":
					reqs = append(reqs, Requirement{Key: key, Op: OpNotIn, Values: vals})
				}
				goto next
			}
		}
		reqs = append(reqs, Requirement{Key: p, Op: OpExists})
	next:
	}
	return &Selector{requirements: reqs}, nil
}
func NewSelector(reqs ...Requirement) *Selector { return &Selector{requirements: reqs} }
func (s *Selector) Matches(labels map[string]string) bool {
	for _, r := range s.requirements {
		val, ok := labels[r.Key]
		switch r.Op {
		case OpEq:
			if !ok || val != r.Values[0] {
				return false
			}
		case OpNeq:
			if ok && val == r.Values[0] {
				return false
			}
		case OpIn:
			found := false
			for _, v := range r.Values {
				if ok && val == v {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		case OpNotIn:
			for _, v := range r.Values {
				if ok && val == v {
					return false
				}
			}
		case OpExists:
			if !ok {
				return false
			}
		case OpNotExists:
			if ok {
				return false
			}
		}
	}
	return true
}
func (s *Selector) String() string {
	var parts []string
	for _, r := range s.requirements {
		switch r.Op {
		case OpEq:
			parts = append(parts, fmt.Sprintf("%s=%s", r.Key, r.Values[0]))
		case OpNeq:
			parts = append(parts, fmt.Sprintf("%s!=%s", r.Key, r.Values[0]))
		case OpIn:
			parts = append(parts, fmt.Sprintf("%s in (%s)", r.Key, strings.Join(r.Values, ",")))
		case OpNotIn:
			parts = append(parts, fmt.Sprintf("%s notin (%s)", r.Key, strings.Join(r.Values, ",")))
		case OpExists:
			parts = append(parts, r.Key)
		case OpNotExists:
			parts = append(parts, "!"+r.Key)
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

type Filter struct {
	mu     sync.Mutex
	items  []map[string]string
	labels map[string]map[string]string
}

func NewFilter() *Filter { return &Filter{labels: map[string]map[string]string{}} }
func (f *Filter) Add(id string, labels map[string]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.labels[id] = labels
	f.items = append(f.items, labels)
}
func (f *Filter) Remove(id string) { f.mu.Lock(); defer f.mu.Unlock(); delete(f.labels, id) }
func (f *Filter) Match(sel *Selector) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for id, labels := range f.labels {
		if sel.Matches(labels) {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}
func (f *Filter) Format() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var sb strings.Builder
	fmt.Fprintf(&sb, "Label Filter (%d items):\n%s\n\n", len(f.labels), strings.Repeat("─", 40))
	ids := make([]string, 0, len(f.labels))
	for id := range f.labels {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		labels := f.labels[id]
		fmt.Fprintf(&sb, "  %s: %v\n", id, labels)
	}
	return sb.String()
}
