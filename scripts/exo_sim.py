"""exo simulator: an OpenAI-compatible endpoint that drives lumen end-to-end.

Stands in for a real exo cluster so the whole local-compute -> lumen -> quant
loop can be dry-run on one machine with no GPU. It serves the same API exo does
(GET /v1/models, streaming POST /v1/chat/completions) on exo's port (52415), and
is *stateful*: on the first turn it returns a real `bash` tool call that drives
the quant pipeline; once it sees the tool result it returns a final answer,
ending the agent loop. This exercises lumen's full local-model agent loop
(dispatch -> tool result -> completion) — not just tool-call parsing.

It does NOT simulate a model's reasoning quality; that's what the real cluster
(or `lumen probe-local` against it) is for.
"""
import json
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

MODEL = "exo-sim/qwen2.5-coder-32b"

# The command lumen is told to run — the whole verifiable-quant loop in one shot.
QUANT_CMD = (
    "lumen quant init simstrat >/dev/null 2>&1; "
    "lumen quant backtest simstrat --sandbox local 2>&1 | tail -2; "
    "lumen quant verify simstrat --sandbox local 2>&1 | tail -2"
)


class H(BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass

    def do_GET(self):
        if self.path.rstrip("/").endswith("/v1/models"):
            b = json.dumps({"object": "list", "data": [{"id": MODEL, "object": "model"}]}).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(b)))
            self.end_headers(); self.wfile.write(b)
        else:
            self.send_response(404); self.end_headers()

    def do_POST(self):
        if not self.path.endswith("/chat/completions"):
            self.send_response(404); self.end_headers(); return
        n = int(self.headers.get("Content-Length", 0))
        body = json.loads(self.rfile.read(n) or b"{}")
        msgs = body.get("messages", [])
        tool_done = any(m.get("role") in ("tool", "function") or m.get("tool_call_id") for m in msgs)

        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.end_headers()
        if tool_done:
            self._final_text("Done. Scaffolded `simstrat`, ran the backtest, and the VQ "
                             "certificate verified (reproducible, untampered).")
        else:
            self._bash_call(QUANT_CMD)

    # ── helpers ──
    def _sse(self, obj):
        self.wfile.write(b"data: " + json.dumps(obj).encode() + b"\n\n"); self.wfile.flush()

    def _base(self):
        return {"id": "chatcmpl-exosim", "object": "chat.completion.chunk", "model": MODEL}

    def _bash_call(self, command):
        args = json.dumps({"command": command})
        self._sse({**self._base(), "choices": [{"index": 0, "delta": {"role": "assistant",
            "tool_calls": [{"index": 0, "id": "call_q1", "type": "function",
                "function": {"name": "bash", "arguments": ""}}]}, "finish_reason": None}]})
        self._sse({**self._base(), "choices": [{"index": 0, "delta": {
            "tool_calls": [{"index": 0, "function": {"arguments": args}}]}, "finish_reason": None}]})
        self._sse({**self._base(), "choices": [{"index": 0, "delta": {}, "finish_reason": "tool_calls"}]})
        self._sse({**self._base(), "choices": [], "usage": {"prompt_tokens": 120, "completion_tokens": 40, "total_tokens": 160}})
        self.wfile.write(b"data: [DONE]\n\n"); self.wfile.flush()

    def _final_text(self, text):
        self._sse({**self._base(), "choices": [{"index": 0, "delta": {"role": "assistant", "content": text}, "finish_reason": None}]})
        self._sse({**self._base(), "choices": [{"index": 0, "delta": {}, "finish_reason": "stop"}]})
        self._sse({**self._base(), "choices": [], "usage": {"prompt_tokens": 200, "completion_tokens": 25, "total_tokens": 225}})
        self.wfile.write(b"data: [DONE]\n\n"); self.wfile.flush()


if __name__ == "__main__":
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 52415
    print(f"exo-sim listening on :{port} (model {MODEL})", file=sys.stderr)
    ThreadingHTTPServer(("127.0.0.1", port), H).serve_forever()
