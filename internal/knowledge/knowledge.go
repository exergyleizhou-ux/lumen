// Package knowledge provides a Retrieval-Augmented Generation (RAG) pipeline
// with document ingestion, chunking, embedding, vector storage, and semantic
// search. It enables the agent to answer questions about project documentation,
// codebases, and session history by retrieving relevant context.
package knowledge

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Document struct {
	ID        string
	Title     string
	Content   string
	Source    string
	Metadata  map[string]any
	Embedding []float32
	Chunks    []Chunk
	CreatedAt time.Time
}
type Chunk struct {
	ID          string
	Content     string
	StartOffset int
	EndOffset   int
	Embedding   []float32
}
type SearchResult struct {
	Document   *Document
	Chunk      *Chunk
	Score      float32
	Highlights []string
}
type Pipeline struct {
	mu           sync.RWMutex
	documents    map[string]*Document
	vectors      map[string][]float32
	stats        PipelineStats
	embedder     func(string) ([]float32, error)
	chunkSize    int
	chunkOverlap int
}
type PipelineStats struct {
	TotalDocuments   int64
	TotalChunks      int64
	TotalTokens      int64
	TotalEmbeddings  int64
	FailedEmbeddings int64
	LastIngestion    time.Time
	mu               sync.Mutex
}

func NewPipeline(embedder func(string) ([]float32, error)) *Pipeline {
	return &Pipeline{documents: map[string]*Document{}, chunkSize: 512, chunkOverlap: 50, embedder: embedder}
}
func (p *Pipeline) SetChunkSize(size, overlap int) { p.chunkSize = size; p.chunkOverlap = overlap }
func (p *Pipeline) Ingest(doc *Document) error {
	id := doc.ID
	if id == "" {
		id = fmt.Sprintf("doc-%d", time.Now().UnixNano())
		doc.ID = id
	}
	if doc.CreatedAt.IsZero() {
		doc.CreatedAt = time.Now()
	}
	if len(doc.Chunks) == 0 {
		chunks := chunkText(doc.Content, p.chunkSize, p.chunkOverlap)
		doc.Chunks = make([]Chunk, len(chunks))
		for i, c := range chunks {
			doc.Chunks[i] = Chunk{ID: fmt.Sprintf("%s-chunk-%d", id, i), Content: c.Text, StartOffset: c.Start, EndOffset: c.End}
		}
	}
	p.mu.Lock()
	p.documents[id] = doc
	if doc.Embedding != nil {
		p.vectors[id] = doc.Embedding
	}
	p.stats.TotalDocuments++
	p.stats.TotalChunks += int64(len(doc.Chunks))
	p.stats.LastIngestion = time.Now()
	p.mu.Unlock()
	if p.embedder != nil && doc.Embedding == nil {
		if emb, err := p.embedder(doc.Content); err == nil {
			doc.Embedding = emb
		} else {
			p.stats.FailedEmbeddings++
		}
	}
	return nil
}
func (p *Pipeline) Search(query string, limit int) ([]*SearchResult, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.embedder == nil {
		return p.searchKeyword(query, limit)
	}
	qEmb, err := p.embedder(query)
	if err != nil {
		return p.searchKeyword(query, limit)
	}
	type scored struct {
		result *SearchResult
		score  float32
	}
	var results []scored
	for _, doc := range p.documents {
		if doc.Embedding != nil {
			sim := cosineSim(qEmb, doc.Embedding)
			if sim > 0.3 {
				results = append(results, scored{&SearchResult{Document: doc, Score: sim, Highlights: extractHighlights(doc.Content, query)}, sim})
			}
		}
		for _, ch := range doc.Chunks {
			if ch.Embedding != nil {
				sim := cosineSim(qEmb, ch.Embedding)
				if sim > 0.3 {
					results = append(results, scored{&SearchResult{Document: doc, Chunk: &ch, Score: sim, Highlights: extractHighlights(ch.Content, query)}, sim})
				}
			}
		}
	}
	sort.Slice(results, func(i, j int) bool { return results[i].score > results[j].score })
	if limit > 0 && limit < len(results) {
		results = results[:limit]
	}
	out := make([]*SearchResult, len(results))
	for i, r := range results {
		out[i] = r.result
	}
	return out, nil
}
func (p *Pipeline) searchKeyword(query string, limit int) ([]*SearchResult, error) {
	query = strings.ToLower(query)
	var results []*SearchResult
	for _, doc := range p.documents {
		if strings.Contains(strings.ToLower(doc.Content), query) {
			score := float32(strings.Count(strings.ToLower(doc.Content), query)) / float32(max(1, len(doc.Content)))
			results = append(results, &SearchResult{Document: doc, Score: score, Highlights: extractHighlights(doc.Content, query)})
		}
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	if limit > 0 && limit < len(results) {
		results = results[:limit]
	}
	return results, nil
}
func (p *Pipeline) Get(id string) *Document {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.documents[id]
}
func (p *Pipeline) Delete(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.documents, id)
	delete(p.vectors, id)
}
func (p *Pipeline) Count() int           { p.mu.RLock(); defer p.mu.RUnlock(); return len(p.documents) }
func (p *Pipeline) Stats() PipelineStats { return p.stats }

type chunkResult struct {
	Text  string
	Start int
	End   int
}

func chunkText(text string, size, overlap int) []chunkResult {
	words := strings.Fields(text)
	if len(words) <= size {
		return []chunkResult{{Text: text, Start: 0, End: len(text)}}
	}
	var chunks []chunkResult
	step := size - overlap
	if step <= 0 {
		step = 1
	}
	idx := 0
	for i := 0; i < len(words); i += step {
		end := i + size
		if end > len(words) {
			end = len(words)
		}
		chunk := strings.Join(words[i:end], " ")
		startOffset := idx
		for j := 0; j < i && idx < len(text); j++ {
			idx += len(words[j]) + 1
		}
		chunks = append(chunks, chunkResult{Text: chunk, Start: startOffset, End: startOffset + len(chunk)})
		if end == len(words) {
			break
		}
	}
	return chunks
}
func cosineSim(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
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
	return dot / float32(math.Sqrt(float64(na))*math.Sqrt(float64(nb)))
}
func extractHighlights(text, query string) []string {
	query = strings.ToLower(query)
	textLower := strings.ToLower(text)
	idx := strings.Index(textLower, query)
	if idx < 0 {
		return nil
	}
	start := idx - 40
	if start < 0 {
		start = 0
	}
	end := idx + len(query) + 40
	if end > len(text) {
		end = len(text)
	}
	snippet := text[start:end]
	if start > 0 {
		snippet = "..." + snippet
	}
	if end < len(text) {
		snippet += "..."
	}
	return []string{snippet}
}
func FormatSearchResults(results []*SearchResult) string {
	if len(results) == 0 {
		return "No results found.\n"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d result(s):\n\n", len(results))
	for i, r := range results {
		title := r.Document.Title
		if title == "" {
			title = r.Document.ID
		}
		fmt.Fprintf(&sb, "%d. %s (score: %.2f)\n", i+1, title, r.Score)
		if len(r.Highlights) > 0 {
			fmt.Fprintf(&sb, "   %s\n\n", r.Highlights[0])
		}
	}
	return sb.String()
}
func (p *Pipeline) IngestDirectory(dir string, extensions []string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		matched := len(extensions) == 0
		for _, ext := range extensions {
			if strings.HasSuffix(path, ext) {
				matched = true
				break
			}
		}
		if !matched {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		if rel == "" {
			rel = path
		}
		doc := &Document{ID: fmt.Sprintf("file-%s", strings.ReplaceAll(rel, "/", "-")), Title: rel, Content: string(data), Source: rel}
		return p.Ingest(doc)
	})
}

type mockEmbedder struct{ calls int }
