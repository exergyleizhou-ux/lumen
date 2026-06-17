package oasis

import (
	"context"
	"errors"
	"testing"
)

type fakeSandbox struct {
	stdout []byte
	logs   string
	err    error
}

func (f fakeSandbox) Run(ctx context.Context, image, dataDir, outDir string) ([]byte, string, error) {
	return f.stdout, f.logs, f.err
}

func TestCheckContract(t *testing.T) {
	ctx := context.Background()

	if r := CheckContract(ctx, fakeSandbox{stdout: []byte(`{"accuracy":0.9}`)}, "img", "metrics", nil); !r.OK {
		t.Errorf("a compliant algorithm should pass, got %v", r.Violations)
	}
	if r := CheckContract(ctx, fakeSandbox{err: errors.New("exit status 1"), logs: "boom"}, "img", "metrics", nil); r.OK {
		t.Error("a run that errors under isolation must be a violation")
	}
	if r := CheckContract(ctx, fakeSandbox{stdout: []byte("not json")}, "img", "model", nil); r.OK {
		t.Error("invalid output.json must be a violation")
	}
	if r := CheckContract(ctx, fakeSandbox{stdout: []byte(``)}, "img", "model", nil); r.OK {
		t.Error("empty output must be a violation")
	}
}

func TestCheckOutput(t *testing.T) {
	cases := []struct {
		name    string
		data    string
		kind    string
		wantErr bool
	}{
		{"valid model object", `{"weights":[1,2,3]}`, "model", false},
		{"valid metrics object", `{"accuracy":0.9}`, "metrics", false},
		{"empty output", ``, "model", true},
		{"whitespace only", "  \n ", "model", true},
		{"not json", `not json at all`, "model", true},
		{"model kind but array", `[1,2,3]`, "model", true},
		{"bytes kind allows non-object", `42`, "bytes", false},
		{"no kind allows any valid json", `"hello"`, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			errs := CheckOutput([]byte(c.data), c.kind)
			if c.wantErr && len(errs) == 0 {
				t.Errorf("expected a contract violation for %q (kind=%s)", c.data, c.kind)
			}
			if !c.wantErr && len(errs) != 0 {
				t.Errorf("expected no violation for %q (kind=%s), got %v", c.data, c.kind, errs)
			}
		})
	}
}
