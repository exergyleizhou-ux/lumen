package toolkit

import (
	"strings"
	"testing"
)

func TestJSONToolParse(t *testing.T) {
	j := NewJSONTool()
	v, err := j.Parse(`{"a":1}`)
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	if m["a"].(float64) != 1 {
		t.Error("parse")
	}
}
func TestJSONToolGet(t *testing.T) {
	j := NewJSONTool()
	v, _ := j.Parse(`{"a":{"b":2}}`)
	val, ok := j.Get(v, "a.b")
	if !ok || val != float64(2) {
		t.Error("get")
	}
}
func TestJSONToolSet(t *testing.T) {
	j := NewJSONTool()
	v := map[string]any{"a": map[string]any{"b": 1}}
	r, _ := j.Set(v, "a.b", 2)
	m := r.(map[string]any)["a"].(map[string]any)
	if m["b"].(int) != 2 {
		t.Error("set")
	}
}
func TestStringSlug(t *testing.T) {
	s := NewStringTool()
	if s.Slug("Hello World!") != "hello-world" {
		t.Error("slug")
	}
}
func TestEncodeBase64(t *testing.T) {
	e := NewEncodeTool()
	enc := e.Base64Encode("hello")
	dec, _ := e.Base64Decode(enc)
	if dec != "hello" {
		t.Error("base64")
	}
}
func TestMD5(t *testing.T) {
	e := NewEncodeTool()
	if len(e.MD5("test")) != 32 {
		t.Error("md5 length")
	}
}
func TestDiff(t *testing.T) {
	d := NewDiffTool()
	res := d.Diff("a\nb", "a\nc")
	formatted := d.FormatDiff(res)
	if !strings.Contains(formatted, "+ c") {
		t.Error("diff add")
	}
	if !strings.Contains(formatted, "- b") {
		t.Error("diff remove")
	}
}
func TestFileTool(t *testing.T) {
	ft := NewFileTool()
	info := ft.Info("/path/to/file.txt")
	if info.Ext != "txt" {
		t.Error("ext")
	}
}
func TestSummaryTool(t *testing.T) {
	st := NewSummaryTool()
	stats := st.Analyze("hello world hello")
	if stats.Words != 3 {
		t.Error("words")
	}
	if stats.UniqueWords != 2 {
		t.Error("unique")
	}
}
func TestRegistry(t *testing.T) {
	tr := NewToolRegistry()
	tr.Register("json", NewJSONTool())
	if _, ok := tr.Get("json"); !ok {
		t.Error("get")
	}
	if len(tr.Names()) != 1 {
		t.Error("names")
	}
}
