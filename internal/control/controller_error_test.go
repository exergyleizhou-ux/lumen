package control

import (
	"context"
	"errors"
	"strings"
	"testing"

	"lumen/internal/agent"
	"lumen/internal/event"
	"lumen/internal/provider"
	"lumen/internal/tool"
)

// errProvider streams a single ChunkError, simulating a hard provider failure
// such as a 402 Insufficient Balance or a dropped connection.
type errProvider struct{ name string }

func (p *errProvider) Name() string { return p.name }
func (p *errProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 1)
	go func() {
		defer close(ch)
		ch <- provider.Chunk{Type: provider.ChunkError, Err: errors.New("HTTP 402: Insufficient Balance")}
	}()
	return ch, nil
}

func collectErrNotices(t *testing.T, run func(c *Controller, sink event.Sink) error) []event.Event {
	t.Helper()
	var notices []event.Event
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice && e.Level == event.LevelErr {
			notices = append(notices, e)
		}
	})
	failing := &errProvider{name: "test"}
	ag := agent.New(failing, tool.NewRegistry(), agent.NewSession(""), agent.Options{MaxSteps: 2, Sink: sink})
	c := &Controller{prov: failing, sink: sink, ag: ag}
	err := run(c, sink)
	if err == nil {
		t.Fatal("expected the provider error to propagate, got nil")
	}
	return notices
}

func TestRunSurfacesProviderError(t *testing.T) {
	notices := collectErrNotices(t, func(c *Controller, _ event.Sink) error {
		return c.Run(context.Background(), "do something")
	})
	if len(notices) == 0 {
		t.Fatal("Run must emit a LevelErr Notice so the user sees the failure; got none (silent failure)")
	}
	if !strings.Contains(notices[len(notices)-1].Text, "402") {
		t.Errorf("error notice should name the cause, got %q", notices[len(notices)-1].Text)
	}
}

func TestPlanSurfacesProviderError(t *testing.T) {
	notices := collectErrNotices(t, func(c *Controller, _ event.Sink) error {
		return c.Plan(context.Background(), "make a plan")
	})
	if len(notices) == 0 {
		t.Fatal("Plan must emit a LevelErr Notice so the user sees the failure; got none (silent failure)")
	}
}
