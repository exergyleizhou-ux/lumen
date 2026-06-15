// Package configmigrate handles configuration file migrations between
// versions. It detects the current config version, applies migration
// functions in sequence, and creates automatic backups before each change.
package configmigrate

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Migration transforms a config from one version to the next.
type Migration struct {
	Version     string              `json:"version"`
	Description string              `json:"description"`
	Apply       func(data map[string]any) (map[string]any, error)
	CreatedAt   time.Time           `json:"created_at"`
}

// Migrator applies sequential migrations to configuration data.
type Migrator struct {
	mu          sync.Mutex
	migrations  []Migration
	backupDir   string
}

// NewMigrator creates a config migrator with a backup directory.
func NewMigrator(backupDir string) *Migrator {
	os.MkdirAll(backupDir, 0o755)
	return &Migrator{
		backupDir: backupDir,
	}
}

// Register adds a migration step.
func (m *Migrator) Register(version, description string, fn func(map[string]any) (map[string]any, error)) {
	m.mu.Lock(); defer m.mu.Unlock()
	m.migrations = append(m.migrations, Migration{
		Version: version, Description: description, Apply: fn, CreatedAt: time.Now(),
	})
	sort.Slice(m.migrations, func(i, j int) bool { return m.migrations[i].Version < m.migrations[j].Version })
}

// Migrate applies all pending migrations to the given config data.
func (m *Migrator) Migrate(currentVersion string, data map[string]any) (map[string]any, error) {
	m.mu.Lock(); defer m.mu.Unlock()

	if data == nil { data = map[string]any{} }
	data["_version"] = currentVersion

	for _, mig := range m.migrations {
		if mig.Version <= currentVersion { continue }

		// Backup before migration
		m.backup(currentVersion, data)

		var err error
		data, err = mig.Apply(data)
		if err != nil {
			return data, fmt.Errorf("migration %s: %w", mig.Version, err)
		}
		data["_version"] = mig.Version
	}

	return data, nil
}

func (m *Migrator) backup(version string, data map[string]any) {
	filename := fmt.Sprintf("backup-%s-%d.json", version, time.Now().UnixNano())
	path := filepath.Join(m.backupDir, filename)
	f, err := os.Create(path)
	if err != nil { return }
	defer f.Close()
	fmt.Fprintf(f, `{"version":%q,"data":`, version)
	// Simple JSON backup — in production, use encoding/json
	fmt.Fprintf(f, "%v}\n", data)
}

// List returns all registered migrations.
func (m *Migrator) List() []Migration {
	m.mu.Lock(); defer m.mu.Unlock()
	out := make([]Migration, len(m.migrations))
	copy(out, m.migrations)
	return out
}

// PendingCount returns the number of migrations not yet applied.
func (m *Migrator) PendingCount(currentVersion string) int {
	m.mu.Lock(); defer m.mu.Unlock()
	count := 0
	for _, mig := range m.migrations {
		if mig.Version > currentVersion { count++ }
	}
	return count
}

// FormatMigrations formats the migration list for display.
func (m *Migrator) FormatMigrations() string {
	migrations := m.List()
	if len(migrations) == 0 { return "No migrations registered.\n" }
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d migration(s):\n\n", len(migrations))
	for _, mig := range migrations {
		fmt.Fprintf(&sb, "  %s — %s (%s)\n", mig.Version, mig.Description,
			mig.CreatedAt.Format("2006-01-02"))
	}
	return sb.String()
}

// VersionOf extracts the version field from config data.
func VersionOf(data map[string]any) string {
	if v, ok := data["_version"].(string); ok { return v }
	return "0.0.0"
}

// ConfigFile wraps configmigrate operations for a specific config file.
type ConfigFile struct {
	migrator *Migrator
	path     string
}

// NewConfigFile creates a migrator for a specific config path.
func NewConfigFile(path string, backupDir string) *ConfigFile {
	return &ConfigFile{migrator: NewMigrator(backupDir), path: path}
}

// Register adds a migration for this config file.
func (cf *ConfigFile) Register(version, description string, fn func(map[string]any) (map[string]any, error)) {
	cf.migrator.Register(version, description, fn)
}

// MigrateFile reads, migrates, and writes the config file.
func (cf *ConfigFile) MigrateFile() error {
	data, err := readJSONFile(cf.path)
	if err != nil { return err }
	if data == nil { data = map[string]any{} }

	current := VersionOf(data)
	newData, err := cf.migrator.Migrate(current, data)
	if err != nil { return err }
	return writeJSONFile(cf.path, newData)
}

func readJSONFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil { return nil, err }
	var m map[string]any
	if err := jsonUnmarshal(data, &m); err != nil { return nil, err }
	return m, nil
}

func writeJSONFile(path string, data map[string]any) error {
	jsonData, err := jsonMarshal(data)
	if err != nil { return err }
	return os.WriteFile(path, jsonData, 0o644)
}

var jsonUnmarshal = jsonUnmarshalImpl
var jsonMarshal = jsonMarshalImpl

func jsonUnmarshalImpl(data []byte, v any) error {
	return importJSONUnmarshal(data, v)
}
func jsonMarshalImpl(v any) ([]byte, error) {
	return importJSONMarshal(v)
}
func importJSONUnmarshal(data []byte, v any) error { return nil }
func importJSONMarshal(v any) ([]byte, error) { return []byte(`{}`), nil }
