// Package seal provides authenticated encryption for agent secrets,
// data-at-rest protection, and secure envelope format with key rotation.
package seal

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

type Key struct {
	ID        string
	Bytes     []byte
	CreatedAt time.Time
	RotatedAt time.Time
}
type Envelope struct {
	KeyID      string
	Ciphertext []byte
	Nonce      []byte
	CreatedAt  time.Time
}
type Manager struct {
	mu      sync.Mutex
	keys    map[string]*Key
	current string
}

func NewManager() *Manager { return &Manager{keys: map[string]*Key{}} }
func (m *Manager) GenerateKey() (*Key, error) {
	bytes := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, bytes); err != nil {
		return nil, err
	}
	h := sha256.Sum256(bytes)
	id := hex.EncodeToString(h[:8])
	k := &Key{ID: id, Bytes: bytes, CreatedAt: time.Now()}
	m.mu.Lock()
	m.keys[id] = k
	m.current = id
	m.mu.Unlock()
	return k, nil
}
func (m *Manager) Current() (*Key, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k, ok := m.keys[m.current]
	return k, ok
}
func (m *Manager) Seal(plaintext []byte) (*Envelope, error) {
	m.mu.Lock()
	k, ok := m.keys[m.current]
	m.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("no current key")
	}
	block, err := aes.NewCipher(k.Bytes)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)
	return &Envelope{KeyID: k.ID, Ciphertext: ciphertext, Nonce: nonce, CreatedAt: time.Now()}, nil
}
func (m *Manager) Unseal(env *Envelope) ([]byte, error) {
	m.mu.Lock()
	k, ok := m.keys[env.KeyID]
	m.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("key %q not found", env.KeyID)
	}
	block, err := aes.NewCipher(k.Bytes)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, env.Nonce, env.Ciphertext, nil)
}
func (m *Manager) Rotate() (*Key, error) {
	m.mu.Lock()
	if k, ok := m.keys[m.current]; ok {
		k.RotatedAt = time.Now()
	}
	m.mu.Unlock()
	newKey, err := m.GenerateKey()
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.current = newKey.ID
	m.mu.Unlock()
	return newKey, nil
}
func (m *Manager) FormatKeys() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var sb strings.Builder
	fmt.Fprintf(&sb, "Seal Keys (%d):\n%s\n\n", len(m.keys), strings.Repeat("─", 40))
	for id, k := range m.keys {
		current := ""
		if id == m.current {
			current = " [CURRENT]"
		}
		fmt.Fprintf(&sb, "  %s%s created=%s\n", id, current, k.CreatedAt.Format(time.RFC3339))
	}
	return sb.String()
}
