package harness

import (
	"bytes"
	"encoding/xml"
	"os"
	"strings"
	"testing"
	"time"
)

func TestNewSuite(t *testing.T) {
	s := NewSuite("test-suite")
	if s.name != "test-suite" {
		t.Errorf("expected name 'test-suite', got %q", s.name)
	}
}

func TestSuite_RunSingleTest(t *testing.T) {
	s := NewSuite("single-test-suite")
	s.AddTest("test-pass", func(ht *T) {
		ht.Log("this test passes")
	})

	result := s.Run()
	if result.Total != 1 {
		t.Errorf("expected 1 test, got %d", result.Total)
	}
	if result.Passed != 1 {
		t.Errorf("expected 1 passed, got %d", result.Passed)
	}
}

func TestSuite_RunFailingTest(t *testing.T) {
	s := NewSuite("failing-suite")
	s.AddTest("test-fail", func(ht *T) {
		ht.Error("intentional failure")
	})

	result := s.Run()
	if result.Failed != 1 {
		t.Errorf("expected 1 failed, got %d", result.Failed)
	}
}

func TestSuite_RunSkippedTest(t *testing.T) {
	s := NewSuite("skip-suite")
	s.AddTest("test-skip", func(ht *T) {
		ht.Skip("intentional skip")
	})

	result := s.Run()
	if result.Skipped != 1 {
		t.Errorf("expected 1 skipped, got %d", result.Skipped)
	}
}

func TestSuite_BeforeAfter(t *testing.T) {
	order := make([]string, 0)

	s := NewSuite("lifecycle-suite")
	s.BeforeAll(func() error {
		order = append(order, "beforeAll")
		return nil
	})
	s.AfterAll(func() error {
		order = append(order, "afterAll")
		return nil
	})
	s.BeforeEach(func(ht *T) {
		order = append(order, "beforeEach")
	})
	s.AfterEach(func(ht *T) {
		order = append(order, "afterEach")
	})
	s.AddTest("test1", func(ht *T) {
		order = append(order, "test1")
	})

	s.Run()

	expected := []string{"beforeAll", "beforeEach", "test1", "afterEach", "afterAll"}
	if len(order) != len(expected) {
		t.Errorf("expected len %d, got %d: %v", len(expected), len(order), order)
	}
}

func TestSuite_Parallel(t *testing.T) {
	s := NewSuite("parallel-suite")
	s.Parallel(4)

	for i := 0; i < 10; i++ {
		idx := i
		s.AddTest("parallel-test", func(ht *T) {
			ht.Logf("test %d", idx)
			time.Sleep(10 * time.Millisecond)
		})
	}

	result := s.Run()
	if result.Total != 10 {
		t.Errorf("expected 10 tests, got %d", result.Total)
	}
}

func TestT_Assertions(t *testing.T) {
	s := NewSuite("assert-suite")

	s.AddTest("assert-equal-pass", func(ht *T) {
		ht.AssertEqual(42, 42)
	})
	s.AddTest("assert-equal-fail", func(ht *T) {
		ht.AssertEqual(42, 43)
	})
	s.AddTest("assert-true", func(ht *T) {
		ht.AssertTrue(1 == 1)
	})
	s.AddTest("assert-false", func(ht *T) {
		ht.AssertFalse(1 == 2)
	})
	s.AddTest("assert-nil", func(ht *T) {
		ht.AssertNil(nil)
	})
	s.AddTest("assert-not-nil", func(ht *T) {
		ht.AssertNotNil("string")
	})

	result := s.Run()
	if result.Failed < 1 {
		t.Errorf("expected at least 1 failure (assert-equal-fail), got %d", result.Failed)
	}
	if result.Passed < 5 {
		t.Errorf("expected at least 5 passed, got %d", result.Passed)
	}
}

func TestT_Subtest(t *testing.T) {
	s := NewSuite("subtest-suite")
	s.AddTest("parent", func(ht *T) {
		ht.Run("child1", func(cht *T) {
			cht.Log("child 1")
		})
		ht.Run("child2", func(cht *T) {
			cht.Error("child 2 fails")
		})
	})

	result := s.Run()
	// The parent test is one entry; subtest results are embedded in output
	if result.Total < 1 {
		t.Errorf("expected at least 1 test, got %d", result.Total)
	}
}

func TestT_Cleanup(t *testing.T) {
	cleaned := false
	s := NewSuite("cleanup-suite")
	s.AddTest("test-with-cleanup", func(ht *T) {
		ht.Cleanup(func() {
			cleaned = true
		})
		ht.Log("test body")
	})
	s.Run()
	if !cleaned {
		t.Error("cleanup should have run")
	}
}

func TestT_Fatal(t *testing.T) {
	s := NewSuite("fatal-suite")
	afterFatal := false
	s.AddTest("test-fatal", func(ht *T) {
		ht.Fatal("fatal error")
		afterFatal = true
	})
	s.Run()
	if afterFatal {
		t.Error("code after Fatal should not execute")
	}
}

func TestSuite_Timeout(t *testing.T) {
	s := NewSuite("timeout-suite")
	s.Timeout(50 * time.Millisecond)
	s.AddTest("slow-test", func(ht *T) {
		time.Sleep(200 * time.Millisecond)
	})
	result := s.Run()
	if result.Timeouts != 1 {
		t.Errorf("expected 1 timeout, got %d (status distribution: pass=%d fail=%d skip=%d err=%d timeout=%d)",
			result.Timeouts, result.Passed, result.Failed, result.Skipped, result.Errors, result.Timeouts)
	}
}

func TestJUnitOutput(t *testing.T) {
	s := NewSuite("junit-suite")
	s.AddTest("test-a", func(ht *T) {
		ht.Log("all good")
	})
	s.AddTest("test-b", func(ht *T) {
		ht.Error("something wrong")
	})
	s.AddTest("test-c", func(ht *T) {
		ht.Skip("not now")
	})

	result := s.Run()

	var buf bytes.Buffer
	err := WriteJUnitReport(result, "com.example.Test", &buf)
	if err != nil {
		t.Fatalf("WriteJUnitReport error: %v", err)
	}

	// Parse back to verify
	var jUnit JUnitTestSuite
	if err := xml.Unmarshal(buf.Bytes(), &jUnit); err != nil {
		t.Fatalf("invalid XML: %v\n%s", err, buf.String())
	}
	if jUnit.Tests != 3 {
		t.Errorf("expected 3 tests, got %d", jUnit.Tests)
	}
	if jUnit.Failures != 1 {
		t.Errorf("expected 1 failure, got %d", jUnit.Failures)
	}
	if jUnit.Skipped != 1 {
		t.Errorf("expected 1 skipped, got %d", jUnit.Skipped)
	}
}

func TestJUnitFile(t *testing.T) {
	s := NewSuite("file-suite")
	s.AddTest("t1", func(ht *T) { ht.Log("ok") })

	result := s.Run()
	tmpFile := t.TempDir() + "/report.xml"
	err := WriteJUnitFile(result, "test.Class", tmpFile)
	if err != nil {
		t.Fatalf("WriteJUnitFile error: %v", err)
	}
	if _, err := os.Stat(tmpFile); os.IsNotExist(err) {
		t.Error("report file should exist")
	}
}

func TestRunner(t *testing.T) {
	r := NewRunner()

	s1 := NewSuite("suite-1")
	s1.AddTest("t1", func(ht *T) { ht.Log("ok") })
	s2 := NewSuite("suite-2")
	s2.AddTest("t2", func(ht *T) { ht.Error("fail") })

	r.AddSuite(s1)
	r.AddSuite(s2)

	results := r.RunAll()
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}

func TestRunner_AggregateSummary(t *testing.T) {
	r := NewRunner()
	s1 := NewSuite("s1")
	s1.AddTest("pass", func(ht *T) {})
	s1.AddTest("fail", func(ht *T) { ht.Error("x") })

	r.AddSuite(s1)
	r.RunAll()

	summary := r.AggregateSummary()
	if summary.Total != 2 {
		t.Errorf("expected 2 total, got %d", summary.Total)
	}
	if summary.Passed != 1 {
		t.Errorf("expected 1 passed, got %d", summary.Passed)
	}
}

func TestRunner_AllJUnit(t *testing.T) {
	r := NewRunner()
	s1 := NewSuite("suite-a")
	s1.AddTest("t1", func(ht *T) {})
	r.AddSuite(s1)
	r.RunAll()

	var buf bytes.Buffer
	err := r.WriteAllJUnit(&buf, "com.test")
	if err != nil {
		t.Fatalf("WriteAllJUnit error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "<testsuites>") {
		t.Error("should contain testsuites element")
	}
	if !strings.Contains(output, "suite-a") {
		t.Error("should contain suite name")
	}
}

func TestFixtureManager(t *testing.T) {
	fm := NewFixtureManager()
	setupVal := 0

	f := NewFixture("db", func() (interface{}, error) {
		setupVal = 42
		return 42, nil
	}, func(v interface{}) error {
		setupVal = 0
		return nil
	})

	fm.Add(f)
	if err := fm.SetUpAll(); err != nil {
		t.Fatalf("SetUpAll error: %v", err)
	}
	if setupVal != 42 {
		t.Errorf("expected 42, got %d", setupVal)
	}

	errs := fm.TearDownAll()
	if len(errs) > 0 {
		t.Errorf("teardown errors: %v", errs)
	}
	if setupVal != 0 {
		t.Error("teardown should have reset value")
	}
}

func TestTagFilter(t *testing.T) {
	tf := NewTagFilter([]string{"smoke", "regression"}, []string{"slow"})

	if !tf.Matches([]string{"smoke"}) {
		t.Error("should match smoke")
	}
	if tf.Matches([]string{"unit"}) {
		t.Error("should not match unit without included tag")
	}
	if tf.Matches([]string{"smoke", "slow"}) {
		t.Error("should not match because of excluded 'slow'")
	}
}

func TestTagFilter_EmptyInclude(t *testing.T) {
	tf := NewTagFilter(nil, []string{"flaky"})
	if !tf.Matches([]string{"anything"}) {
		t.Error("should match when include is empty")
	}
	if tf.Matches([]string{"flaky"}) {
		t.Error("should not match excluded")
	}
}

func TestRunTestCases(t *testing.T) {
	s := NewSuite("param-suite")
	s.AddTest("parameterized", func(ht *T) {
		cases := []TestCase{
			{Name: "case-1", Data: 1},
			{Name: "case-2", Data: 2},
		}
		count := 0
		RunTestCases(ht, cases, func(subT *T, tc TestCase) {
			count++
		})
		ht.AssertEqual(2, count)
	})

	result := s.Run()
	if result.Passed != 1 {
		t.Errorf("expected 1 passed, got %d", result.Passed)
	}
}

func TestRunBenchmark(t *testing.T) {
	br := RunBenchmark("test-bench", func() {
		_ = 1 + 1
	}, 10000)

	if br.Iterations != 10000 {
		t.Errorf("expected 10000 iterations, got %d", br.Iterations)
	}
	if br.NsPerOp <= 0 {
		t.Error("NsPerOp should be positive")
	}
	if br.Name != "test-bench" {
		t.Errorf("expected name 'test-bench', got %q", br.Name)
	}
}

func TestSummaryString(t *testing.T) {
	s := NewSuite("summary-suite")
	s.AddTest("passing", func(ht *T) {})
	s.AddTest("failing", func(ht *T) { ht.Error("fail") })

	result := s.Run()
	summary := SummaryString(result)
	if !strings.Contains(summary, "fail") {
		t.Error("summary should mention failure details")
	}
	if !strings.Contains(summary, "Failures") {
		t.Error("summary should contain failures section")
	}
	if !strings.Contains(summary, "Passed: 1") {
		t.Error("summary should show passed count")
	}
}

func TestAllPassed(t *testing.T) {
	r := &SuiteResult{Passed: 5, Total: 5}
	if !AllPassed(r) {
		t.Error("should be all passed")
	}
	r.Failed = 1
	if AllPassed(r) {
		t.Error("should not be all passed")
	}
}

func TestSortResults(t *testing.T) {
	tests := []*TestResult{
		{Name: "c", Status: StatusFailed},
		{Name: "a", Status: StatusPassed},
		{Name: "b", Status: StatusSkipped},
	}
	SortResults(tests)
	if tests[0].Name != "a" {
		t.Errorf("expected 'a' first, got %q", tests[0].Name)
	}
}

func TestFilterResults(t *testing.T) {
	tests := []*TestResult{
		{Name: "a", Status: StatusPassed},
		{Name: "b", Status: StatusFailed},
		{Name: "c", Status: StatusPassed},
	}
	passed := FilterResults(tests, StatusPassed)
	if len(passed) != 2 {
		t.Errorf("expected 2 passed, got %d", len(passed))
	}
}

func TestTestStatus_String(t *testing.T) {
	if StatusPassed.String() != "passed" {
		t.Error("unexpected StatusPassed string")
	}
	if StatusTimeout.String() != "timeout" {
		t.Error("unexpected StatusTimeout string")
	}
}

func TestT_Logf(t *testing.T) {
	s := NewSuite("logf-suite")
	var captured string
	s.writer = &bytes.Buffer{}
	s.AddTest("t", func(ht *T) {
		ht.Logf("formatted: %d", 42)
		captured = ht.output.String()
	})
	s.Run()
	if !strings.Contains(captured, "formatted: 42") {
		t.Errorf("expected formatted output, got %q", captured)
	}
}

func TestT_Errorf(t *testing.T) {
	s := NewSuite("errorf-suite")
	s.AddTest("t", func(ht *T) {
		ht.Errorf("error: %s", "bad")
	})
	result := s.Run()
	if result.Failed != 1 {
		t.Errorf("expected 1 failure, got %d", result.Failed)
	}
}

func TestT_Skipf(t *testing.T) {
	s := NewSuite("skipf-suite")
	s.AddTest("t", func(ht *T) {
		ht.Skipf("skip reason: %d", 123)
	})
	result := s.Run()
	if result.Skipped != 1 {
		t.Errorf("expected 1 skipped, got %d", result.Skipped)
	}
}

func TestT_AssertNotEqual(t *testing.T) {
	s := NewSuite("neq-suite")
	s.AddTest("neq-pass", func(ht *T) {
		ht.AssertNotEqual(1, 2)
	})
	s.AddTest("neq-fail", func(ht *T) {
		ht.AssertNotEqual(1, 1)
	})
	result := s.Run()
	if result.Failed != 1 {
		t.Errorf("expected 1 failure, got %d", result.Failed)
	}
}

func TestFixture_Value(t *testing.T) {
	f := NewFixture("fixt", func() (interface{}, error) {
		return "val", nil
	}, nil)
	f.SetUp()
	if f.Value() != "val" {
		t.Errorf("expected 'val', got %v", f.Value())
	}
}
