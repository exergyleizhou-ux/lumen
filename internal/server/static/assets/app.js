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
let permissionMode = "agent";

function loadStorage() {
  currentKey = localStorage.getItem("lumen_api_key") || "";
  currentProvider = localStorage.getItem("lumen_provider") || "deepseek";
  currentModel = localStorage.getItem("lumen_model") || "deepseek-chat";
  permissionMode = localStorage.getItem("lumen_mode") || "agent";
  updateModelBadge();
  syncModePills();
  if (document.getElementById("providerSelect")) {
    $("providerSelect").value = currentProvider;
    $("modelInput").value = currentModel;
    if (currentKey) $("keyInput").value = currentKey;
  }
}

function updateModelBadge() {
  const el = $("modelBadge");
  if (!el) return;
  if (currentKey) {
    el.textContent = `${currentProvider}/${currentModel}`;
    el.classList.add("live");
  } else {
    el.textContent = "未连接 · 点击设置";
    el.classList.remove("live");
  }
}

function syncModePills() {
  document.querySelectorAll(".mode-pill").forEach((b) => {
    b.classList.toggle("active", b.dataset.mode === permissionMode);
  });
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
  avatar.textContent = role === "user" ? "你" : "L";
  div.appendChild(avatar);

  const body = document.createElement("div");
  body.className = "msg-body";
  const bubble = document.createElement("div");
  bubble.className = "bubble";
  if (role === "assistant") {
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
  hd.innerHTML = `${spin}<span>${icon}</span><span>${escapeHtml(name)}</span>`;
  card.appendChild(hd);
  parent.querySelector(".msg-body").appendChild(card);
  return hd;
}

async function send() {
  const input = $("input");
  const prompt = input.value.trim();
  if ((!prompt && !pendingImages.length) || running) return;

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
    const body = { prompt, provider: currentProvider, model: currentModel, api_key: currentKey };
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
                el.querySelector(".msg-body").insertBefore(thinkEl, bubble);
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
                tokensIn += ev.usage.prompt_tokens || 0;
                tokensOut += ev.usage.completion_tokens || 0;
              }
              break;
            case "notice":
            case "error":
              if (ev.text) {
                const err = document.createElement("div");
                err.className = "msg-error";
                err.textContent = ev.text;
                el.querySelector(".msg-body").appendChild(err);
              }
              break;
            case "turn_done":
              turn++;
              break;
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
      el.querySelector(".msg-body").appendChild(err);
    }
  }

  running = false;
  abortCtrl = null;
  $("sendBtn").disabled = false;
  $("stopBtn").hidden = true;
  setStatus("就绪", false);
  updateFooter();
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
      btn.textContent = s.name.replace(".jsonl", "");
      btn.title = s.mtime;
      list.appendChild(btn);
    });
  } catch (_) {}
}

async function loadMemories() {
  try {
    const r = await fetch(API_BASE + "/v1/memories");
    const d = await r.json();
    const n = (d.memories || []).length;
    if (n > 0) {
      $("memPill").hidden = false;
      $("memCount").textContent = String(n);
    }
  } catch (_) {}
}

function bindEvents() {
  $("sendBtn")?.addEventListener("click", send);
  $("stopBtn")?.addEventListener("click", stopGeneration);
  $("settingsBtn")?.addEventListener("click", openSetup);
  $("connectBtn")?.addEventListener("click", connectModel);
  $("setupClose")?.addEventListener("click", () => $("setupModal").close());
  $("newChatBtn")?.addEventListener("click", newChat);

  const input = $("input");
  input?.addEventListener("input", () => autoResize(input));
  input?.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      send();
    }
  });

  document.querySelectorAll(".mode-pill").forEach((b) => {
    b.addEventListener("click", () => {
      permissionMode = b.dataset.mode;
      localStorage.setItem("lumen_mode", permissionMode);
      syncModePills();
    });
  });

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