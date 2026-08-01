#!/usr/bin/env python3
"""Local relay: OpenAI Chat Completions → Anthropic Messages API for sub2api.eqing.tech.

Lumen connects here via `chat_completions` backend (well-tested, reliable).
This relay translates to the Messages API format the proxy expects (x-api-key +
/v1/messages), exactly matching Claude Code CLI behavior.
"""

import json
import os
import sys
import time
import http.server
import urllib.request
import urllib.error
import ssl
import socketserver

PROXY_HOST = "https://sub2api.eqing.tech"

# Model → API key mapping (4 keys)
MODEL_KEY_MAP = {
    # Key 1
    "grok-4.5": "sk-YOUR_KEY_1_GROK",
    # Key 2
    "GLM-5.2": "sk-YOUR_KEY_2_CN_MODELS",
    "MiniMax-M3": "sk-YOUR_KEY_2_CN_MODELS",
    "minimax-m3": "sk-YOUR_KEY_2_CN_MODELS",
    "qwen3.8-max-preview": "sk-YOUR_KEY_2_CN_MODELS",
    # Key 3
    "gpt-5.5": "sk-YOUR_KEY_3_GPT",
    "gpt-5.6": "sk-YOUR_KEY_3_GPT",
    "gpt-5.6-sol": "sk-YOUR_KEY_3_GPT",
    "gpt-5.4": "sk-YOUR_KEY_3_GPT",
    "gpt-5.6-terra": "sk-YOUR_KEY_3_GPT",
    "gpt-5.6-luna": "sk-YOUR_KEY_3_GPT",
    "gpt-5.4-mini": "sk-YOUR_KEY_3_GPT",
    "gpt-5.2": "sk-YOUR_KEY_3_GPT",
    "gpt-5.3-codex-spark": "sk-YOUR_KEY_3_GPT",
    "codex-auto-review": "sk-YOUR_KEY_3_GPT",
    # Key 4
    "claude-opus-5": "sk-YOUR_KEY_4_CLAUDE",
    "claude-sonnet-5": "sk-YOUR_KEY_4_CLAUDE",
    "claude-haiku-4-5-20251001": "sk-YOUR_KEY_4_CLAUDE",
    "claude-sonnet-4-6": "sk-YOUR_KEY_4_CLAUDE",
    "claude-opus-4-8": "sk-YOUR_KEY_4_CLAUDE",
    "claude-opus-4-7": "sk-YOUR_KEY_4_CLAUDE",
    "claude-sonnet-4-5-20250929": "sk-YOUR_KEY_4_CLAUDE",
    "claude-sonnet-4-20250514": "sk-YOUR_KEY_4_CLAUDE",
    "claude-fable-5": "sk-YOUR_KEY_4_CLAUDE",
}

PORT = int(os.environ.get("PROXY_RELAY_PORT", "18992"))


def translate_to_messages(chat_request: dict) -> dict:
    """OpenAI Chat Completions → Anthropic Messages."""
    messages = []
    system = None
    for msg in chat_request.get("messages", []):
        role = msg.get("role", "user")
        content = msg.get("content", "")
        if role == "system":
            system = content
        else:
            messages.append({"role": role, "content": content})

    body = {
        "model": chat_request["model"],
        "max_tokens": chat_request.get("max_tokens", chat_request.get("max_completion_tokens", 4096)),
        "messages": messages,
    }
    if system:
        body["system"] = system
    return body


def translate_to_chat_completion(messages_response: dict, model: str) -> dict:
    """Anthropic Messages → OpenAI Chat Completions."""
    text_parts = []
    for part in messages_response.get("content", []):
        if part.get("type") == "text":
            text_parts.append(part["text"])
        elif part.get("type") == "thinking":
            pass  # skip thinking blocks

    usage = messages_response.get("usage", {})
    return {
        "id": messages_response.get("id", f"chatcmpl-{int(time.time())}"),
        "object": "chat.completion",
        "created": int(time.time()),
        "model": model,
        "choices": [{
            "index": 0,
            "message": {"role": "assistant", "content": "\n".join(text_parts)},
            "finish_reason": "stop" if messages_response.get("stop_reason") == "end_turn" else "length",
        }],
        "usage": {
            "prompt_tokens": usage.get("input_tokens", 0),
            "completion_tokens": usage.get("output_tokens", 0),
            "total_tokens": usage.get("input_tokens", 0) + usage.get("output_tokens", 0),
        },
    }


class RelayHandler(http.server.BaseHTTPRequestHandler):
    def log_message(self, format, *args):
        sys.stderr.write(f"[relay] {self.address_string()} {args[0]}\n")
        sys.stderr.flush()

    def do_GET(self):
        if self.path == "/health":
            self.send_response(200)
            self.send_header("Content-Type", "text/plain")
            self.end_headers()
            self.wfile.write(b"ok\n")
            return
        self.send_error(404)

    def do_POST(self):
        if self.path not in ("/v1/chat/completions", "/chat/completions"):
            self.send_error(404)
            return

        content_length = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(content_length)

        try:
            chat_request = json.loads(body)
        except json.JSONDecodeError:
            self.send_error(400, "Invalid JSON")
            return

        model = chat_request.get("model", "")
        api_key = MODEL_KEY_MAP.get(model)
        if not api_key:
            self.send_error(400, f"Unknown model: {model}")
            return

        # Translate to Messages API format
        msg_body = translate_to_messages(chat_request)
        sys.stderr.write(f"[relay] request model={model} stream={chat_request.get('stream',False)} msgs={len(msg_body.get('messages',[]))} max_tokens={msg_body.get('max_tokens',0)}\n")
        sys.stderr.flush()

        # Forward to proxy with retry
        url = f"{PROXY_HOST}/v1/messages"
        last_error = None
        for attempt in range(3):
            try:
                sys.stderr.write(f"[relay] → {model} attempt {attempt+1}\n")
                sys.stderr.flush()
                req = urllib.request.Request(
                    url,
                    data=json.dumps(msg_body).encode("utf-8"),
                    headers={
                        "Content-Type": "application/json",
                        "x-api-key": api_key,
                        "anthropic-version": "2023-06-01",
                    },
                    method="POST",
                )
                ctx = ssl.create_default_context()
                with urllib.request.urlopen(req, context=ctx, timeout=300) as resp:
                    raw = resp.read()
                    msg_response = json.loads(raw)
                sys.stderr.write(f"[relay] ← {model} done ({len(raw)} bytes)\n")
                sys.stderr.write(f"[relay] body: {raw[:200]}\n")
                sys.stderr.flush()

                if msg_response.get("type") == "error":
                    err = msg_response.get("error", {})
                    raise Exception(f"API error: {err.get('message', 'unknown')}")

                chat_response = translate_to_chat_completion(msg_response, model)
                response_bytes = json.dumps(chat_response).encode("utf-8")

                # Check if client wants streaming
                is_stream = chat_request.get("stream", False)

                if is_stream:
                    # Build SSE streaming response - send full content as single delta
                    content = chat_response["choices"][0]["message"]["content"]
                    self.send_response(200)
                    self.send_header("Content-Type", "text/event-stream")
                    self.send_header("Cache-Control", "no-cache")
                    self.send_header("Connection", "keep-alive")
                    self.end_headers()
                    # Single delta with full content
                    delta = {
                        "id": chat_response["id"],
                        "object": "chat.completion.chunk",
                        "created": chat_response["created"],
                        "model": chat_response["model"],
                        "choices": [{"index": 0, "delta": {"content": content}, "finish_reason": None}],
                    }
                    self.wfile.write(f"data: {json.dumps(delta)}\n\n".encode())
                    self.wfile.flush()
                    # Final chunk
                    final = {
                        "id": chat_response["id"],
                        "object": "chat.completion.chunk",
                        "created": chat_response["created"],
                        "model": chat_response["model"],
                        "choices": [{"index": 0, "delta": {}, "finish_reason": "stop"}],
                        "usage": chat_response["usage"],
                    }
                    self.wfile.write(f"data: {json.dumps(final)}\n\n".encode())
                    self.wfile.write(b"data: [DONE]\n\n")
                    self.wfile.flush()
                    sys.stderr.write(f"[relay] streamed {len(content)} chars\n")
                    sys.stderr.flush()
                else:
                    self.send_response(200)
                    self.send_header("Content-Type", "application/json")
                    self.send_header("Content-Length", str(len(response_bytes)))
                    self.end_headers()
                    self.wfile.write(response_bytes)
                return  # success

            except urllib.error.HTTPError as e:
                last_error = f"HTTP {e.code}: {e.reason}"
                sys.stderr.write(f"[relay] {last_error} (attempt {attempt+1})\n")
                sys.stderr.flush()
                if attempt < 2:
                    time.sleep(2 ** attempt)  # 1s, 2s backoff
                    continue
                self.send_response(e.code)
                self.send_header("Content-Type", "application/json")
                self.end_headers()
                try:
                    self.wfile.write(e.read())
                except:
                    pass
                return
            except Exception as e:
                last_error = str(e)
                sys.stderr.write(f"[relay] ERROR: {last_error} (attempt {attempt+1})\n")
                sys.stderr.flush()
                if attempt < 2 and "Bad Gateway" not in last_error and "timed out" not in str(e).lower():
                    time.sleep(1)
                    continue

        self.send_error(502, last_error or "All retries failed")


def main():
    server = socketserver.ThreadingTCPServer(("127.0.0.1", PORT), RelayHandler)
    print(f"[relay] Listening on http://127.0.0.1:{PORT}/v1/chat/completions", flush=True)
    print(f"[relay] Proxying to {PROXY_HOST}", flush=True)
    server.serve_forever()


if __name__ == "__main__":
    main()
