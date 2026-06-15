package archive

import (
	"testing"
)

func TestTarGz(t *testing.T) {
	a := NewArchiver()
	data, err := a.CreateTarGz([]Entry{{Name: "f.txt", Data: []byte("x")}})
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Error("empty")
	}
}
func TestZip(t *testing.T) {
	a := NewArchiver()
	data, _ := a.CreateZip([]Entry{{Name: "f.txt", Data: []byte("x")}})
	if len(data) == 0 {
		t.Error("empty")
	}
}
func TestExtract(t *testing.T) {
	a := NewArchiver()
	data, _ := a.CreateTarGz([]Entry{{Name: "f.txt", Data: []byte("hello world")}})
	ext, _ := a.ExtractTarGz(data, "")
	if len(ext) != 1 {
		t.Error("count")
	}
}
func TestList(t *testing.T) {
	a := NewArchiver()
	data, _ := a.CreateTarGz([]Entry{{Name: "a.txt", Data: []byte("x")}})
	s := a.ListArchive(data)
	if s == "" {
		t.Error("list")
	}
}
func TestSnapshot(t *testing.T) {
	s := NewSnapshot("/tmp")
	s.AddFile("a", []byte("x"))
	if s.Count() != 1 {
		t.Error("count")
	}
}
