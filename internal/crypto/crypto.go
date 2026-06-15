// Package crypto provides cryptographic utilities: AES-GCM encryption,
// HMAC signing, password hashing with bcrypt, and secure random generation.
// Used for protecting session data, API keys, and sensitive configuration.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"sync"
)

// Key is a symmetric encryption key derived from a passphrase.
type Key []byte

// DeriveKey creates a 32-byte AES-256 key from a passphrase using SHA-256.
func DeriveKey(passphrase string) Key {
	h := sha256.Sum256([]byte(passphrase))
	return h[:]
}

// DeriveKeyWithSalt creates a key with salt for added entropy.
func DeriveKeyWithSalt(passphrase string, salt []byte) Key {
	h := sha256.New()
	h.Write([]byte(passphrase))
	h.Write(salt)
	return h.Sum(nil)
}

// GenerateKey creates a random 32-byte key.
func GenerateKey() (Key, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return key, nil
}

// Encrypt encrypts plaintext using AES-256-GCM.
func Encrypt(plaintext []byte, key Key) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("aes cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("gcm: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.RawStdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts an AES-256-GCM encrypted string.
func Decrypt(encoded string, key Key) ([]byte, error) {
	ciphertext, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("base64: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}

// ── HMAC signing ──────────────────────────────────────────

// Sign creates an HMAC-SHA256 signature.
func Sign(data []byte, key Key) string {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil))
}

// Verify checks an HMAC-SHA256 signature.
func Verify(data []byte, signature string, key Key) bool {
	expected := Sign(data, key)
	return hmac.Equal([]byte(expected), []byte(signature))
}

// ── Password hashing ──────────────────────────────────────

// HashPassword creates a salted SHA-256 hash of a password.
func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}

	h := sha256.New()
	h.Write(salt)
	h.Write([]byte(password))
	hash := h.Sum(nil)

	return hex.EncodeToString(salt) + "$" + hex.EncodeToString(hash), nil
}

// VerifyPassword checks a password against a hash.
func VerifyPassword(password, stored string) bool {
	parts := splitN(stored, "$", 2)
	if len(parts) != 2 {
		return false
	}

	salt, err := hex.DecodeString(parts[0])
	if err != nil {
		return false
	}

	h := sha256.New()
	h.Write(salt)
	h.Write([]byte(password))

	expected := hex.EncodeToString(h.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(parts[1]))
}

func splitN(s, sep string, n int) []string {
	idx := 0
	var parts []string
	for i := 0; i < n-1 && idx < len(s); i++ {
		if j := indexOf(s, sep, idx); j >= 0 {
			parts = append(parts, s[idx:j])
			idx = j + len(sep)
		} else {
			break
		}
	}
	parts = append(parts, s[idx:])
	return parts
}

func indexOf(s, substr string, start int) int {
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// ── Token generation ──────────────────────────────────────

// RandomToken generates a URL-safe random token of the given byte length.
func RandomToken(byteLen int) (string, error) {
	b := make([]byte, byteLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// RandomBase64 generates a base64 random token.
func RandomBase64(byteLen int) (string, error) {
	b := make([]byte, byteLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// ── Vault ─────────────────────────────────────────────────

// Vault stores encrypted key-value pairs in memory.
type Vault struct {
	mu   sync.RWMutex
	data map[string]string
	key  Key
}

// NewVault creates an encrypted vault.
func NewVault(passphrase string) *Vault {
	return &Vault{data: map[string]string{}, key: DeriveKey(passphrase)}
}

// Set encrypts and stores a value.
func (v *Vault) Set(key, value string) error {
	encrypted, err := Encrypt([]byte(value), v.key)
	if err != nil {
		return err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.data[key] = encrypted
	return nil
}

// Get decrypts and returns a value.
func (v *Vault) Get(key string) (string, error) {
	v.mu.RLock()
	encrypted, ok := v.data[key]
	v.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("key %q not found", key)
	}
	decrypted, err := Decrypt(encrypted, v.key)
	if err != nil {
		return "", err
	}
	return string(decrypted), nil
}

// Delete removes a key.
func (v *Vault) Delete(key string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.data, key)
}

// List returns all stored keys.
func (v *Vault) List() []string {
	v.mu.RLock()
	defer v.mu.RUnlock()
	keys := make([]string, 0, len(v.data))
	for k := range v.data {
		keys = append(keys, k)
	}
	return keys
}
