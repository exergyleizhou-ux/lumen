package editverify

import (
	"strings"
	"testing"
)

// tsc reports diagnostics as `file.ts(line,col): error TSxxxx: msg` — a paren
// format the Go-style `file:line:col:` matcher misses. Parse must extract it so
// the TypeScript self-repair feedback is structured like Go's, not just raw text.
func TestParse_typescriptDiagnostic(t *testing.T) {
	out := "src/app.ts(3,5): error TS2304: Cannot find name 'foo'.\n" +
		"src/util.tsx(10,1): error TS1005: ';' expected."
	diags := Parse(Step{Name: "typecheck"}, out)
	if len(diags) != 2 {
		t.Fatalf("expected 2 diagnostics, got %d: %+v", len(diags), diags)
	}
	d := diags[0]
	if d.File != "src/app.ts" || d.Line != 3 || d.Col != 5 {
		t.Errorf("file/line/col = %q/%d/%d, want src/app.ts/3/5", d.File, d.Line, d.Col)
	}
	if !strings.Contains(d.Msg, "TS2304") {
		t.Errorf("msg should retain the TS error code, got %q", d.Msg)
	}
	if diags[1].File != "src/util.tsx" || diags[1].Line != 10 || diags[1].Col != 1 {
		t.Errorf("second diag = %q/%d/%d, want src/util.tsx/10/1", diags[1].File, diags[1].Line, diags[1].Col)
	}
}
