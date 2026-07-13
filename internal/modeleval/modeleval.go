// Package modeleval provides the production model-capability evaluation gate.
// The default recorded mode is deterministic and network-free. Live execution
// is an explicit, credential-gated operation and never silently falls back.
package modeleval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

type Task struct {
	ID                string   `json:"id"`
	Profile           string   `json:"profile"`
	Prompt            string   `json:"prompt"`
	ExpectedTools     []string `json:"expected_tools"`
	RequiresRepair    bool     `json:"requires_repair"`
	ExpectedCitations int      `json:"expected_citations"`
}

type Observation struct {
	TaskID           string   `json:"task_id"`
	Success          bool     `json:"success"`
	Tools            []string `json:"tools"`
	VerificationRun  bool     `json:"verification_run"`
	RepairSucceeded  bool     `json:"repair_succeeded"`
	Citations        int      `json:"citations"`
	PromptTokens     int      `json:"prompt_tokens"`
	CompletionTokens int      `json:"completion_tokens"`
	CostMicrosUSD    int64    `json:"cost_micros_usd"`
	DurationMillis   int64    `json:"duration_ms"`
	FailureClass     string   `json:"failure_class,omitempty"`
	Error            string   `json:"error,omitempty"`
}

type Report struct {
	SchemaVersion int           `json:"schema_version"`
	Mode          string        `json:"mode"`
	Model         string        `json:"model"`
	GeneratedAt   string        `json:"generated_at"`
	Results       []Observation `json:"results"`
	Metrics       Metrics       `json:"metrics"`
}

type Metrics struct {
	Total                      int     `json:"total"`
	CodeTasks                  int     `json:"code_tasks"`
	LabTasks                   int     `json:"lab_tasks"`
	SuccessRate                float64 `json:"success_rate"`
	ToolCorrectnessRate        float64 `json:"tool_correctness_rate"`
	VerificationRepairRate     float64 `json:"verification_repair_rate"`
	CitationCompletenessRate   float64 `json:"citation_completeness_rate"`
	AverageTokens              float64 `json:"average_tokens"`
	TotalCostMicrosUSD         int64   `json:"total_cost_micros_usd"`
	AverageDurationMillis      float64 `json:"average_duration_ms"`
	NetworkFailures            int     `json:"network_failures"`
	ExternalCredentialFailures int     `json:"external_credential_failures"`
	CodeFailures               int     `json:"code_failures"`
}

type Runner interface {
	Run(context.Context, Task) (Observation, error)
}

var ErrExternalCredential = errors.New("live model evaluation requires an external provider credential")

func LoadTasks(path string) ([]Task, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var tasks []Task
	if err := json.Unmarshal(b, &tasks); err != nil {
		return nil, fmt.Errorf("decode tasks: %w", err)
	}
	seen := map[string]bool{}
	code, lab := 0, 0
	for _, t := range tasks {
		if t.ID == "" || seen[t.ID] || t.Prompt == "" || len(t.ExpectedTools) == 0 {
			return nil, fmt.Errorf("invalid or duplicate task %q", t.ID)
		}
		seen[t.ID] = true
		switch t.Profile {
		case "code":
			code++
		case "lab":
			lab++
		default:
			return nil, fmt.Errorf("task %s: invalid profile %q", t.ID, t.Profile)
		}
	}
	if code != 10 || lab != 10 {
		return nil, fmt.Errorf("suite must contain 10 code and 10 lab tasks; got %d and %d", code, lab)
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].ID < tasks[j].ID })
	return tasks, nil
}

func LoadRecorded(path string) (map[string]Observation, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rows []Observation
	if err := json.Unmarshal(b, &rows); err != nil {
		return nil, fmt.Errorf("decode recording: %w", err)
	}
	out := make(map[string]Observation, len(rows))
	for _, r := range rows {
		if r.TaskID == "" || out[r.TaskID].TaskID != "" {
			return nil, fmt.Errorf("invalid or duplicate recording %q", r.TaskID)
		}
		out[r.TaskID] = r
	}
	return out, nil
}

type RecordedRunner struct{ Rows map[string]Observation }

func (r RecordedRunner) Run(_ context.Context, t Task) (Observation, error) {
	o, ok := r.Rows[t.ID]
	if !ok {
		return Observation{}, fmt.Errorf("missing recording for %s", t.ID)
	}
	return o, nil
}

func Evaluate(ctx context.Context, mode, model string, tasks []Task, runner Runner, now time.Time) (Report, error) {
	rep := Report{SchemaVersion: 1, Mode: mode, Model: model, GeneratedAt: now.UTC().Format(time.RFC3339)}
	for _, task := range tasks {
		o, err := runner.Run(ctx, task)
		if err != nil {
			o = Observation{TaskID: task.ID, FailureClass: ClassifyFailure(err), Error: err.Error()}
		}
		if o.TaskID != task.ID {
			return Report{}, fmt.Errorf("runner returned task %q for %q", o.TaskID, task.ID)
		}
		rep.Results = append(rep.Results, o)
	}
	rep.Metrics = Aggregate(tasks, rep.Results)
	return rep, nil
}

func Aggregate(tasks []Task, rows []Observation) Metrics {
	m := Metrics{Total: len(tasks)}
	byID := map[string]Observation{}
	for _, r := range rows {
		byID[r.TaskID] = r
	}
	var success, toolsOK, repairNeed, repairOK, citeNeed, citeGot, tokens int
	var duration int64
	for _, t := range tasks {
		r := byID[t.ID]
		if t.Profile == "code" {
			m.CodeTasks++
		} else {
			m.LabTasks++
		}
		if r.Success {
			success++
		}
		if sameTools(t.ExpectedTools, r.Tools) {
			toolsOK++
		}
		if t.RequiresRepair {
			repairNeed++
			if r.VerificationRun && r.RepairSucceeded {
				repairOK++
			}
		}
		citeNeed += t.ExpectedCitations
		if r.Citations < t.ExpectedCitations {
			citeGot += r.Citations
		} else {
			citeGot += t.ExpectedCitations
		}
		tokens += r.PromptTokens + r.CompletionTokens
		m.TotalCostMicrosUSD += r.CostMicrosUSD
		duration += r.DurationMillis
		switch r.FailureClass {
		case "network":
			m.NetworkFailures++
		case "external_credential":
			m.ExternalCredentialFailures++
		case "code", "model":
			m.CodeFailures++
		}
	}
	if m.Total > 0 {
		m.SuccessRate = float64(success) / float64(m.Total)
		m.ToolCorrectnessRate = float64(toolsOK) / float64(m.Total)
		m.AverageTokens = float64(tokens) / float64(m.Total)
		m.AverageDurationMillis = float64(duration) / float64(m.Total)
	}
	if repairNeed > 0 {
		m.VerificationRepairRate = float64(repairOK) / float64(repairNeed)
	}
	if citeNeed > 0 {
		m.CitationCompletenessRate = float64(citeGot) / float64(citeNeed)
	}
	return m
}

func sameTools(want, got []string) bool {
	if len(want) != len(got) {
		return false
	}
	for i := range want {
		if want[i] != got[i] {
			return false
		}
	}
	return true
}
func ClassifyFailure(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, ErrExternalCredential) {
		return "external_credential"
	}
	s := strings.ToLower(err.Error())
	for _, marker := range []string{"timeout", "deadline exceeded", "connection reset", "connection refused", "temporary", "tls", "dns", "429", "502", "503", "504"} {
		if strings.Contains(s, marker) {
			return "network"
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "network"
	}
	return "model"
}
