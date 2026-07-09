/* Lumen Science Lab — OCS-style static workbench
   SSE kinds: text, reasoning, tool_dispatch, tool_result,
   approval_request, error, turn_started, turn_done
   Contract: internal/event/event.go + approval.go */

/* ── 1. Utilities ── */
const $ = (id) => document.getElementById(id);

const API_BASE = (function () {
  const p = location.pathname || "";
  if (p.startsWith("/lumen-lab")) return "/lumen-lab";
  return "";
})();

function labPath(path) {
  return API_BASE + path;
}

function escHtml(s) {
  return String(s).replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
}

function fmtSize(bytes) {
  if (!bytes || bytes < 1024) return (bytes || 0) + "B";
  if (bytes < 1048576) return (bytes / 1024).toFixed(1) + "KB";
  return (bytes / 1048576).toFixed(1) + "MB";
}

function fileIcon(name) {
  const ext = String(name).split(".").pop().toLowerCase();
  const map = { md: "📝", py: "🐍", r: "📊", json: "📋", csv: "📈", png: "🖼", jpg: "🖼", jpeg: "🖼", svg: "🖼", pdf: "📕", html: "🌐", css: "🎨", js: "📜", txt: "📄", log: "📋", yml: "⚙", yaml: "⚙", toml: "⚙" };
  return map[ext] || "📄";
}

async function api(path, opts = {}) {
  const res = await fetch(labPath(path), {
    headers: { "Content-Type": "application/json", ...(opts.headers || {}) },
    ...opts,
  });
  const text = await res.text();
  let data;
  try { data = JSON.parse(text); } catch (_) { data = { raw: text }; }
  if (!res.ok) throw new Error(data.error || res.statusText);
  return data;
}

/* ── 2. Pure functions (window.LabUI for testing) ── */

function renderMarkdown(src) {
  // Safe subset: escHtml first, then regex → HTML
  var html = escHtml(src);

  // Fenced code blocks (before inline code)
  html = html.replace(/```(\w*)\n([\s\S]*?)```/g, function (_, lang, code) {
    var cls = lang ? ' class="language-' + escHtml(lang) + '"' : "";
    return "<pre><code" + cls + ">" + code.trimEnd() + "</code></pre>";
  });

  // Inline code
  html = html.replace(/`([^`]+)`/g, "<code>$1</code>");

  // Bold
  html = html.replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>");

  // Headings
  html = html.replace(/^### (.+)$/gm, "<h3>$1</h3>");
  html = html.replace(/^## (.+)$/gm, "<h2>$1</h2>");
  html = html.replace(/^# (.+)$/gm, "<h1>$1</h1>");

  // Unordered list items
  html = html.replace(/^[\-\*] (.+)$/gm, "<li>$1</li>");
  // Ordered list items
  html = html.replace(/^\d+\. (.+)$/gm, "<li>$1</li>");

  // Links [text](url) — only http/https
  html = html.replace(/\[([^\]]+)\]\((https?:\/\/[^)]+)\)/g, '<a href="$2" target="_blank" rel="noopener">$1</a>');

  // Horizontal rules
  html = html.replace(/^---$/gm, "<hr>");

  // Blockquotes
  html = html.replace(/^&gt; (.+)$/gm, "<blockquote>$1</blockquote>");

  // Paragraphs: split on double newline
  html = html.replace(/\n\n/g, "</p><p>");
  html = "<p>" + html + "</p>";

  // Clean up empty paragraphs and fix list wrapping
  html = html.replace(/<p><\/p>/g, "");
  html = html.replace(/<p>(<li>[\s\S]*?<\/li>)<\/p>/g, "<ul>$1</ul>");
  html = html.replace(/<p>(<h[123][\s\S]*?<\/h[123]>)<\/p>/g, "$1");
  html = html.replace(/<p>(<pre>[\s\S]*?<\/pre>)<\/p>/g, "$1");
  html = html.replace(/<p>(<blockquote>[\s\S]*?<\/blockquote>)<\/p>/g, "$1");
  html = html.replace(/<p>(<hr>)<\/p>/g, "$1");
  html = html.replace(/<p>\s*<\/p>/g, "");

  return html;
}

function reduceSSE(state, ev) {
  if (!state || !state.tools) state = { text: "", reasoning: "", tools: {}, approvals: [], errors: [], turn: null };
  state.text = state.text || "";
  state.reasoning = state.reasoning || "";
  state.tools = state.tools || {};
  state.approvals = state.approvals || [];
  state.errors = state.errors || [];
  var kind = ev.kind || "";

  switch (kind) {
    case "text":
      state.text += (ev.text || "");
      break;
    case "reasoning":
      state.reasoning += (ev.text || "");
      break;
    case "tool_dispatch":
      if (ev.tool && ev.tool.id) {
        state.tools[ev.tool.id] = {
          id: ev.tool.id,
          name: ev.tool.name || "",
          args: ev.tool.args || "",
          description: ev.tool.description || "",
          output: "",
          err: "",
          status: "running",
          readOnly: ev.tool.read_only || false,
        };
      }
      break;
    case "tool_result":
      if (ev.tool && ev.tool.id) {
        var existing = state.tools[ev.tool.id];
        if (existing) {
          if (ev.tool.output !== undefined) existing.output = (existing.output || "") + ev.tool.output;
          if (ev.tool.err) existing.err = (existing.err || "") + ev.tool.err;
          existing.status = ev.tool.err ? "error" : "done";
        } else {
          state.tools[ev.tool.id] = {
            id: ev.tool.id,
            name: ev.tool.name || "",
            args: ev.tool.args || "",
            output: ev.tool.output || "",
            err: ev.tool.err || "",
            status: ev.tool.err ? "error" : "done",
            readOnly: ev.tool.read_only || false,
          };
        }
      }
      break;
    case "tool_progress":
      // Optional: update tool progress text
      if (ev.tool && ev.tool.id && ev.text) {
        var tp = state.tools[ev.tool.id];
        if (tp) tp.output = (tp.output || "") + ev.text;
      }
      break;
    case "approval_request":
      state.approvals.push({ id: ev.id, tool: ev.tool, summary: ev.summary });
      break;
    case "error":
      state.errors.push(ev.text || "未知错误");
      break;
    case "turn_started":
      state.turn = "started";
      break;
    case "turn_done":
      state.turn = "done";
      break;
    case "phase":
    case "usage":
    case "notice":
    case "perf":
    case "file_preview":
      // Optional: silently accept
      break;
  }
  return state;
}

window.LabUI = { escHtml: escHtml, renderMarkdown: renderMarkdown, reduceSSE: reduceSSE };

/* ── 3. Global state ── */
var activeProject = null;
var threads = [{ id: "main", title: "对话" }];
var activeThread = "main";
var currentAbort = null;
var fileCwd = ".";
var sseState = null; // per-turn SSE accumulator

/* ── 4. API functions ── */

async function refreshHealth() {
  try {
    var h = await api("/api/lab/health");
    var pack = h.research_pack || {};
    var f = h.fleet || {};
    $("packBadge").textContent = pack.healthy ? (pack.domain_tools || 0) + " tools · " + (pack.skills || 0) + " skills" : "未安装 Research Pack";
    $("fleetBadge").textContent = "⚡ " + (f.connected_total || 0) + "/" + (f.cs_domains || 0) + " fleet";
    $("modeHint").textContent = h.science_mode || "hybrid";
    var ib = $("inspectorBody");
    if (ib) {
      ib.innerHTML = [
        '<div class="sr"><span class="sr k">状态</span><span class="sr v ok">● 在线</span></div>',
        '<div class="sr"><span class="sr k">版本</span><span class="sr v">' + escHtml(h.version || "dev") + "</span></div>",
        '<div class="sr"><span class="sr k">模式</span><span class="sr v">' + escHtml(h.science_mode || "hybrid") + "</span></div>",
        '<div class="sr-div"></div>',
        '<div class="sr"><span class="sr k">Research</span><span class="sr v ' + (pack.healthy ? "ok" : "") + '">' + (pack.healthy ? "✓" : "✗") + " " + (pack.domain_tools || 0) + " tools</span></div>",
        '<div class="sr"><span class="sr k">CS fleet</span><span class="sr v">' + (f.cs_connected || 0) + "/" + (f.cs_domains || 0) + "</span></div>",
        '<div class="sr"><span class="sr k">原生 fleet</span><span class="sr v">' + (f.lumen_native || 0) + "</span></div>",
        '<div class="sr-div"></div>',
        '<div class="sr"><span class="sr k">模型</span><span class="sr v">' + escHtml((h.provider && h.provider.masked) || "—") + "</span></div>",
      ].join("");
    }
    return h;
  } catch (e) {
    var ib2 = $("inspectorBody");
    if (ib2) ib2.innerHTML = '<div class="ft-err">' + escHtml(e.message) + "</div>";
  }
}

async function loadProjects() {
  try {
    var list = await api("/api/lab/projects");
    var nav = $("projectList");
    if (!nav) return;
    nav.innerHTML = "";
    list.forEach(function (p) {
      var btn = document.createElement("button");
      btn.type = "button";
      btn.className = (activeProject && activeProject.slug === p.slug) ? " active" : "";
      btn.textContent = p.title;
      btn.addEventListener("click", function () {
        activeProject = p;
        loadProjects();
        refreshFiles();
        var nm = $("activeProjectName");
        var mt = $("activeProjectMeta");
        if (nm) nm.textContent = p.title;
        if (mt) mt.textContent = p.slug;
      });
      nav.appendChild(btn);
    });
    if (!activeProject && list.length) {
      activeProject = list[0];
      refreshFiles();
      var nm = $("activeProjectName");
      var mt = $("activeProjectMeta");
      if (nm) nm.textContent = list[0].title;
      if (mt) mt.textContent = list[0].slug;
    }
  } catch (e) {
    var nav = $("projectList");
    if (nav) nav.innerHTML = '<div class="ft-err">' + escHtml(e.message) + "</div>";
  }
}

async function ensureProject() {
  if (activeProject) return activeProject;
  var p = await api("/api/lab/projects", { method: "POST", body: JSON.stringify({ title: "默认课题" }) });
  activeProject = p;
  await loadProjects();
  return p;
}

async function loadSkills() {
  var el = $("skillsBody");
  if (!el) return;
  try {
    var slug = activeProject ? activeProject.slug : "";
    var d = await api("/api/lab/skills?project_id=" + slug);
    var s = d.skills || [];
    el.innerHTML = s.length
      ? s.map(function (sk) { return '<div class="ft-row"><span>📋 ' + escHtml(sk.name || sk) + "</span></div>"; }).join("")
      : '<div class="hint">暂无技能 — 安装 Research Pack 后可用</div>';
  } catch (e) {
    el.innerHTML = '<div class="ft-err">' + escHtml(e.message) + "</div>";
  }
}

/* ── 5. Thread tabs ── */

function renderThreadTabs() {
  var host = $("convTabs");
  if (!host) return;
  host.innerHTML = threads
    .map(function (t) {
      return '<button type="button" class="ctr-tab' + (t.id === activeThread ? " active" : "") + '" data-id="' + escHtml(t.id) + '"><span>' + escHtml(t.title) + '</span><span class="close" data-close="' + escHtml(t.id) + '">×</span></button>';
    })
    .join("");
  host.querySelectorAll(".ctr-tab").forEach(function (btn) {
    btn.addEventListener("click", function (e) {
      if (e.target.classList.contains("close")) {
        e.stopPropagation();
        closeThread(e.target.dataset.close);
        return;
      }
      activeThread = btn.dataset.id || "main";
      sseState = null;
      renderThreadTabs();
    });
  });
}

function closeThread(id) {
  if (threads.length <= 1) return;
  threads = threads.filter(function (t) { return t.id !== id; });
  if (activeThread === id) activeThread = threads[0].id;
  renderThreadTabs();
}

function newConv() {
  var id = "t-" + Date.now().toString(36);
  threads.push({ id: id, title: "对话 " + threads.length });
  activeThread = id;
  sseState = null;
  renderThreadTabs();
  var scroll = $("chatScroll");
  if (scroll) {
    var hero = document.createElement("section");
    hero.className = "hero";
    hero.innerHTML = "<h2>新对话</h2><p>描述你的科研任务</p>";
    scroll.appendChild(hero);
  }
}

/* ── 6. DOM rendering helpers ── */

function createAgentBubble() {
  $("welcome") && $("welcome").remove();
  var wrap = document.createElement("div");
  wrap.className = "chat-msg agent-wrap";

  // Think block (hidden by default)
  var think = document.createElement("details");
  think.className = "think-block";
  think.style.display = "none";
  think.innerHTML = '<summary class="think-summary"><span class="think-dot"></span>思考中…</summary><div class="think-body"></div>';
  wrap.appendChild(think);

  // Agent text
  var textDiv = document.createElement("div");
  textDiv.className = "agent-text agent-md";
  wrap.appendChild(textDiv);

  // Tool log
  var toolLog = document.createElement("div");
  toolLog.className = "tool-log";
  wrap.appendChild(toolLog);

  $("chatScroll").appendChild(wrap);
  wrap.scrollIntoView({ behavior: "smooth", block: "end" });
  return { wrap: wrap, think: think, thinkBody: think.querySelector(".think-body"), textDiv: textDiv, toolLog: toolLog };
}

function upsertToolCard(toolLog, tool) {
  var id = tool.id;
  var existing = toolLog.querySelector('.tool-card[data-tool-id="' + id + '"]');
  if (existing) {
    // Update existing card
    updateToolCardDOM(existing, tool);
    return existing;
  }
  // Create new card
  var card = document.createElement("div");
  card.className = "tool-card status-" + (tool.status || "running");
  card.setAttribute("data-tool-id", id);
  card.innerHTML =
    '<div class="tool-card-hd">' +
    '<span class="tool-card-arrow">▸</span>' +
    '<span class="tool-card-icon">⚙</span>' +
    '<span class="tool-card-name">' + escHtml(tool.name || "tool") + "</span>" +
    '<span class="tool-card-status">' + statusLabel(tool.status) + "</span>" +
    "</div>" +
    '<div class="tool-card-body" style="display:none">' +
    (tool.args ? '<div class="tool-card-section"><div class="tool-card-label">参数</div><pre>' + escHtml(tool.args) + "</pre></div>" : "") +
    (tool.description ? '<div class="tool-card-section"><div class="tool-card-label">说明</div><div>' + escHtml(tool.description) + "</div></div>" : "") +
    '<div class="tool-card-section tool-card-output-section" style="display:none"><div class="tool-card-label">输出</div><pre class="tool-card-output"></pre></div>' +
    '<div class="tool-card-section tool-card-err-section" style="display:none"><div class="tool-card-label">错误</div><pre class="tool-card-err"></pre></div>' +
    "</div>";

  // Click header to toggle
  card.querySelector(".tool-card-hd").addEventListener("click", function () {
    var body = card.querySelector(".tool-card-body");
    var open = body.style.display !== "none";
    body.style.display = open ? "none" : "block";
    card.classList.toggle("is-open", !open);
  });

  toolLog.appendChild(card);
  card.scrollIntoView({ behavior: "smooth", block: "nearest" });
  return card;
}

function updateToolCardDOM(card, tool) {
  card.className = "tool-card status-" + (tool.status || "running") + (card.classList.contains("is-open") ? " is-open" : "");
  var statusEl = card.querySelector(".tool-card-status");
  if (statusEl) statusEl.textContent = statusLabel(tool.status);

  if (tool.output) {
    var outSec = card.querySelector(".tool-card-output-section");
    var outPre = card.querySelector(".tool-card-output");
    if (outSec) outSec.style.display = "block";
    if (outPre) outPre.textContent = tool.output;
  }
  if (tool.err) {
    var errSec = card.querySelector(".tool-card-err-section");
    var errPre = card.querySelector(".tool-card-err");
    if (errSec) errSec.style.display = "block";
    if (errPre) errPre.textContent = tool.err;
  }
}

function statusLabel(status) {
  switch (status) {
    case "running": return "运行中";
    case "done": return "完成";
    case "error": return "错误";
    default: return status || "";
  }
}

function setRunStatus(running) {
  var dot = $("liveDot");
  var label = $("runStatus");
  var stopBtn = $("btnStop");
  var sendBtn = $("btnSend");
  if (running) {
    if (dot) dot.style.background = "#f59e0b";
    if (label) label.textContent = "运行中";
    if (stopBtn) stopBtn.disabled = false;
    if (sendBtn) sendBtn.disabled = true;
  } else {
    if (dot) dot.style.background = "";
    if (label) label.textContent = "就绪";
    if (stopBtn) stopBtn.disabled = true;
    if (sendBtn) sendBtn.disabled = false;
  }
}

function addErrorBubble(toolLog, message) {
  var div = document.createElement("div");
  div.className = "chat-msg error-msg";
  div.textContent = "⚠ " + message;
  toolLog.appendChild(div);
}

/* ── 7. SSE streaming + chat ── */

function handleSSEEvent(ev, state, bubble) {
  state = reduceSSE(state, ev);
  var kind = ev.kind || "";

  // Update reasoning (think block)
  if (state.reasoning && bubble.think) {
    bubble.think.style.display = "block";
    bubble.thinkBody.textContent = state.reasoning;
    if (kind === "turn_done" || (kind === "text" && state.text)) {
      bubble.think.open = false;
    }
  }

  // Update markdown text
  if (state.text && bubble.textDiv) {
    bubble.textDiv.innerHTML = renderMarkdown(state.text) + '<span class="cursor">|</span>';
    $("chatScroll").scrollTop = $("chatScroll").scrollHeight;
  }

  // Tool dispatch
  if (kind === "tool_dispatch" && ev.tool && ev.tool.id) {
    upsertToolCard(bubble.toolLog, state.tools[ev.tool.id]);
  }

  // Tool result
  if (kind === "tool_result" && ev.tool && ev.tool.id) {
    var card = bubble.toolLog.querySelector('.tool-card[data-tool-id="' + ev.tool.id + '"]');
    if (card) {
      updateToolCardDOM(card, state.tools[ev.tool.id]);
    } else {
      upsertToolCard(bubble.toolLog, state.tools[ev.tool.id]);
    }
  }

  // Tool progress
  if (kind === "tool_progress" && ev.tool && ev.tool.id) {
    var tcard = bubble.toolLog.querySelector('.tool-card[data-tool-id="' + ev.tool.id + '"]');
    if (tcard) updateToolCardDOM(tcard, state.tools[ev.tool.id]);
  }

  // Approval
  if (kind === "approval_request") {
    renderApprovalCard(bubble.toolLog, ev);
  }

  // Error
  if (kind === "error") {
    addErrorBubble(bubble.toolLog, ev.text || "未知错误");
  }

  // Turn lifecycle
  if (kind === "turn_started") {
    setRunStatus(true);
  }
  if (kind === "turn_done") {
    setRunStatus(false);
    // Remove cursor
    if (bubble.textDiv) {
      var cur = bubble.textDiv.querySelector(".cursor");
      if (cur) cur.remove();
    }
    refreshFiles();
    currentAbort = null;
  }

  return state;
}

async function streamChat(prompt, mode) {
  mode = mode || "agent";
  var p;
  try { p = await ensureProject(); } catch (e) { addErrorBubble($("chatScroll"), "无法获取课题: " + e.message); return; }

  // User bubble
  var ue = document.createElement("div");
  ue.className = "chat-msg user";
  ue.textContent = prompt;
  $("chatScroll").appendChild(ue);
  ue.scrollIntoView({ behavior: "smooth", block: "end" });

  // Agent bubble
  var bubble = createAgentBubble();

  // Reset state
  sseState = { text: "", reasoning: "", tools: {}, approvals: [], errors: [], turn: null };
  setRunStatus(true);
  currentAbort = new AbortController();

  try {
    var res = await fetch(labPath("/api/lab/chat"), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ project_id: p.slug, prompt: prompt, mode: mode, session_id: activeThread }),
      signal: currentAbort.signal,
    });

    if (!res.ok) {
      bubble.textDiv.innerHTML = '<span class="error-text">错误: HTTP ' + res.status + "</span>";
      setRunStatus(false);
      currentAbort = null;
      return;
    }

    var reader = res.body.getReader();
    var dec = new TextDecoder();
    var buf = "";

    while (true) {
      var chunk;
      try { chunk = await reader.read(); } catch (e) {
        if (e.name === "AbortError") break;
        throw e;
      }
      if (chunk.done) break;
      buf += dec.decode(chunk.value, { stream: true });
      var lines = buf.split("\n");
      buf = lines.pop();
      for (var i = 0; i < lines.length; i++) {
        var line = lines[i];
        if (line.indexOf("data:") !== 0) continue;
        var json = line.slice(5).trim();
        if (!json.startsWith("{")) continue;
        var ev;
        try { ev = JSON.parse(json); } catch (_) { continue; }
        sseState = handleSSEEvent(ev, sseState, bubble);
      }
    }
  } catch (e) {
    if (e.name === "AbortError") {
      bubble.textDiv.innerHTML = (bubble.textDiv.innerHTML || "") + '<div class="stop-notice">⏹ 已停止</div>';
    } else {
      bubble.textDiv.innerHTML = '<span class="error-text">错误: ' + escHtml(e.message) + "</span>";
    }
  } finally {
    setRunStatus(false);
    // Remove cursor
    if (bubble.textDiv) {
      var cur = bubble.textDiv.querySelector(".cursor");
      if (cur) cur.remove();
    }
    currentAbort = null;
    try { await refreshFiles(); } catch (_) {}
  }

  $("chatScroll").scrollTop = $("chatScroll").scrollHeight;
}

/* ── 8. Approval card ── */

function renderApprovalCard(toolLog, ev) {
  var card = document.createElement("div");
  card.className = "approval-card";
  card.innerHTML =
    '<div class="appr-title">需要确认工具</div>' +
    '<div class="appr-tool">' + escHtml(ev.tool || "tool") + "</div>" +
    '<div class="appr-sum">' + escHtml(ev.summary || "") + "</div>" +
    '<div class="appr-actions">' +
    '<button type="button" class="btn primary sm appr-yes">允许</button>' +
    '<button type="button" class="btn sm appr-no">拒绝</button>' +
    "</div>";

  var yesBtn = card.querySelector(".appr-yes");
  var noBtn = card.querySelector(".appr-no");
  var done = false;

  async function reply(allow) {
    if (done) return;
    done = true;
    yesBtn.disabled = true;
    noBtn.disabled = true;
    try {
      await api("/api/lab/approve", {
        method: "POST",
        body: JSON.stringify({ id: ev.id, allow: allow }),
      });
      card.classList.add(allow ? "ok" : "deny");
      card.querySelector(".appr-actions").textContent = allow ? "已允许" : "已拒绝";
    } catch (e) {
      card.querySelector(".appr-actions").textContent = "提交失败: " + e.message;
    }
  }

  yesBtn.addEventListener("click", function () { reply(true); });
  noBtn.addEventListener("click", function () { reply(false); });
  toolLog.appendChild(card);
  card.scrollIntoView({ behavior: "smooth", block: "nearest" });
}

/* ── 9. File panel ── */

async function refreshFiles() {
  var tree = $("fileTree");
  var cwdEl = $("fileCwd");
  if (!tree || !activeProject) return;

  if (cwdEl) cwdEl.textContent = fileCwd || ".";

  try {
    var data = await api("/api/lab/files?project_id=" + activeProject.slug + "&path=" + encodeURIComponent(fileCwd || "."));
    var files = data.files || [];
    tree.innerHTML = files.map(function (f) {
      var icon = f.isDir ? "📁" : fileIcon(f.name || f.path);
      var path = f.path || f.name;
      return '<div class="ft-row' + (f.isDir ? " dir" : "") + '" data-path="' + escHtml(path) + '" data-isdir="' + (f.isDir ? "1" : "0") + '" data-preview="' + escHtml(f.previewKind || "") + '">' +
        '<span style="flex-shrink:0;font-size:.9rem">' + icon + "</span>" +
        '<span class="ft-name">' + escHtml(f.name || f.path) + "</span>" +
        (f.isDir ? "" : '<span class="ft-size">' + fmtSize(f.size) + "</span>") +
        "</div>";
    }).join("");

    // Click handlers
    tree.querySelectorAll(".ft-row").forEach(function (row) {
      row.addEventListener("click", function () {
        var p = row.dataset.path;
        var isDir = row.dataset.isdir === "1";
        var pk = row.dataset.preview;
        if (isDir) {
          fileCwd = p;
          refreshFiles();
        } else {
          previewFile(p, pk);
        }
      });
    });
  } catch (e) {
    tree.innerHTML = '<div class="ft-err">' + escHtml(e.message) + "</div>";
  }
}

function fileUp() {
  if (!fileCwd || fileCwd === ".") return;
  // Get parent: "a/b/c" → "a/b"; "a" → "."
  var parts = fileCwd.split("/");
  parts.pop();
  fileCwd = parts.length === 0 ? "." : parts.join("/");
  refreshFiles();
}

async function previewFile(path, previewKind) {
  var preview = $("filePreview");
  if (!preview || !activeProject) return;
  try {
    var data = await api("/api/lab/files/content?project_id=" + activeProject.slug + "&path=" + encodeURIComponent(path));
    var prov = await loadProvenance(path);
    var kind = previewKind || data.previewKind || "text";

    var bodyHtml = "";
    switch (kind) {
      case "markdown":
        bodyHtml = '<div class="fp-md">' + renderMarkdown(data.content || "") + "</div>";
        break;
      case "image":
        bodyHtml = '<img src="' + labPath("/api/lab/files/download?project_id=" + activeProject.slug + "&path=" + encodeURIComponent(path)) + '" alt="' + escHtml(path) + '" style="max-width:100%;border-radius:8px" loading="lazy" />';
        break;
      case "pdf":
        bodyHtml = '<div class="hint">📕 PDF 文件 (' + fmtSize(data.size) + ') — <a href="' + labPath("/api/lab/files/download?project_id=" + activeProject.slug + "&path=" + encodeURIComponent(path)) + '" target="_blank">下载查看</a></div>';
        break;
      case "molecule":
        bodyHtml = '<pre class="fp-body">' + escHtml(data.content || "") + "</pre>";
        break;
      case "binary":
        bodyHtml = '<div class="hint">不支持预览 (' + fmtSize(data.size) + ")</div>";
        break;
      default:
        bodyHtml = '<pre class="fp-body">' + escHtml(data.content || "") + "</pre>";
        break;
    }
    preview.innerHTML =
      '<div class="fp-hd">📄 ' + escHtml(data.path || path) + " (" + fmtSize(data.size) + ")</div>" +
      bodyHtml +
      '<div class="pv">' + prov + "</div>";
  } catch (e) {
    preview.innerHTML = '<div class="ft-err">' + escHtml(e.message) + "</div>";
  }
}

async function loadProvenance(path) {
  if (!activeProject) return "";
  try {
    var data = await api("/api/lab/provenance?project_id=" + activeProject.slug + "&path=" + encodeURIComponent(path));
    if (!data.count) return '<div class="pv-empty">无溯源记录</div>';
    return data.records.map(function (r) {
      var mcp = (r.mcp_calls || []).map(function (m) { return m.tool + '("' + (m.query || "") + '")'; }).join(", ");
      return '<div class="pv-row">' +
        '<span class="pv ts">' + (r.ts || "").slice(0, 19).replace("T", " ") + "</span>" +
        '<span class="pv tag">' + (r.kind || "artifact") + "</span>" +
        '<span class="pv model">' + (r.model || "—") + "</span>" +
        (mcp ? '<span class="pv mcp">🔗 ' + escHtml(mcp) + "</span>" : "") +
        (r.content_hash ? '<span class="pv hash">#' + r.content_hash.slice(7, 15) + "</span>" : "") +
        "</div>";
    }).join("");
  } catch (_) {
    return "";
  }
}

/* ── 10. Composer ── */

function submitPrompt() {
  var inp = $("promptInput");
  if (!inp) return;
  var prompt = inp.value.trim();
  if (!prompt) return;
  inp.value = "";
  inp.style.height = "auto";
  var mode = ($("chatMode") && $("chatMode").value) || "agent";
  streamChat(prompt, mode).catch(function (err) {
    addErrorBubble($("chatScroll"), err.message);
  });
}

/* ── 11. Chrome wiring ── */

// Dark mode
(function () {
  var toggle = $("dmToggle");
  if (!toggle) return;
  // Restore preference
  if (localStorage.getItem("lumen-dark") === "1") {
    document.documentElement.classList.add("dark");
    toggle.textContent = "☀️";
  }
  toggle.addEventListener("click", function () {
    var isDark = document.documentElement.classList.toggle("dark");
    localStorage.setItem("lumen-dark", isDark ? "1" : "0");
    toggle.textContent = isDark ? "☀️" : "🌙";
  });
})();

// Mobile sidebar / inspector toggles + mask
(function () {
  function toggleSide() {
    var side = $("sidebarPanel");
    var mask = $("sideMask");
    if (side) side.classList.toggle("open");
    if (mask) mask.hidden = !(side && side.classList.contains("open"));
  }
  function toggleInsp() {
    var insp = $("inspectorPanel");
    if (insp) insp.classList.toggle("open");
  }
  var btnSide = $("btnSide");
  var btnInsp = $("btnInsp");
  var mask = $("sideMask");
  if (btnSide) btnSide.addEventListener("click", toggleSide);
  if (btnInsp) btnInsp.addEventListener("click", toggleInsp);
  if (mask) mask.addEventListener("click", function () {
    var side = $("sidebarPanel");
    if (side) side.classList.remove("open");
    mask.hidden = true;
  });
})();

// Refresh all
$("refreshAllBtn") && $("refreshAllBtn").addEventListener("click", function () {
  refreshHealth();
  loadProjects();
  refreshFiles();
});

// New project
$("newProjectBtn") && $("newProjectBtn").addEventListener("click", async function () {
  var title = prompt("课题名称");
  if (!title) return;
  try {
    activeProject = await api("/api/lab/projects", { method: "POST", body: JSON.stringify({ title: title }) });
    await loadProjects();
    refreshFiles();
  } catch (e) { alert(e.message); }
});

// New conversation
$("newConvBtn") && $("newConvBtn").addEventListener("click", newConv);

// File panel buttons
$("fileUp") && $("fileUp").addEventListener("click", fileUp);
$("fileRefresh") && $("fileRefresh").addEventListener("click", function () { refreshFiles(); });

// Composer
$("promptInput") && $("promptInput").addEventListener("keydown", function (e) {
  if (e.key === "Enter" && !e.shiftKey) {
    e.preventDefault();
    submitPrompt();
  }
});
$("composer") && $("composer").addEventListener("submit", function (e) {
  e.preventDefault();
  submitPrompt();
});

// Stop button
$("btnStop") && $("btnStop").addEventListener("click", function () {
  if (currentAbort) {
    currentAbort.abort();
    currentAbort = null;
    setRunStatus(false);
  }
});

// File upload
$("fileUpload") && $("fileUpload").addEventListener("change", async function () {
  var file = this.files && this.files[0];
  if (!file || !activeProject) return;
  try {
    var fd = new FormData();
    fd.append("file", file);
    var res = await fetch(labPath("/api/lab/files/upload?project_id=" + activeProject.slug), {
      method: "POST",
      body: fd,
    });
    if (!res.ok) {
      var txt = await res.text();
      throw new Error(txt);
    }
    await refreshFiles();
  } catch (e) {
    alert("上传失败: " + e.message);
  }
  this.value = "";
});

// Chips
document.querySelectorAll(".chip").forEach(function (btn) {
  btn.addEventListener("click", async function () {
    if (btn.dataset.brief) {
      try {
        var p = await ensureProject();
        var res = await api("/api/lab/brief", { method: "POST", body: JSON.stringify({ project_id: p.slug, topic: btn.dataset.brief }) });
        var chatScroll = $("chatScroll");
        var div = document.createElement("div");
        div.className = "chat-msg agent";
        div.textContent = "Brief 已写入 " + res.path;
        chatScroll.appendChild(div);
        div.scrollIntoView({ behavior: "smooth", block: "end" });
        setTimeout(refreshFiles, 1500);
      } catch (e) {
        var cs = $("chatScroll");
        var d2 = document.createElement("div");
        d2.className = "chat-msg agent";
        d2.textContent = "Brief 失败: " + e.message;
        cs.appendChild(d2);
      }
      return;
    }
    if (btn.dataset.prompt) streamChat(btn.dataset.prompt).catch(function (e) {
      addErrorBubble($("chatScroll"), e.message);
    });
  });
});

// Bridge link
$("bridgeLink") && $("bridgeLink").addEventListener("click", function (e) {
  e.preventDefault();
  window.open(API_BASE ? "/lumen-science/?embed=1&oasis=1" : "http://127.0.0.1:18990/", "_blank");
});

/* ── 12. Inspector tabs ── */

var ketcherLoaded = false, molLoaded = false;
document.querySelectorAll(".insp-tab").forEach(function (t) {
  t.addEventListener("click", function () {
    document.querySelectorAll(".insp-tab").forEach(function (b) { b.classList.remove("active"); });
    t.classList.add("active");
    var pane = t.dataset.pane;
    $("statusPane") && ($("statusPane").style.display = pane === "status" ? "block" : "none");
    $("filesPane") && ($("filesPane").style.display = pane === "files" ? "block" : "none");
    if ($("skillsPane")) {
      $("skillsPane").style.display = pane === "skills" ? "block" : "none";
      if (pane === "skills") loadSkills();
    }
    if ($("ketcherPane")) $("ketcherPane").style.display = pane === "ketcher" ? "block" : "none";
    if ($("moleculePane")) $("moleculePane").style.display = pane === "molecule" ? "block" : "none";
    if (pane === "ketcher" && !ketcherLoaded) {
      ketcherLoaded = true;
      var frame = $("ketcherFrame");
      if (frame) frame.src = "https://lifescience.opensource.epam.com/ketcher/standalone/index.html";
    }
    if (pane === "molecule" && !molLoaded) {
      molLoaded = true;
      loadMoleculeViewer();
    }
  });
});

async function loadMoleculeViewer() {
  var el = $("molViewer");
  if (!el) return;
  var script = document.createElement("script");
  script.src = "https://3Dmol.org/build/3Dmol-min.js";
  script.onload = function () {
    if (typeof $3Dmol === "undefined") { el.innerHTML = '<p class="hint">3Dmol 加载失败</p>'; return; }
    var viewer = $3Dmol.createViewer("molViewer", { backgroundColor: "#fbf9f6" });
    fetch("https://files.rcsb.org/download/4HHB.pdb").then(function (r) { return r.text(); }).then(function (pdb) {
      viewer.addModel(pdb, "pdb");
      viewer.setStyle({}, { cartoon: { color: "#c28b4b" } });
      viewer.zoomTo();
      viewer.render();
    }).catch(function () { el.innerHTML = '<p class="hint">输入 PDB ID 或路径查看结构</p>'; });
  };
  document.head.appendChild(script);
}

/* ── 13. Command palette (⌘K / Ctrl+K) ── */

var paletteCmds = [
  { label: "新建课题", action: function () { $("newProjectBtn") && $("newProjectBtn").click(); }, hotkey: "⌘N" },
  { label: "一键 Brief: aspirin", action: function () { ensureProject().then(function (p) { return api("/api/lab/brief", { method: "POST", body: JSON.stringify({ project_id: p.slug, topic: "aspirin" }) }); }).then(function (r) { var d = document.createElement("div"); d.className = "chat-msg agent"; d.textContent = "Brief 已写入 " + r.path; $("chatScroll").appendChild(d); }).catch(function (e) { addErrorBubble($("chatScroll"), e.message); }); } },
  { label: "文献检索: PubMed", action: function () { streamChat("用 pubmed 域检索最新文献").catch(function (e) { addErrorBubble($("chatScroll"), e.message); }); } },
  { label: "打开 Bridge", action: function () { window.open(API_BASE ? "/lumen-science/?embed=1&oasis=1" : "http://127.0.0.1:18990/", "_blank"); } },
  { label: "刷新状态", action: function () { refreshHealth(); } },
];

function openPalette() {
  var el = $("cmdPalette");
  if (!el) return;
  el.style.display = "flex";
  var inp = $("paletteInput");
  inp.value = "";
  inp.focus();
  renderPaletteItems("");
}
function closePalette() {
  var el = $("cmdPalette");
  if (el) el.style.display = "none";
}
function renderPaletteItems(filter) {
  var res = $("paletteResults");
  if (!res) return;
  var q = (filter || "").toLowerCase();
  var items = paletteCmds.filter(function (c) { return c.label.toLowerCase().indexOf(q) !== -1; });
  res.innerHTML = items.map(function (c, i) { return '<div class="item" data-idx="' + i + '"><span>' + c.label + '</span><span class="hotkey">' + (c.hotkey || "") + "</span></div>"; }).join("");
  res.querySelectorAll(".item").forEach(function (el) {
    el.addEventListener("click", function () {
      var cmd = paletteCmds[parseInt(el.dataset.idx)];
      if (cmd) { closePalette(); cmd.action(); }
    });
  });
}

$("paletteInput") && $("paletteInput").addEventListener("input", function (e) { renderPaletteItems(e.target.value); });
$("paletteInput") && $("paletteInput").addEventListener("keydown", function (e) {
  if (e.key === "Escape") closePalette();
  if (e.key === "Enter") {
    var sel = document.querySelector("#paletteResults .item");
    if (sel) sel.click();
  }
});
$("cmdPalette") && $("cmdPalette").addEventListener("click", function (e) { if (e.target === $("cmdPalette")) closePalette(); });
document.addEventListener("keydown", function (e) {
  if ((e.metaKey || e.ctrlKey) && e.key === "k") { e.preventDefault(); openPalette(); }
});

/* ── 14. Embed + Resize ── */

var params = new URLSearchParams(location.search);
if (params.get("embed") || params.get("oasis")) document.body.classList.add("embed-oasis");

(function () {
  function makeResizable(handle, panel, isRight) {
    if (!handle || !panel) return;
    var startX, startW;
    handle.addEventListener("mousedown", function (e) {
      startX = e.clientX; startW = panel.getBoundingClientRect().width;
      document.body.style.cursor = "col-resize"; document.body.style.userSelect = "none";
      handle.classList.add("active");
      function onMove(e) { var delta = isRight ? startX - e.clientX : e.clientX - startX; panel.style.width = Math.max(180, Math.min(480, startW + delta)) + "px"; }
      function onUp() { document.body.style.cursor = ""; document.body.style.userSelect = ""; handle.classList.remove("active"); document.removeEventListener("mousemove", onMove); document.removeEventListener("mouseup", onUp); }
      document.addEventListener("mousemove", onMove); document.addEventListener("mouseup", onUp);
    });
  }
  makeResizable($("resizeLeft"), $("sidebarPanel"), false);
  makeResizable($("resizeRight"), $("inspectorPanel"), true);
})();

/* ── 15. Init ── */

(async function init() {
  try {
    await refreshHealth();
    await loadProjects();
    renderThreadTabs();
  } catch (e) {
    var ib = $("inspectorBody");
    if (ib) ib.innerHTML = '<div class="ft-err">' + escHtml(e.message) + "</div>";
  }
  setTimeout(function () {
    var s = $("splash");
    if (s) s.classList.add("hide");
  }, 1200);
})();
