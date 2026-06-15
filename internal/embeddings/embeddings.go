// Package embeddings provides text embedding generation for RAG (Retrieval
// Augmented Generation). It supports OpenAI-compatible embedding APIs
// (DeepSeek, OpenAI, etc.) and local embedding models via Ollama.
// The generated vectors can be stored in a vector store for semantic search.
package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Vector is a dense embedding vector.
type Vector []float32

// Client generates embeddings from text.
type Client struct {
	baseURL string
	model   string
	apiKey  string
	client  *http.Client
	mu      sync.Mutex
}

// NewClient creates an embeddings client using the configured provider.
// Uses DEEPSEEK_API_KEY + DEEPSEEK_BASE_URL, or OPENAI_API_KEY.
func NewClient() *Client {
	baseURL := os.Getenv("EMBEDDING_BASE_URL")
	if baseURL == "" {
		baseURL = os.Getenv("DEEPSEEK_BASE_URL")
	}
	if baseURL == "" {
		baseURL = "https://api.deepseek.com"
	}
	model := os.Getenv("EMBEDDING_MODEL")
	if model == "" {
		model = "text-embedding-3-small"
	}
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}

	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		apiKey:  apiKey,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// Embed generates an embedding vector for a single text.
func (c *Client) Embed(ctx context.Context, text string) (Vector, error) {
	vecs, err := c.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}
	return vecs[0], nil
}

// EmbedBatch generates embeddings for multiple texts in one API call.
func (c *Client) EmbedBatch(ctx context.Context, texts []string) ([]Vector, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	body := map[string]any{
		"model": c.model,
		"input": texts,
	}
	bodyJSON, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/embeddings", bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("embeddings request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embeddings call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("embeddings HTTP %d: %s", resp.StatusCode, b)
	}

	var result struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("embeddings decode: %w", err)
	}

	vecs := make([]Vector, len(result.Data))
	for i, d := range result.Data {
		vecs[i] = d.Embedding
	}
	return vecs, nil
}

// ── Cosine similarity ──────────────────────────────────────

// CosineSimilarity computes the cosine similarity between two vectors.
func CosineSimilarity(a, b Vector) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float32
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	return dot / (sqrt32(normA) * sqrt32(normB))
}

func sqrt32(x float32) float32 {
	if x <= 0 {
		return 0
	}
	// Newton's method
	r := x
	for i := 0; i < 10; i++ {
		r = (r + x/r) / 2
	}
	return float32(r)
}

// TopK returns the indices of the top-K most similar vectors to the query.
func TopK(query Vector, candidates []Vector, k int) []int {
	if k > len(candidates) {
		k = len(candidates)
	}
	type scored struct {
		idx   int
		score float32
	}
	scores := make([]scored, len(candidates))
	for i, c := range candidates {
		scores[i] = scored{idx: i, score: CosineSimilarity(query, c)}
	}
	// Simple insertion sort (fine for small K)
	for i := 1; i < len(scores); i++ {
		for j := i; j > 0 && scores[j].score > scores[j-1].score; j-- {
			scores[j], scores[j-1] = scores[j-1], scores[j]
		}
	}
	out := make([]int, k)
	for i := 0; i < k; i++ {
		out[i] = scores[i].idx
	}
	return out
}

// ── Text chunking for embedding ─────────────────────────────

// Chunk splits text into overlapping chunks suitable for embedding.
func Chunk(text string, chunkSize, overlap int) []string {
	if chunkSize <= 0 {
		chunkSize = 512
	}
	if overlap < 0 {
		overlap = 0
	}
	words := strings.Fields(text)
	if len(words) <= chunkSize {
		return []string{text}
	}

	var chunks []string
	step := chunkSize - overlap
	if step <= 0 {
		step = 1
	}
	for i := 0; i < len(words); i += step {
		end := i + chunkSize
		if end > len(words) {
			end = len(words)
		}
		chunks = append(chunks, strings.Join(words[i:end], " "))
		if end == len(words) {
			break
		}
	}
	return chunks
}
