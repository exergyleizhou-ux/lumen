// Package scheduler provides cron-like job scheduling with second precision,
// job history, overlapping prevention, and graceful shutdown.
package scheduler

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ---------------------------------------------------------------------------
// Job — a unit of scheduled work.

// JobFunc is the signature of a scheduled function.
type JobFunc func(ctx context.Context) error

// JobState tracks the lifecycle of a single job run.
type JobState int

const (
	JobPending JobState = iota
	JobRunning
	JobSuccess
	JobFailed
	JobSkipped // overlapping prevention skipped this run
)

func (s JobState) String() string {
	switch s {
	case JobPending:
		return "pending"
	case JobRunning:
		return "running"
	case JobSuccess:
		return "success"
	case JobFailed:
		return "failed"
	case JobSkipped:
		return "skipped"
	}
	return "unknown"
}

// Job represents a scheduled function with metadata.
type Job struct {
	ID           string
	Name         string
	Spec         string // cron spec string
	fn           JobFunc
	schedule     *cronSchedule // parsed
	nextRun      time.Time
	mu           sync.Mutex
	runCount     int64
	lastRun      time.Time
	lastDur      time.Duration
	lastErr      error
	allowOverlap bool
	timeout      time.Duration
	enabled      bool
	tags         []string
}

// NewJob creates a Job.  spec is a 5-field cron expression.
func NewJob(id, name, spec string, fn JobFunc) (*Job, error) {
	cs, err := parseCronSpec(spec)
	if err != nil {
		return nil, fmt.Errorf("scheduler: job %q: %w", id, err)
	}
	return &Job{
		ID:       id,
		Name:     name,
		Spec:     spec,
		fn:       fn,
		schedule: cs,
		enabled:  true,
	}, nil
}

// SetAllowOverlap permits concurrent runs of the same job.
func (j *Job) SetAllowOverlap(v bool) *Job { j.allowOverlap = v; return j }

// SetTimeout limits each run; 0 means no timeout.
func (j *Job) SetTimeout(d time.Duration) *Job { j.timeout = d; return j }

// SetTags attaches metadata tags.
func (j *Job) SetTags(tags ...string) *Job { j.tags = tags; return j }

// Enable / Disable control whether the job fires.
func (j *Job) Enable()  { j.enabled = true }
func (j *Job) Disable() { j.enabled = false }

// Stats returns run count, last duration, and last error.
func (j *Job) Stats() (count int64, lastDur time.Duration, lastErr error) {
	return atomic.LoadInt64(&j.runCount), j.lastDur, j.lastErr
}

// NextRun returns the next scheduled time (approximate).
func (j *Job) NextRun() time.Time {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.nextRun
}

func (j *Job) computeNext(now time.Time) time.Time {
	return j.schedule.Next(now)
}

// ---------------------------------------------------------------------------
// JobHistory — a ring-buffer record of completed runs.

// JobRecord is one entry in the history.
type JobRecord struct {
	JobID    string
	Start    time.Time
	Duration time.Duration
	State    JobState
	Error    string // empty if success
}

// JobHistory keeps the last N records per job, or globally.
type JobHistory struct {
	mu       sync.RWMutex
	records  []JobRecord
	capacity int
}

// NewJobHistory creates a history with a given capacity.
func NewJobHistory(capacity int) *JobHistory {
	if capacity < 1 {
		capacity = 100
	}
	return &JobHistory{capacity: capacity}
}

// Record adds a record, evicting oldest if at capacity.
func (jh *JobHistory) Record(r JobRecord) {
	jh.mu.Lock()
	defer jh.mu.Unlock()
	jh.records = append(jh.records, r)
	if len(jh.records) > jh.capacity {
		jh.records = jh.records[len(jh.records)-jh.capacity:]
	}
}

// List returns a copy of all records.
func (jh *JobHistory) List() []JobRecord {
	jh.mu.RLock()
	defer jh.mu.RUnlock()
	out := make([]JobRecord, len(jh.records))
	copy(out, jh.records)
	return out
}

// ByJob returns records for a specific job ID.
func (jh *JobHistory) ByJob(jobID string) []JobRecord {
	jh.mu.RLock()
	defer jh.mu.RUnlock()
	var out []JobRecord
	for _, r := range jh.records {
		if r.JobID == jobID {
			out = append(out, r)
		}
	}
	return out
}

// Stats computes summary stats from history.
func (jh *JobHistory) Stats() map[string]interface{} {
	jh.mu.RLock()
	defer jh.mu.RUnlock()
	total := len(jh.records)
	var failures int
	var totalDur time.Duration
	for _, r := range jh.records {
		if r.State == JobFailed {
			failures++
		}
		totalDur += r.Duration
	}
	avg := time.Duration(0)
	if total > 0 {
		avg = totalDur / time.Duration(total)
	}
	return map[string]interface{}{
		"total":    total,
		"failures": failures,
		"avg_dur":  avg.String(),
	}
}

// ---------------------------------------------------------------------------
// Scheduler — the core engine.

// Scheduler manages a set of jobs, ticking every second and dispatching those
// whose cron spec matches the current time.
type Scheduler struct {
	jobs    map[string]*Job
	history *JobHistory
	mu      sync.RWMutex
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	running atomic.Bool
	ticker  *time.Ticker
	loc     *time.Location
}

// NewScheduler creates a Scheduler.
func NewScheduler() *Scheduler {
	return &Scheduler{
		jobs:    make(map[string]*Job),
		history: NewJobHistory(1000),
		loc:     time.Local,
	}
}

// SetLocation changes the timezone used for cron evaluation.
func (s *Scheduler) SetLocation(loc *time.Location) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loc = loc
}

// AddJob registers a job.  Returns error if ID already exists.
func (s *Scheduler) AddJob(job *Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.jobs[job.ID]; ok {
		return fmt.Errorf("scheduler: duplicate job id %q", job.ID)
	}
	s.jobs[job.ID] = job
	return nil
}

// RemoveJob removes a job by ID.
func (s *Scheduler) RemoveJob(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.jobs, id)
}

// Job returns a job by ID, or nil.
func (s *Scheduler) Job(id string) *Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.jobs[id]
}

// Jobs returns a snapshot of all job IDs.
func (s *Scheduler) Jobs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]string, 0, len(s.jobs))
	for id := range s.jobs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// History returns the shared history store.
func (s *Scheduler) History() *JobHistory { return s.history }

// Start begins the scheduler loop.  It ticks every second.
func (s *Scheduler) Start() {
	if s.running.Swap(true) {
		return // already running
	}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.ticker = time.NewTicker(1 * time.Second)
	s.wg.Add(1)
	go s.loop()
}

// Stop gracefully shuts down the scheduler.  Running jobs are given deadline
// to finish.
func (s *Scheduler) Stop() {
	if !s.running.Swap(false) {
		return // not running
	}
	s.cancel()
	s.ticker.Stop()
	s.wg.Wait()
}

func (s *Scheduler) loop() {
	defer s.wg.Done()
	for {
		select {
		case <-s.ctx.Done():
			return
		case now := <-s.ticker.C:
			s.tick(now)
		}
	}
}

func (s *Scheduler) tick(now time.Time) {
	s.mu.RLock()
	// Snapshot jobs to avoid holding lock during execution.
	jobs := make([]*Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		jobs = append(jobs, j)
	}
	s.mu.RUnlock()

	nowLocal := now.In(s.loc)
	for _, j := range jobs {
		j.mu.Lock()
		if !j.enabled {
			j.mu.Unlock()
			continue
		}
		next := j.nextRun
		if next.IsZero() {
			next = j.computeNext(nowLocal)
			j.nextRun = next
		}
		if nowLocal.Before(next) {
			j.mu.Unlock()
			continue
		}
		// Advance to next tick.
		j.nextRun = j.computeNext(nowLocal)
		j.mu.Unlock()

		// Dispatch.
		s.wg.Add(1)
		go s.runJob(j, now)
	}
}

func (s *Scheduler) runJob(job *Job, start time.Time) {
	defer s.wg.Done()

	job.mu.Lock()
	if !job.allowOverlap && job.lastDur != 0 && time.Since(job.lastRun) < job.lastDur {
		// Previous run still in-flight; skip.
		job.mu.Unlock()
		s.history.Record(JobRecord{
			JobID:    job.ID,
			Start:    start,
			Duration: 0,
			State:    JobSkipped,
		})
		return
	}
	job.lastRun = start
	job.mu.Unlock()

	ctx := context.Background()
	if job.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, job.timeout)
		defer cancel()
	}

	runStart := time.Now()
	err := job.fn(ctx)
	dur := time.Since(runStart)

	job.mu.Lock()
	job.lastDur = dur
	job.lastErr = err
	atomic.AddInt64(&job.runCount, 1)
	job.mu.Unlock()

	state := JobSuccess
	errStr := ""
	if err != nil {
		state = JobFailed
		errStr = err.Error()
	}
	s.history.Record(JobRecord{
		JobID:    job.ID,
		Start:    runStart,
		Duration: dur,
		State:    state,
		Error:    errStr,
	})
}

// ---------------------------------------------------------------------------
// cronSchedule — parsed 5-field cron expression.

type cronField struct {
	values   []int // explicit values
	star     bool  // wildcard
	step     int   // step interval (0 = none)
	rangeMin int
	rangeMax int
}

type cronSchedule struct {
	minute     cronField
	hour       cronField
	dayOfMonth cronField
	month      cronField
	dayOfWeek  cronField
}

// parseCronSpec parses a 5-field cron expression (min hour dom month dow).
// Supports: *, */N, N, N-M, N,M,O.
func parseCronSpec(spec string) (*cronSchedule, error) {
	fields := strings.Fields(spec)
	if len(fields) != 5 {
		return nil, fmt.Errorf("cron spec must have 5 fields, got %d", len(fields))
	}
	cs := &cronSchedule{}
	ranges := []struct {
		min, max int
		target   *cronField
	}{
		{0, 59, &cs.minute},
		{0, 23, &cs.hour},
		{1, 31, &cs.dayOfMonth},
		{1, 12, &cs.month},
		{0, 6, &cs.dayOfWeek}, // 0=Sunday
	}
	for i, raw := range fields {
		if err := parseField(raw, ranges[i].min, ranges[i].max, ranges[i].target); err != nil {
			return nil, fmt.Errorf("field %d (%q): %w", i, raw, err)
		}
	}
	return cs, nil
}

func parseField(raw string, min, max int, cf *cronField) error {
	cf.rangeMin = min
	cf.rangeMax = max
	if raw == "*" {
		cf.star = true
		return nil
	}
	// Step: */N
	if strings.HasPrefix(raw, "*/") {
		step, err := strconv.Atoi(raw[2:])
		if err != nil || step < 1 {
			return fmt.Errorf("invalid step %q", raw)
		}
		cf.star = true
		cf.step = step
		return nil
	}
	// Comma-separated values and ranges.
	parts := strings.Split(raw, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "*" {
			cf.star = true
			continue
		}
		// Range: N-M
		if strings.Contains(part, "-") {
			ends := strings.SplitN(part, "-", 2)
			lo, err := strconv.Atoi(strings.TrimSpace(ends[0]))
			if err != nil {
				return fmt.Errorf("invalid range start %q", ends[0])
			}
			hi, err := strconv.Atoi(strings.TrimSpace(ends[1]))
			if err != nil {
				return fmt.Errorf("invalid range end %q", ends[1])
			}
			if lo < min || hi > max || lo > hi {
				return fmt.Errorf("range %d-%d out of bounds [%d,%d]", lo, hi, min, max)
			}
			for v := lo; v <= hi; v++ {
				cf.values = append(cf.values, v)
			}
			continue
		}
		// Single value.
		v, err := strconv.Atoi(part)
		if err != nil {
			return fmt.Errorf("invalid value %q", part)
		}
		if v < min || v > max {
			return fmt.Errorf("value %d out of bounds [%d,%d]", v, min, max)
		}
		cf.values = append(cf.values, v)
	}
	sort.Ints(cf.values)
	return nil
}

// matches returns whether the given value matches the cron field.
func (cf *cronField) matches(v int) bool {
	if cf.star {
		if cf.step > 0 {
			return (v-cf.rangeMin)%cf.step == 0
		}
		return true
	}
	if len(cf.values) == 0 {
		return false
	}
	// Binary search.
	i := sort.SearchInts(cf.values, v)
	return i < len(cf.values) && cf.values[i] == v
}

// Next finds the next time after `after` that satisfies the schedule.
// Uses a simple minute-stepping algorithm.
func (cs *cronSchedule) Next(after time.Time) time.Time {
	// Start from the next second, round up to the next minute.
	t := after.Add(1 * time.Second).Truncate(time.Minute)
	// Search up to 4 years ahead (safety limit).
	limit := after.AddDate(4, 0, 0)
	for t.Before(limit) {
		if cs.minute.matches(t.Minute()) &&
			cs.hour.matches(t.Hour()) &&
			cs.dayOfMonth.matches(t.Day()) &&
			cs.month.matches(int(t.Month())) &&
			cs.dayOfWeek.matches(int(t.Weekday())) {
			return t
		}
		t = t.Add(1 * time.Minute)
	}
	return after.AddDate(4, 0, 0) // fallback
}

// ---------------------------------------------------------------------------
// ParseCronSpec is the public entry point for validating a cron expression.

// ParseCronSpec validates and parses a 5-field cron spec.
func ParseCronSpec(spec string) error {
	_, err := parseCronSpec(spec)
	return err
}

// FormatSchedule describes a cron spec in human-readable English.
func FormatSchedule(spec string) (string, error) {
	cs, err := parseCronSpec(spec)
	if err != nil {
		return "", err
	}
	return cs.Describe(), nil
}

// Describe returns a human-readable approximation of the schedule.
func (cs *cronSchedule) Describe() string {
	parts := []string{
		describeField("minute", cs.minute),
		describeField("hour", cs.hour),
		describeField("day of month", cs.dayOfMonth),
		describeField("month", cs.month),
		describeField("day of week", cs.dayOfWeek),
	}
	// Compact common patterns.
	if cs.minute.star && cs.minute.step == 0 &&
		cs.hour.star && cs.hour.step == 0 &&
		cs.dayOfMonth.star && cs.dayOfMonth.step == 0 &&
		cs.month.star && cs.month.step == 0 &&
		cs.dayOfWeek.star && cs.dayOfWeek.step == 0 {
		return "every minute"
	}
	return strings.Join(parts, ", ")
}

func describeField(name string, cf cronField) string {
	if cf.star && cf.step == 0 {
		return "every " + name
	}
	if cf.star && cf.step > 0 {
		return fmt.Sprintf("every %d %ss", cf.step, name)
	}
	if len(cf.values) == 1 {
		return fmt.Sprintf("%s %d", name, cf.values[0])
	}
	strs := make([]string, len(cf.values))
	for i, v := range cf.values {
		strs[i] = strconv.Itoa(v)
	}
	return name + " " + strings.Join(strs, ",")
}

// ---------------------------------------------------------------------------
// Convenience: run once at a specific time.

// At returns a cron spec string that fires once at the given time.
func At(t time.Time) string {
	return fmt.Sprintf("%d %d %d %d *", t.Minute(), t.Hour(), t.Day(), int(t.Month()))
}

// Every returns a cron spec for an interval (as a duration).
// This is a simplified helper; complex intervals use raw cron.
func Every(d time.Duration) string {
	switch {
	case d >= 24*time.Hour:
		days := int(d.Hours() / 24)
		return fmt.Sprintf("0 0 */%d * *", days)
	case d >= time.Hour:
		hours := int(d.Hours())
		return fmt.Sprintf("0 */%d * * *", hours)
	case d >= time.Minute:
		mins := int(d.Minutes())
		return fmt.Sprintf("*/%d * * * *", mins)
	default:
		return "* * * * *" // every minute fallback
	}
}

// ---------------------------------------------------------------------------
// Validate a cron spec, returning individual field errors.

// ValidateSpec checks each field and returns a map of field->error.
func ValidateSpec(spec string) map[string]error {
	fields := strings.Fields(spec)
	if len(fields) != 5 {
		return map[string]error{"spec": fmt.Errorf("need 5 fields, got %d", len(fields))}
	}
	names := []string{"minute", "hour", "day_of_month", "month", "day_of_week"}
	ranges := []struct{ min, max int }{
		{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 6},
	}
	errs := map[string]error{}
	for i, raw := range fields {
		var cf cronField
		if err := parseField(raw, ranges[i].min, ranges[i].max, &cf); err != nil {
			errs[names[i]] = err
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return errs
}
