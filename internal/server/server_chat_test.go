// Chat demo-guard tests (goal:d6aa846b round9).
package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"lumen/internal/control"
)

func TestHandleChatDemoSkipsWhenAPIKeyPresent(t *testing.T) {
	os.Setenv("LUMEN_DEMO", "1")
	defer os.Unsetenv("LUMEN_DEMO")

	ctrl := control.New()
	s, err := New(Config{Addr: ":0", Ctrl: ctrl})
	if err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]any{
		"prompt":   "hello",
		"api_key":  "sk-test-key",
		"provider": "deepseek",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleChat(rec, req)

	out := rec.Body.String()
	if strings.Contains(out, "[Demo mode] You said:") {
		t.Fatalf("demo echo should not run when api_key is set:\n%s", out)
	}
	if !strings.Contains(out, "no providers configured") {
		t.Fatalf("expected configure error via SSE, got:\n%s", out)
	}
}

func TestHandleChatDemoEchoWithoutAPIKey(t *testing.T) {
	os.Setenv("LUMEN_DEMO", "1")
	defer os.Unsetenv("LUMEN_DEMO")

	ctrl := control.New()
	s, err := New(Config{Addr: ":0", Ctrl: ctrl})
	if err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]any{"prompt": "ping"})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleChat(rec, req)

	out := rec.Body.String()
	if !strings.Contains(out, "[Demo mode] You said: ping") {
		t.Fatalf("expected demo echo without api_key:\n%s", out)
	}
}