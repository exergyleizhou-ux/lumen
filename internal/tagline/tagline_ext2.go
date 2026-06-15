// Package tagline - more extensions: text segmentation, readability scoring,
// TF-IDF vectorizer, cosine similarity, text normalizer, string distance.
package tagline

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

// ---- Text Segmentation ----

// Segment splits text into sentences.
func Segment(text string) []string {
	var sentences []string
	var current []rune

	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		ch := runes[i]
		current = append(current, ch)

		// End of sentence detection
		if ch == '.' || ch == '!' || ch == '?' {
			// Look ahead: is this really end of sentence?
			if i+1 < len(runes) && (runes[i+1] == ' ' || runes[i+1] == '\n' || i+1 == len(runes)) {
				// Could be an abbreviation - check if previous word is short
				s := strings.TrimSpace(string(current))
				if len(s) > 0 {
					sentences = append(sentences, s)
				}
				current = nil
				// Skip following whitespace
				for i+1 < len(runes) && (runes[i+1] == ' ' || runes[i+1] == '\n') {
					i++
				}
			}
		}
	}

	if len(current) > 0 {
		s := strings.TrimSpace(string(current))
		if len(s) > 0 {
			sentences = append(sentences, s)
		}
	}

	return sentences
}

// SegmentParagraphs splits text into paragraphs.
func SegmentParagraphs(text string) []string {
	parts := strings.Split(text, "\n\n")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// ---- Readability Scoring ----

// ReadabilityScore holds various readability metrics.
type ReadabilityScore struct {
	FleschKincaid float64 `json:"flesch_kincaid"`
	FleschEase    float64 `json:"flesch_ease"`
	Words         int     `json:"words"`
	Sentences     int     `json:"sentences"`
	Syllables     int     `json:"syllables"`
	Level         string  `json:"level"` // "easy", "moderate", "hard"
}

// ComputeReadability computes readability scores for text.
func ComputeReadability(text string) *ReadabilityScore {
	sentences := Segment(text)
	words := tokenize(text)

	var totalSyllables int
	for _, w := range words {
		totalSyllables += countSyllables(w)
	}

	wordCount := len(words)
	sentenceCount := len(sentences)

	if wordCount == 0 || sentenceCount == 0 {
		return &ReadabilityScore{
			Words:     wordCount,
			Sentences: sentenceCount,
			Syllables: totalSyllables,
			Level:     "unknown",
		}
	}

	// Flesch Reading Ease
	ease := 206.835 - 1.015*float64(wordCount)/float64(sentenceCount) - 84.6*float64(totalSyllables)/float64(wordCount)

	// Flesch-Kincaid Grade Level
	grade := 0.39*float64(wordCount)/float64(sentenceCount) + 11.8*float64(totalSyllables)/float64(wordCount) - 15.59

	level := "easy"
	if grade > 12 {
		level = "hard"
	} else if grade > 8 {
		level = "moderate"
	}

	return &ReadabilityScore{
		FleschKincaid: grade,
		FleschEase:    ease,
		Words:         wordCount,
		Sentences:     sentenceCount,
		Syllables:     totalSyllables,
		Level:         level,
	}
}

func countSyllables(word string) int {
	word = strings.ToLower(word)
	count := 0
	prevVowel := false

	for _, ch := range word {
		isVowel := strings.ContainsRune("aeiouy", ch)
		if isVowel && !prevVowel {
			count++
		}
		prevVowel = isVowel
	}

	// Silent e at end
	if strings.HasSuffix(word, "e") && count > 1 {
		// Check if it ends in "le" (like "apple") — then the e counts
		if !strings.HasSuffix(word, "le") || (len(word) > 3 && !strings.ContainsRune("bcdfghjklmnpqrstvwxyz", rune(word[len(word)-3]))) {
			count--
		}
	}

	if count == 0 {
		count = 1
	}
	return count
}

// ---- TF-IDF Vectorizer ----

// TFIDFVectorizer converts documents to TF-IDF vectors.
type TFIDFVectorizer struct {
	vocabulary  map[string]int
	idf         map[string]float64
	docCount    int
}

// NewTFIDFVectorizer creates a TF-IDF vectorizer.
func NewTFIDFVectorizer() *TFIDFVectorizer {
	return &TFIDFVectorizer{
		vocabulary: make(map[string]int),
		idf:        make(map[string]float64),
	}
}

// Fit learns the vocabulary and IDF from documents.
func (v *TFIDFVectorizer) Fit(documents []string) {
	v.docCount = len(documents)
	docFreq := make(map[string]int)

	for _, doc := range documents {
		words := tokenize(doc)
		seen := make(map[string]bool)
		for _, w := range words {
			lower := strings.ToLower(w)
			if len(lower) < 2 || isNumeric(lower) {
				continue
			}
			if !seen[lower] {
				docFreq[lower]++
				seen[lower] = true
			}
		}
	}

	idx := 0
	for word, df := range docFreq {
		v.vocabulary[word] = idx
		v.idf[word] = math.Log(float64(v.docCount+1)/float64(df+1)) + 1
		idx++
	}
}

// Transform converts a document to a TF-IDF vector.
func (v *TFIDFVectorizer) Transform(document string) map[int]float64 {
	words := tokenize(document)
	tf := make(map[string]int)
	for _, w := range words {
		lower := strings.ToLower(w)
		tf[lower]++
	}

	vec := make(map[int]float64)
	total := float64(len(words))
	for word, count := range tf {
		if idx, ok := v.vocabulary[word]; ok {
			vec[idx] = (float64(count) / total) * v.idf[word]
		}
	}
	return vec
}

// FitTransform fits and transforms documents in one step.
func (v *TFIDFVectorizer) FitTransform(documents []string) []map[int]float64 {
	v.Fit(documents)
	vecs := make([]map[int]float64, len(documents))
	for i, doc := range documents {
		vecs[i] = v.Transform(doc)
	}
	return vecs
}

// VocabularySize returns the number of unique terms.
func (v *TFIDFVectorizer) VocabularySize() int {
	return len(v.vocabulary)
}

// ---- Cosine Similarity ----

// CosineSimilarity computes cosine similarity between two sparse vectors.
func CosineSimilarity(a, b map[int]float64) float64 {
	var dotProduct float64
	var normA, normB float64

	for k, v := range a {
		normA += v * v
		if bv, ok := b[k]; ok {
			dotProduct += v * bv
		}
	}

	for _, v := range b {
		normB += v * v
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

// ---- Text Normalizer ----

// TextNormalizer provides text normalization capabilities.
type TextNormalizer struct {
	lowercase    bool
	removeAccents bool
	removePunct  bool
	collapseWS   bool
}

// NewTextNormalizer creates a text normalizer.
func NewTextNormalizer() *TextNormalizer {
	return &TextNormalizer{
		lowercase:    true,
		removeAccents: true,
		removePunct:  false,
		collapseWS:   true,
	}
}

// Normalize applies normalization rules.
func (tn *TextNormalizer) Normalize(text string) string {
	if tn.lowercase {
		text = strings.ToLower(text)
	}

	if tn.removeAccents {
		text = removeAccents(text)
	}

	if tn.removePunct {
		text = removePunctuation(text)
	}

	if tn.collapseWS {
		text = collapseWhitespace(text)
	}

	return text
}

func removeAccents(s string) string {
	replacements := map[rune]rune{
		'à': 'a', 'á': 'a', 'â': 'a', 'ã': 'a', 'ä': 'a', 'å': 'a',
		'è': 'e', 'é': 'e', 'ê': 'e', 'ë': 'e',
		'ì': 'i', 'í': 'i', 'î': 'i', 'ï': 'i',
		'ò': 'o', 'ó': 'o', 'ô': 'o', 'õ': 'o', 'ö': 'o',
		'ù': 'u', 'ú': 'u', 'û': 'u', 'ü': 'u',
		'ñ': 'n', 'ç': 'c',
		'À': 'A', 'Á': 'A', 'Â': 'A', 'Ã': 'A', 'Ä': 'A',
		'È': 'E', 'É': 'E', 'Ê': 'E', 'Ë': 'E',
		'Ì': 'I', 'Í': 'I', 'Î': 'I', 'Ï': 'I',
		'Ò': 'O', 'Ó': 'O', 'Ô': 'O', 'Õ': 'O', 'Ö': 'O',
		'Ù': 'U', 'Ú': 'U', 'Û': 'U', 'Ü': 'U',
		'Ñ': 'N', 'Ç': 'C',
	}

	var result strings.Builder
	for _, r := range s {
		if repl, ok := replacements[r]; ok {
			result.WriteRune(repl)
		} else {
			result.WriteRune(r)
		}
	}
	return result.String()
}

func removePunctuation(s string) string {
	var result strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) {
			result.WriteRune(r)
		} else {
			result.WriteRune(' ')
		}
	}
	return result.String()
}

func collapseWhitespace(s string) string {
	var result strings.Builder
	prevSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !prevSpace {
				result.WriteRune(' ')
				prevSpace = true
			}
		} else {
			result.WriteRune(r)
			prevSpace = false
		}
	}
	return strings.TrimSpace(result.String())
}

// ---- String Distance Metrics ----

// Levenshtein computes the Levenshtein (edit) distance between two strings.
func Levenshtein(a, b string) int {
	ar, br := []rune(a), []rune(b)
	n, m := len(ar), len(br)

	if n == 0 {
		return m
	}
	if m == 0 {
		return n
	}

	// Use two rows to save memory
	prev := make([]int, m+1)
	curr := make([]int, m+1)

	for j := 0; j <= m; j++ {
		prev[j] = j
	}

	for i := 1; i <= n; i++ {
		curr[0] = i
		for j := 1; j <= m; j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			curr[j] = minInt3(
				prev[j]+1,      // deletion
				curr[j-1]+1,    // insertion
				prev[j-1]+cost, // substitution
			)
		}
		prev, curr = curr, prev
	}

	return prev[m]
}

func minInt3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

// JaccardSimilarity computes Jaccard similarity between two text strings.
func JaccardSimilarity(a, b string) float64 {
	wordsA := tokenize(a)
	wordsB := tokenize(b)

	setA := make(map[string]bool)
	setB := make(map[string]bool)

	for _, w := range wordsA {
		setA[strings.ToLower(w)] = true
	}
	for _, w := range wordsB {
		setB[strings.ToLower(w)] = true
	}

	intersection := 0
	for w := range setA {
		if setB[w] {
			intersection++
		}
	}

	union := len(setA) + len(setB) - intersection
	if union == 0 {
		return 1.0
	}
	return float64(intersection) / float64(union)
}

// ---- N-gram frequency analysis ----

// NgramFreq computes n-gram frequency distribution.
func NgramFreq(text string, n int) map[string]int {
	freq := make(map[string]int)
	ngrams := Ngrams(text, n)
	for _, ng := range ngrams {
		freq[ng]++
	}
	return freq
}

// TopNgrams returns the most frequent n-grams.
func TopNgrams(text string, n, top int) []Tag {
	freq := NgramFreq(text, n)
	tags := make([]Tag, 0, len(freq))
	for ng, count := range freq {
		tags = append(tags, Tag{Name: ng, Count: count, Weight: float64(count), Type: "ngram"})
	}
	sort.Slice(tags, func(i, j int) bool {
		return tags[i].Weight > tags[j].Weight
	})
	if top > 0 && top < len(tags) {
		tags = tags[:top]
	}
	return tags
}
