#!/usr/bin/env python3
"""Minimal LangGraph runner for Lumen Lab sidecar.

Invoked by Go: python langgraph_runner.py --project-id <id> --prompt <text>
Prints a single JSON object to stdout: {"ok": true, "result": "..."} or
{"ok": false, "error": "..."}.
"""
from __future__ import annotations

import argparse
import json
from typing import TypedDict


def main() -> None:
    parser = argparse.ArgumentParser(description="Lumen Lab LangGraph sidecar runner")
    parser.add_argument("--project-id", default="")
    parser.add_argument("--prompt", default="")
    args = parser.parse_args()

    try:
        from langgraph.graph import END, START, StateGraph
    except Exception as e:  # pragma: no cover - import path
        print(json.dumps({"ok": False, "error": f"import langgraph failed: {e}"}, ensure_ascii=False))
        return

    class State(TypedDict, total=False):
        project_id: str
        prompt: str
        result: str

    def process_node(state: State) -> State:
        pid = (state.get("project_id") or "").strip()
        prompt = (state.get("prompt") or "").strip()
        prefix = f"[project={pid}] " if pid else ""
        body = prompt if len(prompt) <= 2000 else prompt[:2000] + "…"
        return {
            "result": f"{prefix}LangGraph processed: {body}",
        }

    try:
        graph = StateGraph(State)
        graph.add_node("process", process_node)
        graph.add_edge(START, "process")
        graph.add_edge("process", END)
        app = graph.compile()

        out = app.invoke(
            {
                "project_id": args.project_id or "",
                "prompt": args.prompt or "",
            }
        )
        result = out.get("result", "") if isinstance(out, dict) else str(out)
        print(json.dumps({"ok": True, "result": result}, ensure_ascii=False))
    except Exception as e:
        print(json.dumps({"ok": False, "error": str(e)}, ensure_ascii=False))


if __name__ == "__main__":
    main()
