// Package env provides environment variable and configuration file
// management with validation, defaults, secret masking, and type-safe
// accessors for Lumen agent deployments.
package env

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Var represents an environment variable definition.
type Var struct {
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Default     string             `json:"default"`
	Required    bool               `json:"required"`
	Secret      bool               `json:"secret"`
	Validator   func(string) error `json:"-"`
}

// Store manages environment variables.
type Store struct {
	mu   sync.RWMutex
	vars map[string]*Var
	env  map[string]string
}

// NewStore creates an environment store.
func NewStore() *Store {
	s := &Store{vars: map[string]*Var{}, env: map[string]string{}}
	for _, e := range os.Environ() {
		kv := strings.SplitN(e, "=", 2)
		if len(kv) == 2 {
			s.env[strings.ToUpper(kv[0])] = kv[1]
		}
	}
	return s
}

// Define registers a variable definition.
func (s *Store) Define(v Var) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.vars[strings.ToUpper(v.Name)] = &v
}

// Get retrieves an environment variable value.
func (s *Store) Get(name string) string {
	name = strings.ToUpper(name)
	s.mu.RLock()
	defer s.mu.RUnlock()
	if val, ok := s.env[name]; ok {
		return val
	}
	if v, ok := s.vars[name]; ok {
		return v.Default
	}
	return ""
}

// GetInt retrieves an integer environment variable.
func (s *Store) GetInt(name string) int {
	val := s.Get(name)
	if val == "" {
		return 0
	}
	n, _ := strconv.Atoi(val)
	return n
}

// GetBool retrieves a boolean environment variable.
func (s *Store) GetBool(name string) bool {
	val := strings.ToLower(s.Get(name))
	return val == "true" || val == "1" || val == "yes" || val == "on"
}

// GetDuration retrieves a duration environment variable.
func (s *Store) GetDuration(name string) time.Duration {
	val := s.Get(name)
	if val == "" {
		return 0
	}
	d, _ := time.ParseDuration(val)
	return d
}

// GetSlice retrieves a comma-separated list.
func (s *Store) GetSlice(name string) []string {
	val := s.Get(name)
	if val == "" {
		return nil
	}
	parts := strings.Split(val, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// Validate checks all required variables are set.
func (s *Store) Validate() []error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var errs []error
	for name, v := range s.vars {
		val := s.env[name]
		if val == "" {
			val = v.Default
		}
		if v.Required && val == "" {
			errs = append(errs, fmt.Errorf("%s: required but not set", v.Name))
		}
		if v.Validator != nil && val != "" {
			if err := v.Validator(val); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", v.Name, err))
			}
		}
	}
	return errs
}

// List returns all variable definitions.
func (s *Store) List() []Var {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Var
	for _, v := range s.vars {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Dump returns all environment variables (secrets masked).
func (s *Store) Dump() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[string]string{}
	for k, v := range s.env {
		display := v
		if def, ok := s.vars[k]; ok && def.Secret {
			if len(v) > 4 {
				display = v[:4] + strings.Repeat("*", len(v)-4)
			} else {
				display = "****"
			}
		}
		out[k] = display
	}
	return out
}

// FormatDump formats the environment dump.
func (s *Store) FormatDump() string {
	dump := s.Dump()
	var sb strings.Builder
	keys := make([]string, 0, len(dump))
	for k := range dump {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Fprintf(&sb, "Environment (%d vars):\n%s\n\n", len(keys), strings.Repeat("─", 50))
	for _, k := range keys {
		fmt.Fprintf(&sb, "  %-35s = %s\n", k, dump[k])
	}
	return sb.String()
}

// ── Config Files ──────────────────────────────────────────

// FileStore loads configuration from files with env override.
type FileStore struct {
	mu   sync.Mutex
	data map[string]string
}

// NewFileStore creates a file-backed config store.
func NewFileStore() *FileStore {
	return &FileStore{data: map[string]string{}}
}

// Load reads key=value pairs from a file (simplified .env format).
func (fs *FileStore) Load(path string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		kv := strings.SplitN(line, "=", 2)
		if len(kv) != 2 {
			continue
		}
		fs.data[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
	}
	return nil
}

// Get returns a config value, falling back to env.
func (fs *FileStore) Get(name string) string {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if v, ok := fs.data[name]; ok {
		return v
	}
	return os.Getenv(name)
}

// ── Environment Reporter ──────────────────────────────────

// Reporter checks the runtime environment.
type Reporter struct{}

// NewReporter creates an environment reporter.
func NewReporter() *Reporter { return &Reporter{} }

// Info returns basic runtime information.
func (r *Reporter) Info() map[string]string {
	host, _ := os.Hostname()
	return map[string]string{
		"os":         os.Getenv("GOOS"),
		"arch":       os.Getenv("GOARCH"),
		"hostname":   host,
		"pid":        fmt.Sprintf("%d", os.Getpid()),
		"tmpdir":     os.TempDir(),
		"home":       os.Getenv("HOME"),
		"shell":      os.Getenv("SHELL"),
		"user":       os.Getenv("USER"),
		"timezone":   time.Now().Format("MST"),
		"go_version": os.Getenv("GOVERSION"),
	}
}

// CheckCapabilities checks for required binaries.
func (r *Reporter) CheckCapabilities(binaries []string) map[string]bool {
	result := map[string]bool{}
	pathEnv := os.Getenv("PATH")
	for _, bin := range binaries {
		found := false
		for _, dir := range strings.Split(pathEnv, string(os.PathListSeparator)) {
			if _, err := os.Stat(dir + string(os.PathSeparator) + bin); err == nil {
				found = true
				break
			}
		}
		result[bin] = found
	}
	return result
}
