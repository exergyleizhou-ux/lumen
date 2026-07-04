const $ = (id) => document.getElementById(id);
let activeProject = null;

async function api(path, opts = {}) {
  const r = await fetch(path, {
    headers: { "Content-Type": "application/json", ...(opts.headers || {}) },
    ...opts,
  });
  const text = await r.text();
  let data;
  try { data = JSON.parse(text); } catch { data = { raw: text }; }
  if (!r.ok) throw new Error(data.error || r.statusText);
  return data;
}

function appendMsg(cls, text) {
  $("welcome")?.remove();
  const el = document.createElement("div");
  el.className = `msg ${cls}`;
  el.textContent = text;
  $("chatScroll").appendChild(el);
  el.scrollIntoView({ behavior: "smooth", block: "end" });
}

async function refreshHealth() {
  const h = await api("/api/lab/health");
  const pack = h.research_pack || {};
  $("packBadge").textContent = pack.healthy ? `Pack ✓ ${pack.domain_tools || 0} tools` : "Pack ✗ 需 science start";
  const f = h.fleet || {};
  $("fleetBadge").textContent = `Fleet ${f.cs_connected || 0}/${f.cs_domains || 0}`;
  $("modeHint").textContent = `science_mode: ${h.science_mode || "hybrid"}`;
  $("inspectorBody").textContent = JSON.stringify(h, null, 2);
  return h;
}

async function loadProjects() {
  const list = await api("/api/lab/projects");
  const nav = $("projectList");
  nav.innerHTML = "";
  list.forEach((p) => {
    const btn = document.createElement("button");
    btn.type = "button";
    btn.className = "project-item" + (activeProject?.slug === p.slug ? " active" : "");
    btn.textContent = p.title;
    btn.onclick = () => { activeProject = p; loadProjects(); };
    nav.appendChild(btn);
  });
  if (!activeProject && list.length) activeProject = list[0];
}

async function ensureProject() {
  if (activeProject) return activeProject;
  const p = await api("/api/lab/projects", { method: "POST", body: JSON.stringify({ title: "默认课题" }) });
  activeProject = p;
  await loadProjects();
  return p;
}

async function streamChat(prompt) {
  const p = await ensureProject();
  appendMsg("user", prompt);
  const res = await fetch("/api/lab/chat", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ project_id: p.slug, prompt, mode: "plan" }),
  });
  const reader = res.body.getReader();
  const dec = new TextDecoder();
  let buf = "";
  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    buf += dec.decode(value, { stream: true });
    const lines = buf.split("\n");
    buf = lines.pop() || "";
    for (const line of lines) {
      if (!line.startsWith("data: ")) continue;
      try {
        const ev = JSON.parse(line.slice(6));
        if (ev.kind === "text" && ev.text) appendMsg("agent", ev.text);
        if (ev.kind === "tool" || ev.tool) appendMsg("tool", JSON.stringify(ev));
        if (ev.kind === "error") appendMsg("agent", "错误: " + ev.text);
      } catch (_) {}
    }
  }
}

$("composer")?.addEventListener("submit", (e) => {
  e.preventDefault();
  const prompt = ($("promptInput").value || "").trim();
  if (!prompt) return;
  $("promptInput").value = "";
  streamChat(prompt).catch((err) => appendMsg("agent", err.message));
});

$("newProjectBtn")?.addEventListener("click", async () => {
  const title = prompt("课题名称");
  if (!title) return;
  activeProject = await api("/api/lab/projects", { method: "POST", body: JSON.stringify({ title }) });
  await loadProjects();
});

document.querySelectorAll(".chip").forEach((btn) => {
  btn.addEventListener("click", async () => {
    if (btn.dataset.brief) {
      const p = await ensureProject();
      const res = await api("/api/lab/brief", { method: "POST", body: JSON.stringify({ project_id: p.slug, topic: btn.dataset.brief }) });
      appendMsg("agent", "Brief 已写入 " + res.path);
      return;
    }
    if (btn.dataset.prompt) streamChat(btn.dataset.prompt).catch((e) => appendMsg("agent", e.message));
  });
});

const params = new URLSearchParams(location.search);
if (params.get("embed") || params.get("oasis")) document.body.classList.add("embed-oasis");

(async () => {
  try {
    await refreshHealth();
    await loadProjects();
  } catch (e) {
    $("inspectorBody").textContent = String(e);
  }
})();