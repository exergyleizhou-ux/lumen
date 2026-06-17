package gemini

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"lumen/internal/provider"
)

func TestGeminiRetriesTransient(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&hits, 1) == 1 {
			w.WriteHeader(503)
			w.Write([]byte("UNAVAILABLE"))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
	}))
	defer srv.Close()

	prov, err := New(provider.Config{Name: "gemini", BaseURL: srv.URL, Model: "gemini-x"})
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

func TestGeminiFailsFastOnPermanent(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(400)
		w.Write([]byte("bad request"))
	}))
	defer srv.Close()

	prov, _ := New(provider.Config{Name: "gemini", BaseURL: srv.URL, Model: "gemini-x"})
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
		t.Fatal("expected an error chunk from 400")
	}
	if !strings.Contains(gotErr.Error(), "400") {
		t.Errorf("error should name the status, got %q", gotErr.Error())
	}
	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Fatalf("400 is permanent — want exactly 1 request, got %d", n)
	}
}
