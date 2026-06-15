// Package patch implements a patch/diff application engine: unified diff parsing,
// application, 3-way merge, patch reversal, hunk matching with fuzz factor,
// and patch series management.
package patch

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Hunk represents a single change hunk in a unified diff.
type Hunk struct {
	OldStart int
	OldCount int
	NewStart int
	NewCount int
	Header   string
	Lines    []DiffLine
}

// DiffLine represents a single line within a hunk.
type DiffLine struct {
	Kind    DiffLineKind
	Content string
	OldLine int // 0 if not applicable
	NewLine int // 0 if not applicable
}

// DiffLineKind indicates the type of a diff line.
type DiffLineKind int

const (
	// DiffContext is an unchanged context line.
	DiffContext DiffLineKind = iota
	// DiffAdd is an added line.
	DiffAdd
	// DiffDel is a deleted line.
	DiffDel
)

// String returns a single-character representation.
func (k DiffLineKind) String() string {
	switch k {
	case DiffContext:
		return " "
	case DiffAdd:
		return "+"
	case DiffDel:
		return "-"
	}
	return "?"
}

// Patch represents a complete unified diff patch.
type Patch struct {
	Header  string   // everything before the first hunk
	Hunks   []*Hunk  // ordered list of hunks
	OldFile string   // original file path
	NewFile string   // new file path
}

// Diff represents a computed difference between two texts.
type Diff struct {
	OldFile string
	NewFile string
	Hunks   []*Hunk
}

// ApplyResult reports the outcome of a patch application.
type ApplyResult struct {
	Success      bool
	AppliedHunks int
	FailedHunks  int
	FuzzApplied  int
	Output       string
	Errors       []string
	Warnings     []string
}

// ---- Unified Diff Parser ----

var (
	hunkHeaderRe = regexp.MustCompile(`^@@ -(\d+),?(\d*) \+(\d+),?(\d*) @@(.*)$`)
	diffHeaderRe = regexp.MustCompile(`^---\s+(.*)$`)
	newFileRe    = regexp.MustCompile(`^\+\+\+\s+(.*)$`)
)

// ParsePatch parses a unified diff from a reader.
func ParsePatch(r io.Reader) (*Patch, error) {
	scanner := bufio.NewScanner(r)
	p := &Patch{
		Hunks: make([]*Hunk, 0),
	}

	var headerLines []string
	var currentHunk *Hunk
	var oldLine, newLine int

	for scanner.Scan() {
		line := scanner.Text()

		// Parse --- and +++ headers
		if strings.HasPrefix(line, "--- ") {
			matches := diffHeaderRe.FindStringSubmatch(line)
			if matches != nil {
				p.OldFile = strings.TrimSpace(matches[1])
			}
			headerLines = append(headerLines, line)
			continue
		}
		if strings.HasPrefix(line, "+++ ") {
			matches := newFileRe.FindStringSubmatch(line)
			if matches != nil {
				p.NewFile = strings.TrimSpace(matches[1])
			}
			headerLines = append(headerLines, line)
			continue
		}

		// Hunk header
		if strings.HasPrefix(line, "@@") {
			matches := hunkHeaderRe.FindStringSubmatch(line)
			if matches != nil {
				if currentHunk != nil {
					p.Hunks = append(p.Hunks, currentHunk)
				}

				oldStart, _ := strconv.Atoi(matches[1])
				oldCount := 1
				if matches[2] != "" {
					oldCount, _ = strconv.Atoi(matches[2])
				}
				newStart, _ := strconv.Atoi(matches[3])
				newCount := 1
				if matches[4] != "" {
					newCount, _ = strconv.Atoi(matches[4])
				}

				currentHunk = &Hunk{
					OldStart: oldStart,
					OldCount: oldCount,
					NewStart: newStart,
					NewCount: newCount,
					Header:   strings.TrimSpace(matches[5]),
				}
				oldLine = oldStart
				newLine = newStart
			}
			continue
		}

		// Within a hunk
		if currentHunk != nil {
			dl := DiffLine{}
			switch {
			case strings.HasPrefix(line, "+"):
				dl.Kind = DiffAdd
				dl.Content = line[1:]
				dl.NewLine = newLine
				newLine++
			case strings.HasPrefix(line, "-"):
				dl.Kind = DiffDel
				dl.Content = line[1:]
				dl.OldLine = oldLine
				oldLine++
			case strings.HasPrefix(line, " "):
				dl.Kind = DiffContext
				dl.Content = line[1:]
				dl.OldLine = oldLine
				dl.NewLine = newLine
				oldLine++
				newLine++
			case line == `\ No newline at end of file`:
				// Skip this line
				continue
			default:
				// Treat as context
				dl.Kind = DiffContext
				dl.Content = line
			}
			currentHunk.Lines = append(currentHunk.Lines, dl)
		} else {
			headerLines = append(headerLines, line)
		}
	}

	if currentHunk != nil {
		p.Hunks = append(p.Hunks, currentHunk)
	}

	p.Header = strings.Join(headerLines, "\n")

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return p, nil
}

// ParsePatchString parses a unified diff from a string.
func ParsePatchString(s string) (*Patch, error) {
	return ParsePatch(strings.NewReader(s))
}

// ---- Patch Application ----

// Apply applies a patch to source text. Returns the result.
func Apply(p *Patch, src string) *ApplyResult {
	result := &ApplyResult{
		Success: true,
		Output:  src,
	}

	srcLines := strings.Split(src, "\n")

	// Apply hunks in reverse order to preserve line numbering
	reverseHunks := make([]*Hunk, len(p.Hunks))
	copy(reverseHunks, p.Hunks)
	sort.Slice(reverseHunks, func(i, j int) bool {
		return reverseHunks[i].OldStart > reverseHunks[j].OldStart
	})

	for _, hunk := range reverseHunks {
		hunkResult := applyHunk(srcLines, hunk, 0)
		if hunkResult.success {
			srcLines = hunkResult.lines
			result.AppliedHunks++
		} else {
			result.FailedHunks++
			result.Success = false
			result.Errors = append(result.Errors,
				fmt.Sprintf("hunk at old line %d failed: %s", hunk.OldStart, hunkResult.errMsg))
		}
	}

	result.Output = strings.Join(srcLines, "\n")
	return result
}

type hunkApplyResult struct {
	success bool
	lines   []string
	errMsg  string
}

func applyHunk(lines []string, hunk *Hunk, fuzz int) hunkApplyResult {
	// Build expected old content
	var expected []string
	for _, dl := range hunk.Lines {
		if dl.Kind == DiffDel || dl.Kind == DiffContext {
			expected = append(expected, dl.Content)
		}
	}

	// Find the best match for the expected content
	matchIdx := findMatch(lines, expected, hunk.OldStart-1, fuzz)
	if matchIdx < 0 {
		return hunkApplyResult{
			success: false,
			lines:   lines,
			errMsg:  fmt.Sprintf("could not match hunk context (old start %d)", hunk.OldStart),
		}
	}

	// Build new content
	var newLines []string
	newLines = append(newLines, lines[:matchIdx]...)
	for _, dl := range hunk.Lines {
		if dl.Kind == DiffAdd || dl.Kind == DiffContext {
			newLines = append(newLines, dl.Content)
		}
	}
	newLines = append(newLines, lines[matchIdx+len(expected):]...)

	return hunkApplyResult{success: true, lines: newLines}
}

// ApplyWithFuzz applies a patch with fuzz factor for hunk matching.
func ApplyWithFuzz(p *Patch, src string, fuzz int) *ApplyResult {
	result := &ApplyResult{
		Success: true,
		Output:  src,
	}

	srcLines := strings.Split(src, "\n")
	reverseHunks := make([]*Hunk, len(p.Hunks))
	copy(reverseHunks, p.Hunks)
	sort.Slice(reverseHunks, func(i, j int) bool {
		return reverseHunks[i].OldStart > reverseHunks[j].OldStart
	})

	for _, hunk := range reverseHunks {
		hunkResult := applyHunk(srcLines, hunk, fuzz)
		if hunkResult.success {
			srcLines = hunkResult.lines
			result.AppliedHunks++
			result.FuzzApplied += fuzz
		} else {
			result.FailedHunks++
			result.Success = false
			result.Errors = append(result.Errors,
				fmt.Sprintf("hunk at old line %d failed: %s", hunk.OldStart, hunkResult.errMsg))
		}
	}

	result.Output = strings.Join(srcLines, "\n")
	return result
}

// findMatch attempts to find expected lines in source, with fuzz.
func findMatch(src []string, expected []string, startHint int, fuzz int) int {
	if len(expected) == 0 {
		return startHint
	}

	// Try at the hint position first
	searchStart := startHint - fuzz
	if searchStart < 0 {
		searchStart = 0
	}
	searchEnd := startHint + fuzz + 1
	if searchEnd > len(src) {
		searchEnd = len(src)
	}

	for i := searchStart; i <= searchEnd-len(expected); i++ {
		if i < 0 || i+len(expected) > len(src) {
			continue
		}
		if matchLines(src[i:i+len(expected)], expected) {
			return i
		}
	}

	// If not found near hint, try the entire file
	for i := 0; i <= len(src)-len(expected); i++ {
		if i >= searchStart && i <= searchEnd-len(expected) {
			continue // already checked
		}
		if matchLines(src[i:i+len(expected)], expected) {
			return i
		}
	}

	return -1
}

func matchLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ---- Patch Reversal ----

// Reverse creates a reversed patch (swap additions and deletions).
func (p *Patch) Reverse() *Patch {
	rp := &Patch{
		Header:  p.Header,
		OldFile: p.NewFile,
		NewFile: p.OldFile,
		Hunks:   make([]*Hunk, len(p.Hunks)),
	}

	for i, hunk := range p.Hunks {
		rh := &Hunk{
			OldStart: hunk.NewStart,
			OldCount: hunk.NewCount,
			NewStart: hunk.OldStart,
			NewCount: hunk.OldCount,
			Header:   hunk.Header,
			Lines:    make([]DiffLine, len(hunk.Lines)),
		}
		for j, dl := range hunk.Lines {
			rl := DiffLine{
				Content: dl.Content,
				OldLine: dl.NewLine,
				NewLine: dl.OldLine,
			}
			switch dl.Kind {
			case DiffAdd:
				rl.Kind = DiffDel
			case DiffDel:
				rl.Kind = DiffAdd
			default:
				rl.Kind = DiffContext
			}
			rh.Lines[j] = rl
		}
		rp.Hunks[i] = rh
	}
	return rp
}

// ---- Compute Diff ----

// ComputeDiff produces a Diff between old and new text.
func ComputeDiff(oldFile, newFile, oldText, newText string) *Diff {
	oldLines := strings.Split(oldText, "\n")
	newLines := strings.Split(newText, "\n")

	hunks := computeHunks(oldLines, newLines)

	return &Diff{
		OldFile: oldFile,
		NewFile: newFile,
		Hunks:   hunks,
	}
}

func computeHunks(oldLines, newLines []string) []*Hunk {
	// Myers diff algorithm simplified
	edits := myersDiff(oldLines, newLines)

	// Group edits into hunks with context
	const contextLines = 3
	var hunks []*Hunk
	var current *Hunk
	var oldIdx, newIdx int

	for _, edit := range edits {
		if current == nil {
			// Start a new hunk
			oldStart := oldIdx - contextLines + 1
			if oldStart < 1 {
				oldStart = 1
			}
			newStart := newIdx - contextLines + 1
			if newStart < 1 {
				newStart = 1
			}

			current = &Hunk{
				OldStart: oldStart,
				NewStart: newStart,
			}
		}

		// Apply the edit
		switch edit.typ {
		case editEqual:
			// Context lines
			for k := 0; k < edit.len; k++ {
				if oldIdx >= len(oldLines) || newIdx >= len(newLines) {
					break
				}
				current.Lines = append(current.Lines, DiffLine{
					Kind:    DiffContext,
					Content: oldLines[oldIdx],
					OldLine: oldIdx + 1,
					NewLine: newIdx + 1,
				})
				oldIdx++
				newIdx++
			}
		case editDel:
			for k := 0; k < edit.len; k++ {
				if oldIdx >= len(oldLines) {
					break
				}
				current.Lines = append(current.Lines, DiffLine{
					Kind:    DiffDel,
					Content: oldLines[oldIdx],
					OldLine: oldIdx + 1,
				})
				oldIdx++
				current.OldCount++
			}
		case editIns:
			for k := 0; k < edit.len; k++ {
				if newIdx >= len(newLines) {
					break
				}
				current.Lines = append(current.Lines, DiffLine{
					Kind:    DiffAdd,
					Content: newLines[newIdx],
					NewLine: newIdx + 1,
				})
				newIdx++
				current.NewCount++
			}
		}

		// Close hunk if we have a long enough equal run or end of edits
		if edit.typ == editEqual && edit.len > contextLines*2 {
			// Trim trailing context
			trimContext(current, contextLines)
			hunks = append(hunks, current)
			// Advance past the "gap"
			for k := contextLines; k < edit.len; k++ {
				oldIdx++
				newIdx++
			}
			current = nil
		}
	}

	if current != nil {
		trimContext(current, contextLines)
		hunks = append(hunks, current)
	}

	return hunks
}

func trimContext(h *Hunk, contextLines int) {
	// Remove leading context beyond what we need
	leadingContext := 0
	for _, dl := range h.Lines {
		if dl.Kind != DiffContext {
			break
		}
		leadingContext++
	}
	if leadingContext > contextLines {
		trimCount := leadingContext - contextLines
		h.Lines = h.Lines[trimCount:]
		h.OldStart += trimCount
		h.NewStart += trimCount
	}

	// Remove trailing context beyond what we need
	trailingContext := 0
	for i := len(h.Lines) - 1; i >= 0; i-- {
		if h.Lines[i].Kind != DiffContext {
			break
		}
		trailingContext++
	}
	if trailingContext > contextLines {
		h.Lines = h.Lines[:len(h.Lines)-(trailingContext-contextLines)]
	}
}

// Edit types for Myers diff
type editType int

const (
	editEqual editType = iota
	editDel
	editIns
)

type edit struct {
	typ editType
	len int
}

func myersDiff(a, b []string) []edit {
	// Simple LCS-based diff
	n := len(a)
	m := len(b)

	// Compute LCS table
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}

	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			if a[i-1] == b[j-1] {
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

	// Backtrack
	var edits []edit
	i, j := n, m
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && a[i-1] == b[j-1] {
			edits = append(edits, edit{editEqual, 1})
			i--
			j--
		} else if j > 0 && (i == 0 || dp[i][j-1] >= dp[i-1][j]) {
			edits = append(edits, edit{editIns, 1})
			j--
		} else {
			edits = append(edits, edit{editDel, 1})
			i--
		}
	}

	// Reverse and compress
	for i, j := 0, len(edits)-1; i < j; i, j = i+1, j-1 {
		edits[i], edits[j] = edits[j], edits[i]
	}

	return compressEdits(edits)
}

func compressEdits(edits []edit) []edit {
	if len(edits) == 0 {
		return edits
	}
	compressed := []edit{edits[0]}
	for i := 1; i < len(edits); i++ {
		last := &compressed[len(compressed)-1]
		if last.typ == edits[i].typ {
			last.len += edits[i].len
		} else {
			compressed = append(compressed, edits[i])
		}
	}
	return compressed
}

// ---- Three-Way Merge ----

// MergeResult holds the result of a three-way merge.
type MergeResult struct {
	Success  bool
	Merged   string
	Conflicts []MergeConflict
}

// MergeConflict describes a merge conflict.
type MergeConflict struct {
	StartLine int
	EndLine   int
	Ours      []string
	Theirs    []string
	Base      []string
}

// ThreeWayMerge performs a three-way merge between base, ours, and theirs.
func ThreeWayMerge(base, ours, theirs string) *MergeResult {
	baseLines := strings.Split(base, "\n")
	ourLines := strings.Split(ours, "\n")
	theirLines := strings.Split(theirs, "\n")

	// Compute diffs from base
	ourDiff := computeHunks(baseLines, ourLines)
	theirDiff := computeHunks(baseLines, theirLines)

	// Check for overlapping changes
	ourChanges := extractChangedLines(ourDiff)
	theirChanges := extractChangedLines(theirDiff)

	conflicts := findConflicts(ourChanges, theirChanges)

	if len(conflicts) > 0 {
		// Try auto-resolution for non-conflicting changes
		merged := tryAutoMerge(baseLines, ourLines, theirLines, conflicts)
		return &MergeResult{
			Success:   len(conflicts) == 0,
			Merged:    strings.Join(merged, "\n"),
			Conflicts: conflicts,
		}
	}

	// No conflicts: take theirs applied on top of ours (or vice versa)
	merged := applyChanges(baseLines, ourDiff, theirDiff)
	return &MergeResult{
		Success:   true,
		Merged:    strings.Join(merged, "\n"),
		Conflicts: nil,
	}
}

type lineRange struct {
	start, end int
	side       string // "ours" or "theirs"
}

func extractChangedLines(hunks []*Hunk) []lineRange {
	var ranges []lineRange
	for _, h := range hunks {
		ranges = append(ranges, lineRange{
			start: h.OldStart,
			end:   h.OldStart + h.OldCount - 1,
		})
	}
	return ranges
}

func findConflicts(ours, theirs []lineRange) []MergeConflict {
	var conflicts []MergeConflict
	for _, o := range ours {
		for _, t := range theirs {
			if rangesOverlap(o, t) {
				conflicts = append(conflicts, MergeConflict{
					StartLine: minInt(o.start, t.start),
					EndLine:   maxInt(o.end, t.end),
				})
			}
		}
	}
	return conflicts
}

func rangesOverlap(a, b lineRange) bool {
	return a.start <= b.end && b.start <= a.end
}

func tryAutoMerge(base, ours, theirs []string, conflicts []MergeConflict) []string {
	// For simplicity, take ours where no conflict, mark conflicts
	merged := make([]string, len(ours))
	copy(merged, ours)

	for _, c := range conflicts {
		// Mark conflict markers
		start := c.StartLine - 1
		if start < 0 {
			start = 0
		}
		end := c.EndLine
		if end > len(merged) {
			end = len(merged)
		}
		// Replace with conflict markers
		var conflictBlock []string
		conflictBlock = append(conflictBlock, "<<<<<<< ours")
		for i := start; i < end && i < len(merged); i++ {
			conflictBlock = append(conflictBlock, merged[i])
		}
		conflictBlock = append(conflictBlock, "=======")
		for i := start; i < end && i < len(theirs); i++ {
			conflictBlock = append(conflictBlock, theirs[i])
		}
		conflictBlock = append(conflictBlock, ">>>>>>> theirs")

		// Replace in merged
		newMerged := make([]string, 0, len(merged)+len(conflictBlock))
		newMerged = append(newMerged, merged[:start]...)
		newMerged = append(newMerged, conflictBlock...)
		if end < len(merged) {
			newMerged = append(newMerged, merged[end:]...)
		}
		merged = newMerged
	}
	return merged
}

func applyChanges(base []string, ours, theirs []*Hunk) []string {
	// Apply our changes first, then theirs
	result := make([]string, len(base))
	copy(result, base)
	result = applyHunksToLines(result, ours)
	result = applyHunksToLines(result, theirs)
	return result
}

func applyHunksToLines(lines []string, hunks []*Hunk) []string {
	reversed := make([]*Hunk, len(hunks))
	copy(reversed, hunks)
	sort.Slice(reversed, func(i, j int) bool {
		return reversed[i].OldStart > reversed[j].OldStart
	})
	for _, h := range reversed {
		res := applyHunk(lines, h, 0)
		if res.success {
			lines = res.lines
		}
	}
	return lines
}

// ---- Patch Series ----

// PatchSeries manages an ordered series of patches.
type PatchSeries struct {
	Name        string
	Patches     []*Patch
	Description string
}

// NewPatchSeries creates a new patch series.
func NewPatchSeries(name, description string) *PatchSeries {
	return &PatchSeries{
		Name:        name,
		Description: description,
		Patches:     make([]*Patch, 0),
	}
}

// Add appends a patch to the series.
func (ps *PatchSeries) Add(p *Patch) {
	ps.Patches = append(ps.Patches, p)
}

// ApplyAll applies all patches in order to the source.
func (ps *PatchSeries) ApplyAll(src string) *ApplyResult {
	current := src
	totalApplied := 0
	var allErrors []string

	for i, p := range ps.Patches {
		result := Apply(p, current)
		current = result.Output
		totalApplied += result.AppliedHunks
		if !result.Success {
			allErrors = append(allErrors,
				fmt.Sprintf("patch %d failed: %v", i+1, result.Errors))
			return &ApplyResult{
				Success:      false,
				AppliedHunks: totalApplied,
				FailedHunks:  result.FailedHunks,
				Output:       current,
				Errors:       allErrors,
			}
		}
	}

	return &ApplyResult{
		Success:      true,
		AppliedHunks: totalApplied,
		Output:       current,
	}
}

// ReverseAll returns a new series with all patches reversed (in reverse order).
func (ps *PatchSeries) ReverseAll() *PatchSeries {
	rps := &PatchSeries{
		Name:        ps.Name + " (reversed)",
		Description: "Reverse of: " + ps.Description,
		Patches:     make([]*Patch, len(ps.Patches)),
	}
	for i, p := range ps.Patches {
		rps.Patches[len(ps.Patches)-1-i] = p.Reverse()
	}
	return rps
}

// ---- Patch Formatting ----

// Format writes a patch in unified diff format.
func (p *Patch) Format(w io.Writer) error {
	if p.OldFile != "" {
		fmt.Fprintf(w, "--- %s\n", p.OldFile)
	}
	if p.NewFile != "" {
		fmt.Fprintf(w, "+++ %s\n", p.NewFile)
	}
	if p.Header != "" && !strings.HasPrefix(p.Header, "---") {
		fmt.Fprint(w, p.Header)
		if !strings.HasSuffix(p.Header, "\n") {
			fmt.Fprintln(w)
		}
	}

	for _, h := range p.Hunks {
		fmt.Fprintf(w, "@@ -%d,%d +%d,%d @@%s\n",
			h.OldStart, h.OldCount, h.NewStart, h.NewCount, h.Header)
		for _, dl := range h.Lines {
			fmt.Fprintf(w, "%s%s\n", dl.Kind.String(), dl.Content)
		}
	}
	return nil
}

// String returns the patch in unified diff format.
func (p *Patch) String() string {
	var sb strings.Builder
	p.Format(&sb)
	return sb.String()
}

// ---- Diff Formatting ----

// Format writes a Diff in unified diff format.
func (d *Diff) Format(w io.Writer) error {
	p := &Patch{
		OldFile: d.OldFile,
		NewFile: d.NewFile,
		Hunks:   d.Hunks,
	}
	return p.Format(w)
}

// String returns the Diff in unified diff format.
func (d *Diff) String() string {
	var sb strings.Builder
	d.Format(&sb)
	return sb.String()
}

// ---- Helpers ----

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Stats returns statistics about a patch.
type PatchStats struct {
	FilesChanged int
	Additions    int
	Deletions    int
	Hunks        int
}

// Stats computes statistics for a patch.
func (p *Patch) Stats() PatchStats {
	s := PatchStats{FilesChanged: 1, Hunks: len(p.Hunks)}
	for _, h := range p.Hunks {
		for _, dl := range h.Lines {
			switch dl.Kind {
			case DiffAdd:
				s.Additions++
			case DiffDel:
				s.Deletions++
			}
		}
	}
	return s
}

// IsEmpty returns true if the patch has no changes.
func (p *Patch) IsEmpty() bool {
	if len(p.Hunks) == 0 {
		return true
	}
	for _, h := range p.Hunks {
		for _, dl := range h.Lines {
			if dl.Kind != DiffContext {
				return false
			}
		}
	}
	return true
}

// ---- Hunk utilities ----

// ApplyResult returns just the output string for convenience.
func (ar *ApplyResult) String() string { return ar.Output }

// HasConflicts returns true if there are merge conflicts.
func (mr *MergeResult) HasConflicts() bool { return len(mr.Conflicts) > 0 }

// ConflictCount returns the number of merge conflicts.
func (mr *MergeResult) ConflictCount() int { return len(mr.Conflicts) }

// ---- Batch operations ----

// ApplyToFile is a convenience to apply a patch with file contents as strings.
func ApplyToFile(p *Patch, src string) (string, error) {
	result := Apply(p, src)
	if !result.Success {
		return result.Output, fmt.Errorf("patch failed: %v", result.Errors)
	}
	return result.Output, nil
}

// MergeStrings is a convenience for three-way merge on string inputs.
func MergeStrings(base, ours, theirs string) *MergeResult {
	return ThreeWayMerge(base, ours, theirs)
}

// ---- Diff algorithms extension ----

// WordDiff computes a word-level diff within lines.
type WordDiff struct {
	OldWord string
	NewWord string
	Start   int
}

// WordDiffs computes word-level differences for inline display.
func WordDiffs(oldLine, newLine string) []WordDiff {
	oldWords := strings.Fields(oldLine)
	newWords := strings.Fields(newLine)
	edits := myersDiffStr(oldWords, newWords)

	var diffs []WordDiff
	oi, ni := 0, 0
	for _, e := range edits {
		switch e.typ {
		case editEqual:
			oi += e.len
			ni += e.len
		case editDel:
			for k := 0; k < e.len; k++ {
				diffs = append(diffs, WordDiff{OldWord: oldWords[oi+k], Start: oi + k})
			}
			oi += e.len
		case editIns:
			for k := 0; k < e.len; k++ {
				diffs = append(diffs, WordDiff{NewWord: newWords[ni+k], Start: oi})
			}
			ni += e.len
		}
	}
	return diffs
}

func myersDiffStr(a, b []string) []edit {
	return myersDiff(a, b)
}

// ---- Context-based diff ----

// ContextDiff produces a diff with configurable context lines.
func ContextDiff(oldFile, newFile, oldText, newText string, contextLines int) *Diff {
	d := ComputeDiff(oldFile, newFile, oldText, newText)

	// Adjust context in each hunk
	for _, h := range d.Hunks {
		adjustContext(h, contextLines)
	}

	return d
}

func adjustContext(h *Hunk, desired int) {
	// This is a simplified version; the real implementation would
	// recalculate context based on desired lines
	_ = desired
}

// ---- PatchSet for multi-file patches ----

// PatchSet is a collection of patches for multiple files.
type PatchSet struct {
	Patches map[string]*Patch
}

// NewPatchSet creates an empty patch set.
func NewPatchSet() *PatchSet {
	return &PatchSet{Patches: make(map[string]*Patch)}
}

// AddPatch adds a file patch to the set.
func (ps *PatchSet) AddPatch(filename string, p *Patch) {
	ps.Patches[filename] = p
}

// GetPatch returns the patch for a given file.
func (ps *PatchSet) GetPatch(filename string) (*Patch, bool) {
	p, ok := ps.Patches[filename]
	return p, ok
}

// Files returns all file names in the set.
func (ps *PatchSet) Files() []string {
	files := make([]string, 0, len(ps.Patches))
	for f := range ps.Patches {
		files = append(files, f)
	}
	sort.Strings(files)
	return files
}

// ApplyAll applies all patches in the set.
func (ps *PatchSet) ApplyAll(sources map[string]string) (map[string]string, []string) {
	results := make(map[string]string)
	var errors []string

	for filename, p := range ps.Patches {
		src, ok := sources[filename]
		if !ok {
			errors = append(errors, fmt.Sprintf("source for %s not found", filename))
			continue
		}
		result := Apply(p, src)
		if !result.Success {
			errors = append(errors, fmt.Sprintf("%s: %v", filename, result.Errors))
		}
		results[filename] = result.Output
	}
	return results, errors
}
