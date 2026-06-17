// Package codesearch: add vector search to existing BM25 index.
package codesearch

import (
	"context"
	"strings"
	"sync"
	"time"

	"lumen/internal/embeddings"
)

// SearchService combines BM25 index with optional vector search.
type SearchService struct {
	idx        *Index
	vecStore   *embeddings.Store
	embProvider embeddings.Provider
	mu         sync.RWMutex
	indexing   bool
}

// NewSearchService creates a search service with optional embeddings.
// Pass nil for embProvider to use BM25-only.
func NewSearchService(idx *Index, emb embeddings.Provider) *SearchService {
	return &SearchService{
		idx:         idx,
		vecStore:    embeddings.NewStore(),
		embProvider: emb,
	}
}

// IndexDir indexes a directory with both BM25 and optional vector embeddings.
func (s *SearchService) IndexDir(dir string) error {
	s.mu.Lock()
	s.indexing = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.indexing = false; s.mu.Unlock() }()

	// BM25 index (fast, always available)
	if err := s.idx.indexDir(dir); err != nil {
		return err
	}

	// Vector index (optional, may be slow for large codebases)
	if s.embProvider != nil {
		if err := s.buildVectorIndex(dir); err != nil {
			// Non-fatal: BM25 still works
			_ = err
		}
	}

	return nil
}

// Search runs a hybrid search: vector + BM25.
func (s *SearchService) Search(query string, k int) []Result {
	// BM25 first (fast, always works)
	bm25Results := s.idx.search(query, k*2)

	// Vector search (if available)
	if s.embProvider != nil && s.vecStore != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		vecs, err := s.embProvider.Embed(ctx, []string{query})
		if err == nil && len(vecs) > 0 {
			vecResults := s.vecStore.Search(vecs[0], k)
			// Merge BM25 and vector results
			return mergeResults(bm25Results, vecResults, k)
		}
	}

	// Fallback: BM25 only
	if len(bm25Results) > k {
		bm25Results = bm25Results[:k]
	}
	return bm25Results
}

// ── Vector index builder ─────────────────────────────────────

func (s *SearchService) buildVectorIndex(dir string) error {
	// Walk files, chunk them, embed chunks in batches
	var chunks []embeddings.ChunkVector

	err := walkDir(dir, 3000, defaultExts(), defaultSkipDirs(), func(path string) error {
		lines, err := readLines(path)
		if err != nil || len(lines) == 0 {
			return err
		}

		for _, ch := range chunkFile(lines) {
			if len(strings.TrimSpace(ch.text)) < 20 {
				continue
			}
			// Take first 500 chars for embedding
			text := ch.text
			if len(text) > 500 {
				text = text[:500]
			}
			line := 0
			if len(ch.lines) > 0 {
				// Find first non-empty line
				for i, l := range ch.lines {
					if strings.TrimSpace(l) != "" {
						line = i
						break
					}
				}
			}
			chunks = append(chunks, embeddings.ChunkVector{
				Path: path,
				Text: text,
				Line: line + 1,
			})
		}
		return nil
	})
	if err != nil {
		return err
	}

	if len(chunks) == 0 {
		return nil
	}

	// Embed in batches of 20
	batchSize := 20
	for i := 0; i < len(chunks); i += batchSize {
		end := i + batchSize
		if end > len(chunks) {
			end = len(chunks)
		}
		batch := chunks[i:end]
		texts := make([]string, len(batch))
		for j, ch := range batch {
			texts[j] = ch.Text
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		vecs, err := s.embProvider.Embed(ctx, texts)
		cancel()
		if err != nil {
			return err
		}

		for j, v := range vecs {
			chunks[i+j].Vec = v
		}
	}

	// Store
	s.vecStore.Index(chunks)
	return nil
}

// ── Result merging ───────────────────────────────────────────

func mergeResults(bm25 []Result, vec []embeddings.SearchResult, k int) []Result {
	seen := make(map[string]bool)
	merged := make([]Result, 0, k)

	// Interleave: pick best from each, deduplicate by path:line
	bi, vi := 0, 0
	for len(merged) < k && (bi < len(bm25) || vi < len(vec)) {
		// Take from BM25
		if bi < len(bm25) {
			key := bm25[bi].Path + ":" + itoa(bm25[bi].Line)
			if !seen[key] {
				seen[key] = true
				merged = append(merged, bm25[bi])
			}
			bi++
		}
		// Take from vector
		if vi < len(vec) && len(merged) < k {
			key := vec[vi].Path + ":" + itoa(vec[vi].Line)
			if !seen[key] {
				seen[key] = true
				merged = append(merged, Result{
					Path:    vec[vi].Path,
					Line:    vec[vi].Line,
					Snippet: vec[vi].Text,
					Score:   float64(vec[vi].Score),
				})
			}
			vi++
		}
	}

	return merged
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

// ── Helpers ──────────────────────────────────────────────────

func defaultExts() map[string]bool {
	return map[string]bool{
		".go": true, ".py": true, ".js": true, ".ts": true, ".jsx": true, ".tsx": true,
		".rs": true, ".c": true, ".cpp": true, ".h": true, ".hpp": true,
		".java": true, ".kt": true, ".swift": true, ".rb": true, ".php": true,
		".sh": true, ".sql": true, ".yaml": true, ".yml": true, ".toml": true,
		".json": true, ".xml": true, ".html": true, ".css": true, ".scss": true,
		".md": true, ".txt": true, ".proto": true, ".vue": true, ".svelte": true,
	}
}

func defaultSkipDirs() map[string]bool {
	return map[string]bool{
		"node_modules": true, ".git": true, "vendor": true, "dist": true,
		"build": true, ".next": true, "__pycache__": true, "target": true,
		".venv": true, "venv": true, ".idea": true, ".vscode": true,
	}
}
