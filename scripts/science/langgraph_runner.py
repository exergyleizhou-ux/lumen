#!/usr/bin/env python3
"""LangGraph runner for Lumen Lab sidecar (workspace-aware).

Invoked by Go:
  python langgraph_runner.py --project-id <slug> --prompt <text> [--workspace <abs>]

Prints a single JSON object to stdout:
  {"ok": true, "result": "..."} or {"ok": false, "error": "..."}

Three-node graph (no external LLM):
  inventory → read_context → synthesize
"""
from __future__ import annotations

import argparse
import json
import os
import re
from pathlib import Path
from typing import Any, List, TypedDict

# Limits (keep result bounded for API responses)
MAX_FILES = 80
MAX_DEPTH = 5
MAX_READ_FILES = 8
MAX_SNIPPET_CHARS = 4000
MAX_FILE_BYTES = 256 * 1024
MAX_RESULT_CHARS = 12000

SKIP_DIRS = {
    ".git",
    "node_modules",
    "__pycache__",
    ".venv",
    "venv",
    ".tox",
    ".mypy_cache",
    ".pytest_cache",
    "dist",
    "build",
    ".idea",
    ".vscode",
}

SKIP_SUFFIXES = {
    ".png",
    ".jpg",
    ".jpeg",
    ".gif",
    ".webp",
    ".ico",
    ".bmp",
    ".pdf",
    ".zip",
    ".gz",
    ".tgz",
    ".bz2",
    ".xz",
    ".7z",
    ".so",
    ".dylib",
    ".dll",
    ".o",
    ".a",
    ".woff",
    ".woff2",
    ".ttf",
    ".eot",
    ".mp4",
    ".mp3",
    ".wav",
    ".pyc",
    ".pyo",
    ".class",
    ".exe",
    ".bin",
}

TEXTISH_SUFFIXES = {
    ".md",
    ".txt",
    ".py",
    ".ipynb",
    ".csv",
    ".json",
    ".yaml",
    ".yml",
    ".toml",
    ".ini",
    ".cfg",
    ".sh",
    ".r",
    ".jl",
    ".go",
    ".js",
    ".ts",
    ".tsx",
    ".html",
    ".css",
    ".rst",
    ".tex",
    ".bib",
    ".log",
}


class State(TypedDict, total=False):
    project_id: str
    prompt: str
    workspace: str
    inventory: str
    file_paths: List[str]
    snippets: str
    result: str
    mode: str  # heuristic | llm


def _rel_paths(workspace: Path) -> List[str]:
    if not workspace.is_dir():
        return []
    out: List[str] = []
    root = workspace.resolve()
    for dirpath, dirnames, filenames in os.walk(root):
        # prune
        dirnames[:] = [d for d in dirnames if d not in SKIP_DIRS and not d.startswith(".")]
        rel_dir = Path(dirpath).resolve().relative_to(root)
        depth = 0 if str(rel_dir) == "." else len(rel_dir.parts)
        if depth > MAX_DEPTH:
            dirnames[:] = []
            continue
        for name in sorted(filenames):
            if name.startswith(".") and name not in (".env.example",):
                continue
            p = Path(dirpath) / name
            try:
                if not p.is_file():
                    continue
                if p.suffix.lower() in SKIP_SUFFIXES:
                    continue
                rel = str(p.resolve().relative_to(root)).replace("\\", "/")
                out.append(rel)
            except (OSError, ValueError):
                continue
            if len(out) >= MAX_FILES:
                return out
    return out


def inventory_node(state: State) -> State:
    ws = (state.get("workspace") or "").strip()
    if not ws:
        return {
            "inventory": "（无工作区：未提供 project 或目录不存在）",
            "file_paths": [],
        }
    root = Path(ws)
    if not root.is_dir():
        return {
            "inventory": f"（工作区不存在或不是目录: {ws}）",
            "file_paths": [],
        }
    paths = _rel_paths(root)
    lines = [f"共 {len(paths)} 个文件（上限 {MAX_FILES}）"]
    for rel in paths:
        lines.append(f"- {rel}")
    if not paths:
        lines.append("（目录为空或仅有被跳过的文件）")
    return {"inventory": "\n".join(lines), "file_paths": paths}


def _prompt_tokens(prompt: str) -> List[str]:
    toks = re.split(r"[\s,;|/\\]+", (prompt or "").lower())
    return [t for t in toks if len(t) >= 2]


def _score_path(rel: str, tokens: List[str]) -> int:
    low = rel.lower()
    name = Path(rel).name.lower()
    score = 0
    # Prefer common science/docs names
    for kw, pts in (
        ("readme", 50),
        ("notes", 40),
        ("todo", 25),
        ("plan", 20),
        ("report", 20),
        ("notebook", 15),
        ("data", 10),
    ):
        if kw in low:
            score += pts
    suf = Path(rel).suffix.lower()
    if suf in TEXTISH_SUFFIXES:
        score += 15
    if suf in (".md", ".txt", ".py", ".ipynb", ".csv"):
        score += 10
    for t in tokens:
        if t in low:
            score += 20
        if t in name:
            score += 10
    return score


def _read_text_file(path: Path) -> str:
    try:
        st = path.stat()
        if st.st_size > MAX_FILE_BYTES:
            return f"（跳过：文件过大 {st.st_size} bytes）"
        raw = path.read_bytes()
        # skip obvious binary
        if b"\x00" in raw[:2048]:
            return "（跳过：二进制）"
        text = raw.decode("utf-8", errors="replace")
        if path.suffix.lower() == ".ipynb":
            try:
                nb = json.loads(text)
                parts: List[str] = []
                for cell in nb.get("cells") or []:
                    src = cell.get("source") or ""
                    if isinstance(src, list):
                        src = "".join(src)
                    if src:
                        parts.append(str(src))
                text = "\n\n".join(parts) if parts else text
            except Exception:
                pass
        if len(text) > MAX_SNIPPET_CHARS:
            text = text[:MAX_SNIPPET_CHARS] + "\n…(截断)"
        return text
    except OSError as e:
        return f"（读取失败: {e}）"


def read_context_node(state: State) -> State:
    ws = (state.get("workspace") or "").strip()
    paths = list(state.get("file_paths") or [])
    if not ws or not paths:
        return {"snippets": "（无文件可摘录）"}
    root = Path(ws)
    tokens = _prompt_tokens(state.get("prompt") or "")
    ranked = sorted(paths, key=lambda p: _score_path(p, tokens), reverse=True)
    chosen = ranked[:MAX_READ_FILES]
    blocks: List[str] = []
    for rel in chosen:
        abs_path = root / rel
        body = _read_text_file(abs_path)
        blocks.append(f"### {rel}\n{body}")
    return {"snippets": "\n\n".join(blocks) if blocks else "（无文件可摘录）"}


def _truncate(s: str, n: int) -> str:
    s = s or ""
    if len(s) <= n:
        return s
    return s[:n] + "\n…(截断)"


def _llm_available() -> bool:
    """True when an OpenAI-compatible key is present and LLM mode not disabled."""
    if os.environ.get("LUMEN_LANGGRAPH_LLM", "1").strip() == "0":
        return False
    selected = os.environ.get("LUMEN_LANGGRAPH_SELECTED_API_KEY", "").strip()
    if os.environ.get("LUMEN_LANGGRAPH_PROVIDER_ONLY") == "1":
        return bool(selected)
    return bool(selected or
        os.environ.get("DEEPSEEK_API_KEY")
        or os.environ.get("OPENAI_API_KEY")
        or os.environ.get("MOONSHOT_API_KEY")
        or os.environ.get("DASHSCOPE_API_KEY")
    )


def _llm_chat(system: str, user: str) -> str:
    """Call OpenAI-compatible chat completions (DeepSeek default)."""
    import json as _json
    import urllib.error
    import urllib.request

    selected_key = os.environ.get("LUMEN_LANGGRAPH_SELECTED_API_KEY", "").strip()
    provider_only = os.environ.get("LUMEN_LANGGRAPH_PROVIDER_ONLY") == "1"
    key = selected_key
    if not key and not provider_only:
        key = (os.environ.get("DEEPSEEK_API_KEY") or os.environ.get("OPENAI_API_KEY")
               or os.environ.get("MOONSHOT_API_KEY") or os.environ.get("DASHSCOPE_API_KEY") or "")
    key = key.strip()
    if not key:
        raise RuntimeError("no API key")

    base = os.environ.get("LUMEN_LANGGRAPH_SELECTED_BASE_URL", "")
    if not base and not provider_only:
        base = os.environ.get("OPENAI_BASE_URL") or os.environ.get("LUMEN_LANGGRAPH_BASE_URL") or ""
    base = base.strip().rstrip("/")
    if not base:
        if os.environ.get("DEEPSEEK_API_KEY"):
            base = "https://api.deepseek.com/v1"
        elif os.environ.get("MOONSHOT_API_KEY"):
            base = "https://api.moonshot.cn/v1"
        elif os.environ.get("DASHSCOPE_API_KEY"):
            base = "https://dashscope.aliyuncs.com/compatible-mode/v1"
        else:
            base = "https://api.openai.com/v1"

    model = os.environ.get("LUMEN_LANGGRAPH_SELECTED_MODEL", "")
    if not model and not provider_only:
        model = os.environ.get("LUMEN_SCIENCE_MODEL") or os.environ.get("LUMEN_LANGGRAPH_MODEL") or os.environ.get("DEEPSEEK_MODEL") or "deepseek-chat"
    if not model:
        raise RuntimeError("selected provider model is empty")
    payload = _json.dumps(
        {
            "model": model,
            "messages": [
                {"role": "system", "content": system},
                {"role": "user", "content": user},
            ],
            "temperature": 0.3,
            "max_tokens": 2000,
        }
    ).encode("utf-8")
    req = urllib.request.Request(
        base + "/chat/completions",
        data=payload,
        headers={
            "Content-Type": "application/json",
            "Authorization": "Bearer " + key,
        },
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=90) as resp:
        data = _json.loads(resp.read().decode("utf-8", errors="replace"))
    choices = data.get("choices") or []
    if not choices:
        raise RuntimeError("empty LLM choices")
    msg = (choices[0].get("message") or {}).get("content") or ""
    if not str(msg).strip():
        raise RuntimeError("empty LLM content")
    return str(msg).strip()


def synthesize_node(state: State) -> State:
    pid = (state.get("project_id") or "").strip() or "（未指定）"
    prompt = (state.get("prompt") or "").strip()
    ws = (state.get("workspace") or "").strip() or "（无）"
    inventory = state.get("inventory") or ""
    snippets = state.get("snippets") or ""
    paths = list(state.get("file_paths") or [])
    n = len(paths)

    inv_show = _truncate(inventory, 2500)
    snip_show = _truncate(snippets, 5000)
    mode = "heuristic"

    llm_err = ""
    # Prefer real LLM when keys are available (does not replace Go main agent).
    if _llm_available():
        try:
            system = (
                "你是 Lumen Lab 的 LangGraph 旁路分析助手（不是主 Agent）。"
                "根据工作区文件清单与摘录，用中文给出结构化分析："
                "简要结论、对用户问题的直接回答、2-5 条可执行下一步。"
                "不要编造不存在的文件；摘录不足时明确说明。"
            )
            user = (
                f"课题: {pid}\n工作区: {ws}\n文件数: {n}\n\n"
                f"用户问题:\n{prompt}\n\n"
                f"工作区摘要:\n{inv_show}\n\n"
                f"相关摘录:\n{snip_show}\n"
            )
            llm_body = _llm_chat(system, user)
            mode = "llm"
            result = f"""## LangGraph 旁路分析（LLM）
- 课题: {pid}
- 工作区: {ws}
- 文件数: {n}
- 模式: llm

## 工作区摘要
{inv_show}

## 模型分析
{llm_body}
"""
            result = _truncate(result.strip(), MAX_RESULT_CHARS)
            return {"result": result, "mode": mode}
        except Exception as e:
            # Fall through to heuristic with a note
            llm_err = str(e)[:200]

    # Heuristic response
    if n == 0:
        response = (
            f"针对「{prompt}」：当前工作区没有可读文件或路径无效。"
            "建议先在 Lab 文件面板创建笔记/脚本，或确认已选择正确课题。"
        )
        steps = [
            "在 Inspector → 文件 中新建 notes.md 或 scripts/run.py",
            "上传已有数据/代码后再运行 LangGraph",
            "确认左侧课题 slug 正确（不要用 proj_ 内部 id）",
        ]
    else:
        top = ", ".join(paths[:5])
        has_nb = any(p.endswith(".ipynb") for p in paths)
        has_md = any(p.endswith(".md") for p in paths)
        response = (
            f"针对「{prompt}」：工作区约有 {n} 个文件，优先关注：{top}。"
            "以下摘录供你核对；"
            + (
                f"LLM 调用失败已回退启发式（{llm_err}）。"
                if llm_err
                else "当前为启发式摘要（未配置 API 密钥时）。"
            )
        )
        steps = [
            "根据摘录核对关键路径，用主 Agent 对话做深入分析/改写",
            "补全 README 或 notes.md 记录假设与下一步实验",
        ]
        if has_nb:
            steps.append("在 Notebook 面板打开/执行相关 .ipynb 验证结果")
        elif has_md:
            steps.append("把结论写回 notes/report，并用 Office 页导出文档（若已配置）")
        else:
            steps.append("为脚本补充可重复运行的入口（如 scripts/run.py）")

    result = f"""## LangGraph 旁路分析
- 课题: {pid}
- 工作区: {ws}
- 文件数: {n}
- 模式: heuristic

## 工作区摘要
{inv_show}

## 相关摘录
{snip_show}

## 对你问题的回应
{response}

## 建议下一步
"""
    for i, s in enumerate(steps[:5], 1):
        result += f"{i}. {s}\n"

    result = _truncate(result.strip(), MAX_RESULT_CHARS)
    return {"result": result, "mode": mode}


def main() -> None:
    parser = argparse.ArgumentParser(description="Lumen Lab LangGraph sidecar runner")
    parser.add_argument("--project-id", default="")
    parser.add_argument("--prompt", default="")
    parser.add_argument("--workspace", default="")
    parser.add_argument("--provider-debug", action="store_true")
    args = parser.parse_args()

    if args.provider_debug:
        print(json.dumps({
            "provider": os.environ.get("LUMEN_LANGGRAPH_SELECTED_PROVIDER", ""),
            "key": os.environ.get("LUMEN_LANGGRAPH_SELECTED_API_KEY", ""),
            "base_url": os.environ.get("LUMEN_LANGGRAPH_SELECTED_BASE_URL", ""),
            "model": os.environ.get("LUMEN_LANGGRAPH_SELECTED_MODEL", ""),
            "deepseek_present": bool(os.environ.get("DEEPSEEK_API_KEY")),
            "provider_only": os.environ.get("LUMEN_LANGGRAPH_PROVIDER_ONLY") == "1",
        }))
        return

    try:
        from langgraph.graph import END, START, StateGraph
    except Exception as e:  # pragma: no cover
        print(json.dumps({"ok": False, "error": f"import langgraph failed: {e}"}, ensure_ascii=False))
        return

    try:
        graph = StateGraph(State)
        graph.add_node("inventory", inventory_node)
        graph.add_node("read_context", read_context_node)
        graph.add_node("synthesize", synthesize_node)
        graph.add_edge(START, "inventory")
        graph.add_edge("inventory", "read_context")
        graph.add_edge("read_context", "synthesize")
        graph.add_edge("synthesize", END)
        app = graph.compile()

        out: Any = app.invoke(
            {
                "project_id": args.project_id or "",
                "prompt": args.prompt or "",
                "workspace": args.workspace or "",
            }
        )
        result = out.get("result", "") if isinstance(out, dict) else str(out)
        mode = ""
        if isinstance(out, dict):
            mode = str(out.get("mode") or "")
        payload = {"ok": True, "result": result}
        if mode:
            payload["mode"] = mode
        print(json.dumps(payload, ensure_ascii=False))
    except Exception as e:
        print(json.dumps({"ok": False, "error": str(e)}, ensure_ascii=False))


if __name__ == "__main__":
    main()
