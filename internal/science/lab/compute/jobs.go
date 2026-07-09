package compute

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// MaxOutputBytes caps captured stdout/stderr stored on disk (anti-OOM).
	MaxOutputBytes = 1 << 20 // 1 MiB
	// DefaultJobTimeout bounds a single SSH job.
	DefaultJobTimeout = 30 * time.Minute
	// MaxJobTimeout is the hard ceiling even if caller asks for more.
	MaxJobTimeout = 2 * time.Hour
)

// Job represents a detached compute job submitted to a remote host.
type Job struct {
	ID              string    `json:"id"`
	Host            string    `json:"host"`
	Command         string    `json:"command"`
	WorkDir         string    `json:"work_dir"`
	Status          string    `json:"status"` // pending, running, done, failed, timeout
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	PID             int       `json:"pid,omitempty"`
	ExitCode        int       `json:"exit_code,omitempty"`
	Output          string    `json:"output,omitempty"`
	OutputTruncated bool      `json:"output_truncated,omitempty"`
	TimeoutSec      int       `json:"timeout_sec,omitempty"`
	Error           string    `json:"error,omitempty"`
}

// SubmitOpts optional job parameters.
type SubmitOpts struct {
	Timeout time.Duration
}

// Store manages job state on disk.
type Store struct {
	mu   sync.Mutex
	root string
}

// NewStore creates a job store under the project .lumen/compute directory.
func NewStore(projectDir string) (*Store, error) {
	root := filepath.Join(projectDir, ".lumen", "compute")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	return &Store{root: root}, nil
}

func (s *Store) jobPath(id string) string {
	return filepath.Join(s.root, id+".json")
}

func newJobID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "job_" + hex.EncodeToString(b)
}

// Submit creates and starts a detached compute job.
func (s *Store) Submit(host, command, workDir string) (*Job, error) {
	return s.SubmitOpts(host, command, workDir, SubmitOpts{})
}

// SubmitOpts starts a job with optional timeout.
func (s *Store) SubmitOpts(host, command, workDir string, opts SubmitOpts) (*Job, error) {
	if host == "" || command == "" {
		return nil, fmt.Errorf("host and command required")
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultJobTimeout
	}
	if timeout > MaxJobTimeout {
		timeout = MaxJobTimeout
	}

	s.mu.Lock()
	now := time.Now().UTC()
	j := &Job{
		ID:         newJobID(),
		Host:       host,
		Command:    command,
		WorkDir:    workDir,
		Status:     "pending",
		CreatedAt:  now,
		UpdatedAt:  now,
		TimeoutSec: int(timeout.Seconds()),
	}
	if err := s.saveLocked(j); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	id := j.ID
	s.mu.Unlock()

	go s.run(id, host, command, workDir, timeout)
	return j, nil
}

func (s *Store) run(id, host, command, workDir string, timeout time.Duration) {
	s.patch(id, func(j *Job) {
		j.Status = "running"
	})

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	remote := command
	if workDir != "" {
		remote = "cd " + shellQuote(workDir) + " && " + command
	}
	cmd := exec.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=15", host, remote)
	cmd.Stdin = nil
	out, err := cmd.CombinedOutput()
	outStr, truncated := truncateOutput(string(out), MaxOutputBytes)

	s.patch(id, func(j *Job) {
		j.Output = outStr
		j.OutputTruncated = truncated
		j.UpdatedAt = time.Now().UTC()
		if ctx.Err() == context.DeadlineExceeded {
			j.Status = "timeout"
			j.ExitCode = -1
			j.Error = fmt.Sprintf("exceeded timeout %s", timeout)
			return
		}
		if err != nil {
			j.Status = "failed"
			j.Error = err.Error()
			if ee, ok := err.(*exec.ExitError); ok {
				j.ExitCode = ee.ExitCode()
			} else {
				j.ExitCode = 1
			}
			return
		}
		j.Status = "done"
		j.ExitCode = 0
	})
}

func truncateOutput(s string, max int) (string, bool) {
	if max <= 0 || len(s) <= max {
		return s, false
	}
	// Keep head + note
	keep := max - 80
	if keep < 0 {
		keep = max
	}
	return s[:keep] + "\n…[truncated]…\n", true
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

// Get loads a job by ID.
func (s *Store) Get(id string) (*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked(id)
}

// List returns all jobs for this project.
func (s *Store) List() ([]*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, err
	}
	var jobs []*Job
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		id := e.Name()[:len(e.Name())-5]
		j, err := s.loadLocked(id)
		if err != nil {
			continue
		}
		jobs = append(jobs, j)
	}
	return jobs, nil
}

func (s *Store) loadLocked(id string) (*Job, error) {
	data, err := os.ReadFile(s.jobPath(id))
	if err != nil {
		return nil, err
	}
	var j Job
	if err := json.Unmarshal(data, &j); err != nil {
		return nil, err
	}
	return &j, nil
}

func (s *Store) saveLocked(j *Job) error {
	data, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.jobPath(j.ID), append(data, '\n'), 0o600)
}

func (s *Store) patch(id string, fn func(*Job)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, err := s.loadLocked(id)
	if err != nil {
		return
	}
	fn(j)
	j.UpdatedAt = time.Now().UTC()
	_ = s.saveLocked(j)
}
