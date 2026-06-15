// Package harness provides a test harness framework: test suite runner,
// fixture management (setup/teardown), parallel execution, timeout
// enforcement, and report aggregation (JUnit XML output).
package harness

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// TestStatus represents the outcome of a test.
type TestStatus int

const (
	// StatusPassed means the test passed.
	StatusPassed TestStatus = iota
	// StatusFailed means the test failed.
	StatusFailed
	// StatusSkipped means the test was skipped.
	StatusSkipped
	// StatusError means the test had an error (e.g., panic).
	StatusError
	// StatusTimeout means the test timed out.
	StatusTimeout
)

var statusStrings = map[TestStatus]string{
	StatusPassed:  "passed",
	StatusFailed:  "failed",
	StatusSkipped: "skipped",
	StatusError:   "error",
	StatusTimeout: "timeout",
}

func (s TestStatus) String() string {
	if str, ok := statusStrings[s]; ok {
		return str
	}
	return "unknown"
}

// TestResult holds the result of a single test.
type TestResult struct {
	Name       string
	Status     TestStatus
	Duration   time.Duration
	Message    string
	StackTrace string
	StartedAt  time.Time
	EndedAt    time.Time
	Output     string
}

// SuiteResult aggregates results from a test suite.
type SuiteResult struct {
	Name       string
	Tests      []*TestResult
	Total      int
	Passed     int
	Failed     int
	Skipped    int
	Errors     int
	Timeouts   int
	Duration   time.Duration
	StartedAt  time.Time
	EndedAt    time.Time
	Properties map[string]string
}

// ---- Test function types ----

// TestFunc is a single test function.
type TestFunc func(t *T)

// BeforeFunc runs before a test.
type BeforeFunc func(t *T)

// AfterFunc runs after a test.
type AfterFunc func(t *T)

// BeforeAllFunc runs once before the suite.
type BeforeAllFunc func() error

// AfterAllFunc runs once after the suite.
type AfterAllFunc func() error

// ---- T is the test context passed to each test ----

// T provides assertion and logging methods for tests.
type T struct {
	name      string
	suite     *Suite
	failed    bool
	skipped   bool
	timedOut  bool
	logs      []string
	output    strings.Builder
	startTime time.Time
	mu        sync.Mutex
	cleanups  []func()
	timeout   time.Duration
	parent    *T // for subtests
}

// Name returns the test name.
func (t *T) Name() string { return t.name }

// Log records a log message.
func (t *T) Log(args ...interface{}) {
	t.mu.Lock()
	defer t.mu.Unlock()
	msg := fmt.Sprint(args...)
	t.logs = append(t.logs, msg)
	t.output.WriteString(msg)
	t.output.WriteString("\n")
}

// Logf records a formatted log message.
func (t *T) Logf(format string, args ...interface{}) {
	t.Log(fmt.Sprintf(format, args...))
}

// Error marks the test as failed and logs a message.
func (t *T) Error(args ...interface{}) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.failed = true
	msg := fmt.Sprint(args...)
	t.logs = append(t.logs, "ERROR: "+msg)
	t.output.WriteString("ERROR: " + msg + "\n")
}

// Errorf marks the test as failed and logs a formatted message.
func (t *T) Errorf(format string, args ...interface{}) {
	t.Error(fmt.Sprintf(format, args...))
}

// Fatal marks the test as failed, logs a message, and stops execution.
func (t *T) Fatal(args ...interface{}) {
	t.Error(args...)
	runtime.Goexit()
}

// Fatalf marks the test as failed, logs a formatted message, and stops.
func (t *T) Fatalf(format string, args ...interface{}) {
	t.Fatal(fmt.Sprintf(format, args...))
}

// Fail marks the test as failed.
func (t *T) Fail() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.failed = true
}

// Failed returns whether the test has failed.
func (t *T) Failed() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.failed
}

// Skip marks the test as skipped and stops execution.
func (t *T) Skip(args ...interface{}) {
	t.mu.Lock()
	t.skipped = true
	t.mu.Unlock()
	msg := fmt.Sprint(args...)
	t.Log("SKIP: " + msg)
	runtime.Goexit()
}

// Skipf marks the test as skipped with a formatted message.
func (t *T) Skipf(format string, args ...interface{}) {
	t.Skip(fmt.Sprintf(format, args...))
}

// Skipped returns whether the test has been skipped.
func (t *T) Skipped() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.skipped
}

// Cleanup registers a function to run after the test completes.
func (t *T) Cleanup(fn func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.cleanups = append(t.cleanups, fn)
}

// Deadline returns the test deadline time, if any.
func (t *T) Deadline() (time.Time, bool) {
	if t.timeout > 0 {
		return t.startTime.Add(t.timeout), true
	}
	return time.Time{}, false
}

// Run runs a subtest.
func (t *T) Run(name string, fn TestFunc) bool {
	subT := &T{
		name:      t.name + "/" + name,
		suite:     t.suite,
		startTime: time.Now(),
		timeout:   t.timeout,
		parent:    t,
	}
	return runTest(subT, fn)
}

// ---- Assertions ----

// Assert compares expected and actual.
func (t *T) Assert(expected, actual interface{}, msg ...interface{}) {
	if expected != actual {
		message := fmt.Sprintf("assertion failed: expected %v, got %v", expected, actual)
		if len(msg) > 0 {
			message = fmt.Sprint(msg...)
		}
		t.Error(message)
	}
}

// AssertEqual checks that two values are equal.
func (t *T) AssertEqual(expected, actual interface{}, msg ...interface{}) {
	if expected != actual {
		message := fmt.Sprintf("expected %v, got %v", expected, actual)
		if len(msg) > 0 {
			message = fmt.Sprint(msg...)
		}
		t.Error(message)
	}
}

// AssertNotEqual checks that two values are not equal.
func (t *T) AssertNotEqual(a, b interface{}, msg ...interface{}) {
	if a == b {
		message := fmt.Sprintf("values should not be equal: %v", a)
		if len(msg) > 0 {
			message = fmt.Sprint(msg...)
		}
		t.Error(message)
	}
}

// AssertNil checks that a value is nil.
func (t *T) AssertNil(v interface{}, msg ...interface{}) {
	if v != nil {
		message := fmt.Sprintf("expected nil, got %v", v)
		if len(msg) > 0 {
			message = fmt.Sprint(msg...)
		}
		t.Error(message)
	}
}

// AssertNotNil checks that a value is not nil.
func (t *T) AssertNotNil(v interface{}, msg ...interface{}) {
	if v == nil {
		message := "expected non-nil value"
		if len(msg) > 0 {
			message = fmt.Sprint(msg...)
		}
		t.Error(message)
	}
}

// AssertTrue checks that a condition is true.
func (t *T) AssertTrue(cond bool, msg ...interface{}) {
	if !cond {
		message := "expected true, got false"
		if len(msg) > 0 {
			message = fmt.Sprint(msg...)
		}
		t.Error(message)
	}
}

// AssertFalse checks that a condition is false.
func (t *T) AssertFalse(cond bool, msg ...interface{}) {
	if cond {
		message := "expected false, got true"
		if len(msg) > 0 {
			message = fmt.Sprint(msg...)
		}
		t.Error(message)
	}
}

// ---- Suite ----

// Suite is a collection of related tests.
type Suite struct {
	name        string
	tests       []testEntry
	beforeAll   BeforeAllFunc
	afterAll    AfterAllFunc
	beforeEach  BeforeFunc
	afterEach   AfterFunc
	timeout     time.Duration
	parallel    bool
	maxParallel int
	properties  map[string]string
	verbose     bool
	writer      io.Writer
}

type testEntry struct {
	name string
	fn   TestFunc
}

// NewSuite creates a new test suite.
func NewSuite(name string) *Suite {
	return &Suite{
		name:        name,
		tests:       make([]testEntry, 0),
		maxParallel: runtime.NumCPU(),
		properties:  make(map[string]string),
		writer:      os.Stdout,
	}
}

// BeforeAll sets the before-all hook.
func (s *Suite) BeforeAll(fn BeforeAllFunc) {
	s.beforeAll = fn
}

// AfterAll sets the after-all hook.
func (s *Suite) AfterAll(fn AfterAllFunc) {
	s.afterAll = fn
}

// BeforeEach sets the before-each hook.
func (s *Suite) BeforeEach(fn BeforeFunc) {
	s.beforeEach = fn
}

// AfterEach sets the after-each hook.
func (s *Suite) AfterEach(fn AfterFunc) {
	s.afterEach = fn
}

// Timeout sets the default timeout for tests.
func (s *Suite) Timeout(d time.Duration) {
	s.timeout = d
}

// Parallel enables parallel test execution.
func (s *Suite) Parallel(max int) {
	s.parallel = true
	if max > 0 {
		s.maxParallel = max
	}
}

// Verbose enables verbose output.
func (s *Suite) Verbose(v bool) {
	s.verbose = v
}

// SetOutput sets the writer for test output.
func (s *Suite) SetOutput(w io.Writer) {
	s.writer = w
}

// AddTest adds a test to the suite.
func (s *Suite) AddTest(name string, fn TestFunc) {
	s.tests = append(s.tests, testEntry{name: name, fn: fn})
}

// SetProperty sets a suite-level property.
func (s *Suite) SetProperty(key, value string) {
	s.properties[key] = value
}

// Run executes all tests in the suite.
func (s *Suite) Run() *SuiteResult {
	result := &SuiteResult{
		Name:       s.name,
		StartedAt:  time.Now(),
		Properties: s.properties,
	}

	// BeforeAll
	if s.beforeAll != nil {
		if err := s.beforeAll(); err != nil {
			result.Errors++
			result.EndedAt = time.Now()
			result.Duration = result.EndedAt.Sub(result.StartedAt)
			return result
		}
	}

	// Run tests
	if s.parallel {
		result.Tests = s.runParallel()
	} else {
		for _, te := range s.tests {
			tr := s.runSingleTest(te.name, te.fn)
			result.Tests = append(result.Tests, tr)
		}
	}

	// AfterAll
	if s.afterAll != nil {
		s.afterAll()
	}

	result.EndedAt = time.Now()
	result.Duration = result.EndedAt.Sub(result.StartedAt)

	// Aggregate
	for _, tr := range result.Tests {
		result.Total++
		switch tr.Status {
		case StatusPassed:
			result.Passed++
		case StatusFailed:
			result.Failed++
		case StatusSkipped:
			result.Skipped++
		case StatusError:
			result.Errors++
		case StatusTimeout:
			result.Timeouts++
		}
	}

	return result
}

func (s *Suite) runSingleTest(name string, fn TestFunc) *TestResult {
	tr := &TestResult{
		Name:      name,
		StartedAt: time.Now(),
	}

	t := &T{
		name:      name,
		suite:     s,
		startTime: time.Now(),
		timeout:   s.timeout,
	}

	// Clone fn to avoid issues with loop variable
	testFn := fn
	success := runTest(t, testFn)

	tr.EndedAt = time.Now()
	tr.Duration = tr.EndedAt.Sub(tr.StartedAt)
	tr.Output = t.output.String()

	if t.Skipped() {
		tr.Status = StatusSkipped
	} else if t.timedOut {
		tr.Status = StatusTimeout
	} else if !success {
		if t.Failed() {
			tr.Status = StatusFailed
		} else {
			tr.Status = StatusError
		}
	} else {
		tr.Status = StatusPassed
	}

	if tr.Status == StatusFailed || tr.Status == StatusError {
		tr.Message = strings.Join(t.logs, "\n")
	}

	if s.verbose {
		statusIcon := "✓"
		if tr.Status != StatusPassed {
			statusIcon = "✗"
		}
		fmt.Fprintf(s.writer, "  %s %s (%v)\n", statusIcon, name, tr.Duration)
	}

	return tr
}

func runTest(t *T, fn TestFunc) (success bool) {
	success = true
	timedOut := false
	done := make(chan struct{})

	go func() {
		defer func() {
			if r := recover(); r != nil {
				t.mu.Lock()
				t.failed = true
				t.mu.Unlock()
				t.Logf("PANIC: %v", r)
				success = false
			}
			close(done)
		}()

		// Run beforeEach
		if t.suite.beforeEach != nil {
			t.suite.beforeEach(t)
			if t.Failed() {
				return
			}
		}

		fn(t)

		// Run afterEach
		if t.suite.afterEach != nil {
			t.suite.afterEach(t)
		}

		// Run cleanups in reverse order
		for i := len(t.cleanups) - 1; i >= 0; i-- {
			t.cleanups[i]()
		}

		if t.Failed() {
			success = false
		}
	}()

	// Wait with optional timeout
	if t.timeout > 0 {
		select {
		case <-done:
		case <-time.After(t.timeout):
			t.Log("Test timed out")
			t.Fail()
			timedOut = true
			success = false
		}
	} else {
		<-done
	}

	if timedOut {
		t.mu.Lock()
		t.timedOut = true
		t.mu.Unlock()
	}

	return success
}

func (s *Suite) runParallel() []*TestResult {
	var wg sync.WaitGroup
	sem := make(chan struct{}, s.maxParallel)
	results := make([]*TestResult, len(s.tests))

	for i, te := range s.tests {
		wg.Add(1)
		go func(idx int, name string, fn TestFunc) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[idx] = s.runSingleTest(name, fn)
		}(i, te.name, te.fn)
	}

	wg.Wait()
	return results
}

// ---- Report Aggregation ----

// JUnitTestSuite is the XML structure for JUnit reports.
type JUnitTestSuite struct {
	XMLName    xml.Name        `xml:"testsuite"`
	Name       string          `xml:"name,attr"`
	Tests      int             `xml:"tests,attr"`
	Failures   int             `xml:"failures,attr"`
	Errors     int             `xml:"errors,attr"`
	Skipped    int             `xml:"skipped,attr"`
	Time       string          `xml:"time,attr"`
	Timestamp  string          `xml:"timestamp,attr"`
	Properties []JUnitProperty `xml:"properties>property,omitempty"`
	TestCases  []JUnitTestCase `xml:"testcase"`
}

// JUnitProperty is a property in JUnit XML.
type JUnitProperty struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

// JUnitTestCase is a test case in JUnit XML.
type JUnitTestCase struct {
	Name      string        `xml:"name,attr"`
	ClassName string        `xml:"classname,attr"`
	Time      string        `xml:"time,attr"`
	Skipped   *JUnitSkipped `xml:"skipped,omitempty"`
	Failure   *JUnitFailure `xml:"failure,omitempty"`
	Error     *JUnitError   `xml:"error,omitempty"`
	SystemOut string        `xml:"system-out,omitempty"`
}

// JUnitSkipped represents a skipped test in JUnit.
type JUnitSkipped struct {
	Message string `xml:"message,attr,omitempty"`
}

// JUnitFailure represents a test failure in JUnit.
type JUnitFailure struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Text    string `xml:",chardata"`
}

// JUnitError represents a test error in JUnit.
type JUnitError struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Text    string `xml:",chardata"`
}

// ToJUnit converts a SuiteResult to JUnit XML format.
func ToJUnit(result *SuiteResult, className string) *JUnitTestSuite {
	ts := &JUnitTestSuite{
		Name:      result.Name,
		Tests:     result.Total,
		Failures:  result.Failed,
		Errors:    result.Errors,
		Skipped:   result.Skipped,
		Time:      formatDuration(result.Duration),
		Timestamp: result.StartedAt.Format(time.RFC3339),
		TestCases: make([]JUnitTestCase, 0, len(result.Tests)),
	}

	for k, v := range result.Properties {
		ts.Properties = append(ts.Properties, JUnitProperty{Name: k, Value: v})
	}

	for _, tr := range result.Tests {
		tc := JUnitTestCase{
			Name:      tr.Name,
			ClassName: className,
			Time:      formatDuration(tr.Duration),
			SystemOut: tr.Output,
		}

		switch tr.Status {
		case StatusSkipped:
			tc.Skipped = &JUnitSkipped{Message: tr.Message}
		case StatusFailed:
			tc.Failure = &JUnitFailure{
				Message: tr.Message,
				Type:    "failure",
				Text:    tr.StackTrace,
			}
		case StatusError:
			tc.Error = &JUnitError{
				Message: tr.Message,
				Type:    "error",
				Text:    tr.StackTrace,
			}
		}

		ts.TestCases = append(ts.TestCases, tc)
	}

	return ts
}

// WriteJUnitReport writes a JUnit XML report to a writer.
func WriteJUnitReport(result *SuiteResult, className string, w io.Writer) error {
	ts := ToJUnit(result, className)
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(ts); err != nil {
		return err
	}
	return nil
}

// WriteJUnitFile writes a JUnit XML report to a file.
func WriteJUnitFile(result *SuiteResult, className, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return WriteJUnitReport(result, className, f)
}

func formatDuration(d time.Duration) string {
	return fmt.Sprintf("%.3f", d.Seconds())
}

// ---- Multi-Suite Runner ----

// Runner manages multiple test suites.
type Runner struct {
	suites   []*Suite
	timeout  time.Duration
	parallel bool
	results  []*SuiteResult
	verbose  bool
	writer   io.Writer
}

// NewRunner creates a new test runner.
func NewRunner() *Runner {
	return &Runner{
		suites: make([]*Suite, 0),
		writer: os.Stdout,
	}
}

// AddSuite adds a suite to the runner.
func (r *Runner) AddSuite(s *Suite) {
	r.suites = append(r.suites, s)
}

// SetTimeout sets a global timeout for all suites.
func (r *Runner) SetTimeout(d time.Duration) {
	r.timeout = d
}

// SetParallel enables parallel suite execution.
func (r *Runner) SetParallel(v bool) {
	r.parallel = v
}

// SetVerbose enables verbose output.
func (r *Runner) SetVerbose(v bool) {
	r.verbose = v
}

// SetWriter sets the output writer.
func (r *Runner) SetWriter(w io.Writer) {
	r.writer = w
}

// RunAll executes all suites.
func (r *Runner) RunAll() []*SuiteResult {
	r.results = make([]*SuiteResult, 0)

	if r.parallel {
		return r.runSuitesParallel()
	}

	for _, s := range r.suites {
		if r.verbose {
			fmt.Fprintf(r.writer, "\n=== SUITE: %s ===\n", s.name)
		}
		s.Verbose(r.verbose)
		if r.timeout > 0 && s.timeout == 0 {
			s.Timeout(r.timeout)
		}
		s.SetOutput(r.writer)
		result := s.Run()
		r.results = append(r.results, result)

		if r.verbose {
			fmt.Fprintf(r.writer, "  %d passed, %d failed, %d skipped, %d errors (%v)\n",
				result.Passed, result.Failed, result.Skipped, result.Errors, result.Duration)
		}
	}

	return r.results
}

func (r *Runner) runSuitesParallel() []*SuiteResult {
	var wg sync.WaitGroup
	results := make([]*SuiteResult, len(r.suites))

	for i, s := range r.suites {
		wg.Add(1)
		go func(idx int, suite *Suite) {
			defer wg.Done()
			suite.Verbose(r.verbose)
			if r.timeout > 0 && suite.timeout == 0 {
				suite.Timeout(r.timeout)
			}
			suite.SetOutput(r.writer)
			results[idx] = suite.Run()
		}(i, s)
	}

	wg.Wait()
	r.results = results
	return results
}

// Results returns all suite results.
func (r *Runner) Results() []*SuiteResult { return r.results }

// AggregateSummary creates a consolidated summary.
func (r *Runner) AggregateSummary() *SuiteResult {
	summary := &SuiteResult{
		Name:  "All Suites",
		Tests: make([]*TestResult, 0),
	}

	for _, sr := range r.results {
		summary.Total += sr.Total
		summary.Passed += sr.Passed
		summary.Failed += sr.Failed
		summary.Skipped += sr.Skipped
		summary.Errors += sr.Errors
		summary.Timeouts += sr.Timeouts
		summary.Tests = append(summary.Tests, sr.Tests...)
		summary.Duration += sr.Duration
	}

	return summary
}

// WriteAllJUnit writes JUnit XML for all suites to a writer.
func (r *Runner) WriteAllJUnit(w io.Writer, classNamePrefix string) error {
	fmt.Fprintln(w, `<?xml version="1.0" encoding="UTF-8"?>`)
	fmt.Fprintln(w, `<testsuites>`)
	for i, sr := range r.results {
		className := fmt.Sprintf("%s.%s", classNamePrefix, sr.Name)
		if err := WriteJUnitReport(sr, className, w); err != nil {
			return fmt.Errorf("suite %d: %w", i, err)
		}
	}
	fmt.Fprintln(w, `</testsuites>`)
	return nil
}

// ---- Fixture Management ----

// Fixture represents a test fixture (setup/teardown resources).
type Fixture struct {
	name     string
	setup    func() (interface{}, error)
	teardown func(interface{}) error
	value    interface{}
}

// NewFixture creates a new fixture.
func NewFixture(name string, setup func() (interface{}, error), teardown func(interface{}) error) *Fixture {
	return &Fixture{
		name:     name,
		setup:    setup,
		teardown: teardown,
	}
}

// SetUp initializes the fixture.
func (f *Fixture) SetUp() error {
	v, err := f.setup()
	if err != nil {
		return err
	}
	f.value = v
	return nil
}

// TearDown cleans up the fixture.
func (f *Fixture) TearDown() error {
	if f.teardown != nil && f.value != nil {
		return f.teardown(f.value)
	}
	return nil
}

// Value returns the fixture's value.
func (f *Fixture) Value() interface{} {
	return f.value
}

// FixtureManager manages a collection of fixtures.
type FixtureManager struct {
	fixtures []*Fixture
	mu       sync.Mutex
}

// NewFixtureManager creates a fixture manager.
func NewFixtureManager() *FixtureManager {
	return &FixtureManager{
		fixtures: make([]*Fixture, 0),
	}
}

// Add registers a fixture.
func (fm *FixtureManager) Add(f *Fixture) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	fm.fixtures = append(fm.fixtures, f)
}

// SetUpAll initializes all fixtures in order.
func (fm *FixtureManager) SetUpAll() error {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	for _, f := range fm.fixtures {
		if err := f.SetUp(); err != nil {
			return fmt.Errorf("fixture '%s' setup failed: %w", f.name, err)
		}
	}
	return nil
}

// TearDownAll tears down all fixtures in reverse order.
func (fm *FixtureManager) TearDownAll() []error {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	var errs []error
	for i := len(fm.fixtures) - 1; i >= 0; i-- {
		if err := fm.fixtures[i].TearDown(); err != nil {
			errs = append(errs, fmt.Errorf("fixture '%s' teardown: %w", fm.fixtures[i].name, err))
		}
	}
	return errs
}

// ---- Tag-based test filtering ----

// TagFilter selects tests based on tags/labels.
type TagFilter struct {
	Include map[string]bool
	Exclude map[string]bool
}

// NewTagFilter creates a tag filter.
func NewTagFilter(include, exclude []string) *TagFilter {
	tf := &TagFilter{
		Include: make(map[string]bool),
		Exclude: make(map[string]bool),
	}
	for _, t := range include {
		tf.Include[t] = true
	}
	for _, t := range exclude {
		tf.Exclude[t] = true
	}
	return tf
}

// Matches returns true if the given tags pass the filter.
func (tf *TagFilter) Matches(tags []string) bool {
	// If Include is set, at least one tag must match
	if len(tf.Include) > 0 {
		found := false
		for _, t := range tags {
			if tf.Include[t] {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	// If Exclude is set, no tag must match
	for _, t := range tags {
		if tf.Exclude[t] {
			return false
		}
	}
	return true
}

// ---- Test Data Provider ----

// TestCase is a parameterized test case.
type TestCase struct {
	Name string
	Data interface{}
}

// RunTestCases runs a function against multiple test cases.
func RunTestCases(t *T, cases []TestCase, fn func(t *T, tc TestCase)) {
	for _, tc := range cases {
		t.Run(tc.Name, func(subT *T) {
			fn(subT, tc)
		})
	}
}

// ---- Benchmark Support ----

// BenchmarkResult holds the result of a benchmark.
type BenchmarkResult struct {
	Name       string
	Iterations int
	Duration   time.Duration
	NsPerOp    float64
}

// RunBenchmark runs a simple benchmark.
func RunBenchmark(name string, fn func(), minIterations int) *BenchmarkResult {
	if minIterations < 1 {
		minIterations = 1
	}

	// Warmup
	fn()

	start := time.Now()
	for i := 0; i < minIterations; i++ {
		fn()
	}
	elapsed := time.Since(start)

	return &BenchmarkResult{
		Name:       name,
		Iterations: minIterations,
		Duration:   elapsed,
		NsPerOp:    float64(elapsed.Nanoseconds()) / float64(minIterations),
	}
}

// ---- Summary utilities ----

// SummaryString returns a human-readable summary of a SuiteResult.
func SummaryString(r *SuiteResult) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Suite: %s\n", r.Name))
	sb.WriteString(fmt.Sprintf("  Duration: %v\n", r.Duration))
	sb.WriteString(fmt.Sprintf("  Total: %d | Passed: %d | Failed: %d | Skipped: %d | Errors: %d\n",
		r.Total, r.Passed, r.Failed, r.Skipped, r.Errors))

	// List failures
	failures := 0
	for _, tr := range r.Tests {
		if tr.Status != StatusPassed && tr.Status != StatusSkipped {
			if failures == 0 {
				sb.WriteString("Failures:\n")
			}
			sb.WriteString(fmt.Sprintf("    [%s] %s: %s\n", tr.Status.String(), tr.Name, tr.Message))
			failures++
		}
	}

	// List skipped
	skips := 0
	for _, tr := range r.Tests {
		if tr.Status == StatusSkipped {
			if skips == 0 {
				sb.WriteString("Skipped:\n")
			}
			sb.WriteString(fmt.Sprintf("    %s\n", tr.Name))
			skips++
		}
	}

	return sb.String()
}

// AllPassed returns true if all tests passed.
func AllPassed(r *SuiteResult) bool {
	return r.Failed == 0 && r.Errors == 0 && r.Timeouts == 0
}

// SortResults sorts test results by status then name.
func SortResults(tests []*TestResult) {
	sort.Slice(tests, func(i, j int) bool {
		if tests[i].Status != tests[j].Status {
			return tests[i].Status < tests[j].Status
		}
		return tests[i].Name < tests[j].Name
	})
}

// FilterResults returns tests matching a status.
func FilterResults(tests []*TestResult, status TestStatus) []*TestResult {
	var filtered []*TestResult
	for _, tr := range tests {
		if tr.Status == status {
			filtered = append(filtered, tr)
		}
	}
	return filtered
}
