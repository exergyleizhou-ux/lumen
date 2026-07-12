package artifact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"lumen/internal/event"
	"lumen/internal/runstate"
	"lumen/internal/workspace"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type CapturingSink struct {
	Context      context.Context
	Store        Store
	Owner        runstate.Owner
	RunID, Model string
	Workspace    workspace.Context
	Next         event.Sink
	Failure      func(error)
	mu           sync.Mutex
	pending      map[string]captureCall
}
type captureCall struct{ step, path, argsHash string }

func (s *CapturingSink) Emit(e event.Event) {
	s.mu.Lock()
	if s.pending == nil {
		s.pending = map[string]captureCall{}
	}
	s.mu.Unlock()
	switch e.Kind {
	case event.ToolDispatch:
		if p := artifactPath(e.Tool.Name, e.Tool.Args); p != "" {
			h := sha256.Sum256([]byte(e.Tool.Args))
			s.mu.Lock()
			s.pending[e.Tool.ID] = captureCall{step: e.StepID, path: p, argsHash: hex.EncodeToString(h[:])}
			s.mu.Unlock()
		}
	case event.ToolResult:
		if e.Tool.Err == "" {
			if err := s.persist(e); err != nil && s.Failure != nil {
				s.Failure(err)
				return
			}
		}
	}
	if s.Next != nil {
		s.Next.Emit(e)
	}
}
func (s *CapturingSink) persist(e event.Event) error {
	s.mu.Lock()
	c, ok := s.pending[e.Tool.ID]
	delete(s.pending, e.Tool.ID)
	s.mu.Unlock()
	if !ok {
		return nil
	}
	if s.Store == nil || s.Workspace.Backend == nil {
		return fmt.Errorf("artifact persistence unavailable")
	}
	abs, err := s.Workspace.Backend.Resolve(c.path, false)
	if err != nil {
		return err
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return err
	}
	if len(b) > 25<<20 {
		return fmt.Errorf("artifact exceeds 25 MiB")
	}
	identity := sha256.Sum256([]byte(s.RunID + "\x00" + e.Tool.ID + "\x00" + c.path))
	id := "art_" + hex.EncodeToString(identity[:16])
	content := sha256.Sum256(b)
	r := Record{ID: id, RunID: s.RunID, StepID: c.step, ToolCallID: e.Tool.ID, Owner: s.Owner, Name: SafeName(filepath.Base(c.path)), Path: filepath.ToSlash(c.path), ObjectKey: "workbench/" + s.Owner.WorkspaceID + "/" + s.RunID + "/" + id, SHA256: hex.EncodeToString(content[:]), MIME: mime(c.path), Size: int64(len(b)), Model: s.Model, InputRefs: []string{"args-sha256:" + c.argsHash}, Provenance: map[string]any{"event_id": e.EventID, "tool": e.Tool.Name, "captured_at": time.Now().UTC()}, CreatedAt: time.Now().UTC()}
	w, ok := s.Store.(Writer)
	if !ok {
		return fmt.Errorf("artifact writer unavailable")
	}
	ctx := s.Context
	if ctx == nil {
		ctx = context.Background()
	}
	return w.Persist(ctx, r, b)
}
func artifactPath(name, args string) string {
	switch name {
	case "write_file", "edit_file", "notebook_edit":
		var v struct {
			Path string `json:"path"`
		}
		if json.Unmarshal([]byte(args), &v) == nil {
			return v.Path
		}
	case "science_brief_generate":
		return "reports/brief.md"
	}
	return ""
}
func mime(path string) string {
	switch filepath.Ext(path) {
	case ".json":
		return "application/json"
	case ".csv":
		return "text/csv"
	case ".md":
		return "text/markdown"
	case ".png":
		return "image/png"
	case ".pdf":
		return "application/pdf"
	}
	return "application/octet-stream"
}
