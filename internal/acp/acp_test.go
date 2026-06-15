package acp

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"
)

type testHandler struct{}

func (h *testHandler) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	return ChatResponse{SessionID: "test", Content: "echo: " + req.Message}, nil
}
func (h *testHandler) Diagnostics(ctx context.Context, req DiagnosticRequest) (DiagnosticResponse, error) {
	return DiagnosticResponse{FilePath: req.FilePath, Items: []DiagItem{{Line: 1, Message: "test", Severity: 2}}}, nil
}
func (h *testHandler) Completion(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	return CompletionResponse{Items: []CompletionItem{{Text: "test", Label: "Test"}}}, nil
}
func (h *testHandler) Status(ctx context.Context) StatusResponse {
	return StatusResponse{Name: "Lumen", Version: "0.2.0", Model: "test", Ready: true}
}

func TestServerTCP(t *testing.T) {
	s := NewServer(&testHandler{})
	err := s.ListenTCP("127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenTCP: %v", err)
	}
	defer s.Shutdown()

	// Get the actual address
	addr := s.ln.Addr().String()
	t.Logf("ACP listening on %s", addr)

	// Connect and send a request
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	req := request{JSONRPC: "2.0", ID: 1, Method: "status"}
	data, _ := json.Marshal(req)
	data = append(data, '\n')
	conn.Write(data)

	// Read response
	var resp response
	decoder := json.NewDecoder(conn)
	if err := decoder.Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ID != 1 || resp.Error != nil {
		t.Errorf("unexpected response: id=%d err=%v", resp.ID, resp.Error)
	}
}

func TestServerChat(t *testing.T) {
	s := NewServer(&testHandler{})
	s.ListenTCP("127.0.0.1:0")
	defer s.Shutdown()

	addr := s.ln.Addr().String()
	conn, _ := net.DialTimeout("tcp", addr, 2*time.Second)
	defer conn.Close()

	params, _ := json.Marshal(ChatRequest{Message: "hello"})
	req := request{JSONRPC: "2.0", ID: 2, Method: "chat/send", Params: params}
	data, _ := json.Marshal(req)
	conn.Write(append(data, '\n'))

	var resp response
	json.NewDecoder(conn).Decode(&resp)
	if resp.Error != nil {
		t.Errorf("chat error: %v", resp.Error)
	}
}

func TestServerUnknownMethod(t *testing.T) {
	s := NewServer(&testHandler{})
	s.ListenTCP("127.0.0.1:0")
	defer s.Shutdown()

	addr := s.ln.Addr().String()
	conn, _ := net.DialTimeout("tcp", addr, 2*time.Second)
	defer conn.Close()

	req := request{JSONRPC: "2.0", ID: 3, Method: "unknown/method"}
	data, _ := json.Marshal(req)
	conn.Write(append(data, '\n'))

	var resp response
	json.NewDecoder(conn).Decode(&resp)
	if resp.Error == nil {
		t.Error("unknown method should return error")
	}
}

func TestDefaultHandler(t *testing.T) {
	h := NewDefaultHandler("test-model")
	status := h.Status(context.Background())
	if status.Name != "Lumen" {
		t.Errorf("name: got %s", status.Name)
	}
	if !status.Ready {
		t.Error("handler should be ready")
	}
}

func TestZedConfig(t *testing.T) {
	cfg := ZedConfig("/tmp/lumen.sock")
	if len(cfg) == 0 {
		t.Error("ZedConfig should return non-empty JSON")
	}
}
