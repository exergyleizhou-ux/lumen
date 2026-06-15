// Package searchengine provides full-text search over a collection of documents
// with BM25 ranking, prefix matching, and result highlighting. Used for searching
// session history, project files, documentation, and memory entries.
package searchengine

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"unicode"
)

// Document is one searchable text document.
type Document struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Content string   `json:"content"`
	Tags    []string `json:"tags,omitempty"`
	Score   float64  `json:"score,omitempty"`
}

// Engine provides BM25-ranked full-text search.
type Engine struct {
	mu          sync.RWMutex
	documents   map[string]*Document
	index       map[string]map[string]int // term → docID → term frequency
	docLengths  map[string]int
	totalLength float64
	k1          float64 // BM25 term saturation
	b           float64 // BM25 length normalization
	stopWords   map[string]bool
}

// NewEngine creates a search engine.
func NewEngine() *Engine {
	return &Engine{
		documents:  map[string]*Document{},
		index:      map[string]map[string]int{},
		docLengths: map[string]int{},
		k1:         1.5,
		b:          0.75,
		stopWords:  defaultStopWords(),
	}
}

func defaultStopWords() map[string]bool {
	return map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true, "was": true, "were": true,
		"be": true, "been": true, "being": true, "have": true, "has": true, "had": true,
		"do": true, "does": true, "did": true, "will": true, "would": true, "shall": true,
		"may": true, "might": true, "can": true, "could": true, "should": true,
		"i": true, "me": true, "my": true, "we": true, "our": true, "you": true, "your": true,
		"he": true, "she": true, "it": true, "they": true, "them": true, "their": true,
		"and": true, "or": true, "but": true, "not": true, "in": true, "on": true, "at": true,
		"to": true, "for": true, "of": true, "with": true, "from": true, "by": true, "as": true,
	}
}

// Index adds or updates a document in the search index.
func (e *Engine) Index(doc *Document) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Remove old index entries
	if old, ok := e.documents[doc.ID]; ok {
		e.removeDocLocked(old)
	}

	e.documents[doc.ID] = doc
	text := doc.Title + " " + doc.Content + " " + strings.Join(doc.Tags, " ")
	tokens := tokenize(text)
	e.docLengths[doc.ID] = len(tokens)
	e.totalLength += float64(len(tokens))

	termFreq := map[string]int{}
	for _, t := range tokens {
		if e.stopWords[t] {
			continue
		}
		termFreq[t]++
	}
	for term, freq := range termFreq {
		if e.index[term] == nil {
			e.index[term] = map[string]int{}
		}
		e.index[term][doc.ID] = freq
	}
}

func (e *Engine) removeDocLocked(doc *Document) {
	for term, docs := range e.index {
		delete(docs, doc.ID)
		if len(docs) == 0 {
			delete(e.index, term)
		}
	}
	e.totalLength -= float64(e.docLengths[doc.ID])
	delete(e.docLengths, doc.ID)
	delete(e.documents, doc.ID)
}

// Remove deletes a document from the index.
func (e *Engine) Remove(docID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if doc, ok := e.documents[docID]; ok {
		e.removeDocLocked(doc)
	}
}

// Search returns documents matching the query, ranked by BM25.
func (e *Engine) Search(query string, limit int) []*Document {
	e.mu.RLock()
	defer e.mu.RUnlock()

	tokens := tokenize(query)
	return e.searchTokens(tokens, limit)
}

func (e *Engine) searchTokens(tokens []string, limit int) []*Document {
	if len(tokens) == 0 || len(e.documents) == 0 {
		return nil
	}

	avgDL := e.totalLength / float64(len(e.documents))
	if avgDL == 0 {
		avgDL = 1
	}

	scores := map[string]float64{}

	for _, term := range tokens {
		if e.stopWords[term] {
			continue
		}
		docs, ok := e.index[term]
		if !ok {
			continue
		}

		idf := math.Log(1 + (float64(len(e.documents))-float64(len(docs))+0.5)/(float64(len(docs))+0.5))

		for docID, tf := range docs {
			docLen := float64(e.docLengths[docID])
			score := idf * (float64(tf) * (e.k1 + 1)) / (float64(tf) + e.k1*(1-e.b+e.b*docLen/avgDL))
			scores[docID] += score
		}
	}

	// Title match bonus
	for _, term := range tokens {
		for docID, doc := range e.documents {
			if strings.Contains(strings.ToLower(doc.Title), term) {
				scores[docID] += 1.5
			}
			for _, tag := range doc.Tags {
				if strings.EqualFold(tag, term) {
					scores[docID] += 2.0
				}
			}
		}
	}

	type scoredDoc struct {
		doc   *Document
		score float64
	}
	var ranked []scoredDoc
	for docID, score := range scores {
		if score <= 0 {
			continue
		}
		ranked = append(ranked, scoredDoc{e.documents[docID], score})
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })

	if limit > 0 && limit < len(ranked) {
		ranked = ranked[:limit]
	}

	out := make([]*Document, len(ranked))
	for i, r := range ranked {
		doc := *r.doc
		doc.Score = r.score
		out[i] = &doc
	}
	return out
}

// ── Highlighting ─────────────────────────────────────────

// Highlight marks query terms in text with ANSI color codes.
func Highlight(text, query string) string {
	if query == "" {
		return text
	}
	tokens := tokenize(query)
	result := text
	for _, t := range tokens {
		result = highlightTerm(result, t)
	}
	return result
}

func highlightTerm(text, term string) string {
	lower := strings.ToLower(text)
	termLower := strings.ToLower(term)
	var sb strings.Builder
	last := 0
	for {
		idx := strings.Index(lower[last:], termLower)
		if idx < 0 {
			break
		}
		idx += last
		sb.WriteString(text[last:idx])
		sb.WriteString("\x1b[33m")
		sb.WriteString(text[idx : idx+len(term)])
		sb.WriteString("\x1b[0m")
		last = idx + len(term)
	}
	sb.WriteString(text[last:])
	return sb.String()
}

// ── Suggest ──────────────────────────────────────────────

// Suggest returns autocomplete suggestions for a prefix.
func (e *Engine) Suggest(prefix string, limit int) []string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	prefix = strings.ToLower(prefix)
	seen := map[string]bool{}
	var suggestions []string

	for term := range e.index {
		if strings.HasPrefix(term, prefix) && !seen[term] {
			seen[term] = true
			suggestions = append(suggestions, term)
		}
	}
	sort.Strings(suggestions)
	if limit > 0 && limit < len(suggestions) {
		suggestions = suggestions[:limit]
	}
	return suggestions
}

// FormatResults formats search results for display.
func FormatResults(docs []*Document) string {
	if len(docs) == 0 {
		return "No results found.\n"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d result(s):\n\n", len(docs))
	for i, doc := range docs {
		fmt.Fprintf(&sb, "%d. %s", i+1, doc.Title)
		if doc.Score > 0 {
			fmt.Fprintf(&sb, " (score: %.1f)", doc.Score)
		}
		sb.WriteByte('\n')
		snippet := doc.Content
		if len(snippet) > 150 {
			snippet = snippet[:150] + "..."
		}
		fmt.Fprintf(&sb, "   %s\n\n", snippet)
	}
	return sb.String()
}

// ── Tokenizer ─────────────────────────────────────────────

func tokenize(text string) []string {
	var tokens []string
	var current strings.Builder
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current.WriteRune(r)
		} else {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}
