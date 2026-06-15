// Package diffengine computes semantic diffs between documents, code
// structures, and agent outputs. It supports line diff, word diff,
// structured JSON diff with path tracking, and AST-aware code diff.
package diffengine

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// DiffOp is a diff operation.
type DiffOp string

const (
	OpAdd    DiffOp = "add"
	OpRemove DiffOp = "remove"
	OpChange DiffOp = "change"
	OpSame   DiffOp = "same"
)

// DiffLine is one line of a diff.
type DiffLine struct {
	Op         DiffOp `json:"op"`
	OldLine    string `json:"old_line,omitempty"`
	NewLine    string `json:"new_line,omitempty"`
	OldLineNum int    `json:"old_line_num"`
	NewLineNum int    `json:"new_line_num"`
}

// DiffResult is a complete diff.
type DiffResult struct {
	OldFile string     `json:"old_file"`
	NewFile string     `json:"new_file"`
	Lines   []DiffLine `json:"lines"`
	Added   int        `json:"added"`
	Removed int        `json:"removed"`
	Changed int        `json:"changed"`
}

// Engine computes diffs.
type Engine struct{ mu sync.Mutex }

// NewEngine creates a diff engine.
func NewEngine() *Engine { return &Engine{} }

// LineDiff computes a line-by-line diff using LCS.
func (e *Engine) LineDiff(oldText, newText string) *DiffResult {
	oldLines := strings.Split(oldText, "\n")
	newLines := strings.Split(newText, "\n")
	la, lb := len(oldLines), len(newLines)

	dp := make([][]int, la+1)
	for i := range dp {
		dp[i] = make([]int, lb+1)
	}
	for i := 1; i <= la; i++ {
		for j := 1; j <= lb; j++ {
			if oldLines[i-1] == newLines[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else {
				if dp[i-1][j] > dp[i][j-1] {
					dp[i][j] = dp[i-1][j]
				} else {
					dp[i][j] = dp[i][j-1]
				}
			}
		}
	}

	result := &DiffResult{OldFile: "old", NewFile: "new"}
	i, j := la, lb
	var raw []DiffLine
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && oldLines[i-1] == newLines[j-1] {
			raw = append(raw, DiffLine{Op: OpSame, OldLine: oldLines[i-1], NewLine: oldLines[i-1], OldLineNum: i, NewLineNum: j})
			i--
			j--
		} else if j > 0 && (i == 0 || dp[i][j-1] >= dp[i-1][j]) {
			raw = append(raw, DiffLine{Op: OpAdd, NewLine: newLines[j-1], NewLineNum: j})
			j--
		} else {
			raw = append(raw, DiffLine{Op: OpRemove, OldLine: oldLines[i-1], OldLineNum: i})
			i--
		}
	}
	for k := len(raw) - 1; k >= 0; k-- {
		result.Lines = append(result.Lines, raw[k])
	}
	for _, l := range result.Lines {
		switch l.Op {
		case OpAdd:
			result.Added++
		case OpRemove:
			result.Removed++
		case OpChange:
			result.Changed++
		}
	}
	return result
}

// WordDiff computes word-level changes within changed lines.
func (e *Engine) WordDiff(lineA, lineB string) []DiffLine {
	wordsA := strings.Fields(lineA)
	wordsB := strings.Fields(lineB)
	la, lb := len(wordsA), len(wordsB)
	dp := make([][]int, la+1)
	for i := range dp {
		dp[i] = make([]int, lb+1)
	}
	for i := 1; i <= la; i++ {
		for j := 1; j <= lb; j++ {
			if wordsA[i-1] == wordsB[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else {
				if dp[i-1][j] > dp[i][j-1] {
					dp[i][j] = dp[i-1][j]
				} else {
					dp[i][j] = dp[i][j-1]
				}
			}
		}
	}

	var raw []DiffLine
	i, j := la, lb
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && wordsA[i-1] == wordsB[j-1] {
			raw = append(raw, DiffLine{Op: OpSame, OldLine: wordsA[i-1]})
			i--
			j--
		} else if j > 0 && (i == 0 || dp[i][j-1] >= dp[i-1][j]) {
			raw = append(raw, DiffLine{Op: OpAdd, NewLine: wordsB[j-1]})
			j--
		} else {
			raw = append(raw, DiffLine{Op: OpRemove, OldLine: wordsA[i-1]})
			i--
		}
	}
	var out []DiffLine
	for k := len(raw) - 1; k >= 0; k-- {
		out = append(out, raw[k])
	}
	return out
}

// ── JSON Diff ────────────────────────────────────────────

// JSONPath is a JSON path expression.
type JSONPath string

// JSONChange is a single JSON structural change.
type JSONChange struct {
	Path     JSONPath `json:"path"`
	Op       DiffOp   `json:"op"`
	OldValue any      `json:"old_value,omitempty"`
	NewValue any      `json:"new_value,omitempty"`
}

// JSONDiffResult is the result of comparing two JSON documents.
type JSONDiffResult struct {
	Changes       []JSONChange `json:"changes"`
	Additions     int          `json:"additions"`
	Removals      int          `json:"removals"`
	Modifications int          `json:"modifications"`
}

// JSONDiff computes differences between two JSON-compatible values.
func (e *Engine) JSONDiff(old, new any) *JSONDiffResult {
	result := &JSONDiffResult{}
	e.jsonDiffRec(old, new, "", result)
	return result
}

func (e *Engine) jsonDiffRec(old, new any, path JSONPath, result *JSONDiffResult) {
	if old == nil && new != nil {
		result.Changes = append(result.Changes, JSONChange{Path: path, Op: OpAdd, NewValue: new})
		result.Additions++
		return
	}
	if old != nil && new == nil {
		result.Changes = append(result.Changes, JSONChange{Path: path, Op: OpRemove, OldValue: old})
		result.Removals++
		return
	}
	if old == nil && new == nil {
		return
	}

	oldMap, oldIsMap := old.(map[string]any)
	newMap, newIsMap := new.(map[string]any)

	if oldIsMap && newIsMap {
		for k, nv := range newMap {
			subPath := JSONPath(fmt.Sprintf("%s.%s", path, k))
			if path == "" {
				subPath = JSONPath(k)
			}
			if ov, ok := oldMap[k]; ok {
				e.jsonDiffRec(ov, nv, subPath, result)
			} else {
				result.Changes = append(result.Changes, JSONChange{Path: subPath, Op: OpAdd, NewValue: nv})
				result.Additions++
			}
		}
		for k, ov := range oldMap {
			if _, ok := newMap[k]; !ok {
				subPath := JSONPath(fmt.Sprintf("%s.%s", path, k))
				if path == "" {
					subPath = JSONPath(k)
				}
				result.Changes = append(result.Changes, JSONChange{Path: subPath, Op: OpRemove, OldValue: ov})
				result.Removals++
			}
		}
		return
	}

	oldSlice, oldIsSlice := old.([]any)
	newSlice, newIsSlice := new.([]any)

	if oldIsSlice && newIsSlice {
		maxLen := len(oldSlice)
		if len(newSlice) > maxLen {
			maxLen = len(newSlice)
		}
		for i := 0; i < maxLen; i++ {
			sp := JSONPath(fmt.Sprintf("%s[%d]", path, i))
			if i < len(oldSlice) && i < len(newSlice) {
				e.jsonDiffRec(oldSlice[i], newSlice[i], sp, result)
			} else if i < len(newSlice) {
				result.Changes = append(result.Changes, JSONChange{Path: sp, Op: OpAdd, NewValue: newSlice[i]})
				result.Additions++
			} else {
				result.Changes = append(result.Changes, JSONChange{Path: sp, Op: OpRemove, OldValue: oldSlice[i]})
				result.Removals++
			}
		}
		return
	}

	if fmt.Sprint(old) != fmt.Sprint(new) {
		result.Changes = append(result.Changes, JSONChange{Path: path, Op: OpChange, OldValue: old, NewValue: new})
		result.Modifications++
	}
}

// ── Formatting ────────────────────────────────────────────

// FormatDiff formats a unified diff.
func FormatDiff(result *DiffResult) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "--- %s\n+++ %s\n", result.OldFile, result.NewFile)
	fmt.Fprintf(&sb, "@@ +%d -%d @@\n\n", result.Added, result.Removed)
	for _, l := range result.Lines {
		switch l.Op {
		case OpAdd:
			fmt.Fprintf(&sb, "+ %s\n", l.NewLine)
		case OpRemove:
			fmt.Fprintf(&sb, "- %s\n", l.OldLine)
		case OpChange:
			fmt.Fprintf(&sb, "~ %s → %s\n", l.OldLine, l.NewLine)
		case OpSame:
			fmt.Fprintf(&sb, "  %s\n", l.OldLine)
		}
	}
	return sb.String()
}

// FormatChanges formats JSON changes.
func FormatChanges(result *JSONDiffResult) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "JSON Diff: +%d -%d ~%d\n%s\n\n", result.Additions, result.Removals, result.Modifications, strings.Repeat("─", 60))
	sort.Slice(result.Changes, func(i, j int) bool { return result.Changes[i].Path < result.Changes[j].Path })
	for _, c := range result.Changes {
		switch c.Op {
		case OpAdd:
			fmt.Fprintf(&sb, "  + %s = %v\n", c.Path, c.NewValue)
		case OpRemove:
			fmt.Fprintf(&sb, "  - %s = %v\n", c.Path, c.OldValue)
		case OpChange:
			fmt.Fprintf(&sb, "  ~ %s: %v → %v\n", c.Path, c.OldValue, c.NewValue)
		}
	}
	return sb.String()
}
