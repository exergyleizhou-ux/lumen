package control

import (
	"context"
	"testing"

	"lumen/internal/agent"
	"lumen/internal/event"
	"lumen/internal/provider"
	"lumen/internal/tool"
)

type modeTestProvider struct{}

func (modeTestProvider) Name() string { return "mode-test" }

func (modeTestProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func newModeTestController() *Controller {
	p := modeTestProvider{}
	ag := agent.New(p, tool.NewRegistry(), agent.NewSession(""), agent.Options{
		MaxSteps: 2,
		Sink:     event.Discard,
	})
	c := &Controller{prov: p, ag: ag}
	c.storeSink(event.Discard)
	return c
}

func TestPlanModeDoesNotLeakIntoRun(t *testing.T) {
	c := newModeTestController()
	if err := c.Plan(context.Background(), "inspect"); err != nil {
		t.Fatal(err)
	}
	if c.ag.IsPlanMode() {
		t.Fatal("plan mode must be restored after Plan returns")
	}
	if err := c.Run(context.Background(), "execute"); err != nil {
		t.Fatal(err)
	}
	if c.ag.IsPlanMode() {
		t.Fatal("Run must execute with plan mode disabled")
	}
}

func TestRunClearsPreviouslySetPlanMode(t *testing.T) {
	c := newModeTestController()
	c.ag.SetPlanMode(true)
	if err := c.Run(context.Background(), "execute"); err != nil {
		t.Fatal(err)
	}
	if c.ag.IsPlanMode() {
		t.Fatal("Run left plan mode enabled")
	}
}
