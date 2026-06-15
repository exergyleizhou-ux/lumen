// Package policy provides a policy evaluation engine for Lumen agents.
// It supports Rego-inspired rule evaluation, condition matching, effect
// decisions (allow/deny), and policy bundles. Rules are evaluated against
// input contexts to make authorization and configuration decisions.
package policy

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Effect is a policy decision outcome.
type Effect string

const (
	EffectAllow Effect = "allow"
	EffectDeny  Effect = "deny"
	EffectWarn  Effect = "warn"
)

// Operator defines a comparison operator for conditions.
type Operator string

const (
	OpEqual        Operator = "eq"
	OpNotEqual     Operator = "ne"
	OpGreaterThan  Operator = "gt"
	OpLessThan     Operator = "lt"
	OpContains     Operator = "contains"
	OpNotContains  Operator = "not_contains"
	OpIn           Operator = "in"
	OpNotIn        Operator = "not_in"
	OpMatches      Operator = "matches"
	OpExists       Operator = "exists"
	OpNotExists    Operator = "not_exists"
)

// Condition is a single rule condition.
type Condition struct {
	Field    string      `json:"field"`
	Operator Operator    `json:"operator"`
	Value    any         `json:"value"`
}

// Rule is a policy rule with conditions and an effect.
type Rule struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Conditions  []Condition `json:"conditions"`
	Effect      Effect      `json:"effect"`
	Priority    int         `json:"priority"`
	Scope       string      `json:"scope,omitempty"`
}

// Policy is a named collection of rules.
type Policy struct {
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	Description string    `json:"description"`
	Rules       []Rule    `json:"rules"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Decision is the result of evaluating a policy.
type Decision struct {
	PolicyName  string    `json:"policy_name"`
	RuleName    string    `json:"rule_name,omitempty"`
	Effect      Effect    `json:"effect"`
	Reason      string    `json:"reason"`
	EvaluatedAt time.Time `json:"evaluated_at"`
	Duration    time.Duration `json:"duration"`
}

// Engine evaluates policies against input contexts.
type Engine struct {
	mu       sync.RWMutex
	policies map[string]*Policy
	auditLog []Decision
	maxAudit int
}

// NewEngine creates a policy engine.
func NewEngine() *Engine {
	return &Engine{policies: map[string]*Policy{}, maxAudit: 1000}
}

// Register adds a policy.
func (e *Engine) Register(p *Policy) {
	e.mu.Lock(); defer e.mu.Unlock()
	p.CreatedAt = time.Now()
	p.UpdatedAt = time.Now()
	e.policies[p.Name] = p
}

// Remove deletes a policy.
func (e *Engine) Remove(name string) {
	e.mu.Lock(); defer e.mu.Unlock()
	delete(e.policies, name)
}

// Evaluate checks a policy against an input context.
func (e *Engine) Evaluate(policyName string, input map[string]any) (*Decision, error) {
	e.mu.RLock()
	p, ok := e.policies[policyName]
	e.mu.RUnlock()
	if !ok { return nil, fmt.Errorf("policy %q not found", policyName) }

	start := time.Now()

	// Sort rules by priority (lower number = higher priority)
	rules := make([]Rule, len(p.Rules))
	copy(rules, p.Rules)
	sort.Slice(rules, func(i, j int) bool { return rules[i].Priority < rules[j].Priority })

	for _, rule := range rules {
		if matchConditions(rule.Conditions, input) {
			d := &Decision{
				PolicyName: p.Name, RuleName: rule.Name, Effect: rule.Effect,
				Reason: fmt.Sprintf("matched rule %q (priority %d)", rule.Name, rule.Priority),
				EvaluatedAt: time.Now(), Duration: time.Since(start),
			}
			e.auditDecision(d)
			return d, nil
		}
	}

	// No matching rule — implicit deny
	d := &Decision{
		PolicyName: p.Name, Effect: EffectDeny,
		Reason: "no matching rule (implicit deny)", EvaluatedAt: time.Now(), Duration: time.Since(start),
	}
	e.auditDecision(d)
	return d, nil
}

// EvaluateAll evaluates all registered policies and collects decisions.
func (e *Engine) EvaluateAll(input map[string]any) ([]Decision, error) {
	e.mu.RLock()
	names := make([]string, 0, len(e.policies))
	for n := range e.policies { names = append(names, n) }
	e.mu.RUnlock()
	sort.Strings(names)

	var decisions []Decision
	for _, name := range names {
		d, err := e.Evaluate(name, input)
		if err != nil { return decisions, err }
		decisions = append(decisions, *d)
	}
	return decisions, nil
}

// AllowAll returns true only if all policies evaluate to allow.
func (e *Engine) AllowAll(input map[string]any) bool {
	decisions, err := e.EvaluateAll(input)
	if err != nil { return false }
	for _, d := range decisions {
		if d.Effect != EffectAllow { return false }
	}
	return true
}

// AuditLog returns recent decisions.
func (e *Engine) AuditLog() []Decision {
	e.mu.RLock(); defer e.mu.RUnlock()
	out := make([]Decision, len(e.auditLog))
	copy(out, e.auditLog)
	return out
}

func (e *Engine) auditDecision(d *Decision) {
	e.mu.Lock(); defer e.mu.Unlock()
	e.auditLog = append(e.auditLog, *d)
	if len(e.auditLog) > e.maxAudit { e.auditLog = e.auditLog[1:] }
}

// Policies returns registered policy names.
func (e *Engine) Policies() []string {
	e.mu.RLock(); defer e.mu.RUnlock()
	var out []string
	for n := range e.policies { out = append(out, n) }
	sort.Strings(out)
	return out
}

func matchConditions(conditions []Condition, input map[string]any) bool {
	for _, c := range conditions {
		if !matchCondition(c, input) { return false }
	}
	return len(conditions) > 0
}

func matchCondition(c Condition, input map[string]any) bool {
	actual := getField(input, c.Field)

	switch c.Operator {
	case OpEqual:
		return fmt.Sprint(actual) == fmt.Sprint(c.Value)
	case OpNotEqual:
		return fmt.Sprint(actual) != fmt.Sprint(c.Value)
	case OpExists:
		return actual != nil
	case OpNotExists:
		return actual == nil
	case OpContains:
		switch v := actual.(type) {
		case string: return strings.Contains(v, fmt.Sprint(c.Value))
		case []any:
			for _, item := range v { if fmt.Sprint(item) == fmt.Sprint(c.Value) { return true } }
			return false
		default: return false
		}
	case OpNotContains:
		switch v := actual.(type) {
		case string: return !strings.Contains(v, fmt.Sprint(c.Value))
		case []any:
			for _, item := range v { if fmt.Sprint(item) == fmt.Sprint(c.Value) { return false } }
			return true
		default: return true
		}
	case OpIn:
		list, ok := c.Value.([]any)
		if !ok { return false }
		for _, item := range list { if fmt.Sprint(actual) == fmt.Sprint(item) { return true } }
		return false
	case OpNotIn:
		list, ok := c.Value.([]any)
		if !ok { return true }
		for _, item := range list { if fmt.Sprint(actual) == fmt.Sprint(item) { return false } }
		return true
	case OpGreaterThan:
		return compareNums(actual, c.Value) > 0
	case OpLessThan:
		return compareNums(actual, c.Value) < 0
	case OpMatches:
		return strings.Contains(fmt.Sprint(actual), fmt.Sprint(c.Value))
	default:
		return false
	}
}

func getField(input map[string]any, path string) any {
	parts := strings.Split(path, ".")
	cur := any(input)
	for _, p := range parts {
		m, ok := cur.(map[string]any)
		if !ok { return nil }
		cur = m[p]
	}
	return cur
}

func compareNums(a, b any) int {
	af := toFloat64(a)
	bf := toFloat64(b)
	if af > bf { return 1 }
	if af < bf { return -1 }
	return 0
}

func toFloat64(v any) float64 {
	switch n := v.(type) {
	case int: return float64(n)
	case int64: return float64(n)
	case float64: return n
	default: return 0
	}
}

// ── Policy Bundle ─────────────────────────────────────────

// Bundle is a collection of policies that deploy together.
type Bundle struct {
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	Policies    []Policy  `json:"policies"`
	Hash        string    `json:"hash"`
	CreatedAt   time.Time `json:"created_at"`
}

// BundleManager manages policy bundles.
type BundleManager struct {
	mu      sync.Mutex
	bundles map[string]*Bundle
	engine  *Engine
}

// NewBundleManager creates a bundle manager.
func NewBundleManager(engine *Engine) *BundleManager {
	return &BundleManager{bundles: map[string]*Bundle{}, engine: engine}
}

// Load registers all policies from a bundle.
func (bm *BundleManager) Load(bundle *Bundle) error {
	bm.mu.Lock(); defer bm.mu.Unlock()
	for i := range bundle.Policies {
		p := &bundle.Policies[i]
		bm.engine.Register(p)
	}
	bundle.CreatedAt = time.Now()
	bm.bundles[bundle.Name] = bundle
	return nil
}

// Unload removes all policies from a bundle.
func (bm *BundleManager) Unload(name string) error {
	bm.mu.Lock()
	b, ok := bm.bundles[name]
	bm.mu.Unlock()
	if !ok { return fmt.Errorf("bundle %q not found", name) }
	for _, p := range b.Policies { bm.engine.Remove(p.Name) }
	bm.mu.Lock()
	delete(bm.bundles, name)
	bm.mu.Unlock()
	return nil
}

// Bundles returns loaded bundle names.
func (bm *BundleManager) Bundles() []string {
	bm.mu.Lock(); defer bm.mu.Unlock()
	var out []string
	for n := range bm.bundles { out = append(out, n) }
	sort.Strings(out)
	return out
}

// ── Policy Formatter ─────────────────────────────────────

// FormatDecision formats a policy decision.
func FormatDecision(d *Decision) string {
	icon := "✅"; if d.Effect == EffectDeny { icon = "🔴" } else if d.Effect == EffectWarn { icon = "⚠️" }
	return fmt.Sprintf("%s [%s] %s — %s (%v)", icon, d.Effect, d.PolicyName, d.Reason, d.Duration)
}

// FormatPolicy formats a policy definition.
func FormatPolicy(p *Policy) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Policy: %s v%s\n", p.Name, p.Version)
	fmt.Fprintf(&sb, "  Description: %s\n", p.Description)
	fmt.Fprintf(&sb, "  Rules: %d\n", len(p.Rules))
	for _, r := range p.Rules {
		fmt.Fprintf(&sb, "    [p%d] %s → %s\n", r.Priority, r.Name, r.Effect)
		for _, c := range r.Conditions {
			fmt.Fprintf(&sb, "      %s %s %v\n", c.Field, c.Operator, c.Value)
		}
	}
	return sb.String()
}

// ── Built-in Policies ────────────────────────────────────

// DefaultSecurityPolicy returns a basic security policy.
func DefaultSecurityPolicy() *Policy {
	return &Policy{
		Name: "security.default", Version: "1.0",
		Description: "Default security policy for Lumen agents",
		Rules: []Rule{
			{Name: "deny-shell-exec", Description: "Block shell execution", Conditions: []Condition{
				{Field: "action", Operator: OpContains, Value: "exec"},
			}, Effect: EffectDeny, Priority: 0},
			{Name: "allow-read", Description: "Allow file reads in workspace", Conditions: []Condition{
				{Field: "action", Operator: OpIn, Value: []any{"read_file", "glob", "grep", "ls"}},
			}, Effect: EffectAllow, Priority: 10},
			{Name: "allow-tools", Description: "Allow standard tools", Conditions: []Condition{
				{Field: "action", Operator: OpExists},
			}, Effect: EffectAllow, Priority: 100},
		},
	}
}
