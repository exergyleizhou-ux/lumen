// Package codesearch provides a local, offline code search engine.
// It builds an inverted TF-IDF index from a directory tree and answers
// natural-language queries with ranked code snippets. No embeddings API,
// no vector DB — pure Go BM25 + TF-IDF.
package codesearch

import (
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"
)

// Index holds the inverted index and document store.
type Index struct {
	mu     sync.RWMutex
	docs   []document       // indexed documents
	inv    map[string][]hit // token → posting list
	avgDL  float64          // average document length (in tokens)
	docN   int              // total documents
}

type document struct {
	path    string // file path, relative to root
	lines   []string
	tokens  []string // pre-tokenized
	tf      map[string]float64
	len     int // token count
}

type hit struct {
	docID int
	tf    float64
	pos   []int
}

// Result is a single search hit.
type Result struct {
	Path    string  `json:"path"`
	Line    int     `json:"line"`    // 1-based
	Snippet string  `json:"snippet"` // ~5 lines around the match
	Score   float64 `json:"score"`
}

// ── Public API ────────────────────────────────────────────────

// New creates an empty index.
func New() *Index {
	return &Index{inv: make(map[string][]hit)}
}

// IndexDir walks dir recursively and indexes all text-like files.
func (idx *Index) IndexDir(dir string) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	return idx.indexDir(dir)
}

// Search runs a natural-language query and returns top-k results.
func (idx *Index) Search(query string, k int) []Result {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.search(query, k)
}

// Stats returns index statistics.
func (idx *Index) Stats() map[string]any {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return map[string]any{
		"documents": idx.docN,
		"tokens":    len(idx.inv),
		"avg_len":   idx.avgDL,
	}
}

// ── Tokenizer ─────────────────────────────────────────────────

var (
	tokenRE   = regexp.MustCompile(`[a-zA-Z_][a-zA-Z0-9_]*`)
	stopWords = map[string]bool{
		"a": true, "an": true, "the": true, "is": true, "are": true,
		"was": true, "were": true, "be": true, "been": true, "being": true,
		"have": true, "has": true, "had": true, "do": true, "does": true,
		"did": true, "will": true, "would": true, "could": true, "should": true,
		"may": true, "might": true, "can": true, "shall": true, "must": true,
		"i": true, "me": true, "my": true, "we": true, "our": true, "you": true,
		"your": true, "he": true, "she": true, "it": true, "its": true,
		"they": true, "them": true, "this": true, "that": true, "these": true,
		"those": true, "and": true, "or": true, "not": true, "but": true,
		"if": true, "then": true, "else": true, "for": true, "with": true,
		"from": true, "to": true, "in": true, "on": true, "at": true,
		"by": true, "of": true, "as": true, "so": true, "all": true,
		"no": true, "yes": true, "just": true, "only": true, "also": true,
		"very": true, "too": true, "here": true, "there": true, "when": true,
		"where": true, "what": true, "which": true, "who": true, "how": true,
	}
)

func tokenize(text string) []string {
	matches := tokenRE.FindAllString(strings.ToLower(text), -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		if len(m) < 2 || stopWords[m] {
			continue
		}
		out = append(out, stem(m))
	}
	return out
}

// stem applies a simple suffix-stripping stemmer (Porter-lite).
func stem(word string) string {
	if len(word) <= 4 {
		return word
	}
	// Common programming suffixes
	for _, s := range []string{"ing", "tion", "ment", "ness", "able", "ible", "ical", "ally", "fully"} {
		if strings.HasSuffix(word, s) && len(word)-len(s) >= 3 {
			return word[:len(word)-len(s)]
		}
	}
	// Plural
	if strings.HasSuffix(word, "s") && !strings.HasSuffix(word, "ss") && len(word) > 3 {
		word = word[:len(word)-1]
	}
	return word
}

// ── TF-IDF ────────────────────────────────────────────────────

const (
	k1 = 1.2 // BM25 term saturation
	b  = 0.75 // BM25 length normalization
)

func (idx *Index) bm25Score(tf, docLen float64, df int) float64 {
	idf := math.Log(1 + (float64(idx.docN)-float64(df)+0.5)/(float64(df)+0.5))
	num := tf * (k1 + 1)
	den := tf + k1*(1-b+b*docLen/idx.avgDL)
	return idf * num / den
}

// ── Indexing ──────────────────────────────────────────────────

func (idx *Index) indexDir(dir string) error {
	// Fast walk: only index files with known text extensions
	exts := map[string]bool{
		".go": true, ".py": true, ".js": true, ".ts": true, ".jsx": true, ".tsx": true,
		".rs": true, ".c": true, ".cpp": true, ".h": true, ".hpp": true,
		".java": true, ".kt": true, ".swift": true, ".rb": true, ".php": true,
		".sh": true, ".bash": true, ".zsh": true, ".sql": true, ".yaml": true,
		".yml": true, ".toml": true, ".json": true, ".xml": true, ".html": true,
		".css": true, ".scss": true, ".md": true, ".txt": true, ".proto": true,
		".vue": true, ".svelte": true, ".graphql": true, ".tf": true,
		".Makefile": true, "Makefile": true, "Dockerfile": true,
	}

	// Walk limited to 5000 files, skip common dirs
	skipDirs := map[string]bool{
		"node_modules": true, ".git": true, "vendor": true, "dist": true,
		"build": true, ".next": true, "__pycache__": true, "target": true,
		".venv": true, "venv": true, ".idea": true, ".vscode": true,
	}
	return walkDir(dir, 5000, exts, skipDirs, func(path string) error {
		return idx.indexFile(path)
	})
}

func (idx *Index) indexFile(path string) error {
	lines, err := readLines(path)
	if err != nil || len(lines) == 0 {
		return err
	}

	// Chunk at function/class boundaries or double newlines
	for _, chunk := range chunkFile(lines) {
		if len(chunk.lines) == 0 {
			continue
		}
		tokens := tokenize(chunk.text)
		if len(tokens) == 0 {
			continue
		}

		doc := document{
			path:   chunk.path,
			lines:  chunk.lines,
			tokens: tokens,
			tf:     make(map[string]float64),
			len:    len(tokens),
		}

		// Compute term frequencies
		tc := make(map[string]int)
		for _, t := range tokens {
			tc[t]++
		}
		for t, c := range tc {
			doc.tf[t] = float64(c) / float64(len(tokens))
		}

		docID := len(idx.docs)
		idx.docs = append(idx.docs, doc)

		// Update inverted index
		for t, c := range tc {
			idx.inv[t] = append(idx.inv[t], hit{
				docID: docID,
				tf:    float64(c),
			})
		}
	}

	idx.docN = len(idx.docs)
	if idx.docN > 0 {
		idx.avgDL = 0
		for _, d := range idx.docs {
			idx.avgDL += float64(d.len)
		}
		idx.avgDL /= float64(idx.docN)
	}
	return nil
}

// ── Search ────────────────────────────────────────────────────

func (idx *Index) search(query string, k int) []Result {
	if k <= 0 {
		k = 10
	}
	if idx.docN == 0 {
		return nil
	}

	qTokens := tokenize(query)
	if len(qTokens) == 0 {
		return nil
	}

	// Score every document
	scores := make([]float64, idx.docN)
	for _, qt := range qTokens {
		postings := idx.inv[qt]
		if len(postings) == 0 {
			continue
		}
		df := len(postings)
		for _, h := range postings {
			scores[h.docID] += idx.bm25Score(h.tf, float64(idx.docs[h.docID].len), df)
		}
	}

	// Rank by score
	type scored struct {
		id    int
		score float64
	}
	ranked := make([]scored, 0, idx.docN)
	for i, s := range scores {
		if s > 0 {
			ranked = append(ranked, scored{i, s})
		}
	}
	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].score > ranked[j].score
	})

	// Build results with snippets
	results := make([]Result, 0, k)
	for i := 0; i < len(ranked) && i < k; i++ {
		r := ranked[i]
		doc := idx.docs[r.id]

		// Find the first line containing a query token
		matchLine := 0
		for li, l := range doc.lines {
			lower := strings.ToLower(l)
			for _, qt := range qTokens {
				if strings.Contains(lower, qt) {
					matchLine = li
					goto found
				}
			}
		}
	found:

		// Extract surrounding lines (~3 lines context)
		start := matchLine - 2
		if start < 0 {
			start = 0
		}
		end := matchLine + 3
		if end > len(doc.lines) {
			end = len(doc.lines)
		}
		snippet := strings.Join(doc.lines[start:end], "\n")

		results = append(results, Result{
			Path:    doc.path,
			Line:    matchLine + 1,
			Snippet: snippet,
			Score:   math.Round(r.score*1000) / 1000,
		})
	}
	return results
}

// ── Chunking ──────────────────────────────────────────────────

type chunk struct {
	path  string
	text  string
	lines []string
}

// Heuristic patterns for function/class/method boundaries.
var chunkBoundary = regexp.MustCompile(`(?m)^(func |fn |def |class |public |private |protected |export |const |let |var |import |package |module |struct |interface |impl |trait |enum |type )`)

func chunkFile(lines []string) []chunk {
	if len(lines) == 0 {
		return nil
	}

	// Single chunk for short files (<50 lines)
	if len(lines) <= 50 {
		return []chunk{{text: strings.Join(lines, "\n"), lines: lines}}
	}

	var chunks []chunk
	start := 0
	for i := 1; i < len(lines); i++ {
		if chunkBoundary.MatchString(strings.TrimSpace(lines[i])) && i-start > 2 {
			c := chunk{
				text:  strings.Join(lines[start:i], "\n"),
				lines: lines[start:i],
			}
			if len(strings.TrimSpace(c.text)) > 10 {
				chunks = append(chunks, c)
			}
			start = i
		}
	}
	// Last chunk
	if start < len(lines) {
		c := chunk{
			text:  strings.Join(lines[start:], "\n"),
			lines: lines[start:],
		}
		if len(strings.TrimSpace(c.text)) > 10 {
			chunks = append(chunks, c)
		}
	}

	if len(chunks) == 0 {
		return []chunk{{text: strings.Join(lines, "\n"), lines: lines}}
	}
	return chunks
}

// ── Helpers ──────────────────────────────────────────────────

func readLines(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return strings.Split(string(data), "\n"), nil
}

func walkDir(dir string, maxFiles int, exts, skipDirs map[string]bool, fn func(string) error) error {
	count := 0
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable files
		}
		if d.IsDir() {
			name := d.Name()
			if name != "." && strings.HasPrefix(name, ".") && name != ".." {
				return filepath.SkipDir
			}
			if skipDirs[name] {
				return filepath.SkipDir
			}
			return nil
		}

		name := d.Name()
		ext := filepath.Ext(name)
		if ext == "" {
			// Check full name match (Makefile, Dockerfile)
			if !exts[name] {
				return nil
			}
		} else if !exts[ext] && !exts[strings.ToLower(ext)] {
			return nil
		}

		if count >= maxFiles {
			return filepath.SkipAll
		}
		count++
		return fn(path)
	})
}

// ── Unicode helpers ──────────────────────────────────────────

func isLetter(r rune) bool {
	return unicode.IsLetter(r) || r == '_'
}
