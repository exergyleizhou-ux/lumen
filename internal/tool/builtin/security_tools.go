package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"lumen/internal/audit"
	"lumen/internal/notary"
	"lumen/internal/seal"
	"lumen/internal/tool"
)

func init() {
	tool.RegisterBuiltin(&SealDataTool{})
	tool.RegisterBuiltin(&UnsealDataTool{})
	tool.RegisterBuiltin(&GenerateKeypairTool{})
	tool.RegisterBuiltin(&SignAttestationTool{})
	tool.RegisterBuiltin(&VerifyAttestationTool{})
	tool.RegisterBuiltin(&AuditQueryTool{})
	tool.RegisterBuiltin(&HashChainAppendTool{})
	tool.RegisterBuiltin(&HashChainVerifyTool{})
}

// ── Shared state ────────────────────────────────────────────────────────────

var (
	sealMgr    *seal.Manager
	sealOnce   sync.Once
	notaryInst *notary.Notary
	notaryOnce sync.Once
	hashChain  *notary.HashChain
	hashOnce   sync.Once
)

// auditStoreFn returns the audit store the audit_query tool reads. It is a var so
// tests can substitute a fixture store without touching the process-wide default.
var auditStoreFn = audit.Default

func getSealManager() *seal.Manager {
	sealOnce.Do(func() {
		sealMgr = seal.NewManager()
		sealMgr.GenerateKey()
	})
	return sealMgr
}

func getNotary() *notary.Notary {
	notaryOnce.Do(func() {
		notaryInst = notary.NewNotary()
	})
	return notaryInst
}

func getHashChain() *notary.HashChain {
	hashOnce.Do(func() {
		hashChain = notary.NewHashChain()
	})
	return hashChain
}

// ── seal_data ───────────────────────────────────────────────────────────────

type SealDataTool struct{}

func (t *SealDataTool) Name() string   { return "seal_data" }
func (t *SealDataTool) ReadOnly() bool { return false }

func (t *SealDataTool) Description() string {
	return "Encrypt plaintext and return a sealed envelope. Uses AES-GCM authenticated encryption with a managed key."
}

func (t *SealDataTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "plaintext":{"type":"string","description":"Plaintext data to encrypt"}
},
"required":["plaintext"]
}`)
}

func (t *SealDataTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Plaintext string `json:"plaintext"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Plaintext == "" {
		return "", fmt.Errorf("plaintext is required")
	}

	mgr := getSealManager()
	env, err := mgr.Seal([]byte(p.Plaintext))
	if err != nil {
		return "", fmt.Errorf("seal failed: %w", err)
	}
	b, _ := json.MarshalIndent(env, "", "  ")
	return string(b), nil
}

// ── unseal_data ─────────────────────────────────────────────────────────────

type UnsealDataTool struct{}

func (t *UnsealDataTool) Name() string   { return "unseal_data" }
func (t *UnsealDataTool) ReadOnly() bool { return false }

func (t *UnsealDataTool) Description() string {
	return "Decrypt a sealed envelope JSON returned by seal_data. Returns the original plaintext."
}

func (t *UnsealDataTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "envelope":{"type":"object","description":"The sealed envelope JSON produced by seal_data"}
},
"required":["envelope"]
}`)
}

func (t *UnsealDataTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Envelope json.RawMessage `json:"envelope"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}

	var env seal.Envelope
	if err := json.Unmarshal(p.Envelope, &env); err != nil {
		return "", fmt.Errorf("invalid envelope: %w", err)
	}

	mgr := getSealManager()
	plaintext, err := mgr.Unseal(&env)
	if err != nil {
		return "", fmt.Errorf("unseal failed: %w", err)
	}
	return string(plaintext), nil
}

// ── generate_keypair ────────────────────────────────────────────────────────

type GenerateKeypairTool struct{}

func (t *GenerateKeypairTool) Name() string   { return "generate_keypair" }
func (t *GenerateKeypairTool) ReadOnly() bool { return false }

func (t *GenerateKeypairTool) Description() string {
	return "Generate a new Ed25519 key pair for signing attestations. Returns the key ID (a hex-encoded identifier derived from the public key)."
}

func (t *GenerateKeypairTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "label":{"type":"string","description":"Optional human-readable label for the key pair"}
}
}`)
}

func (t *GenerateKeypairTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Label string `json:"label"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}

	n := getNotary()
	kp, err := n.GenerateAndAddKey(p.Label)
	if err != nil {
		return "", fmt.Errorf("key generation failed: %w", err)
	}

	out := map[string]interface{}{
		"key_id":     kp.KeyID(),
		"label":      kp.Label,
		"created_at": kp.CreatedAt.Format(time.RFC3339),
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return string(b), nil
}

// ── sign_attestation ────────────────────────────────────────────────────────

type SignAttestationTool struct{}

func (t *SignAttestationTool) Name() string   { return "sign_attestation" }
func (t *SignAttestationTool) ReadOnly() bool { return false }

func (t *SignAttestationTool) Description() string {
	return "Create a signed attestation for a subject with claims. Provide a key ID (from generate_keypair), a subject name, and a JSON object of claims. Optionally set a TTL in seconds."
}

func (t *SignAttestationTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "key_id":{"type":"string","description":"Key ID from generate_keypair"},
  "subject":{"type":"string","description":"Subject of the attestation (e.g. artifact name, action name)"},
  "claims":{"type":"object","description":"JSON object of claims about the subject"},
  "ttl_seconds":{"type":"number","description":"Optional time-to-live in seconds"}
},
"required":["key_id","subject","claims"]
}`)
}

func (t *SignAttestationTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		KeyID      string          `json:"key_id"`
		Subject    string          `json:"subject"`
		Claims     json.RawMessage `json:"claims"`
		TTLSeconds float64         `json:"ttl_seconds"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.KeyID == "" || p.Subject == "" {
		return "", fmt.Errorf("key_id and subject are required")
	}

	var claims map[string]interface{}
	if err := json.Unmarshal(p.Claims, &claims); err != nil {
		return "", fmt.Errorf("invalid claims: %w", err)
	}

	ttl := time.Duration(p.TTLSeconds) * time.Second
	n := getNotary()
	att, err := n.Sign(p.KeyID, p.Subject, claims, ttl)
	if err != nil {
		return "", fmt.Errorf("sign failed: %w", err)
	}
	b, _ := json.MarshalIndent(att, "", "  ")
	return string(b), nil
}

// ── verify_attestation ──────────────────────────────────────────────────────

type VerifyAttestationTool struct{}

func (t *VerifyAttestationTool) Name() string   { return "verify_attestation" }
func (t *VerifyAttestationTool) ReadOnly() bool { return true }

func (t *VerifyAttestationTool) Description() string {
	return "Verify a signed attestation. Provide the attestation JSON object. Returns whether it's valid and a reason string."
}

func (t *VerifyAttestationTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "attestation":{"type":"object","description":"The attestation JSON from sign_attestation"}
},
"required":["attestation"]
}`)
}

func (t *VerifyAttestationTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Attestation json.RawMessage `json:"attestation"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}

	var att notary.Attestation
	if err := json.Unmarshal(p.Attestation, &att); err != nil {
		return "", fmt.Errorf("invalid attestation: %w", err)
	}

	n := getNotary()
	valid, reason := n.Verify(&att)
	out := map[string]interface{}{
		"valid":  valid,
		"reason": reason,
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return string(b), nil
}

// ── audit_query ─────────────────────────────────────────────────────────────

type AuditQueryTool struct{}

func (t *AuditQueryTool) Name() string   { return "audit_query" }
func (t *AuditQueryTool) ReadOnly() bool { return true }

func (t *AuditQueryTool) Description() string {
	return "Query the persistent audit trail (every tool call the agent ran, with the recorded reason/args/result) for entries matching optional actor, action, and resource filters. Reads the on-disk JSONL store, so it answers \"why did the agent do X\" across restarts."
}

func (t *AuditQueryTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "actor":{"type":"string","description":"Filter by actor name (optional)"},
  "action":{"type":"string","description":"Filter by action name (optional)"},
  "resource":{"type":"string","description":"Filter by resource name (optional)"}
}
}`)
}

func (t *AuditQueryTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Actor    string `json:"actor"`
		Action   string `json:"action"`
		Resource string `json:"resource"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}

	// Read the persistent default store (nil-safe when auditing is disabled).
	entries := auditStoreFn().Query(p.Actor, p.Action, p.Resource, time.Time{}, time.Time{})
	return audit.FormatTrail(entries), nil
}

// ── hash_chain_append ───────────────────────────────────────────────────────

type HashChainAppendTool struct{}

func (t *HashChainAppendTool) Name() string   { return "hash_chain_append" }
func (t *HashChainAppendTool) ReadOnly() bool { return false }

func (t *HashChainAppendTool) Description() string {
	return "Append a data entry to the tamper-evident hash chain. Returns the new chain entry with its hash."
}

func (t *HashChainAppendTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "data":{"type":"string","description":"Data to append to the hash chain"}
},
"required":["data"]
}`)
}

func (t *HashChainAppendTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Data == "" {
		return "", fmt.Errorf("data is required")
	}

	hc := getHashChain()
	entry := hc.Append(p.Data)
	b, _ := json.MarshalIndent(entry, "", "  ")
	return string(b), nil
}

// ── hash_chain_verify ───────────────────────────────────────────────────────

type HashChainVerifyTool struct{}

func (t *HashChainVerifyTool) Name() string   { return "hash_chain_verify" }
func (t *HashChainVerifyTool) ReadOnly() bool { return true }

func (t *HashChainVerifyTool) Description() string {
	return "Verify the integrity of the entire hash chain. Returns whether the chain is valid and any issues found."
}

func (t *HashChainVerifyTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}

func (t *HashChainVerifyTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	hc := getHashChain()
	valid, issues := hc.VerifyChain()
	out := map[string]interface{}{
		"valid":  valid,
		"issues": issues,
		"length": hc.Length(),
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return string(b), nil
}
