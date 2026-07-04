package lumenstore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSessionMessagesRoundTripAndMigrate(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sess.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	sid := "2026-07-04-120000"
	p1 := `{"role":"user","content":"hi"}`
	p2 := `{"role":"assistant","content":"hello"}`
	if err := db.AppendSessionMessage(sid, 0, "user", []byte(p1)); err != nil {
		t.Fatal(err)
	}
	if err := db.AppendSessionMessage(sid, 1, "assistant", []byte(p2)); err != nil {
		t.Fatal(err)
	}
	rows, err := db.LoadSessionMessages(sid)
	if err != nil || len(rows) != 2 {
		t.Fatalf("load: len=%d err=%v", len(rows), err)
	}
	if string(rows[0]) != p1 || string(rows[1]) != p2 {
		t.Fatalf("payload mismatch: %q %q", rows[0], rows[1])
	}

	hist := filepath.Join(dir, "history")
	if err := os.MkdirAll(hist, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hist, sid+".jsonl"), []byte(p1+"\n"+p2+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	n, err := MigrateJSONLSessions(db, hist)
	if err != nil || n < 2 {
		t.Fatalf("migrate n=%d err=%v", n, err)
	}
	cnt, _ := db.CountSessionMessages(sid)
	if cnt < 2 {
		t.Fatalf("count=%d", cnt)
	}

	// Single-file migrate API
	n2, err := MigrateJSONLSessionFile(db, filepath.Join(hist, sid+".jsonl"))
	if err != nil || n2 < 2 {
		t.Fatalf("MigrateJSONLSessionFile n=%d err=%v", n2, err)
	}
}
