// Package tagline provides semantic tag extraction: NLP-lite keyword extraction,
// entity recognition stubs, tag ranking, tag cloud generation, and
// co-occurrence matrix computation.
package tagline

import (
	"math"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"
)

// Tag represents an extracted semantic tag.
type Tag struct {
	Name   string  `json:"name"`
	Weight float64 `json:"weight"`
	Count  int     `json:"count"`
	Type   string  `json:"type"` // "keyword", "entity", "hashtag"
}

// ---- Stop Words ----

var defaultStopWords = map[string]bool{
	"the": true, "is": true, "at": true, "which": true, "on": true,
	"a": true, "an": true, "and": true, "or": true, "but": true,
	"in": true, "with": true, "to": true, "for": true, "of": true,
	"by": true, "from": true, "as": true, "into": true, "through": true,
	"it": true, "its": true, "be": true, "been": true, "being": true,
	"was": true, "were": true, "are": true, "am": true, "this": true,
	"that": true, "these": true, "those": true, "has": true, "have": true,
	"had": true, "do": true, "does": true, "did": true, "will": true,
	"would": true, "could": true, "should": true, "may": true, "might": true,
	"can": true, "shall": true, "not": true, "no": true, "nor": true,
	"so": true, "if": true, "then": true, "than": true, "too": true,
	"very": true, "just": true, "about": true, "also": true, "up": true,
	"out": true, "when": true, "where": true, "how": true, "all": true,
	"both": true, "each": true, "few": true, "more": true, "most": true,
	"other": true, "some": true, "such": true, "only": true, "own": true,
	"same": true, "here": true, "there": true, "what": true, "who": true,
}

// ---- Extractor ----

// Extractor extracts semantic tags from text.
type Extractor struct {
	mu             sync.RWMutex
	stopWords      map[string]bool
	minWordLen     int
	maxKeywords    int
	entityPatterns []*regexp.Regexp
}

// NewExtractor creates a new tag extractor with default settings.
func NewExtractor() *Extractor {
	return &Extractor{
		stopWords:   copyStopWords(defaultStopWords),
		minWordLen:  3,
		maxKeywords: 20,
	}
}

// SetStopWords sets custom stop words.
func (e *Extractor) SetStopWords(words []string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.stopWords = make(map[string]bool)
	for _, w := range words {
		e.stopWords[strings.ToLower(w)] = true
	}
}

// AddStopWord adds a single stop word.
func (e *Extractor) AddStopWord(word string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.stopWords[strings.ToLower(word)] = true
}

// SetMinWordLen sets the minimum word length for keyword extraction.
func (e *Extractor) SetMinWordLen(n int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.minWordLen = n
}

// SetMaxKeywords sets the maximum number of keywords to extract.
func (e *Extractor) SetMaxKeywords(n int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.maxKeywords = n
}

// AddEntityPattern registers a regex pattern for entity recognition.
func (e *Extractor) AddEntityPattern(pattern string) error {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.entityPatterns = append(e.entityPatterns, re)
	return nil
}

// ---- Keyword Extraction ----

// ExtractKeywords extracts the top keywords from text using TF-based ranking.
func (e *Extractor) ExtractKeywords(text string) []Tag {
	e.mu.RLock()
	defer e.mu.RUnlock()

	words := tokenize(text)
	wordFreq := make(map[string]int)
	wordCount := 0

	for _, w := range words {
		lower := strings.ToLower(w)
		if len(lower) < e.minWordLen {
			continue
		}
		if e.stopWords[lower] {
			continue
		}
		if isNumeric(lower) {
			continue
		}
		wordFreq[lower]++
		wordCount++
	}

	if wordCount == 0 {
		return nil
	}

	tags := make([]Tag, 0, len(wordFreq))
	for word, count := range wordFreq {
		tf := float64(count) / float64(wordCount)
		tags = append(tags, Tag{
			Name:   word,
			Weight: tf,
			Count:  count,
			Type:   "keyword",
		})
	}

	sort.Slice(tags, func(i, j int) bool {
		if tags[i].Weight != tags[j].Weight {
			return tags[i].Weight > tags[j].Weight
		}
		return tags[i].Name < tags[j].Name
	})

	if len(tags) > e.maxKeywords {
		tags = tags[:e.maxKeywords]
	}

	return tags
}

// ExtractKeywordsTFIDF extracts keywords using TF-IDF across multiple documents.
func ExtractKeywordsTFIDF(documents []string, maxKeywords int) [][]Tag {
	if len(documents) == 0 {
		return nil
	}

	// Tokenize all documents
	docWords := make([][]string, len(documents))
	wordFreqs := make([]map[string]int, len(documents))
	docFreq := make(map[string]int) // number of documents containing each word
	stopWords := defaultStopWords

	for i, doc := range documents {
		words := tokenize(doc)
		docWords[i] = words
		wordFreqs[i] = make(map[string]int)
		seen := make(map[string]bool)
		for _, w := range words {
			lower := strings.ToLower(w)
			if len(lower) < 3 || stopWords[lower] || isNumeric(lower) {
				continue
			}
			wordFreqs[i][lower]++
			if !seen[lower] {
				docFreq[lower]++
				seen[lower] = true
			}
		}
	}

	N := float64(len(documents))
	results := make([][]Tag, len(documents))

	for i := range documents {
		total := float64(len(docWords[i]))
		if total == 0 {
			continue
		}
		var tags []Tag
		for word, tf := range wordFreqs[i] {
			idf := math.Log(N / float64(docFreq[word]))
			weight := (float64(tf) / total) * idf
			tags = append(tags, Tag{
				Name:   word,
				Weight: weight,
				Count:  tf,
				Type:   "keyword",
			})
		}
		sort.Slice(tags, func(a, b int) bool {
			return tags[a].Weight > tags[b].Weight
		})
		if maxKeywords > 0 && len(tags) > maxKeywords {
			tags = tags[:maxKeywords]
		}
		results[i] = tags
	}

	return results
}

// ---- Entity Recognition ----

// Entity represents a recognized named entity.
type Entity struct {
	Name  string `json:"name"`
	Type  string `json:"type"` // "person", "org", "location", "date", "email", "url"
	Start int    `json:"start"`
	End   int    `json:"end"`
}

// ExtractEntities extracts named entities from text using pattern matching.
func (e *Extractor) ExtractEntities(text string) []Entity {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var entities []Entity

	// URL pattern
	urlRe := regexp.MustCompile(`https?://[^\s]+`)
	for _, m := range urlRe.FindAllStringIndex(text, -1) {
		entities = append(entities, Entity{
			Name:  text[m[0]:m[1]],
			Type:  "url",
			Start: m[0],
			End:   m[1],
		})
	}

	// Email pattern
	emailRe := regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)
	for _, m := range emailRe.FindAllStringIndex(text, -1) {
		entities = append(entities, Entity{
			Name:  text[m[0]:m[1]],
			Type:  "email",
			Start: m[0],
			End:   m[1],
		})
	}

	// Date patterns (ISO-like)
	dateRe := regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)
	for _, m := range dateRe.FindAllStringIndex(text, -1) {
		entities = append(entities, Entity{
			Name:  text[m[0]:m[1]],
			Type:  "date",
			Start: m[0],
			End:   m[1],
		})
	}

	// Hashtag pattern
	hashtagRe := regexp.MustCompile(`#\w+`)
	for _, m := range hashtagRe.FindAllStringIndex(text, -1) {
		entities = append(entities, Entity{
			Name:  text[m[0]:m[1]],
			Type:  "hashtag",
			Start: m[0],
			End:   m[1],
		})
	}

	// Custom entity patterns
	for _, pattern := range e.entityPatterns {
		for _, m := range pattern.FindAllStringIndex(text, -1) {
			entities = append(entities, Entity{
				Name:  text[m[0]:m[1]],
				Type:  "custom",
				Start: m[0],
				End:   m[1],
			})
		}
	}

	// Sort by position
	sort.Slice(entities, func(i, j int) bool {
		return entities[i].Start < entities[j].Start
	})

	return entities
}

// ---- Tag Ranking ----

// RankTags sorts and scores tags using a combination of frequency and position.
func RankTags(tags []Tag) []Tag {
	ranked := make([]Tag, len(tags))
	copy(ranked, tags)

	// Score: weight * log(1 + count)
	for i := range ranked {
		if ranked[i].Count > 0 {
			ranked[i].Weight = ranked[i].Weight * math.Log2(1+float64(ranked[i].Count))
		}
	}

	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].Weight > ranked[j].Weight
	})

	return ranked
}

// NormalizeWeights scales tag weights to sum to 1.0.
func NormalizeWeights(tags []Tag) {
	var total float64
	for _, t := range tags {
		total += t.Weight
	}
	if total > 0 {
		for i := range tags {
			tags[i].Weight /= total
		}
	}
}

// ---- Tag Cloud ----

// TagCloud represents a weighted collection of tags for visualization.
type TagCloud struct {
	Tags      []Tag   `json:"tags"`
	MaxWeight float64 `json:"max_weight"`
	MinWeight float64 `json:"min_weight"`
}

// GenerateCloud creates a tag cloud from tags.
func GenerateCloud(tags []Tag) *TagCloud {
	if len(tags) == 0 {
		return &TagCloud{Tags: tags}
	}

	maxW := tags[0].Weight
	minW := tags[0].Weight
	for _, t := range tags {
		if t.Weight > maxW {
			maxW = t.Weight
		}
		if t.Weight < minW {
			minW = t.Weight
		}
	}

	return &TagCloud{
		Tags:      tags,
		MaxWeight: maxW,
		MinWeight: minW,
	}
}

// FontSize returns a relative font size (1-5) for a tag based on its weight.
func (tc *TagCloud) FontSize(tag Tag) int {
	if tc.MaxWeight == tc.MinWeight {
		return 3
	}
	ratio := (tag.Weight - tc.MinWeight) / (tc.MaxWeight - tc.MinWeight)
	return int(math.Ceil(ratio*4 + 1))
}

// TopN returns the top N tags from the cloud.
func (tc *TagCloud) TopN(n int) []Tag {
	if n >= len(tc.Tags) {
		return tc.Tags
	}
	return tc.Tags[:n]
}

// ---- Co-occurrence Matrix ----

// CoOccurrenceMatrix tracks how often tags appear together.
type CoOccurrenceMatrix struct {
	mu     sync.RWMutex
	matrix map[string]map[string]int
	tags   map[string]bool
}

// NewCoOccurrenceMatrix creates a co-occurrence matrix.
func NewCoOccurrenceMatrix() *CoOccurrenceMatrix {
	return &CoOccurrenceMatrix{
		matrix: make(map[string]map[string]int),
		tags:   make(map[string]bool),
	}
}

// Add records a set of tags that co-occurred.
func (cm *CoOccurrenceMatrix) Add(tags []string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	for _, t := range tags {
		cm.tags[t] = true
	}

	for i := 0; i < len(tags); i++ {
		for j := i + 1; j < len(tags); j++ {
			a, b := tags[i], tags[j]
			if a > b {
				a, b = b, a
			}
			if cm.matrix[a] == nil {
				cm.matrix[a] = make(map[string]int)
			}
			cm.matrix[a][b]++
		}
	}
}

// Get returns the co-occurrence count for two tags.
func (cm *CoOccurrenceMatrix) Get(a, b string) int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if a > b {
		a, b = b, a
	}
	if row, ok := cm.matrix[a]; ok {
		return row[b]
	}
	return 0
}

// AllTags returns all known tags.
func (cm *CoOccurrenceMatrix) AllTags() []string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	tags := make([]string, 0, len(cm.tags))
	for t := range cm.tags {
		tags = append(tags, t)
	}
	sort.Strings(tags)
	return tags
}

// Related returns tags most commonly co-occurring with the given tag, up to n.
func (cm *CoOccurrenceMatrix) Related(tag string, n int) []Tag {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	type pair struct {
		tag   string
		count int
	}
	var pairs []pair

	for other := range cm.tags {
		if other == tag {
			continue
		}
		count := cm.getLocked(tag, other)
		if count > 0 {
			pairs = append(pairs, pair{other, count})
		}
	}

	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].count > pairs[j].count
	})

	if n > 0 && n < len(pairs) {
		pairs = pairs[:n]
	}

	result := make([]Tag, len(pairs))
	for i, p := range pairs {
		result[i] = Tag{Name: p.tag, Count: p.count, Type: "related"}
	}
	return result
}

func (cm *CoOccurrenceMatrix) getLocked(a, b string) int {
	if a > b {
		a, b = b, a
	}
	if row, ok := cm.matrix[a]; ok {
		return row[b]
	}
	return 0
}

// Matrix returns the full co-occurrence matrix as a 2D map.
func (cm *CoOccurrenceMatrix) Matrix() map[string]map[string]int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	result := make(map[string]map[string]int)
	for a, row := range cm.matrix {
		result[a] = make(map[string]int)
		for b, count := range row {
			result[a][b] = count
		}
	}
	return result
}

// ---- Phrase Extraction ----

// ExtractPhrases extracts common bigrams and trigrams from text.
func ExtractPhrases(text string, minCount int) []Tag {
	words := tokenize(text)
	if len(words) < 2 {
		return nil
	}

	bigrams := make(map[string]int)
	trigrams := make(map[string]int)

	for i := 0; i < len(words)-1; i++ {
		bigram := strings.ToLower(words[i]) + " " + strings.ToLower(words[i+1])
		bigrams[bigram]++
	}

	for i := 0; i < len(words)-2; i++ {
		trigram := strings.ToLower(words[i]) + " " + strings.ToLower(words[i+1]) + " " + strings.ToLower(words[i+2])
		trigrams[trigram]++
	}

	var tags []Tag
	for phrase, count := range bigrams {
		if count >= minCount {
			tags = append(tags, Tag{Name: phrase, Count: count, Weight: float64(count), Type: "bigram"})
		}
	}
	for phrase, count := range trigrams {
		if count >= minCount {
			tags = append(tags, Tag{Name: phrase, Count: count, Weight: float64(count) * 1.5, Type: "trigram"})
		}
	}

	sort.Slice(tags, func(i, j int) bool {
		return tags[i].Weight > tags[j].Weight
	})

	return tags
}

// ---- Text Summarization helpers ----

// Sentence represents a scored sentence for summarization.
type Sentence struct {
	Text  string  `json:"text"`
	Score float64 `json:"score"`
	Index int     `json:"index"`
}

// ScoreSentences scores sentences by keyword density.
func ScoreSentences(sentences []string, keywords []Tag) []Sentence {
	keywordSet := make(map[string]float64)
	for _, kw := range keywords {
		keywordSet[kw.Name] = kw.Weight
	}

	result := make([]Sentence, len(sentences))
	for i, s := range sentences {
		words := tokenize(s)
		var score float64
		for _, w := range words {
			if weight, ok := keywordSet[strings.ToLower(w)]; ok {
				score += weight
			}
		}
		if len(words) > 0 {
			score /= float64(len(words))
		}
		result[i] = Sentence{Text: s, Score: score, Index: i}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Score > result[j].Score
	})

	return result
}

// ---- Helper functions ----

func tokenize(text string) []string {
	var words []string
	var current []rune

	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '\'' {
			current = append(current, r)
		} else {
			if len(current) > 0 {
				words = append(words, string(current))
				current = nil
			}
		}
	}
	if len(current) > 0 {
		words = append(words, string(current))
	}
	return words
}

func isNumeric(s string) bool {
	for _, r := range s {
		if !unicode.IsDigit(r) && r != '.' && r != ',' {
			return false
		}
	}
	return len(s) > 0
}

func copyStopWords(src map[string]bool) map[string]bool {
	dst := make(map[string]bool, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// ---- Batch Processing ----

// BatchResult contains extraction results for multiple texts.
type BatchResult struct {
	Documents int        `json:"documents"`
	Tags      [][]Tag    `json:"tags"`
	Entities  [][]Entity `json:"entities"`
}

// ProcessBatch extracts tags and entities from multiple documents.
func (e *Extractor) ProcessBatch(texts []string) *BatchResult {
	result := &BatchResult{
		Documents: len(texts),
		Tags:      make([][]Tag, len(texts)),
		Entities:  make([][]Entity, len(texts)),
	}

	for i, text := range texts {
		result.Tags[i] = e.ExtractKeywords(text)
		result.Entities[i] = e.ExtractEntities(text)
	}

	return result
}

// ---- Tag Deduplication ----

// DeduplicateTags removes near-duplicate tags using simple Levenshtein comparison.
func DeduplicateTags(tags []Tag, threshold float64) []Tag {
	if threshold <= 0 {
		threshold = 0.8
	}

	var result []Tag
	used := make(map[string]bool)

	for _, tag := range tags {
		name := strings.ToLower(tag.Name)
		isDup := false
		for existing := range used {
			if similarity(existing, name) >= threshold {
				isDup = true
				break
			}
		}
		if !isDup {
			used[name] = true
			result = append(result, tag)
		}
	}

	return result
}

func similarity(a, b string) float64 {
	if a == b {
		return 1.0
	}
	if len(a) == 0 || len(b) == 0 {
		return 0
	}

	// Jaccard-like character bigram similarity
	bigramsA := make(map[string]int)
	bigramsB := make(map[string]int)

	for i := 0; i < len(a)-1; i++ {
		bigramsA[a[i:i+2]]++
	}
	for i := 0; i < len(b)-1; i++ {
		bigramsB[b[i:i+2]]++
	}

	intersection := 0
	for bg, countA := range bigramsA {
		if countB, ok := bigramsB[bg]; ok {
			if countA < countB {
				intersection += countA
			} else {
				intersection += countB
			}
		}
	}

	union := 0
	allBigrams := make(map[string]bool)
	for bg := range bigramsA {
		allBigrams[bg] = true
	}
	for bg := range bigramsB {
		allBigrams[bg] = true
	}

	for bg := range allBigrams {
		aCount := bigramsA[bg]
		bCount := bigramsB[bg]
		if aCount > bCount {
			union += aCount
		} else {
			union += bCount
		}
	}

	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}
