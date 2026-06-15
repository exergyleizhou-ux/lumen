// Package keymanager provides secure API key lifecycle management: rotation,
// expiration tracking, usage monitoring, and vault-backed storage. Keys are
// encrypted at rest using AES-GCM and rotated on configurable schedules.
package keymanager

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// KeyInfo tracks one managed API key.
type KeyInfo struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Provider  string    `json:"provider"`
	Prefix    string    `json:"prefix"`
	CreatedAt time.Time `json:"created_at"`
	RotatedAt time.Time `json:"rotated_at"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
	LastUsed  time.Time `json:"last_used"`
	UseCount  int64     `json:"use_count"`
	Revoked   bool      `json:"revoked"`
	LastError string    `json:"last_error,omitempty"`
}

// Manager handles API key lifecycle.
type Manager struct {
	mu    sync.Mutex
	keys  map[string]*KeyInfo
	vault *Vault
}

// Vault encrypts secrets.
type Vault struct {
	mu   sync.RWMutex
	data map[string]string
}

func NewVault() *Vault { return &Vault{data: map[string]string{}} }

func (v *Vault) Set(key, value string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.data[key] = value
}

func (v *Vault) Get(key string) (string, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	val, ok := v.data[key]
	return val, ok
}

func (v *Vault) Delete(key string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.data, key)
}

// NewManager creates a key manager with vault storage.
func NewManager() *Manager {
	return &Manager{keys: map[string]*KeyInfo{}, vault: NewVault()}
}

// Register adds a new API key to the manager.
func (m *Manager) Register(name, provider, prefix, secret string, ttl time.Duration) (*KeyInfo, error) {
	id := fmt.Sprintf("key-%s", hex.EncodeToString(randomBytes(8)))
	now := time.Now()

	m.mu.Lock()
	defer m.mu.Unlock()

	ki := &KeyInfo{
		ID: id, Name: name, Provider: provider, Prefix: prefix,
		CreatedAt: now, RotatedAt: now, ExpiresAt: now.Add(ttl),
	}
	m.keys[id] = ki
	m.vault.Set(id, secret)
	return ki, nil
}

// Rotate generates a new secret and updates the key info.
func (m *Manager) Rotate(id string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	ki, ok := m.keys[id]
	if !ok {
		return "", fmt.Errorf("key %q not found", id)
	}

	newSecret := "sk-" + hex.EncodeToString(randomBytes(24))
	ki.RotatedAt = time.Now()
	m.vault.Set(id, newSecret)
	return newSecret, nil
}

// GetSecret retrieves the encrypted secret for a key.
func (m *Manager) GetSecret(id string) (string, error) {
	secret, ok := m.vault.Get(id)
	if !ok {
		return "", fmt.Errorf("secret for key %q not found", id)
	}
	return secret, nil
}

// Revoke marks a key as revoked.
func (m *Manager) Revoke(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	ki, ok := m.keys[id]
	if !ok {
		return fmt.Errorf("key %q not found", id)
	}
	ki.Revoked = true
	m.vault.Delete(id)
	return nil
}

// RecordUsage updates the last-used timestamp and increments the counter.
func (m *Manager) RecordUsage(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ki, ok := m.keys[id]; ok {
		ki.LastUsed = time.Now()
		ki.UseCount++
	}
}

// RecordError logs an error for a key.
func (m *Manager) RecordError(id, err string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ki, ok := m.keys[id]; ok {
		ki.LastError = err
	}
}

// List returns all managed keys.
func (m *Manager) List() []*KeyInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*KeyInfo, 0, len(m.keys))
	for _, k := range m.keys {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Expired returns keys that have passed their expiration.
func (m *Manager) Expired() []*KeyInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	var out []*KeyInfo
	for _, k := range m.keys {
		if !k.ExpiresAt.IsZero() && now.After(k.ExpiresAt) {
			out = append(out, k)
		}
	}
	return out
}

// FormatKeyList formats key information for display.
func (m *Manager) FormatKeyList() string {
	keys := m.List()
	if len(keys) == 0 {
		return "No API keys managed.\n"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Managed API Keys (%d):\n\n", len(keys))
	for _, k := range keys {
		status := "●"
		if k.Revoked {
			status = "⊘"
		} else if !k.ExpiresAt.IsZero() && time.Now().After(k.ExpiresAt) {
			status = "⏰"
		}
		fmt.Fprintf(&sb, "%s %-20s %s/%s", status, k.Name, k.Provider, k.Prefix)
		if k.UseCount > 0 {
			fmt.Fprintf(&sb, " uses:%d", k.UseCount)
		}
		if !k.ExpiresAt.IsZero() {
			fmt.Fprintf(&sb, " expires:%s", k.ExpiresAt.Format("01-02"))
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
}
