package control

import (
	"context"
	"testing"

	"lumen/internal/agent"
	"lumen/internal/event"
	"lumen/internal/provider"
	"lumen/internal/tool"
	runworkspace "lumen/internal/workspace"
)

type workspaceProbeProvider struct {
	got runworkspace.Context
}

func (p *workspaceProbeProvider) Name() string { return "workspace-probe" }

func (p *workspaceProbeProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.got, _ = runworkspace.FromContext(ctx)
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestControllerRunInjectsConfiguredWorkspace(t *testing.T) {
	ws, err := runworkspace.NewLocal("controller", t.TempDir(), "user", map[string]string{"RUN_MARKER": "controller"})
	if err != nil {
		t.Fatal(err)
	}
	p := &workspaceProbeProvider{}
	ag := agent.New(p, tool.NewRegistry(), agent.NewSession(""), agent.Options{MaxSteps: 2, Sink: event.Discard})
	c := &Controller{prov: p, ag: ag, workspace: ws}
	c.storeSink(event.Discard)

	if err := c.Run(context.Background(), "inspect"); err != nil {
		t.Fatal(err)
	}
	if p.got.Root != ws.Root || p.got.Env["RUN_MARKER"] != "controller" {
		t.Fatalf("provider workspace=%+v want root=%q", p.got, ws.Root)
	}
}
