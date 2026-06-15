// Package notary provides cryptographic signing, verification, and
// attestation for agent actions and artifacts. It supports Ed25519
// signatures, hash chains, timestamping, and trust-on-first-use (TOFU).
package notary

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// KeyPair is an Ed25519 key pair.
type KeyPair struct {
	PublicKey  ed25519.PublicKey  `json:"public_key"`
	PrivateKey ed25519.PrivateKey `json:"-"`
	CreatedAt  time.Time          `json:"created_at"`
	Label      string             `json:"label"`
}

// GenerateKeyPair creates a new Ed25519 key pair.
func GenerateKeyPair(label string) (*KeyPair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil { return nil, err }
	return &KeyPair{PublicKey: pub, PrivateKey: priv, CreatedAt: time.Now(), Label: label}, nil
}

// KeyID returns a short identifier derived from the public key.
func (kp *KeyPair) KeyID() string { h := sha256.Sum256(kp.PublicKey); return hex.EncodeToString(h[:8]) }

// Sign signs a message with the private key.
func (kp *KeyPair) Sign(message []byte) []byte { return ed25519.Sign(kp.PrivateKey, message) }

// Verify checks a signature against the public key.
func (kp *KeyPair) Verify(message, signature []byte) bool { return ed25519.Verify(kp.PublicKey, message, signature) }

// Signature is a signed attestation.
type Signature struct {
	KeyID     string    `json:"key_id"`
	Message   []byte    `json:"message"`
	Signature []byte    `json:"signature"`
	Timestamp time.Time `json:"timestamp"`
}

// Attestation is a signed claim about an artifact or action.
type Attestation struct {
	Subject   string            `json:"subject"`
	Claims    map[string]any    `json:"claims"`
	Signature *Signature        `json:"signature"`
	CreatedAt time.Time         `json:"created_at"`
	ExpiresAt time.Time         `json:"expires_at,omitempty"`
}

// Notary manages signing and verification.
type Notary struct {
	mu       sync.RWMutex
	keys     map[string]*KeyPair
	attestations map[string][]*Attestation
	maxAttest int
}

// NewNotary creates a notary.
func NewNotary() *Notary { return &Notary{keys: map[string]*KeyPair{}, attestations: map[string][]*Attestation{}, maxAttest: 1000} }

// AddKey registers a key pair.
func (n *Notary) AddKey(kp *KeyPair) {
	n.mu.Lock(); defer n.mu.Unlock()
	n.keys[kp.KeyID()] = kp
}

// GenerateAndAddKey creates a new key and registers it.
func (n *Notary) GenerateAndAddKey(label string) (*KeyPair, error) {
	kp, err := GenerateKeyPair(label)
	if err != nil { return nil, err }
	n.AddKey(kp)
	return kp, nil
}

// Sign creates an attestation for a subject.
func (n *Notary) Sign(keyID, subject string, claims map[string]any, ttl time.Duration) (*Attestation, error) {
	n.mu.RLock()
	kp, ok := n.keys[keyID]
	n.mu.RUnlock()
	if !ok { return nil, fmt.Errorf("key %q not found", keyID) }

	att := &Attestation{Subject: subject, Claims: claims, CreatedAt: time.Now()}
	if ttl > 0 { att.ExpiresAt = att.CreatedAt.Add(ttl) }

	payload, _ := json.Marshal(map[string]any{"subject": subject, "claims": claims, "created_at": att.CreatedAt.Unix(), "expires_at": att.ExpiresAt.Unix()})
	sig := &Signature{KeyID: keyID, Message: payload, Signature: kp.Sign(payload), Timestamp: time.Now()}
	att.Signature = sig

	n.mu.Lock()
	n.attestations[subject] = append(n.attestations[subject], att)
	if len(n.attestations[subject]) > n.maxAttest { n.attestations[subject] = n.attestations[subject][1:] }
	n.mu.Unlock()

	return att, nil
}

// Verify checks an attestation's signature and expiry.
func (n *Notary) Verify(att *Attestation) (bool, string) {
	if att.Signature == nil { return false, "no signature" }
	if !att.ExpiresAt.IsZero() && time.Now().After(att.ExpiresAt) { return false, "attestation expired" }

	n.mu.RLock()
	kp, ok := n.keys[att.Signature.KeyID]
	n.mu.RUnlock()
	if !ok { return false, "unknown key" }

	payload, _ := json.Marshal(map[string]any{"subject": att.Subject, "claims": att.Claims, "created_at": att.CreatedAt.Unix(), "expires_at": att.ExpiresAt.Unix()})
	if !kp.Verify(payload, att.Signature.Signature) { return false, "signature mismatch" }
	return true, "ok"
}

// Keys returns all registered key IDs.
func (n *Notary) Keys() []string {
	n.mu.RLock(); defer n.mu.RUnlock()
	var out []string
	for k := range n.keys { out = append(out, k) }
	sort.Strings(out)
	return out
}

// AttestationsFor returns attestations for a subject.
func (n *Notary) AttestationsFor(subject string) []*Attestation {
	n.mu.RLock(); defer n.mu.RUnlock()
	out := make([]*Attestation, len(n.attestations[subject]))
	copy(out, n.attestations[subject])
	return out
}

// ── Hash Chain ────────────────────────────────────────────

// ChainEntry is one link in a hash chain.
type ChainEntry struct {
	Index     int       `json:"index"`
	Data      string    `json:"data"`
	Hash      string    `json:"hash"`
	PrevHash  string    `json:"prev_hash"`
	Timestamp time.Time `json:"timestamp"`
}

// HashChain is a tamper-evident append-only log.
type HashChain struct {
	mu       sync.Mutex
	entries  []ChainEntry
	lastHash string
}

// NewHashChain creates a hash chain.
func NewHashChain() *HashChain { return &HashChain{} }

// Append adds an entry to the chain.
func (hc *HashChain) Append(data string) ChainEntry {
	hc.mu.Lock(); defer hc.mu.Unlock()
	idx := len(hc.entries)
	h := sha256.New()
	raw := fmt.Sprintf("%d:%s:%s:%d", idx, hc.lastHash, data, time.Now().UnixNano())
	h.Write([]byte(raw))
	entry := ChainEntry{Index: idx, Data: data, Hash: hex.EncodeToString(h.Sum(nil)), PrevHash: hc.lastHash, Timestamp: time.Now()}
	hc.lastHash = entry.Hash
	hc.entries = append(hc.entries, entry)
	return entry
}

// VerifyChain checks the integrity of the entire chain.
func (hc *HashChain) VerifyChain() (bool, []string) {
	hc.mu.Lock(); defer hc.mu.Unlock()
	var issues []string
	var prev string
	for _, e := range hc.entries {
		if e.PrevHash != prev { issues = append(issues, fmt.Sprintf("chain break at index %d", e.Index)) }
		h := sha256.New()
		raw := fmt.Sprintf("%d:%s:%s:%d", e.Index, prev, e.Data, e.Timestamp.UnixNano())
		h.Write([]byte(raw))
		if hex.EncodeToString(h.Sum(nil)) != e.Hash { issues = append(issues, fmt.Sprintf("hash mismatch at index %d", e.Index)) }
		prev = e.Hash
	}
	return len(issues) == 0, issues
}

// Length returns the chain length.
func (hc *HashChain) Length() int { hc.mu.Lock(); defer hc.mu.Unlock(); return len(hc.entries) }

// ── Formatters ────────────────────────────────────────────

// FormatAttestation formats an attestation.
func FormatAttestation(att *Attestation) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Attestation: %s\n%s\n\n", att.Subject, strings.Repeat("─", 50))
	fmt.Fprintf(&sb, "  Created:  %s\n", att.CreatedAt.Format(time.RFC3339))
	if !att.ExpiresAt.IsZero() { fmt.Fprintf(&sb, "  Expires:  %s\n", att.ExpiresAt.Format(time.RFC3339)) }
	fmt.Fprintf(&sb, "  Signed by: %s\n", att.Signature.KeyID)
	for k, v := range att.Claims { fmt.Fprintf(&sb, "  %s: %v\n", k, v) }
	return sb.String()
}

// FormatChain formats a hash chain.
func FormatChain(hc *HashChain) string {
	hc.mu.Lock(); defer hc.mu.Unlock()
	var sb strings.Builder
	fmt.Fprintf(&sb, "Hash Chain (%d entries):\n%s\n\n", len(hc.entries), strings.Repeat("─", 60))
	for _, e := range hc.entries {
		fmt.Fprintf(&sb, "  [%d] %s → %s\n", e.Index, e.Timestamp.Format("15:04:05"), truncHex(e.Hash, 16))
	}
	return sb.String()
}

func truncHex(h string, n int) string { if len(h) <= n { return h }; return h[:n] + "..." }
