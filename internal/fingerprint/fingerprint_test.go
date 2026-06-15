package fingerprint

import (
	"testing"
)

func TestSHA256(t *testing.T) {
	fp := SHA256([]byte("test"))
	if len(fp.Hash) != 64 {
		t.Error("hash length")
	}
}
func TestRegistry(t *testing.T) {
	r := NewRegistry()
	fp := SHA256([]byte("data"))
	if !r.Add("d1", fp) {
		t.Error("add")
	}
	if r.Add("d2", fp) {
		t.Error("dup")
	}
	if r.Count() != 1 {
		t.Error("count")
	}
}
func TestFormat(t *testing.T) {
	r := NewRegistry()
	r.Add("d", SHA256([]byte("x")))
	if r.FormatRegistry() == "" {
		t.Error("format")
	}
}
