package project

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// Store manages lab projects on disk.
type Store struct {
	sciDir string
	root   string
}

// NewStore creates a project store at ~/.lumen/science/lab/projects.
func NewStore(sciDir string) *Store {
	return &Store{sciDir: sciDir, root: filepath.Join(sciDir, "lab", "projects")}
}

// SciDir returns the science config root.
func (s *Store) SciDir() string { return s.sciDir }

// Root returns the projects root directory.
func (s *Store) Root() string { return s.root }

func (s *Store) projectDir(slug string) string {
	return filepath.Join(s.root, slug)
}

// SlugFromTitle converts a title to a URL-safe slug.
func SlugFromTitle(title string) string {
	t := strings.ToLower(strings.TrimSpace(title))
	var b strings.Builder
	prevDash := false
	for _, r := range t {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		case r == ' ' || r == '_' || r == '-':
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		default:
			// skip other runes
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "project"
	}
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}

func newID(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return prefix + "_" + hex.EncodeToString(b)
}

// List returns all projects sorted by updated time descending.
func (s *Store) List() ([]Project, error) {
	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, err
	}
	var out []Project
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p, err := s.load(filepath.Join(s.root, e.Name()))
		if err != nil {
			continue
		}
		out = append(out, p)
	}
	// simple sort by UpdatedAt
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].UpdatedAt.Before(out[j-1].UpdatedAt); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out, nil
}

func (s *Store) load(dir string) (Project, error) {
	data, err := os.ReadFile(filepath.Join(dir, "project.json"))
	if err != nil {
		return Project{}, err
	}
	var p Project
	if err := json.Unmarshal(data, &p); err != nil {
		return Project{}, err
	}
	return p, nil
}

// Get loads a project by slug.
func (s *Store) Get(slug string) (Project, error) {
	if !slugRe.MatchString(slug) {
		return Project{}, fmt.Errorf("invalid slug %q", slug)
	}
	return s.load(s.projectDir(slug))
}

// Create adds a new project with workspace scaffolding.
func (s *Store) Create(title, template string) (Project, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return Project{}, fmt.Errorf("title is required")
	}
	slug := SlugFromTitle(title)
	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return Project{}, err
	}
	dir := s.projectDir(slug)
	if _, err := os.Stat(dir); err == nil {
		slug = slug + "-2"
		dir = s.projectDir(slug)
	}
	now := time.Now().UTC()
	p := Project{
		ID:           newID("proj"),
		Slug:         slug,
		Title:        title,
		Template:     template,
		CreatedAt:    now,
		UpdatedAt:    now,
		WorkspaceRel: "workspace",
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Project{}, err
	}
	for _, sub := range []string{"workspace", "workspace/data", "workspace/figures", "workspace/reports", "workspace/notebooks", "sessions", ".lumen/skills"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o700); err != nil {
			return Project{}, err
		}
	}
	provPath := filepath.Join(dir, "provenance.jsonl")
	if _, err := os.Stat(provPath); os.IsNotExist(err) {
		if err := os.WriteFile(provPath, nil, 0o600); err != nil {
			return Project{}, err
		}
	}
	if err := s.save(p); err != nil {
		return Project{}, err
	}
	ws := filepath.Join(dir, "workspace")
	if err := ApplySeedTemplate(s.sciDir, template, ws); err != nil {
		return Project{}, err
	}
	return p, nil
}

// ProjectDir returns the absolute project directory for a slug.
func (s *Store) ProjectDir(slug string) (string, error) {
	if !slugRe.MatchString(slug) {
		return "", fmt.Errorf("invalid slug %q", slug)
	}
	return s.projectDir(slug), nil
}

func (s *Store) save(p Project) error {
	dir := s.projectDir(p.Slug)
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "project.json"), append(data, '\n'), 0o600)
}

// WorkspacePath returns the absolute workspace directory for a project slug.
func (s *Store) WorkspacePath(slug string) (string, error) {
	if !slugRe.MatchString(slug) {
		return "", fmt.Errorf("invalid slug %q", slug)
	}
	return filepath.Join(s.projectDir(slug), "workspace"), nil
}

// ProvenancePath returns provenance.jsonl for a project.
func (s *Store) ProvenancePath(slug string) (string, error) {
	if !slugRe.MatchString(slug) {
		return "", fmt.Errorf("invalid slug %q", slug)
	}
	return filepath.Join(s.projectDir(slug), "provenance.jsonl"), nil
}

// CreateSession adds a session file under the project.
func (s *Store) CreateSession(slug, title string) (Session, error) {
	p, err := s.Get(slug)
	if err != nil {
		return Session{}, err
	}
	now := time.Now().UTC()
	if title == "" {
		title = "会话 " + now.Format("15:04")
	}
	sess := Session{
		ID:        newID("sess"),
		ProjectID: p.ID,
		Title:     title,
		CreatedAt: now,
		UpdatedAt: now,
	}
	dir := filepath.Join(s.projectDir(slug), "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Session{}, err
	}
	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return Session{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, sess.ID+".json"), append(data, '\n'), 0o600); err != nil {
		return Session{}, err
	}
	p.ActiveSession = sess.ID
	p.UpdatedAt = now
	_ = s.save(p)
	return sess, nil
}

// ListSessions returns sessions for a project.
func (s *Store) ListSessions(slug string) ([]Session, error) {
	dir := filepath.Join(s.projectDir(slug), "sessions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Session
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var sess Session
		if json.Unmarshal(data, &sess) == nil {
			out = append(out, sess)
		}
	}
	return out, nil
}
