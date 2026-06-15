// Package asset implements an in-memory asset store with content addressing,
// tag-based lookup, compression, and deduplication.
package asset

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"
)

// Asset represents a stored asset with metadata.
type Asset struct {
	ID          string            `json:"id"`          // Content-addressable ID (SHA-256).
	Name        string            `json:"name"`        // Optional human-readable name.
	Tags        []string          `json:"tags"`        // Lookup tags.
	ContentType string            `json:"content_type"` // MIME type.
	Size        int64             `json:"size"`        // Original size in bytes.
	Compressed  bool              `json:"compressed"`  // Whether stored compressed.
	StoredSize  int64             `json:"stored_size"` // Size on disk/memory.
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	Meta        map[string]string `json:"meta"` // Arbitrary metadata.
}

// Store is an in-memory content-addressed asset store with deduplication.
type Store struct {
	mu       sync.RWMutex
	assets   map[string]*Asset   // id -> asset
	data     map[string][]byte   // id -> raw data
	tagIndex map[string]map[string]bool // tag -> set of asset IDs
	nameIndex map[string]string  // name -> id
	totalOriginal int64
	totalStored   int64
	dedupSaved    int64
}

// NewStore creates an empty asset store.
func NewStore() *Store {
	return &Store{
		assets:    make(map[string]*Asset),
		data:      make(map[string][]byte),
		tagIndex:  make(map[string]map[string]bool),
		nameIndex: make(map[string]string),
	}
}

// computeID computes the content-addressable ID from raw bytes.
func computeID(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// Put stores an asset. If the same content already exists (dedup), returns the existing asset.
func (s *Store) Put(name string, data []byte, contentType string, tags []string, compress bool) (*Asset, error) {
	id := computeID(data)

	s.mu.Lock()
	defer s.mu.Unlock()

	// Deduplication: if content already exists, return existing.
	if existing, ok := s.assets[id]; ok {
		s.dedupSaved += int64(len(data))
		// Add new tags.
		for _, t := range tags {
			if existing.hasTag(t) {
				continue
			}
			existing.Tags = append(existing.Tags, t)
			if s.tagIndex[t] == nil {
				s.tagIndex[t] = make(map[string]bool)
			}
			s.tagIndex[t][id] = true
		}
		// Update name mapping if new name.
		if name != "" && existing.Name != name {
			existing.Name = name
			s.nameIndex[name] = id
		}
		return existing, nil
	}

	storedData := data
	storedSize := int64(len(data))
	compressed := false
	if compress && len(data) > 256 {
		var buf bytes.Buffer
		w, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
		if err == nil {
			if _, err := w.Write(data); err == nil {
				if err := w.Close(); err == nil {
					if buf.Len() < len(data) {
						storedData = buf.Bytes()
						storedSize = int64(len(storedData))
						compressed = true
					}
				}
			}
		}
	}

	now := time.Now()
	a := &Asset{
		ID:          id,
		Name:        name,
		Tags:        tags,
		ContentType: contentType,
		Size:        int64(len(data)),
		Compressed:  compressed,
		StoredSize:  storedSize,
		CreatedAt:   now,
		UpdatedAt:   now,
		Meta:        make(map[string]string),
	}

	s.assets[id] = a
	s.data[id] = storedData
	s.totalOriginal += int64(len(data))
	s.totalStored += storedSize

	for _, t := range tags {
		if s.tagIndex[t] == nil {
			s.tagIndex[t] = make(map[string]bool)
		}
		s.tagIndex[t][id] = true
	}
	if name != "" {
		s.nameIndex[name] = id
	}
	return a, nil
}

func (a *Asset) hasTag(tag string) bool {
	for _, t := range a.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

// Get retrieves asset data by ID, decompressing if needed.
func (s *Store) Get(id string) ([]byte, *Asset, error) {
	s.mu.RLock()
	a, ok := s.assets[id]
	d, ok2 := s.data[id]
	s.mu.RUnlock()
	if !ok || !ok2 {
		return nil, nil, fmt.Errorf("asset %q not found", id)
	}
	if !a.Compressed {
		return d, a, nil
	}
	// Decompress.
	r, err := gzip.NewReader(bytes.NewReader(d))
	if err != nil {
		return nil, nil, fmt.Errorf("decompress: %w", err)
	}
	defer r.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		return nil, nil, fmt.Errorf("decompress read: %w", err)
	}
	return out, a, nil
}

// GetByName retrieves an asset by name.
func (s *Store) GetByName(name string) ([]byte, *Asset, error) {
	s.mu.RLock()
	id, ok := s.nameIndex[name]
	s.mu.RUnlock()
	if !ok {
		return nil, nil, fmt.Errorf("asset %q not found", name)
	}
	return s.Get(id)
}

// ListByTag returns all assets matching a given tag.
func (s *Store) ListByTag(tag string) []*Asset {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := s.tagIndex[tag]
	if len(ids) == 0 {
		return nil
	}
	out := make([]*Asset, 0, len(ids))
	for id := range ids {
		if a, ok := s.assets[id]; ok {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// ListByTags returns assets matching ALL given tags (AND).
func (s *Store) ListByTags(tags []string) []*Asset {
	if len(tags) == 0 {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Start with first tag's set.
	candidates := make(map[string]bool)
	if set, ok := s.tagIndex[tags[0]]; ok {
		for id := range set {
			candidates[id] = true
		}
	}
	// Intersect.
	for _, tag := range tags[1:] {
		set, ok := s.tagIndex[tag]
		if !ok {
			return nil
		}
		for id := range candidates {
			if !set[id] {
				delete(candidates, id)
			}
		}
	}
	out := make([]*Asset, 0, len(candidates))
	for id := range candidates {
		if a, ok := s.assets[id]; ok {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// Delete removes an asset by ID.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.assets[id]
	if !ok {
		return fmt.Errorf("asset %q not found", id)
	}
	// Remove from tag index.
	for _, t := range a.Tags {
		if set, ok := s.tagIndex[t]; ok {
			delete(set, id)
			if len(set) == 0 {
				delete(s.tagIndex, t)
			}
		}
	}
	// Remove from name index.
	if a.Name != "" {
		delete(s.nameIndex, a.Name)
	}
	s.totalStored -= a.StoredSize
	s.totalOriginal -= a.Size
	delete(s.assets, id)
	delete(s.data, id)
	return nil
}

// AddTags adds tags to an existing asset.
func (s *Store) AddTags(id string, tags []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.assets[id]
	if !ok {
		return fmt.Errorf("asset %q not found", id)
	}
	for _, t := range tags {
		if !a.hasTag(t) {
			a.Tags = append(a.Tags, t)
			if s.tagIndex[t] == nil {
				s.tagIndex[t] = make(map[string]bool)
			}
			s.tagIndex[t][id] = true
		}
	}
	a.UpdatedAt = time.Now()
	return nil
}

// RemoveTags removes tags from an asset.
func (s *Store) RemoveTags(id string, tags []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.assets[id]
	if !ok {
		return fmt.Errorf("asset %q not found", id)
	}
	removeSet := make(map[string]bool)
	for _, t := range tags {
		removeSet[t] = true
	}
	filtered := a.Tags[:0]
	for _, t := range a.Tags {
		if !removeSet[t] {
			filtered = append(filtered, t)
		} else {
			if set, ok := s.tagIndex[t]; ok {
				delete(set, id)
				if len(set) == 0 {
					delete(s.tagIndex, t)
				}
			}
		}
	}
	a.Tags = filtered
	a.UpdatedAt = time.Now()
	return nil
}

// List returns all assets.
func (s *Store) List() []*Asset {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Asset, 0, len(s.assets))
	for _, a := range s.assets {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// Stats returns storage statistics.
func (s *Store) Stats() map[string]int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]int64{
		"total_assets":   int64(len(s.assets)),
		"total_tags":     int64(len(s.tagIndex)),
		"total_original": s.totalOriginal,
		"total_stored":   s.totalStored,
		"dedup_saved":    s.dedupSaved,
	}
}

// FormatStore returns a human-readable listing of the store.
func (s *Store) FormatStore() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st := s.Stats()
	out := fmt.Sprintf("Asset Store: %d assets, %d tags, original=%d stored=%d saved=%d\n",
		st["total_assets"], st["total_tags"], st["total_original"], st["total_stored"], st["dedup_saved"])
	for _, a := range s.assets {
		out += fmt.Sprintf("  %s name=%q tags=%v size=%d compressed=%v\n",
			a.ID[:12], a.Name, a.Tags, a.Size, a.Compressed)
	}
	return out
}
