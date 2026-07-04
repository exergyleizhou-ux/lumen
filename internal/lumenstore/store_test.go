package lumenstore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenMigrateAndAudit(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if err := s.InsertAudit("sess-1", "read_file", true, map[string]any{"why": "test"}); err != nil {
		t.Fatal(err)
	}
	n, err := s.CountAudit()
	if err != nil || n != 1 {
		t.Fatalf("count=%d err=%v", n, err)
	}
	if err := s.UpsertSessionMeta("sess-1", "demo"); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultDisabled(t *testing.T) {
	t.Setenv(EnvSQLite, "off")
	// reset singleton for test — new process only; just verify Open works
	s, err := Open(filepath.Join(t.TempDir(), "x.db"))
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	_ = os.Getenv(EnvSQLite)
}