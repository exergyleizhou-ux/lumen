package agent

import (
	"strings"
	"testing"

	"lumen/internal/provider"
)

// The token estimate drives auto-compaction. It previously ignored image
// payloads (large base64/URL strings) and the tool schemas sent on every
// request, so on multimodal or many-tool runs it under-counted badly and
// compaction fired late or never.
func TestEstimateTokens_CountsImagesAndSchemas(t *testing.T) {
	base := estimateTokens([]provider.Message{{Content: "hi"}}, nil)

	withImg := estimateTokens([]provider.Message{{Content: "hi", Images: []string{strings.Repeat("x", 3000)}}}, nil)
	if withImg <= base {
		t.Errorf("images must add to the estimate: base=%d withImg=%d", base, withImg)
	}

	withSchema := estimateTokens(
		[]provider.Message{{Content: "hi"}},
		[]provider.ToolSchema{{Name: "t", Parameters: []byte(strings.Repeat("x", 3000))}},
	)
	if withSchema <= base {
		t.Errorf("tool schemas must add to the estimate: base=%d withSchema=%d", base, withSchema)
	}
}
