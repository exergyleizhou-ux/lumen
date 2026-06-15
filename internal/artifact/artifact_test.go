package artifact

import (
	"testing"
)

func TestNewArtifact(t *testing.T) {
	a := NewArtifact("app", "1.0", "/path", []byte("content"))
	if a.Name != "app" {
		t.Error("name")
	}
	if len(a.Checksum) != 64 {
		t.Error("checksum")
	}
}
func TestRegistry(t *testing.T) {
	r := NewRegistry()
	a := NewArtifact("lib", "0.1", "/lib", []byte("data"))
	r.Register(a)
	got, ok := r.Get("lib", "0.1")
	if !ok || got.Size != 4 {
		t.Error("get")
	}
}
func TestTags(t *testing.T) {
	a := NewArtifact("img", "2.0", "/img", []byte("x"))
	a.AddTag("latest")
	a.AddTag("prod")
	if !a.HasTag("latest") {
		t.Error("has tag")
	}
}
func TestPromoteArchive(t *testing.T) {
	a := NewArtifact("svc", "1.0", "/svc", []byte("y"))
	a.Promote()
	a.Archive()
	if !a.IsArchived() {
		t.Error("archived")
	}
}
func TestFormat(t *testing.T) {
	r := NewRegistry()
	r.Register(NewArtifact("a", "1.0", "/a", []byte("x")))
	if r.FormatArtifacts() == "" {
		t.Error("format")
	}
}
