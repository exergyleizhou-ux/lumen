// Package artifact owns durable metadata and adapter-compatible object bytes.
package artifact

import (
	"bytes"
	"context"
	"crypto/rand"
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
type Writer interface {
	Persist(context.Context, Record, []byte) error
}

// ObjectBackend mirrors the established Oasis local/S3 semantics without
// importing a second storage client into Lumen.
type ObjectBackend interface {
	Put(context.Context, string, io.Reader, int64, string) error
	Get(context.Context, string) (io.ReadCloser, error)
	Delete(context.Context, string) error
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
	for _, existing := range s.records {
		if existing.ID == r.ID {
			if existing.ObjectKey == r.ObjectKey && existing.SHA256 == r.SHA256 {
				return nil
			}
			return errors.New("artifact id conflict")
		}
	}
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
func (s *MemoryStore) Persist(_ context.Context, r Record, b []byte) error { return s.Put(r, b) }
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

func NewRecord(owner runstate.Owner, runID, name, mime string, data []byte) (Record, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return Record{}, err
	}
	id := "art_" + hex.EncodeToString(raw[:])
	h := sha256.Sum256(data)
	return Record{ID: id, RunID: runID, Owner: owner, Name: SafeName(name), ObjectKey: "workbench/" + owner.WorkspaceID + "/" + runID + "/" + id, SHA256: hex.EncodeToString(h[:]), MIME: mime, Size: int64(len(data)), CreatedAt: time.Now().UTC()}, nil
}
func Persist(ctx context.Context, store Store, backend ObjectBackend, r Record, data []byte) error {
	if backend == nil {
		return errors.New("artifact object backend unavailable")
	}
	if int64(len(data)) != r.Size {
		return errors.New("artifact size mismatch")
	}
	if err := backend.Put(ctx, r.ObjectKey, bytes.NewReader(data), r.Size, r.MIME); err != nil {
		return err
	}
	if err := store.Create(r); err != nil {
		_ = backend.Delete(ctx, r.ObjectKey)
		return err
	}
	return nil
}
