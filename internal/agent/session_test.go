package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"lumen/internal/lumenstore"
	"lumen/internal/provider"
)

func TestCompactRewritesPersistedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	s := NewSession(path)
	for i := 0; i < 10; i++ {
		s.Add(provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("m%d", i)})
	}
	s.Compact(2, 2, "summary")
	want := s.Len() // 2 + marker + 2 = 5 in memory

	// Reloading from disk must yield the compacted history (first 2 + marker +
	// last 2) in order — not the original 10, and not a scrambled set.
	reloaded := NewSession(path)
	if reloaded.Len() != want {
		t.Fatalf("reloaded %d messages from file, want %d (file must match compacted memory)", reloaded.Len(), want)
	}
	got := reloaded.Snapshot()
	wantContent := []string{"m0", "m1", "[SESSION COMPACTED]\n\nsummary", "m8", "m9"}
	for i, wc := range wantContent {
		if got[i].Content != wc {
			t.Errorf("reloaded msg[%d] = %q, want %q (compaction scrambled order/content)", i, got[i].Content, wc)
		}
	}
}

func TestSessionAddRecordsPersistError(t *testing.T) {
	// Parent dir does not exist, so the append must fail. The session must record
	// that failure instead of silently dropping persisted turns.
	s := NewSession(filepath.Join(t.TempDir(), "missing", "s.jsonl"))
	s.Add(provider.Message{Role: provider.RoleUser, Content: "hi"})
	if s.PersistErr() == nil {
		t.Fatal("Add should record a persistence error when the file can't be written")
	}
}

func TestSessionAdd(t *testing.T) {
	s := NewSession("")
	if s.Len() != 0 {
		t.Error("new session should be empty")
	}

	s.Add(provider.Message{Role: provider.RoleSystem, Content: "you are a bot"})
	if s.Len() != 1 {
		t.Errorf("expected 1 message, got %d", s.Len())
	}

	s.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	if s.Len() != 2 {
		t.Errorf("expected 2 messages, got %d", s.Len())
	}
}

func TestSessionSnapshot(t *testing.T) {
	s := NewSession("")
	s.Add(provider.Message{Role: provider.RoleSystem, Content: "sys"})
	s.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})

	snap := s.Snapshot()
	if len(snap) != 2 {
		t.Errorf("expected 2 messages in snapshot, got %d", len(snap))
	}
	if snap[0].Content != "sys" {
		t.Errorf("expected first message 'sys', got %q", snap[0].Content)
	}

	// Snapshot should be a copy — modifying it doesn't affect the session
	snap[0] = provider.Message{Role: provider.RoleUser, Content: "mutated"}
	snap2 := s.Snapshot()
	if snap2[0].Content != "sys" {
		t.Error("snapshot should return a copy, not the original slice")
	}
}

func TestSessionCompactSQLiteDualWrite(t *testing.T) {
	lumenstore.ResetDefaultForTest()
	t.Cleanup(lumenstore.ResetDefaultForTest)

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "lumen.db")
	t.Setenv(lumenstore.EnvSQLite, dbPath)

	path := filepath.Join(dir, "compact.jsonl")
	s := NewSession(path)
	for i := 0; i < 10; i++ {
		s.Add(provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("m%d", i)})
	}
	s.Compact(2, 2, "mid summary")
	if s.Len() != 5 {
		t.Fatalf("after compact memory len=%d want 5", s.Len())
	}

	db, err := lumenstore.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sid := lumenstore.SessionIDFromPath(path)
	cnt, err := db.CountSessionMessages(sid)
	if err != nil || cnt != 5 {
		t.Fatalf("compact sqlite count=%d err=%v", cnt, err)
	}
	t.Logf("sqlite-session-evidence: compact_sqlite_count=%d", cnt)
}

func TestSessionCompactTooSmallSQLiteNoOp(t *testing.T) {
	lumenstore.ResetDefaultForTest()
	t.Cleanup(lumenstore.ResetDefaultForTest)

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "lumen.db")
	t.Setenv(lumenstore.EnvSQLite, dbPath)

	path := filepath.Join(dir, "small.jsonl")
	s := NewSession(path)
	s.Add(provider.Message{Role: provider.RoleUser, Content: "1"})
	s.Add(provider.Message{Role: provider.RoleUser, Content: "2"})
	s.Add(provider.Message{Role: provider.RoleUser, Content: "3"})
	s.Compact(2, 2, "summary")
	if s.Len() != 3 {
		t.Fatalf("compact should no-op when too small: len=%d", s.Len())
	}

	db, err := lumenstore.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cnt, _ := db.CountSessionMessages(lumenstore.SessionIDFromPath(path))
	if cnt != 3 {
		t.Fatalf("sqlite count after no-op compact=%d want 3", cnt)
	}
}

func TestSessionSQLiteDualWriteAndReload(t *testing.T) {
	lumenstore.ResetDefaultForTest()
	t.Cleanup(lumenstore.ResetDefaultForTest)

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "lumen.db")
	t.Setenv(lumenstore.EnvSQLite, dbPath)

	path := filepath.Join(dir, "sess.jsonl")
	s := NewSession(path)
	s.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "hi there"})

	db, err := lumenstore.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sid := lumenstore.SessionIDFromPath(path)
	cnt, err := db.CountSessionMessages(sid)
	if err != nil || cnt != 2 {
		t.Fatalf("sqlite count=%d err=%v", cnt, err)
	}

	reloaded := NewSession(path)
	if reloaded.Len() != 2 {
		t.Fatalf("reloaded %d messages, want 2", reloaded.Len())
	}
	got := reloaded.Snapshot()
	if got[0].Content != "hello" || got[1].Content != "hi there" {
		t.Fatalf("sqlite reload mismatch: %+v", got)
	}
}

func TestSessionAutoMigrateJSONLToSQLite(t *testing.T) {
	lumenstore.ResetDefaultForTest()
	t.Cleanup(lumenstore.ResetDefaultForTest)

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "lumen.db")
	t.Setenv(lumenstore.EnvSQLite, dbPath)

	path := filepath.Join(dir, "legacy.jsonl")
	lines := `{"role":"user","content":"from-jsonl"}
{"role":"assistant","content":"migrated"}
`
	if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}

	s := NewSession(path)
	if s.Len() != 2 {
		t.Fatalf("load from jsonl: got %d messages", s.Len())
	}

	db, err := lumenstore.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sid := lumenstore.SessionIDFromPath(path)
	cnt, err := db.CountSessionMessages(sid)
	if err != nil || cnt != 2 {
		t.Fatalf("after auto-migrate count=%d err=%v", cnt, err)
	}

	// Second load should prefer SQLite (delete jsonl to prove source).
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	reloaded := NewSession(path)
	if reloaded.Len() != 2 {
		t.Fatalf("sqlite-only reload: got %d messages", reloaded.Len())
	}
}

func TestSessionDropLastSyncsSQLite(t *testing.T) {
	lumenstore.ResetDefaultForTest()
	t.Cleanup(lumenstore.ResetDefaultForTest)

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "lumen.db")
	t.Setenv(lumenstore.EnvSQLite, dbPath)

	path := filepath.Join(dir, "drop.jsonl")
	s := NewSession(path)
	s.Add(provider.Message{Role: provider.RoleUser, Content: "a"})
	s.Add(provider.Message{Role: provider.RoleUser, Content: "b"})
	s.DropLast()

	db, err := lumenstore.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sid := lumenstore.SessionIDFromPath(path)
	cnt, err := db.CountSessionMessages(sid)
	if err != nil || cnt != 1 {
		t.Fatalf("after drop sqlite count=%d err=%v", cnt, err)
	}
	rows, err := db.LoadSessionMessages(sid)
	if err != nil || len(rows) != 1 {
		t.Fatalf("load rows=%d err=%v", len(rows), err)
	}
	var m provider.Message
	if json.Unmarshal(rows[0], &m) != nil || m.Content != "a" {
		t.Fatalf("sqlite payload after drop: %q", rows[0])
	}

	reloaded := NewSession(path)
	if reloaded.Len() != 1 || reloaded.Snapshot()[0].Content != "a" {
		t.Fatalf("reload after drop: %+v", reloaded.Snapshot())
	}
}

// TestSessionSQLiteMutationMatrix is the authoritative AC3 proof: Add, Compact,
// and DropLast all keep SQLite + JSONL aligned through reload.
func TestSessionSQLiteMutationMatrix(t *testing.T) {
	lumenstore.ResetDefaultForTest()
	t.Cleanup(lumenstore.ResetDefaultForTest)

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "lumen.db")
	t.Setenv(lumenstore.EnvSQLite, dbPath)

	path := filepath.Join(dir, "matrix.jsonl")
	s := NewSession(path)
	for i := 0; i < 10; i++ {
		s.Add(provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("m%d", i)})
	}

	db, err := lumenstore.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sid := lumenstore.SessionIDFromPath(path)

	assertCount := func(want int64, step string) {
		t.Helper()
		cnt, err := db.CountSessionMessages(sid)
		if err != nil || cnt != want {
			t.Fatalf("%s: sqlite count=%d want %d err=%v", step, cnt, want, err)
		}
	}
	assertCount(10, "after add×10")
	t.Logf("sqlite-session-evidence: after_add sqlite_count=10")

	s.Compact(2, 2, "summary")
	assertCount(5, "after compact")
	t.Logf("sqlite-session-evidence: after_compact sqlite_count=5")

	reloaded := NewSession(path)
	if reloaded.Len() != 5 {
		t.Fatalf("reload after compact: len=%d want 5", reloaded.Len())
	}
	wantContent := []string{"m0", "m1", "[SESSION COMPACTED]\n\nsummary", "m8", "m9"}
	got := reloaded.Snapshot()
	for i, wc := range wantContent {
		if got[i].Content != wc {
			t.Fatalf("reload compacted msg[%d]=%q want %q", i, got[i].Content, wc)
		}
	}
	t.Logf("sqlite-session-evidence: after_compact reload_len=5 content_first=%q content_last=%q", got[0].Content, got[4].Content)

	s = reloaded
	s.DropLast()
	assertCount(4, "after drop last")
	t.Logf("sqlite-session-evidence: after_drop sqlite_count=4")

	reloaded2 := NewSession(path)
	if reloaded2.Len() != 4 {
		t.Fatalf("reload after drop: len=%d want 4", reloaded2.Len())
	}
	if reloaded2.Snapshot()[3].Content != "m8" {
		t.Fatalf("last msg after drop: %+v", reloaded2.Snapshot())
	}
}

func TestSessionLoadUnreadableJSONLPreservesSQLite(t *testing.T) {
	lumenstore.ResetDefaultForTest()
	t.Cleanup(lumenstore.ResetDefaultForTest)

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "lumen.db")
	t.Setenv(lumenstore.EnvSQLite, dbPath)

	path := filepath.Join(dir, "unreadable.jsonl")
	line := `{"role":"user","content":"keep-me"}`
	if err := os.WriteFile(path, []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := lumenstore.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sid := lumenstore.SessionIDFromPath(path)
	if err := db.AppendSessionMessage(sid, 0, "user", []byte(line)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	s := NewSession(path)
	if s.Len() != 1 || s.Snapshot()[0].Content != "keep-me" {
		t.Fatalf("unreadable jsonl must fall back to sqlite: %+v", s.Snapshot())
	}
	cnt, _ := db.CountSessionMessages(sid)
	if cnt != 1 {
		t.Fatalf("sqlite must not be cleared on read error: count=%d", cnt)
	}
	t.Logf("sqlite-session-evidence: unreadable_jsonl preserved sqlite count=%d", cnt)
}

func TestSessionLoadEmptyJSONLClearsStaleSQLite(t *testing.T) {
	lumenstore.ResetDefaultForTest()
	t.Cleanup(lumenstore.ResetDefaultForTest)

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "lumen.db")
	t.Setenv(lumenstore.EnvSQLite, dbPath)

	path := filepath.Join(dir, "stale.jsonl")
	db, err := lumenstore.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sid := lumenstore.SessionIDFromPath(path)
	p1 := `{"role":"user","content":"stale"}`
	if err := db.AppendSessionMessage(sid, 0, "user", []byte(p1)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	s := NewSession(path)
	if s.Len() != 0 {
		t.Fatalf("empty jsonl must not load stale sqlite: len=%d", s.Len())
	}
	cnt, _ := db.CountSessionMessages(sid)
	if cnt != 0 {
		t.Fatalf("stale sqlite must be cleared: count=%d", cnt)
	}
	t.Logf("sqlite-session-evidence: empty_jsonl cleared stale sqlite count=0")
}

func TestSessionConcurrentAddPreservesSQLiteSeq(t *testing.T) {
	lumenstore.ResetDefaultForTest()
	t.Cleanup(lumenstore.ResetDefaultForTest)

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "lumen.db")
	t.Setenv(lumenstore.EnvSQLite, dbPath)

	path := filepath.Join(dir, "conc.jsonl")
	s := NewSession(path)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			s.Add(provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("m%d", n)})
		}(i)
	}
	wg.Wait()

	if s.Len() != 20 {
		t.Fatalf("memory len=%d want 20", s.Len())
	}

	db, err := lumenstore.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sid := lumenstore.SessionIDFromPath(path)
	cnt, _ := db.CountSessionMessages(sid)
	if cnt != 20 {
		t.Fatalf("sqlite count=%d want 20", cnt)
	}
}

func TestSessionSystemPrompt(t *testing.T) {
	s := NewSession("")

	prompt := s.SystemPrompt("You are a bot.", "Project memory here.")
	if len(prompt) == 0 {
		t.Error("SystemPrompt should return non-empty string")
	}
	if !strings.HasPrefix(prompt, "You are a bot.") {
		t.Errorf("prompt should start with base prompt, got %q", prompt[:min(30, len(prompt))])
	}
}
