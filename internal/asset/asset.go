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
	ID          string            `json:"id"`           // Content-addressable ID (SHA-256).
	Name        string            `json:"name"`         // Optional human-readable name.
	Tags        []string          `json:"tags"`         // Lookup tags.
	ContentType string            `json:"content_type"` // MIME type.
	Size        int64             `json:"size"`         // Original size in bytes.
	Compressed  bool              `json:"compressed"`   // Whether stored compressed.
	StoredSize  int64             `json:"stored_size"`  // Size on disk/memory.
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	Meta        map[string]string `json:"meta"` // Arbitrary metadata.
}

// Store is an in-memory content-addressed asset store with deduplication.
type Store struct {
	mu            sync.RWMutex
	assets        map[string]*Asset          // id -> asset
	data          map[string][]byte          // id -> raw data
	tagIndex      map[string]map[string]bool // tag -> set of asset IDs
	nameIndex     map[string]string          // name -> id
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
	return s.statsLocked()
}

// statsLocked assumes the caller holds the lock.
func (s *Store) statsLocked() map[string]int64 {
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
	st := s.statsLocked()
	out := fmt.Sprintf("Asset Store: %d assets, %d tags, original=%d stored=%d saved=%d\n",
		st["total_assets"], st["total_tags"], st["total_original"], st["total_stored"], st["dedup_saved"])
	for _, a := range s.assets {
		out += fmt.Sprintf("  %s name=%q tags=%v size=%d compressed=%v\n",
			a.ID[:12], a.Name, a.Tags, a.Size, a.Compressed)
	}
	return out
}

// --- Content Type Detection ---

// DetectContentType guesses MIME type from the first few bytes.
func DetectContentType(data []byte) string {
	if len(data) == 0 {
		return "application/octet-stream"
	}
	switch {
	case len(data) >= 4 && data[0] == 0x89 && data[1] == 'P' && data[2] == 'N' && data[3] == 'G':
		return "image/png"
	case len(data) >= 2 && data[0] == 0xFF && data[1] == 0xD8:
		return "image/jpeg"
	case len(data) >= 4 && data[0] == 'G' && data[1] == 'I' && data[2] == 'F':
		return "image/gif"
	case len(data) >= 2 && data[0] == '{' && data[len(data)-1] == '}':
		return "application/json"
	case len(data) >= 5 && string(data[:5]) == "<?xml":
		return "application/xml"
	case len(data) >= 6 && string(data[:6]) == "<html>":
		return "text/html"
	case len(data) >= 4 && data[0] == 'P' && data[1] == 'K':
		return "application/zip"
	default:
		if isText(data) {
			return "text/plain"
		}
		return "application/octet-stream"
	}
}

func isText(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return false
		}
	}
	return true
}

// --- Asset Transform ---

// TransformFunc transforms asset data.
type TransformFunc func(data []byte, meta map[string]string) ([]byte, error)

// Transformer applies transforms to assets.
type Transformer struct {
	transforms []namedTransform
}

type namedTransform struct {
	name string
	fn   TransformFunc
}

// NewTransformer creates an asset transformer.
func NewTransformer() *Transformer { return &Transformer{} }

// Register adds a named transform.
func (at *Transformer) Register(name string, fn TransformFunc) {
	at.transforms = append(at.transforms, namedTransform{name: name, fn: fn})
}

// Apply applies all registered transforms to an asset.
func (at *Transformer) Apply(asset *Asset, data []byte) ([]byte, error) {
	var err error
	meta := asset.Meta
	for _, t := range at.transforms {
		data, err = t.fn(data, meta)
		if err != nil {
			return nil, fmt.Errorf("transform %s: %w", t.name, err)
		}
	}
	return data, nil
}

// --- Asset Collection ---

// Collection groups related assets.
type Collection struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	AssetIDs  []string          `json:"asset_ids"`
	CreatedAt time.Time         `json:"created_at"`
	Meta      map[string]string `json:"meta"`
}

// CollectionManager manages asset collections.
type CollectionManager struct {
	mu          sync.RWMutex
	collections map[string]*Collection
	store       *Store
}

// NewCollectionManager creates a collection manager.
func NewCollectionManager(store *Store) *CollectionManager {
	return &CollectionManager{collections: make(map[string]*Collection), store: store}
}

// CreateCollection creates a new collection.
func (cm *CollectionManager) CreateCollection(id, name string, assetIDs []string) *Collection {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	c := &Collection{
		ID: id, Name: name, AssetIDs: assetIDs,
		CreatedAt: time.Now(), Meta: make(map[string]string),
	}
	cm.collections[id] = c
	return c
}

// GetCollection returns a collection by ID.
func (cm *CollectionManager) GetCollection(id string) *Collection {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.collections[id]
}

// AddToCollection adds an asset to a collection.
func (cm *CollectionManager) AddToCollection(collectionID, assetID string) bool {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	c, ok := cm.collections[collectionID]
	if !ok {
		return false
	}
	for _, id := range c.AssetIDs {
		if id == assetID {
			return true
		}
	}
	c.AssetIDs = append(c.AssetIDs, assetID)
	return true
}

// GetCollectionAssets returns all assets in a collection.
func (cm *CollectionManager) GetCollectionAssets(collectionID string) []*Asset {
	cm.mu.RLock()
	c, ok := cm.collections[collectionID]
	cm.mu.RUnlock()
	if !ok {
		return nil
	}
	out := make([]*Asset, 0, len(c.AssetIDs))
	for _, id := range c.AssetIDs {
		cm.store.mu.RLock()
		if a, ok := cm.store.assets[id]; ok {
			out = append(out, a)
		}
		cm.store.mu.RUnlock()
	}
	return out
}

// --- Streaming Support ---

// Chunk represents a partial piece of a large asset.
type Chunk struct {
	AssetID string `json:"asset_id"`
	Index   int    `json:"index"`
	Offset  int64  `json:"offset"`
	Size    int    `json:"size"`
	Data    []byte `json:"data"`
	Last    bool   `json:"last"`
}

// ChunkedAsset supports storing large assets in chunks.
type ChunkedAsset struct {
	mu     sync.RWMutex
	chunks map[string][]Chunk // assetID -> chunks
}

// NewChunkedAsset creates a chunked asset store.
func NewChunkedAsset() *ChunkedAsset {
	return &ChunkedAsset{chunks: make(map[string][]Chunk)}
}

// AddChunk adds a chunk.
func (ca *ChunkedAsset) AddChunk(chunk Chunk) {
	ca.mu.Lock()
	defer ca.mu.Unlock()
	ca.chunks[chunk.AssetID] = append(ca.chunks[chunk.AssetID], chunk)
}

// Assemble reconstructs the full asset from chunks.
func (ca *ChunkedAsset) Assemble(assetID string) ([]byte, error) {
	ca.mu.RLock()
	chunks, ok := ca.chunks[assetID]
	ca.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("no chunks for %q", assetID)
	}
	sort.Slice(chunks, func(i, j int) bool { return chunks[i].Index < chunks[j].Index })
	var total int64
	for _, c := range chunks {
		total += int64(len(c.Data))
	}
	out := make([]byte, 0, total)
	for _, c := range chunks {
		out = append(out, c.Data...)
	}
	return out, nil
}

// --- Snapshot / Versioning ---

// Snapshot captures the state of an asset at a point in time.
type Snapshot struct {
	AssetID string    `json:"asset_id"`
	Version int       `json:"version"`
	Data    []byte    `json:"data"`
	TakenAt time.Time `json:"taken_at"`
	Label   string    `json:"label"`
}

// VersionedStore adds snapshot versioning on top of Store.
type VersionedStore struct {
	*Store
	mu        sync.RWMutex
	snapshots map[string][]Snapshot // assetID -> snapshots
}

// NewVersionedStore creates a versioned asset store.
func NewVersionedStore() *VersionedStore {
	return &VersionedStore{Store: NewStore(), snapshots: make(map[string][]Snapshot)}
}

// Snapshot creates a snapshot of the current asset version.
func (vs *VersionedStore) Snapshot(assetID, label string) (*Snapshot, error) {
	data, _, err := vs.Store.Get(assetID)
	if err != nil {
		return nil, err
	}
	vs.mu.Lock()
	defer vs.mu.Unlock()
	snap := Snapshot{
		AssetID: assetID, Data: data, TakenAt: time.Now(), Label: label,
		Version: len(vs.snapshots[assetID]) + 1,
	}
	vs.snapshots[assetID] = append(vs.snapshots[assetID], snap)
	return &snap, nil
}

// GetSnapshot retrieves a specific snapshot.
func (vs *VersionedStore) GetSnapshot(assetID string, version int) (*Snapshot, error) {
	vs.mu.RLock()
	defer vs.mu.RUnlock()
	snaps := vs.snapshots[assetID]
	if version < 1 || version > len(snaps) {
		return nil, fmt.Errorf("version %d not found", version)
	}
	s := snaps[version-1]
	return &s, nil
}

// ListSnapshots returns all snapshots for an asset.
func (vs *VersionedStore) ListSnapshots(assetID string) []Snapshot {
	vs.mu.RLock()
	defer vs.mu.RUnlock()
	out := make([]Snapshot, len(vs.snapshots[assetID]))
	copy(out, vs.snapshots[assetID])
	return out
}

// --- Asset Search ---

// SearchParams defines search criteria for assets.
type SearchParams struct {
	NamePrefix    string    `json:"name_prefix,omitempty"`
	ContentType   string    `json:"content_type,omitempty"`
	Tag           string    `json:"tag,omitempty"`
	MinSize       int64     `json:"min_size,omitempty"`
	MaxSize       int64     `json:"max_size,omitempty"`
	CreatedAfter  time.Time `json:"created_after,omitempty"`
	CreatedBefore time.Time `json:"created_before,omitempty"`
}

// Search finds assets matching the given parameters.
func (s *Store) Search(params SearchParams) []*Asset {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*Asset
	for _, a := range s.assets {
		if params.NamePrefix != "" && (len(a.Name) < len(params.NamePrefix) || a.Name[:len(params.NamePrefix)] != params.NamePrefix) {
			continue
		}
		if params.ContentType != "" && a.ContentType != params.ContentType {
			continue
		}
		if params.Tag != "" && !a.hasTag(params.Tag) {
			continue
		}
		if params.MinSize > 0 && a.Size < params.MinSize {
			continue
		}
		if params.MaxSize > 0 && a.Size > params.MaxSize {
			continue
		}
		if !params.CreatedAfter.IsZero() && a.CreatedAt.Before(params.CreatedAfter) {
			continue
		}
		if !params.CreatedBefore.IsZero() && a.CreatedAt.After(params.CreatedBefore) {
			continue
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// --- Asset Copy / Clone ---

// CopyTo duplicates an asset with a new name.
func (s *Store) CopyTo(id, newName string, newTags []string) (*Asset, error) {
	data, _, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	a, ok := s.assets[id]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("asset %q not found", id)
	}
	return s.Put(newName, data, a.ContentType, newTags, a.Compressed)
}

// --- Integrity Check ---

// VerifyIntegrity checks that all stored assets match their content digests.
func (s *Store) VerifyIntegrity() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var mismatches []string
	for id, a := range s.assets {
		data, ok := s.data[id]
		if !ok {
			mismatches = append(mismatches, fmt.Sprintf("%s: missing data", id))
			continue
		}
		expected := computeID(data)
		if a.ID != expected {
			mismatches = append(mismatches, fmt.Sprintf("%s: digest mismatch", id))
		}
	}
	return mismatches
}

// --- Bulk Import ---

// BulkImport adds multiple assets efficiently.
func (s *Store) BulkImport(items []struct {
	Name        string
	Data        []byte
	ContentType string
	Tags        []string
	Compress    bool
}) ([]*Asset, []error) {
	assets := make([]*Asset, len(items))
	errs := make([]error, len(items))
	for i, item := range items {
		assets[i], errs[i] = s.Put(item.Name, item.Data, item.ContentType, item.Tags, item.Compress)
	}
	return assets, errs
}
