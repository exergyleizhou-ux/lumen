// Package embeddings provides vector embeddings for semantic code search.
// It supports multiple backends (OpenAI-compatible, Ollama) and falls back
// to a lightweight local n-gram hash when no API is configured.
//
// Architecture:
//   Provider → Embed(texts) → [][]float32
//   VectorStore → Store(path, vectors) / Search(query, k) → results
package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// ── Provider ─────────────────────────────────────────────────

// Provider embeds text into fixed-size vectors.
type Provider interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Dim() int
	Name() string
}

// ── OpenAI-compatible provider ──────────────────────────────

// OpenAIProvider calls any OpenAI-compatible embeddings endpoint
// (OpenAI, Ollama, DeepSeek, local llama.cpp server, etc.)
type OpenAIProvider struct {
	BaseURL string // e.g. "https://api.openai.com/v1"
	APIKey  string
	Model   string // e.g. "text-embedding-3-small"
	dim     int
	client  *http.Client
}

// NewOpenAI creates an embeddings provider pointing at an OpenAI-compatible API.
// For Ollama: BaseURL="http://localhost:11434/v1", Model="nomic-embed-text"
func NewOpenAI(baseURL, apiKey, model string) *OpenAIProvider {
	return &OpenAIProvider{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		Model:   model,
		dim:     1536, // default; updated after first Embed call
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *OpenAIProvider) Name() string { return "openai:" + p.Model }

func (p *OpenAIProvider) Dim() int { return p.dim }

func (p *OpenAIProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	reqBody := map[string]any{
		"model": p.Model,
		"input": texts,
	}
	body, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, "POST", p.BaseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embeddings: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		var errBody bytes.Buffer
		errBody.ReadFrom(resp.Body)
		return nil, fmt.Errorf("embeddings: HTTP %d: %s", resp.StatusCode, errBody.String())
	}

	var result struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("embeddings: decode: %w", err)
	}

	vecs := make([][]float32, len(result.Data))
	for i, d := range result.Data {
		vecs[i] = d.Embedding
		if len(d.Embedding) > 0 {
			p.dim = len(d.Embedding)
		}
	}
	return vecs, nil
}

// ── Local n-gram fallback ────────────────────────────────────

// LocalProvider is a zero-dependency fallback that hashes n-grams into a
// sparse vector. Good enough for code search when no embeddings API is
// available. Much better than BM25 alone.
type LocalProvider struct {
	dim int
}

// NewLocal creates a local n-gram hash embeddings provider.
// dim controls vector size (higher = more precision, more memory).
func NewLocal(dim int) *LocalProvider {
	if dim <= 0 {
		dim = 768
	}
	return &LocalProvider{dim: dim}
}

func (p *LocalProvider) Name() string { return "local:ngram" }
func (p *LocalProvider) Dim() int     { return p.dim }

func (p *LocalProvider) Embed(_ context.Context, texts []string) ([][]float32, error) {
	vecs := make([][]float32, len(texts))
	for i, text := range texts {
		vecs[i] = ngramEmbed(text, p.dim)
	}
	return vecs, nil
}

// ngramEmbed hashes character trigrams into a fixed-size unit vector.
func ngramEmbed(text string, dim int) []float32 {
	vec := make([]float32, dim)
	text = strings.ToLower(text)
	var count int
	for i := 0; i+3 <= len(text); i++ {
		h := hash3(text[i : i+3])
		vec[h%dim] += 1.0
		count++
	}
	// Also hash bigrams and whole words for better precision
	for i := 0; i+2 <= len(text); i++ {
		h := hash3(text[i:i+2] + " ")
		vec[h%dim] += 0.5
	}
	words := strings.Fields(text)
	for _, w := range words {
		if len(w) > 2 {
			h := hash3(w)
			vec[h%dim] += 2.0
		}
	}
	// Normalize to unit vector
	var norm float32
	for _, v := range vec {
		norm += v * v
	}
	if norm > 0 {
		norm = float32(math.Sqrt(float64(norm)))
		for i := range vec {
			vec[i] /= norm
		}
	}
	return vec
}

func hash3(s string) int {
	h := 0
	for i := 0; i < len(s); i++ {
		h = h*31 + int(s[i])
	}
	if h < 0 {
		h = -h
	}
	return h
}

// ── Vector Store ─────────────────────────────────────────────

// DocVector pairs a document path with its embedding.
type DocVector struct {
	Path   string    `json:"path"`
	Lines  []int     `json:"lines"`  // starting line numbers
	Vec    []float32 `json:"vec"`
}

// ChunkVector is a single searchable chunk from a file.
type ChunkVector struct {
	Path  string    `json:"p"`
	Text  string    `json:"t"` // first 200 chars for display
	Vec   []float32 `json:"v"`
	Line  int       `json:"l"`
}

// SearchResult is a ranked search hit.
type SearchResult struct {
	Path    string  `json:"path"`
	Line    int     `json:"line"`
	Text    string  `json:"text"`
	Score   float32 `json:"score"`
}

// Store holds indexed vectors and provides cosine similarity search.
type Store struct {
	mu     sync.RWMutex
	Chunks []ChunkVector `json:"chunks"`
	dim    int
	ready  bool
}

// NewStore creates an empty vector store.
func NewStore() *Store {
	return &Store{}
}

// Index replaces the store contents with new chunk vectors.
func (s *Store) Index(chunks []ChunkVector) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Chunks = chunks
	if len(chunks) > 0 {
		s.dim = len(chunks[0].Vec)
	}
	s.ready = true
}

// Search returns top-k results by cosine similarity.
func (s *Store) Search(queryVec []float32, k int) []SearchResult {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.ready || len(s.Chunks) == 0 {
		return nil
	}

	type scored struct {
		idx   int
		score float32
	}
	scores := make([]scored, len(s.Chunks))

	for i, ch := range s.Chunks {
		scores[i] = scored{i, cosine(queryVec, ch.Vec)}
	}

	sort.Slice(scores, func(i, j int) bool {
		return scores[i].score > scores[j].score
	})

	if k > len(scores) {
		k = len(scores)
	}
	results := make([]SearchResult, 0, k)
	for i := 0; i < k && scores[i].score > 0.1; i++ {
		ch := s.Chunks[scores[i].idx]
		text := ch.Text
		if len(text) > 200 {
			text = text[:197] + "..."
		}
		results = append(results, SearchResult{
			Path:  ch.Path,
			Line:  ch.Line,
			Text:  text,
			Score: float32(math.Round(float64(scores[i].score)*10000) / 10000),
		})
	}
	return results
}

func cosine(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0
	}
	var dot, na, nb float32
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / float32(math.Sqrt(float64(na)*float64(nb)))
}
