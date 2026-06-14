package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"

	"lumen/internal/provider"
)

// PrefixShape describes the cache-stable prefix that DeepSeek's automatic
// prefix-cache engine sees. If any field changes between two requests, the
// entire prefix is re-evaluated and that turn's cache-hit rate drops to 0%.
// Tracking the shape lets us explain cache misses to the user.
type PrefixShape struct {
	SystemPromptHash string `json:"system_prompt_hash"`
	ToolSchemaHash   string `json:"tool_schema_hash"`
	FirstUserHash    string `json:"first_user_hash"`
	LastCompaction   string `json:"last_compaction,omitempty"`
}

// Equals reports whether two shapes are identical.
func (s PrefixShape) Equals(o PrefixShape) bool {
	return s.SystemPromptHash == o.SystemPromptHash &&
		s.ToolSchemaHash == o.ToolSchemaHash &&
		s.FirstUserHash == o.FirstUserHash &&
		s.LastCompaction == o.LastCompaction
}

// Diff returns a human-readable explanation of what changed between two shapes.
func (s PrefixShape) Diff(o PrefixShape) string {
	var diffs []string
	if s.SystemPromptHash != o.SystemPromptHash {
		diffs = append(diffs, "system prompt changed")
	}
	if s.ToolSchemaHash != o.ToolSchemaHash {
		diffs = append(diffs, "tool schemas changed (new tool added or schema edited?)")
	}
	if s.FirstUserHash != o.FirstUserHash {
		diffs = append(diffs, "user message history changed (compaction or repair?)")
	}
	if s.LastCompaction != o.LastCompaction {
		diffs = append(diffs, "session was compacted")
	}
	if len(diffs) == 0 {
		return "no detectable difference"
	}
	return strings.Join(diffs, "; ")
}

// ── Cache tracker ──────────────────────────────────────────

// cacheTracker monitors prefix-cache stability across turns. It computes the
// prefix shape before each API call, compares it to the previous turn's shape,
// and emits diagnostics when the prefix churns (the next turn will have 0%
// cache hit — and now the user knows why).
type cacheTracker struct {
	mu           sync.Mutex
	lastShape    *PrefixShape
	turnCount    int
	churnReasons []string // collected reasons for /cache command
}

func newCacheTracker() *cacheTracker { return &cacheTracker{} }

// check computes the current prefix shape and returns the previous one for
// comparison. If the shape changed, it records the reason. The caller should
// emit a notice when churn is detected.
func (c *cacheTracker) check(sysPrompt string, schemas []provider.ToolSchema, firstUser string, compacted bool) (prev *PrefixShape, churn bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	shape := computeShape(sysPrompt, schemas, firstUser, compacted)
	c.turnCount++

	if c.lastShape == nil {
		c.lastShape = &shape
		return nil, false // first turn — no previous shape to compare
	}

	prevShape := *c.lastShape
	if !shape.Equals(prevShape) {
		reason := prevShape.Diff(shape)
		c.churnReasons = append(c.churnReasons, fmt.Sprintf("turn %d: %s", c.turnCount, reason))
		c.lastShape = &shape
		return &prevShape, true
	}

	return &prevShape, false
}

// reasons returns all recorded churn reasons for diagnostics.
func (c *cacheTracker) reasons() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.churnReasons))
	copy(out, c.churnReasons)
	return out
}

// hitRate returns the session-level cache hit rate from atomic counters.
func (c *cacheTracker) hitRate(hit, miss int64) float64 {
	total := hit + miss
	if total == 0 {
		return 0
	}
	return float64(hit) / float64(total) * 100
}

// computeShape builds a deterministic hash of the cache-stable prefix.
func computeShape(sysPrompt string, schemas []provider.ToolSchema, firstUser string, compacted bool) PrefixShape {
	h := sha256.New()

	h.Write([]byte(sysPrompt))

	// Sort schemas by name for stability
	names := make([]string, len(schemas))
	for i, s := range schemas {
		names[i] = s.Name
	}
	sort.Strings(names)
	for _, name := range names {
		for _, s := range schemas {
			if s.Name == name {
				h.Write([]byte(s.Name))
				h.Write([]byte(s.Description))
				h.Write(s.Parameters)
				break
			}
		}
	}
	schemaHash := hex.EncodeToString(h.Sum(nil))
	h.Reset()

	h.Write([]byte(firstUser))
	firstUserHash := hex.EncodeToString(h.Sum(nil))

	shape := PrefixShape{
		SystemPromptHash: sha256Hex([]byte(sysPrompt)),
		ToolSchemaHash:   schemaHash,
		FirstUserHash:    firstUserHash,
	}
	if compacted {
		shape.LastCompaction = "compacted"
	}
	return shape
}

func sha256Hex(data []byte) string {
	s := sha256.Sum256(data)
	return hex.EncodeToString(s[:])
}
