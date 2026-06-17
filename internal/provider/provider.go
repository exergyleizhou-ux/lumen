// Package provider defines the model-backend abstraction. Concrete
// implementations live in subpackages (e.g. provider/openai) and self-register
// via init(). The core resolves providers by kind from config.
package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// Role is the role of a message.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message is a single conversation message.
type Message struct {
	Role               Role       `json:"role"`
	Content            string     `json:"content,omitempty"`
	Images             []string   `json:"images,omitempty"`
	ReasoningContent   string     `json:"reasoning_content,omitempty"`
	ReasoningSignature string     `json:"reasoning_signature,omitempty"`
	ToolCalls          []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID         string     `json:"tool_call_id,omitempty"`
	Name               string     `json:"name,omitempty"`
}

// ToolCall is a tool invocation requested by the model. Arguments is raw JSON.
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolSchema is a tool definition exposed to the model.
type ToolSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// Request is a single completion request.
type Request struct {
	Messages    []Message
	Tools       []ToolSchema
	Temperature float64
	MaxTokens   int
}

// ChunkType identifies the kind of a streamed increment.
type ChunkType int

const (
	ChunkText ChunkType = iota
	ChunkReasoning
	ChunkToolCallStart
	ChunkToolCall
	ChunkUsage
	ChunkDone
	ChunkError
)

// Usage reports token accounting for a completion.
type Usage struct {
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	TotalTokens      int    `json:"total_tokens"`
	CacheHitTokens   int    `json:"cache_hit_tokens"`  // normalize: DeepSeek prompt_cache_hit_tokens or OpenAI cached_tokens
	CacheMissTokens  int    `json:"cache_miss_tokens"` // normalize: DeepSeek prompt_cache_miss_tokens or uncached part
	ReasoningTokens  int    `json:"reasoning_tokens"`
	FinishReason     string `json:"finish_reason,omitempty"`
}

// Pricing is a provider's per-1M-token rates.
type Pricing struct {
	CacheHit float64 `toml:"cache_hit"`
	Input    float64 `toml:"input"`
	Output   float64 `toml:"output"`
	Currency string  `toml:"currency"`
}

// Cost estimates the spend for a usage record.
func (p *Pricing) Cost(u *Usage) float64 {
	if p == nil || u == nil {
		return 0
	}
	return (float64(u.CacheHitTokens)*p.CacheHit +
		float64(u.CacheMissTokens)*p.Input +
		float64(u.CompletionTokens)*p.Output) / 1e6
}

// Chunk is a single streamed event.
type Chunk struct {
	Type      ChunkType
	Text      string
	Signature string
	ToolCall  *ToolCall
	Usage     *Usage
	Err       error
}

// StreamInterruptedError marks a recoverable transport cut after output had
// already been received.
type StreamInterruptedError struct {
	Err error
}

func (e *StreamInterruptedError) Error() string {
	if e == nil || e.Err == nil {
		return "stream interrupted"
	}
	return e.Err.Error()
}

func (e *StreamInterruptedError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func IsStreamInterrupted(err error) bool {
	var interrupted *StreamInterruptedError
	return errors.As(err, &interrupted)
}

// Provider is a chat-capable model backend.
type Provider interface {
	Name() string
	Stream(ctx context.Context, req Request) (<-chan Chunk, error)
}

// Config is a resolved provider instance configuration.
type Config struct {
	Name    string
	BaseURL string
	Model   string
	APIKey  string
}

// AuthError reports that a provider rejected the API key (HTTP 401/403).
type AuthError struct {
	Provider string
	KeyEnv   string
	Status   int
	HasKey   bool
}

func (e *AuthError) Error() string {
	key := "the API key"
	if e.KeyEnv != "" {
		key = e.KeyEnv
	}
	return fmt.Sprintf("authentication failed for provider %q (HTTP %d): %s is invalid or expired",
		e.Provider, e.Status, key)
}

// APIError reports a non-auth HTTP error from a provider. Retryable is true for
// transient failures (429, 503, 5xx) that may succeed on retry, and false for
// permanent ones (e.g. 402 Insufficient Balance, 400 Bad Request) that will not.
type APIError struct {
	Provider  string
	Status    int
	Body      string
	Retryable bool
}

func (e *APIError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("HTTP %d (provider %s)", e.Status, e.Provider)
	}
	return fmt.Sprintf("HTTP %d: %s", e.Status, e.Body)
}

// Factory builds a Provider from a resolved Config.
type Factory func(cfg Config) (Provider, error)

var registry = map[string]Factory{}

// Register adds a factory under a kind (e.g. "openai").
func Register(kind string, f Factory) {
	if _, dup := registry[kind]; dup {
		panic("provider: duplicate kind " + kind)
	}
	registry[kind] = f
}

// New instantiates the provider of the given kind.
func New(kind string, cfg Config) (Provider, error) {
	f, ok := registry[kind]
	if !ok {
		return nil, fmt.Errorf("provider: unknown kind %q (registered: %v)", kind, Kinds())
	}
	return f(cfg)
}

// Kinds returns the registered kinds, sorted.
func Kinds() []string {
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// CanonicalizeSchema normalizes a JSON Schema so identical shapes produce
// byte-identical output, keeping the prefix cache stable.
func CanonicalizeSchema(schema json.RawMessage) json.RawMessage {
	var v any
	if err := json.Unmarshal(schema, &v); err != nil {
		return schema
	}
	b, err := json.Marshal(v)
	if err != nil {
		return schema
	}
	return json.RawMessage(b)
}

// sanitizeToolPairing ensures every assistant tool_calls turn is followed by
// matching tool result messages (required by OpenAI/DeepSeek API).
func SanitizeToolPairing(msgs []Message) []Message {
	out := make([]Message, 0, len(msgs))
	for i := 0; i < len(msgs); {
		m := msgs[i]
		if m.Role == RoleAssistant && len(m.ToolCalls) > 0 {
			j := i + 1
			for j < len(msgs) && msgs[j].Role == RoleTool {
				j++
			}
			out = append(out, m)
			// Keep the real results, then backfill a placeholder for ANY tool_call
			// that has no matching result — a single unpaired call 400s the turn,
			// and a partial set (some calls answered, some not) is just as invalid.
			results := msgs[i+1 : j]
			out = append(out, results...)
			present := make(map[string]bool, len(results))
			for _, r := range results {
				present[r.ToolCallID] = true
			}
			for _, tc := range m.ToolCalls {
				if !present[tc.ID] {
					out = append(out, Message{
						Role:       RoleTool,
						ToolCallID: tc.ID,
						Name:       tc.Name,
						Content:    "[no result: interrupted]",
					})
				}
			}
			i = j
			continue
		}
		if m.Role == RoleTool {
			i++ // orphan tool message — drop
			continue
		}
		out = append(out, m)
		i++
	}
	return out
}

func parseImageDataURL(dataURL string) (mediaType, base64Data string, ok bool) {
	rest, found := strings.CutPrefix(dataURL, "data:")
	if !found {
		return "", "", false
	}
	meta, payload, found := strings.Cut(rest, ",")
	if !found {
		return "", "", false
	}
	mt, found := strings.CutSuffix(meta, ";base64")
	if !found || mt == "" {
		return "", "", false
	}
	return mt, payload, true
}

func currencySymbol(currency string) string {
	value := strings.TrimSpace(currency)
	if value == "" {
		return "¥"
	}
	switch strings.ToLower(value) {
	case "cny", "rmb", "yuan", "renminbi":
		return "¥"
	case "usd", "dollar":
		return "$"
	case "eur", "euro":
		return "€"
	}
	for _, r := range value {
		if unicode.Is(unicode.Sc, r) {
			return value
		}
	}
	return "¥"
}
