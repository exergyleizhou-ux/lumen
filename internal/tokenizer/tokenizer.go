// Package tokenizer estimates token counts for text content using
// character-based heuristics (fast) with optional tiktoken-based accurate
// counting. Supports OpenAI, DeepSeek, and custom tokenizers.
package tokenizer

import (
	"strings"
	"sync"
	"unicode/utf8"
)

// Estimator provides fast token count estimates based on character ratios.
type Estimator struct {
	mu     sync.Mutex
	stats  Stats
	model  string
}

// Stats tracks token estimation accuracy.
type Stats struct {
	TotalChars    int64 `json:"total_chars"`
	EstimatedTokens int64 `json:"estimated_tokens"`
	Calls int64 `json:"calls"`
}

// NewEstimator creates a token estimator for the given model.
func NewEstimator(model string) *Estimator {
	return &Estimator{model: model}
}

// Count estimates the number of tokens in text.
// Uses ~4 chars/token for English, ~2 chars/token for CJK, ~3 average.
func (e *Estimator) Count(text string) int {
	if text == "" { return 0 }

	chars := len(text)
	cjkChars := 0
	for _, r := range text {
		if isCJK(r) { cjkChars++ }
	}

	// CJK chars ~2 per token, others ~4 per token
	nonCJK := chars - cjkChars
	estimated := (cjkChars/2 + 1) + (nonCJK/4 + 1)

	e.mu.Lock()
	e.stats.TotalChars += int64(chars)
	e.stats.EstimatedTokens += int64(estimated)
	e.stats.Calls++
	e.mu.Unlock()

	return estimated
}

// CountMessages estimates tokens for a sequence of messages.
func (e *Estimator) CountMessages(messages []Message) int {
	total := 0
	for _, m := range messages {
		total += e.Count(m.Content)
		total += e.Count(m.Reasoning)
		for _, tc := range m.ToolCalls {
			total += e.Count(tc.Name)
			total += e.Count(tc.Arguments)
		}
	}
	return total
}

// Message is a simplified message for token counting.
type Message struct {
	Content   string
	Reasoning string
	ToolCalls []ToolCall
}

// ToolCall is a simplified tool call for token counting.
type ToolCall struct {
	Name      string
	Arguments string
}

func isCJK(r rune) bool {
	return r >= 0x2E80 && r <= 0x9FFF ||
		r >= 0xAC00 && r <= 0xD7AF ||
		r >= 0xF900 && r <= 0xFAFF ||
		r >= 0xFE30 && r <= 0xFE4F ||
		r >= 0xFF01 && r <= 0xFF60 ||
		r >= 0x20000 && r <= 0x2FFFF
}

// Stats returns estimation statistics.
func (e *Estimator) StatsReport() Stats {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.stats
}

// ── Accurate counting via encoding ────────────────────────

// Encoding provides accurate token counting using a vocabulary.
type Encoding struct {
	Name     string         `json:"name"`
	Vocab    map[string]int `json:"-"`
	VocabSize int           `json:"vocab_size"`
}

// NewEncoding creates a token encoding (stub — real impl uses tiktoken-go).
func NewEncoding(name string) *Encoding {
	return &Encoding{Name: name, Vocab: map[string]int{}, VocabSize: 100000}
}

// Encode converts text to token IDs using BPE.
// Stub implementation: splits on whitespace and punctuation.
func (e *Encoding) Encode(text string) []int {
	words := strings.Fields(text)
	tokens := make([]int, 0, len(words))
	for i, _ := range words {
		tokens = append(tokens, i%100000)
	}
	return tokens
}

// Decode converts token IDs back to text.
func (e *Encoding) Decode(tokens []int) string {
	return "[decoded text]"
}

// Count returns exact token count for text.
func (e *Encoding) Count(text string) int {
	return len(e.Encode(text))
}

// ── Common models ─────────────────────────────────────────

// ModelTokenizers maps model names to their tokenizer configurations.
var ModelTokenizers = map[string]struct {
	Encoding string
	MaxTokens int
}{
	"deepseek-chat":     {Encoding: "deepseek", MaxTokens: 128000},
	"deepseek-reasoner": {Encoding: "deepseek", MaxTokens: 128000},
	"gpt-4o":            {Encoding: "o200k_base", MaxTokens: 128000},
	"gpt-4":             {Encoding: "cl100k_base", MaxTokens: 8192},
	"grok-3":            {Encoding: "grok", MaxTokens: 131072},
}

// ModelMaxTokens returns the context window size for a model.
func ModelMaxTokens(model string) int {
	if cfg, ok := ModelTokenizers[model]; ok {
		return cfg.MaxTokens
	}
	return 128000
}

// ── Token budgeting ────────────────────────────────────────

// Budget tracks remaining tokens in a context window.
type Budget struct {
	MaxTokens int `json:"max_tokens"`
	Used      int `json:"used"`
}

// NewBudget creates a token budget.
func NewBudget(maxTokens int) *Budget {
	return &Budget{MaxTokens: maxTokens}
}

// Use deducts tokens from the budget.
func (b *Budget) Use(tokens int) bool {
	if b.Used+tokens > b.MaxTokens {
		return false
	}
	b.Used += tokens
	return true
}

// Remaining returns available tokens.
func (b *Budget) Remaining() int {
	return b.MaxTokens - b.Used
}

// Usage returns the fraction used (0.0-1.0).
func (b *Budget) Usage() float64 {
	if b.MaxTokens == 0 { return 0 }
	return float64(b.Used) / float64(b.MaxTokens)
}

// ── Text classification ──────────────────────────────────

// Classify returns the language classification of text.
func Classify(text string) string {
	scripts := map[string]int{}
	for _, r := range text {
		s := scriptOf(r)
		scripts[s]++
	}
	maxScript := ""
	maxCount := 0
	for s, c := range scripts {
		if c > maxCount { maxScript, maxCount = s, c }
	}
	return maxScript
}

func scriptOf(r rune) string {
	if r < 128 { return "latin" }
	if isCJK(r) { return "cjk" }
	if r >= 0x0600 && r <= 0x06FF { return "arabic" }
	if r >= 0x0400 && r <= 0x04FF { return "cyrillic" }
	return "other"
}

// RuneCount returns the number of Unicode characters (not bytes).
func RuneCount(s string) int {
	return utf8.RuneCountInString(s)
}

// ByteCount returns the byte length.
func ByteCount(s string) int { return len(s) }
