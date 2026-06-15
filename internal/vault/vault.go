// Package vault implements a secret vault with envelope encryption, key hierarchy
// (master key → data keys), secret versioning, access policies, and audit logging.
package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"
)

// --- Core types ---

// SecretVersion represents one version of a secret.
type SecretVersion struct {
	Version    int       `json:"version"`
	CreatedAt  time.Time `json:"created_at"`
	CreatedBy  string    `json:"created_by"`
	Ciphertext string    `json:"ciphertext"` // Base64-encoded encrypted value.
	KeyID      string    `json:"key_id"`     // Which data key encrypted this version.
	Nonce      string    `json:"nonce"`      // Base64-encoded nonce.
}

// Secret holds metadata and all versions of a secret.
type Secret struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Labels      map[string]string `json:"labels"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	Versions    []SecretVersion   `json:"versions"`
	MaxVersions int               `json:"max_versions"`
}

// LatestVersion returns the most recent version index and value.
func (s *Secret) LatestVersion() (int, *SecretVersion) {
	if len(s.Versions) == 0 {
		return 0, nil
	}
	idx := len(s.Versions) - 1
	return idx, &s.Versions[idx]
}

// --- Key hierarchy ---

// DataKey is an encryption key used to encrypt secrets.
type DataKey struct {
	ID        string    `json:"id"`
	Key       []byte    `json:"key"` // Raw AES-256 key.
	CreatedAt time.Time `json:"created_at"`
	Active    bool      `json:"active"`
}

// MasterKey wraps (encrypts) data keys. In production this would use a KMS.
type MasterKey struct {
	key []byte // AES-256 master key, derived from a passphrase or KMS.
}

// NewMasterKey creates a master key from a passphrase using PBKDF2-like derivation.
func NewMasterKey(passphrase string) *MasterKey {
	h := sha256.Sum256([]byte(passphrase))
	return &MasterKey{key: h[:]}
}

// GenerateMasterKey creates a random master key.
func GenerateMasterKey() (*MasterKey, error) {
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("failed to generate master key: %w", err)
	}
	return &MasterKey{key: key}, nil
}

// Encrypt encrypts plaintext with the master key directly (used for data key wrapping).
func (mk *MasterKey) Encrypt(plaintext []byte) (ciphertext []byte, nonce []byte, _ error) {
	block, err := aes.NewCipher(mk.key)
	if err != nil {
		return nil, nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	n := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, n); err != nil {
		return nil, nil, err
	}
	return gcm.Seal(nil, n, plaintext, nil), n, nil
}

// Decrypt decrypts ciphertext with the master key.
func (mk *MasterKey) Decrypt(ciphertext []byte, nonce []byte) ([]byte, error) {
	block, err := aes.NewCipher(mk.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, nonce, ciphertext, nil)
}

// WrapKey encrypts a data key with the master key.
func (mk *MasterKey) WrapKey(dk *DataKey) (wrapped []byte, nonce []byte, _ error) {
	return mk.Encrypt(dk.Key)
}

// UnwrapKey decrypts a data key.
func (mk *MasterKey) UnwrapKey(wrapped, nonce []byte) ([]byte, error) {
	return mk.Decrypt(wrapped, nonce)
}

// --- Data Key Manager ---

type dataKeyManager struct {
	mu        sync.RWMutex
	keys      map[string]*DataKey // keyID -> DataKey
	wrapped   map[string][]byte   // keyID -> wrapped key bytes
	nonces    map[string][]byte   // keyID -> nonce
	masterKey *MasterKey
}

func newDataKeyManager(mk *MasterKey) *dataKeyManager {
	return &dataKeyManager{
		keys:      make(map[string]*DataKey),
		wrapped:   make(map[string][]byte),
		nonces:    make(map[string][]byte),
		masterKey: mk,
	}
}

// createDataKey generates a new data key and wraps it with the master key.
func (dkm *dataKeyManager) createDataKey() (*DataKey, error) {
	raw := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return nil, err
	}
	id := generateID("dk")
	dk := &DataKey{
		ID:        id,
		Key:       raw,
		CreatedAt: time.Now(),
		Active:    true,
	}
	wrapped, nonce, err := dkm.masterKey.WrapKey(dk)
	if err != nil {
		return nil, err
	}
	dkm.mu.Lock()
	dkm.keys[id] = dk
	dkm.wrapped[id] = wrapped
	dkm.nonces[id] = nonce
	dkm.mu.Unlock()
	return dk, nil
}

// getDataKey returns the unwrapped key for a given key ID.
func (dkm *dataKeyManager) getDataKey(keyID string) ([]byte, error) {
	dkm.mu.RLock()
	dk, ok := dkm.keys[keyID]
	if ok {
		dkm.mu.RUnlock()
		return dk.Key, nil
	}
	wrapped := dkm.wrapped[keyID]
	nonce := dkm.nonces[keyID]
	dkm.mu.RUnlock()

	if wrapped == nil {
		return nil, fmt.Errorf("data key %q not found", keyID)
	}
	key, err := dkm.masterKey.UnwrapKey(wrapped, nonce)
	if err != nil {
		return nil, err
	}
	return key, nil
}

// --- Access Policy ---

// AccessCapability defines a permission.
type AccessCapability string

const (
	AccessRead   AccessCapability = "read"
	AccessWrite  AccessCapability = "write"
	AccessAdmin  AccessCapability = "admin"
	AccessList   AccessCapability = "list"
	AccessDelete AccessCapability = "delete"
)

// Policy defines who can access which secrets.
type Policy struct {
	ID           string             `json:"id"`
	Name         string             `json:"name"`
	Subjects     []string           `json:"subjects"`      // Who this applies to (user IDs, role names).
	Capabilities []AccessCapability `json:"capabilities"`  // Allowed operations.
	SecretPrefix string             `json:"secret_prefix"` // Scope: which secrets this applies to.
	CreatedAt    time.Time          `json:"created_at"`
}

// Allows checks whether the policy grants a capability for a given secret path.
func (p *Policy) Allows(subject, secretPath string, cap AccessCapability) bool {
	// Check subject match.
	subjectMatch := false
	for _, s := range p.Subjects {
		if s == subject || s == "*" {
			subjectMatch = true
			break
		}
	}
	if !subjectMatch {
		return false
	}
	// Check prefix match.
	if p.SecretPrefix != "" && len(secretPath) >= len(p.SecretPrefix) &&
		secretPath[:len(p.SecretPrefix)] != p.SecretPrefix {
		return false
	}
	// Check capability.
	for _, c := range p.Capabilities {
		if c == cap || c == AccessAdmin {
			return true
		}
	}
	return false
}

// --- Audit Log ---

// AuditEntry records an operation on the vault.
type AuditEntry struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Subject   string    `json:"subject"`
	Action    string    `json:"action"` // "create", "read", "update", "delete", "list".
	SecretID  string    `json:"secret_id"`
	Success   bool      `json:"success"`
	Details   string    `json:"details"`
}

// --- Vault ---

// Vault is the top-level secret vault.
type Vault struct {
	mu        sync.RWMutex
	secrets   map[string]*Secret // id -> secret
	policies  []Policy
	auditLog  []AuditEntry
	dkm       *dataKeyManager
	masterKey *MasterKey
	auditCap  int
}

// New creates a new Vault with the given master passphrase.
func New(passphrase string) *Vault {
	mk := NewMasterKey(passphrase)
	return &Vault{
		secrets:   make(map[string]*Secret),
		masterKey: mk,
		dkm:       newDataKeyManager(mk),
		auditCap:  10000,
	}
}

// NewWithMasterKey creates a vault with an existing master key.
func NewWithMasterKey(mk *MasterKey) *Vault {
	return &Vault{
		secrets:   make(map[string]*Secret),
		masterKey: mk,
		dkm:       newDataKeyManager(mk),
		auditCap:  10000,
	}
}

// CreateSecret encrypts and stores a new secret.
func (v *Vault) CreateSecret(name string, value []byte, labels map[string]string, createdBy string) (*Secret, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	if _, exists := v.secrets[name]; exists {
		return nil, fmt.Errorf("secret %q already exists", name)
	}

	dk, err := v.dkm.createDataKey()
	if err != nil {
		return nil, fmt.Errorf("failed to create data key: %w", err)
	}

	ciphertext, nonce, err := v.encryptWithKey(dk.Key, value)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	sec := &Secret{
		ID:          generateID("sec"),
		Name:        name,
		Labels:      labels,
		CreatedAt:   now,
		UpdatedAt:   now,
		MaxVersions: 10,
		Versions: []SecretVersion{{
			Version:    1,
			CreatedAt:  now,
			CreatedBy:  createdBy,
			Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
			KeyID:      dk.ID,
			Nonce:      base64.StdEncoding.EncodeToString(nonce),
		}},
	}
	v.secrets[sec.ID] = sec
	v.addAudit(AuditEntry{
		Timestamp: now,
		Subject:   createdBy,
		Action:    "create",
		SecretID:  sec.ID,
		Success:   true,
	})
	return sec, nil
}

// GetSecret decrypts and returns the latest version of a secret.
func (v *Vault) GetSecret(id, subject string) ([]byte, *Secret, error) {
	v.mu.RLock()
	sec, ok := v.secrets[id]
	v.mu.RUnlock()
	if !ok {
		v.addAudit(AuditEntry{Timestamp: time.Now(), Subject: subject, Action: "read", SecretID: id, Success: false, Details: "not found"})
		return nil, nil, fmt.Errorf("secret %q not found", id)
	}

	if !v.authorize(subject, sec.Name, AccessRead) {
		v.addAudit(AuditEntry{Timestamp: time.Now(), Subject: subject, Action: "read", SecretID: id, Success: false, Details: "unauthorized"})
		return nil, nil, fmt.Errorf("unauthorized: %s cannot read %s", subject, sec.Name)
	}

	_, sv := sec.LatestVersion()
	if sv == nil {
		return nil, nil, fmt.Errorf("no versions for secret %q", id)
	}

	dk, err := v.dkm.getDataKey(sv.KeyID)
	if err != nil {
		return nil, nil, err
	}

	ct, _ := base64.StdEncoding.DecodeString(sv.Ciphertext)
	n, _ := base64.StdEncoding.DecodeString(sv.Nonce)
	plain, err := v.decryptWithKey(dk, ct, n)
	if err != nil {
		return nil, nil, err
	}

	v.addAudit(AuditEntry{Timestamp: time.Now(), Subject: subject, Action: "read", SecretID: id, Success: true})
	return plain, sec, nil
}

// UpdateSecret adds a new version to an existing secret.
func (v *Vault) UpdateSecret(id string, newValue []byte, updatedBy string) (*Secret, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	sec, ok := v.secrets[id]
	if !ok {
		return nil, fmt.Errorf("secret %q not found", id)
	}

	if !v.authorize(updatedBy, sec.Name, AccessWrite) {
		return nil, fmt.Errorf("unauthorized")
	}

	dk, err := v.dkm.createDataKey()
	if err != nil {
		return nil, err
	}

	ciphertext, nonce, err := v.encryptWithKey(dk.Key, newValue)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	newVer := SecretVersion{
		Version:    len(sec.Versions) + 1,
		CreatedAt:  now,
		CreatedBy:  updatedBy,
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
		KeyID:      dk.ID,
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
	}
	sec.Versions = append(sec.Versions, newVer)
	sec.UpdatedAt = now

	// Trim old versions if over limit.
	if len(sec.Versions) > sec.MaxVersions {
		sec.Versions = sec.Versions[len(sec.Versions)-sec.MaxVersions:]
	}

	v.addAudit(AuditEntry{Timestamp: now, Subject: updatedBy, Action: "update", SecretID: id, Success: true})
	return sec, nil
}

// DeleteSecret removes a secret entirely.
func (v *Vault) DeleteSecret(id, subject string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	sec, ok := v.secrets[id]
	if !ok {
		return fmt.Errorf("secret %q not found", id)
	}

	if !v.authorize(subject, sec.Name, AccessDelete) {
		return fmt.Errorf("unauthorized")
	}

	delete(v.secrets, id)
	v.addAudit(AuditEntry{Timestamp: time.Now(), Subject: subject, Action: "delete", SecretID: id, Success: true})
	return nil
}

// ListSecrets returns all secret IDs matching a prefix.
func (v *Vault) ListSecrets(prefix, subject string) ([]*Secret, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()

	if !v.authorize(subject, prefix, AccessList) {
		return nil, fmt.Errorf("unauthorized")
	}

	out := make([]*Secret, 0)
	for _, sec := range v.secrets {
		if prefix == "" || (len(sec.Name) >= len(prefix) && sec.Name[:len(prefix)] == prefix) {
			out = append(out, sec)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	v.addAudit(AuditEntry{Timestamp: time.Now(), Subject: subject, Action: "list", SecretID: "", Success: true, Details: fmt.Sprintf("%d results", len(out))})
	return out, nil
}

// GetVersion returns a specific version of a secret.
func (v *Vault) GetVersion(id string, version int, subject string) ([]byte, error) {
	v.mu.RLock()
	sec, ok := v.secrets[id]
	v.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("secret %q not found", id)
	}
	if !v.authorize(subject, sec.Name, AccessRead) {
		return nil, fmt.Errorf("unauthorized")
	}
	for i := len(sec.Versions) - 1; i >= 0; i-- {
		if sec.Versions[i].Version == version {
			sv := &sec.Versions[i]
			dk, err := v.dkm.getDataKey(sv.KeyID)
			if err != nil {
				return nil, err
			}
			ct, _ := base64.StdEncoding.DecodeString(sv.Ciphertext)
			n, _ := base64.StdEncoding.DecodeString(sv.Nonce)
			return v.decryptWithKey(dk, ct, n)
		}
	}
	return nil, fmt.Errorf("version %d not found", version)
}

// --- Policy Management ---

// AddPolicy adds an access policy.
func (v *Vault) AddPolicy(p Policy) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if p.ID == "" {
		p.ID = generateID("pol")
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now()
	}
	v.policies = append(v.policies, p)
}

// RemovePolicy deletes a policy by ID.
func (v *Vault) RemovePolicy(id string) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	for i, p := range v.policies {
		if p.ID == id {
			v.policies = append(v.policies[:i], v.policies[i+1:]...)
			return true
		}
	}
	return false
}

// Policies returns all policies.
func (v *Vault) Policies() []Policy {
	v.mu.RLock()
	defer v.mu.RUnlock()
	out := make([]Policy, len(v.policies))
	copy(out, v.policies)
	return out
}

// authorize checks whether a subject has the given capability for a secret.
func (v *Vault) authorize(subject, secretPath string, cap AccessCapability) bool {
	for _, p := range v.policies {
		if p.Allows(subject, secretPath, cap) {
			return true
		}
	}
	// Default: allow if no policies defined (open mode).
	return len(v.policies) == 0
}

// --- Audit ---

func (v *Vault) addAudit(e AuditEntry) {
	if e.ID == "" {
		e.ID = generateID("audit")
	}
	v.auditLog = append(v.auditLog, e)
	if len(v.auditLog) > v.auditCap {
		v.auditLog = v.auditLog[len(v.auditLog)-v.auditCap:]
	}
}

// AuditLog returns the audit log.
func (v *Vault) AuditLog() []AuditEntry {
	v.mu.RLock()
	defer v.mu.RUnlock()
	out := make([]AuditEntry, len(v.auditLog))
	copy(out, v.auditLog)
	return out
}

// --- Encryption helpers ---

func (v *Vault) encryptWithKey(key, plaintext []byte) (ciphertext, nonce []byte, _ error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	n := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, n); err != nil {
		return nil, nil, err
	}
	return gcm.Seal(nil, n, plaintext, nil), n, nil
}

func (v *Vault) decryptWithKey(key, ciphertext, nonce []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, nonce, ciphertext, nil)
}

// FormatSecrets returns a human-readable listing of all secrets.
func (v *Vault) FormatSecrets() string {
	v.mu.RLock()
	defer v.mu.RUnlock()
	s := fmt.Sprintf("Vault: %d secrets, %d policies, %d audit entries\n",
		len(v.secrets), len(v.policies), len(v.auditLog))
	for _, sec := range v.secrets {
		s += fmt.Sprintf("  %s (%s) versions=%d updated=%s\n",
			sec.ID, sec.Name, len(sec.Versions), sec.UpdatedAt.Format(time.RFC3339))
	}
	return s
}

// --- ID generation ---

var idCounter int64

func generateID(prefix string) string {
	idCounter++
	return fmt.Sprintf("%s-%d-%x", prefix, time.Now().UnixNano(), idCounter)
}

// --- Key Rotation ---

// RotateMasterKey derives a new master key and re-wraps all data keys.
func (v *Vault) RotateMasterKey(newPassphrase string) error {
	newMK := NewMasterKey(newPassphrase)
	v.mu.Lock()
	defer v.mu.Unlock()

	for id, dk := range v.dkm.keys {
		wrapped, nonce, err := newMK.WrapKey(dk)
		if err != nil {
			return fmt.Errorf("rewrap key %s: %w", id, err)
		}
		v.dkm.wrapped[id] = wrapped
		v.dkm.nonces[id] = nonce
	}
	v.masterKey = newMK
	v.addAudit(AuditEntry{Timestamp: time.Now(), Action: "rotate-master-key", Success: true})
	return nil
}

// --- Secret Labels / Search ---

// SearchSecrets finds secrets by label key-value pairs.
func (v *Vault) SearchSecrets(labels map[string]string) []*Secret {
	v.mu.RLock()
	defer v.mu.RUnlock()
	var out []*Secret
	for _, sec := range v.secrets {
		match := true
		for lk, lv := range labels {
			if sec.Labels[lk] != lv {
				match = false
				break
			}
		}
		if match {
			out = append(out, sec)
		}
	}
	return out
}

// --- Secret Metadata ---

// SecretMetadata returns a secret without decrypting its contents.
type SecretMetadata struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Labels       map[string]string `json:"labels"`
	VersionCount int               `json:"version_count"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

// GetMetadata returns metadata for a secret.
func (v *Vault) GetMetadata(id string) (*SecretMetadata, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	sec, ok := v.secrets[id]
	if !ok {
		return nil, fmt.Errorf("secret %q not found", id)
	}
	return &SecretMetadata{
		ID: sec.ID, Name: sec.Name, Labels: sec.Labels,
		VersionCount: len(sec.Versions), CreatedAt: sec.CreatedAt, UpdatedAt: sec.UpdatedAt,
	}, nil
}

// --- Export / Import ---

// ExportSecret exports a secret's metadata and all encrypted versions.
func (v *Vault) ExportSecret(id string) (*Secret, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	sec, ok := v.secrets[id]
	if !ok {
		return nil, fmt.Errorf("secret %q not found", id)
	}
	cp := *sec
	cp.Versions = make([]SecretVersion, len(sec.Versions))
	copy(cp.Versions, sec.Versions)
	return &cp, nil
}

// ImportSecret imports an exported secret.
func (v *Vault) ImportSecret(sec *Secret) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if _, exists := v.secrets[sec.ID]; exists {
		return fmt.Errorf("secret %q already exists", sec.ID)
	}
	v.secrets[sec.ID] = sec
	v.addAudit(AuditEntry{Timestamp: time.Now(), Action: "import", SecretID: sec.ID, Success: true})
	return nil
}

// --- Batch Operations ---

// BatchCreate creates multiple secrets.
func (v *Vault) BatchCreate(items []struct {
	Name   string
	Value  []byte
	Labels map[string]string
}, createdBy string) ([]*Secret, []error) {
	secrets := make([]*Secret, len(items))
	errs := make([]error, len(items))
	for i, item := range items {
		secrets[i], errs[i] = v.CreateSecret(item.Name, item.Value, item.Labels, createdBy)
	}
	return secrets, errs
}

// --- Policy Evaluation Helpers ---

// EffectiveCapabilities returns the set of capabilities a subject has for a secret path.
func (v *Vault) EffectiveCapabilities(subject, secretPath string) []AccessCapability {
	v.mu.RLock()
	defer v.mu.RUnlock()
	var caps []AccessCapability
	for _, p := range v.policies {
		if p.Allows(subject, secretPath, AccessAdmin) {
			return []AccessCapability{AccessAdmin, AccessRead, AccessWrite, AccessList, AccessDelete}
		}
		for _, c := range p.Capabilities {
			if p.Allows(subject, secretPath, c) {
				caps = append(caps, c)
			}
		}
	}
	return caps
}

// --- Vault Stats ---

// VaultStats holds aggregate vault statistics.
type VaultStats struct {
	TotalSecrets  int `json:"total_secrets"`
	TotalVersions int `json:"total_versions"`
	TotalPolicies int `json:"total_policies"`
	TotalAudit    int `json:"total_audit_entries"`
	TotalDataKeys int `json:"total_data_keys"`
}

// VaultStats returns aggregate statistics.
func (v *Vault) VaultStats() VaultStats {
	v.mu.RLock()
	defer v.mu.RUnlock()
	totalVersions := 0
	for _, sec := range v.secrets {
		totalVersions += len(sec.Versions)
	}
	return VaultStats{
		TotalSecrets:  len(v.secrets),
		TotalVersions: totalVersions,
		TotalPolicies: len(v.policies),
		TotalAudit:    len(v.auditLog),
		TotalDataKeys: len(v.dkm.keys),
	}
}

// --- SecureString ---

// SecureString is a string that overwrites its memory on zeroing.
type SecureString struct {
	data []byte
}

// NewSecureString creates a secure string from bytes.
func NewSecureString(b []byte) *SecureString {
	cp := make([]byte, len(b))
	copy(cp, b)
	return &SecureString{data: cp}
}

// Bytes returns a copy of the underlying bytes.
func (ss *SecureString) Bytes() []byte {
	cp := make([]byte, len(ss.data))
	copy(cp, ss.data)
	return cp
}

// String returns the string value.
func (ss *SecureString) String() string { return string(ss.data) }

// Zero overwrites the data with zeros.
func (ss *SecureString) Zero() {
	for i := range ss.data {
		ss.data[i] = 0
	}
	ss.data = nil
}
