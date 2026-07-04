package audit

// store.go adds disk-backed (JSONL) persistence on top of the in-memory Trail,
// plus a package-level default store and a Record() convenience the agent loop
// can call in one line to answer "why did the agent run this tool?".
//
// See docs/threat-model.md §7 (G5).

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"lumen/internal/lumenstore"
)

// maxArgLen bounds how much of a tool's args we persist, so a giant payload
// can't bloat the log (and to keep lines readable). Truncated args get a marker.
const maxArgLen = 4096

const (
	// EnvAudit toggles the default audit store. Set to off/0/false/none to
	// disable persistence entirely (no file is written).
	EnvAudit = "LUMEN_AUDIT"
	// EnvAuditLog overrides the JSONL path (default: ~/.lumen/audit.jsonl).
	EnvAuditLog = "LUMEN_AUDIT_LOG"
)

// ToolCall is one tool execution to record. Why is the model's stated reason for
// the call (when available); Args is the JSON arguments; Result is a short
// summary or error string; OK distinguishes success from failure.
type ToolCall struct {
	Session string
	Tool    string
	Why     string
	Args    string
	Result  string
	OK      bool
}

// Store is a Trail with optional append-only JSONL persistence.
type Store struct {
	mu    sync.Mutex
	trail *Trail
	f     *os.File
	path  string
}

// NewStore opens (creating as needed) a JSONL-backed audit store at path. If
// path is empty the store is in-memory only. Existing entries at path are loaded
// so queries survive restarts. A nil/zero file (e.g. unwritable path) degrades
// to memory-only rather than failing.
func NewStore(path string) *Store {
	s := &Store{trail: NewTrail(0), path: path}
	if path == "" {
		return s
	}
	if dir := filepath.Dir(path); dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}
	// Load any prior entries first (preserving their original hashes), then open
	// for appending.
	s.load(path)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err == nil {
		s.f = f
	}
	return s
}

// load reads an existing JSONL file into the trail without recomputing hashes,
// so a reopened store keeps a verifiable chain.
func (s *Store) load(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e Entry
		if json.Unmarshal(line, &e) != nil {
			continue // skip a corrupt line rather than abort the whole load
		}
		ee := e
		s.trail.restore(&ee)
	}
}

// RecordToolCall appends a tool-call entry to the trail and (if persistent)
// writes one JSONL line. Safe on a nil store (returns nil — auditing disabled).
func (s *Store) RecordToolCall(tc ToolCall) *Entry {
	if s == nil {
		return nil
	}
	result := "success"
	if !tc.OK {
		result = "failure"
	}
	actor := tc.Session
	if actor == "" {
		actor = "agent"
	}
	details := map[string]any{
		"why":    tc.Why,
		"args":   truncate(tc.Args),
		"result": truncate(tc.Result),
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.trail.Record(actor, tc.Tool, tc.Tool, result, details)
	if s.f != nil {
		if b, err := json.Marshal(e); err == nil {
			s.f.Write(append(b, '\n'))
		}
	}
	if db := lumenstore.Default(); db != nil {
		_ = db.InsertAudit(tc.Session, tc.Tool, tc.OK, details)
	}
	return e
}

// Query proxies to the underlying trail. Safe on a nil store (returns nil).
func (s *Store) Query(actor, action, resource string, since, until time.Time) []*Entry {
	if s == nil {
		return nil
	}
	return s.trail.Query(actor, action, resource, since, until)
}

// Verify reports whether the persisted hash chain is intact.
func (s *Store) Verify() (bool, []string) {
	if s == nil {
		return true, nil
	}
	return s.trail.Verify()
}

// Path returns the JSONL path ("" for memory-only).
func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// Close releases the file handle.
func (s *Store) Close() error {
	if s == nil || s.f == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.f.Close()
	s.f = nil
	return err
}

func truncate(s string) string {
	if len(s) <= maxArgLen {
		return s
	}
	return s[:maxArgLen] + "…[truncated]"
}

// ── Package-level default store ──────────────────────────────────────────────

var (
	defStore *Store
	defOnce  sync.Once
)

// storeConfig resolves the default store's path and whether auditing is enabled.
func storeConfig() (path string, enabled bool) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvAudit))) {
	case "off", "0", "false", "none", "disabled":
		return "", false
	}
	if p := strings.TrimSpace(os.Getenv(EnvAuditLog)); p != "" {
		return p, true
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(os.TempDir(), "lumen-audit.jsonl"), true
	}
	return filepath.Join(home, ".lumen", "audit.jsonl"), true
}

// Default returns the process-wide audit store, initialized from the
// environment on first use. When auditing is disabled it returns a nil *Store,
// which makes Record/Query safe no-ops.
func Default() *Store {
	defOnce.Do(func() {
		path, enabled := storeConfig()
		if !enabled {
			return // defStore stays nil → no-op
		}
		defStore = NewStore(path)
	})
	return defStore
}

// Record appends a tool-call entry to the default audit store. This is the
// one-line hook the agent loop (owned by the system/S2 track) calls per tool
// execution:
//
//	audit.Record(audit.ToolCall{Tool: name, Why: reason, Args: string(args), Result: summary, OK: err == nil})
func Record(tc ToolCall) *Entry {
	return Default().RecordToolCall(tc)
}
