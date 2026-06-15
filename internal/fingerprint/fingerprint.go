// Package fingerprint generates content fingerprints using SHA-256
// and minhash algorithms for duplicate detection and integrity checking.
package fingerprint

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"sync"
)

type Fingerprint struct {
	Hash      string
	Algorithm string
	Size      int64
}

func SHA256(data []byte) *Fingerprint {
	h := sha256.Sum256(data)
	return &Fingerprint{Hash: hex.EncodeToString(h[:]), Algorithm: "sha256", Size: int64(len(data))}
}
func MinHash(data []byte, numHashes int) []uint64 {
	if numHashes <= 0 {
		numHashes = 128
	}
	out := make([]uint64, numHashes)
	h := fnv.New64a()
	h.Write(data)
	seed := h.Sum64()
	for i := 0; i < numHashes; i++ {
		h2 := fnv.New64a()
		h2.Write([]byte(fmt.Sprintf("%d:%d", seed, i)))
		out[i] = h2.Sum64()
	}
	return out
}
func MinHashSimilarity(a, b []uint64) float64 {
	if len(a) != len(b) {
		return 0
	}
	match := 0
	for i := range a {
		if a[i] == b[i] {
			match++
		}
	}
	return float64(match) / float64(len(a))
}

type Registry struct {
	mu   sync.Mutex
	seen map[string]*Fingerprint
}

func NewRegistry() *Registry { return &Registry{seen: map[string]*Fingerprint{}} }
func (r *Registry) Add(id string, fp *Fingerprint) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.seen[fp.Hash]; ok {
		_ = existing
		return false
	}
	r.seen[fp.Hash] = fp
	return true
}
func (r *Registry) Seen(hash string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.seen[hash]
	return ok
}
func (r *Registry) Count() int { r.mu.Lock(); defer r.mu.Unlock(); return len(r.seen) }
func (r *Registry) FormatRegistry() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var sb strings.Builder
	fmt.Fprintf(&sb, "Fingerprint Registry (%d):\n%s\n\n", len(r.seen), strings.Repeat("─", 50))
	hashes := make([]string, 0, len(r.seen))
	for h := range r.seen {
		hashes = append(hashes, h)
	}
	sort.Strings(hashes)
	for _, h := range hashes {
		fp := r.seen[h]
		fmt.Fprintf(&sb, "  %s [%s] %d bytes\n", fp.Hash[:16], fp.Algorithm, fp.Size)
	}
	return sb.String()
}
