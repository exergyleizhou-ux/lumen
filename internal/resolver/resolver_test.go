package resolver

import (
	"testing"
)

func TestParseVersion(t *testing.T) {
	tests := []struct {
		input string
		major int
		minor int
		patch int
		pre   string
	}{
		{"1.2.3", 1, 2, 3, ""},
		{"0.0.1", 0, 0, 1, ""},
		{"2.0.0-alpha", 2, 0, 0, "alpha"},
		{"10.20.30", 10, 20, 30, ""},
	}

	for _, tc := range tests {
		v, err := ParseVersion(tc.input)
		if err != nil {
			t.Fatalf("parse %q: %v", tc.input, err)
		}
		if v.Major != tc.major || v.Minor != tc.minor || v.Patch != tc.patch || v.Pre != tc.pre {
			t.Fatalf("%q: got %d.%d.%d-%s, want %d.%d.%d-%s",
				tc.input, v.Major, v.Minor, v.Patch, v.Pre,
				tc.major, tc.minor, tc.patch, tc.pre)
		}
	}
}

func TestVersionCompare(t *testing.T) {
	a := Version{Major: 1, Minor: 2, Patch: 3}
	_ = a
	v1, _ := ParseVersion("1.0.0")
	v2, _ := ParseVersion("2.0.0")
	v3, _ := ParseVersion("1.0.0")

	if v1.Compare(v2) >= 0 {
		t.Fatal("expected 1.0.0 < 2.0.0")
	}
	if v2.Compare(v1) <= 0 {
		t.Fatal("expected 2.0.0 > 1.0.0")
	}
	if v1.Compare(v3) != 0 {
		t.Fatal("expected 1.0.0 == 1.0.0")
	}
}

func TestParseConstraint(t *testing.T) {
	tests := []struct {
		input string
		op    ConstraintOp
		ver   string
	}{
		{">=1.2.3", OpGTE, "1.2.3"},
		{"^2.0.0", OpCaret, "2.0.0"},
		{"~1.5.0", OpTilde, "1.5.0"},
		{"*", OpWildcard, ""},
		{"1.0.0", OpEQ, "1.0.0"},
	}

	for _, tc := range tests {
		c, err := ParseConstraint(tc.input)
		if err != nil {
			t.Fatalf("parse %q: %v", tc.input, err)
		}
		if c.Op != tc.op {
			t.Fatalf("%q: expected op %v, got %v", tc.input, tc.op, c.Op)
		}
		if tc.ver != "" && c.Version.String() != tc.ver {
			t.Fatalf("%q: expected version %s, got %s", tc.input, tc.ver, c.Version.String())
		}
	}
}

func TestConstraintSatisfies(t *testing.T) {
	v123, _ := ParseVersion("1.2.3")
	v130, _ := ParseVersion("1.3.0")
	v200, _ := ParseVersion("2.0.0")

	// >= 1.2.3
	cGTE, _ := ParseConstraint(">=1.2.3")
	if !cGTE.Satisfies(v123) {
		t.Fatal("1.2.3 should satisfy >=1.2.3")
	}
	if !cGTE.Satisfies(v130) {
		t.Fatal("1.3.0 should satisfy >=1.2.3")
	}
	if cGTE.Satisfies(Version{Major: 1, Minor: 0, Patch: 0}) {
		t.Fatal("1.0.0 should not satisfy >=1.2.3")
	}

	// ^1.2.3
	cCaret, _ := ParseConstraint("^1.2.3")
	if !cCaret.Satisfies(v130) {
		t.Fatal("1.3.0 should satisfy ^1.2.3")
	}
	if cCaret.Satisfies(v200) {
		t.Fatal("2.0.0 should NOT satisfy ^1.2.3")
	}

	// Wildcard.
	cWild, _ := ParseConstraint("*")
	if !cWild.Satisfies(v200) {
		t.Fatal("2.0.0 should satisfy *")
	}
}

func TestResolver_Resolve(t *testing.T) {
	reg := NewRegistry()

	reg.AddPackage(&PkgInfo{
		Name:     "dep-a",
		Versions: []Version{{Major: 1, Minor: 0, Patch: 0}, {Major: 1, Minor: 1, Patch: 0}},
	})
	reg.AddPackage(&PkgInfo{
		Name:     "dep-b",
		Versions: []Version{{Major: 2, Minor: 0, Patch: 0}},
	})

	r := NewResolver(reg, "newest")

	roots := []Dependency{
		{Name: "dep-a", Constraint: Constraint{Op: OpGTE, Version: Version{Major: 1, Minor: 0, Patch: 0}}},
		{Name: "dep-b", Constraint: Constraint{Op: OpWildcard}},
	}

	result := r.Resolve(roots)
	if !result.Success {
		t.Fatalf("expected success, got conflicts: %v", result.Conflicts)
	}
	if len(result.Resolved) != 2 {
		t.Fatalf("expected 2 resolved, got %d", len(result.Resolved))
	}

	// dep-a should resolve to 1.1.0 (newest).
	for _, e := range result.Resolved {
		if e.Name == "dep-a" {
			if e.Version.Minor != 1 {
				t.Fatalf("expected dep-a 1.1.0, got %s", e.Version)
			}
		}
	}
}

func TestResolver_Conflict(t *testing.T) {
	reg := NewRegistry()
	reg.AddPackage(&PkgInfo{
		Name:     "pkg",
		Versions: []Version{{Major: 1, Minor: 0, Patch: 0}},
	})

	r := NewResolver(reg, "newest")
	roots := []Dependency{
		{Name: "nonexistent", Constraint: Constraint{Op: OpGTE, Version: Version{Major: 1, Minor: 0, Patch: 0}}},
	}

	result := r.Resolve(roots)
	if result.Success {
		t.Fatal("expected failure due to missing package")
	}
}

func TestSATSolver(t *testing.T) {
	s := NewSATSolver()

	// Simple clause: (a OR b) AND (NOT a OR b)
	s.AddClause([]SATLiteral{
		{Var: SATVar{Name: "a", Version: Version{Major: 1}}, Negate: false},
		{Var: SATVar{Name: "b", Version: Version{Major: 1}}, Negate: false},
	})
	s.AddClause([]SATLiteral{
		{Var: SATVar{Name: "a", Version: Version{Major: 1}}, Negate: true},
		{Var: SATVar{Name: "b", Version: Version{Major: 1}}, Negate: false},
	})

	assignment, ok := s.Solve()
	if !ok {
		t.Fatal("expected SAT")
	}
	keyB := "b@1.0.0"
	if !assignment[keyB] {
		t.Fatal("expected b to be true")
	}
}

func TestLockFile_Diff(t *testing.T) {
	old := &LockFile{Entries: []LockEntry{
		{Name: "a", Version: Version{Major: 1, Minor: 0, Patch: 0}},
		{Name: "b", Version: Version{Major: 2, Minor: 0, Patch: 0}},
	}}
	new := &LockFile{Entries: []LockEntry{
		{Name: "a", Version: Version{Major: 1, Minor: 1, Patch: 0}},
		{Name: "c", Version: Version{Major: 3, Minor: 0, Patch: 0}},
	}}

	added, removed, changed := DiffLockFiles(old, new)
	if len(added) != 1 || added[0].Name != "c" {
		t.Fatalf("expected 'c' added, got %v", added)
	}
	if len(removed) != 1 || removed[0].Name != "b" {
		t.Fatalf("expected 'b' removed, got %v", removed)
	}
	if len(changed) != 1 || changed[0].Name != "a" {
		t.Fatalf("expected 'a' changed, got %v", changed)
	}
}
