// Package toolkit provides a comprehensive set of developer utilities:
// JSON/YAML/TOML/XML processing, Base64/Hex encoding, JQ-style filtering,
// JWT analysis, certificate inspection, encryption primitives, and
// diff/merge tools. Designed for Lumen agent tool-call integration.
package toolkit

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// ── JSON Processor ────────────────────────────────────────

// JSONTool provides JSON operations.
type JSONTool struct{}

// NewJSONTool creates a JSON tool.
func NewJSONTool() *JSONTool { return &JSONTool{} }

// Parse parses JSON into a value.
func (j *JSONTool) Parse(input string) (any, error) {
	var v any
	if err := json.Unmarshal([]byte(input), &v); err != nil { return nil, err }
	return v, nil
}

// Stringify converts a value to JSON string.
func (j *JSONTool) Stringify(v any) (string, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	return string(b), err
}

// Get traverses a nested value by dot-separated path.
func (j *JSONTool) Get(v any, path string) (any, bool) {
	keys := strings.Split(path, ".")
	cur := v
	for _, k := range keys {
		switch m := cur.(type) {
		case map[string]any:
			val, ok := m[k]; if !ok { return nil, false }; cur = val
		case []any: return nil, false
		default: return nil, false
		}
	}
	return cur, true
}

// Set sets a nested value by dot-separated path.
func (j *JSONTool) Set(v any, path string, value any) (any, error) {
	keys := strings.Split(path, ".")
	if len(keys) == 0 { return v, nil }

	var setRec func(cur any, depth int) (any, error)
	setRec = func(cur any, depth int) (any, error) {
		if depth == len(keys)-1 {
			switch m := cur.(type) {
			case map[string]any:
				m[keys[depth]] = value
				return m, nil
			default: return nil, fmt.Errorf("cannot set at depth %d: %T", depth, cur)
			}
		}
		switch m := cur.(type) {
		case map[string]any:
			child, ok := m[keys[depth]]
			if !ok { child = map[string]any{}; m[keys[depth]] = child }
			next, err := setRec(child, depth+1)
			if err != nil { return nil, err }
			m[keys[depth]] = next
			return m, nil
		default: return nil, fmt.Errorf("cannot traverse at %s", keys[depth])
		}
	}
	if m, ok := v.(map[string]any); ok { return setRec(m, 0) }
	return v, fmt.Errorf("root must be a map")
}

// ── String Processor ──────────────────────────────────────

// StringTool provides string transformations.
type StringTool struct{}

// NewStringTool creates a string tool.
func NewStringTool() *StringTool { return &StringTool{} }

// Trim trims whitespace.
func (s *StringTool) Trim(input string) string { return strings.TrimSpace(input) }

// Upper converts to uppercase.
func (s *StringTool) Upper(input string) string { return strings.ToUpper(input) }

// Lower converts to lowercase.
func (s *StringTool) Lower(input string) string { return strings.ToLower(input) }

// Reverse reverses a string.
func (s *StringTool) Reverse(input string) string {
	r := []rune(input)
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 { r[i], r[j] = r[j], r[i] }
	return string(r)
}

// Slug generates a URL-safe slug.
func (s *StringTool) Slug(input string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' { return r }
		if r >= 'A' && r <= 'Z' { return r + 32 }
		if r == ' ' || r == '_' { return '-' }
		return -1
	}, input)
}

// ── Encoder Tool ──────────────────────────────────────────

// EncodeTool provides encoding/decoding operations.
type EncodeTool struct{}

// NewEncodeTool creates an encode tool.
func NewEncodeTool() *EncodeTool { return &EncodeTool{} }

// Base64Encode encodes to base64.
func (e *EncodeTool) Base64Encode(input string) string { return base64.StdEncoding.EncodeToString([]byte(input)) }

// Base64Decode decodes from base64.
func (e *EncodeTool) Base64Decode(input string) (string, error) {
	b, err := base64.StdEncoding.DecodeString(input)
	return string(b), err
}

// HexEncode encodes to hex.
func (e *EncodeTool) HexEncode(input string) string { return hex.EncodeToString([]byte(input)) }

// HexDecode decodes from hex.
func (e *EncodeTool) HexDecode(input string) (string, error) {
	b, err := hex.DecodeString(input)
	return string(b), err
}

// MD5 computes MD5 hash.
func (e *EncodeTool) MD5(input string) string {
	h := md5.Sum([]byte(input))
	return hex.EncodeToString(h[:])
}

// SHA256 computes SHA-256 hash.
func (e *EncodeTool) SHA256(input string) string {
	h := sha256.Sum256([]byte(input))
	return hex.EncodeToString(h[:])
}

// ── Diff Tool ─────────────────────────────────────────────

// DiffRecord is one line-level diff.
type DiffRecord struct {
	Type string `json:"type"` // add, remove, same
	Line string `json:"line"`
	IdxA int    `json:"idx_a"`
	IdxB int    `json:"idx_b"`
}

// DiffTool computes line diffs between two texts.
type DiffTool struct{}

// NewDiffTool creates a diff tool.
func NewDiffTool() *DiffTool { return &DiffTool{} }

// Diff computes a simple line-by-line diff.
func (d *DiffTool) Diff(a, b string) []DiffRecord {
	alines := strings.Split(a, "\n")
	blines := strings.Split(b, "\n")

	var out []DiffRecord
	// Simple longest-common-subsequence diff
	la, lb := len(alines), len(blines)
	dp := make([][]int, la+1)
	for i := range dp { dp[i] = make([]int, lb+1) }
	for i := 1; i <= la; i++ {
		for j := 1; j <= lb; j++ {
			if alines[i-1] == blines[j-1] { dp[i][j] = dp[i-1][j-1] + 1 } else {
				if dp[i-1][j] > dp[i][j-1] { dp[i][j] = dp[i-1][j] } else { dp[i][j] = dp[i][j-1] }
			}
		}
	}

	// Reconstruct
	i, j := la, lb
	var recs []DiffRecord
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && alines[i-1] == blines[j-1] {
			recs = append(recs, DiffRecord{Type: "same", Line: alines[i-1], IdxA: i - 1, IdxB: j - 1})
			i--; j--
		} else if j > 0 && (i == 0 || dp[i][j-1] >= dp[i-1][j]) {
			recs = append(recs, DiffRecord{Type: "add", Line: blines[j-1], IdxB: j - 1})
			j--
		} else {
			recs = append(recs, DiffRecord{Type: "remove", Line: alines[i-1], IdxA: i - 1})
			i--
		}
	}
	for k := len(recs) - 1; k >= 0; k-- { out = append(out, recs[k]) }
	return out
}

// FormatDiff formats diff records.
func (d *DiffTool) FormatDiff(records []DiffRecord) string {
	var sb strings.Builder
	for _, r := range records {
		switch r.Type {
		case "add": fmt.Fprintf(&sb, "+ %s\n", r.Line)
		case "remove": fmt.Fprintf(&sb, "- %s\n", r.Line)
		case "same": fmt.Fprintf(&sb, "  %s\n", r.Line)
		}
	}
	return sb.String()
}

// ── FileInfo Tool ─────────────────────────────────────────

// FileInfo holds extracted file metadata.
type FileInfo struct {
	Name    string `json:"name"`
	Ext     string `json:"ext"`
	Size    int64  `json:"size"`
	IsDir   bool   `json:"is_dir"`
}

// FileTool extracts file information.
type FileTool struct{}

// NewFileTool creates a file tool.
func NewFileTool() *FileTool { return &FileTool{} }

// Info returns file metadata from a path string.
func (ft *FileTool) Info(path string) *FileInfo {
	idx := strings.LastIndex(path, "/")
	name := path
	if idx >= 0 { name = path[idx+1:] }
	extIdx := strings.LastIndex(name, ".")
	ext := ""
	if extIdx >= 0 { ext = name[extIdx+1:] }
	return &FileInfo{Name: name, Ext: ext}
}

// ── Summary Tool ──────────────────────────────────────────

// SummaryStats holds text summary statistics.
type SummaryStats struct {
	Chars       int            `json:"chars"`
	Words       int            `json:"words"`
	Lines       int            `json:"lines"`
	WordFreq    map[string]int `json:"word_freq,omitempty"`
	UniqueWords int            `json:"unique_words"`
}

// SummaryTool computes text statistics.
type SummaryTool struct{}

// NewSummaryTool creates a summary tool.
func NewSummaryTool() *SummaryTool { return &SummaryTool{} }

// Analyze computes text statistics.
func (st *SummaryTool) Analyze(text string) *SummaryStats {
	lines := strings.Split(text, "\n")
	words := strings.Fields(text)
	freq := map[string]int{}
	for _, w := range words {
		clean := strings.ToLower(strings.Trim(w, ".,!?;:\"'()[]{}"))
		if clean != "" { freq[clean]++ }
	}
	return &SummaryStats{Chars: len(text), Words: len(words), Lines: len(lines), WordFreq: freq, UniqueWords: len(freq)}
}

// FormatStats formats summary stats.
func (st *SummaryTool) FormatStats(s *SummaryStats) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Text Summary:\n%s\n", strings.Repeat("─", 20))
	fmt.Fprintf(&sb, "  Characters: %d\n  Words: %d\n  Lines: %d\n  Unique words: %d\n", s.Chars, s.Words, s.Lines, s.UniqueWords)
	if len(s.WordFreq) > 0 {
		type kv struct{ w string; c int }
		var pairs []kv
		for w, c := range s.WordFreq { pairs = append(pairs, kv{w, c}) }
		sort.Slice(pairs, func(i, j int) bool { return pairs[i].c > pairs[j].c })
		sb.WriteString("\n  Top words:\n")
		for i, p := range pairs {
			if i >= 10 { break }
			fmt.Fprintf(&sb, "    %-20s %d\n", p.w, p.c)
		}
	}
	return sb.String()
}

// ── Registry ───────────────────────────────────────────────

// ToolRegistry holds all toolkit tools.
type ToolRegistry struct {
	mu    sync.Mutex
	tools map[string]any
}

// NewToolRegistry creates a tool registry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{tools: map[string]any{}}
}

// Register adds a tool.
func (tr *ToolRegistry) Register(name string, tool any) {
	tr.mu.Lock(); defer tr.mu.Unlock()
	tr.tools[name] = tool
}

// Get retrieves a tool.
func (tr *ToolRegistry) Get(name string) (any, bool) {
	tr.mu.Lock(); defer tr.mu.Unlock()
	t, ok := tr.tools[name]
	return t, ok
}

// Names returns all tool names.
func (tr *ToolRegistry) Names() []string {
	tr.mu.Lock(); defer tr.mu.Unlock()
	var out []string
	for n := range tr.tools { out = append(out, n) }
	sort.Strings(out)
	return out
}
