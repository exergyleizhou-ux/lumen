package provenance

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	labworkspace "lumen/internal/science/lab/workspace"
)

// Recorder tracks MCP calls and appends provenance records for workspace writes.
type Recorder struct {
	mu         sync.Mutex
	writer     *Writer
	guard      *labworkspace.Guard
	projectDir string
	sessionID  string
	runID      string
	model      string
}

// SetRunID binds subsequent provenance records to one durable Runtime Run.
func (r *Recorder) SetRunID(runID string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.runID = runID
	r.mu.Unlock()
}

// NewRecorder opens provenance logging for a lab project.
func NewRecorder(projectDir, sessionID, model string) (*Recorder, error) {
	if projectDir == "" {
		return nil, fmt.Errorf("project dir required")
	}
	g, err := labworkspace.NewGuard(projectDir)
	if err != nil {
		return nil, err
	}
	return &Recorder{
		writer:     NewWriter(filepath.Join(projectDir, "provenance.jsonl")),
		guard:      g,
		projectDir: projectDir,
		sessionID:  sessionID,
		model:      model,
	}, nil
}

// RecordMCP appends an immediate mcp_call provenance entry (not queued).
func (r *Recorder) RecordMCP(domain, tool, query string) {
	if r == nil || r.writer == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	rec := Record{
		TS:         time.Now().UTC(),
		ArtifactID: domain + "/" + tool,
		Kind:       "mcp_call",
		Version:    r.nextVersion(),
		SessionID:  r.sessionID,
		RunID:      r.runID,
		Model:      r.model,
		MCPCalls: []MCPCall{{
			Tool:  domain + "/" + tool,
			Query: query,
		}},
	}
	_ = r.append(rec)
}

// RecordArtifact appends provenance for a file under the project.
func (r *Recorder) RecordArtifact(absPath string) error {
	if r == nil || r.writer == nil {
		return nil
	}
	rel, err := filepath.Rel(r.projectDir, absPath)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	b, err := r.guard.ReadFile(rel)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(b)
	rec := Record{TS: time.Now().UTC(), ArtifactID: filepath.Base(rel), Path: rel, Version: r.nextVersion(), Kind: "file_write", SessionID: r.sessionID, RunID: r.runID, Model: r.model, ContentHash: "sha256:" + hex.EncodeToString(sum[:])}
	return r.append(rec)
}

func (r *Recorder) nextVersion() int {
	b, err := r.guard.ReadFile("provenance.jsonl")
	if err != nil || len(b) == 0 {
		return 1
	}
	return bytes.Count(b, []byte{'\n'}) + 1
}
func (r *Recorder) append(rec Record) error {
	b, err := r.guard.ReadFile("provenance.jsonl")
	if err != nil {
		b = nil
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	b = append(b, append(line, '\n')...)
	return r.guard.AtomicWriteFile("provenance.jsonl", b, 0o600)
}
