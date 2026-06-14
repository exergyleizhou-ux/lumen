package diff

import "testing"

func TestChangeNew(t *testing.T) {
	c := Change{
		Path:   "/tmp/new.go",
		Before: "",
		After:  "package main",
		New:    true,
	}
	if !c.New {
		t.Error("Change.New should be true for new files")
	}
	if c.Path != "/tmp/new.go" {
		t.Errorf("path: want /tmp/new.go, got %s", c.Path)
	}
}

func TestChangeRemoved(t *testing.T) {
	c := Change{
		Path:    "/tmp/old.go",
		Before:  "package main",
		After:   "",
		Removed: true,
	}
	if !c.Removed {
		t.Error("Change.Removed should be true for deleted files")
	}
}

func TestChangeBinary(t *testing.T) {
	c := Change{
		Path:   "/tmp/image.png",
		Binary: true,
	}
	if !c.Binary {
		t.Error("Change.Binary should be true for binary files")
	}
}
