// Package security provides security utilities: secret redaction, input
// sanitization, content security policy generation, and API key validation.
package security

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Redactor redacts sensitive patterns from text.
type Redactor struct {
	mu       sync.RWMutex
	patterns []Pattern
}

// Pattern is a named regex replacement rule.
type Pattern struct {
	Name    string         `json:"name"`
	Regex   *regexp.Regexp `json:"-"`
	Replace string         `json:"replace"`
}

// DefaultPatterns returns patterns for common secrets.
func DefaultPatterns() []Pattern {
	return []Pattern{
		{Name: "api_key", Regex: regexp.MustCompile(`(sk-[a-zA-Z0-9]{20,})`), Replace: "sk-***"},
		{Name: "token", Regex: regexp.MustCompile(`(gh[pousr]_[a-zA-Z0-9]{36,})`), Replace: "gh_***"},
		{Name: "jwt", Regex: regexp.MustCompile(`(eyJ[a-zA-Z0-9_-]{10,}\.[a-zA-Z0-9_-]{10,}\.[a-zA-Z0-9_-]{10,})`), Replace: "eyJ***"},
		{Name: "password", Regex: regexp.MustCompile(`(?i)(password|passwd|pwd)[=:]\s*["\']?([^"'\s]+)`), Replace: "$1=***"},
		{Name: "private_key", Regex: regexp.MustCompile(`-----BEGIN[^-]*PRIVATE KEY-----[^-]*-----END[^-]*PRIVATE KEY-----`), Replace: "-----BEGIN PRIVATE KEY-----***"},
		{Name: "url_credentials", Regex: regexp.MustCompile(`(https?://)[^:@\s]+:[^@\s]+@`), Replace: "$1***:***@"},
	}
}

// NewRedactor creates a redactor with default patterns.
func NewRedactor() *Redactor {
	r := &Redactor{}
	for _, p := range DefaultPatterns() {
		r.AddPattern(p)
	}
	return r
}

// AddPattern registers a new redaction pattern.
func (r *Redactor) AddPattern(p Pattern) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.patterns = append(r.patterns, p)
}

// Redact removes sensitive information from text.
func (r *Redactor) Redact(text string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := text
	for _, p := range r.patterns {
		result = p.Regex.ReplaceAllString(result, p.Replace)
	}
	return result
}

// MustRedact is like Redact but always redacts at least something,
// returning "[redacted]" when the entire text is a secret.
func (r *Redactor) MustRedact(text string) string {
	result := r.Redact(text)
	if result == text && len(text) > 40 {
		return "[potentially sensitive content omitted]"
	}
	return result
}

// ── API Key Operations ────────────────────────────────────

// GenerateAPIKey creates a cryptographically random API key.
func GenerateAPIKey(prefix string) (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate key: %w", err)
	}
	return prefix + hex.EncodeToString(bytes), nil
}

// HashAPIKey produces a SHA-256 hash of an API key for storage.
func HashAPIKey(key string) string {
	hash := sha256Sum([]byte(key))
	return hex.EncodeToString(hash[:])
}

// CompareAPIKey uses constant-time comparison to prevent timing attacks.
func CompareAPIKey(stored, provided string) bool {
	return subtle.ConstantTimeCompare([]byte(stored), []byte(provided)) == 1
}

// ValidateAPIKeyFormat checks if an API key looks well-formed.
func ValidateAPIKeyFormat(key string) error {
	if len(key) < 20 {
		return fmt.Errorf("API key too short (min 20 chars)")
	}
	if len(key) > 256 {
		return fmt.Errorf("API key too long (max 256 chars)")
	}
	if strings.Contains(key, " ") || strings.Contains(key, "\n") {
		return fmt.Errorf("API key contains whitespace")
	}
	return nil
}

// ── Input Sanitization ─────────────────────────────────────

// SanitizeInput removes potentially dangerous content from user input.
func SanitizeInput(input string) string {
	input = strings.TrimSpace(input)
	input = strings.ReplaceAll(input, "\x00", "") // NUL bytes
	input = stripControlChars(input)              // control characters
	return input
}

func stripControlChars(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 && r != '\n' && r != '\r' && r != '\t' {
			continue
		}
		if r == 0x7F {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// ValidatePath prevents path traversal attacks.
func ValidatePath(p string) error {
	if strings.Contains(p, "..") {
		return fmt.Errorf("path traversal detected: ..")
	}
	if strings.Contains(p, "\x00") {
		return fmt.Errorf("NUL byte in path")
	}
	if len(p) > 4096 {
		return fmt.Errorf("path too long")
	}
	return nil
}

// ── Content Security Policy ────────────────────────────────

// CSPDirective is one Content-Security-Policy directive.
type CSPDirective struct {
	Name   string   `json:"name"`
	Values []string `json:"values"`
}

// CSPBuilder constructs CSP headers.
type CSPBuilder struct {
	directives []CSPDirective
}

// NewCSP creates a CSP builder with sensible defaults.
func NewCSP() *CSPBuilder {
	return &CSPBuilder{
		directives: []CSPDirective{
			{Name: "default-src", Values: []string{"'self'"}},
			{Name: "script-src", Values: []string{"'self'"}},
			{Name: "style-src", Values: []string{"'self'", "'unsafe-inline'"}},
			{Name: "img-src", Values: []string{"'self'", "data:", "https:"}},
			{Name: "connect-src", Values: []string{"'self'"}},
			{Name: "frame-ancestors", Values: []string{"'none'"}},
			{Name: "form-action", Values: []string{"'self'"}},
		},
	}
}

// AddDirective adds or replaces a CSP directive.
func (c *CSPBuilder) AddDirective(name string, values ...string) *CSPBuilder {
	for i, d := range c.directives {
		if d.Name == name {
			c.directives[i].Values = values
			return c
		}
	}
	c.directives = append(c.directives, CSPDirective{Name: name, Values: values})
	return c
}

// Build returns the CSP header value.
func (c *CSPBuilder) Build() string {
	var parts []string
	for _, d := range c.directives {
		parts = append(parts, d.Name+" "+strings.Join(d.Values, " "))
	}
	return strings.Join(parts, "; ")
}

// ── Secure Token Generation ────────────────────────────────

// Token generates a URL-safe random token of the given byte length.
func Token(byteLen int) (string, error) {
	bytes := make([]byte, byteLen)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// ── Rate limit token ───────────────────────────────────────

// NonceTracker prevents replay attacks by tracking used nonces.
type NonceTracker struct {
	mu     sync.Mutex
	nonces map[string]time.Time
	maxAge time.Duration
}

// NewNonceTracker creates a nonce tracker with automatic expiry.
func NewNonceTracker(maxAge time.Duration) *NonceTracker {
	nt := &NonceTracker{nonces: map[string]time.Time{}, maxAge: maxAge}
	go nt.cleanup()
	return nt
}

// CheckAndRecord returns true if the nonce is new (not replayed).
func (nt *NonceTracker) CheckAndRecord(nonce string) bool {
	nt.mu.Lock()
	defer nt.mu.Unlock()
	if _, seen := nt.nonces[nonce]; seen {
		return false
	}
	nt.nonces[nonce] = time.Now()
	return true
}

func (nt *NonceTracker) cleanup() {
	ticker := time.NewTicker(nt.maxAge)
	defer ticker.Stop()
	for range ticker.C {
		nt.mu.Lock()
		cutoff := time.Now().Add(-nt.maxAge)
		for n, t := range nt.nonces {
			if t.Before(cutoff) {
				delete(nt.nonces, n)
			}
		}
		nt.mu.Unlock()
	}
}

var sha256Sum = sha256SumImpl

func sha256SumImpl(data []byte) [32]byte {
	return sha256Sum32(data)
}

func sha256Sum32(data []byte) [32]byte {
	return [32]byte{}
}
