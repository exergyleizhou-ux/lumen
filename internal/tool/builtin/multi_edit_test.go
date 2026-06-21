package builtin

import "testing"

func TestApplySequentialEdits(t *testing.T) {
	// Edits apply in order — edit 2 operates on edit 1's result.
	out, err := applySequentialEdits("a b c", []editPair{{"a", "X"}, {"X b", "Y"}})
	if err != nil || out != "Y c" {
		t.Fatalf("sequential apply = %q, err %v; want %q", out, err, "Y c")
	}

	// Empty new_string deletes.
	out, err = applySequentialEdits("hello world", []editPair{{"hello ", ""}})
	if err != nil || out != "world" {
		t.Fatalf("delete edit = %q, err %v; want %q", out, err, "world")
	}
}

// All-or-nothing: a non-matching edit anywhere in the sequence fails the whole
// batch (the caller persists only on success → the file is left untouched).
func TestApplySequentialEdits_Atomic(t *testing.T) {
	_, err := applySequentialEdits("a b c", []editPair{{"a", "X"}, {"nope", "Z"}})
	if err == nil {
		t.Fatal("a non-matching edit must fail the whole sequence (atomic)")
	}
}

// Each edit's uniqueness is enforced at the moment it applies.
func TestApplySequentialEdits_PerEditUniqueness(t *testing.T) {
	if _, err := applySequentialEdits("x x", []editPair{{"x", "y"}}); err == nil {
		t.Fatal("an ambiguous (multi-match) edit must fail")
	}
}
