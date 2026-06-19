package gemini

import (
	"context"
	"strings"
	"testing"

	"lumen/internal/provider"
)

func drainChunks(ch chan provider.Chunk) (errd bool, text string) {
	for c := range ch {
		switch c.Type {
		case provider.ChunkError:
			errd = true
		case provider.ChunkText:
			text += c.Text
		}
	}
	return
}

// When Gemini blocks a prompt it returns HTTP 200 with promptFeedback.blockReason
// and ZERO candidates. The stream decoded neither field, so the turn ended as a
// silent empty success. It must surface as an error.
func TestGeminiParseSSE_BlockedPromptEmitsError(t *testing.T) {
	ch := make(chan provider.Chunk, 32)
	r := strings.NewReader(`data: {"promptFeedback":{"blockReason":"SAFETY"}}` + "\n\n")
	(&Provider{}).parseSSE(context.Background(), r, ch)
	close(ch)
	if errd, _ := drainChunks(ch); !errd {
		t.Error("a blocked gemini prompt must emit ChunkError, not a silent done")
	}
}

// A per-candidate finishReason of SAFETY/RECITATION with no text must also error.
func TestGeminiParseSSE_SafetyFinishReasonEmitsError(t *testing.T) {
	ch := make(chan provider.Chunk, 32)
	r := strings.NewReader(`data: {"candidates":[{"finishReason":"SAFETY","content":{"parts":[]}}]}` + "\n\n")
	(&Provider{}).parseSSE(context.Background(), r, ch)
	close(ch)
	if errd, _ := drainChunks(ch); !errd {
		t.Error("a SAFETY finishReason must emit ChunkError")
	}
}

// A cancelled/timed-out stream must emit ChunkError (ctx.Err()), matching the
// openai provider — not a clean ChunkDone that looks like a successful turn.
func TestGeminiParseSSE_CancelEmitsError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ch := make(chan provider.Chunk, 32)
	r := strings.NewReader(`data: {"candidates":[{"content":{"parts":[{"text":"hi"}]}}]}` + "\n\n")
	(&Provider{}).parseSSE(ctx, r, ch)
	close(ch)
	if errd, _ := drainChunks(ch); !errd {
		t.Error("a cancelled gemini stream must emit ChunkError, not a clean done")
	}
}
