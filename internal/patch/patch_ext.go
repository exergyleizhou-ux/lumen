// Package patch - extension: semantic patching, patch validation,
// interactive patch editing, patch format conversion.
package patch

import (
	"fmt"
	"regexp"
	"strings"
)

// ---- Semantic Patching ----

// SemanticPatch applies changes based on semantic context rather than line numbers.
type SemanticPatch struct {
	Anchor  string // regex or literal to find the context
	Action  string // "insert_before", "insert_after", "replace", "delete"
	Content string
}

// ApplySemanticPatch applies a semantic patch to source text.
func ApplySemanticPatch(src string, sp *SemanticPatch) (string, error) {
	re, err := regexp.Compile(sp.Anchor)
	if err != nil {
		// Treat as literal
		re = regexp.MustCompile(regexp.QuoteMeta(sp.Anchor))
	}

	loc := re.FindStringIndex(src)
	if loc == nil {
		return src, fmt.Errorf("anchor '%s' not found", sp.Anchor)
	}

	switch sp.Action {
	case "insert_before":
		return src[:loc[0]] + sp.Content + src[loc[0]:], nil
	case "insert_after":
		return src[:loc[1]] + sp.Content + src[loc[1]:], nil
	case "replace":
		return src[:loc[0]] + sp.Content + src[loc[1]:], nil
	case "delete":
		return src[:loc[0]] + src[loc[1]:], nil
	default:
		return src, fmt.Errorf("unknown action '%s'", sp.Action)
	}
}

// ---- Patch Validation ----

// ValidatePatch checks a patch for common issues.
func ValidatePatch(p *Patch) []string {
	var issues []string

	if p.OldFile == "" && p.NewFile == "" {
		issues = append(issues, "patch has no file headers")
	}

	for i, hunk := range p.Hunks {
		if hunk.OldStart < 1 {
			issues = append(issues, fmt.Sprintf("hunk %d: invalid old start %d", i+1, hunk.OldStart))
		}
		if hunk.NewStart < 1 {
			issues = append(issues, fmt.Sprintf("hunk %d: invalid new start %d", i+1, hunk.NewStart))
		}

		oldCount := 0
		newCount := 0
		for _, dl := range hunk.Lines {
			switch dl.Kind {
			case DiffDel, DiffContext:
				oldCount++
			}
			switch dl.Kind {
			case DiffAdd, DiffContext:
				newCount++
			}
		}
		if oldCount != hunk.OldCount {
			issues = append(issues, fmt.Sprintf("hunk %d: header says old count %d but found %d", i+1, hunk.OldCount, oldCount))
		}
		if newCount != hunk.NewCount {
			issues = append(issues, fmt.Sprintf("hunk %d: header says new count %d but found %d", i+1, hunk.NewCount, newCount))
		}
	}

	return issues
}

// ---- Patch Chunking ----

// ChunkByFile splits a multi-file patch by file.
func ChunkByFile(patchStr string) map[string]string {
	chunks := make(map[string]string)

	fileRe := regexp.MustCompile(`^---\s+(\S+)`)
	var currentFile string
	var currentChunk strings.Builder

	for _, line := range strings.Split(patchStr, "\n") {
		if matches := fileRe.FindStringSubmatch(line); matches != nil {
			if currentFile != "" {
				chunks[currentFile] = currentChunk.String()
				currentChunk.Reset()
			}
			currentFile = matches[1]
		}
		if currentFile != "" {
			currentChunk.WriteString(line)
			currentChunk.WriteString("\n")
		}
	}

	if currentFile != "" {
		chunks[currentFile] = currentChunk.String()
	}

	return chunks
}

// ---- Interactive patch editing ----

// PatchEditor supports interactive patch editing operations.
type PatchEditor struct {
	patch *Patch
}

// NewPatchEditor creates an editor for a patch.
func NewPatchEditor(p *Patch) *PatchEditor {
	return &PatchEditor{patch: p}
}

// DropHunk removes a hunk at the given index.
func (pe *PatchEditor) DropHunk(index int) bool {
	if index < 0 || index >= len(pe.patch.Hunks) {
		return false
	}
	pe.patch.Hunks = append(pe.patch.Hunks[:index], pe.patch.Hunks[index+1:]...)
	return true
}

// ModifyHunk changes a hunk's context.
func (pe *PatchEditor) ModifyHunk(index int, oldStart, newStart int) bool {
	if index < 0 || index >= len(pe.patch.Hunks) {
		return false
	}
	h := pe.patch.Hunks[index]
	h.OldStart = oldStart
	h.NewStart = newStart
	return true
}

// AddHunk appends a new hunk.
func (pe *PatchEditor) AddHunk(h *Hunk) {
	pe.patch.Hunks = append(pe.patch.Hunks, h)
}

// Patch returns the edited patch.
func (pe *PatchEditor) Patch() *Patch {
	return pe.patch
}

// ---- Patch format conversion ----

// ToContextDiff converts a unified diff patch to context diff format.
func ToContextDiff(p *Patch) string {
	var sb strings.Builder

	if p.OldFile != "" {
		sb.WriteString(fmt.Sprintf("*** %s\n", p.OldFile))
	}
	if p.NewFile != "" {
		sb.WriteString(fmt.Sprintf("--- %s\n", p.NewFile))
	}
	sb.WriteString("***************\n")

	for _, hunk := range p.Hunks {
		sb.WriteString(fmt.Sprintf("*** %d,%d ****\n", hunk.OldStart, hunk.OldStart+hunk.OldCount-1))
		for _, dl := range hunk.Lines {
			switch dl.Kind {
			case DiffContext:
				sb.WriteString(fmt.Sprintf("  %s\n", dl.Content))
			case DiffDel:
				sb.WriteString(fmt.Sprintf("- %s\n", dl.Content))
			case DiffAdd:
				sb.WriteString(fmt.Sprintf("! %s\n", dl.Content)) // In context diff, changed lines marked with !
			}
		}
		sb.WriteString(fmt.Sprintf("--- %d,%d ----\n", hunk.NewStart, hunk.NewStart+hunk.NewCount-1))
		for _, dl := range hunk.Lines {
			switch dl.Kind {
			case DiffContext:
				sb.WriteString(fmt.Sprintf("  %s\n", dl.Content))
			case DiffAdd:
				sb.WriteString(fmt.Sprintf("+ %s\n", dl.Content))
			case DiffDel:
				sb.WriteString(fmt.Sprintf("! %s\n", dl.Content))
			}
		}
	}

	return sb.String()
}

// ---- Patch dry-run ----

// DryRun simulates applying a patch and returns what would change.
func DryRun(p *Patch, src string) (*ApplyResult, string) {
	result := Apply(p, src)
	return result, result.Output
}

// CanApply checks if a patch can be applied cleanly.
func CanApply(p *Patch, src string) bool {
	result := Apply(p, src)
	return result.Success
}

// ---- Patch dependency resolution ----

// PatchDependency represents a dependency between patches.
type PatchDependency struct {
	Before string
	After  string
}

// SortPatches topologically sorts patches based on dependencies.
func SortPatches(patches []*Patch, deps []PatchDependency) ([]*Patch, error) {
	// Build graph
	inDegree := make(map[string]int)
	graph := make(map[string][]string)
	nodes := make(map[string]*Patch)

	for _, p := range patches {
		name := p.OldFile
		nodes[name] = p
		if _, ok := inDegree[name]; !ok {
			inDegree[name] = 0
		}
	}

	for _, dep := range deps {
		graph[dep.Before] = append(graph[dep.Before], dep.After)
		inDegree[dep.After]++
	}

	// Kahn's algorithm
	var queue []string
	for name, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, name)
		}
	}

	var sorted []*Patch
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if p, ok := nodes[name]; ok {
			sorted = append(sorted, p)
		}
		for _, neighbor := range graph[name] {
			inDegree[neighbor]--
			if inDegree[neighbor] == 0 {
				queue = append(queue, neighbor)
			}
		}
	}

	if len(sorted) != len(patches) {
		return nil, fmt.Errorf("circular dependency detected")
	}

	return sorted, nil
}

// ---- Fuzzy matcher for hunks ----

// FuzzyMatch attempts to match a hunk with variable whitespace.
func FuzzyMatch(srcLines []string, expected []string, fuzz int) int {
	// Try exact match first
	idx := findMatch(srcLines, expected, 0, 0)
	if idx >= 0 {
		return idx
	}

	// Try with whitespace normalization
	normalized := make([]string, len(srcLines))
	for i, line := range srcLines {
		normalized[i] = strings.TrimSpace(line)
	}
	normExpected := make([]string, len(expected))
	for i, line := range expected {
		normExpected[i] = strings.TrimSpace(line)
	}

	return findMatch(normalized, normExpected, 0, fuzz)
}
