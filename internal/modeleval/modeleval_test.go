package modeleval

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("..", "..", "evals", "production", name)
}

func TestRecordedSuiteIsDeterministicAndComplete(t *testing.T) {
	tasks, err := LoadTasks(fixture(t, "tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := LoadRecorded(fixture(t, "recorded.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC)
	one, err := Evaluate(context.Background(), "recorded", "controlled-v1", tasks, RecordedRunner{Rows: rows}, now)
	if err != nil {
		t.Fatal(err)
	}
	two, _ := Evaluate(context.Background(), "recorded", "controlled-v1", tasks, RecordedRunner{Rows: rows}, now)
	if one.GeneratedAt != two.GeneratedAt || one.Metrics != two.Metrics {
		t.Fatalf("recorded evaluation drifted: %#v %#v", one.Metrics, two.Metrics)
	}
	m := one.Metrics
	if m.Total != 20 || m.CodeTasks != 10 || m.LabTasks != 10 {
		t.Fatalf("bad inventory: %+v", m)
	}
	if m.SuccessRate != 1 || m.ToolCorrectnessRate != 1 || m.VerificationRepairRate != 1 || m.CitationCompletenessRate != 1 {
		t.Fatalf("controlled fixture should be perfect: %+v", m)
	}
	if m.TotalCostMicrosUSD != 0 || m.AverageTokens <= 0 || m.AverageDurationMillis <= 0 {
		t.Fatalf("bad resource metrics: %+v", m)
	}
}

func TestLoadTasksRejectsWrongInventory(t *testing.T) {
	p := filepath.Join(t.TempDir(), "tasks.json")
	if err := os.WriteFile(p, []byte(`[{"id":"code-1","profile":"code","prompt":"x","expected_tools":["read_file"]}]`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTasks(p); err == nil {
		t.Fatal("expected inventory error")
	}
}

type failingRunner struct{ err error }

func (r failingRunner) Run(context.Context, Task) (Observation, error) { return Observation{}, r.err }

func TestNetworkFailureIsNotCountedAsCodeFailureOrSuccess(t *testing.T) {
	tasks := []Task{{ID: "code-x", Profile: "code", Prompt: "x", ExpectedTools: []string{"read_file"}}}
	rep, err := Evaluate(context.Background(), "live", "qwen", tasks, failingRunner{errors.New("connection reset by peer")}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Metrics.NetworkFailures != 1 || rep.Metrics.CodeFailures != 0 || rep.Metrics.SuccessRate != 0 {
		t.Fatalf("misclassified: %+v", rep.Metrics)
	}
}

func TestAdapterSelectionIncludesChineseModelAndCredentialGate(t *testing.T) {
	a, err := SelectAdapter("qwen")
	if err != nil {
		t.Fatal(err)
	}
	if a.Model != "qwen-plus" || a.KeyEnv != "DASHSCOPE_API_KEY" {
		t.Fatalf("wrong qwen adapter: %+v", a)
	}
	t.Setenv(a.KeyEnv, "")
	_, err = (LiveRunner{Adapter: a}).Run(context.Background(), Task{ID: "x"})
	if !errors.Is(err, ErrExternalCredential) {
		t.Fatalf("want credential blocker, got %v", err)
	}
}

func TestClassifyFailure(t *testing.T) {
	if got := ClassifyFailure(context.DeadlineExceeded); got != "network" {
		t.Fatalf("got %q", got)
	}
	if got := ClassifyFailure(errors.New("invalid JSON response")); got != "model" {
		t.Fatalf("got %q", got)
	}
	if got := ClassifyFailure(fmt.Errorf("%w: KEY", ErrExternalCredential)); got != "external_credential" {
		t.Fatalf("got %q", got)
	}
}
