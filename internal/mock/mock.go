// Package mock provides a deterministic Anthropic/OpenAI-compatible mock
// service for end-to-end parity testing. Adapted from claw-code's
// mock-anthropic-service crate. It replays scripted tool-call sequences
// so the entire agent loop can be tested without a real API key.
package mock

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"lumen/internal/provider"
)

// Scenario is one scripted exchange in a parity test.
type Scenario struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Turns       []MockTurn `json:"turns"`
}

// MockTurn describes what the model "says" in one turn: optional text
// and optional tool calls.
type MockTurn struct {
	Text      string              `json:"text,omitempty"`
	ToolCalls []provider.ToolCall `json:"tool_calls,omitempty"`
	Usage     *provider.Usage     `json:"usage,omitempty"`
}

// Service is a deterministic mock provider that replays a pre-scripted
// scenario. It wraps a real provider interface so the agent can't tell
// the difference — but every turn is predictable and repeatable.
type Service struct {
	name     string
	model    string
	mu       sync.Mutex
	turn     int
	scenario *Scenario
}

// NewService creates a mock provider that replays the given scenario.
func NewService(name, model string, scenario *Scenario) *Service {
	return &Service{name: name, model: model, scenario: scenario}
}

func (s *Service) Name() string { return s.name }

func (s *Service) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 16)

	go func() {
		defer close(ch)

		s.mu.Lock()
		turnIdx := s.turn
		s.turn++
		s.mu.Unlock()

		if turnIdx >= len(s.scenario.Turns) {
			// No more scripted turns — return empty text (model gives up)
			ch <- provider.Chunk{Type: provider.ChunkText, Text: "done."}
			ch <- provider.Chunk{Type: provider.ChunkDone}
			return
		}

		turn := s.scenario.Turns[turnIdx]

		// Emit text if any
		if turn.Text != "" {
			ch <- provider.Chunk{Type: provider.ChunkText, Text: turn.Text}
		}

		// Emit tool calls if any
		for _, tc := range turn.ToolCalls {
			ch <- provider.Chunk{
				Type:     provider.ChunkToolCall,
				ToolCall: &tc,
			}
		}

		// Emit usage if any
		if turn.Usage != nil {
			ch <- provider.Chunk{Type: provider.ChunkUsage, Usage: turn.Usage}
		}

		ch <- provider.Chunk{Type: provider.ChunkDone}
	}()

	return ch, nil
}

// ── Pre-built parity scenarios ──────────────────────────────

// StreamingTextScenario: model returns only text, no tools.
func StreamingTextScenario() *Scenario {
	return &Scenario{
		Name:        "streaming_text",
		Description: "Model returns text only, no tool calls — simplest happy path",
		Turns: []MockTurn{
			{Text: "Hello! I'm Lumen, a coding agent. How can I help?"},
		},
	}
}

// ReadFileRoundtripScenario: model calls read_file, gets result, then answers.
func ReadFileRoundtripScenario() *Scenario {
	return &Scenario{
		Name:        "read_file_roundtrip",
		Description: "Model calls read_file, receives content, produces answer",
		Turns: []MockTurn{
			{
				ToolCalls: []provider.ToolCall{
					{ID: "call-1", Name: "read_file", Arguments: `{"path":"go.mod"}`},
				},
			},
			{Text: "The Go module is named 'lumen' with Go version 1.23.0."},
		},
	}
}

// WriteFileAllowedScenario: model writes a file, then confirms.
func WriteFileAllowedScenario() *Scenario {
	return &Scenario{
		Name:        "write_file_allowed",
		Description: "Model writes a file (permission allows), confirms",
		Turns: []MockTurn{
			{
				ToolCalls: []provider.ToolCall{
					{ID: "call-1", Name: "write_file", Arguments: `{"path":"/tmp/test.txt","content":"hello"}`},
				},
			},
			{Text: "File written successfully."},
		},
	}
}

// MultiToolTurnScenario: model calls two read-only tools in one turn, then answers.
func MultiToolTurnScenario() *Scenario {
	return &Scenario{
		Name:        "multi_tool_turn_roundtrip",
		Description: "Model calls grep + read_file in one turn (parallel read-only batch)",
		Turns: []MockTurn{
			{
				ToolCalls: []provider.ToolCall{
					{ID: "call-1", Name: "grep", Arguments: `{"pattern":"func Run","path":"internal/agent"}`},
					{ID: "call-2", Name: "read_file", Arguments: `{"path":"internal/agent/agent.go"}`},
				},
			},
			{Text: "Found the Run method. Here's the analysis..."},
		},
	}
}

// BashStdoutScenario: model runs bash, gets output, answers.
func BashStdoutScenario() *Scenario {
	return &Scenario{
		Name:        "bash_stdout_roundtrip",
		Description: "Model runs a bash command, parses stdout, answers",
		Turns: []MockTurn{
			{
				ToolCalls: []provider.ToolCall{
					{ID: "call-1", Name: "bash", Arguments: `{"command":"go build ./..."}`},
				},
			},
			{Text: "Build succeeded. All packages compile cleanly."},
		},
	}
}

// StormBreakerScenario: model keeps failing the same tool 4 times.
func StormBreakerScenario() *Scenario {
	return &Scenario{
		Name:        "storm_breaker",
		Description: "Model calls a non-existent tool 4 times — storm breaker should fire",
		Turns: []MockTurn{
			{ToolCalls: []provider.ToolCall{{ID: "f1", Name: "nonexistent", Arguments: `{}`}}},
			{ToolCalls: []provider.ToolCall{{ID: "f2", Name: "nonexistent", Arguments: `{}`}}},
			{ToolCalls: []provider.ToolCall{{ID: "f3", Name: "nonexistent", Arguments: `{}`}}},
			{ToolCalls: []provider.ToolCall{{ID: "f4", Name: "nonexistent", Arguments: `{"different":"args"}`}}},
			{Text: "Giving up — cannot proceed with nonexistent tool."},
		},
	}
}

// StreamInterruptionScenario: model produces text, stream breaks, recovery prompt.
func StreamInterruptionScenario() *Scenario {
	return &Scenario{
		Name:        "stream_interruption_recovery",
		Description: "First turn: text + tool call, simulated interruption. Second: recovery answer.",
		Turns: []MockTurn{
			{
				Text: "Let me check that file...",
				ToolCalls: []provider.ToolCall{
					{ID: "call-1", Name: "read_file", Arguments: `{"path":"test.txt"}`},
				},
			},
			{Text: "The file contains the expected content. Task complete."},
		},
	}
}

// LongConversationScenario: 5-turn conversation that triggers compaction.
func LongConversationScenario() *Scenario {
	turns := make([]MockTurn, 5)
	for i := 0; i < 5; i++ {
		if i%2 == 0 {
			turns[i] = MockTurn{
				ToolCalls: []provider.ToolCall{
					{ID: fmt.Sprintf("c-%d", i), Name: "read_file", Arguments: `{"path":"test.txt"}`},
				},
			}
		} else {
			turns[i] = MockTurn{Text: fmt.Sprintf("Turn %d analysis complete.", i)}
		}
	}
	return &Scenario{
		Name:        "long_conversation_compact",
		Description: "5-turn conversation that may trigger auto-compaction",
		Turns:       turns,
	}
}

// PlanModeScenario: model in plan mode tries to write — blocked, then produces plan.
func PlanModeScenario() *Scenario {
	return &Scenario{
		Name:        "plan_mode_blocked",
		Description: "Model in plan mode tries write_file (blocked), then produces text plan",
		Turns: []MockTurn{
			{
				ToolCalls: []provider.ToolCall{
					{ID: "call-1", Name: "write_file", Arguments: `{"path":"main.go","content":"..."}`},
				},
			},
			{
				ToolCalls: []provider.ToolCall{
					{ID: "call-2", Name: "read_file", Arguments: `{"path":"main.go"}`},
				},
			},
			{Text: "## Plan\n\n1. Refactor main.go\n2. Add tests\n3. Build"},
		},
	}
}

// EvidenceScenario: model calls write_file, then complete_step with evidence.
func EvidenceScenario() *Scenario {
	return &Scenario{
		Name:        "evidence_complete_step",
		Description: "Model writes file, then calls complete_step with verification evidence",
		Turns: []MockTurn{
			{
				ToolCalls: []provider.ToolCall{
					{ID: "call-1", Name: "write_file", Arguments: `{"path":"/tmp/test.txt","content":"test"}`},
				},
			},
			{
				ToolCalls: []provider.ToolCall{
					{ID: "call-2", Name: "bash", Arguments: `{"command":"go build ./..."}`},
				},
			},
			{
				ToolCalls: []provider.ToolCall{
					{ID: "call-3", Name: "complete_step", Arguments: `{"step":"add tests","result":"done","evidence":[{"kind":"verification","summary":"build passes","command":"go build ./..."}]}`},
				},
			},
			{Text: "Step completed and verified."},
		},
	}
}

// AllScenarios returns every built-in parity scenario.
func AllScenarios() []*Scenario {
	return []*Scenario{
		StreamingTextScenario(),
		ReadFileRoundtripScenario(),
		WriteFileAllowedScenario(),
		MultiToolTurnScenario(),
		BashStdoutScenario(),
		StormBreakerScenario(),
		StreamInterruptionScenario(),
		LongConversationScenario(),
		PlanModeScenario(),
		EvidenceScenario(),
	}
}

// ── Helper to build JSON scenario files ────────────────────

// MarshalScenario serializes a scenario to JSON for persistence.
func MarshalScenario(s *Scenario) ([]byte, error) {
	return json.MarshalIndent(s, "", "  ")
}

// UnmarshalScenario deserializes a scenario from JSON.
func UnmarshalScenario(data []byte) (*Scenario, error) {
	var s Scenario
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// ScenarioName returns a safe filename from a scenario name.
func ScenarioName(name string) string {
	return strings.ReplaceAll(name, " ", "_") + ".json"
}
