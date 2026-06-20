package orchestrator

import "context"

// FuncAgent adapts a plain function into an orchestrator Agent. It is the seam
// that connects the orchestrator to a real executor (e.g. a lumen sub-agent)
// without the orchestrator package having to import the agent loop — the caller
// injects the Exec closure. This is what turns the pool from "built but empty"
// (every task failing "no agent available") into one that runs real work.
type FuncAgent struct {
	AgentName   string
	Caps        []string
	Concurrency int
	Exec        func(ctx context.Context, prompt string) (string, error)
}

func (a *FuncAgent) Name() string { return a.AgentName }

func (a *FuncAgent) Execute(ctx context.Context, prompt string) (string, error) {
	return a.Exec(ctx, prompt)
}

func (a *FuncAgent) Capabilities() []string { return a.Caps }

func (a *FuncAgent) IsAvailable() bool { return a.Exec != nil }

func (a *FuncAgent) MaxConcurrency() int {
	if a.Concurrency <= 0 {
		return 1
	}
	return a.Concurrency
}
