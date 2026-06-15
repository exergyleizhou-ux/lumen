// Package proptest provides property-based testing utilities for Lumen's
// agent components. It generates random inputs, shrinks failing cases,
// and reports minimal reproduction scenarios. Used to find edge cases
// in tool execution, permission logic, and agent state machines.
package proptest

import (
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"
)

// ── Generators ─────────────────────────────────────────────

// Gen is a random value generator.
type Gen[T any] struct {
	Name string
	Gen  func(*rand.Rand) T
}

// IntRange generates integers in [min, max].
func IntRange(min, max int) *Gen[int] {
	return &Gen[int]{Name: "int", Gen: func(r *rand.Rand) int { return min + r.Intn(max-min+1) }}
}

// StringOf generates strings of a given length from an alphabet.
func StringOf(length int, alphabet string) *Gen[string] {
	return &Gen[string]{Name: "string", Gen: func(r *rand.Rand) string {
		b := make([]byte, length)
		for i := range b {
			b[i] = alphabet[r.Intn(len(alphabet))]
		}
		return string(b)
	}}
}

// OneOf generates one of the given values.
func OneOf[T any](values ...T) *Gen[T] {
	return &Gen[T]{Name: "oneof", Gen: func(r *rand.Rand) T { return values[r.Intn(len(values))] }}
}

// SliceOf generates a slice of values.
func SliceOf[T any](g *Gen[T], minLen, maxLen int) *Gen[[]T] {
	return &Gen[[]T]{Name: "slice", Gen: func(r *rand.Rand) []T {
		n := minLen + r.Intn(maxLen-minLen+1)
		s := make([]T, n)
		for i := range s {
			s[i] = g.Gen(r)
		}
		return s
	}}
}

// ── Property ───────────────────────────────────────────────

// Property is a testable property with shrinking support.
type Property struct {
	Name    string
	Test    func() bool
	MaxSize int
}

// Result is the outcome of a property test.
type Result struct {
	Property string        `json:"property"`
	Passed   bool          `json:"passed"`
	TestsRun int           `json:"tests_run"`
	Duration time.Duration `json:"duration"`
	Smallest string        `json:"smallest,omitempty"`
	Shrinks  int           `json:"shrinks,omitempty"`
}

// ── Runner ─────────────────────────────────────────────────

// Config configures the property test runner.
type Config struct {
	MaxTests    int           `json:"max_tests"`
	MaxDuration time.Duration `json:"max_duration"`
	Seed        int64         `json:"seed"`
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{MaxTests: 100, MaxDuration: 10 * time.Second, Seed: time.Now().UnixNano()}
}

// Runner executes property tests.
type Runner struct {
	cfg     Config
	results []Result
	mu      sync.Mutex
}

// NewRunner creates a property test runner.
func NewRunner(cfg Config) *Runner {
	return &Runner{cfg: cfg}
}

// Check runs a single property for N iterations.
func (r *Runner) Check(name string, prop func() bool) *Result {
	start := time.Now()
	result := &Result{Property: name, Passed: true}

	for i := 0; i < r.cfg.MaxTests; i++ {
		if time.Since(start) > r.cfg.MaxDuration {
			break
		}
		if !prop() {
			result.Passed = false
			result.TestsRun = i + 1
			result.Duration = time.Since(start)
			r.mu.Lock()
			r.results = append(r.results, *result)
			r.mu.Unlock()
			return result
		}
	}

	result.TestsRun = r.cfg.MaxTests
	result.Duration = time.Since(start)
	r.mu.Lock()
	r.results = append(r.results, *result)
	r.mu.Unlock()
	return result
}

// CheckAll runs all registered properties and returns results.
func (r *Runner) CheckAll(props []Property) []Result {
	for _, p := range props {
		r.Check(p.Name, p.Test)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Result, len(r.results))
	copy(out, r.results)
	return out
}

// AllPassed reports whether all properties passed.
func (r *Runner) AllPassed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, res := range r.results {
		if !res.Passed {
			return false
		}
	}
	return true
}

// FormatResults formats all results.
func (r *Runner) FormatResults() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.results) == 0 {
		return "No properties tested.\n"
	}
	var sb strings.Builder
	passed, failed := 0, 0
	for _, res := range r.results {
		if res.Passed {
			passed++
		} else {
			failed++
		}
	}
	fmt.Fprintf(&sb, "Property Tests: %d passed, %d failed (%d total)\n\n", passed, failed, len(r.results))
	for _, res := range r.results {
		icon := "✅"
		if !res.Passed {
			icon = "❌"
		}
		fmt.Fprintf(&sb, "%s %-30s %d tests %v", icon, res.Property, res.TestsRun, res.Duration)
		if !res.Passed {
			fmt.Fprintf(&sb, " — smallest: %s (%d shrinks)", res.Smallest, res.Shrinks)
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

// ── Shrinker ───────────────────────────────────────────────

// Shrinker reduces failing inputs to a minimal reproduction.
type Shrinker struct {
	maxShrinks int
}

// NewShrinker creates a shrinker.
func NewShrinker(maxShrinks int) *Shrinker {
	return &Shrinker{maxShrinks: maxShrinks}
}

// Shrink reduces a failing string to its minimal failing prefix.
func (s *Shrinker) Shrink(failing string, test func(string) bool) string {
	current := failing
	for i := 0; i < s.maxShrinks; i++ {
		foundSmaller := false
		// Try removing each character
		for j := 0; j < len(current); j++ {
			candidate := current[:j] + current[j+1:]
			if !test(candidate) {
				current = candidate
				foundSmaller = true
				break
			}
		}
		if !foundSmaller {
			break
		}
		// Try halving
		half := current[:len(current)/2]
		if !test(half) {
			current = half
			foundSmaller = true
		}
	}
	return current
}

// ShrinkInt reduces a failing integer by halving.
func (s *Shrinker) ShrinkInt(failing int, test func(int) bool) int {
	current := failing
	for i := 0; i < s.maxShrinks; i++ {
		if current == 0 {
			break
		}
		candidate := current / 2
		if !test(candidate) {
			current = candidate
			continue
		}
		candidate = current - 1
		if !test(candidate) {
			current = candidate
			continue
		}
		break
	}
	return current
}

// ── Pre-built properties ──────────────────────────────────

// IsMonotonic checks that f(x) <= f(x+1) for increasing integers.
func IsMonotonic(name string, f func(int) int, max int) Property {
	return Property{
		Name: name,
		Test: func() bool {
			x := rand.Intn(max)
			return f(x) <= f(x+1)
		},
		MaxSize: max,
	}
}

// IsIdempotent checks that f(f(x)) == f(x).
func IsIdempotent(name string, f func(string) string) Property {
	return Property{
		Name: name,
		Test: func() bool {
			input := randomString(20)
			first := f(input)
			second := f(first)
			return first == second
		},
	}
}

// IsInverse checks that g(f(x)) == x.
func IsInverse(name string, f func(int) int, g func(int) int, max int) Property {
	return Property{
		Name: name,
		Test: func() bool {
			x := rand.Intn(max)
			return g(f(x)) == x
		},
		MaxSize: max,
	}
}

func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}
