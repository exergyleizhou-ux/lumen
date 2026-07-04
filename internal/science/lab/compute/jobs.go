package compute

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// Job represents a detached compute job submitted to a remote host.
type Job struct {
	ID        string    `json:"id"`
	Host      string    `json:"host"`
	Command   string    `json:"command"`
	WorkDir   string    `json:"work_dir"`
	Status    string    `json:"status"` // pending, running, done, failed
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	PID       int       `json:"pid,omitempty"`
	ExitCode  int       `json:"exit_code,omitempty"`
	Output    string    `json:"output,omitempty"`
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
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	j := &Job{
		ID:        newJobID(),
		Host:      host,
		Command:   command,
		WorkDir:   workDir,
		Status:    "pending",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.save(j); err != nil {
		return nil, err
	}

	// Launch detached
	go func() {
		s.updateStatus(j.ID, "running")

		sshArgs := []string{host}
		if workDir != "" {
			sshArgs = append(sshArgs, "cd", workDir, "&&")
		}
		sshArgs = append(sshArgs, command)

		cmd := exec.Command("ssh", sshArgs...)
		cmd.Stdin = nil
		out, err := cmd.CombinedOutput()

		j.Status = "done"
		j.ExitCode = 0
		if err != nil {
			j.Status = "failed"
			if ee, ok := err.(*exec.ExitError); ok {
				j.ExitCode = ee.ExitCode()
			} else {
				j.ExitCode = 1
			}
		}
		j.Output = string(out)
		j.UpdatedAt = time.Now().UTC()
		s.save(j)
	}()

	return j, nil
}

// Get loads a job by ID.
func (s *Store) Get(id string) (*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load(id)
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
		j, err := s.load(id)
		if err != nil {
			continue
		}
		jobs = append(jobs, j)
	}
	return jobs, nil
}

func (s *Store) load(id string) (*Job, error) {
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

func (s *Store) save(j *Job) error {
	data, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.jobPath(j.ID), append(data, '\n'), 0o600)
}

func (s *Store) updateStatus(id, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, err := s.load(id)
	if err != nil {
		return
	}
	j.Status = status
	j.UpdatedAt = time.Now().UTC()
	s.save(j)
}
