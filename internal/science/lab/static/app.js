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
  function makeResizable(handle, panel, isRight) {
    let startX, startW;
    handle.addEventListener("mousedown", (e) => {
      startX = e.clientX;
      startW = panel.getBoundingClientRect().width;
      document.body.style.cursor = "col-resize";
      document.body.style.userSelect = "none";
      function onMove(e) {
        const delta = isRight ? startX - e.clientX : e.clientX - startX;
        const w = Math.max(180, Math.min(480, startW + delta));
        panel.style.width = w + "px";
      }
      function onUp() {
        document.body.style.cursor = "";
        document.body.style.userSelect = "";
        document.removeEventListener("mousemove", onMove);
        document.removeEventListener("mouseup", onUp);
      }
      document.addEventListener("mousemove", onMove);
      document.addEventListener("mouseup", onUp);
    });
  }
  var rl = document.getElementById("resizeLeft");
  var rr = document.getElementById("resizeRight");
  var sp = document.getElementById("sidebarPanel");
  var ip = document.getElementById("inspectorPanel");
  if (rl && sp) makeResizable(rl, sp, false);
  if (rr && ip) makeResizable(rr, ip, true);
})();

// ── Dark mode ──
(function(){
  const saved = localStorage.getItem("lumen-lab-theme");
  if (saved === "dark" || (!saved && window.matchMedia("(prefers-color-scheme:dark)").matches)) {
    document.documentElement.classList.add("dark");
  }
  updateThemeIcon();
})();
function toggleDark(){ document.documentElement.classList.toggle("dark"); localStorage.setItem("lumen-lab-theme",document.documentElement.classList.contains("dark")?"dark":"light"); updateThemeIcon(); }
function updateThemeIcon(){ var b=$("themeToggle"); if(b)b.textContent=document.documentElement.classList.contains("dark")?"☀":"🌙"; var d=$("darkToggle"); if(d)d.textContent=document.documentElement.classList.contains("dark")?"关闭暗色":"开启暗色"; }
$("themeToggle")?.addEventListener("click",toggleDark);
$("darkToggle")?.addEventListener("click",toggleDark);

// ── Splash ──
setTimeout(function(){ var s=$("splash"); if(s)s.classList.add("hide"); },800);

// ── Conversation threads ──
var threads=[{id:"main",title:"对话"}],activeThread="main";
function switchThread(id){
  activeThread=id;
  document.querySelectorAll(".ctr-tab").forEach(function(t){t.classList.toggle("active",t.dataset.thread===id);});
  // Load thread messages (simplified for now)
}
function newThread(){
  var id="thread_"+Date.now();
  threads.push({id:id,title:"新对话"});
  renderThreadTabs();
  switchThread(id);
  activeProject=null;
  $("welcome")?$("welcome").style.display="":0;
  $("chatScroll").innerHTML='<div class="empty-state"><span class="icon">💬</span>开始新的对话</div>';
}
function closeThread(id,ev){ ev.stopPropagation(); threads=threads.filter(function(t){return t.id!==id;});
  if(threads.length===0){threads.push({id:"main",title:"对话"});}
  if(activeThread===id){activeThread=threads[0].id;}
  renderThreadTabs();
}
function renderThreadTabs(){
  var el=$("threadTabs"); if(!el)return;
  el.innerHTML=threads.map(function(t){return '<button class="ctr-tab'+(t.id===activeThread?" active":"")+'" data-thread="'+t.id+'" onclick="switchThread(\''+t.id+'\')"><span>'+escHtml(t.title)+'</span><span class="close" onclick="closeThread(\''+t.id+'\',event)">×</span></button>';}).join("");
  el.innerHTML+='<button class="ctr-tab-add" id="newThreadBtn" title="新建对话" onclick="newThread()">＋</button>';
}

// ── Simple markdown renderer ──
function renderMD(text){
  if(!text)return"";
  return text
    .replace(/&/g,"&amp;").replace(/</g,"&lt;").replace(/>/g,"&gt;")
    .replace(/^### (.+)$/gm,"<h3>$1</h3>")
    .replace(/^## (.+)$/gm,"<h2>$1</h2>")
    .replace(/^# (.+)$/gm,"<h1>$1</h1>")
    .replace(/\*\*(.+?)\*\*/g,"<strong>$1</strong>")
    .replace(/\*(.+?)\*/g,"<em>$1</em>")
    .replace(/`([^`]+)`/g,"<code>$1</code>")
    .replace(/```(\w*)\n([\s\S]*?)```/g,"<pre><code>$2</code></pre>")
    .replace(/\[([^\]]+)\]\(([^)]+)\)/g,"<a href=\"$2\" target=\"_blank\">$1</a>")
    .replace(/^- (.+)$/gm,"<li>$1</li>").replace(/(<li>.*<\/li>)/s,"<ul>$1</ul>")
    .replace(/^> (.+)$/gm,"<blockquote>$1</blockquote>")
    .replace(/^---$/gm,"<hr>")
    .replace(/\n\n/g,"</p><p>").replace(/^(.+)$/gm,"<p>$1</p>");
}

// Update appendMsg to use markdown
var _origAppendMsg=appendMsg;
appendMsg=function(cls,text){
  $("welcome")?.remove();
  var el=document.createElement("div");
  el.className="msg "+cls;
  if(cls==="agent")el.innerHTML=renderMD(text);else el.textContent=text;
  $("chatScroll").appendChild(el);
  el.scrollIntoView({behavior:"smooth",block:"end"});
};

// ── Fleet browser ──
async function refreshFleetBrowser(){
  var el=$("fleetList"),emp=$("fleetEmpty"); if(!el)return;
  try{
    var h=await api("/api/lab/health");
    var cs=h.fleet?.cs_domains||0,conn=h.fleet?.cs_connected||0,nat=h.fleet?.lumen_native||0;
    var skills=h.research_pack?.skills||0,tools=h.research_pack?.domain_tools||0;
    var html='<div class="set-card"><h3>🚀 舰队总览</h3>';
    html+='<div class="set-row"><span class="k">CS domains</span><span class="v g">'+conn+'/'+cs+'</span></div>';
    html+='<div class="set-row"><span class="k">原生 fleet</span><span class="v g">'+nat+'</span></div>';
    html+='<div class="set-row"><span class="k">Skills</span><span class="v">'+skills+'</span></div>';
    html+='<div class="set-row"><span class="k">Domain tools</span><span class="v">'+tools+'</span></div>';
    html+='</div>';
    html+='<div class="set-card"><h3>📡 原生舰队</h3>';
    ["pubmed","chembl","geo","oasis","c2d"].forEach(function(f){
      html+='<div class="flt-domain"><span class="flt-dot"></span><span class="flt-name">'+f+'</span></div>';
    });
    html+='</div>';
    el.innerHTML=html;
    if(emp)emp.style.display="none";
  }catch(e){}
}
$("refreshFleetBtn")?.addEventListener("click",refreshFleetBrowser);
$("c2dBtn")?.addEventListener("click",function(){ window.open("https://demo.oasisdata2026.xyz/c2d","_blank"); });

// Settings pane refresh
$("settingsBtn")?.addEventListener("click",function(){
  document.querySelectorAll(".insp-tab").forEach(function(t){t.classList.remove("active");});
  var st=document.querySelector('.insp-tab[data-pane="settings"]');
  if(st)st.classList.add("active");
  ["statusPane","filesPane","fleetPane","ketcherPane","moleculePane","settingsPane"].forEach(function(id){
    var p=$(id);if(p)p.hidden=id!=="settingsPane";
  });
  refreshFleetBrowser();
  refreshHealth().then(function(h){
    var info=$("researchInfo");if(!info)return;
    var p=h.research_pack||{};
    info.innerHTML='<div class="set-row"><span class="k">状态</span><span class="v'+(p.healthy?' g':'')+'">'+(p.healthy?'✓ 健康':'✗ 未安装')+'</span></div>'
      +'<div class="set-row"><span class="k">Bio clients</span><span class="v">'+(p.bio_clients||0)+'</span></div>'
      +'<div class="set-row"><span class="k">Domain tools</span><span class="v">'+(p.domain_tools||0)+'</span></div>'
      +'<div class="set-row"><span class="k">Skills</span><span class="v">'+(p.skills||0)+'</span></div>';
  });
});

// Fleet pane tab
document.querySelector('.insp-tab[data-pane="fleet"]')?.addEventListener("click",refreshFleetBrowser);

// ── Composer @menu ──
var composerCommands=[
  {label:"文献检索",action:function(){streamChat("用 pubmed 域检索最新文献")}},
  {label:"生成简报",action:function(){ensureProject().then(function(p){return api("/api/lab/brief",{method:"POST",body:JSON.stringify({project_id:p.slug,topic:"aspirin"})})}).then(function(r){appendMsg("agent","Brief ✓ "+r.path)})}},
  {label:"C2D 算法",action:function(){window.open("https://demo.oasisdata2026.xyz/c2d","_blank")}},
  {label:"绿洲数据集",action:function(){window.open("https://demo.oasisdata2026.xyz/datasets","_blank")}},
  {label:"打开 Bridge",action:function(){window.open("http://127.0.0.1:18990/","_blank")}},
];
var menuIdx=-1;
$("promptInput")?.addEventListener("input",function(e){
  var v=e.target.value,menu=$("composerMenu");
  if(!menu)return;
  if(v.startsWith("/")){
    var q=v.slice(1).toLowerCase();
    var items=composerCommands.filter(function(c){return c.label.toLowerCase().includes(q);});
    if(items.length){
      menu.innerHTML=items.map(function(c,i){return '<div class="item'+(i===menuIdx?" sel":"")+'" data-idx="'+i+'">'+c.label+'</div>';}).join("");
      menu.classList.add("show");menuIdx=-1;
      return;
    }
  }
  menu.classList.remove("show");menuIdx=-1;
});
$("promptInput")?.addEventListener("keydown",function(e){
  var menu=$("composerMenu");if(!menu||!menu.classList.contains("show"))return;
  var items=menu.querySelectorAll(".item");
  if(e.key==="ArrowDown"){e.preventDefault();menuIdx=Math.min(menuIdx+1,items.length-1);updateMenuSel(items);}
  else if(e.key==="ArrowUp"){e.preventDefault();menuIdx=Math.max(menuIdx-1,0);updateMenuSel(items);}
  else if(e.key==="Enter"&&menuIdx>=0){e.preventDefault();items[menuIdx].click();menu.classList.remove("show");}
  else if(e.key==="Escape"){menu.classList.remove("show");}
});
function updateMenuSel(items){items.forEach(function(el,i){el.classList.toggle("sel",i===menuIdx);});}
$("composerMenu")?.addEventListener("click",function(e){
  var idx=parseInt(e.target.dataset.idx);
  if(idx>=0&&composerCommands[idx]){composerCommands[idx].action();this.classList.remove("show");$("promptInput").value="";}
});

// ── File drag-drop ──
(function(){
  var overlay=$("dropOverlay"),fileTree=$("fileTree");
  if(!overlay)return;
  document.addEventListener("dragover",function(e){e.preventDefault();overlay.classList.add("show");});
  document.addEventListener("dragleave",function(e){if(e.target===document.documentElement)overlay.classList.remove("show");});
  document.addEventListener("drop",function(e){e.preventDefault();overlay.classList.remove("show");
    var files=e.dataTransfer.files;
    if(files.length&&activeProject){
      // Upload files via API
      for(var i=0;i<files.length;i++){
        appendMsg("agent","📎 "+files[i].name+" ("+fmtSize(files[i].size)+")");
      }
      setTimeout(refreshFiles,500);
    }
  });
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

// ── Init ──
(async function(){
  try{await refreshHealth();await loadProjects();renderThreadTabs();}catch(e){$("inspectorBody").innerHTML='<div class="ft-err">'+e.message+'</div>';}
  setTimeout(function(){var s=$("splash");if(s)s.classList.add("hide");},1200);
})();
