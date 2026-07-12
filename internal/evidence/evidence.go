// Package evidence records host-observable tool-call receipts so complete_step
// can validate that cited evidence (bash output, file changes, test results)
// actually happened before a step is signed off.
package evidence

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"lumen/internal/tool"
)

// ctxKey is the context key for the active evidence ledger.
type ctxKey struct{}

// WithLedger stamps ctx with the ledger for tool execution.
func WithLedger(ctx context.Context, l *Ledger) context.Context {
	return context.WithValue(ctx, ctxKey{}, l)
}

// FromContext returns the evidence ledger from ctx, or nil.
func FromContext(ctx context.Context) *Ledger {
	l, _ := ctx.Value(ctxKey{}).(*Ledger)
	return l
}

// Ledger is a per-user-turn record of host-observed tool call receipts.
// The agent records each call's outcome; complete_step consults it to
// verify cited evidence before advancing the canonical todo list.
type Ledger struct {
	mu       sync.Mutex
	receipts []Receipt
}

// Receipt is one tool call's host-observable outcome, recorded immediately
// after executeOne returns.
type Receipt struct {
	ToolName      string     `json:"tool"`
	Success       bool       `json:"success"`
	ReadOnly      bool       `json:"read_only"`
	WritesFiles   bool       `json:"writes_files,omitempty"`
	RunsCommands  bool       `json:"runs_commands,omitempty"`
	UsesNetwork   bool       `json:"uses_network,omitempty"`
	StartsCompute bool       `json:"starts_compute,omitempty"`
	Step          string     `json:"step,omitempty"`    // complete_step: which step
	Todos         []TodoItem `json:"todos,omitempty"`   // todo_write: task list snapshot
	Result        string     `json:"result,omitempty"`  // complete_step: result claim
	Command       string     `json:"command,omitempty"` // bash: the command string
	Paths         []string   `json:"paths,omitempty"`   // write_file/edit_file: target paths
}

// TodoItem mirrors the agent's canonical task representation.
type TodoItem struct {
	Content    string `json:"content"`
	Status     string `json:"status"`
	ActiveForm string `json:"active_form,omitempty"`
	Level      int    `json:"level,omitempty"`
}

// NewLedger creates an empty evidence ledger for one user turn.
func NewLedger() *Ledger {
	return &Ledger{}
}

// Record appends a receipt to the ledger. Thread-safe.
func (l *Ledger) Record(r Receipt) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.receipts = append(l.receipts, r)
}

// HasEvidence reports whether the ledger contains a successful receipt
// matching the given step name — called by complete_step to validate
// that the cited evidence actually occurred this turn.
func (l *Ledger) HasEvidence(step string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range l.receipts {
		if r.Success && r.Step == step {
			return true
		}
	}
	return false
}

// LastTodoWrite returns the most recent todo_write receipt's task list,
// or nil if todo_write was never called this turn.
func (l *Ledger) LastTodoWrite() []TodoItem {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := len(l.receipts) - 1; i >= 0; i-- {
		if l.receipts[i].ToolName == "todo_write" && len(l.receipts[i].Todos) > 0 {
			return l.receipts[i].Todos
		}
	}
	return nil
}

// Receipts returns a snapshot of all receipts (for display/debug).
func (l *Ledger) Receipts() []Receipt {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Receipt, len(l.receipts))
	copy(out, l.receipts)
	return out
}

// ReceiptFromToolCall constructs a Receipt from a tool call's outcome,
// extracting step/todo/command/path metadata from the raw args.
func ReceiptFromToolCall(name string, args json.RawMessage, success, readOnly bool, effects tool.Effects) Receipt {
	r := Receipt{
		ToolName:      name,
		Success:       success,
		ReadOnly:      readOnly,
		WritesFiles:   effects.WritesFiles,
		RunsCommands:  effects.RunsCommands,
		UsesNetwork:   effects.UsesNetwork,
		StartsCompute: effects.StartsCompute,
	}
	switch name {
	case "complete_step":
		var p struct {
			Step   string `json:"step"`
			Result string `json:"result"`
		}
		if err := json.Unmarshal(args, &p); err == nil {
			r.Step = p.Step
			r.Result = p.Result
		}
	case "todo_write":
		var p struct {
			Todos []struct {
				Content    string `json:"content"`
				Status     string `json:"status"`
				ActiveForm string `json:"activeForm"`
				Level      int    `json:"level"`
			} `json:"todos"`
		}
		if err := json.Unmarshal(args, &p); err == nil {
			r.Todos = make([]TodoItem, len(p.Todos))
			for i, td := range p.Todos {
				r.Todos[i] = TodoItem{
					Content:    td.Content,
					Status:     td.Status,
					ActiveForm: td.ActiveForm,
					Level:      td.Level,
				}
			}
		}
	case "bash":
		var p struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(args, &p); err == nil {
			r.Command = p.Command
		}
	case "write_file", "edit_file":
		var p struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(args, &p); err == nil {
			r.Paths = []string{p.Path}
		}
	case "multi_edit":
		var p struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(args, &p); err == nil {
			r.Paths = []string{p.Path}
		}
	}
	return r
}

// VerifyEvidence checks that the evidence cited in a complete_step call is
// backed by actual tool receipts in the ledger. Returns true and a summary
// when the claim is valid; returns false and a reason when it's not.
func (l *Ledger) VerifyEvidence(step, result string, evidence []EvidenceItem) (bool, string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(evidence) == 0 {
		return false, "at least one evidence item is required"
	}

	// Must have at least one successful material side effect this turn.
	hasMaterialEffect := false
	for _, r := range l.receipts {
		if r.Success && (r.WritesFiles || r.RunsCommands || r.StartsCompute) && r.ToolName != "complete_step" && r.ToolName != "todo_write" {
			hasMaterialEffect = true
			break
		}
	}
	if !hasMaterialEffect {
		return false, fmt.Sprintf("no successful material-effect receipts this turn") +
			" — complete_step requires a file write, command, or compute call as evidence of work done"
	}

	// Cross-reference each cited evidence item against receipts
	for _, ei := range evidence {
		switch ei.Kind {
		case "verification":
			if !l.hasBashReceipt(ei.Command) {
				return false, fmt.Sprintf("evidence verification %q: no matching bash command found", ei.Summary)
			}
		case "diff", "files":
			for _, path := range ei.Paths {
				if !l.hasFileReceipt(path) {
					return false, fmt.Sprintf("evidence %s %q: no writer tool touched %s", ei.Kind, ei.Summary, path)
				}
			}
		case "manual":
			// manual evidence is always accepted
		default:
			return false, fmt.Sprintf("unknown evidence kind %q", ei.Kind)
		}
	}

	return true, fmt.Sprintf("step %q completed: %s", step, result)
}

func (l *Ledger) hasBashReceipt(command string) bool {
	if command == "" {
		return false
	}
	for _, r := range l.receipts {
		if r.ToolName == "bash" && r.Success && r.RunsCommands && r.Command == command {
			return true
		}
	}
	return false
}

func (l *Ledger) hasFileReceipt(path string) bool {
	if path == "" {
		return false
	}
	for _, r := range l.receipts {
		if !r.Success || !r.WritesFiles {
			continue
		}
		for _, p := range r.Paths {
			if p == path {
				return true
			}
		}
	}
	return false
}

// EvidenceItem mirrors the evidence entry in complete_step args.
type EvidenceItem struct {
	Kind    string   `json:"kind"`
	Summary string   `json:"summary"`
	Command string   `json:"command,omitempty"`
	Paths   []string `json:"paths,omitempty"`
}
