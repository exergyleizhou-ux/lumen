// Package compressor provides prompt compression strategies for reducing
// context window usage: deduplication, whitespace normalization, comment
// stripping, and intelligent summarization side-channel. Used by the agent
// when the context window is approaching its limit.
package compressor

import (
	"regexp"
	"strings"
	"sync"
)

// Strategy defines how text is compressed.
type Strategy int

const (
	StrategyNone         Strategy = iota
	StrategyWhitespace            // normalize whitespace
	StrategyComments              // strip comments
	StrategyDedup                 // remove duplicate lines
	StrategyAggressive            // all of the above
)

// Compressor reduces text size using the given strategy.
type Compressor struct {
	mu       sync.Mutex
	strategy Strategy
	stats    Stats
}

// Stats tracks compression effectiveness.
type Stats struct {
	OriginalBytes  int64 `json:"original_bytes"`
	CompressedBytes int64 `json:"compressed_bytes"`
	Calls int64 `json:"calls"`
}

// New creates a compressor with the given strategy.
func New(s Strategy) *Compressor {
	return &Compressor{strategy: s}
}

// Compress reduces the size of text.
func (c *Compressor) Compress(text string) string {
	if text == "" { return "" }
	original := len(text)

	var result string
	switch c.strategy {
	case StrategyWhitespace:
		result = compressWhitespace(text)
	case StrategyComments:
		result = stripComments(text)
	case StrategyDedup:
		result = dedupLines(text)
	case StrategyAggressive:
		result = dedupLines(stripComments(compressWhitespace(text)))
	default:
		result = text
	}

	c.mu.Lock()
	c.stats.OriginalBytes += int64(original)
	c.stats.CompressedBytes += int64(len(result))
	c.stats.Calls++
	c.mu.Unlock()

	return result
}

// CompressWithBudget compresses until the text fits within tokenBudget.
func (c *Compressor) CompressWithBudget(text string, tokenBudget int, estimator func(string) int) string {
	if text == "" || estimator(text) <= tokenBudget {
		return text
	}
	strategies := []Strategy{StrategyWhitespace, StrategyComments, StrategyDedup, StrategyAggressive}
	result := text
	for _, s := range strategies {
		c.strategy = s
		result = c.Compress(result)
		if estimator(result) <= tokenBudget {
			return result
		}
	}
	// Last resort: truncate
	words := strings.Fields(result)
	if len(words) <= 10 { return result }
	keep := tokenBudget / 2
	if keep > len(words) { keep = len(words) }
	return strings.Join(words[len(words)-keep:], " ")
}

func compressWhitespace(s string) string {
	s = regexp.MustCompile(`[ \t]+`).ReplaceAllString(s, " ")
	s = regexp.MustCompile(`\n{3,}`).ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

func stripComments(s string) string {
	// Strip Go-style comments
	s = regexp.MustCompile(`//.*`).ReplaceAllString(s, "")
	// Strip /* */ blocks
	s = regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(s, "")
	// Strip # comments (Python, Ruby, Shell)
	lines := strings.Split(s, "\n")
	var out []string
	for _, l := range lines {
		if !strings.HasPrefix(strings.TrimSpace(l), "#") {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

func dedupLines(s string) string {
	lines := strings.Split(s, "\n")
	seen := map[string]bool{}
	var out []string
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" || !seen[trimmed] {
			seen[trimmed] = true
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

// Stats returns compression statistics.
func (c *Compressor) StatsReport() Stats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stats
}

// Ratio returns the compression ratio as a percentage.
func (c *Compressor) Ratio() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stats.OriginalBytes == 0 { return 100 }
	return float64(c.stats.CompressedBytes) / float64(c.stats.OriginalBytes) * 100
}

// ── Context window helpers ──────────────────────────────

// EstimateTokens returns a rough token count.
func EstimateTokens(text string) int {
	if text == "" { return 0 }
	// ~4 characters per token for mixed English/CJK
	return len(text)/3 + 1
}

// FitToBudget reduces text to fit within a token budget.
func FitToBudget(text string, maxTokens int) string {
	if EstimateTokens(text) <= maxTokens {
		return text
	}
	c := New(StrategyAggressive)
	return c.CompressWithBudget(text, maxTokens, EstimateTokens)
}
