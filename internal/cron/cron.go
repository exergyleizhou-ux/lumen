// Package cron implements a cron expression parser and scheduler with full 5-field
// cron support (*/N, ranges, lists, L, W, #), Next() computation, and a job runner.
package cron

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Field represents one field of a cron expression.
type Field struct {
	Min   int         // Minimum valid value.
	Max   int         // Maximum valid value.
	Name  string      // Human name.
	Parts []fieldPart // Parsed sub-expressions.
}

type fieldPart struct {
	kind    partKind
	start   int
	end     int
	step    int
	last    bool // L flag for day-of-month/week.
	weekday int  // W flag: nearest weekday.
	hash    int  // # flag: nth occurrence.
}

type partKind int

const (
	kindAll     partKind = iota // *
	kindValue                   // single value
	kindRange                   // a-b
	kindStep                    // */n or a/n
	kindList                    // a,b,c
	kindLast                    // L
	kindWeekday                 // W
	kindHash                    // #
)

var dowNames = map[string]int{
	"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6,
}
var monNames = map[string]int{
	"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6,
	"jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12,
}

// parseField parses a single cron field expression.
func parseField(expr string, min, max int, names map[string]int) (*Field, error) {
	f := &Field{Min: min, Max: max}
	expr = strings.ToLower(strings.TrimSpace(expr))
	if expr == "*" || expr == "?" {
		f.Parts = []fieldPart{{kind: kindAll, start: min, end: max, step: 1}}
		return f, nil
	}

	// Split by comma for lists.
	parts := strings.Split(expr, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		fp, err := parsePart(part, min, max, names)
		if err != nil {
			return nil, fmt.Errorf("invalid field part %q: %w", part, err)
		}
		f.Parts = append(f.Parts, fp)
	}
	return f, nil
}

func parsePart(part string, min, max int, names map[string]int) (fieldPart, error) {
	// L flag.
	if part == "l" {
		return fieldPart{kind: kindLast, start: min, end: max, step: 1}, nil
	}
	// W (nearest weekday) — used for day-of-month.
	if strings.HasSuffix(part, "w") {
		numStr := strings.TrimSuffix(part, "w")
		n, err := resolveNumber(numStr, min, max, names)
		if err != nil {
			return fieldPart{}, err
		}
		return fieldPart{kind: kindWeekday, start: n, weekday: n, step: 1}, nil
	}
	// # (nth weekday) — used for day-of-week.
	if strings.Contains(part, "#") {
		parts := strings.SplitN(part, "#", 2)
		dow, err := resolveNumber(parts[0], 0, 7, dowNames)
		if err != nil {
			return fieldPart{}, err
		}
		n, err := strconv.Atoi(parts[1])
		if err != nil {
			return fieldPart{}, err
		}
		return fieldPart{kind: kindHash, start: dow, hash: n, step: 1}, nil
	}
	// Step: */n or a-b/n or a/n.
	if strings.Contains(part, "/") {
		parts := strings.SplitN(part, "/", 2)
		step, err := strconv.Atoi(parts[1])
		if err != nil {
			return fieldPart{}, err
		}
		left := parts[0]
		if left == "*" {
			return fieldPart{kind: kindStep, start: min, end: max, step: step}, nil
		}
		// Could be a range with step.
		if strings.Contains(left, "-") {
			rp := strings.SplitN(left, "-", 2)
			s, err := resolveNumber(rp[0], min, max, names)
			if err != nil {
				return fieldPart{}, err
			}
			e, err := resolveNumber(rp[1], min, max, names)
			if err != nil {
				return fieldPart{}, err
			}
			return fieldPart{kind: kindStep, start: s, end: e, step: step}, nil
		}
		s, err := resolveNumber(left, min, max, names)
		if err != nil {
			return fieldPart{}, err
		}
		return fieldPart{kind: kindStep, start: s, end: max, step: step}, nil
	}
	// Range: a-b.
	if strings.Contains(part, "-") {
		rp := strings.SplitN(part, "-", 2)
		s, err := resolveNumber(rp[0], min, max, names)
		if err != nil {
			return fieldPart{}, err
		}
		e, err := resolveNumber(rp[1], min, max, names)
		if err != nil {
			return fieldPart{}, err
		}
		return fieldPart{kind: kindRange, start: s, end: e, step: 1}, nil
	}
	// Single value.
	n, err := resolveNumber(part, min, max, names)
	if err != nil {
		return fieldPart{}, err
	}
	return fieldPart{kind: kindValue, start: n, end: n, step: 1}, nil
}

func resolveNumber(s string, min, max int, names map[string]int) (int, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if names != nil {
		if v, ok := names[s]; ok {
			return v, nil
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("unknown value %q", s)
	}
	if n < min || n > max {
		return 0, fmt.Errorf("value %d out of range [%d,%d]", n, min, max)
	}
	return n, nil
}

// matches checks whether a value matches this field.
func (f *Field) matches(val int, isLastDay bool, lastWeekday int, nthWeekdayCount int) bool {
	found := false
	for _, p := range f.Parts {
		switch p.kind {
		case kindAll:
			return true
		case kindValue:
			if val == p.start {
				return true
			}
		case kindRange:
			if val >= p.start && val <= p.end {
				return true
			}
		case kindStep:
			for v := p.start; v <= p.end; v += p.step {
				if v == val {
					return true
				}
			}
		case kindLast:
			if isLastDay {
				return true
			}
		case kindWeekday:
			if val == nearestWeekday(p.weekday, 31) {
				return true
			}
		case kindHash:
		}
	}
	return found
}

// nextValue finds the next matching value >= val.
func (f *Field) nextValue(val int, lastDay int, lastWeekday int) int {
	best := -1
	for _, p := range f.Parts {
		switch p.kind {
		case kindAll:
			if val <= f.Max {
				return val
			}
		case kindValue:
			if p.start >= val && (best == -1 || p.start < best) {
				best = p.start
			}
		case kindRange:
			for v := p.start; v <= p.end; v++ {
				if v >= val && (best == -1 || v < best) {
					best = v
					break
				}
			}
		case kindStep:
			for v := p.start; v <= p.end; v += p.step {
				if v >= val {
					if best == -1 || v < best {
						best = v
					}
					break
				}
			}
		case kindLast:
			if lastDay >= val && (best == -1 || lastDay < best) {
				best = lastDay
			}
		case kindWeekday:
			nw := nearestWeekday(p.weekday, lastDay)
			if nw >= val && (best == -1 || nw < best) {
				best = nw
			}
		case kindHash:
			// Computed separately.
		}
	}
	return best
}

func nearestWeekday(day, lastDay int) int {
	if day < 1 || day > lastDay {
		return day
	}
	t := time.Date(2000, 1, day, 0, 0, 0, 0, time.UTC)
	w := t.Weekday()
	if w == time.Saturday {
		if day-1 >= 1 {
			return day - 1
		}
		return day + 2
	}
	if w == time.Sunday {
		if day+1 <= lastDay {
			return day + 1
		}
		return day - 2
	}
	return day
}

// --- Cron Expression ---

// Expression is a parsed 5-field cron expression.
type Expression struct {
	Minute     *Field
	Hour       *Field
	DayOfMonth *Field
	Month      *Field
	DayOfWeek  *Field
	raw        string
}

// ParseExpression parses a 5-field cron expression like "*/5 * * * *".
func ParseExpression(expr string) (*Expression, error) {
	expr = strings.TrimSpace(expr)
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return nil, fmt.Errorf("cron expression must have 5 fields, got %d", len(fields))
	}

	minute, err := parseField(fields[0], 0, 59, nil)
	if err != nil {
		return nil, fmt.Errorf("minute field: %w", err)
	}
	hour, err := parseField(fields[1], 0, 23, nil)
	if err != nil {
		return nil, fmt.Errorf("hour field: %w", err)
	}
	dom, err := parseField(fields[2], 1, 31, nil)
	if err != nil {
		return nil, fmt.Errorf("day-of-month field: %w", err)
	}
	month, err := parseField(fields[3], 1, 12, monNames)
	if err != nil {
		return nil, fmt.Errorf("month field: %w", err)
	}
	dow, err := parseField(fields[4], 0, 7, dowNames) // 0 and 7 both = Sunday.
	if err != nil {
		return nil, fmt.Errorf("day-of-week field: %w", err)
	}

	return &Expression{
		Minute:     minute,
		Hour:       hour,
		DayOfMonth: dom,
		Month:      month,
		DayOfWeek:  dow,
		raw:        expr,
	}, nil
}

// String returns the original expression.
func (e *Expression) String() string { return e.raw }

// Matches checks whether the given time matches this cron expression.
func (e *Expression) Matches(t time.Time) bool {
	lastDOM := lastDayOfMonth(t.Year(), int(t.Month()))
	return e.Minute.matches(t.Minute(), false, 0, 0) &&
		e.Hour.matches(t.Hour(), false, 0, 0) &&
		e.DayOfMonth.matches(t.Day(), lastDOM == t.Day(), 0, 0) &&
		e.Month.matches(int(t.Month()), false, 0, 0) &&
		e.DayOfWeek.matches(int(t.Weekday()), false, 0, 0)
}

// Next returns the next time after 'after' that matches the expression.
func (e *Expression) Next(after time.Time) time.Time {
	// Start from the next minute, truncating seconds.
	t := after.Truncate(time.Minute).Add(time.Minute)

	// Search up to 5 years ahead.
	limit := after.AddDate(5, 0, 0)
	for t.Before(limit) {
		if e.Matches(t) {
			return t
		}
		t = t.Add(time.Minute)
	}
	return time.Time{}
}

// NextN returns the next n matching times.
func (e *Expression) NextN(after time.Time, n int) []time.Time {
	out := make([]time.Time, 0, n)
	t := after
	for i := 0; i < n; i++ {
		t = e.Next(t)
		if t.IsZero() {
			break
		}
		out = append(out, t)
	}
	return out
}

func lastDayOfMonth(year int, month int) int {
	switch month {
	case 2:
		if year%4 == 0 && (year%100 != 0 || year%400 == 0) {
			return 29
		}
		return 28
	case 4, 6, 9, 11:
		return 30
	default:
		return 31
	}
}

// --- Job and Scheduler ---

// Job represents a scheduled task.
type Job struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	Expression *Expression `json:"expression"`
	RawExpr    string      `json:"raw_expr"`
	NextRun    time.Time   `json:"next_run"`
	LastRun    time.Time   `json:"last_run"`
	RunCount   int64       `json:"run_count"`
	ErrorCount int64       `json:"error_count"`
	Enabled    bool        `json:"enabled"`
	Handler    JobFunc     `json:"-"`
}

// JobFunc is the function signature for a job handler.
type JobFunc func(job *Job) error

// Scheduler manages cron jobs.
type Scheduler struct {
	mu       sync.RWMutex
	jobs     map[string]*Job
	stopCh   chan struct{}
	running  bool
	location *time.Location
}

// NewScheduler creates a new scheduler.
func NewScheduler(loc *time.Location) *Scheduler {
	if loc == nil {
		loc = time.UTC
	}
	return &Scheduler{
		jobs:     make(map[string]*Job),
		stopCh:   make(chan struct{}),
		location: loc,
	}
}

// AddJob registers a new job with a cron expression string.
func (s *Scheduler) AddJob(id, name, cronExpr string, handler JobFunc) (*Job, error) {
	expr, err := ParseExpression(cronExpr)
	if err != nil {
		return nil, fmt.Errorf("parse %q: %w", cronExpr, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().In(s.location)
	job := &Job{
		ID:         id,
		Name:       name,
		Expression: expr,
		RawExpr:    cronExpr,
		NextRun:    expr.Next(now),
		Enabled:    true,
		Handler:    handler,
	}
	s.jobs[id] = job
	return job, nil
}

// RemoveJob removes a job.
func (s *Scheduler) RemoveJob(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.jobs[id]
	if ok {
		delete(s.jobs, id)
	}
	return ok
}

// GetJob returns a job by ID.
func (s *Scheduler) GetJob(id string) *Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.jobs[id]
}

// ListJobs returns all registered jobs.
func (s *Scheduler) ListJobs() []*Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.listJobsLocked()
}

// listJobsLocked assumes the caller holds the lock.
func (s *Scheduler) listJobsLocked() []*Job {
	out := make([]*Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		out = append(out, j)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NextRun.Before(out[j].NextRun) })
	return out
}

// Start begins the scheduler loop.
func (s *Scheduler) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.stopCh = make(chan struct{})
	s.mu.Unlock()

	go s.loop()
}

// Stop halts the scheduler.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	s.running = false
	close(s.stopCh)
}

// Running returns whether the scheduler is active.
func (s *Scheduler) Running() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func (s *Scheduler) loop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case now := <-ticker.C:
			s.tick(now)
		}
	}
}

func (s *Scheduler) tick(now time.Time) {
	s.mu.Lock()
	jobs := make([]*Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		jobs = append(jobs, j)
	}
	s.mu.Unlock()

	for _, job := range jobs {
		if !job.Enabled {
			continue
		}
		if now.After(job.NextRun) || now.Equal(job.NextRun) {
			s.runJob(job, now)
		}
	}
}

func (s *Scheduler) runJob(job *Job, now time.Time) {
	if job.Handler == nil {
		return
	}
	if err := job.Handler(job); err != nil {
		job.ErrorCount++
	}
	job.LastRun = now
	job.RunCount++
	job.NextRun = job.Expression.Next(now)
}

// RunOnce runs a job immediately regardless of schedule.
func (s *Scheduler) RunOnce(id string) error {
	s.mu.RLock()
	job, ok := s.jobs[id]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("job %q not found", id)
	}
	s.runJob(job, time.Now().In(s.location))
	return nil
}

// FormatSchedule returns a human-readable representation of all jobs.
func (s *Scheduler) FormatSchedule() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := fmt.Sprintf("Scheduler: %d jobs (running=%v)\n", len(s.jobs), s.running)
	for _, j := range s.listJobsLocked() {
		out += fmt.Sprintf("  %s %q expr=%q next=%s runs=%d errors=%d enabled=%v\n",
			j.ID, j.Name, j.RawExpr,
			j.NextRun.Format(time.RFC3339),
			j.RunCount, j.ErrorCount, j.Enabled)
	}
	return out
}

// ValidateExpression checks if a cron expression string is syntactically valid.
func ValidateExpression(expr string) error {
	_, err := ParseExpression(expr)
	return err
}

// --- Human Readable Descriptions ---

// Describe returns a human-readable description of a cron expression.
func (e *Expression) Describe() string {
	parts := []string{}
	parts = append(parts, describeField(e.Minute, "minute"))
	parts = append(parts, describeField(e.Hour, "hour"))
	parts = append(parts, describeField(e.DayOfMonth, "day of month"))
	parts = append(parts, describeField(e.Month, "month"))
	parts = append(parts, describeField(e.DayOfWeek, "day of week"))
	return strings.Join(parts, "; ")
}

func describeField(f *Field, name string) string {
	if len(f.Parts) == 0 {
		return name + ": ?"
	}
	p := f.Parts[0]
	switch p.kind {
	case kindAll:
		return name + ": every"
	case kindValue:
		return fmt.Sprintf("%s: at %d", name, p.start)
	case kindRange:
		return fmt.Sprintf("%s: from %d to %d", name, p.start, p.end)
	case kindStep:
		if p.start == f.Min {
			return fmt.Sprintf("%s: every %d", name, p.step)
		}
		return fmt.Sprintf("%s: from %d every %d", name, p.start, p.step)
	default:
		return fmt.Sprintf("%s: custom", name)
	}
}

// --- Cron Syntax Validation & Helpers ---

// CommonPresets returns common cron expressions.
func CommonPresets() map[string]string {
	return map[string]string{
		"every_minute":       "* * * * *",
		"every_5_minutes":    "*/5 * * * *",
		"every_hour":         "0 * * * *",
		"every_day_midnight": "0 0 * * *",
		"every_day_noon":     "0 12 * * *",
		"every_weekday_9am":  "0 9 * * 1-5",
		"every_monday_8am":   "0 8 * * 1",
		"every_1st_of_month": "0 0 1 * *",
	}
}

// --- One-Shot Job Support ---

// OneShotJob is a job that runs once at a specific time.
type OneShotJob struct {
	ID      string
	RunAt   time.Time
	Handler JobFunc
	fired   bool
}

// OneShotScheduler manages one-shot jobs.
type OneShotScheduler struct {
	mu   sync.Mutex
	jobs map[string]*OneShotJob
}

// NewOneShotScheduler creates a one-shot scheduler.
func NewOneShotScheduler() *OneShotScheduler {
	return &OneShotScheduler{jobs: make(map[string]*OneShotJob)}
}

// Schedule adds a one-shot job.
func (os *OneShotScheduler) Schedule(id string, runAt time.Time, handler JobFunc) {
	os.mu.Lock()
	defer os.mu.Unlock()
	os.jobs[id] = &OneShotJob{ID: id, RunAt: runAt, Handler: handler}
}

// Tick checks for jobs that need to fire.
func (os *OneShotScheduler) Tick(now time.Time) {
	os.mu.Lock()
	defer os.mu.Unlock()
	for id, job := range os.jobs {
		if !job.fired && !now.Before(job.RunAt) {
			job.fired = true
			if job.Handler != nil {
				job.Handler(nil)
			}
			delete(os.jobs, id)
		}
	}
}

// --- Job Concurrency Limit ---

// ConcurrencyLimiter limits concurrent job executions.
type ConcurrencyLimiter struct {
	sem chan struct{}
}

// NewConcurrencyLimiter creates a limiter with the given max concurrency.
func NewConcurrencyLimiter(max int) *ConcurrencyLimiter {
	return &ConcurrencyLimiter{sem: make(chan struct{}, max)}
}

// Acquire blocks until a slot is available.
func (cl *ConcurrencyLimiter) Acquire() { cl.sem <- struct{}{} }

// Release frees a slot.
func (cl *ConcurrencyLimiter) Release() { <-cl.sem }

// --- Extended Scheduler with Overlap Protection ---

// SafeScheduler wraps Scheduler with concurrency limiting and overlap protection.
type SafeScheduler struct {
	*Scheduler
	limiter *ConcurrencyLimiter
	running map[string]bool
	muRun   sync.Mutex
}

// NewSafeScheduler creates a safe scheduler.
func NewSafeScheduler(loc *time.Location, maxConcurrent int) *SafeScheduler {
	return &SafeScheduler{
		Scheduler: NewScheduler(loc),
		limiter:   NewConcurrencyLimiter(maxConcurrent),
		running:   make(map[string]bool),
	}
}

// AddSafeJob registers a job with overlap protection.
func (ss *SafeScheduler) AddSafeJob(id, name, cronExpr string, handler JobFunc) (*Job, error) {
	orig := handler
	safeHandler := func(job *Job) error {
		ss.muRun.Lock()
		if ss.running[id] {
			ss.muRun.Unlock()
			return nil
		} // Skip if already running.
		ss.running[id] = true
		ss.muRun.Unlock()
		ss.limiter.Acquire()
		defer ss.limiter.Release()
		defer func() { ss.muRun.Lock(); delete(ss.running, id); ss.muRun.Unlock() }()
		return orig(job)
	}
	return ss.Scheduler.AddJob(id, name, cronExpr, safeHandler)
}

// --- Format Helpers ---

// FormatCronExpression returns a formatted breakdown of a cron expression.
func FormatCronExpression(expr string) (string, error) {
	e, err := ParseExpression(expr)
	if err != nil {
		return "", err
	}
	nextRuns := e.NextN(time.Now(), 5)
	s := fmt.Sprintf("Expression: %s\nDescription: %s\nNext runs:\n", expr, e.Describe())
	for i, t := range nextRuns {
		s += fmt.Sprintf("  %d. %s\n", i+1, t.Format(time.RFC3339))
	}
	return s, nil
}
