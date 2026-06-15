// Package vectorstore provides a simple in-memory vector database for
// semantic search. It stores embedding vectors and their associated
// metadata, supports CRUD operations, and provides cosine-similarity
// nearest-neighbor search. Used for RAG (Retrieval Augmented Generation)
// when combined with the embeddings package.
package vectorstore

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

// Entry is one stored vector with metadata.
type Entry struct {
	ID        string         `json:"id"`
	Vector    []float32      `json:"vector"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	Content   string         `json:"content,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// Store is a thread-safe in-memory vector store.
type Store struct {
	mu      sync.RWMutex
	entries map[string]*Entry
	dim     int
}

// NewStore creates a vector store expecting vectors of the given dimension.
func NewStore(dim int) *Store {
	return &Store{entries: map[string]*Entry{}, dim: dim}
}

// Insert adds or updates an entry.
func (s *Store) Insert(id string, vec []float32, content string, meta map[string]any) error {
	if len(vec) != s.dim && s.dim > 0 {
		s.dim = len(vec)
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.entries[id]; ok {
		existing.Vector = vec
		existing.Content = content
		existing.Metadata = meta
		existing.UpdatedAt = now
		return nil
	}
	s.entries[id] = &Entry{
		ID: id, Vector: vec, Content: content, Metadata: meta,
		CreatedAt: now, UpdatedAt: now,
	}
	return nil
}

// Get returns an entry by ID.
func (s *Store) Get(id string) (*Entry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entries[id]
	return e, ok
}

// Delete removes an entry.
func (s *Store) Delete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.entries[id]
	delete(s.entries, id)
	return ok
}

// Size returns the number of stored vectors.
func (s *Store) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries)
}

// List returns all entries.
func (s *Store) List() []*Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Entry, 0, len(s.entries))
	for _, e := range s.entries {
		out = append(out, e)
	}
	return out
}

// Search returns the K nearest neighbors to the query vector.
func (s *Store) Search(query []float32, k int) []*Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	type scored struct {
		entry *Entry
		score float32
	}

	var results []scored
	for _, e := range s.entries {
		sim := cosineSimilarity(query, e.Vector)
		results = append(results, scored{entry: e, score: sim})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	if k > len(results) {
		k = len(results)
	}

	out := make([]*Entry, k)
	for i := 0; i < k; i++ {
		out[i] = results[i].entry
	}
	return out
}

// SearchWithThreshold returns neighbors above a similarity threshold.
func (s *Store) SearchWithThreshold(query []float32, k int, threshold float32) []*Entry {
	all := s.Search(query, k)
	var filtered []*Entry
	for _, e := range all {
		if cosineSimilarity(query, e.Vector) >= threshold {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float32
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (sqrt32(normA) * sqrt32(normB))
}

func sqrt32(x float32) float32 {
	return float32(math.Sqrt(float64(x)))
}

// Clear removes all entries.
func (s *Store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = map[string]*Entry{}
}

// Backup returns a snapshot of all entries for persistence.
func (s *Store) Backup() []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Entry, 0, len(s.entries))
	for _, e := range s.entries {
		out = append(out, *e)
	}
	return out
}

// Restore loads entries from a backup.
func (s *Store) Restore(entries []Entry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = map[string]*Entry{}
	for i := range entries {
		e := entries[i]
		s.entries[e.ID] = &e
	}
}

// Stats returns summary statistics.
type Stats struct {
	Count     int     `json:"count"`
	Dimension int     `json:"dimension"`
	AvgSim    float32 `json:"avg_similarity,omitempty"`
}

func (s *Store) StatsReport() Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	stats := Stats{Count: len(s.entries), Dimension: s.dim}
	if len(s.entries) < 2 {
		return stats
	}
	var total float32
	var pairs int
	entries := make([]*Entry, 0, len(s.entries))
	for _, e := range s.entries {
		entries = append(entries, e)
	}
	for i := 0; i < len(entries) && i < 100; i++ {
		for j := i + 1; j < len(entries) && j < 100; j++ {
			total += cosineSimilarity(entries[i].Vector, entries[j].Vector)
			pairs++
		}
	}
	if pairs > 0 {
		stats.AvgSim = total / float32(pairs)
	}
	return stats
}

// FormatResults formats search results for display.
func FormatResults(results []*Entry) string {
	if len(results) == 0 {
		return "No results found.\n"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d result(s):\n\n", len(results))
	for i, e := range results {
		score := float32(0)
		fmt.Fprintf(&sb, "%d. %s", i+1, e.ID)
		if e.Content != "" {
			if len(e.Content) > 80 {
				fmt.Fprintf(&sb, " — %s...\n", e.Content[:80])
			} else {
				fmt.Fprintf(&sb, " — %s\n", e.Content)
			}
		} else {
			sb.WriteByte('\n')
		}
		_ = score
	}
	return sb.String()
}
