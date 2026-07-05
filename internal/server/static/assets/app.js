// Lumen web UI — goal:d6aa846b round9
const API_BASE =
  typeof location !== "undefined" && location.pathname.startsWith("/lumen")
    ? "/lumen"
    : "";

const $ = (id) => document.getElementById(id);

let running = false;
let abortCtrl = null;
let tokensIn = 0;
let tokensOut = 0;
let cost = 0;
let turn = 0;
let pendingImages = [];
let currentKey = "";
let currentProvider = "deepseek";
let currentModel = "deepseek-chat";
let permissionMode = "bypass";
let planReady = false;
let planPrompt = "";

function loadStorage() {
  currentKey = localStorage.getItem("lumen_api_key") || "";
  currentProvider = localStorage.getItem("lumen_provider") || "deepseek";
  currentModel = localStorage.getItem("lumen_model") || "deepseek-chat";
  permissionMode = localStorage.getItem("lumen_mode") || "bypass";
  updateModelBadge();
  syncModeSelect();
  if ($("providerSelect")) {
    $("providerSelect").value = currentProvider;
    $("modelInput").value = currentModel;
    if (currentKey) $("keyInput").value = currentKey;
  }
}

function updateModelBadge() {
  const el = $("modelBadge");
  if (!el) return;
  if (currentKey) {
    el.textContent = `${currentProvider}/${currentModel} · ${permissionMode}`;
    el.classList.add("live");
  } else {
    el.textContent = "未连接 · 点击设置";
    el.classList.remove("live");
  }
}

function syncModeSelect() {
  const sel = $("modeSelect");
  if (sel) sel.value = permissionMode;
}

let pendingApprovalId = null;

async function respondApproval(allow) {
  if (!pendingApprovalId) return;
  const id = pendingApprovalId;
  pendingApprovalId = null;
  $("approvalModal")?.close();
  try {
    await fetch(API_BASE + "/v1/approve", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ id, allow }),
    });
  } catch (_) {}
}

async function setServerMode(mode) {
  try {
    await fetch(API_BASE + "/v1/mode", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ mode }),
    });
  } catch (_) {}
}

function openSetup() {
  $("setupModal").showModal();
}

function hideWelcome() {
  const w = $("welcome");
  if (w) w.hidden = true;
}

function showWelcome() {
  const w = $("welcome");
  if (w) w.hidden = false;
}

function setStatus(text, busy) {
  const el = $("statStatus");
  if (!el) return;
  el.textContent = text;
  el.className = busy ? "stat-busy" : "stat-ok";
}

function updateFooter() {
  const tk = tokensIn + tokensOut;
  $("statTokens").textContent = tk > 0 ? `${(tk / 1000).toFixed(1)}k tokens` : "— tokens";
  $("statCost").textContent = cost > 0 ? `$${cost.toFixed(4)}` : "—";
  $("statTurn").textContent = `turn ${turn}`;
}

function autoResize(el) {
  el.style.height = "auto";
  el.style.height = Math.min(el.scrollHeight, 160) + "px";
}

function escapeHtml(s) {
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");
}

function renderMarkdown(text) {
  let html = escapeHtml(text);
  html = html.replace(/```(\w*)\n([\s\S]*?)```/g, (_, _lang, code) => {
    return `<pre><code>${code.trim()}</code></pre>`;
  });
  html = html.replace(/`([^`]+)`/g, "<code>$1</code>");
  html = html.replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>");
  html = html.replace(/\n\n/g, "</p><p>");
  html = html.replace(/\n/g, "<br>");
  return `<p>${html}</p>`;
}

function appendMsg(role, text) {
  hideWelcome();
  const chat = $("chat");
  const div = document.createElement("div");
  div.className = `msg ${role}`;

  const avatar = document.createElement("div");
  avatar.className = "msg-avatar";
  avatar.textContent = role === "user" ? "你" : role === "system" ? "⌘" : "L";
  div.appendChild(avatar);

  const body = document.createElement("div");
  body.className = "bubble";
  const bubble = document.createElement("div");
  bubble.className = "bubble";
  if (role === "assistant" || role === "system") {
    bubble.innerHTML = text ? renderMarkdown(text) : '<span class="cursor-blink"></span>';
  } else {
    bubble.textContent = text;
  }
  body.appendChild(bubble);
  div.appendChild(body);
  chat.appendChild(div);
  scrollChat();
  return { el: div, bubble };
}

function scrollChat() {
  const sc = $("chatScroll");
  if (sc) sc.scrollTop = sc.scrollHeight;
}

function addToolCard(parent, name, state) {
  const card = document.createElement("div");
  card.className = "tool-card";
  const hd = document.createElement("div");
  hd.className = `tool-hd ${state}`;
  const spin = state === "running" ? '<span class="tool-spin"></span>' : "";
  const icon = state === "done-ok" ? "✓" : state === "done-err" ? "✗" : "⚙";
  hd.innerHTML = `${spin}<span>${icon}</span><span>${escapeHtml(name)}</span>` +
    (state === "running" ? '<span class="tool-approve-btn" style="margin-left:auto;font-size:10px;cursor:pointer;padding:2px 8px;border-radius:4px;background:var(--ocs-success, #5b8c7a);color:#fff" onclick="event.stopPropagation();if(window.pendingApprovalId){fetch('/api/approve/'+window.pendingApprovalId,{method:'POST'});this.textContent='已批准';this.style.background='var(--ocs-muted)';?.close();}">✓ 批准</span>' : '');
  card.appendChild(hd);
  parent.querySelector(".bubble").appendChild(card);
  return hd;
}

function normalizeMode(m) {
  if (m === "plan" || m === "default" || m === "accept-edits") return m;
  if (m === "bypass" || m === "agent") return m === "agent" ? "agent" : "bypass";
  return "bypass";
}

function showPlanBar(prompt) {
  planReady = true;
  planPrompt = prompt || "";
  const bar = $("planBar");
  const text = $("planBarText");
  if (bar) bar.hidden = false;
  if (text) text.textContent = planPrompt ? `待审：${planPrompt}` : "计划待审";
}

function hidePlanBar() {
  planReady = false;
  planPrompt = "";
  const bar = $("planBar");
  if (bar) bar.hidden = true;
}

async function streamWorkflow(action, prompt) {
  running = true;
  $("sendBtn").disabled = true;
  $("stopBtn").hidden = false;
  setStatus("工作流…", true);

  const label = action === "workflow" ? `/workflow ${prompt}` :
    action === "ultra" ? `/ultra ${prompt}` :
    action === "goal" ? `/goal ${prompt}` : "/execute";
  appendMsg("user", label);
  const { el, bubble } = appendMsg("assistant", "");
  let assistantText = "";

  try {
    const resp = await fetch(API_BASE + "/v1/workflow", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        action,
        prompt,
        api_key: currentKey,
        provider: currentProvider,
      }),
      signal: abortCtrl?.signal,
    });

    if (!resp.ok) {
      const d = await resp.json().catch(() => ({}));
      throw new Error(d.error || `HTTP ${resp.status}`);
    }

    const reader = resp.body.getReader();
    const decoder = new TextDecoder();
    let buf = "";

    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      buf += decoder.decode(value, { stream: true });
      const lines = buf.split("\n");
      buf = lines.pop();
      for (const line of lines) {
        if (!line.startsWith("data: ")) continue;
        try {
          const ev = JSON.parse(line.slice(6));
          if (ev.kind === "text" || ev.kind === "phase") {
            assistantText += (ev.text || "") + (ev.kind === "phase" ? "\n" : "");
            bubble.innerHTML = renderMarkdown(assistantText) + '<span class="cursor-blink"></span>';
            scrollChat();
          } else if (ev.kind === "plan_ready") {
            showPlanBar(ev.text || prompt);
          } else if (ev.kind === "plan_start") {
            onPlanStart();
          } else if (ev.kind === "plan_step") {
            onPlanStep(ev.step || ev);
          } else if (ev.kind === "plan_done") {
            onPlanDone();
          } else if (ev.kind === "workflow_done") {
            hidePlanBar();
          } else if (ev.kind === "error") {
            const err = document.createElement("div");
            err.className = "msg-error";
            err.textContent = ev.text || "工作流错误";
            el.querySelector(".bubble").appendChild(err);
          }
        } catch (_) {}
      }
    }

    if (assistantText) {
      bubble.innerHTML = renderMarkdown(assistantText);
    } else if (!bubble.querySelector(".tool-card")) {
      bubble.innerHTML = "<p>（工作流完成）</p>";
    }
  } catch (e) {
    if (e.name !== "AbortError") {
      bubble.innerHTML = renderMarkdown(`工作流失败：${e.message}`);
    }
  }

  running = false;
  abortCtrl = null;
  $("sendBtn").disabled = false;
  $("stopBtn").hidden = true;
  setStatus("就绪", false);
  turn++;
  updateFooter();
  scrollChat();
}

async function runSlashCommand(cmd) {
  const lower = cmd.toLowerCase().trim();
  if (lower.startsWith("/workflow ")) {
    abortCtrl = new AbortController();
    await streamWorkflow("workflow", cmd.slice("/workflow ".length).trim());
    return;
  }
  if (lower.startsWith("/ultra ")) {
    abortCtrl = new AbortController();
    await streamWorkflow("ultra", cmd.slice("/ultra ".length).trim());
    return;
  }
  if (lower.startsWith("/goal ")) {
    abortCtrl = new AbortController();
    await streamWorkflow("goal", cmd.slice("/goal ".length).trim());
    return;
  }
  if (lower === "/execute") {
    abortCtrl = new AbortController();
    await streamWorkflow("execute", planPrompt);
    return;
  }
  if (lower === "/reject") {
    appendMsg("user", cmd);
    try {
      const r = await fetch(API_BASE + "/v1/workflow", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ action: "reject" }),
      });
      const d = await r.json();
      appendMsg("system", d.text || "已拒绝");
      hidePlanBar();
    } catch (_) {
      appendMsg("system", "拒绝失败");
    }
    return;
  }

  appendMsg("user", cmd);
  const { bubble } = appendMsg("system", "…");
  try {
    const r = await fetch(API_BASE + "/v1/command", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        command: cmd,
        api_key: currentKey,
        provider: currentProvider,
      }),
    });
    const d = await r.json();
    if (!r.ok) {
      bubble.innerHTML = renderMarkdown(d.error || "命令失败");
      return;
    }
    bubble.innerHTML = renderMarkdown(d.text || "完成");
    if (d.data?.plan_ready) {
      showPlanBar(d.data.prompt || "");
    }
    if (d.data?.executed || d.data?.rejected) {
      hidePlanBar();
    }
    if (d.data?.cost_usd != null) {
      cost = d.data.cost_usd;
      updateFooter();
    }
    if (d.data?.mode) {
      permissionMode = normalizeMode(d.data.mode);
      localStorage.setItem("lumen_mode", permissionMode);
      syncModeSelect();
      updateModelBadge();
    }
    if (d.data?.model) {
      currentModel = d.data.model;
      localStorage.setItem("lumen_model", currentModel);
      updateModelBadge();
    }
  } catch (_) {
    bubble.innerHTML = renderMarkdown("命令请求失败");
  }
  scrollChat();
}

async function send() {
  const input = $("input");
  const prompt = input.value.trim();
  if ((!prompt && !pendingImages.length) || running) return;

  if (prompt.startsWith("/")) {
    input.value = "";
    autoResize(input);
    await runSlashCommand(prompt);
    return;
  }

  if (!currentKey) {
    openSetup();
    return;
  }

  input.value = "";
  autoResize(input);
  running = true;
  abortCtrl = new AbortController();
  $("sendBtn").disabled = true;
  $("stopBtn").hidden = false;
  $("attachHint").hidden = !pendingImages.length;
  setStatus("生成中…", true);

  appendMsg("user", prompt || "(图片)");
  const { el, bubble } = appendMsg("assistant", "");
  let assistantText = "";
  let thinkEl = null;

  const imgs = pendingImages;
  pendingImages = [];
  $("attachHint").hidden = true;

  if (imgs.length) {
    const img = document.createElement("img");
    img.src = imgs[0];
    img.style.cssText = "max-width:220px;border-radius:10px;margin-top:8px";
    bubble.appendChild(img);
  }

  try {
    const body = {
      prompt,
      provider: currentProvider,
      model: currentModel,
      api_key: currentKey,
      mode: permissionMode,
    };
    if (imgs.length) body.images = imgs;

    const resp = await fetch(API_BASE + "/v1/chat", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
      signal: abortCtrl.signal,
    });

    if (!resp.ok) {
      throw new Error(`HTTP ${resp.status}`);
    }

    const reader = resp.body.getReader();
    const decoder = new TextDecoder();
    let buf = "";
    let lastTool = null;

    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      buf += decoder.decode(value, { stream: true });
      const lines = buf.split("\n");
      buf = lines.pop();

      for (const line of lines) {
        if (!line.startsWith("data: ")) continue;
        try {
          const ev = JSON.parse(line.slice(6));
          switch (ev.kind) {
            case "text":
              assistantText += ev.text || "";
              bubble.innerHTML = renderMarkdown(assistantText) + '<span class="cursor-blink"></span>';
              scrollChat();
              break;
            case "reasoning":
              if (!thinkEl) {
                thinkEl = document.createElement("div");
                thinkEl.className = "think-block";
                el.querySelector(".bubble").insertBefore(thinkEl, bubble);
              }
              thinkEl.textContent = (thinkEl.textContent || "思考中… ") + (ev.text || "");
              break;
            case "tool_dispatch":
              if (ev.tool) {
                lastTool = addToolCard(el, ev.tool.name || "tool", "running");
              }
              break;
            case "tool_result":
              if (lastTool && ev.tool) {
                lastTool.className = `tool-hd ${ev.tool.err ? "done-err" : "done-ok"}`;
                lastTool.querySelector(".tool-spin")?.remove();
              }
              break;
            case "usage":
              if (ev.usage) {
                tokensIn += ev.usage.prompt_tokens || ev.usage.cache_miss_tokens || 0;
                tokensOut += ev.usage.completion_tokens || 0;
                const hit = ev.usage.cache_hit_tokens || 0;
                const miss = ev.usage.cache_miss_tokens || ev.usage.prompt_tokens || 0;
                const out = ev.usage.completion_tokens || 0;
                cost += (miss * 0.14 + hit * 0.014 + out * 0.28) / 1e6;
              }
              break;
            case "notice":
            case "error":
              if (ev.text) {
                const err = document.createElement("div");
                err.className = "msg-error";
                err.textContent = ev.text;
                el.querySelector(".bubble").appendChild(err);
              }
              break;
            case "approval_request":
              pendingApprovalId = ev.id;
              $("approvalSummary").textContent = `${ev.tool}: ${ev.summary || ""}`;
              $("approvalModal")?.showModal();
              break;
            case "turn_done":
              turn++;
              break;
            case "plan_start":
              onPlanStart(); break;
            case "plan_step":
              onPlanStep(ev.step || ev); break;
            case "plan_done":
              onPlanDone(); break;
          }
        } catch (_) {}
      }
    }

    if (assistantText) {
      bubble.innerHTML = renderMarkdown(assistantText);
    } else if (!bubble.querySelector(".tool-card")) {
      bubble.innerHTML = "<p>（无文本输出）</p>";
    } else {
      bubble.remove();
    }
    if (thinkEl) thinkEl.textContent = thinkEl.textContent || "（推理完成）";
  } catch (e) {
    if (e.name === "AbortError") {
      bubble.innerHTML = renderMarkdown(assistantText || "（已停止）");
    } else {
      const err = document.createElement("div");
      err.className = "msg-error";
      err.textContent = "连接中断，请重试";
      el.querySelector(".bubble").appendChild(err);
    }
  }

  running = false;
  abortCtrl = null;
  $("sendBtn").disabled = false;
  $("stopBtn").hidden = true;
  setStatus("就绪", false);
  updateFooter();
  loadSessions();
  input.focus();
  scrollChat();
}

function stopGeneration() {
  abortCtrl?.abort();
}

function connectModel() {
  const key = $("keyInput").value.trim();
  const prov = $("providerSelect").value;
  const model = $("modelInput").value.trim() || "deepseek-chat";
  if (!key) {
    $("keyInput").focus();
    return;
  }
  localStorage.setItem("lumen_api_key", key);
  localStorage.setItem("lumen_provider", prov);
  localStorage.setItem("lumen_model", model);
  currentKey = key;
  currentProvider = prov;
  currentModel = model;
  updateModelBadge();
  $("setupModal").close();
  $("input").focus();
}

function newChat() {
  $("chat").querySelectorAll(".msg").forEach((n) => n.remove());
  showWelcome();
  setStatus("就绪", false);
}

async function resumeSession(name) {
  try {
    const r = await fetch(API_BASE + "/v1/sessions/resume", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name }),
    });
    const d = await r.json();
    if (!r.ok) {
      appendMsg("system", d.error || "无法恢复会话");
      return;
    }
    await loadSessionContent(name, true);
    appendMsg("system", `已恢复会话，可继续对话（${d.messages || 0} 条上下文）`);
  } catch (_) {
    appendMsg("system", "恢复会话失败");
  }
}

async function loadSessionContent(name, skipBanner) {
  try {
    const r = await fetch(API_BASE + "/v1/sessions/content?name=" + encodeURIComponent(name));
    if (!r.ok) return;
    const d = await r.json();
    $("chat").querySelectorAll(".msg").forEach((n) => n.remove());
    hideWelcome();
    if (!skipBanner) {
      appendMsg("system", `已加载会话 ${d.name}（${(d.messages || []).length} 条消息）`);
    }
    for (const m of d.messages || []) {
      appendMsg(m.role === "user" ? "user" : "assistant", m.content || "");
    }
    scrollChat();
  } catch (_) {}
}

async function loadSessions() {
  try {
    const r = await fetch(API_BASE + "/v1/sessions");
    const d = await r.json();
    const list = $("sessionList");
    const empty = $("sessionEmpty");
    list.querySelectorAll(".session-item").forEach((n) => n.remove());
    const sessions = d.sessions || [];
    if (!sessions.length) {
      empty.hidden = false;
      return;
    }
    empty.hidden = true;
    sessions.slice(0, 12).forEach((s) => {
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "session-item";
      const label = s.name.replace(".jsonl", "");
      btn.textContent = label.length > 28 ? label.slice(0, 25) + "…" : label;
      btn.title = s.mtime;
      btn.addEventListener("click", () => {
        resumeSession(s.name);
        setSidebarOpen(false);
      });
      const row=document.createElement('div');row.className='session-item';const del=document.createElement('span');del.textContent='✕';del.title='删除会话';del.style.cssText='display:none;color:#ef4444;cursor:pointer;padding:2px 6px;border-radius:4px;font-size:14px';del.addEventListener('click',function(e){e.stopPropagation();if(confirm('删除会话?')){fetch('/api/sessions/'+encodeURIComponent(s.name),{method:'DELETE'}).then(function(){renderSessions()})}});row.appendChild(btn);row.appendChild(del);list.appendChild(row);row.addEventListener('mouseenter',function(){del.style.display='block'});row.addEventListener('mouseleave',function(){del.style.display='none'});
    });
  } catch (_) {}
}

async function loadMemories() {
  try {
    const r = await fetch(API_BASE + "/v1/memories");
    const d = await r.json();
    const mems = d.memories || [];
    const n = mems.length;
    $("memPill").hidden = n === 0;
    $("memCount").textContent = String(n);
    return mems;
  } catch (_) {
    return [];
  }
}

function openMemoriesModal() {
  renderMemoriesList();
  $("memoriesModal")?.showModal();
}

async function renderMemoriesList() {
  const list = $("memList");
  if (!list) return;
  list.innerHTML = "";
  const mems = await loadMemories();
  if (!mems.length) {
    const li = document.createElement("li");
    li.textContent = "暂无记忆";
    list.appendChild(li);
    return;
  }
  for (const m of mems) {
    const li = document.createElement("li");
    const info = document.createElement("div");
    info.innerHTML = `<strong>${escapeHtml(m.title || m.name)}</strong><br><span style="color:var(--muted)">${escapeHtml((m.description || m.body || "").slice(0, 80))}</span>`;
    const del = document.createElement("button");
    del.type = "button";
    del.className = "mem-del";
    del.textContent = "删除";
    del.addEventListener("click", async () => {
      await fetch(API_BASE + "/v1/memories", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ action: "delete", name: m.name }),
      });
      renderMemoriesList();
    });
    li.appendChild(info);
    li.appendChild(del);
    list.appendChild(li);
  }
}

async function saveMemory() {
  const name = $("memName")?.value.trim();
  const body = $("memBody")?.value.trim();
  if (!name || !body) return;
  await fetch(API_BASE + "/v1/memories", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      action: "save",
      entry: { name, title: name, body, description: body.slice(0, 120), kind: "user" },
    }),
  });
  $("memName").value = "";
  $("memBody").value = "";
  await renderMemoriesList();
}

async function runDoctor() {
  appendMsg("system", "运行 lumen doctor…");
  try {
    const r = await fetch(API_BASE + "/v1/doctor");
    const d = await r.json();
    const lines = (d.results || []).map((x) => `${x.status === "ok" ? "✓" : x.status === "warn" ? "⚠" : "✗"} ${x.name}: ${x.detail || ""}`);
    appendMsg("system", lines.join("\n") || "自检完成");
  } catch (_) {
    appendMsg("system", "自检请求失败");
  }
}

async function fetchSkills() {
  const r = await fetch(API_BASE + "/v1/skills");
  const d = await r.json();
  return d.skills || [];
}

async function invokeSkill(name) {
  $("skillsModal")?.close();
  if (!currentKey) {
    openSetup();
    return;
  }
  $("input").value = `run the ${name} skill`;
  await send();
}

async function openSkillsModal() {
  const list = $("skillList");
  if (!list) return;
  list.innerHTML = "<li>加载中…</li>";
  $("skillsModal")?.showModal();
  try {
    const skills = await fetchSkills();
    list.innerHTML = "";
    if (!skills.length) {
      list.innerHTML = "<li>未加载 skills（检查 serve 工作目录）</li>";
      return;
    }
    for (const sk of skills) {
      const li = document.createElement("li");
      const info = document.createElement("div");
      info.innerHTML = `<strong>${escapeHtml(sk.name)}</strong><br><span style="color:var(--muted)">${escapeHtml(sk.description || "")}</span>`;
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "skill-invoke";
      btn.textContent = "调用";
      btn.addEventListener("click", () => invokeSkill(sk.name));
      li.appendChild(info);
      li.appendChild(btn);
      list.appendChild(li);
    }
  } catch (_) {
    list.innerHTML = "<li>skills 请求失败</li>";
  }
}

async function loadPresets() {
  try {
    const r = await fetch(API_BASE + "/v1/models");
    const d = await r.json();
    if (d.ui_mode) {
      permissionMode = normalizeMode(d.ui_mode);
      localStorage.setItem("lumen_mode", permissionMode);
      syncModeSelect();
      updateModelBadge();
    }
    const sel = $("presetSelect");
    if (!sel) return;
    sel.innerHTML = "";
    const presets = d.presets || [];
    if (!presets.length) {
      sel.innerHTML = '<option value="">（无预设）</option>';
      return;
    }
    for (const p of presets) {
      const opt = document.createElement("option");
      opt.value = p.name;
      opt.textContent = `${p.name} (${p.model})`;
      opt.dataset.provider = p.provider || "";
      opt.dataset.model = p.model || "";
      sel.appendChild(opt);
    }
    const match = presets.find((p) => p.model === currentModel || p.name === currentModel);
    if (match) sel.value = match.name;
  } catch (_) {}
}

function setSidebarOpen(open) {
  const sidebar = $("sidebar");
  const backdrop = $("sidebarBackdrop");
  const toggle = $("sidebarToggle");
  if (!sidebar) return;
  sidebar.classList.toggle("open", open);
  if (backdrop) backdrop.hidden = !open;
  if (toggle) toggle.setAttribute("aria-expanded", open ? "true" : "false");
}

function bindEvents() {
  $("sidebarToggle")?.addEventListener("click", () => {
    const open = !$("sidebar")?.classList.contains("open");
    setSidebarOpen(open);
  });
  $("sidebarBackdrop")?.addEventListener("click", () => setSidebarOpen(false));

  $("sendBtn")?.addEventListener("click", send);
  $("stopBtn")?.addEventListener("click", stopGeneration);
  $("settingsBtn")?.addEventListener("click", openSetup);
  $("connectBtn")?.addEventListener("click", connectModel);
  $("setupClose")?.addEventListener("click", () => $("setupModal").close());
  $("newChatBtn")?.addEventListener("click", () => {
    newChat();
    setSidebarOpen(false);
  });
  $("doctorBtn")?.addEventListener("click", () => {
    runDoctor();
    setSidebarOpen(false);
  });
  $("skillsBtn")?.addEventListener("click", () => {
    openSkillsModal();
    setSidebarOpen(false);
  });
  $("memPill")?.addEventListener("click", openMemoriesModal);
  $("memoriesClose")?.addEventListener("click", () => $("memoriesModal").close());
  $("memSaveBtn")?.addEventListener("click", saveMemory);
  $("skillsClose")?.addEventListener("click", () => $("skillsModal").close());
  $("planExecBtn")?.addEventListener("click", async () => {
    abortCtrl = new AbortController();
    await streamWorkflow("execute", planPrompt);
  });
  $("planRejectBtn")?.addEventListener("click", async () => {
    await runSlashCommand("/reject");
  });
  $("presetSelect")?.addEventListener("change", (e) => {
    const opt = e.target.selectedOptions[0];
    if (!opt) return;
    if (opt.dataset.provider) {
      currentProvider = opt.dataset.provider;
      localStorage.setItem("lumen_provider", currentProvider);
      $("providerSelect").value = currentProvider;
    }
    if (opt.dataset.model) {
      currentModel = opt.dataset.model;
      localStorage.setItem("lumen_model", currentModel);
      $("modelInput").value = currentModel;
    }
    updateModelBadge();
  });

  const input = $("input");
  input?.addEventListener("input", () => autoResize(input));
  input?.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      send();
    }
  });

  $("modeSelect")?.addEventListener("change", async (e) => {
    permissionMode = e.target.value;
    localStorage.setItem("lumen_mode", permissionMode);
    syncModeSelect();
    updateModelBadge();
    await setServerMode(permissionMode);
  });

  $("approvalAllow")?.addEventListener("click", () => respondApproval(true));
  $("approvalDeny")?.addEventListener("click", () => respondApproval(false));

  document.querySelectorAll(".prompt-chip").forEach((chip) => {
    chip.addEventListener("click", () => {
      if (!currentKey) {
        openSetup();
        return;
      }
      $("input").value = chip.dataset.prompt || "";
      autoResize($("input"));
      send();
    });
  });

  document.addEventListener("paste", (e) => {
    const items = e.clipboardData?.items;
    if (!items) return;
    for (const item of items) {
      if (item.type.startsWith("image/")) {
        e.preventDefault();
        const reader = new FileReader();
        reader.onload = () => {
          pendingImages.push(reader.result);
          $("attachHint").hidden = false;
        };
        reader.readAsDataURL(item.getAsFile());
        break;
      }
    }
  });
}

async function init() {
  loadStorage();
  bindEvents();
  updateFooter();
  await loadPresets();
  await setServerMode(permissionMode);
  loadSessions();
  loadMemories();

  if (!currentKey) {
    setTimeout(openSetup, 400);
  } else {
    $("input")?.focus();
  }

  try {
    await fetch(API_BASE + "/v1/status");
  } catch (_) {
    setStatus("离线", false);
  }
}

init();
// ── File panel ──
var filePanelOpen=false;
function toggleFilePanel(){
  filePanelOpen=!filePanelOpen;
  var p=document.getElementById("filePanel");
  if(p)p.hidden=!filePanelOpen;
  if(filePanelOpen)loadFileTree();
}
async function loadFileTree(){
  var el=document.getElementById("fileTree");if(!el)return;
  try{
    var data=await lumenAPI("/api/files?path=.");
    var files=data.files||[];
    el.innerHTML=files.map(function(f){return '<div class="ft-item'+(f.isDir?' ft-dir':'')+'" style="cursor:pointer"><span>'+(f.isDir?'📁':'📄')+'</span><span class="ft-name">'+escHtml(f.name)+'</span></div>';}).join("");
  }catch(e){el.innerHTML='<div class="ft-item"><span class="ft-name" style="color:var(--muted)">'+e.message+'</span></div>';}
}
function previewFile(path){
  var el=document.getElementById("filePreview");if(!el)return;
  fetch("/api/files/content?path="+encodeURIComponent(path)).then(function(r){return r.json();}).then(function(d){
    el.textContent=d.content||"";
  }).catch(function(e){el.textContent=e.message;});
}
document.getElementById("filesBtn")?.addEventListener("click",toggleFilePanel);
document.getElementById("filePanelClose")?.addEventListener("click",function(){filePanelOpen=false;var p=document.getElementById("filePanel");if(p)p.hidden=true;});

// ── Plan 看板 ──
var planSteps=[], planOpen=false, planApproved={};

function showPlanPanel(){
  var fp=$("filePanel"); if(fp)fp.hidden=false; planOpen=true;
  var pane=$("fpFilesPane"),ppane=$("fpPlanPane");
  if(pane)pane.style.display="none"; if(ppane)ppane.style.display="block";
  document.querySelectorAll(".fp-tab").forEach(function(t){t.classList.toggle("active",t.dataset.fptab==="plan");});
}
function showFilesPanel(){
  var pane=$("fpFilesPane"),ppane=$("fpPlanPane");
  if(pane)pane.style.display="block"; if(ppane)ppane.style.display="none";
  planOpen=false;
  document.querySelectorAll(".fp-tab").forEach(function(t){t.classList.toggle("active",t.dataset.fptab==="files");});
}
// Tab clicks
document.querySelectorAll(".fp-tab").forEach(function(t){t.addEventListener("click",function(){
  if(this.dataset.fptab==="plan")showPlanPanel();else showFilesPanel();
});});

function renderPlanStep(step){
  var el=$("planSteps"); if(!el) return;
  var card=document.createElement("div");
  card.className="plan-step-card"; card.setAttribute("data-step-id",step.id||"");
  card.style.cssText="margin-bottom:8px;padding:10px 12px;border:1px solid var(--ocs-line);border-radius:10px;background:var(--ocs-surface-soft);border-left:4px solid "+(planApproved[step.id]?"var(--ocs-success)":"var(--ocs-accent)");
  var riskColors={low:"#5b8c7a",mid:"#c28b4b",high:"#b42318"};
  var risk=step.risk||"low";
  card.innerHTML='<div style="display:flex;justify-content:space-between;align-items:flex-start;gap:8px"><div style="flex:1"><div style="font-size:12px;font-weight:650;color:var(--ocs-ink)">'+(step.idx||"")+". '+escapeHtml(step.title||"步骤")+'</div><div style="font-size:11px;color:var(--ocs-muted);margin-top:2px">'+escapeHtml(step.desc||"")+'</div>'+
    (step.files&&step.files.length?'<div style="margin-top:4px;font-size:10px">'+step.files.map(function(f){return'<span style="color:var(--ocs-accent);cursor:pointer;margin-right:8px">📄 '+escapeHtml(f)+'</span>';}).join("")+'</div>':'')+
    '<div style="display:flex;gap:6px;margin-top:6px"><span style="font-size:10px;padding:1px 6px;border-radius:4px;background:'+(riskColors[risk]||riskColors.low)+'20;color:'+(riskColors[risk]||riskColors.low)+'">'+(step.risk||"低")+'风险</span><span style="font-size:10px;color:var(--ocs-muted)">~'+((step.lines||0)||"?")+' 行</span></div></div>'+
    (planApproved[step.id]?'<span style="color:var(--ocs-success);font-weight:650;font-size:11px">✓ 已批准</span>':'<div style="display:flex;flex-direction:column;gap:3px;flex-shrink:0"><button class="btn sm plan-approve" style="font-size:10px;background:var(--ocs-success);color:#fff;border-color:var(--ocs-success)" data-sid="'+step.id+'">批准</button><button class="btn sm plan-skip" style="font-size:10px;color:var(--ocs-muted)" data-sid="'+step.id+'">跳过</button></div>')+
    '</div>';
  el.appendChild(card);
  // Wire buttons
  card.querySelector(".plan-approve")?.addEventListener("click",function(e){e.stopPropagation();planApproved[step.id]=true;refreshPlanUI();});
  card.querySelector(".plan-skip")?.addEventListener("click",function(e){e.stopPropagation();planApproved[step.id]="skip";refreshPlanUI();});
}

function refreshPlanUI(){
  var el=$("planSteps"); if(!el)return; el.innerHTML="";
  planSteps.forEach(renderPlanStep);
  // Show/hide actions
  var act=$("planActions"); if(act)act.style.display=planSteps.some(function(s){return planApproved[s.id];})?"flex":"none";
  // Update stats
  var stats=$("planStats"); if(stats&&planSteps.length){
    var files=new Set(); planSteps.forEach(function(s){(s.files||[]).forEach(function(f){files.add(f);});});
    stats.style.display="block";
    stats.innerHTML="📋 "+planSteps.length+" 步骤 · 📄 "+files.size+" 文件 · ~"+(planSteps.reduce(function(a,s){return a+(s.lines||0);},0)||"?")+" 行";
  }
  // Auto-show plan panel when steps arrive
  if(planSteps.length>0&&!planOpen) showPlanPanel();
}

// SSE plan event handlers
function onPlanStart(){planSteps=[];planApproved={};var el=$("planSteps");if(el)el.innerHTML="";showPlanPanel();}
function onPlanStep(step){planSteps.push(step);renderPlanStep(step);refreshPlanUI();}
function onPlanDone(){refreshPlanUI();var act=$("planActions");if(act)act.style.display="flex";}

// Wire approve all / reject all
$("planApproveAll")?.addEventListener("click",function(){
  planSteps.forEach(function(s){if(planApproved[s.id]!="skip")planApproved[s.id]=true;});
  refreshPlanUI();
  // Notify backend
  try{fetch("/api/plan/approve",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({approved:Object.keys(planApproved).filter(function(k){return planApproved[k]===true;})})});}catch(_){}
});
$("planRejectAll")?.addEventListener("click",function(){planSteps=[];planApproved={};refreshPlanUI();showFilesPanel();});

// Intercept SSE in the existing message handlers - patch the plan_ready case
var _origPlanReady = window.showPlanBar;
window.showPlanBar = function(prompt){
  if(_origPlanReady) _origPlanReady(prompt);
  // Also open plan panel
  setTimeout(function(){onPlanStart();},100);
};

// Hook into the existing SSE stream for plan_step events
var _origHandleSSE = window._handleChatLine || window.handleSSE;
if(!_origHandleSSE){
  // The SSE handling is inline in streamChat - we need to patch it
  // We'll hook via a MutationObserver on the chat container
  var planObserver = new MutationObserver(function(mutations){
    // Check if plan bar appeared
  });
}


// Input bar enhancements
function fillInput(text){
  var inp=$("input"); if(inp){inp.value+=text;inp.focus();}
  var menu=$("mentionMenu"); if(menu)menu.style.display="none";
}
$("mentionBtn")?.addEventListener("click",function(e){
  e.stopPropagation();
  var menu=$("mentionMenu"); if(menu)menu.style.display=menu.style.display==="block"?"none":"block";
});
document.addEventListener("click",function(){var m=$("mentionMenu");if(m)m.style.display="none";});
// Hover style for mention items
document.querySelectorAll(".mention-item").forEach(function(el){
  el.addEventListener("mouseenter",function(){this.style.background="var(--accent-light)";});
  el.addEventListener("mouseleave",function(){this.style.background="";});
});

// Plan send button
$("planSendBtn")?.addEventListener("click",function(){
  var inp=$("input"); if(!inp||!inp.value.trim()) return;
  // Set mode to plan and submit
  var modeSel=$("modeSelect"); if(modeSel)modeSel.value="plan";
  $("composer")?.querySelector('[type="submit"]')?.click();
});

// Token estimator
$("input")?.addEventListener("input",function(){
  var chars=this.value.length;
  var tokens=Math.max(1,Math.round(chars/4));
  var est=$("tokenEst"); if(est)est.textContent="~"+tokens+" tokens";
});

// ⌘K Command palette
document.addEventListener("keydown",function(e){
  if((e.metaKey||e.ctrlKey)&&e.key==="k"){e.preventDefault();showCmdPalette();}
});
function showCmdPalette(){
  var existing=$("cmdPalette");if(existing){existing.style.display="flex";return;}
  var overlay=document.createElement("div");overlay.id="cmdPalette";overlay.style.cssText="position:fixed;inset:0;background:rgba(0,0,0,.4);z-index:1000;display:flex;justify-content:center;padding-top:16vh;backdrop-filter:blur(4px)";
  var box=document.createElement("div");box.style.cssText="width:440px;max-height:360px;background:var(--surface);border-radius:14px;box-shadow:0 16px 48px rgba(0,0,0,.2);overflow:hidden;border:1px solid var(--rule)";
  var inp=document.createElement("input");inp.placeholder="搜索命令…";inp.style.cssText="width:100%;padding:12px 16px;border:none;border-bottom:1px solid var(--rule);font-size:14px;outline:none;font-family:inherit;background:transparent;color:var(--ink)";
  var results=document.createElement("div");results.style.cssText="max-height:280px;overflow-y:auto";
  var cmds=[
    {label:"/workflow 工作流",action:function(){fillInput("/workflow ");closePalette();}},
    {label:"/ultra 超级模式",action:function(){fillInput("/ultra ");closePalette();}},
    {label:"切换 Plan 模式",action:function(){var m=$("modeSelect");if(m)m.value="plan";closePalette();}},
    {label:"切换暗色模式",action:function(){var b=$("dmToggle");if(b)b.click();closePalette();}},
    {label:"打开文件面板",action:function(){toggleFilePanel();closePalette();}},
    {label:"打开设置",action:function(){$("settingsBtn")?.click();closePalette();}},
  ];
  function render(q){results.innerHTML="";cmds.filter(function(c){return c.label.toLowerCase().includes((q||"").toLowerCase());}).forEach(function(c){var d=document.createElement("div");d.style.cssText="padding:8px 16px;font-size:13px;cursor:pointer;font-weight:650";d.textContent=c.label;d.addEventListener("click",c.action);d.addEventListener("mouseenter",function(){d.style.background="var(--sidebar-hover)";});results.appendChild(d);});}
  render("");inp.addEventListener("input",function(){render(inp.value);});
  inp.addEventListener("keydown",function(e){if(e.key==="Escape")closePalette();if(e.key==="Enter"){var first=results.querySelector("div");if(first)first.click();}});
  overlay.addEventListener("click",function(e){if(e.target===overlay)closePalette();});
  function closePalette(){overlay.remove();}
  box.appendChild(inp);box.appendChild(results);overlay.appendChild(box);document.body.appendChild(overlay);inp.focus();
}

// ── Right panel tabs ──
document.querySelectorAll(".rp-tab").forEach(function(t){t.addEventListener("click",function(){
  document.querySelectorAll(".rp-tab").forEach(function(b){b.classList.remove("text-accent","border-accent");b.classList.add("text-muted");});
  this.classList.add("text-accent","border-accent");this.classList.remove("text-muted");
  var p=this.dataset.rptab;
  ["rpFiles","rpPlan","rpChanges"].forEach(function(id){var el=document.getElementById(id);if(el)el.style.display=id==="rp"+p.charAt(0).toUpperCase()+p.slice(1)?"block":"none";});
});});

// ── Mode buttons ──
document.querySelectorAll(".mode-btn").forEach(function(b){b.addEventListener("click",function(){
  document.querySelectorAll(".mode-btn").forEach(function(x){x.classList.remove("bg-accent","text-white");});
  this.classList.add("bg-accent","text-white");window._chatMode=this.dataset.mode;
});});

// ── Suggest tasks ──
var suggestTasks=[{icon:"🔍",text:"分析项目结构",prompt:"分析当前项目的代码结构和主要模块"},{icon:"🐛",text:"找 bug",prompt:"我遇到了一个 bug，帮我排查"},{icon:"✨",text:"加新功能",prompt:"我想给项目添加一个新功能"},{icon:"📝",text:"写测试",prompt:"帮我写单元测试"},{icon:"📚",text:"解释代码",prompt:"帮我解释一下这段代码"}];
(function(){var el=document.getElementById("suggestTasks");if(!el)return;el.innerHTML=suggestTasks.map(function(t){return'<button class="flex items-center gap-2 px-4 py-2.5 text-xs font-semibold bg-card border border-rule rounded-2xl hover:border-accent hover:bg-accent-soft transition-all" onclick="var i=document.getElementById(\'input\');if(i){i.value=this.dataset.p;document.getElementById(\'sendBtn\').click();}" data-p="'+t.prompt+'">'+t.icon+' '+t.text+'</button>';}).join("");})();

// ── Token estimator ──
document.getElementById("input")?.addEventListener("input",function(){var t=document.getElementById("tokenEst");if(t)t.textContent="~"+Math.max(1,Math.round(this.value.length/4))+" tokens";});

// ── Plan send ──
document.getElementById("planSendBtn")?.addEventListener("click",function(){window._chatMode="plan";document.getElementById("sendBtn")?.click();});

// ── Session filter ──
document.getElementById("sessionSearch")?.addEventListener("input",function(){var q=this.value.toLowerCase();document.querySelectorAll("#sessionList .session-item").forEach(function(r){r.style.display=q&&!r.textContent.toLowerCase().includes(q)?"none":"";});});

// ── Mention menu ──
var mentionItems=[{label:"📄 @file 引用文件",cmd:"@file "},{label:"🧩 @skill 调用技能",cmd:"@skill "},{label:"🧠 @memory 引用记忆",cmd:"@memory "}];
var mm=document.getElementById("mentionMenu");
if(mm){mm.innerHTML=mentionItems.map(function(m){return'<div class="px-3 py-1.5 text-xs hover:bg-hover cursor-pointer rounded-lg transition-colors" onclick="fillInput(\''+m.cmd+'\');document.getElementById(\'mentionMenu\').classList.add(\'hidden\')">'+m.label+'</div>';}).join("");}
document.getElementById("mentionBtn")?.addEventListener("click",function(e){e.stopPropagation();var m=document.getElementById("mentionMenu");if(m)m.classList.toggle("hidden");});
document.addEventListener("click",function(){document.getElementById("mentionMenu")?.classList.add("hidden");});

function fillInput(t){var i=document.getElementById("input");if(i){i.value+=t;i.focus();}}

// ── Dark mode ──
var dm=localStorage.getItem("lumen-dm"),dt=document.getElementById("dmToggle");
if(dm==="dark")document.documentElement.classList.add("dark-mode");
if(dt){dt.textContent=dm==="dark"?"☀":"🌙";dt.onclick=function(){document.documentElement.classList.toggle("dark-mode");var d=document.documentElement.classList.contains("dark-mode");localStorage.setItem("lumen-dm",d?"dark":"light");dt.textContent=d?"☀":"🌙";};}

// Widen file panel toggle
window.toggleFilePanel=function(){var fp=document.getElementById("rpFiles");if(fp)fp.closest("aside").classList.toggle("w-[360px]");};
