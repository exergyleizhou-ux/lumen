package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"lumen/internal/science/lab/project"
	"lumen/internal/science/native/brief"
)

// BriefGenerateTool runs the Research Brief pipeline.
type BriefGenerateTool struct {
	SciDir      string
	ProjectRoot string
	Projects    *project.Store
	OnWrite     func(path string)
}

func (t *BriefGenerateTool) Name() string { return "science_brief_generate" }

func (t *BriefGenerateTool) ReadOnly() bool { return false }

func (t *BriefGenerateTool) Description() string {
	return "Generate a provenance-linked Research Brief (PubMed + ChEMBL + GEO + Oasis) and save to workspace/reports/brief.md"
}

func (t *BriefGenerateTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"topic":{"type":"string"},"dataset_query":{"type":"string"}},"required":["topic"]}`)
}

func (t *BriefGenerateTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Topic        string `json:"topic"`
		DatasetQuery string `json:"dataset_query"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	topic := strings.TrimSpace(p.Topic)
	if topic == "" {
		return "", fmt.Errorf("topic is required")
	}
	res, err := brief.Generate(ctx, t.SciDir, brief.Request{
		Topic:        topic,
		DatasetQuery: p.DatasetQuery,
	})
	if err != nil {
		return "", err
	}
	outPath := filepath.Join(t.ProjectRoot, "reports", "brief.md")
	if err := os.MkdirAll(filepath.Dir(outPath), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(outPath, []byte(res.Markdown), 0o600); err != nil {
		return "", err
	}
	if t.OnWrite != nil {
		t.OnWrite(outPath)
	}
	return fmt.Sprintf("Brief saved to %s (%d bytes)", outPath, len(res.Markdown)), nil
}
