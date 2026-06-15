package cron

import (
	"testing"
	"time"
)

func TestParseExpression_EveryMinute(t *testing.T) {
	expr, err := ParseExpression("* * * * *")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	if !expr.Matches(now) {
		t.Fatal("expected to match every minute")
	}
}

func TestParseExpression_SpecificTime(t *testing.T) {
	expr, err := ParseExpression("30 9 * * 1-5")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// Monday at 9:30.
	monday := time.Date(2024, 1, 1, 9, 30, 0, 0, time.UTC) // Jan 1 2024 is a Monday.
	if !expr.Matches(monday) {
		t.Fatal("expected to match Monday 9:30")
	}

	// Monday at 9:31 - should not match.
	notMatch := time.Date(2024, 1, 1, 9, 31, 0, 0, time.UTC)
	if expr.Matches(notMatch) {
		t.Fatal("expected NOT to match Monday 9:31")
	}

	// Saturday - should not match.
	saturday := time.Date(2024, 1, 6, 9, 30, 0, 0, time.UTC)
	if expr.Matches(saturday) {
		t.Fatal("expected NOT to match Saturday")
	}
}

func TestParseExpression_Step(t *testing.T) {
	expr, err := ParseExpression("*/5 * * * *")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// Minutes 0, 5, 10, ... should match.
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	t5 := time.Date(2024, 1, 1, 0, 5, 0, 0, time.UTC)
	t7 := time.Date(2024, 1, 1, 0, 7, 0, 0, time.UTC)

	if !expr.Matches(t0) {
		t.Fatal("expected to match minute 0")
	}
	if !expr.Matches(t5) {
		t.Fatal("expected to match minute 5")
	}
	if expr.Matches(t7) {
		t.Fatal("expected NOT to match minute 7")
	}
}

func TestParseExpression_Range(t *testing.T) {
	expr, err := ParseExpression("0 9-17 * * *")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	h9 := time.Date(2024, 1, 1, 9, 0, 0, 0, time.UTC)
	h12 := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	h17 := time.Date(2024, 1, 1, 17, 0, 0, 0, time.UTC)
	h18 := time.Date(2024, 1, 1, 18, 0, 0, 0, time.UTC)

	if !expr.Matches(h9) || !expr.Matches(h12) || !expr.Matches(h17) {
		t.Fatal("expected to match 9, 12, 17")
	}
	if expr.Matches(h18) {
		t.Fatal("expected NOT to match 18")
	}
}

func TestExpression_Next(t *testing.T) {
	expr, err := ParseExpression("0 0 * * *")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	after := time.Date(2024, 1, 1, 12, 30, 0, 0, time.UTC)
	next := expr.Next(after)

	if next.Hour() != 0 {
		t.Fatalf("expected next midnight, got %s", next)
	}
	if !next.After(after) {
		t.Fatal("next should be after the given time")
	}
}

func TestExpression_NextN(t *testing.T) {
	expr, _ := ParseExpression("0 12 * * *")
	after := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	next3 := expr.NextN(after, 3)
	if len(next3) != 3 {
		t.Fatalf("expected 3 times, got %d", len(next3))
	}
	for _, n := range next3 {
		if n.Hour() != 12 || n.Minute() != 0 {
			t.Fatalf("expected noon, got %s", n)
		}
	}
}

func TestParser_InvalidExpression(t *testing.T) {
	_, err := ParseExpression("invalid")
	if err == nil {
		t.Fatal("expected error for invalid expression")
	}

	_, err = ParseExpression("* * * *") // Only 4 fields.
	if err == nil {
		t.Fatal("expected error for 4-field expression")
	}
}

func TestScheduler_AddAndRun(t *testing.T) {
	s := NewScheduler(time.UTC)

	runCount := 0
	_, err := s.AddJob("j1", "test-job", "* * * * *", func(job *Job) error {
		runCount++
		return nil
	})
	if err != nil {
		t.Fatalf("add job: %v", err)
	}

	job := s.GetJob("j1")
	if job == nil {
		t.Fatal("expected to find job")
	}
	if !job.Enabled {
		t.Fatal("expected job to be enabled")
	}

	// Run once manually.
	if err := s.RunOnce("j1"); err != nil {
		t.Fatalf("run once: %v", err)
	}
	if runCount != 1 {
		t.Fatalf("expected 1 run, got %d", runCount)
	}
}

func TestScheduler_StartStop(t *testing.T) {
	s := NewScheduler(time.UTC)
	s.Start()
	if !s.Running() {
		t.Fatal("expected scheduler to be running")
	}
	s.Stop()
	if s.Running() {
		t.Fatal("expected scheduler to be stopped")
	}
}

func TestValidateExpression(t *testing.T) {
	if err := ValidateExpression("*/5 * * * *"); err != nil {
		t.Fatalf("expected valid: %v", err)
	}
	if err := ValidateExpression("bad"); err == nil {
		t.Fatal("expected invalid")
	}
}

func TestFormatSchedule(t *testing.T) {
	s := NewScheduler(time.UTC)
	s.AddJob("j1", "job1", "0 0 * * *", nil)
	s.AddJob("j2", "job2", "0 12 * * *", nil)

	out := s.FormatSchedule()
	if out == "" {
		t.Fatal("expected non-empty format")
	}
}
