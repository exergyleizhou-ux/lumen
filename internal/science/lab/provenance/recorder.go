package provenance

import (
	"fmt"
	"path/filepath"
	"sync"
)

// Recorder tracks MCP calls and appends provenance records for workspace writes.
type Recorder struct {
	mu         sync.Mutex
	writer     *Writer
	projectDir string
	sessionID  string
	model      string
	pending    []MCPCall
}

// NewRecorder opens provenance logging for a lab project.
func NewRecorder(projectDir, sessionID, model string) (*Recorder, error) {
	if projectDir == "" {
		return nil, fmt.Errorf("project dir required")
	}
	return &Recorder{
		writer:     NewWriter(filepath.Join(projectDir, "provenance.jsonl")),
		projectDir: projectDir,
		sessionID:  sessionID,
		model:      model,
	}, nil
}

// RecordMCP queues one MCP tool invocation for the next artifact write.
func (r *Recorder) RecordMCP(domain, tool, query string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pending = append(r.pending, MCPCall{
		Tool:  domain + "/" + tool,
		Query: query,
	})
}

// RecordArtifact appends provenance for a file under the project and clears pending MCP calls.
func (r *Recorder) RecordArtifact(absPath string) error {
	if r == nil || r.writer == nil {
		return nil
	}
	rel, err := filepath.Rel(r.projectDir, absPath)
	if err != nil {
		return err
	}
	rec, err := RecordWrite(r.projectDir, rel, r.sessionID, r.model)
	if err != nil {
		return err
	}
	r.mu.Lock()
	rec.MCPCalls = append([]MCPCall(nil), r.pending...)
	r.pending = nil
	r.mu.Unlock()
	return r.writer.Append(rec)
}
