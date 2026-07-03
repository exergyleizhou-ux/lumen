package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// anthropicKeyOrder keeps a stable top-level field order so DeepSeek prefix-cache
// sees a consistent wire shape across turns (Go's map marshal order is random).
var anthropicKeyOrder = []string{
	"model", "max_tokens", "messages", "system", "tools", "tool_choice",
	"thinking", "stream", "temperature", "top_p", "top_k", "stop_sequences",
	"metadata",
}

// PatchAnthropicBodyRaw applies only the mutations DeepSeek needs (model remap,
// thinking normalize, max_tokens clamp) while preserving the original bytes of
// messages/system/tools and all other untouched fields — critical for prefix-cache hit rate.
func PatchAnthropicBodyRaw(raw []byte, spec ProviderSpec, cacheBoost bool) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	if cacheBoost {
		if sys, ok := fields["system"]; ok {
			fields["system"] = boostSystemCache(sys)
		}
		if tools, ok := fields["tools"]; ok {
			fields["tools"] = boostToolsCache(tools)
		}
	}
	srcModel := rawStringField(fields["model"])
	target := ResolveModel(spec, srcModel)
	fields["model"] = json.RawMessage(mustQuote(target))

	if mt := rawIntField(fields["max_tokens"]); mt > 0 {
		clamped := ClampMaxTokens(spec, mt, target)
		fields["max_tokens"] = json.RawMessage(fmt.Sprintf("%d", clamped))
	}

	if th := patchThinking(fields["thinking"], fields["tool_choice"]); len(th) > 0 {
		fields["thinking"] = th
	} else {
		delete(fields, "thinking")
	}

	return stableMarshal(fields, anthropicKeyOrder), nil
}

func patchThinking(thinkingRaw, toolChoiceRaw json.RawMessage) json.RawMessage {
	forcing := false
	if len(toolChoiceRaw) > 0 {
		var tc map[string]any
		if json.Unmarshal(toolChoiceRaw, &tc) == nil {
			t, _ := tc["type"].(string)
			forcing = t == "any" || t == "tool"
		}
	}
	if forcing {
		return json.RawMessage(`{"type":"disabled"}`)
	}
	if len(thinkingRaw) == 0 {
		return thinkingRaw
	}
	var th map[string]any
	if json.Unmarshal(thinkingRaw, &th) != nil {
		return thinkingRaw
	}
	if th["type"] == "auto" {
		th["type"] = "adaptive"
		b, _ := json.Marshal(th)
		return b
	}
	return thinkingRaw
}

func rawStringField(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return ""
}

func rawIntField(raw json.RawMessage) int {
	if len(raw) == 0 {
		return 0
	}
	var n int
	if json.Unmarshal(raw, &n) == nil {
		return n
	}
	var f float64
	if json.Unmarshal(raw, &f) == nil {
		return int(f)
	}
	return 0
}

func mustQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func stableMarshal(fields map[string]json.RawMessage, preferred []string) []byte {
	var buf bytes.Buffer
	buf.WriteByte('{')
	first := true
	seen := map[string]bool{}
	writeField := func(k string, v json.RawMessage) {
		if !first {
			buf.WriteByte(',')
		}
		first = false
		buf.WriteString(mustQuote(k))
		buf.WriteByte(':')
		buf.Write(v)
	}
	for _, k := range preferred {
		if v, ok := fields[k]; ok {
			writeField(k, v)
			seen[k] = true
		}
	}
	rest := make([]string, 0, len(fields))
	for k := range fields {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	sortStrings(rest)
	for _, k := range rest {
		writeField(k, fields[k])
	}
	buf.WriteByte('}')
	return buf.Bytes()
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}