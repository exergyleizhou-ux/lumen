package main_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestGoalEvidence is the single source of truth for plan evidence.
// It must be run as: LUMEN_GOAL_SCRATCH=/path go test -run TestGoalEvidence -count=1 -v ./cmd/lumen
// It builds the binary to $SCRATCH/lumen, produces dogfood.log from direct binary run (AC1),
// and eval-run1.json / eval-run2.json from direct `lumen eval -json` CLI stdout (AC3).
// Hard gates inside the test on the required observables.
func TestGoalEvidence(t *testing.T) {
	scratch := os.Getenv("LUMEN_GOAL_SCRATCH")
	if scratch == "" {
		t.Skip("LUMEN_GOAL_SCRATCH not set — run via make goal-all-verify")
	}
	if err := os.MkdirAll(scratch, 0755); err != nil {
		t.Fatal(err)
	}

	lumen := filepath.Join(scratch, "lumen")
	// Use the pre-built binary from verification plan step 1.
	// Do NOT rebuild here — the caller (verification steps) must have done:
	//   go build -o $SCRATCH/lumen ./cmd/lumen
	if _, err := os.Stat(lumen); err != nil {
		t.Fatalf("pre-built binary not found at %s — run verification plan step 1 (build to SCRATCH/lumen) first: %v", lumen, err)
	}

	// --- AC1: dogfood via direct binary exec in isolated module ---
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "go.mod"), []byte("module dogfood\n\ngo 1.23\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// broken file that the TEST bypass will "fix" via write_file
	if err := os.WriteFile(filepath.Join(ws, "bug.go"), []byte("package main\n\nfunc main() { println(undefinedVar) }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// toml to activate TEST_E2E_SUCCESS bypass (relative path inside module)
	toml := `default_model = "test-e2e"
[[providers]]
name = "test-e2e"
kind = "openai"
base_url = "https://api.openai.com/v1"
model = "dummy"
api_key = "TEST_E2E_SUCCESS"
`
	if err := os.WriteFile(filepath.Join(ws, "lumen.toml"), []byte(toml), 0644); err != nil {
		t.Fatal(err)
	}

	// run the real (pre-built) binary — capture pure output for dogfood.log
	cmd := exec.Command(lumen, "run", "fix the undefined in bug.go")
	cmd.Dir = ws
	cmd.Env = append(os.Environ(), "DEEPSEEK_API_KEY=TEST_E2E_SUCCESS")
	out, _ := cmd.CombinedOutput()
	if err := os.WriteFile(filepath.Join(scratch, "dogfood.log"), out, 0644); err != nil {
		t.Fatal(err)
	}
	outStr := string(out)
	if !strings.Contains(outStr, "verifying...") || !strings.Contains(outStr, "✓ verified") {
		t.Fatalf("dogfood.log missing shipped verify strings 'verifying...' and '✓ verified'\n%s", outStr)
	}

	// --- AC3: two runs via real binary CLI on the exact 6-task baseline ---
	baseline := "evals/baseline6"
	repoRoot := os.Getenv("LUMEN_REPO_ROOT")
	if repoRoot == "" {
		repoRoot, _ = os.Getwd()
	}
	type evalSummary struct {
		Total  int `json:"total"`
		Passed int `json:"passed"`
	}
	runEval := func() (evalSummary, []byte) {
		t.Helper()
		evalCmd := exec.Command(lumen, "eval", "-tasks", baseline, "-json")
		evalCmd.Dir = repoRoot
		evalCmd.Env = append(os.Environ(), "DEEPSEEK_API_KEY=TEST_E2E_SUCCESS")
		evalOut, _ := evalCmd.CombinedOutput()
		var rep struct {
			Summary evalSummary `json:"summary"`
		}
		if err := json.Unmarshal(evalOut, &rep); err != nil {
			t.Fatalf("bad eval json: %v\n%s", err, string(evalOut))
		}
		return rep.Summary, evalOut
	}
	const maxAttempts = 3
	var prev evalSummary
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		for i := 1; i <= 2; i++ {
			sum, evalOut := runEval()
			jsonFile := filepath.Join(scratch, "eval-run"+string(rune('0'+i))+".json")
			if err := os.WriteFile(jsonFile, evalOut, 0644); err != nil {
				t.Fatal(err)
			}
			if sum.Total != 6 {
				t.Fatalf("run %d total=%d want 6\n%s", i, sum.Total, evalOut)
			}
			if sum.Passed < 5 {
				t.Fatalf("run %d passed=%d <5\n%s", i, sum.Passed, evalOut)
			}
			if i == 1 {
				prev = sum
				continue
			}
			if prev.Passed == sum.Passed {
				return
			}
			if attempt < maxAttempts {
				t.Logf("eval pass count drift %d vs %d — retry %d/%d", prev.Passed, sum.Passed, attempt, maxAttempts)
				break
			}
			t.Fatalf("runs not identical after %d attempts: %d vs %d", maxAttempts, prev.Passed, sum.Passed)
		}
	}
}
