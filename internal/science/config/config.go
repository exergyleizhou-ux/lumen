// Package config manages ~/.lumen/science/config.json for the Science bridge.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"lumen/internal/science/guard"
	"lumen/internal/science/proxy"
)

const (
	DefaultProvider    = "deepseek"
	DefaultProxyPort   = 18991
	DefaultSandboxPort = 8990
	DefaultMode        = "proxy"
)

// ProviderCfg holds one provider API key (0600 file, never log).
type ProviderCfg struct {
	Key string `json:"key,omitempty"`
}

// File is the persisted science bridge configuration.
type File struct {
	Provider    string                 `json:"provider"`
	ProxyPort   int                    `json:"proxy_port"`
	SandboxPort int                    `json:"sandbox_port"`
	Secret      string                 `json:"secret,omitempty"`
	Mode        string                 `json:"mode"` // "proxy" | "official"
	CacheBoost  *bool                  `json:"cache_boost,omitempty"` // DeepSeek prefix-cache: inject system/tools cache_control
	Providers   map[string]ProviderCfg `json:"providers,omitempty"`
}

// Default returns factory defaults.
func Default() File {
	return File{
		Provider:    DefaultProvider,
		ProxyPort:   DefaultProxyPort,
		SandboxPort: DefaultSandboxPort,
		Mode:        DefaultMode,
		Providers:   map[string]ProviderCfg{},
	}
}

// Dir returns ~/.lumen/science.
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".lumen", "science"), nil
}

func configPath(dir string) string {
	return filepath.Join(dir, "config.json")
}

var writeMu sync.Mutex

// Load reads config.json; missing file yields defaults.
func Load(dir string) (File, error) {
	if err := assertNotSymlink(dir); err != nil {
		return File{}, err
	}
	path := configPath(dir)
	if err := assertNotSymlink(path); err != nil {
		return File{}, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Default(), nil
	}
	if err != nil {
		return File{}, err
	}
	_ = os.Chmod(path, 0o600)
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return File{}, fmt.Errorf("config.json: %w", err)
	}
	if f.Provider == "" {
		f.Provider = DefaultProvider
	}
	if f.ProxyPort == 0 {
		f.ProxyPort = DefaultProxyPort
	}
	if f.SandboxPort == 0 {
		f.SandboxPort = DefaultSandboxPort
	}
	if f.Mode == "" {
		f.Mode = DefaultMode
	}
	if f.Providers == nil {
		f.Providers = map[string]ProviderCfg{}
	}
	return f, nil
}

// Save atomically writes config.json with 0600 permissions.
func Save(dir string, f File) error {
	writeMu.Lock()
	defer writeMu.Unlock()
	if err := ensureDir(dir); err != nil {
		return err
	}
	path := configPath(dir)
	if err := assertNotSymlink(path); err != nil {
		return err
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, fmt.Sprintf(".config.json.tmp-%d", os.Getpid()))
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Chmod(path, 0o600)
}

// Validate checks ports, provider, and mode before persisting.
func Validate(f File) error {
	if f.Provider != "" {
		if _, ok := proxy.LookupProvider(f.Provider); !ok {
			return fmt.Errorf("unsupported provider %q", f.Provider)
		}
	}
	if err := guard.AssertPortSafe(f.ProxyPort); err != nil {
		return err
	}
	if err := guard.AssertPortSafe(f.SandboxPort); err != nil {
		return err
	}
	if err := guard.AssertPortsDistinct(f.ProxyPort, f.SandboxPort); err != nil {
		return err
	}
	if f.Mode != "" && f.Mode != "proxy" && f.Mode != "official" {
		return fmt.Errorf("mode must be proxy or official")
	}
	return nil
}

// Update loads, applies fn, saves.
func Update(dir string, fn func(*File)) (File, error) {
	writeMu.Lock()
	defer writeMu.Unlock()
	f, err := Load(dir)
	if err != nil {
		return File{}, err
	}
	fn(&f)
	if err := Validate(f); err != nil {
		return File{}, err
	}
	if err := saveUnlocked(dir, f); err != nil {
		return File{}, err
	}
	return f, nil
}

func saveUnlocked(dir string, f File) error {
	if err := ensureDir(dir); err != nil {
		return err
	}
	path := configPath(dir)
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, fmt.Sprintf(".config.json.tmp-%d", os.Getpid()))
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Chmod(path, 0o600)
}

// CacheBoostEnabled returns whether to inject cache_control on system/tools (default true for deepseek).
func (f File) CacheBoostEnabled() bool {
	if f.CacheBoost != nil {
		return *f.CacheBoost
	}
	return strings.EqualFold(f.Provider, "deepseek")
}

// KeyFor returns the stored key for a provider name.
func (f File) KeyFor(provider string) string {
	if p, ok := f.Providers[provider]; ok {
		return p.Key
	}
	return ""
}

// MaskKey returns last-4 masked key for display.
func MaskKey(key string) string {
	n := len(key)
	if n == 0 {
		return ""
	}
	if n <= 4 {
		return strings.Repeat("•", n)
	}
	return strings.Repeat("•", n-4) + key[n-4:]
}

func ensureDir(dir string) error {
	if err := assertNotSymlink(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.Chmod(dir, 0o700)
}

func assertNotSymlink(path string) error {
	fi, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refuse symlink: %s", path)
	}
	return nil
}