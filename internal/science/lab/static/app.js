const $ = (id) => document.getElementById(id);
let activeProject = null;

var threads=[{id:"main",title:"对话"}],activeThread="main";
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
  el.className = "chat-msg " + cls;
  el.textContent = text;
  $("chatScroll").appendChild(el);
  el.scrollIntoView({ behavior: "smooth", block: "end" });
}

async function refreshHealth() {
  const h = await api("/api/lab/health");
  const pack = h.research_pack || {};
  const f = h.fleet || {};
  $("packBadge").textContent = pack.healthy ? `${pack.domain_tools || 0} tools · ${pack.skills || 0} skills` : "未安装 Research Pack";
  $("fleetBadge").textContent = `⚡ ${f.connected_total || 0}/${f.cs_domains || 0} fleet`;
  $("modeHint").textContent = h.science_mode || "hybrid";
  // Structured status display
  $("inspectorBody").innerHTML = [
    `<div class="sr"><span class="sr k">状态</span><span class="sr v ok">● 在线</span></div>`,
    `<div class="sr"><span class="sr k">版本</span><span class="sr v">${escHtml(h.version||'dev')}</span></div>`,
    `<div class="sr"><span class="sr k">模式</span><span class="sr v">${escHtml(h.science_mode||'hybrid')}</span></div>`,
    `<div class="sr-div"></div>`,
    `<div class="sr"><span class="sr k">Research</span><span class="sr v ${pack.healthy?'ok':''}">${pack.healthy?'✓':'✗'} ${pack.domain_tools||0} tools</span></div>`,
    `<div class="sr"><span class="sr k">CS fleet</span><span class="sr v">${f.cs_connected||0}/${f.cs_domains||0}</span></div>`,
    `<div class="sr"><span class="sr k">原生 fleet</span><span class="sr v">${f.lumen_native||0}</span></div>`,
    `<div class="sr-div"></div>`,
    `<div class="sr"><span class="sr k">模型</span><span class="sr v">${escHtml(h.provider?.masked||'—')}</span></div>`,
  ].join('');
  return h;
}

async function loadProjects() {
  const list = await api("/api/lab/projects");
  const nav = $("projectList");
  nav.innerHTML = "";
  list.forEach((p) => {
    const btn = document.createElement("button");
    btn.type = "button";
    btn.className = "" + (activeProject?.slug === p.slug ? " active" : "");
    btn.textContent = p.title;
    btn.onclick = () => { activeProject = p; loadProjects(); refreshFiles(); };
    nav.appendChild(btn);
  });
  if (!activeProject && list.length) { activeProject = list[0]; refreshFiles(); }
}

async function ensureProject() {
  if (activeProject) return activeProject;
  const p = await api("/api/lab/projects", { method: "POST", body: JSON.stringify({ title: "默认课题" }) });
  activeProject = p;
  await loadProjects();
  return p;
}

async function streamChat(prompt, mode) {
  mode = mode || "plan";
  var p = await ensureProject();
  // User bubble
  var ue = document.createElement("div"); ue.className = "chat-msg user"; ue.textContent = prompt;
  $("chatScroll").appendChild(ue);
  // Agent bubble — single element, text accumulated
  $("welcome")?.remove();
  var ae = document.createElement("div"); ae.className = "chat-msg agent";
  $("chatScroll").appendChild(ae);
  var textAcc = "";
  try {
    var res = await fetch("/api/lab/chat", {method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({project_id:p.slug,prompt:prompt,mode:mode,session_id:activeThread})});
    var reader = res.body.getReader(); var dec = new TextDecoder(); var buf = "";
    while(true){
      var r = await reader.read(); if(r.done)break;
      buf += dec.decode(r.value,{stream:true});
      var lines = buf.split("\n"); buf = lines.pop();
      for(var i=0;i<lines.length;i++){
        var line = lines[i]; if(line.indexOf("data:")!==0)continue;
        var json = line.slice(5).trim(); if(!json.startsWith("{"))continue;
        try{var ev=JSON.parse(json);
          if(ev.text&&(ev.kind==="text"||ev.kind==="thinking")){textAcc+=ev.text;ae.textContent=textAcc;}
          if(ev.kind==="error"){textAcc+="\n❌ "+(ev.text||"err");ae.textContent=textAcc;}
        }catch(_){}
      }
      $("chatScroll").scrollTop = $("chatScroll").scrollHeight;
    }
  }catch(e){ae.textContent = "错误: "+e.message;}
  refreshFiles();
}


// ── File panel ──

async function refreshFiles() {
  const el = $("fileTree");
  if (!el || !activeProject) return;
  try {
    const data = await api(`/api/lab/files?project_id=${activeProject.slug}`);
    const files = data.files || [];
    el.innerHTML = files.map(f => {
      const icon = f.isDir ? "📁" : fileIcon(f.name);
      return `<div class="ft-row ${f.isDir ? "dir" : ""}" data-path="${f.name}" onclick="${f.isDir ? "" : `previewFile('${f.name}')`}">
        <span style="flex-shrink:0;font-size:.9rem">${icon}</span>
        <span class="ft-name">${f.name}</span>
        ${!f.isDir ? `<span class="ft-size">${fmtSize(f.size)}</span>` : ""}
      </div>`;
    }).join("");
  } catch (e) {
    el.innerHTML = `<div class="ft-err">${e.message}</div>`;
  }
}

async function previewFile(path) {
  const preview = $("filePreview");
  if (!preview || !activeProject) return;
  try {
    const data = await api(`/api/lab/files/content?project_id=${activeProject.slug}&path=${encodeURIComponent(path)}`);
    const prov = await loadProvenance(path);
    preview.innerHTML = `<div class="fp-hd">📄 ${data.path} (${fmtSize(data.size)})</div>
      <pre class="fp-body">${escHtml(data.content)}</pre>
      <div class="pv">${prov}</div>`;
  } catch (e) {
    preview.innerHTML = `<div class="ft-err">${e.message}</div>`;
  }
}

async function loadProvenance(path) {
  try {
    const data = await api(`/api/lab/provenance?project_id=${activeProject.slug}&path=${encodeURIComponent(path)}`);
    if (!data.count) return '<div class="pv-empty">无溯源记录</div>';
    return data.records.map(r => {
      const mcp = (r.mcp_calls || []).map(m => `${m.tool}("${m.query || ''}")`).join(', ');
      return `<div class="pv-row">
        <span class="pv ts">${(r.ts || '').slice(0,19).replace('T',' ')}</span>
        <span class="pv tag">${r.kind || 'artifact'}</span>
        <span class="pv model">${r.model || '—'}</span>
        ${mcp ? `<span class="pv mcp">🔗 ${mcp}</span>` : ''}
        ${r.content_hash ? `<span class="pv hash">#${r.content_hash.slice(7,15)}</span>` : ''}
      </div>`;
    }).join('');
  } catch {
    return '';
  }
}

function fileIcon(name) {
  const ext = name.split(".").pop().toLowerCase();
  const map = { md: "📝", py: "🐍", r: "📊", json: "📋", csv: "📈", png: "🖼", jpg: "🖼", jpeg: "🖼", svg: "🖼", pdf: "📕", html: "🌐", css: "🎨", js: "📜", txt: "📄", log: "📋", yml: "⚙", yaml: "⚙", toml: "⚙" };
  return map[ext] || "📄";
}

function fmtSize(bytes) {
  if (!bytes || bytes < 1024) return `${bytes || 0}B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)}KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)}MB`;
}

function escHtml(s) {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

// ── Event wiring ──

$("composer")?.addEventListener("submit", function(e) {
  e.preventDefault();
  var inp = $("promptInput"); if (!inp) return;
  var prompt = inp.value.trim(); if (!prompt) return;
  inp.value = "";
  var mode = $("chatMode")?.value || "plan";
  streamChat(prompt, mode).catch(function(err) { appendMsg("agent", "错误: " + err.message); });
});

$("newProjectBtn")?.addEventListener("click", async () => {
  const title = prompt("课题名称");
  if (!title) return;
  activeProject = await api("/api/lab/projects", { method: "POST", body: JSON.stringify({ title }) });
  await loadProjects();
  refreshFiles();
});

document.querySelectorAll(".chip").forEach((btn) => {
  btn.addEventListener("click", async () => {
    if (btn.dataset.brief) {
      const p = await ensureProject();
      const res = await api("/api/lab/brief", { method: "POST", body: JSON.stringify({ project_id: p.slug, topic: btn.dataset.brief }) });
      appendMsg("agent", "Brief 已写入 " + res.path);
      setTimeout(refreshFiles, 1500);
      return;
    }
    if (btn.dataset.prompt) streamChat(btn.dataset.prompt).catch((e) => appendMsg("agent", e.message));
  });
});

// Tab toggle: inspector tabs
let ketcherLoaded = false, molLoaded = false;
document.querySelectorAll(".insp-tab").forEach((t) => {
  t.addEventListener("click", () => {
    document.querySelectorAll(".insp-tab").forEach(b => b.classList.remove("active"));
    t.classList.add("active");
    const pane = t.dataset.pane;
    $("statusPane").style.display = pane === "status" ? "block" : "none";
    $("filesPane").style.display = pane === "files" ? "block" : "none";
    if ($("ketcherPane")) $("ketcherPane").style.display = pane === "ketcher" ? "block" : "none";
    if ($("moleculePane")) $("moleculePane").style.display = pane === "molecule" ? "block" : "none";
    if (pane === "ketcher" && !ketcherLoaded) {
      ketcherLoaded = true;
      const frame = $("ketcherFrame");
      if (frame) frame.src = "https://lifescience.opensource.epam.com/ketcher/standalone/index.html";
    }
    if (pane === "molecule" && !molLoaded) {
      molLoaded = true;
      loadMoleculeViewer();
    }
  });
});

// ── Molecule viewer (3Dmol.js) ──
async function loadMoleculeViewer() {
  const el = $("molViewer");
  if (!el) return;
  // Load 3Dmol.js from CDN
  const script = document.createElement("script");
  script.src = "https://3Dmol.org/build/3Dmol-min.js";
  script.onload = () => {
    if (typeof $3Dmol === "undefined") { el.innerHTML = "<p class='hint'>3Dmol 加载失败</p>"; return; }
    const viewer = $3Dmol.createViewer("molViewer", { backgroundColor: "#fbf9f6" });
    // Default: load a simple demo
    fetch("https://files.rcsb.org/download/4HHB.pdb").then(r => r.text()).then(pdb => {
      viewer.addModel(pdb, "pdb");
      viewer.setStyle({}, { cartoon: { color: "#c28b4b" } });
      viewer.zoomTo();
      viewer.render();
    }).catch(() => el.innerHTML = "<p class='hint'>输入 PDB ID 或路径查看结构</p>");
  };
  document.head.appendChild(script);
}

// ── Command palette (⌘K / Ctrl+K) ──
const paletteCmds = [
  { label: "新建课题", action: () => { $("newProjectBtn")?.click(); }, hotkey: "⌘N" },
  { label: "一键 Brief: aspirin", action: () => { ensureProject().then(p => api("/api/lab/brief", { method: "POST", body: JSON.stringify({ project_id: p.slug, topic: "aspirin" }) })).then(r => appendMsg("agent", "Brief 已写入 " + r.path)).catch(e => appendMsg("agent", e.message)); } },
  { label: "文献检索: PubMed", action: () => { streamChat("用 pubmed 域检索最新文献").catch(e => appendMsg("agent", e.message)); } },
  { label: "打开 Bridge", action: () => { window.open("http://127.0.0.1:18990/", "_blank"); } },
  { label: "刷新状态", action: () => { refreshHealth(); } },
];

function openPalette() {
  const el = $("cmdPalette");
  if (!el) return;
  el.style.display = "flex";
  const inp = $("paletteInput");
  inp.value = "";
  inp.focus();
  renderPaletteItems("");
}
function closePalette() {
  const el = $("cmdPalette");
  if (el) el.style.display = "none";
}
function renderPaletteItems(filter) {
  const res = $("paletteResults");
  if (!res) return;
  const q = (filter || "").toLowerCase();
  const items = paletteCmds.filter(c => c.label.toLowerCase().includes(q));
  res.innerHTML = items.map((c, i) => `<div class="item" data-idx="${i}">
    <span>${c.label}</span><span class="hotkey">${c.hotkey || ""}</span>
  </div>`).join("");
  res.querySelectorAll(".item").forEach(el => {
    el.addEventListener("click", () => {
      const cmd = paletteCmds[parseInt(el.dataset.idx)];
      if (cmd) { closePalette(); cmd.action(); }
    });
  });
}
$("paletteInput")?.addEventListener("input", (e) => renderPaletteItems(e.target.value));
$("paletteInput")?.addEventListener("keydown", (e) => {
  if (e.key === "Escape") closePalette();
  if (e.key === "Enter") {
    const sel = document.querySelector(".item");
    if (sel) sel.click();
  }
});
$("cmdPalette")?.addEventListener("click", (e) => { if (e.target === $("cmdPalette")) closePalette(); });
document.addEventListener("keydown", (e) => {
  if ((e.metaKey || e.ctrlKey) && e.key === "k") { e.preventDefault(); openPalette(); }
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

// ── Resize handles ──
(function(){
  function makeResizable(handle,panel,isRight){
    var startX,startW;
    handle.addEventListener("mousedown",function(e){
      startX=e.clientX;startW=panel.getBoundingClientRect().width;
      document.body.style.cursor="col-resize";document.body.style.userSelect="none";
      handle.classList.add("active");
      function onMove(e){var delta=isRight?startX-e.clientX:e.clientX-startX;panel.style.width=Math.max(180,Math.min(480,startW+delta))+"px";}
      function onUp(){document.body.style.cursor="";document.body.style.userSelect="";handle.classList.remove("active");document.removeEventListener("mousemove",onMove);document.removeEventListener("mouseup",onUp);}
      document.addEventListener("mousemove",onMove);document.addEventListener("mouseup",onUp);
    });
  }
  var rl=$("resizeLeft"),rr=$("resizeRight"),sp=$("sidebarPanel"),ip=$("inspectorPanel");
  if(rl&&sp)makeResizable(rl,sp,false);
  if(rr&&ip)makeResizable(rr,ip,true);
})();


function wireChips() {
  document.querySelectorAll(".chip").forEach(function(btn) {
    btn.addEventListener("click", async function() {
      if (this.dataset.brief) {
        var p = await ensureProject();
        var res = await api("/api/lab/brief", { method: "POST", body: JSON.stringify({ project_id: p.slug, topic: this.dataset.brief }) });
        appendMsg("agent", "Brief 已写入 " + res.path);
        return;
      }
      if (this.dataset.prompt) streamChat(this.dataset.prompt).catch(function(e) { appendMsg("agent", e.message); });
    });
  });
}
wireChips();

// ── Init ──
(async function(){
  try{await refreshHealth();await loadProjects();renderThreadTabs();}catch(e){$("inspectorBody").innerHTML='<div class="ft-err">'+e.message+'</div>';}
  setTimeout(function(){var s=$("splash");if(s)s.classList.add("hide");},1200);
})();
