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

  // Links [text](url) — http/https or site-relative /
  html = html.replace(/\[([^\]]+)\]\(((?:https?:\/\/|\/)[^)\s]+)\)/g, '<a href="$2" target="_blank" rel="noopener">$1</a>');

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
      // Backend emits full Output once (agent.go) — replace, don't append
      // (progress may have streamed partial text into output already).
      if (ev.tool && ev.tool.id) {
        var existing = state.tools[ev.tool.id];
        if (existing) {
          if (ev.tool.name) existing.name = ev.tool.name;
          if (ev.tool.args) existing.args = ev.tool.args;
          // Full snapshot from agent — overwrite progress-streamed partials
          if (Object.prototype.hasOwnProperty.call(ev.tool, "output")) existing.output = ev.tool.output || "";
          existing.err = ev.tool.err || "";
          existing.status = ev.tool.err ? "error" : "done";
          if (ev.tool.truncated) existing.truncated = true;
        } else {
          state.tools[ev.tool.id] = {
            id: ev.tool.id,
            name: ev.tool.name || "",
            args: ev.tool.args || "",
            output: ev.tool.output || "",
            err: ev.tool.err || "",
            status: ev.tool.err ? "error" : "done",
            readOnly: ev.tool.read_only || false,
            truncated: !!ev.tool.truncated,
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
var threads = []; // {id,title,turn_count} from API sessions
var activeThread = "";
var currentAbort = null;
var fileCwd = ".";
var sseState = null; // per-turn SSE accumulator
var turnTasks = []; // this-turn tools for tasks pane
var skillsCache = [];

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
        activeThread = "";
        loadProjects();
        refreshFiles();
        loadSessions().then(function () {
          if (activeThread) openSession(activeThread);
        });
        loadSkills();
        loadComputeHosts();
        loadComputeJobs();
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
      loadSessions().then(function () {
        if (activeThread) openSession(activeThread);
      });
      loadSkills();
      loadComputeHosts();
      loadComputeJobs();
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
    skillsCache = d.skills || [];
    var s = skillsCache;
    if (!s.length) {
      el.innerHTML = '<div class="hint">暂无技能 — 安装 Research Pack 或 ~/.lumen/skills</div>';
      return;
    }
    el.innerHTML = s.map(function (sk) {
      var name = sk.name || sk;
      var en = sk.enabled !== false;
      return '<div class="skill-row">' +
        '<label class="skill-en"><input type="checkbox" data-skill="' + escHtml(name) + '"' + (en ? " checked" : "") + " /> 启用</label>" +
        '<button type="button" class="skill-name" data-inject="' + escHtml(name) + '">📋 ' + escHtml(name) + "</button>" +
        '<div class="skill-desc hint">' + escHtml(sk.description || "") + "</div>" +
        '<div class="skill-src hint">' + escHtml(sk.scope || sk.source || "") + "</div></div>";
    }).join("");
    el.querySelectorAll("[data-inject]").forEach(function (btn) {
      btn.addEventListener("click", function () { injectSkill(btn.getAttribute("data-inject")); });
    });
  } catch (e) {
    el.innerHTML = '<div class="ft-err">' + escHtml(e.message) + "</div>";
  }
}

function injectSkill(name) {
  var inp = $("promptInput");
  if (!inp) return;
  var prefix = "请使用技能「" + name + "」：";
  if (inp.value.indexOf(prefix) !== 0) inp.value = prefix + (inp.value || "");
  inp.focus();
  var hint = $("skillsHint");
  if (hint) hint.textContent = "已注入 " + name;
}

async function saveSkillsEnabled() {
  if (!activeProject) return;
  var enabled = [];
  document.querySelectorAll("#skillsBody input[data-skill]").forEach(function (b) {
    if (b.checked) enabled.push(b.getAttribute("data-skill"));
  });
  try {
    await api("/api/lab/skills?project_id=" + activeProject.slug, {
      method: "PUT",
      body: JSON.stringify({ project_id: activeProject.slug, enabled: enabled }),
    });
    var hint = $("skillsHint");
    if (hint) hint.textContent = "已保存 " + enabled.length + " 项";
  } catch (e) {
    alert(e.message);
  }
}

/* ── 5. Sessions (persisted) ── */

function renderThreadTabs() {
  var host = $("convTabs");
  if (!host) return;
  if (!threads.length) {
    host.innerHTML = '<span class="hint" style="padding:8px">无会话 — 点 ＋ 新建</span>';
    return;
  }
  host.innerHTML = threads
    .map(function (t) {
      var count = t.turn_count != null ? t.turn_count : (t.turns ? t.turns.length : 0);
      return '<button type="button" class="ctr-tab' + (t.id === activeThread ? " active" : "") + '" data-id="' + escHtml(t.id) + '"><span>' + escHtml(t.title || t.id) + (count ? " · " + count : "") + "</span></button>";
    })
    .join("");
  host.querySelectorAll(".ctr-tab").forEach(function (btn) {
    btn.addEventListener("click", function () {
      openSession(btn.dataset.id);
    });
  });
  renderSessionListSide();
}

function renderSessionListSide() {
  var el = $("sessionList");
  if (!el) return;
  if (!activeProject) {
    el.innerHTML = '<div class="hint">选择课题后加载…</div>';
    return;
  }
  if (!threads.length) {
    el.innerHTML = '<div class="hint">暂无会话</div>';
    return;
  }
  el.innerHTML = threads.map(function (t) {
    return '<button type="button" class="sess-item' + (t.id === activeThread ? " active" : "") + '" data-id="' + escHtml(t.id) + '">' +
      '<span class="sess-title">' + escHtml(t.title || t.id) + "</span>" +
      '<span class="sess-meta">' + (t.turn_count || 0) + " 轮</span></button>";
  }).join("");
  el.querySelectorAll(".sess-item").forEach(function (btn) {
    btn.addEventListener("click", function () { openSession(btn.dataset.id); });
  });
}

async function loadSessions() {
  if (!activeProject) return;
  try {
    var data = await api("/api/lab/projects/" + activeProject.slug + "/sessions");
    threads = data.sessions || [];
    if (!threads.length) {
      var created = await api("/api/lab/projects/" + activeProject.slug + "/sessions", {
        method: "POST",
        body: JSON.stringify({ title: "对话" }),
      });
      threads = [created];
    }
    if (!activeThread || !threads.some(function (t) { return t.id === activeThread; })) {
      activeThread = threads[0].id;
    }
    renderThreadTabs();
  } catch (e) {
    var el = $("sessionList");
    if (el) el.innerHTML = '<div class="ft-err">' + escHtml(e.message) + "</div>";
  }
}

async function openSession(id) {
  if (!activeProject || !id) return;
  activeThread = id;
  sseState = null;
  turnTasks = [];
  renderTasksPane();
  renderThreadTabs();
  try {
    var sess = await api("/api/lab/projects/" + activeProject.slug + "/sessions/" + encodeURIComponent(id));
    renderHistory(sess.turns || []);
  } catch (e) {
    clearChatScroll('<div class="ft-err">加载会话失败: ' + escHtml(e.message) + "</div>");
  }
}

function clearChatScroll(html) {
  var scroll = $("chatScroll");
  if (!scroll) return;
  scroll.innerHTML = html || "";
}

function renderHistory(turns) {
  clearChatScroll("");
  if (!turns || !turns.length) {
    clearChatScroll(
      '<section class="hero" id="welcome"><h2>空会话</h2><p>发送消息开始；历史会持久化，刷新后可恢复。</p></section>'
    );
    return;
  }
  turns.forEach(function (t) {
    if (t.role === "user") {
      var ue = document.createElement("div");
      ue.className = "chat-msg user";
      ue.textContent = t.text || "";
      $("chatScroll").appendChild(ue);
      return;
    }
    if (t.role === "assistant") {
      var bubble = createAgentBubble();
      if (t.text) bubble.textDiv.innerHTML = renderMarkdown(t.text);
      (t.tools || []).forEach(function (tool) {
        upsertToolCard(bubble.toolLog, {
          id: tool.id || tool.name,
          name: tool.name,
          args: tool.args || "",
          output: tool.output || "",
          err: tool.err || "",
          status: tool.status || (tool.err ? "error" : "done"),
        });
      });
    }
  });
  $("chatScroll").scrollTop = $("chatScroll").scrollHeight;
}

async function newConv() {
  if (!activeProject) {
    try { await ensureProject(); } catch (e) { alert(e.message); return; }
  }
  try {
    var sess = await api("/api/lab/projects/" + activeProject.slug + "/sessions", {
      method: "POST",
      body: JSON.stringify({ title: "对话 " + (threads.length + 1) }),
    });
    threads.unshift({ id: sess.id, title: sess.title, turn_count: 0 });
    activeThread = sess.id;
    renderThreadTabs();
    clearChatScroll(
      '<section class="hero" id="welcome"><h2>新对话</h2><p>描述你的科研任务 — 此会话会保存到服务器</p></section>'
    );
  } catch (e) {
    alert(e.message);
  }
}

function renderTasksPane() {
  var el = $("tasksBody");
  if (!el) return;
  if (!turnTasks.length) {
    el.innerHTML = '<div class="hint">本回合尚无工具调用</div>';
    return;
  }
  el.innerHTML = turnTasks.map(function (t) {
    return '<div class="task-row status-' + escHtml(t.status || "") + '">' +
      '<span class="task-name">⚙ ' + escHtml(t.name || t.id) + "</span>" +
      '<span class="task-st">' + escHtml(statusLabel(t.status)) + "</span></div>";
  }).join("");
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

function findToolCard(toolLog, id) {
  if (!toolLog || id == null) return null;
  var cards = toolLog.querySelectorAll(".tool-card");
  for (var i = 0; i < cards.length; i++) {
    if (cards[i].getAttribute("data-tool-id") === String(id)) return cards[i];
  }
  return null;
}

function setToolCardOpen(card, open) {
  if (!card) return;
  var body = card.querySelector(".tool-card-body");
  if (body) body.style.display = open ? "block" : "none";
  card.classList.toggle("is-open", !!open);
}

function upsertToolCard(toolLog, tool) {
  var id = tool.id;
  var existing = findToolCard(toolLog, id);
  if (existing) {
    updateToolCardDOM(existing, tool);
    return existing;
  }
  // Create new card
  var card = document.createElement("div");
  card.className = "tool-card status-" + (tool.status || "running");
  card.setAttribute("data-tool-id", String(id));
  card.innerHTML =
    '<div class="tool-card-hd">' +
    '<span class="tool-card-arrow">▸</span>' +
    '<span class="tool-card-icon">⚙</span>' +
    '<span class="tool-card-name">' + escHtml(tool.name || "tool") + "</span>" +
    '<span class="tool-card-status">' + statusLabel(tool.status) + "</span>" +
    "</div>" +
    '<div class="tool-card-body" style="display:none">' +
    '<div class="tool-card-section tool-card-args-section" style="' + (tool.args ? "" : "display:none") + '"><div class="tool-card-label">参数</div><pre class="tool-card-args">' + escHtml(tool.args || "") + "</pre></div>" +
    (tool.description ? '<div class="tool-card-section"><div class="tool-card-label">说明</div><div>' + escHtml(tool.description) + "</div></div>" : "") +
    '<div class="tool-card-section tool-card-output-section" style="display:none"><div class="tool-card-label">输出</div><pre class="tool-card-output"></pre></div>' +
    '<div class="tool-card-section tool-card-err-section" style="display:none"><div class="tool-card-label">错误</div><pre class="tool-card-err"></pre></div>' +
    "</div>";

  // Click header to toggle
  card.querySelector(".tool-card-hd").addEventListener("click", function () {
    setToolCardOpen(card, !card.classList.contains("is-open"));
  });

  toolLog.appendChild(card);
  card.scrollIntoView({ behavior: "smooth", block: "nearest" });
  return card;
}

function updateToolCardDOM(card, tool) {
  var open = card.classList.contains("is-open");
  card.className = "tool-card status-" + (tool.status || "running") + (open ? " is-open" : "");
  var statusEl = card.querySelector(".tool-card-status");
  if (statusEl) statusEl.textContent = statusLabel(tool.status);
  var nameEl = card.querySelector(".tool-card-name");
  if (nameEl && tool.name) nameEl.textContent = tool.name;

  if (tool.args) {
    var argsSec = card.querySelector(".tool-card-args-section");
    var argsPre = card.querySelector(".tool-card-args");
    if (argsSec) argsSec.style.display = "block";
    if (argsPre) argsPre.textContent = tool.args;
  }
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
  // Auto-expand when finished with content or on error
  if (tool.status === "error" || (tool.status === "done" && (tool.output || tool.err))) {
    setToolCardOpen(card, true);
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
  var hint = $("composerHint");
  if (running) {
    if (dot) dot.style.background = "#f59e0b";
    if (label) label.textContent = "运行中";
    if (hint) hint.textContent = "生成中… Enter 发送 · Shift+Enter 换行";
    if (stopBtn) stopBtn.disabled = false;
    if (sendBtn) sendBtn.disabled = true;
  } else {
    if (dot) dot.style.background = "";
    if (label) label.textContent = "就绪";
    if (hint) hint.textContent = "就绪 · Enter 发送 · Shift+Enter 换行";
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

  // Session id from server
  if (kind === "session" && ev.id) {
    activeThread = ev.id;
    var found = false;
    threads.forEach(function (t) {
      if (t.id === ev.id) {
        found = true;
        if (ev.title) t.title = ev.title;
      }
    });
    if (!found) threads.unshift({ id: ev.id, title: ev.title || "对话", turn_count: 0 });
    renderThreadTabs();
  }

  // Tool dispatch
  if (kind === "tool_dispatch" && ev.tool && ev.tool.id) {
    upsertToolCard(bubble.toolLog, state.tools[ev.tool.id]);
    syncTurnTask(state.tools[ev.tool.id]);
  }

  // Tool result
  if (kind === "tool_result" && ev.tool && ev.tool.id) {
    var card = findToolCard(bubble.toolLog, ev.tool.id);
    if (card) {
      updateToolCardDOM(card, state.tools[ev.tool.id]);
    } else {
      upsertToolCard(bubble.toolLog, state.tools[ev.tool.id]);
    }
    syncTurnTask(state.tools[ev.tool.id]);
  }

  // Tool progress
  if (kind === "tool_progress" && ev.tool && ev.tool.id) {
    var tcard = findToolCard(bubble.toolLog, ev.tool.id);
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
    if (bubble.textDiv) {
      var cur = bubble.textDiv.querySelector(".cursor");
      if (cur) cur.remove();
    }
    refreshFiles();
    loadSessions();
    currentAbort = null;
  }

  return state;
}

function syncTurnTask(tool) {
  if (!tool) return;
  var i, found = -1;
  for (i = 0; i < turnTasks.length; i++) {
    if (turnTasks[i].id === tool.id) { found = i; break; }
  }
  if (found >= 0) turnTasks[found] = tool;
  else turnTasks.push(tool);
  renderTasksPane();
}

async function streamChat(prompt, mode) {
  mode = mode || "agent";
  var p;
  try { p = await ensureProject(); } catch (e) { addErrorBubble($("chatScroll"), "无法获取课题: " + e.message); return; }
  if (!activeThread) {
    try { await loadSessions(); } catch (_) {}
  }
  $("welcome") && $("welcome").remove();

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
  turnTasks = [];
  renderTasksPane();
  setRunStatus(true);
  currentAbort = new AbortController();

  try {
    var res = await fetch(labPath("/api/lab/chat"), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ project_id: p.slug, prompt: prompt, mode: mode, session_id: activeThread || "" }),
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
    var dl = labPath("/api/lab/files/download?project_id=" + activeProject.slug + "&path=" + encodeURIComponent(path));
    switch (kind) {
      case "markdown":
        bodyHtml = '<div class="fp-md">' + renderMarkdown(data.content || "") + "</div>";
        break;
      case "image":
        bodyHtml = '<img src="' + dl + '" alt="' + escHtml(path) + '" style="max-width:100%;border-radius:8px" loading="lazy" />';
        break;
      case "pdf":
        bodyHtml = '<div class="hint">📕 PDF (' + fmtSize(data.size) + ') — <a href="' + dl + '" target="_blank">下载 / 新窗口打开</a></div>' +
          '<iframe class="fp-pdf" src="' + dl + '" title="pdf"></iframe>';
        break;
      case "office":
        bodyHtml = '<div class="hint">📄 Office 文本抽取 (' + escHtml(data.officeKind || "office") + ") · " + fmtSize(data.size) +
          ' — <a href="' + dl + '" target="_blank">下载原文件</a></div>' +
          (data.hint ? '<div class="hint">' + escHtml(data.hint) + "</div>" : "") +
          '<pre class="fp-body">' + escHtml(data.content || "(无文本)") + "</pre>";
        break;
      case "molecule":
        bodyHtml = '<pre class="fp-body">' + escHtml(data.content || "") + "</pre>";
        break;
      case "binary":
        bodyHtml = '<div class="hint">不支持内联预览 (' + fmtSize(data.size) + ') — <a href="' + dl + '" target="_blank">下载</a></div>';
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
  loadSessions();
  loadSkills();
  loadComputeJobs();
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
var PANE_IDS = ["status", "tasks", "files", "skills", "compute", "ketcher", "molecule"];
document.querySelectorAll(".insp-tab").forEach(function (t) {
  t.addEventListener("click", function () {
    document.querySelectorAll(".insp-tab").forEach(function (b) { b.classList.remove("active"); });
    t.classList.add("active");
    var pane = t.dataset.pane;
    PANE_IDS.forEach(function (id) {
      var el = $(id + "Pane");
      if (el) el.style.display = pane === id ? (id === "ketcher" || id === "molecule" ? "block" : "block") : "none";
    });
    if (pane === "skills") loadSkills();
    if (pane === "compute") { loadComputeHosts(); loadComputeJobs(); }
    if (pane === "tasks") renderTasksPane();
    if (pane === "files") refreshFiles();
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

/* ── File search ── */
async function runFileSearch() {
  var qEl = $("fileSearch");
  var hitsEl = $("fileSearchHits");
  if (!qEl || !hitsEl || !activeProject) return;
  var q = qEl.value.trim();
  if (!q) {
    hitsEl.hidden = true;
    hitsEl.innerHTML = "";
    return;
  }
  try {
    var data = await api("/api/lab/files/search?project_id=" + activeProject.slug + "&q=" + encodeURIComponent(q));
    var hits = data.hits || [];
    hitsEl.hidden = false;
    if (!hits.length) {
      hitsEl.innerHTML = '<div class="hint">无匹配</div>';
      return;
    }
    hitsEl.innerHTML = hits.map(function (h) {
      return '<div class="ft-row hit" data-path="' + escHtml(h.path) + '" data-isdir="' + (h.isDir ? "1" : "0") + '" data-preview="' + escHtml(h.previewKind || "") + '">' +
        '<span class="ft-name">' + escHtml(h.path) + '</span>' +
        '<span class="ft-size">' + escHtml(h.match) + "</span>" +
        (h.snippet ? '<div class="hit-snip">' + escHtml(h.snippet) + "</div>" : "") +
        "</div>";
    }).join("");
    hitsEl.querySelectorAll(".ft-row").forEach(function (row) {
      row.addEventListener("click", function () {
        if (row.dataset.isdir === "1") {
          fileCwd = row.dataset.path;
          refreshFiles();
        } else {
          previewFile(row.dataset.path, row.dataset.preview);
        }
      });
    });
  } catch (e) {
    hitsEl.hidden = false;
    hitsEl.innerHTML = '<div class="ft-err">' + escHtml(e.message) + "</div>";
  }
}

/* ── Compute ── */
async function loadComputeHosts() {
  var sel = $("computeHost");
  if (!sel) return;
  try {
    var data = await api("/api/lab/compute/ssh-hosts");
    var hosts = data.hosts || [];
    // Always ensure local option for zero-SSH productivity
    var hasLocal = hosts.some(function (h) {
      var a = typeof h === "string" ? h : (h.alias || h.Alias || "");
      return a === "local" || a === "localhost";
    });
    if (!hasLocal) hosts = [{ alias: "local" }].concat(hosts);
    sel.innerHTML = hosts.map(function (h) {
      var name = typeof h === "string" ? h : (h.alias || h.Alias || h.Host || h.host || h.name || "local");
      var label = name === "local" ? "local（本机 shell，无需 SSH）" : name;
      return '<option value="' + escHtml(name) + '">' + escHtml(label) + "</option>";
    }).join("");
  } catch (e) {
    sel.innerHTML = '<option value="local">local（本机 shell）</option>';
  }
}

async function loadComputeJobs() {
  var el = $("computeJobs");
  if (!el || !activeProject) return;
  try {
    var data = await api("/api/lab/compute/jobs?project_id=" + activeProject.slug);
    var jobs = data.jobs || [];
    if (!jobs.length) {
      el.innerHTML = '<div class="hint">暂无任务</div>';
      return;
    }
    el.innerHTML = jobs.map(function (j) {
      var outs = (j.outputs || []).map(function (o) {
        return escHtml(o.path) + (o.local_path ? " → " + escHtml(o.local_path) : "") + (o.error ? " (" + escHtml(o.error) + ")" : "");
      }).join("; ");
      return '<div class="job-card status-' + escHtml(j.status || "") + '">' +
        '<div class="job-hd"><strong>' + escHtml(j.id) + "</strong> · " + escHtml(j.status) + "</div>" +
        '<div class="hint mono">' + escHtml(j.host) + " · " + escHtml(j.command) + "</div>" +
        (j.output ? '<pre class="job-out">' + escHtml((j.output || "").slice(0, 500)) + "</pre>" : "") +
        (outs ? '<div class="hint">产物: ' + outs + "</div>" : "") +
        "</div>";
    }).join("");
  } catch (e) {
    el.innerHTML = '<div class="ft-err">' + escHtml(e.message) + "</div>";
  }
}

async function submitComputeJob() {
  if (!activeProject) return;
  var host = $("computeHost") && $("computeHost").value;
  var cmd = $("computeCmd") && $("computeCmd").value.trim();
  var globsRaw = $("computeGlobs") && $("computeGlobs").value.trim();
  if (!host || !cmd) {
    alert("需要主机和命令");
    return;
  }
  var globs = globsRaw ? globsRaw.split(",").map(function (s) { return s.trim(); }).filter(Boolean) : [];
  try {
    await api("/api/lab/compute/jobs?project_id=" + activeProject.slug, {
      method: "POST",
      body: JSON.stringify({ host: host, command: cmd, timeout_sec: 600, output_globs: globs }),
    });
    loadComputeJobs();
    // Poll a few times
    var n = 0;
    var timer = setInterval(function () {
      loadComputeJobs();
      if (++n > 20) clearInterval(timer);
    }, 2000);
  } catch (e) {
    alert(e.message);
  }
}

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

// Productivity wiring
$("skillsSaveBtn") && $("skillsSaveBtn").addEventListener("click", saveSkillsEnabled);
$("fileSearchBtn") && $("fileSearchBtn").addEventListener("click", runFileSearch);
$("fileSearch") && $("fileSearch").addEventListener("keydown", function (e) {
  if (e.key === "Enter") { e.preventDefault(); runFileSearch(); }
});
$("computeSubmit") && $("computeSubmit").addEventListener("click", submitComputeJob);
$("computeRefresh") && $("computeRefresh").addEventListener("click", loadComputeJobs);

/* ── 15. Init ── */

(async function init() {
  try {
    await refreshHealth();
    await loadProjects();
    if (activeProject) {
      await loadSessions();
      if (activeThread) await openSession(activeThread);
      loadSkills();
      loadComputeHosts();
    } else {
      renderThreadTabs();
    }
  } catch (e) {
    var ib = $("inspectorBody");
    if (ib) ib.innerHTML = '<div class="ft-err">' + escHtml(e.message) + "</div>";
  }
  setTimeout(function () {
    var s = $("splash");
    if (s) s.classList.add("hide");
  }, 800);
})();
