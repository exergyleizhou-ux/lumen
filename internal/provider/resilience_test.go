package provider

import (
	"errors"
	"testing"
)

func TestClassifyHTTPError(t *testing.T) {
	cases := []struct {
		status        int
		wantAuth      bool
		wantRetryable bool
	}{
		{401, true, false},
		{403, true, false},
		{402, false, false}, // Insufficient Balance — permanent
		{400, false, false},
		{404, false, false},
		{422, false, false},
		{429, false, true}, // rate limit — transient
		{503, false, true},
		{500, false, true},
		{502, false, true},
		{529, false, true}, // Anthropic overloaded
	}
	for _, c := range cases {
		err := ClassifyHTTPError("p", c.status, []byte("body"))
		if err == nil {
			t.Fatalf("status %d: expected error", c.status)
		}
		var ae *AuthError
		if c.wantAuth {
			if !errors.As(err, &ae) {
				t.Errorf("status %d: want AuthError, got %T", c.status, err)
			}
			continue
		}
		var api *APIError
		if !errors.As(err, &api) {
			t.Fatalf("status %d: want APIError, got %T", c.status, err)
		}
		if api.Retryable != c.wantRetryable {
			t.Errorf("status %d: Retryable=%v, want %v", c.status, api.Retryable, c.wantRetryable)
		}
		if api.Status != c.status {
			t.Errorf("status %d: APIError.Status=%d", c.status, api.Status)
		}
	}
}

func TestStreamWithRetryFailsFastOnPermanent(t *testing.T) {
	ch := make(chan Chunk, 4)
	calls := 0
	StreamWithRetry(nil, ch, func(attempt int) error {
		calls++
		return &APIError{Provider: "p", Status: 402, Retryable: false}
	})
	close(ch)
	if calls != 1 {
		t.Fatalf("permanent error must not retry; calls=%d, want 1", calls)
	}
	if !hasChunkError(ch) {
		t.Error("expected a terminal ChunkError")
	}
}

func TestStreamWithRetryFailsFastOnAuth(t *testing.T) {
	ch := make(chan Chunk, 4)
	calls := 0
	StreamWithRetry(nil, ch, func(attempt int) error {
		calls++
		return &AuthError{Provider: "p", Status: 401}
	})
	close(ch)
	if calls != 1 {
		t.Fatalf("auth error must not retry; calls=%d, want 1", calls)
	}
	if !hasChunkError(ch) {
		t.Error("expected a terminal ChunkError")
	}
}

func TestStreamWithRetrySucceedsNoError(t *testing.T) {
	ch := make(chan Chunk, 4)
	calls := 0
	StreamWithRetry(nil, ch, func(attempt int) error {
		calls++
		ch <- Chunk{Type: ChunkText, Text: "ok"}
		return nil
	})
	close(ch)
	if calls != 1 {
		t.Fatalf("success path: calls=%d, want 1", calls)
	}
	if hasChunkError(ch) {
		t.Error("success path must not emit a ChunkError")
	}
}

func TestStreamWithRetryRecoversAfterTransient(t *testing.T) {
	ch := make(chan Chunk, 8)
	calls := 0
	StreamWithRetry(nil, ch, func(attempt int) error {
		calls++
		if calls == 1 {
			return &APIError{Provider: "p", Status: 503, Retryable: true}
		}
		ch <- Chunk{Type: ChunkText, Text: "recovered"}
		return nil
	})
	close(ch)
	if calls != 2 {
		t.Fatalf("transient error should retry once then succeed; calls=%d, want 2", calls)
	}
	if hasChunkError(ch) {
		t.Error("recovered run must not emit a ChunkError")
	}
}

func hasChunkError(ch <-chan Chunk) bool {
	for c := range ch {
		if c.Type == ChunkError {
			return true
		}
	}
	return false
}
