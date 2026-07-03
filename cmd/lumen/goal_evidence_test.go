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
		t.Fatal("LUMEN_GOAL_SCRATCH env must be set to the private scratch dir")
	}
	if err := os.MkdirAll(scratch, 0755); err != nil {
		t.Fatal(err)
	}

	lumen := filepath.Join(scratch, "lumen")
	// Build the shipped binary (plan step 1)
	build := exec.Command("go", "build", "-o", lumen, "./cmd/lumen")
	build.Dir = "/Users/lei/lumen"
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		t.Fatalf("build: %v", err)
	}

	// --- AC1: dogfood via direct binary exec in isolated module ---
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "go.mod"), []byte("module dogfood\n\ngo 1.23\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// broken file that the default bypass fixes
	if err := os.WriteFile(filepath.Join(ws, "bug.go"), []byte("package main\n\nfunc main() { println(undefinedVar) }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// toml to activate TEST bypass
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

	// run the real binary
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

	// --- AC3: two runs via real binary CLI on the 6-task baseline ---
	baseline := "evals/baseline6"
	for i := 1; i <= 2; i++ {
		evalCmd := exec.Command(lumen, "eval", "-tasks", baseline, "-json")
		evalCmd.Dir = "/Users/lei/lumen"
		evalCmd.Env = append(os.Environ(), "DEEPSEEK_API_KEY=TEST_E2E_SUCCESS")
		evalOut, _ := evalCmd.CombinedOutput()
		jsonFile := filepath.Join(scratch, "eval-run"+string('0'+rune(i))+".json")
		if err := os.WriteFile(jsonFile, evalOut, 0644); err != nil {
			t.Fatal(err)
		}
		var rep struct {
			Summary struct {
				Total  int `json:"total"`
				Passed int `json:"passed"`
			} `json:"summary"`
		}
		if err := json.Unmarshal(evalOut, &rep); err != nil {
			t.Fatalf("bad json in run %d: %v\n%s", i, err, evalOut)
		}
		if rep.Summary.Total != 6 {
			t.Fatalf("run %d total=%d want 6", i, rep.Summary.Total)
		}
		if rep.Summary.Passed < 5 {
			t.Fatalf("run %d passed=%d <5", i, rep.Summary.Passed)
		}
		if i == 2 {
			// compare to previous
			prevB, _ := os.ReadFile(filepath.Join(scratch, "eval-run1.json"))
			var prev struct {
				Summary struct {
					Passed int `json:"passed"`
				} `json:"summary"`
			}
			json.Unmarshal(prevB, &prev)
			if prev.Summary.Passed != rep.Summary.Passed {
				t.Fatalf("runs not identical: %d vs %d", prev.Summary.Passed, rep.Summary.Passed)
			}
		}
	}
}
