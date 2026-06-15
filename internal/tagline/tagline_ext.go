// Package tagline - extension: advanced NLP features, text classification,
// sentiment analysis stubs, keyword expansion, language detection.
package tagline

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

// ---- Text Classification ----

// Classifier is a simple Naive Bayes text classifier.
type Classifier struct {
	classes     map[string]*classStats
	vocabulary  map[string]bool
	totalDocs   int
}

type classStats struct {
	name       string
	docCount   int
	wordCounts map[string]int
	totalWords int
}

// NewClassifier creates a new text classifier.
func NewClassifier() *Classifier {
	return &Classifier{
		classes:    make(map[string]*classStats),
		vocabulary: make(map[string]bool),
	}
}

// Train adds a document with its class label.
func (c *Classifier) Train(text, class string) {
	if _, ok := c.classes[class]; !ok {
		c.classes[class] = &classStats{
			name:       class,
			wordCounts: make(map[string]int),
		}
	}
	stats := c.classes[class]
	stats.docCount++
	c.totalDocs++

	words := tokenize(text)
	for _, w := range words {
		lower := strings.ToLower(w)
		if len(lower) < 2 || defaultStopWords[lower] {
			continue
		}
		c.vocabulary[lower] = true
		stats.wordCounts[lower]++
		stats.totalWords++
	}
}

// Classify predicts the class of a text.
func (c *Classifier) Classify(text string) (string, float64) {
	if len(c.classes) == 0 {
		return "", 0
	}

	words := tokenize(text)
	scores := make(map[string]float64)

	for className, stats := range c.classes {
		// Prior probability
		score := math.Log(float64(stats.docCount) / float64(c.totalDocs))
		vocabSize := float64(len(c.vocabulary))

		for _, w := range words {
			lower := strings.ToLower(w)
			if len(lower) < 2 || defaultStopWords[lower] {
				continue
			}
			// Laplace smoothing
			count := float64(stats.wordCounts[lower])
			prob := (count + 1) / (float64(stats.totalWords) + vocabSize)
			score += math.Log(prob)
		}
		scores[className] = score
	}

	// Find class with highest score
	bestClass := ""
	bestScore := math.Inf(-1)
	for class, score := range scores {
		if score > bestScore {
			bestScore = score
			bestClass = class
		}
	}

	return bestClass, math.Exp(bestScore)
}

// ClassProbabilities returns the probability distribution across classes.
func (c *Classifier) ClassProbabilities(text string) map[string]float64 {
	if len(c.classes) == 0 {
		return nil
	}

	scores := make(map[string]float64)
	var total float64

	for className, stats := range c.classes {
		score := math.Log(float64(stats.docCount) / float64(c.totalDocs))
		vocabSize := float64(len(c.vocabulary))
		words := tokenize(text)
		for _, w := range words {
			lower := strings.ToLower(w)
			if len(lower) < 2 || defaultStopWords[lower] {
				continue
			}
			count := float64(stats.wordCounts[lower])
			prob := (count + 1) / (float64(stats.totalWords) + vocabSize)
			score += math.Log(prob)
		}
		exp := math.Exp(score)
		scores[className] = exp
		total += exp
	}

	if total > 0 {
		for k := range scores {
			scores[k] /= total
		}
	}

	return scores
}

// ---- Sentiment Analysis ----

// SentimentResult holds sentiment analysis results.
type SentimentResult struct {
	Score     float64 `json:"score"`     // -1.0 (negative) to 1.0 (positive)
	Label     string  `json:"label"`     // "positive", "negative", "neutral"
	Magnitude float64 `json:"magnitude"` // intensity
}

// Positive words (small built-in set)
var positiveWords = map[string]float64{
	"good": 0.5, "great": 0.7, "excellent": 0.9, "wonderful": 0.8,
	"fantastic": 0.9, "amazing": 0.85, "love": 0.8, "happy": 0.6,
	"beautiful": 0.7, "perfect": 0.8, "best": 0.7, "awesome": 0.85,
	"brilliant": 0.8, "outstanding": 0.9, "superb": 0.8, "nice": 0.4,
	"pleasant": 0.5, "delightful": 0.7, "enjoyable": 0.6, "fabulous": 0.8,
}

var negativeWords = map[string]float64{
	"bad": -0.5, "terrible": -0.8, "awful": -0.85, "horrible": -0.9,
	"poor": -0.6, "hate": -0.8, "ugly": -0.7, "worst": -0.8,
	"disgusting": -0.9, "dreadful": -0.8, "nasty": -0.7, "unpleasant": -0.6,
	"boring": -0.5, "annoying": -0.6, "frustrating": -0.7, "disappointing": -0.65,
}

// AnalyzeSentiment performs simple sentiment analysis.
func AnalyzeSentiment(text string) *SentimentResult {
	words := tokenize(text)
	var score float64
	var count int

	for _, w := range words {
		lower := strings.ToLower(w)
		if val, ok := positiveWords[lower]; ok {
			score += val
			count++
		}
		if val, ok := negativeWords[lower]; ok {
			score += val
			count++
		}
	}

	if count > 0 {
		score /= float64(count)
	}

	label := "neutral"
	if score > 0.1 {
		label = "positive"
	} else if score < -0.1 {
		label = "negative"
	}

	return &SentimentResult{
		Score:     score,
		Label:     label,
		Magnitude: math.Abs(score),
	}
}

// ---- Keyword Expansion ----

// ExpandKeywords adds related terms to a keyword list using a simple thesaurus.
func ExpandKeywords(tags []Tag, thesaurus map[string][]string) []Tag {
	expanded := make([]Tag, len(tags))
	copy(expanded, tags)

	seen := make(map[string]bool)
	for _, t := range tags {
		seen[strings.ToLower(t.Name)] = true
	}

	for _, t := range tags {
		related, ok := thesaurus[strings.ToLower(t.Name)]
		if !ok {
			continue
		}
		for _, rel := range related {
			if !seen[rel] {
				expanded = append(expanded, Tag{
					Name:   rel,
					Weight: t.Weight * 0.5,
					Count:  1,
					Type:   "expanded",
				})
				seen[rel] = true
			}
		}
	}

	sort.Slice(expanded, func(i, j int) bool {
		return expanded[i].Weight > expanded[j].Weight
	})

	return expanded
}

// ---- Language Detection ----

// LanguageProfile holds n-gram frequencies for a language.
type LanguageProfile struct {
	Name     string
	Bigrams  map[string]float64
}

var languageProfiles = map[string]*LanguageProfile{
	"en": {Name: "english", Bigrams: map[string]float64{
		"th": 0.035, "he": 0.030, "an": 0.025, "in": 0.022,
		"er": 0.020, "on": 0.018, "at": 0.016, "en": 0.015,
	}},
	"es": {Name: "spanish", Bigrams: map[string]float64{
		"de": 0.030, "es": 0.028, "el": 0.025, "la": 0.024,
		"os": 0.022, "ar": 0.020, "ue": 0.018, "en": 0.016,
	}},
	"fr": {Name: "french", Bigrams: map[string]float64{
		"es": 0.032, "le": 0.028, "de": 0.026, "en": 0.024,
		"re": 0.022, "on": 0.020, "nt": 0.018, "er": 0.016,
	}},
	"de": {Name: "german", Bigrams: map[string]float64{
		"en": 0.035, "er": 0.032, "ch": 0.028, "de": 0.024,
		"ei": 0.022, "ie": 0.020, "in": 0.018, "te": 0.016,
	}},
}

// DetectLanguage attempts to detect the language of a text.
func DetectLanguage(text string) (string, float64) {
	text = strings.ToLower(text)
	bigrams := make(map[string]int)
	totalBigrams := 0

	for i := 0; i < len(text)-1; i++ {
		bg := text[i : i+2]
		if isAllLetters(bg) {
			bigrams[bg]++
			totalBigrams++
		}
	}

	if totalBigrams == 0 {
		return "unknown", 0
	}

	bestLang := "en"
	bestScore := math.Inf(-1)

	for code, profile := range languageProfiles {
		score := 0.0
		for bg, count := range bigrams {
			freq := float64(count) / float64(totalBigrams)
			expected, ok := profile.Bigrams[bg]
			if ok {
				// Chi-squared-like contribution
				diff := freq - expected
				score -= diff * diff / expected
			}
		}
		if score > bestScore {
			bestScore = score
			bestLang = code
		}
	}

	return bestLang, math.Exp(bestScore)
}

func isAllLetters(s string) bool {
	for _, r := range s {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	return true
}

// ---- N-gram utilities ----

// Ngrams generates n-grams from text.
func Ngrams(text string, n int) []string {
	if n < 1 {
		return nil
	}
	runes := []rune(text)
	if len(runes) < n {
		return nil
	}
	result := make([]string, len(runes)-n+1)
	for i := 0; i <= len(runes)-n; i++ {
		result[i] = string(runes[i : i+n])
	}
	return result
}

// WordNgrams generates word-level n-grams.
func WordNgrams(text string, n int) []string {
	words := tokenize(text)
	if len(words) < n {
		return nil
	}
	result := make([]string, len(words)-n+1)
	for i := 0; i <= len(words)-n; i++ {
		result[i] = strings.Join(words[i:i+n], " ")
	}
	return result
}

// ---- Tag clustering ----

// ClusterTags groups related tags using simple co-occurrence.
func ClusterTags(matrix *CoOccurrenceMatrix, minCooccurrence int) [][]string {
	tags := matrix.AllTags()
	if len(tags) == 0 {
		return nil
	}

	// Build adjacency for tags with sufficient co-occurrence
	adj := make(map[string]map[string]bool)
	for _, t := range tags {
		adj[t] = make(map[string]bool)
	}

	for i := 0; i < len(tags); i++ {
		for j := i + 1; j < len(tags); j++ {
			if matrix.Get(tags[i], tags[j]) >= minCooccurrence {
				adj[tags[i]][tags[j]] = true
				adj[tags[j]][tags[i]] = true
			}
		}
	}

	// Find connected components
	visited := make(map[string]bool)
	var clusters [][]string

	for _, tag := range tags {
		if visited[tag] {
			continue
		}
		cluster := bfsCluster(tag, adj, visited)
		if len(cluster) > 0 {
			sort.Strings(cluster)
			clusters = append(clusters, cluster)
		}
	}

	return clusters
}

func bfsCluster(start string, adj map[string]map[string]bool, visited map[string]bool) []string {
	var cluster []string
	queue := []string{start}
	visited[start] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		cluster = append(cluster, current)

		for neighbor := range adj[current] {
			if !visited[neighbor] {
				visited[neighbor] = true
				queue = append(queue, neighbor)
			}
		}
	}

	return cluster
}
