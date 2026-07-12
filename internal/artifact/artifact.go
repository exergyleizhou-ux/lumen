// Package artifact owns durable metadata and adapter-compatible object bytes.
package artifact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"lumen/internal/runstate"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

var ErrNotFound = errors.New("artifact not found")

type Record struct {
	ID         string         `json:"id"`
	RunID      string         `json:"run_id"`
	StepID     string         `json:"step_id,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	Owner      runstate.Owner `json:"owner"`
	Name       string         `json:"name"`
	Path       string         `json:"path,omitempty"`
	ObjectKey  string         `json:"object_key"`
	SHA256     string         `json:"sha256"`
	MIME       string         `json:"mime"`
	Size       int64          `json:"size"`
	Model      string         `json:"model,omitempty"`
	InputRefs  []string       `json:"input_refs,omitempty"`
	Provenance map[string]any `json:"provenance,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
}
type Store interface {
	Create(Record) error
	ListRun(runstate.Owner, string) ([]Record, error)
	Open(context.Context, runstate.Owner, Record) (io.ReadCloser, error)
}

// ObjectBackend mirrors the established Oasis local/S3 semantics without
// importing a second storage client into Lumen.
type ObjectBackend interface {
	Put(context.Context, string, io.Reader, int64, string) error
	Get(context.Context, string) (io.ReadCloser, error)
}
type MemoryStore struct {
	mu      sync.Mutex
	records []Record
	data    map[string][]byte
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{data: map[string][]byte{}} }
func (s *MemoryStore) Create(r Record) error {
	if !r.Owner.Valid() || r.ID == "" || r.RunID == "" || r.ObjectKey == "" {
		return errors.New("artifact identity required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, r)
	return nil
}
func (s *MemoryStore) Put(r Record, b []byte) error {
	h := sha256.Sum256(b)
	r.SHA256 = hex.EncodeToString(h[:])
	r.Size = int64(len(b))
	if err := s.Create(r); err != nil {
		return err
	}
	s.mu.Lock()
	s.data[r.ObjectKey] = append([]byte(nil), b...)
	s.mu.Unlock()
	return nil
}
func (s *MemoryStore) ListRun(o runstate.Owner, id string) ([]Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Record
	for _, r := range s.records {
		if r.Owner == o && r.RunID == id {
			out = append(out, r)
		}
	}
	return out, nil
}
func (s *MemoryStore) Open(_ context.Context, o runstate.Owner, r Record) (io.ReadCloser, error) {
	if r.Owner != o {
		return nil, ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.data[r.ObjectKey]
	if !ok {
		return nil, ErrNotFound
	}
	return io.NopCloser(strings.NewReader(string(b))), nil
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func SafeName(v string) string {
	v = filepath.Base(strings.TrimSpace(v))
	v = unsafeName.ReplaceAllString(v, "_")
	v = strings.Trim(v, "._")
	if v == "" {
		return "artifact"
	}
	if len(v) > 120 {
		v = v[:120]
	}
	return v
}
