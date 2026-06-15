package patch

import (
	"strings"
	"testing"
)

func TestParsePatchString_Basic(t *testing.T) {
	patchStr := `--- a/file.txt
+++ b/file.txt
@@ -1,3 +1,3 @@
 line1
-line2
+line2_modified
 line3
`
	p, err := ParsePatchString(patchStr)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if p.OldFile != "a/file.txt" {
		t.Errorf("expected old file 'a/file.txt', got %q", p.OldFile)
	}
	if p.NewFile != "b/file.txt" {
		t.Errorf("expected new file 'b/file.txt', got %q", p.NewFile)
	}
	if len(p.Hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(p.Hunks))
	}
}

func TestApply_Basic(t *testing.T) {
	patchStr := `--- a/test.txt
+++ b/test.txt
@@ -1,3 +1,3 @@
 line1
-line2
+line2_modified
 line3
`
	p, _ := ParsePatchString(patchStr)
	src := "line1\nline2\nline3"
	result := Apply(p, src)
	if !result.Success {
		t.Fatalf("apply failed: %v", result.Errors)
	}
	if !strings.Contains(result.Output, "line2_modified") {
		t.Errorf("expected 'line2_modified' in output: %s", result.Output)
	}
	if strings.Contains(result.Output, "line2\n") {
		t.Error("old line2 should be removed")
	}
}

func TestApply_AddOnly(t *testing.T) {
	patchStr := `--- a/test.txt
+++ b/test.txt
@@ -1,2 +1,3 @@
 line1
 line2
+line3
`
	p, _ := ParsePatchString(patchStr)
	src := "line1\nline2"
	result := Apply(p, src)
	if !result.Success {
		t.Fatalf("apply failed: %v", result.Errors)
	}
	if !strings.Contains(result.Output, "line3") {
		t.Errorf("expected 'line3' in output: %s", result.Output)
	}
}

func TestApply_DeleteOnly(t *testing.T) {
	patchStr := `--- a/test.txt
+++ b/test.txt
@@ -1,3 +1,2 @@
 line1
-line2
 line3
`
	p, _ := ParsePatchString(patchStr)
	src := "line1\nline2\nline3"
	result := Apply(p, src)
	if !result.Success {
		t.Fatalf("apply failed: %v", result.Errors)
	}
	if strings.Contains(result.Output, "line2") {
		t.Error("'line2' should be removed")
	}
}

func TestApply_MultiHunk(t *testing.T) {
	patchStr := `--- a/test.txt
+++ b/test.txt
@@ -1,3 +1,3 @@
 line1
-line2
+line2_new
 line3
@@ -5,3 +5,3 @@
 line5
-line6
+line6_new
 line7
`
	p, _ := ParsePatchString(patchStr)
	src := "line1\nline2\nline3\nline4\nline5\nline6\nline7"
	result := Apply(p, src)
	if !result.Success {
		t.Fatalf("apply failed: %v", result.Errors)
	}
	if !strings.Contains(result.Output, "line2_new") {
		t.Error("expected line2_new")
	}
	if !strings.Contains(result.Output, "line6_new") {
		t.Error("expected line6_new")
	}
	if strings.Contains(result.Output, "line2\n") {
		t.Error("old line2 should be removed")
	}
}

func TestReverse(t *testing.T) {
	patchStr := `--- a/test.txt
+++ b/test.txt
@@ -1,3 +1,3 @@
 line1
-line2
+line2_modified
 line3
`
	p, _ := ParsePatchString(patchStr)
	rev := p.Reverse()

	// Apply the original to source
	src := "line1\nline2\nline3"
	result := Apply(p, src)
	modified := result.Output

	// Apply reverse to get back
	revResult := Apply(rev, modified)
	if revResult.Output != src {
		t.Errorf("reverse application should restore original:\nexpected: %q\ngot:      %q", src, revResult.Output)
	}
}

func TestComputeDiff_RoundTrip(t *testing.T) {
	old := "line1\nline2\nline3\nline4\nline5"
	new := "line1\nline2_modified\nline3\nline4\nline5"

	diff := ComputeDiff("old.txt", "new.txt", old, new)
	p := &Patch{OldFile: diff.OldFile, NewFile: diff.NewFile, Hunks: diff.Hunks}

	result := Apply(p, old)
	if result.Output != new {
		t.Errorf("round-trip failed:\nexpected: %q\ngot:      %q", new, result.Output)
	}
}

func TestThreeWayMerge_NoConflict(t *testing.T) {
	base := "line1\nline2\nline3"
	ours := "line1\nline2_ours\nline3"
	theirs := "line1\nline2\nline3_added"

	result := ThreeWayMerge(base, ours, theirs)
	if !result.Success {
		t.Logf("Conflicts: %d", len(result.Conflicts))
	}
}

func TestThreeWayMerge_Conflict(t *testing.T) {
	base := "line1\nline2\nline3"
	ours := "line1\nline2_ours\nline3"
	theirs := "line1\nline2_theirs\nline3"

	result := ThreeWayMerge(base, ours, theirs)
	if result.HasConflicts() {
		t.Logf("Found %d conflict(s)", result.ConflictCount())
	}
}

func TestParsePatchString_MultipleHunks(t *testing.T) {
	patchStr := `--- a/file.go
+++ b/file.go
@@ -10,7 +10,7 @@ func main() {
 	ctx1
-	old1
+	new1
 	ctx2
@@ -20,5 +20,5 @@ func other() {
 	ctx3
-	old2
+	new2
 	ctx4
`
	p, err := ParsePatchString(patchStr)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(p.Hunks) != 2 {
		t.Errorf("expected 2 hunks, got %d", len(p.Hunks))
	}
}

func TestApply_FuzzFactor(t *testing.T) {
	patchStr := `--- a/test.txt
+++ b/test.txt
@@ -2,2 +2,2 @@
 line2
-line3
+line3_new
`
	p, _ := ParsePatchString(patchStr)
	// Source has an extra line at the top, so the hunk is offset
	src := "extra_line\nline2\nline3\nline4"

	// Without fuzz
	result := Apply(p, src)
	if result.Success {
		t.Log("applied without fuzz")
	}

	// With fuzz
	result2 := ApplyWithFuzz(p, src, 2)
	if !result2.Success {
		t.Logf("even with fuzz: %v", result2.Errors)
	}
}

func TestPatchSeries(t *testing.T) {
	ps := NewPatchSeries("test-series", "A test series")

	p1Str := `--- a/test.txt
+++ b/test.txt
@@ -1,1 +1,1 @@
-line1
+line1_patched
`
	p2Str := `--- a/test.txt
+++ b/test.txt
@@ -1,1 +1,1 @@
 line1_patched
+extra_line
`
	p1, _ := ParsePatchString(p1Str)
	p2, _ := ParsePatchString(p2Str)
	ps.Add(p1)
	ps.Add(p2)

	src := "line1"
	result := ps.ApplyAll(src)
	if !result.Success {
		t.Fatalf("series apply failed: %v", result.Errors)
	}
	if !strings.Contains(result.Output, "line1_patched") {
		t.Error("expected line1_patched")
	}
	if !strings.Contains(result.Output, "extra_line") {
		t.Error("expected extra_line")
	}
}

func TestPatchSeries_Reverse(t *testing.T) {
	p1Str := `--- a/test.txt
+++ b/test.txt
@@ -1,1 +1,1 @@
-line1
+line1_new
`
	p1, _ := ParsePatchString(p1Str)
	ps := NewPatchSeries("test", "test")
	ps.Add(p1)

	rev := ps.ReverseAll()
	if len(rev.Patches) != 1 {
		t.Fatalf("expected 1 reversed patch, got %d", len(rev.Patches))
	}
}

func TestPatchStats(t *testing.T) {
	patchStr := `--- a/file.txt
+++ b/file.txt
@@ -1,3 +1,4 @@
 line1
-line2
+line2_new
+line2_extra
 line3
`
	p, _ := ParsePatchString(patchStr)
	stats := p.Stats()
	if stats.Additions != 2 {
		t.Errorf("expected 2 additions, got %d", stats.Additions)
	}
	if stats.Deletions != 1 {
		t.Errorf("expected 1 deletion, got %d", stats.Deletions)
	}
	if stats.Hunks != 1 {
		t.Errorf("expected 1 hunk, got %d", stats.Hunks)
	}
}

func TestIsEmpty(t *testing.T) {
	p, _ := ParsePatchString("--- a\n+++ b\n")
	if !p.IsEmpty() {
		t.Error("should be empty")
	}

	p2, _ := ParsePatchString("--- a\n+++ b\n@@ -1,1 +1,1 @@\n line\n")
	if !p2.IsEmpty() {
		t.Error("context-only patch should be considered empty (no actual changes)")
	}
}

func TestPatchFormat(t *testing.T) {
	patchStr := `--- a/file.txt
+++ b/file.txt
@@ -1,1 +1,1 @@
-old
+new
`
	p, _ := ParsePatchString(patchStr)
	formatted := p.String()
	if !strings.Contains(formatted, "--- a/file.txt") {
		t.Error("formatted should contain old header")
	}
	if !strings.Contains(formatted, "+++ b/file.txt") {
		t.Error("formatted should contain new header")
	}
}

func TestComputeDiff_Additions(t *testing.T) {
	old := "line1\nline2"
	new := "line1\nline1.5\nline2"
	diff := ComputeDiff("old", "new", old, new)
	if len(diff.Hunks) == 0 {
		t.Error("should have at least one hunk")
	}
	t.Logf("Diff:\n%s", diff.String())
}

func TestComputeDiff_Deletions(t *testing.T) {
	old := "line1\nline2\nline3"
	new := "line1\nline3"
	diff := ComputeDiff("old", "new", old, new)
	if len(diff.Hunks) == 0 {
		t.Error("should have at least one hunk")
	}
	t.Logf("Diff:\n%s", diff.String())
}

func TestPatchSet(t *testing.T) {
	ps := NewPatchSet()
	p, _ := ParsePatchString("--- a/file1.txt\n+++ b/file1.txt\n@@ -1,1 +1,1 @@\n-old\n+new\n")
	ps.AddPatch("file1.txt", p)

	if _, ok := ps.GetPatch("file1.txt"); !ok {
		t.Error("should find file1.txt")
	}
	if _, ok := ps.GetPatch("file2.txt"); ok {
		t.Error("should not find file2.txt")
	}

	files := ps.Files()
	if len(files) != 1 || files[0] != "file1.txt" {
		t.Errorf("unexpected files: %v", files)
	}
}

func TestWordDiffs(t *testing.T) {
	diffs := WordDiffs("hello world", "hello beautiful world")
	if len(diffs) == 0 {
		t.Error("should detect word differences")
	}
	t.Logf("Word diffs: %+v", diffs)
}

func TestPatchApply_ErrorCase(t *testing.T) {
	patchStr := `--- a/test.txt
+++ b/test.txt
@@ -10,3 +10,3 @@
 line10
-line11
+line11_new
 line12
`
	p, _ := ParsePatchString(patchStr)
	src := "line1\nline2\nline3" // no match at all
	result := Apply(p, src)
	if result.Success {
		t.Error("should fail on non-matching hunk")
	}
}

func TestParsePatchString_NoNewline(t *testing.T) {
	patchStr := `--- a/test.txt
+++ b/test.txt
@@ -1,1 +1,1 @@
 line1
\ No newline at end of file
+line1_new
\ No newline at end of file
`
	p, err := ParsePatchString(patchStr)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(p.Hunks) != 1 {
		t.Errorf("expected 1 hunk, got %d", len(p.Hunks))
	}
}

func TestMergeStrings(t *testing.T) {
	result := MergeStrings("base", "base_ours", "base_theirs")
	if result == nil {
		t.Fatal("MergeStrings returned nil")
	}
	t.Logf("Merge result: success=%v conflicts=%d", result.Success, len(result.Conflicts))
}

func TestApplyToFile(t *testing.T) {
	pStr := `--- a/test.txt
+++ b/test.txt
@@ -1,1 +1,1 @@
-old
+new
`
	p, _ := ParsePatchString(pStr)
	out, err := ApplyToFile(p, "old")
	if err != nil {
		t.Fatalf("ApplyToFile error: %v", err)
	}
	if out != "new" {
		t.Errorf("expected 'new', got %q", out)
	}
}

func TestApplyToFile_Error(t *testing.T) {
	pStr := `--- a/test.txt
+++ b/test.txt
@@ -5,1 +5,1 @@
-old
+new
`
	p, _ := ParsePatchString(pStr)
	_, err := ApplyToFile(p, "line1")
	if err == nil {
		t.Error("expected error for non-matching patch")
	}
}

func TestPatchSet_ApplyAll(t *testing.T) {
	ps := NewPatchSet()
	p1, _ := ParsePatchString("--- a/f1.txt\n+++ b/f1.txt\n@@ -1,1 +1,1 @@\n-a\n+a_new\n")
	p2, _ := ParsePatchString("--- a/f2.txt\n+++ b/f2.txt\n@@ -1,1 +1,1 @@\n-b\n+b_new\n")
	ps.AddPatch("f1.txt", p1)
	ps.AddPatch("f2.txt", p2)

	sources := map[string]string{
		"f1.txt": "a",
		"f2.txt": "b",
	}
	results, errs := ps.ApplyAll(sources)
	if len(errs) > 0 {
		t.Errorf("errors: %v", errs)
	}
	if results["f1.txt"] != "a_new" {
		t.Errorf("f1.txt: expected 'a_new', got %q", results["f1.txt"])
	}
	if results["f2.txt"] != "b_new" {
		t.Errorf("f2.txt: expected 'b_new', got %q", results["f2.txt"])
	}
}

func TestPatchSet_ApplyAll_MissingSource(t *testing.T) {
	ps := NewPatchSet()
	p1, _ := ParsePatchString("--- a/f1.txt\n+++ b/f1.txt\n@@ -1,1 +1,1 @@\n-a\n+a_new\n")
	ps.AddPatch("f1.txt", p1)

	sources := map[string]string{} // empty
	_, errs := ps.ApplyAll(sources)
	if len(errs) == 0 {
		t.Error("expected error for missing source")
	}
}
