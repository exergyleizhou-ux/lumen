package patch

import (
	"strings"
	"testing"
)

func TestApplySemanticPatch_InsertBefore(t *testing.T) {
	src := "line1\nline2\nline3"
	sp := &SemanticPatch{
		Anchor:  "line2",
		Action:  "insert_before",
		Content: "inserted\n",
	}
	result, err := ApplySemanticPatch(src, sp)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !strings.Contains(result, "inserted") {
		t.Error("should contain inserted text")
	}
	if !strings.Contains(result, "line2") {
		t.Error("should still contain line2")
	}
}

func TestApplySemanticPatch_InsertAfter(t *testing.T) {
	src := "line1\nline2\nline3"
	sp := &SemanticPatch{
		Anchor:  "line2",
		Action:  "insert_after",
		Content: "\ninserted",
	}
	result, _ := ApplySemanticPatch(src, sp)
	if !strings.Contains(result, "inserted") {
		t.Error("should contain inserted text")
	}
}

func TestApplySemanticPatch_Replace(t *testing.T) {
	src := "hello world"
	sp := &SemanticPatch{
		Anchor:  "world",
		Action:  "replace",
		Content: "golang",
	}
	result, _ := ApplySemanticPatch(src, sp)
	if result != "hello golang" {
		t.Errorf("expected 'hello golang', got %q", result)
	}
}

func TestApplySemanticPatch_Delete(t *testing.T) {
	src := "remove this part please"
	sp := &SemanticPatch{
		Anchor: " this part",
		Action: "delete",
	}
	result, _ := ApplySemanticPatch(src, sp)
	if strings.Contains(result, "this part") {
		t.Error("should have removed 'this part'")
	}
}

func TestApplySemanticPatch_NotFound(t *testing.T) {
	src := "hello"
	sp := &SemanticPatch{
		Anchor: "missing",
		Action: "replace",
	}
	_, err := ApplySemanticPatch(src, sp)
	if err == nil {
		t.Error("should error when anchor not found")
	}
}

func TestValidatePatch(t *testing.T) {
	pStr := "--- a\n+++ b\n@@ -1,3 +1,3 @@\n line1\n-line2\n+line2_new\n line3\n"
	p, _ := ParsePatchString(pStr)
	issues := ValidatePatch(p)
	if len(issues) != 0 {
		t.Errorf("valid patch should have no issues, got %v", issues)
	}
}

func TestValidatePatch_NoHeaders(t *testing.T) {
	p := &Patch{}
	issues := ValidatePatch(p)
	if len(issues) == 0 {
		t.Error("patch with no headers should have issues")
	}
}

func TestValidatePatch_BadCounts(t *testing.T) {
	p := &Patch{
		OldFile: "a",
		NewFile: "b",
		Hunks: []*Hunk{{
			OldStart: 1,
			OldCount: 99, // wrong count
			NewStart: 1,
			NewCount: 1,
			Lines:    []DiffLine{{Kind: DiffContext, Content: "line"}},
		}},
	}
	issues := ValidatePatch(p)
	if len(issues) == 0 {
		t.Error("should detect count mismatch")
	}
}

func TestPatchEditor_DropHunk(t *testing.T) {
	pStr := "--- a\n+++ b\n@@ -1,2 +1,2 @@\n line1\n line2\n@@ -4,2 +4,2 @@\n line4\n line5\n"
	p, _ := ParsePatchString(pStr)
	pe := NewPatchEditor(p)

	if !pe.DropHunk(0) {
		t.Error("DropHunk should succeed")
	}
	if len(p.Hunks) != 1 {
		t.Errorf("expected 1 hunk, got %d", len(p.Hunks))
	}
	if pe.DropHunk(99) {
		t.Error("DropHunk out of bounds should return false")
	}
}

func TestPatchEditor_ModifyHunk(t *testing.T) {
	pStr := "--- a\n+++ b\n@@ -1,1 +1,1 @@\n-old\n+new\n"
	p, _ := ParsePatchString(pStr)
	pe := NewPatchEditor(p)

	if !pe.ModifyHunk(0, 10, 20) {
		t.Error("ModifyHunk should succeed")
	}
	if p.Hunks[0].OldStart != 10 || p.Hunks[0].NewStart != 20 {
		t.Error("hunk not modified")
	}
}

func TestPatchEditor_AddHunk(t *testing.T) {
	p, _ := ParsePatchString("--- a\n+++ b\n")
	pe := NewPatchEditor(p)
	pe.AddHunk(&Hunk{OldStart: 5, NewStart: 5})
	if len(p.Hunks) != 1 {
		t.Errorf("expected 1 hunk, got %d", len(p.Hunks))
	}
}

func TestChunkByFile(t *testing.T) {
	patchStr := "--- a.txt\n+++ b.txt\n@@ -1,1 +1,1 @@\n-old\n+new\n--- c.txt\n+++ d.txt\n@@ -1,1 +1,1 @@\n-old2\n+new2\n"
	chunks := ChunkByFile(patchStr)
	if len(chunks) != 2 {
		t.Errorf("expected 2 chunks, got %d", len(chunks))
	}
}

func TestDryRun(t *testing.T) {
	pStr := "--- a\n+++ b\n@@ -1,1 +1,1 @@\n-old\n+new\n"
	p, _ := ParsePatchString(pStr)
	result, output := DryRun(p, "old")
	if !result.Success {
		t.Error("dry run should succeed")
	}
	if output != "new" {
		t.Errorf("expected 'new', got %q", output)
	}
}

func TestCanApply(t *testing.T) {
	pStr := "--- a\n+++ b\n@@ -1,1 +1,1 @@\n-old\n+new\n"
	p, _ := ParsePatchString(pStr)
	if !CanApply(p, "old") {
		t.Error("should be able to apply")
	}
	if CanApply(p, "something else") {
		t.Error("should not be able to apply to wrong source")
	}
}

func TestSortPatches(t *testing.T) {
	p1, _ := ParsePatchString("--- a\n+++ a\n@@ -1,1 +1,1 @@\n-x\n+y\n")
	p2, _ := ParsePatchString("--- b\n+++ b\n@@ -1,1 +1,1 @@\n-x\n+y\n")
	p1.OldFile = "a"
	p2.OldFile = "b"

	deps := []PatchDependency{{Before: "a", After: "b"}}
	sorted, err := SortPatches([]*Patch{p1, p2}, deps)
	if err != nil {
		t.Fatalf("sort error: %v", err)
	}
	if sorted[0].OldFile != "a" || sorted[1].OldFile != "b" {
		t.Error("incorrect sort order")
	}
}

func TestFuzzyMatch(t *testing.T) {
	src := []string{"  hello  ", "  world  "}
	expected := []string{"hello", "world"}
	idx := FuzzyMatch(src, expected, 2)
	if idx != 0 {
		t.Errorf("fuzzy match: expected 0, got %d", idx)
	}
}

func TestToContextDiff(t *testing.T) {
	pStr := "--- a\n+++ b\n@@ -1,2 +1,2 @@\n line1\n-line2\n+line2_new\n"
	p, _ := ParsePatchString(pStr)
	ctx := ToContextDiff(p)
	if !strings.Contains(ctx, "*** a") {
		t.Error("context diff should contain old file")
	}
	if !strings.Contains(ctx, "--- b") {
		t.Error("context diff should contain new file")
	}
}
