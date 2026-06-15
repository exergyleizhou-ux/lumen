package diff_engine

import (
	"fmt"
	"os"
	"strings"
)

type Op int

const (
	OpEqual Op = iota
	OpDelete
	OpInsert
)

type Edit struct {
	Op   Op     `json:"op"`
	Text string `json:"text"`
}

type Hunk struct {
	OldStart int    `json:"old_start"`
	OldLines int    `json:"old_lines"`
	NewStart int    `json:"new_start"`
	NewLines int    `json:"new_lines"`
	Edits    []Edit `json:"edits"`
}

type Diff struct {
	OldPath string `json:"old_path"`
	NewPath string `json:"new_path"`
	Hunks   []Hunk `json:"hunks"`
}

// Compute produces a line-level diff between two texts.
func Compute(old, new string) Diff {
	oldLines := splitLines(old)
	newLines := splitLines(new)
	edits := computeEdits(oldLines, newLines)
	return Diff{Hunks: groupIntoHunks(edits)}
}

func splitLines(s string) []string {
	if s == "" {
		return []string{""}
	}
	return strings.Split(s, "\n")
}

func computeEdits(a, b []string) []Edit {
	n, m := len(a), len(b)
	if n == 0 && m == 0 {
		return nil
	}
	var edits []Edit
	i, j := 0, 0
	for i < n || j < m {
		if i < n && j < m && a[i] == b[j] {
			edits = append(edits, Edit{Op: OpEqual, Text: a[i]})
			i++
			j++
		} else if j < m && (i >= n || !containsFrom(a, b[j], i)) {
			edits = append(edits, Edit{Op: OpInsert, Text: b[j]})
			j++
		} else {
			edits = append(edits, Edit{Op: OpDelete, Text: a[i]})
			i++
		}
	}
	return edits
}

func containsFrom(lines []string, target string, from int) bool {
	for k := from; k < len(lines); k++ {
		if lines[k] == target {
			return true
		}
	}
	return false
}

func groupIntoHunks(edits []Edit) []Hunk {
	var hunks []Hunk
	if len(edits) == 0 {
		return hunks
	}
	var current *Hunk
	oldLine, newLine := 1, 1

	flushHunk := func() {
		if current != nil && len(current.Edits) > 0 {
			hunks = append(hunks, *current)
			current = nil
		}
	}

	for _, e := range edits {
		if e.Op != OpEqual {
			if current == nil {
				current = &Hunk{OldStart: oldLine, NewStart: newLine}
			}
			current.Edits = append(current.Edits, e)
			if e.Op == OpDelete {
				current.OldLines++
			} else if e.Op == OpInsert {
				current.NewLines++
			}
		} else {
			if current != nil {
				// Add context line
				current.Edits = append(current.Edits, e)
				current.OldLines++
				current.NewLines++
				// Check if we have enough trailing context
				consecutiveEqual := 0
				for k := len(current.Edits) - 1; k >= 0; k-- {
					if current.Edits[k].Op == OpEqual {
						consecutiveEqual++
					} else {
						break
					}
				}
				if consecutiveEqual >= 3 {
					flushHunk()
				}
			}
		}
		if e.Op != OpInsert {
			oldLine++
		}
		if e.Op != OpDelete {
			newLine++
		}
	}
	flushHunk()
	return hunks
}

// UnifiedDiff produces a unified diff string.
func (d *Diff) UnifiedDiff() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "--- %s\n", d.OldPath)
	fmt.Fprintf(&sb, "+++ %s\n", d.NewPath)
	for _, h := range d.Hunks {
		fmt.Fprintf(&sb, "@@ -%d,%d +%d,%d @@\n", h.OldStart, h.OldLines, h.NewStart, h.NewLines)
		for _, e := range h.Edits {
			switch e.Op {
			case OpDelete:
				fmt.Fprintf(&sb, "-%s\n", e.Text)
			case OpInsert:
				fmt.Fprintf(&sb, "+%s\n", e.Text)
			default:
				fmt.Fprintf(&sb, " %s\n", e.Text)
			}
		}
	}
	return sb.String()
}

// SimpleDiff returns a +/- line-by-line diff.
func SimpleDiff(old, new string) string {
	d := Compute(old, new)
	var sb strings.Builder
	for _, h := range d.Hunks {
		for _, e := range h.Edits {
			switch e.Op {
			case OpDelete:
				sb.WriteString("- ")
			case OpInsert:
				sb.WriteString("+ ")
			default:
				sb.WriteString("  ")
			}
			sb.WriteString(e.Text + "\n")
		}
	}
	return sb.String()
}

// SideBySide produces a colorized terminal diff.
func SideBySide(old, new string, width int) string {
	if width <= 0 {
		width = 80
	}
	half := (width - 3) / 2
	oldL := splitLines(old)
	newL := splitLines(new)
	edits := computeEdits(oldL, newL)

	var sb strings.Builder
	oi, ni := 0, 0
	for _, e := range edits {
		switch e.Op {
		case OpEqual:
			l := ""
			if oi < len(oldL) {
				l = oldL[oi]
			}
			r := ""
			if ni < len(newL) {
				r = newL[ni]
			}
			sb.WriteString(fmt.Sprintf("  %-*s │ %-*s\n", half, truncStr(l, half), half, truncStr(r, half)))
			oi++
			ni++
		case OpDelete:
			l := ""
			if oi < len(oldL) {
				l = oldL[oi]
			}
			sb.WriteString(fmt.Sprintf("\x1b[31m- %-*s\x1b[0m │\n", half, truncStr(l, half)))
			oi++
		case OpInsert:
			r := ""
			if ni < len(newL) {
				r = newL[ni]
			}
			sb.WriteString(fmt.Sprintf("  %-*s │ \x1b[32m+ %-*s\x1b[0m\n", half, "", half, truncStr(r, half)))
			ni++
		}
	}
	return sb.String()
}

func truncStr(s string, max int) string {
	if len(s) > max {
		return s[:max-1] + "…"
	}
	return s
}

type Stats struct {
	LinesAdded   int `json:"lines_added"`
	LinesRemoved int `json:"lines_removed"`
}

func ComputeStats(edits []Edit) Stats {
	var s Stats
	for _, e := range edits {
		if e.Op == OpInsert {
			s.LinesAdded++
		} else if e.Op == OpDelete {
			s.LinesRemoved++
		}
	}
	return s
}

func DiffFiles(oldPath, newPath string) (Diff, error) {
	oldC, err := os.ReadFile(oldPath)
	if err != nil {
		return Diff{}, err
	}
	newC, err := os.ReadFile(newPath)
	if err != nil {
		return Diff{}, err
	}
	d := Compute(string(oldC), string(newC))
	d.OldPath = oldPath
	d.NewPath = newPath
	return d, nil
}
