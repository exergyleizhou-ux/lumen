package builtin

import (
	"fmt"
	"strings"
)

// applyReplace replaces exactly one occurrence of old with new in content.
//
// It enforces the edit-correctness invariants shared by edit_file and
// multi_edit:
//   - no-op guard: old == new is rejected (a pointless write + verify cycle).
//   - precise location: old must occur exactly once; 0 or >1 is an error.
//   - actionable not-found: when old isn't present verbatim, the error says
//     whether it WOULD match ignoring trailing-whitespace/indentation, which is
//     the most common cause of failed edits — without silently editing the wrong
//     span.
//
// It never mutates the file; callers persist the returned content.
func applyReplace(content, old, new string) (string, error) {
	if old == "" {
		return "", fmt.Errorf("old_string is required")
	}
	if old == new {
		return "", fmt.Errorf("no-op edit: old_string and new_string are identical")
	}

	switch n := strings.Count(content, old); {
	case n == 1:
		return strings.Replace(content, old, new, 1), nil
	case n > 1:
		return "", fmt.Errorf("old_string matches %d times (must be unique — add surrounding context)", n)
	default:
		return "", notFoundError(content, old)
	}
}

// notFoundError diagnoses why an exact match failed. If the text matches after
// normalizing trailing whitespace and indentation, it tells the caller so they
// can fix the whitespace rather than guessing.
func notFoundError(content, old string) error {
	switch n := countNormalized(content, old); {
	case n == 1:
		return fmt.Errorf("old_string not found exactly, but it matches once ignoring whitespace — copy the text verbatim including leading indentation and trailing spaces")
	case n > 1:
		return fmt.Errorf("old_string not found exactly (it matches %d places ignoring whitespace) — add surrounding context and match whitespace exactly", n)
	default:
		return fmt.Errorf("old_string not found")
	}
}

// countNormalized counts how many times old appears in content when both are
// whitespace-normalized (CRLF→LF, trailing whitespace stripped per line, and
// leading indentation collapsed). Used only for diagnostics, not for editing.
func countNormalized(content, old string) int {
	c := normalizeWS(content)
	o := normalizeWS(old)
	if o == "" {
		return 0
	}
	return strings.Count(c, o)
}

// normalizeWS lowers a string to a whitespace-insensitive form: LF line endings,
// each line trimmed of leading and trailing horizontal whitespace.
func normalizeWS(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = strings.Trim(ln, " \t")
	}
	return strings.Join(lines, "\n")
}

// editPair is one old→new replacement for multi_edit.
type editPair struct{ Old, New string }

// applySequentialEdits applies each edit in order, requiring each old_string to
// match exactly once at the moment it applies. It is all-or-nothing: any failure
// returns an error and no content (the caller persists only on success, so the
// file is left untouched on a mid-sequence failure — atomic multi-edit).
func applySequentialEdits(content string, edits []editPair) (string, error) {
	for i, e := range edits {
		next, err := applyReplace(content, e.Old, e.New)
		if err != nil {
			return "", fmt.Errorf("edit %d: %w", i, err)
		}
		content = next
	}
	return content, nil
}
