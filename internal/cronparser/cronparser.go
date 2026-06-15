// Package cronparser provides a robust 5-field cron expression parser
// with human-readable descriptions, next-N matching, and schedule
// simulation. Supports standard cron syntax with extensions.
package cronparser

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Expression is a parsed cron expression.
type Expression struct {
	Raw        string   `json:"raw"`
	Minute     fieldSet `json:"minute"`
	Hour       fieldSet `json:"hour"`
	DayOfMonth fieldSet `json:"day_of_month"`
	Month      fieldSet `json:"month"`
	DayOfWeek  fieldSet `json:"day_of_week"`
}

type fieldKind int

const (
	kindStar  fieldKind = iota // *
	kindValue                  // 5
	kindRange                  // 1-5
	kindStep                   // */15 or 1-30/5
	kindList                   // 1,3,5
)

type fieldPart struct {
	kind  fieldKind
	start int
	end   int
	step  int
}

type fieldSet struct {
	parts []fieldPart
	min   int
	max   int
	name  string
}

// Parse parses a 5-field cron expression.
func Parse(expr string) (*Expression, error) {
	expr = strings.TrimSpace(expr)
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return nil, fmt.Errorf("cron expression requires 5 fields, got %d", len(fields))
	}

	e := &Expression{Raw: expr}

	parsers := []struct {
		name  string
		min   int
		max   int
		field *fieldSet
	}{
		{"minute", 0, 59, &e.Minute},
		{"hour", 0, 23, &e.Hour},
		{"day of month", 1, 31, &e.DayOfMonth},
		{"month", 1, 12, &e.Month},
		{"day of week", 0, 7, &e.DayOfWeek}, // 0 and 7 both = Sunday
	}

	for i, p := range parsers {
		fs, err := parseField(fields[i], p.min, p.max, p.name)
		if err != nil { return nil, err }
		*p.field = fs
	}
	return e, nil
}

func parseField(raw string, min, max int, name string) (fieldSet, error) {
	if raw == "*" {
		return fieldSet{parts: []fieldPart{{kind: kindStar, start: min, end: max, step: 1}}, min: min, max: max, name: name}, nil
	}

	var parts []fieldPart
	items := strings.Split(raw, ",")
	for _, item := range items {
		item = strings.TrimSpace(item)

		// Step: */5 or 1-30/5
		step := 1
		if idx := strings.Index(item, "/"); idx >= 0 {
			var err error
			step, err = strconv.Atoi(item[idx+1:])
			if err != nil { return fieldSet{}, fmt.Errorf("%s: invalid step: %s", name, item) }
			if step < 1 { return fieldSet{}, fmt.Errorf("%s: step must be >=1", name) }
			item = item[:idx]
		}

		if item == "*" {
			parts = append(parts, fieldPart{kind: kindStep, start: min, end: max, step: step})
		} else if idx := strings.Index(item, "-"); idx >= 0 {
			start, err := strconv.Atoi(item[:idx])
			if err != nil { return fieldSet{}, fmt.Errorf("%s: invalid range start: %s", name, item) }
			end, err := strconv.Atoi(item[idx+1:])
			if err != nil { return fieldSet{}, fmt.Errorf("%s: invalid range end: %s", name, item) }
			if start < min || end > max { return fieldSet{}, fmt.Errorf("%s: range %d-%d out of bounds [%d,%d]", name, start, end, min, max) }
			if step > 1 {
				parts = append(parts, fieldPart{kind: kindStep, start: start, end: end, step: step})
			} else {
				parts = append(parts, fieldPart{kind: kindRange, start: start, end: end, step: 1})
			}
		} else {
			val, err := strconv.Atoi(item)
			if err != nil { return fieldSet{}, fmt.Errorf("%s: invalid value: %s", name, item) }
			if val < min || val > max { return fieldSet{}, fmt.Errorf("%s: value %d out of bounds [%d,%d]", name, val, min, max) }
			parts = append(parts, fieldPart{kind: kindValue, start: val, end: val, step: 1})
		}
	}

	return fieldSet{parts: parts, min: min, max: max, name: name}, nil
}

// matches checks if a value matches this field set.
func (fs fieldSet) matches(val int) bool {
	for _, p := range fs.parts {
		switch p.kind {
		case kindStar:
			return true
		case kindValue:
			if val == p.start { return true }
		case kindRange:
			if val >= p.start && val <= p.end { return true }
		case kindStep:
			for v := p.start; v <= p.end; v += p.step {
				if v == val { return true }
			}
		}
	}
	return false
}

// Matches checks if a time matches the cron expression.
func (e *Expression) Matches(t time.Time) bool {
	return e.Minute.matches(t.Minute()) &&
		e.Hour.matches(t.Hour()) &&
		e.DayOfMonth.matches(t.Day()) &&
		e.Month.matches(int(t.Month())) &&
		e.DayOfWeek.matches(int(t.Weekday()))
}

// Next returns the next matching time after 'after'.
func (e *Expression) Next(after time.Time) time.Time {
	t := after.Add(1 * time.Minute).Truncate(time.Minute)
	limit := after.AddDate(4, 0, 0) // Search up to 4 years
	for t.Before(limit) {
		if e.Matches(t) { return t }
		t = t.Add(1 * time.Minute)
	}
	return after.AddDate(4, 0, 0) // Fallback
}

// NextN returns the next N matching times.
func (e *Expression) NextN(after time.Time, n int) []time.Time {
	var out []time.Time
	t := after
	for i := 0; i < n; i++ {
		t = e.Next(t)
		if t.After(after.AddDate(4, 0, 0)) { break }
		out = append(out, t)
	}
	return out
}

// Describe returns a human-readable description.
func (e *Expression) Describe() string {
	parts := []string{
		describeField(e.Minute, "minute"),
		describeField(e.Hour, "hour"),
		describeField(e.DayOfMonth, "day of month"),
		describeField(e.Month, "month"),
		describeField(e.DayOfWeek, "day of week"),
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "'%s' — ", e.Raw)
	if e.isEveryMinute() {
		sb.WriteString("every minute")
	} else {
		sb.WriteString(strings.Join(parts, ", "))
	}
	return sb.String()
}

func (e *Expression) isEveryMinute() bool {
	return len(e.Minute.parts) == 1 && e.Minute.parts[0].kind == kindStar
}

func describeField(fs fieldSet, unit string) string {
	if len(fs.parts) == 0 { return "every " + unit }
	if len(fs.parts) == 1 {
		p := fs.parts[0]
		switch p.kind {
		case kindStar: return "every " + unit
		case kindValue:
			return fmt.Sprintf("at %s %d", singular(unit), p.start)
		case kindRange:
			return fmt.Sprintf("%s %d through %d", unit, p.start, p.end)
		case kindStep:
			if p.start == fs.min && p.end == fs.max {
				return fmt.Sprintf("every %d %ss", p.step, unit)
			}
			return fmt.Sprintf("every %d %ss from %d to %d", p.step, unit, p.start, p.end)
		}
	}
	// List of values
	vals := []string{}
	for _, p := range fs.parts {
		if p.kind == kindValue {
			vals = append(vals, strconv.Itoa(p.start))
		}
	}
	if len(vals) > 0 {
		return fmt.Sprintf("at %s %s", unit, strings.Join(vals, ", "))
	}
	return "every " + unit
}

func singular(s string) string {
	return strings.TrimSuffix(s, "s")
}

// FormatSchedule prints the next N runs of a cron expression.
func FormatSchedule(expr string, n int) string {
	e, err := Parse(expr)
	if err != nil {
		return fmt.Sprintf("Invalid cron expression: %v\n", err)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Cron: %s\n", e.Describe())
	fmt.Fprintf(&sb, "%s\n\n", strings.Repeat("─", 50))

	times := e.NextN(time.Now(), n)
	fmt.Fprintf(&sb, "Next %d runs:\n", n)
	for i, t := range times {
		relative := time.Until(t).Round(time.Second)
		fmt.Fprintf(&sb, "  %d. %s (in %v)\n", i+1, t.Format("2006-01-02 15:04:05 Mon"), relative)
	}
	return sb.String()
}

// ── Schedule Simulator ────────────────────────────────────

// Simulator simulates cron execution over a time range.
type Simulator struct {
	expr *Expression
}

// NewSimulator creates a schedule simulator.
func NewSimulator(expr string) (*Simulator, error) {
	e, err := Parse(expr)
	if err != nil { return nil, err }
	return &Simulator{expr: e}, nil
}

// Simulate returns all matching times in a range.
func (s *Simulator) Simulate(start, end time.Time) []time.Time {
	var matches []time.Time
	t := start.Truncate(time.Minute)
	for t.Before(end) {
		if s.expr.Matches(t) { matches = append(matches, t) }
		t = t.Add(1 * time.Minute)
	}
	return matches
}

// CountMatches counts how many times the expression matches in a range.
func (s *Simulator) CountMatches(start, end time.Time) int {
	return len(s.Simulate(start, end))
}
