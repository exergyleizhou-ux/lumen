package anthro

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"lumen/internal/provider"
)

func TestAnthroRetriesTransient(t *testing.T) {
	// A transient 503 must be retried (like the openai provider), not aborted on
	// first hit. Previously anthro had no retry wrapper so a single 503 killed
	// the turn.
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&hits, 1) == 1 {
			w.WriteHeader(503)
			w.Write([]byte("overloaded"))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer srv.Close()

	prov, err := New(provider.Config{Name: "anthro", BaseURL: srv.URL, Model: "claude-x"})
	if err != nil {
		t.Fatal(err)
	}
	ch, _ := prov.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	for c := range ch {
		if c.Type == provider.ChunkError {
			t.Fatalf("transient 503 should recover, got error: %v", c.Err)
		}
	}
	if n := atomic.LoadInt32(&hits); n != 2 {
		t.Fatalf("want 2 requests (1 failed + 1 retry), got %d", n)
	}
}

func TestAnthroInBandError(t *testing.T) {
	// Anthropic can send an in-band error event (200 + {"type":"error",...}).
	// It must surface as a ChunkError, not a silent empty turn.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"overloaded\"}}\n\n"))
	}))
	defer srv.Close()

	prov, _ := New(provider.Config{Name: "anthro", BaseURL: srv.URL, Model: "claude-x"})
	ch, _ := prov.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	var gotErr error
	for c := range ch {
		if c.Type == provider.ChunkError {
			gotErr = c.Err
		}
	}
	if gotErr == nil {
		t.Fatal("in-band error event should surface as a ChunkError")
	}
	if !strings.Contains(gotErr.Error(), "overloaded") {
		t.Errorf("error should carry the provider message, got %q", gotErr.Error())
	}
}

func TestAnthroFailsFastOnPermanent(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(402)
		w.Write([]byte("insufficient balance"))
	}))
	defer srv.Close()

	prov, _ := New(provider.Config{Name: "anthro", BaseURL: srv.URL, Model: "claude-x"})
	ch, _ := prov.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	var gotErr error
	for c := range ch {
		if c.Type == provider.ChunkError {
			gotErr = c.Err
		}
	}
	if gotErr == nil {
		t.Fatal("expected an error chunk from 402")
	}
	if !strings.Contains(gotErr.Error(), "402") {
		t.Errorf("error should name the status, got %q", gotErr.Error())
	}
	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Fatalf("402 is permanent — want exactly 1 request, got %d", n)
	}
}
