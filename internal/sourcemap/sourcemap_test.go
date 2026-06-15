package sourcemap

import (
	"testing"
)

func TestNewSourceMap(t *testing.T) {
	sm := NewSourceMap("bundle.js")
	sm.AddMapping(1, 0, 5, 3, "app.ts", "main")
	sm.AddMapping(2, 4, 6, 10, "lib.ts", "helper")
	if len(sm.Mappings) != 2 {
		t.Error("mappings count")
	}
	m := sm.Lookup(1, 5)
	if m == nil || m.Source != "app.ts" {
		t.Error("lookup")
	}
}
func TestToJSON(t *testing.T) {
	sm := NewSourceMap("out.js")
	sm.AddMapping(1, 0, 10, 0, "in.ts", "fn")
	j, err := sm.ToJSON()
	if err != nil {
		t.Fatal(err)
	}
	if len(j) == 0 {
		t.Error("json output")
	}
}
func TestFormatSummary(t *testing.T) {
	sm := NewSourceMap("file.js")
	sm.AddMapping(1, 0, 2, 0, "src.ts", "")
	if sm.FormatSummary() == "" {
		t.Error("format")
	}
}
