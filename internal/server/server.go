// Package server provides an HTTP+SSE server for Lumen.
// It wraps the control.Controller and exposes:
//
//	GET  /            — web UI (embedded HTML/JS)
//	POST /v1/chat     — SSE-streaming chat completion
//	GET  /v1/models   — list available models
//	GET  /v1/status   — agent status (running/idle)
//	GET  /v1/sessions — list recent sessions
//	POST /v1/memories — list/save/delete memories
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"lumen/internal/control"
	"lumen/internal/event"
	"lumen/internal/memory"
)

// Config holds the server configuration.
type Config struct {
	Addr   string // listen address, e.g. ":8080"
	Ctrl   *control.Controller
	Static string // path to static files (empty = use embedded)
}

// Server wraps the HTTP server.
type Server struct {
	cfg    Config
	mux    *http.ServeMux
	memDir string
	// turnMu serializes chat turns: every request shares one Controller/Agent/
	// Session, which Configure+Run mutate without internal locking. Without this,
	// two concurrent POST /v1/chat requests race those fields and interleave
	// messages into one session.
	turnMu sync.Mutex
}

// New creates a new Server.
func New(cfg Config) (*Server, error) {
	s := &Server{cfg: cfg, mux: http.NewServeMux()}
	s.memDir = filepath.Join(os.ExpandEnv("$HOME"), ".lumen", "memories")
	s.routes()
	return s, nil
}

// ListenAndServe starts the HTTP server. Blocks until error.
func (s *Server) ListenAndServe() error {
	log.Printf("lumen serve: listening on %s", s.cfg.Addr)
	log.Printf("  web UI:  http://localhost%s/", s.cfg.Addr)
	log.Printf("  API:     http://localhost%s/v1/chat", s.cfg.Addr)
	return http.ListenAndServe(s.cfg.Addr, s.mux)
}

// ── Routes ──────────────────────────────────────────────────

func (s *Server) routes() {
	s.mux.HandleFunc("/", s.handleIndex)
	s.mux.HandleFunc("/v1/chat", s.handleChat)
	s.mux.HandleFunc("/v1/models", s.handleModels)
	s.mux.HandleFunc("/v1/status", s.handleStatus)
	s.mux.HandleFunc("/v1/sessions", s.handleSessions)
	s.mux.HandleFunc("/v1/memories", s.handleMemories)
}

// ── Web UI ──────────────────────────────────────────────────

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(indexHTML))
}

// ── SSE Chat ────────────────────────────────────────────────

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Prompt string   `json:"prompt"`
		Images []string `json:"images"` // base64-encoded images
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Prompt == "" {
		http.Error(w, `{"error":"prompt required"}`, http.StatusBadRequest)
		return
	}

	// SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// SSE sink — emits each event as an SSE data frame
	sink := sseSink{w: w, flusher: flusher}

	// Serialize turns: the shared Controller/Agent/Session is not safe for
	// concurrent Configure+Run. One chat at a time (acceptable for a single-
	// session agent); concurrent requests queue here rather than corrupt state.
	s.turnMu.Lock()
	defer s.turnMu.Unlock()

	s.cfg.Ctrl.Configure(sink, nil, "")

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	sink.emit("turn_started", "")
	s.cfg.Ctrl.Run(ctx, req.Prompt)
	sink.emit("turn_done", "")

	// Send terminal event
	fmt.Fprintf(w, "event: done\ndata: {}\n\n")
	flusher.Flush()
}

type sseSink struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

func (s sseSink) Emit(e event.Event) {
	data, _ := json.Marshal(map[string]any{
		"kind":      e.Kind,
		"text":      e.Text,
		"tool":      e.Tool,
		"usage":     e.Usage,
		"timestamp": e.Timestamp,
	})
	fmt.Fprintf(s.w, "data: %s\n\n", data)
	s.flusher.Flush()
}

func (s sseSink) emit(kind, text string) {
	data, _ := json.Marshal(map[string]any{"kind": kind, "text": text})
	fmt.Fprintf(s.w, "data: %s\n\n", data)
	s.flusher.Flush()
}

// ── REST ────────────────────────────────────────────────────

func (s *Server) handleModels(w http.ResponseWriter, _ *http.Request) {
	// List model presets from the controller
	ctrl := s.cfg.Ctrl
	jsonOK(w, map[string]any{
		"provider":  ctrl.ProviderName(),
		"model":     ctrl.ModelName(),
		"mode":      string(ctrl.PermissionMode()),
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	jsonOK(w, map[string]any{
		"status": "ok",
		"time":   time.Now().Format(time.RFC3339),
	})
}

func (s *Server) handleSessions(w http.ResponseWriter, _ *http.Request) {
	histDir := filepath.Join(os.ExpandEnv("$HOME"), ".lumen", "history")
	entries, _ := os.ReadDir(histDir)
	var sessions []map[string]any
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, _ := e.Info()
		sessions = append(sessions, map[string]any{
			"name":  e.Name(),
			"size":  info.Size(),
			"mtime": info.ModTime().Format(time.RFC3339),
		})
	}
	jsonOK(w, map[string]any{"sessions": sessions})
}

func (s *Server) handleMemories(w http.ResponseWriter, r *http.Request) {
	store, err := memory.NewStore(s.memDir)
	if err != nil {
		jsonErr(w, err.Error(), http.StatusInternalServerError)
		return
	}

	switch r.Method {
	case http.MethodGet:
		jsonOK(w, map[string]any{"memories": store.List()})
	case http.MethodPost:
		var req struct {
			Action string       `json:"action"` // "save" or "delete"
			Entry  memory.Entry `json:"entry"`
			Name   string       `json:"name"` // for delete
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonErr(w, err.Error(), http.StatusBadRequest)
			return
		}
		switch req.Action {
		case "save":
			if err := store.Save(req.Entry); err != nil {
				jsonErr(w, err.Error(), http.StatusInternalServerError)
				return
			}
			jsonOK(w, map[string]any{"saved": req.Entry.Name})
		case "delete":
			if err := store.Delete(req.Name); err != nil {
				jsonErr(w, err.Error(), http.StatusInternalServerError)
				return
			}
			jsonOK(w, map[string]any{"deleted": req.Name})
		default:
			jsonErr(w, "unknown action", http.StatusBadRequest)
		}
	default:
		jsonErr(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ── Helpers ─────────────────────────────────────────────────

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func jsonErr(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// ── Embedded web UI ─────────────────────────────────────────

const indexHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Lumen Chat</title>
<style>
:root {
  --bg: #0d1117;
  --fg: #c9d1d9;
  --dim: #8b949e;
  --cyan: #58a6ff;
  --green: #3fb950;
  --yellow: #d2991d;
  --red: #f85149;
  --magenta: #bc8cff;
  --border: #30363d;
  --input-bg: #161b22;
  --code-bg: #1c2128;
}
* { box-sizing: border-box; margin: 0; padding: 0; }
body {
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Helvetica, Arial, sans-serif;
  background: var(--bg);
  color: var(--fg);
  display: flex; flex-direction: column; height: 100vh;
}
header {
  padding: 12px 20px;
  border-bottom: 1px solid var(--border);
  display: flex; align-items: center; gap: 12px;
  flex-shrink: 0;
}
.logo { color: var(--cyan); font-weight: 700; font-size: 16px; }
.logo span { color: var(--fg); }
.model { color: var(--dim); font-size: 13px; }
#chat { flex: 1; overflow-y: auto; padding: 20px; }
#chat .msg { margin-bottom: 16px; animation: fadeIn 0.2s; }
@keyframes fadeIn { from { opacity: 0; transform: translateY(4px); } to { opacity: 1; transform: translateY(0); } }
.msg.user { text-align: right; }
.msg.user .body { background: var(--cyan); color: #fff; display: inline-block; padding: 8px 14px; border-radius: 16px; max-width: 75%; }
.msg.assistant .body { padding: 4px 0; line-height: 1.6; }
.msg.assistant .body pre { background: var(--code-bg); border-radius: 6px; padding: 12px; overflow-x: auto; margin: 8px 0; }
.msg.assistant .body code { font-family: 'SF Mono', 'Fira Code', monospace; font-size: 13px; }
.tool { color: var(--dim); font-size: 13px; margin: 4px 0; }
.tool .icon { margin-right: 4px; }
.tool .ok { color: var(--green); }
.tool .err { color: var(--red); }
.thinking { color: var(--dim); font-size: 13px; font-style: italic; }
#input-area {
  padding: 12px 20px;
  border-top: 1px solid var(--border);
  display: flex; gap: 10px;
  flex-shrink: 0;
}
#input {
  flex: 1;
  background: var(--input-bg);
  border: 1px solid var(--border);
  color: var(--fg);
  padding: 10px 14px;
  border-radius: 8px;
  font-size: 14px;
  font-family: inherit;
  resize: none;
  outline: none;
}
#input:focus { border-color: var(--cyan); }
#send {
  background: var(--cyan);
  color: #fff;
  border: none;
  padding: 10px 20px;
  border-radius: 8px;
  cursor: pointer;
  font-size: 14px;
  font-weight: 600;
}
#send:hover { opacity: 0.85; }
#send:disabled { opacity: 0.4; cursor: default; }
footer {
  padding: 6px 20px;
  border-top: 1px solid var(--border);
  color: var(--dim);
  font-size: 12px;
  display: flex; gap: 16px;
  flex-shrink: 0;
}
footer span { white-space: nowrap; }
@media (max-width: 600px) {
  header, #input-area, footer { padding: 10px 12px; }
}
</style>
</head>
<body>

<header>
  <div class="logo">● <span>LUMEN</span></div>
  <div class="model" id="model-info">loading…</div>
</header>

<div id="chat"></div>

<div id="input-area">
  <textarea id="input" rows="1" placeholder="Type a message… (Shift+Enter for newline, Ctrl+V image to paste)"></textarea>
  <button id="send">Send</button>
</div>

<footer>
  <span id="foot-tokens">—</span>
  <span id="foot-cost">—</span>
  <span id="foot-turn">—</span>
</footer>

<script>
let running = false;
let tokensIn = 0, tokensOut = 0, cost = 0, turn = 0;
let pendingImages = [];

async function send() {
  const input = document.getElementById('input');
  const prompt = input.value.trim();
  if ((!prompt && !pendingImages.length) || running) return;
  input.value = '';
  running = true;
  document.getElementById('send').disabled = true;

  appendMsg('user', prompt);
  const el = appendMsg('assistant', '');

  if (pendingImages.length) {
    div.innerHTML = '<img src="'+pendingImages[0]+'" style="max-width:200px;border-radius:8px;margin:4px 0">';
    el.appendChild(div);
  }
  pendingImages = [];

  try {
    const body = {prompt};
    if (pendingImages.length) body.images = pendingImages;
    const resp = await fetch('/v1/chat', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify(body)
    });

    const reader = resp.body.getReader();
    const decoder = new TextDecoder();
    let buf = '';

    while (true) {
      const {done, value} = await reader.read();
      if (done) break;
      buf += decoder.decode(value, {stream: true});
      const lines = buf.split('\n');
      buf = lines.pop(); // keep incomplete line

      for (const line of lines) {
        if (!line.startsWith('data: ')) continue;
        try {
          const ev = JSON.parse(line.slice(6));
          handleEvent(ev, el);
        } catch(e) {}
      }
    }
  } catch(e) {
    el.querySelector('.body').textContent += '\n⚠ connection lost';
  }

  running = false;
  document.getElementById('send').disabled = false;
  document.getElementById('input').focus();
  updateFooter();
}

function handleEvent(ev, el) {
  const body = el.querySelector('.body');
  switch(ev.kind) {
    case 'text':
      body.textContent += ev.text || '';
      break;
    case 'reasoning':
      body.textContent += ev.text || '';
      break;
    case 'tool_dispatch':
      if (ev.tool) {
        const div = document.createElement('div');
        div.className = 'tool';
        div.innerHTML = '🔧 '+ev.tool.name;
        el.appendChild(div);
      }
      break;
    case 'tool_result':
      if (ev.tool) {
        const div = el.querySelector('.tool:last-child');
        if (div) {
          div.innerHTML += ev.tool.err
            ? ' <span class="err">✗</span>'
            : ' <span class="ok">✓</span>';
        }
      }
      break;
    case 'usage':
      if (ev.usage) {
        tokensIn += ev.usage.prompt_tokens || 0;
        tokensOut += ev.usage.completion_tokens || 0;
      }
      break;
    case 'turn_done':
      turn++;
      break;
  }
}

function appendMsg(role, text) {
  const div = document.createElement('div');
  div.className = 'msg '+role;
  const body = document.createElement('div');
  body.className = 'body';
  if (role === 'assistant') {
    body.textContent = '⏵ ';
  } else {
    body.textContent = text;
  }
  div.appendChild(body);
  document.getElementById('chat').appendChild(div);
  document.getElementById('chat').scrollTop = document.getElementById('chat').scrollHeight;
  return div;
}

function updateFooter() {
  const tk = tokensIn + tokensOut;
  document.getElementById('foot-tokens').textContent = (tk/1000).toFixed(0)+'k tokens';
  document.getElementById('foot-cost').textContent = '$'+cost.toFixed(4);
  document.getElementById('foot-turn').textContent = 'turn #'+turn;
}

// Init
fetch('/v1/models').then(r=>r.json()).then(d=>{
  document.getElementById('model-info').textContent = (d.provider||'')+'/'+(d.model||'')+' · '+(d.mode||'');
});
fetch('/v1/memories').then(r=>r.json()).then(d=>{
  if (d.memories && d.memories.length > 0) {
    const el = document.createElement('div');
    el.className = 'thinking';
    el.textContent = '🧠 '+d.memories.length+' memories loaded';
    document.getElementById('chat').appendChild(el);
  }
});
document.getElementById('input').focus();

// Image paste handler
document.addEventListener('paste', e => {
  const items = e.clipboardData?.items;
  if (!items) return;
  for (const item of items) {
    if (item.type.startsWith('image/')) {
      e.preventDefault();
      const blob = item.getAsFile();
      const reader = new FileReader();
      reader.onload = () => {
        pendingImages.push(reader.result);
        document.getElementById('input').placeholder = 'Image attached! Type a prompt…';
      };
      reader.readAsDataURL(blob);
      break;
    }
  }
});
</script>
</body>
</html>`
