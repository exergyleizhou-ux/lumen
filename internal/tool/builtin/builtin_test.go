package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lumen/internal/evidence"
)

func TestBashToolExecute(t *testing.T) {
	bt := &BashTool{}
	result, err := bt.Execute(context.Background(), json.RawMessage(`{"command":"echo hello"}`))
	if err != nil {
		t.Fatalf("bash echo: %v", err)
	}
	if !strings.Contains(result, "hello") {
		t.Errorf("expected 'hello' in output, got %q", result)
	}
}

func TestBashToolStderr(t *testing.T) {
	bt := &BashTool{}
	result, err := bt.Execute(context.Background(), json.RawMessage(`{"command":"echo err >&2; echo ok"}`))
	if err != nil {
		t.Fatalf("bash stderr: %v", err)
	}
	if !strings.Contains(result, "err") {
		t.Errorf("expected stderr in output, got %q", result)
	}
	if !strings.Contains(result, "ok") {
		t.Errorf("expected stdout in output, got %q", result)
	}
}

func TestBashToolEmptyCommand(t *testing.T) {
	bt := &BashTool{}
	_, err := bt.Execute(context.Background(), json.RawMessage(`{"command":""}`))
	if err == nil {
		t.Error("empty command should error")
	}
}

func TestBashToolExitCode(t *testing.T) {
	bt := &BashTool{}
	// A non-zero exit must surface as a non-nil error so the agent marks the
	// step failed (✗) instead of a misleading green ✓ — while the command output
	// is still preserved so the model can see what went wrong and self-correct.
	result, err := bt.Execute(context.Background(), json.RawMessage(`{"command":"echo boom; exit 7"}`))
	if err == nil {
		t.Fatal("non-zero exit should return a non-nil error, got nil")
	}
	if !strings.Contains(err.Error(), "7") {
		t.Errorf("error should report the exit code 7, got %v", err)
	}
	if !strings.Contains(result, "boom") {
		t.Errorf("command output should be preserved for the model, got %q", result)
	}
}

func TestReadFileTool(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("line1\nline2\nline3"), 0o644)

	rt := &ReadFileTool{}
	result, err := rt.Execute(context.Background(), json.RawMessage(`{"path":"`+path+`"}`))
	if err != nil {
		t.Fatalf("read_file: %v", err)
	}
	if !strings.Contains(result, "1→line1") {
		t.Errorf("should contain line numbers, got: %s", result)
	}
	if !strings.Contains(result, "3→line3") {
		t.Errorf("should contain last line: %s", result)
	}
}

func TestReadFileToolOffsetLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("a\nb\nc\nd\ne\n"), 0o644)

	rt := &ReadFileTool{}
	result, err := rt.Execute(context.Background(), json.RawMessage(`{"path":"`+path+`","offset":1,"limit":2}`))
	if err != nil {
		t.Fatalf("read_file offset: %v", err)
	}
	lines := strings.Count(result, "\n")
	if lines < 2 || lines > 3 {
		t.Errorf("offset+limit should return ~2 lines, got %d lines:\n%s", lines, result)
	}
}

func TestReadFileToolMissing(t *testing.T) {
	rt := &ReadFileTool{}
	_, err := rt.Execute(context.Background(), json.RawMessage(`{"path":"/tmp/does-not-exist-99999.txt"}`))
	if err == nil {
		t.Error("missing file should error")
	}
}

func TestWriteFileTool(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "output.txt")

	wt := &WriteFileTool{}
	result, err := wt.Execute(context.Background(), json.RawMessage(`{"path":"`+path+`","content":"hello lumen"}`))
	if err != nil {
		t.Fatalf("write_file: %v", err)
	}
	if !strings.Contains(result, "wrote") {
		t.Errorf("result should mention wrote, got %q", result)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "hello lumen" {
		t.Errorf("content mismatch: %q", string(data))
	}
}

func TestWriteFileToolCreatesDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "nested", "file.txt")

	wt := &WriteFileTool{}
	_, err := wt.Execute(context.Background(), json.RawMessage(`{"path":"`+path+`","content":"data"}`))
	if err != nil {
		t.Fatalf("write_file create dirs: %v", err)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "data" {
		t.Errorf("content mismatch: %q", string(data))
	}
}

func TestEditFileTool(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "edit.txt")
	os.WriteFile(path, []byte("Hello World\n"), 0o644)

	et := &EditFileTool{}
	result, err := et.Execute(context.Background(), json.RawMessage(`{"path":"`+path+`","old_string":"World","new_string":"Lumen"}`))
	if err != nil {
		t.Fatalf("edit_file: %v", err)
	}
	if !strings.Contains(result, "Replaced") {
		t.Errorf("result should mention Replaced, got %q", result)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "Hello Lumen\n" {
		t.Errorf("content mismatch: %q", string(data))
	}
}

func TestEditFileToolNotFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "edit.txt")
	os.WriteFile(path, []byte("Hello"), 0o644)

	et := &EditFileTool{}
	_, err := et.Execute(context.Background(), json.RawMessage(`{"path":"`+path+`","old_string":"xyz","new_string":"abc"}`))
	if err == nil {
		t.Error("nonexistent old_string should error")
	}
}

func TestEditFileToolDuplicate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "edit.txt")
	os.WriteFile(path, []byte("foo foo"), 0o644)

	et := &EditFileTool{}
	_, err := et.Execute(context.Background(), json.RawMessage(`{"path":"`+path+`","old_string":"foo","new_string":"bar"}`))
	if err == nil {
		t.Error("duplicate old_string should error")
	}
}

func TestGrepTool(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("package main\nfunc main() {}"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.go"), []byte("package test\nfunc test() {}"), 0o644)

	gt := &GrepTool{}
	result, err := gt.Execute(context.Background(), json.RawMessage(`{"pattern":"func","path":"`+dir+`"}`))
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	if !strings.Contains(result, "func main") && !strings.Contains(result, "func test") {
		t.Errorf("grep should find func declarations, got %q", result)
	}
	// Should match 2 files
	if strings.Count(result, "\n") != 1 {
		t.Logf("grep result: %q", result)
	}
}

func TestGlobTool(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte(""), 0o644)
	os.WriteFile(filepath.Join(dir, "util.go"), []byte(""), 0o644)
	os.WriteFile(filepath.Join(dir, "readme.md"), []byte(""), 0o644)

	gt := &GlobTool{}
	result, err := gt.Execute(context.Background(), json.RawMessage(`{"pattern":"`+dir+`/*.go"}`))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if !strings.Contains(result, "main.go") || !strings.Contains(result, "util.go") {
		t.Errorf("glob should find .go files, got %q", result)
	}
}

func TestLsTool(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644)
	os.Mkdir(filepath.Join(dir, "subdir"), 0o755)

	lt := &LsTool{}
	result, err := lt.Execute(context.Background(), json.RawMessage(`{"path":"`+dir+`"}`))
	if err != nil {
		t.Fatalf("ls: %v", err)
	}
	if !strings.Contains(result, "a.txt") {
		t.Errorf("ls should list files, got %q", result)
	}
	if !strings.Contains(result, "subdir/") {
		t.Errorf("ls should show dirs with /, got %q", result)
	}
}

func TestTodoWriteTool(t *testing.T) {
	tt := &TodoWriteTool{}
	result, err := tt.Execute(context.Background(), json.RawMessage(`{"todos":[{"content":"add tests","status":"in_progress","activeForm":"adding tests","level":0},{"content":"run tests","status":"pending","level":1}]}`))
	if err != nil {
		t.Fatalf("todo_write: %v", err)
	}
	if !strings.Contains(result, "add tests") {
		t.Errorf("todo_write should list task: %q", result)
	}
	if !strings.Contains(result, "◉") {
		t.Errorf("in_progress should show ◉: %q", result)
	}
}

func TestCompleteStepWithLedger(t *testing.T) {
	// Create a ledger with a successful bash receipt
	l := evidence.NewLedger()
	l.Record(evidence.Receipt{
		ToolName: "bash",
		Success:  true,
		ReadOnly: false,
		Command:  "go test ./...",
	})

	ctx := evidence.WithLedger(context.Background(), l)

	ct := &CompleteStepTool{}
	result, err := ct.Execute(ctx, json.RawMessage(`{"step":"add tests","result":"38 tests pass","evidence":[{"kind":"verification","summary":"tests pass","command":"go test ./..."}]}`))
	if err != nil {
		t.Fatalf("complete_step with evidence: %v", err)
	}
	if !strings.Contains(result, "completed") {
		t.Errorf("complete_step should confirm: %q", result)
	}
}

func TestCompleteStepWithLedgerNoWriter(t *testing.T) {
	l := evidence.NewLedger()
	// Only read-only tools — no writer
	l.Record(evidence.Receipt{ToolName: "read_file", Success: true, ReadOnly: true})

	ctx := evidence.WithLedger(context.Background(), l)

	ct := &CompleteStepTool{}
	_, err := ct.Execute(ctx, json.RawMessage(`{"step":"step","result":"done","evidence":[{"kind":"manual","summary":"checked"}]}`))
	if err == nil {
		t.Error("complete_step should reject when no writer tool ran")
	}
}

func TestCompleteStepWithLedgerMismatch(t *testing.T) {
	l := evidence.NewLedger()
	l.Record(evidence.Receipt{
		ToolName: "bash",
		Success:  true,
		ReadOnly: false,
		Command:  "go build",
	})

	ctx := evidence.WithLedger(context.Background(), l)

	ct := &CompleteStepTool{}
	// Cites a different command than what was run
	_, err := ct.Execute(ctx, json.RawMessage(`{"step":"step","result":"done","evidence":[{"kind":"verification","summary":"test","command":"go test ./..."}]}`))
	if err == nil {
		t.Error("complete_step should reject mismatched bash evidence")
	}
}

func TestCompleteStepHeadless(t *testing.T) {
	ct := &CompleteStepTool{}
	result, err := ct.Execute(context.Background(), json.RawMessage(`{"step":"manual step","result":"done manually","evidence":[{"kind":"manual","summary":"done by hand"}]}`))
	if err != nil {
		t.Fatalf("complete_step headless: %v", err)
	}
	if !strings.Contains(result, "completed") {
		t.Errorf("complete_step headless: %q", result)
	}
}

func TestCompleteStepNoEvidence(t *testing.T) {
	ct := &CompleteStepTool{}
	_, err := ct.Execute(context.Background(), json.RawMessage(`{"step":"step","result":"done","evidence":[]}`))
	if err == nil {
		t.Error("complete_step should require at least one evidence item")
	}
}

func TestAskTool(t *testing.T) {
	at := &AskTool{}
	result, err := at.Execute(context.Background(), json.RawMessage(`{"questions":[{"header":"Q","question":"what?","options":[{"label":"A"}]}]}`))
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	if !strings.Contains(result, "[ask tool called") {
		t.Errorf("ask in headless mode should return placeholder, got %q", result)
	}
}
