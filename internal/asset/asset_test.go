package asset

import (
	"testing"
)

func TestStore_PutAndGet(t *testing.T) {
	s := NewStore()

	data := []byte("hello world")
	a, err := s.Put("greeting.txt", data, "text/plain", []string{"text", "greeting"}, false)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if a.Name != "greeting.txt" {
		t.Fatalf("expected name 'greeting.txt', got %q", a.Name)
	}

	got, _, err := s.Get(a.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got) != "hello world" {
		t.Fatalf("expected 'hello world', got %q", string(got))
	}
}

func TestStore_Deduplication(t *testing.T) {
	s := NewStore()

	data := []byte("duplicate content")
	a1, _ := s.Put("first.txt", data, "text/plain", []string{"a"}, false)
	a2, _ := s.Put("second.txt", data, "text/plain", []string{"b"}, false)

	if a1.ID != a2.ID {
		t.Fatal("expected same ID for duplicate content")
	}

	// Should only have one asset stored.
	stats := s.Stats()
	if stats["total_assets"] != 1 {
		t.Fatalf("expected 1 asset, got %d", stats["total_assets"])
	}
	if stats["dedup_saved"] == 0 {
		t.Fatal("expected dedup savings")
	}
}

func TestStore_Compression(t *testing.T) {
	s := NewStore()

	// Create compressible data (repeating pattern).
	data := make([]byte, 10000)
	for i := range data {
		data[i] = byte('A' + i%26)
	}

	a, err := s.Put("big.txt", data, "text/plain", nil, true)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if !a.Compressed {
		t.Fatal("expected compressed asset")
	}
	if a.StoredSize >= a.Size {
		t.Fatalf("expected stored size (%d) < original size (%d)", a.StoredSize, a.Size)
	}

	// Get should decompress.
	got, _, err := s.Get(a.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got) != string(data) {
		t.Fatal("round-trip compression failed")
	}
}

func TestStore_TagLookup(t *testing.T) {
	s := NewStore()

	s.Put("a.txt", []byte("a"), "text/plain", []string{"alpha", "common"}, false)
	s.Put("b.txt", []byte("b"), "text/plain", []string{"beta", "common"}, false)
	s.Put("c.txt", []byte("c"), "text/plain", []string{"gamma"}, false)

	common := s.ListByTag("common")
	if len(common) != 2 {
		t.Fatalf("expected 2 common assets, got %d", len(common))
	}

	both := s.ListByTags([]string{"alpha", "common"})
	if len(both) != 1 {
		t.Fatalf("expected 1 asset with both tags, got %d", len(both))
	}
}

func TestStore_Delete(t *testing.T) {
	s := NewStore()

	a, _ := s.Put("del.txt", []byte("x"), "text/plain", []string{"temp"}, false)

	if err := s.Delete(a.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, _, err := s.Get(a.ID)
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestStore_AddRemoveTags(t *testing.T) {
	s := NewStore()

	a, _ := s.Put("tagged.txt", []byte("x"), "text/plain", []string{"initial"}, false)

	s.AddTags(a.ID, []string{"newtag"})
	_, a2, _ := s.Get(a.ID)
	found := false
	for _, tag := range a2.Tags {
		if tag == "newtag" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected 'newtag' to be added")
	}

	s.RemoveTags(a.ID, []string{"initial"})
	_, a2, _ = s.Get(a.ID)
	for _, tag := range a2.Tags {
		if tag == "initial" {
			t.Fatal("expected 'initial' to be removed")
		}
	}
}

func TestStore_FormatStore(t *testing.T) {
	s := NewStore()
	s.Put("f1.txt", []byte("data"), "text/plain", nil, false)

	out := s.FormatStore()
	if out == "" {
		t.Fatal("expected non-empty format")
	}
}
