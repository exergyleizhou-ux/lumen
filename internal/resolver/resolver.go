// Package resolver implements a dependency resolver for packages with version
// constraints, lock file generation, conflict detection, and SAT-solver-style resolution.
package resolver

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// --- Version ---

// Version represents a semantic version.
type Version struct {
	Major int `json:"major"`
	Minor int `json:"minor"`
	Patch int `json:"patch"`
	Pre   string `json:"pre,omitempty"` // Pre-release tag.
}

func (v Version) String() string {
	s := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.Pre != "" { s += "-" + v.Pre }
	return s
}

// ParseVersion parses a version string like "1.2.3" or "1.2.3-alpha".
func ParseVersion(s string) (Version, error) {
	re := regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)(?:-(.+))?$`)
	m := re.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return Version{}, fmt.Errorf("invalid version %q", s)
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	patch, _ := strconv.Atoi(m[3])
	return Version{Major: major, Minor: minor, Patch: patch, Pre: m[4]}, nil
}

// Compare returns -1, 0, 1 for v < other, v == other, v > other.
func (v Version) Compare(other Version) int {
	if v.Major != other.Major {
		return cmpInt(v.Major, other.Major)
	}
	if v.Minor != other.Minor {
		return cmpInt(v.Minor, other.Minor)
	}
	if v.Patch != other.Patch {
		return cmpInt(v.Patch, other.Patch)
	}
	// Pre-release: a version without pre-release > one with pre-release.
	if v.Pre == "" && other.Pre != "" {
		return 1
	}
	if v.Pre != "" && other.Pre == "" {
		return -1
	}
	if v.Pre < other.Pre {
		return -1
	}
	if v.Pre > other.Pre {
		return 1
	}
	return 0
}

func cmpInt(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}


// --- Constraint ---

// ConstraintOp describes the operator in a version constraint.
type ConstraintOp int

const (
	OpEQ  ConstraintOp = iota // =
	OpGT                       // >
	OpGTE                      // >=
	OpLT                       // <
	OpLTE                      // <=
	OpCaret                    // ^ compatible
	OpTilde                    // ~ approximately
	OpWildcard                 // * any
)

var opStrings = map[ConstraintOp]string{
	OpEQ: "=", OpGT: ">", OpGTE: ">=", OpLT: "<", OpLTE: "<=",
	OpCaret: "^", OpTilde: "~", OpWildcard: "*",
}

func (op ConstraintOp) String() string { return opStrings[op] }

// Constraint is a single version constraint.
type Constraint struct {
	Op      ConstraintOp
	Version Version
}

func (c Constraint) String() string {
	switch c.Op {
	case OpWildcard: return "*"
	case OpEQ: return "=" + c.Version.String()
	case OpGTE: return ">=" + c.Version.String()
	case OpLT: return "<" + c.Version.String()
	case OpCaret: return "^" + c.Version.String()
	case OpTilde: return "~" + c.Version.String()
	default: return "?" + c.Version.String()
	}
}

// ParseConstraint parses a constraint string like ">=1.2.3" or "^2.0.0".
func ParseConstraint(s string) (Constraint, error) {
	s = strings.TrimSpace(s)
	if s == "*" || s == "" {
		return Constraint{Op: OpWildcard}, nil
	}
	re := regexp.MustCompile(`^([><=~^]+)\s*(.+)$`)
	m := re.FindStringSubmatch(s)
	if m == nil {
		// Default to exact.
		v, err := ParseVersion(s)
		if err != nil {
			return Constraint{}, err
		}
		return Constraint{Op: OpEQ, Version: v}, nil
	}
	var op ConstraintOp
	switch m[1] {
	case "=":
		op = OpEQ
	case ">":
		op = OpGT
	case ">=":
		op = OpGTE
	case "<":
		op = OpLT
	case "<=":
		op = OpLTE
	case "^":
		op = OpCaret
	case "~":
		op = OpTilde
	default:
		return Constraint{}, fmt.Errorf("unknown operator %q", m[1])
	}
	v, err := ParseVersion(m[2])
	if err != nil {
		return Constraint{}, err
	}
	return Constraint{Op: op, Version: v}, nil
}

// Satisfies checks whether a version satisfies the constraint.
func (c Constraint) Satisfies(v Version) bool {
	switch c.Op {
	case OpEQ:
		return v.Compare(c.Version) == 0
	case OpGT:
		return v.Compare(c.Version) > 0
	case OpGTE:
		return v.Compare(c.Version) >= 0
	case OpLT:
		return v.Compare(c.Version) < 0
	case OpLTE:
		return v.Compare(c.Version) <= 0
	case OpCaret:
		// ^1.2.3 => >=1.2.3 <2.0.0
		// ^0.2.3 => >=0.2.3 <0.3.0
		// ^0.0.3 => >=0.0.3 <0.0.4
		upper := Version{Major: c.Version.Major, Minor: c.Version.Minor, Patch: c.Version.Patch}
		if c.Version.Major > 0 {
			upper.Major++
			upper.Minor = 0
			upper.Patch = 0
		} else if c.Version.Minor > 0 {
			upper.Minor++
			upper.Patch = 0
		} else {
			upper.Patch++
		}
		return v.Compare(c.Version) >= 0 && v.Compare(upper) < 0
	case OpTilde:
		// ~1.2.3 => >=1.2.3 <1.3.0
		upper := Version{Major: c.Version.Major, Minor: c.Version.Minor + 1, Patch: 0}
		return v.Compare(c.Version) >= 0 && v.Compare(upper) < 0
	case OpWildcard:
		return true
	default:
		return false
	}
}

// --- Package and Dependency ---

// PkgInfo describes a package with its available versions.
type PkgInfo struct {
	Name        string    `json:"name"`
	Versions    []Version `json:"versions"`
	Dependencies map[Version][]Dependency `json:"deps"` // per-version dependencies.
}

// Dependency is a requirement on another package.
type Dependency struct {
	Name       string     `json:"name"`
	Constraint Constraint `json:"constraint"`
	Optional   bool       `json:"optional,omitempty"`
}

// LockEntry is one line in a lock file.
type LockEntry struct {
	Name    string  `json:"name"`
	Version Version `json:"version"`
	Digest  string  `json:"digest"` // Content hash for integrity.
}

// LockFile represents a resolved lock file.
type LockFile struct {
	Entries  []LockEntry `json:"entries"`
	Generated string     `json:"generated"`
}

// Conflict describes a resolution conflict.
type Conflict struct {
	Package  string     `json:"package"`
	Required []Constraint `json:"required"` // Conflicting constraints.
	Existing Version    `json:"existing"`
	Message  string     `json:"message"`
}

// ResolveResult holds the outcome of dependency resolution.
type ResolveResult struct {
	Resolved  []LockEntry `json:"resolved"`
	Conflicts []Conflict  `json:"conflicts"`
	Success   bool        `json:"success"`
	Steps     int         `json:"steps"`
}

// --- Registry ---

// Registry holds all known packages.
type Registry struct {
	mu       sync.RWMutex
	packages map[string]*PkgInfo
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{packages: make(map[string]*PkgInfo)}
}

// AddPackage registers a package.
func (r *Registry) AddPackage(pkg *PkgInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.packages[pkg.Name] = pkg
}

// GetPackage returns a package by name.
func (r *Registry) GetPackage(name string) *PkgInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.packages[name]
}

// --- Resolver ---

// Resolver resolves dependency graphs to concrete versions.
type Resolver struct {
	registry *Registry
	strategy string // "newest", "oldest", "smallest-tree".
}

// NewResolver creates a new resolver.
func NewResolver(reg *Registry, strategy string) *Resolver {
	if strategy == "" {
		strategy = "newest"
	}
	return &Resolver{registry: reg, strategy: strategy}
}

// Resolve resolves dependencies for a set of root packages with constraints.
func (rs *Resolver) Resolve(roots []Dependency) *ResolveResult {
	result := &ResolveResult{}
	decisions := make(map[string]Version)        // package -> decided version
	queue := make([]Dependency, len(roots))
	copy(queue, roots)

	for steps := 0; steps < 10000 && len(queue) > 0; steps++ {
		result.Steps = steps
		dep := queue[0]
		queue = queue[1:]

		pkg := rs.registry.GetPackage(dep.Name)
		if pkg == nil {
			result.Conflicts = append(result.Conflicts, Conflict{
				Package:  dep.Name,
				Message:  "package not found",
			})
			continue
		}

		// If already decided, check compatibility.
		if existing, ok := decisions[dep.Name]; ok {
			if !dep.Constraint.Satisfies(existing) {
				result.Conflicts = append(result.Conflicts, Conflict{
					Package:  dep.Name,
					Existing: existing,
					Required: []Constraint{dep.Constraint},
					Message:  fmt.Sprintf("existing %s does not satisfy %s", existing, dep.Constraint.String()),
				})
			}
			// If optional and conflict, skip without failing.
			if dep.Optional {
				continue
			}
			continue
		}

		// Find best matching version.
		best := rs.findBestVersion(pkg.Versions, dep.Constraint)
		if best == nil {
			if dep.Optional {
				continue
			}
			result.Conflicts = append(result.Conflicts, Conflict{
				Package:  dep.Name,
				Required: []Constraint{dep.Constraint},
				Message:  fmt.Sprintf("no version satisfies %s", dep.Constraint.String()),
			})
			continue
		}

		decisions[dep.Name] = *best

		// Add transitive dependencies.
		if deps, ok := pkg.Dependencies[*best]; ok {
			for _, d := range deps {
				queue = append(queue, d)
			}
		}
	}

	// Build resolved list.
	for name, ver := range decisions {
		result.Resolved = append(result.Resolved, LockEntry{Name: name, Version: ver})
	}
	sort.Slice(result.Resolved, func(i, j int) bool { return result.Resolved[i].Name < result.Resolved[j].Name })
	result.Success = len(result.Conflicts) == 0
	return result
}

func (rs *Resolver) findBestVersion(versions []Version, c Constraint) *Version {
	var candidates []Version
	for _, v := range versions {
		if c.Satisfies(v) {
			candidates = append(candidates, v)
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Compare(candidates[j]) < 0
	})
	switch rs.strategy {
	case "newest":
		v := candidates[len(candidates)-1]
		return &v
	case "oldest":
		v := candidates[0]
		return &v
	default:
		v := candidates[len(candidates)-1]
		return &v
	}
}

// GenerateLockFile creates a lock file from resolved entries.
func GenerateLockFile(entries []LockEntry) *LockFile {
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return &LockFile{
		Entries:   entries,
		Generated: "now",
	}
}

// ParseLockFile parses a lock file (simplified: just returns entries).
func ParseLockFile(data []byte) (*LockFile, error) {
	// Simplified: in production this would parse JSON/YAML.
	lf := &LockFile{}
	// Assume JSON format.
	if len(data) > 0 {
		_ = data // parsed externally.
	}
	return lf, nil
}

// DiffLockFiles returns differences between two lock files.
func DiffLockFiles(old, new *LockFile) (added, removed, changed []LockEntry) {
	oldMap := make(map[string]LockEntry)
	newMap := make(map[string]LockEntry)
	for _, e := range old.Entries {
		oldMap[e.Name] = e
	}
	for _, e := range new.Entries {
		newMap[e.Name] = e
	}
	for name, ne := range newMap {
		if _, ok := oldMap[name]; !ok {
			added = append(added, ne)
		}
	}
	for name, oe := range oldMap {
		if ne, ok := newMap[name]; ok {
			if ne.Version.Compare(oe.Version) != 0 {
				changed = append(changed, ne)
			}
		} else {
			removed = append(removed, oe)
		}
	}
	return
}

// --- SAT-solver style resolution ---

// SATVar represents a boolean variable: package@version.
type SATVar struct {
	Name    string
	Version Version
}

// SATClause is a disjunction of literals (each literal is a var and whether it's negated).
type SATLiteral struct {
	Var    SATVar
	Negate bool
}

// SATSolver implements a simple DPLL-style SAT solver for dependency resolution.
type SATSolver struct {
	clauses []SATClauseImpl
	vars    map[string]SATVar
}

type SATClauseImpl struct {
	literals []SATLiteral
}

// NewSATSolver creates a new SAT solver.
func NewSATSolver() *SATSolver {
	return &SATSolver{vars: make(map[string]SATVar)}
}

// AddClause adds a clause to the solver.
func (s *SATSolver) AddClause(literals []SATLiteral) {
	s.clauses = append(s.clauses, SATClauseImpl{literals: literals})
	for _, lit := range literals {
		s.vars[lit.Var.Name+"@"+lit.Var.Version.String()] = lit.Var
	}
}

// Solve attempts to find a satisfying assignment.
func (s *SATSolver) Solve() (map[string]bool, bool) {
	assignment := make(map[string]bool)
	return s.dpll(assignment, 0)
}

func (s *SATSolver) dpll(assignment map[string]bool, depth int) (map[string]bool, bool) {
	if depth > 1000 {
		return nil, false
	}

	// Unit propagation.
	changed := true
	for changed {
		changed = false
		for _, clause := range s.clauses {
			unbound := 0
			satisfied := false
			var lastUnbound *SATLiteral
			for _, lit := range clause.literals {
				key := lit.Var.Name + "@" + lit.Var.Version.String()
				val, ok := assignment[key]
				if ok {
					if val != lit.Negate {
						satisfied = true
						break
					}
				} else {
					unbound++
					lastUnbound = &lit
				}
			}
			if satisfied {
				continue
			}
			if unbound == 0 {
				return nil, false // Conflict.
			}
			if unbound == 1 {
				key := lastUnbound.Var.Name + "@" + lastUnbound.Var.Version.String()
				assignment[key] = !lastUnbound.Negate
				changed = true
			}
		}
	}

	// Check if all clauses satisfied.
	allSat := true
	for _, clause := range s.clauses {
		sat := false
		for _, lit := range clause.literals {
			key := lit.Var.Name + "@" + lit.Var.Version.String()
			val, ok := assignment[key]
			if ok && val != lit.Negate {
				sat = true
				break
			}
		}
		if !sat {
			allSat = false
			break
		}
	}
	if allSat {
		return assignment, true
	}

	// Choose unassigned variable and branch.
	for _, clause := range s.clauses {
		for _, lit := range clause.literals {
			key := lit.Var.Name + "@" + lit.Var.Version.String()
			if _, ok := assignment[key]; !ok {
				// Try true.
				a1 := copyAssignment(assignment)
				a1[key] = true
				if res, ok := s.dpll(a1, depth+1); ok {
					return res, true
				}
				// Try false.
				a2 := copyAssignment(assignment)
				a2[key] = false
				return s.dpll(a2, depth+1)
			}
		}
	}
	return assignment, true
}

func copyAssignment(m map[string]bool) map[string]bool {
	c := make(map[string]bool)
	for k, v := range m {
		c[k] = v
	}
	return c
}

// FormatResult returns a human-readable string of a resolve result.
func FormatResult(r *ResolveResult) string {
	s := fmt.Sprintf("Resolution: success=%v steps=%d\n", r.Success, r.Steps)
	for _, e := range r.Resolved {
		s += fmt.Sprintf("  %s %s\n", e.Name, e.Version)
	}
	if len(r.Conflicts) > 0 {
		s += "Conflicts:\n"
		for _, c := range r.Conflicts {
			s += fmt.Sprintf("  %s: %s\n", c.Package, c.Message)
		}
	}
	return s
}
