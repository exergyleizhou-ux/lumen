#!/usr/bin/env node
/**
 * Drives shipped window.LabUI from app.js (escHtml, renderMarkdown, reduceSSE).
 * Run: node labui_test.mjs
 * No re-implementation — loads app.js source via vm up to LabUI export.
 */
import fs from "fs";
import path from "path";
import vm from "vm";
import { fileURLToPath } from "url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const appPath = path.join(__dirname, "app.js");
const src = fs.readFileSync(appPath, "utf8");
const cut = src.indexOf("/* ── 3. Global state");
if (cut < 0) {
  console.error("FAIL: could not find Global state marker in app.js");
  process.exit(1);
}

function loadLabUI() {
  const sandbox = {
    window: {},
    document: { getElementById: () => null },
    location: { pathname: "/" },
    console,
  };
  vm.createContext(sandbox);
  vm.runInContext(src.slice(0, cut), sandbox);
  if (!sandbox.window.LabUI) {
    throw new Error("window.LabUI not exported");
  }
  return sandbox.window.LabUI;
}

function assert(cond, msg) {
  if (!cond) throw new Error("ASSERT: " + msg);
}

function runOnce(runLabel) {
  console.log("--- " + runLabel + " ---");
  const L = loadLabUI();
  assert(typeof L.escHtml === "function", "escHtml is function");
  assert(typeof L.renderMarkdown === "function", "renderMarkdown is function");
  assert(typeof L.reduceSSE === "function", "reduceSSE is function");

  // escHtml
  const esc = L.escHtml('<script>alert(1)</script> & "x"');
  assert(esc.includes("&lt;script&gt;"), "escHtml escapes <script>: " + esc);
  assert(esc.includes("&amp;"), "escHtml escapes &: " + esc);
  assert(!esc.includes("<script>"), "escHtml no raw script tag");
  console.log("OK escHtml escapes tags/amp: " + esc.slice(0, 60));

  // markdown: bold, list, code — and HTML escaped before tags
  const md = L.renderMarkdown("**bold** and `code`\n\n- item one\n- item two");
  assert(md.includes("<strong>bold</strong>"), "md bold strong: " + md);
  assert(md.includes("<code>code</code>"), "md inline code: " + md);
  assert(md.includes("<li>item one</li>") || md.includes("<li>"), "md list li: " + md);
  console.log("OK renderMarkdown bold/list/code tags present");

  // fenced code block has copy button
  const fenced = L.renderMarkdown("```js\nconst x = 1;\n```");
  assert(fenced.includes("code-wrap"), "fenced code-wrap: " + fenced.slice(0, 120));
  assert(fenced.includes("code-copy"), "fenced copy btn");
  assert(fenced.includes("const x = 1"), "fenced body");
  console.log("OK fenced code has code-wrap + copy");

  // attachment path parse
  if (typeof L.parseAttachmentPaths === "function") {
    const paths = L.parseAttachmentPaths("[附件] data/a.csv\nhello @notes/b.txt");
    assert(paths.includes("data/a.csv"), "attach card path: " + JSON.stringify(paths));
    assert(paths.includes("notes/b.txt"), "at path: " + JSON.stringify(paths));
    console.log("OK parseAttachmentPaths");
  }

  // file templates if exported
  if (typeof L.fileTemplateContent === "function") {
    const py = L.fileTemplateContent("scripts/run.py");
    assert(py.includes("def main"), "py template: " + py.slice(0, 80));
    const md = L.fileTemplateContent("notes/x.md");
    assert(md.includes("#"), "md template");
    console.log("OK fileTemplateContent");
  }

  // XSS: raw HTML must be escaped, not executed as tags
  const xss = L.renderMarkdown("<img onerror=alert(1) src=x> **ok**");
  assert(!xss.includes("<img"), "md no raw img tag after escape: " + xss);
  assert(xss.includes("&lt;img") || xss.includes("&lt;"), "md escaped lt: " + xss);
  assert(xss.includes("<strong>ok</strong>"), "md still applies bold after escape");
  console.log("OK HTML escaped before markdown (no raw img)");

  // reduceSSE tool id merge
  let s = L.reduceSSE(null, {
    kind: "tool_dispatch",
    tool: { id: "tool-42", name: "bash", args: '{"cmd":"ls"}' },
  });
  assert(s.tools["tool-42"], "dispatch creates tools[tool-42]");
  assert(s.tools["tool-42"].status === "running", "dispatch status running");
  assert(s.tools["tool-42"].name === "bash", "dispatch name bash");
  console.log("OK tool_dispatch keyed by id status=running");

  s = L.reduceSSE(s, {
    kind: "tool_result",
    tool: { id: "tool-42", name: "bash", output: "FINAL_OUT", err: "" },
  });
  assert(s.tools["tool-42"].status === "done", "result status done");
  assert(s.tools["tool-42"].output === "FINAL_OUT", "result output replace not double: " + s.tools["tool-42"].output);
  // second tool does not clobber first
  s = L.reduceSSE(s, {
    kind: "tool_dispatch",
    tool: { id: "tool-99", name: "read", args: "a.md" },
  });
  assert(s.tools["tool-42"].output === "FINAL_OUT", "tool id merge keeps tool-42");
  assert(s.tools["tool-99"].status === "running", "tool-99 running");
  console.log("OK tool_result merges by id; second tool independent");

  // approval shape
  s = L.reduceSSE(s, {
    kind: "approval_request",
    id: "appr-7",
    tool: "shell",
    summary: "run rm -rf /tmp/x",
  });
  assert(Array.isArray(s.approvals) && s.approvals.length >= 1, "approvals array");
  const ap = s.approvals[s.approvals.length - 1];
  assert(ap.id === "appr-7", "approval id");
  assert(ap.tool === "shell", "approval tool");
  assert(ap.summary && ap.summary.indexOf("rm") >= 0, "approval summary");
  console.log("OK approval_request shape {id,tool,summary}: " + JSON.stringify(ap));

  // text + reasoning accumulate
  s = L.reduceSSE(s, { kind: "text", text: "Hello " });
  s = L.reduceSSE(s, { kind: "text", text: "world" });
  assert(s.text === "Hello world", "text accumulate: " + s.text);
  s = L.reduceSSE(s, { kind: "reasoning", text: "think" });
  assert(s.reasoning === "think", "reasoning: " + s.reasoning);
  console.log("OK text/reasoning accumulate");

  console.log("PASS " + runLabel + " all assertions");
  return true;
}

let fails = 0;
try {
  runOnce("run-1");
} catch (e) {
  console.error(e.message || e);
  fails++;
}
try {
  runOnce("run-2");
} catch (e) {
  console.error(e.message || e);
  fails++;
}

if (fails) {
  console.error("FAIL labui_test.mjs exits with " + fails + " failed run(s)");
  process.exit(1);
}
console.log("PASS labui_test.mjs (2 runs)");
process.exit(0);
