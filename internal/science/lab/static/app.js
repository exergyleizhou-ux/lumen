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
  var el = document.createElement("div");
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

var currentMsgEl=null,currentToolEl=null,thinkingBlock=null;
function thinkBlock(){var e=document.createElement("div");e.className="chat-thinking";e.innerHTML='<div class="chat-thinking-hd" onclick="this.parentElement.classList.toggle(\'open\')"><span class="chat-thinking-dot"></span>思考中…</div><div class="chat-thinking-body"></div>';return e;}
function toolCard(name,args){var e=document.createElement("div");e.className="chat-tool open";var a=typeof args==="string"?args:JSON.stringify(args||{}).slice(0,200);e.innerHTML='<div class="chat-tool-hd" onclick="this.parentElement.classList.toggle(\'open\')"><span class="chat-tool-icon">🔧</span><span class="chat-tool-name">'+escHtml(name)+'</span><span class="chat-tool-status">执行中…</span></div><div class="chat-tool-body"><pre>'+escHtml(a)+'</pre><div class="chat-tool-output"></div></div>';return e;}
function updTool(el,t,d){var s=el.querySelector(".chat-tool-status");if(s)s.textContent=d?"✓ 完成":t||"执行中…";if(d)el.classList.add("done");}
function msgEl(cls){var e=document.createElement("div");e.className="chat-msg "+cls;return e;}

async function streamChat(prompt,mode){
  mode=mode||"plan";var p=await ensureProject();
  var w=$("welcome");if(w)w.remove();
  var ue=msgEl("user");ue.textContent=prompt;$("chatScroll").appendChild(ue);
  var ae=msgEl("agent");$("chatScroll").appendChild(ae);
  currentMsgEl=ae;currentToolEl=null;thinkingBlock=null;textBuf="";
  try{
    var res=await fetch("/api/lab/chat",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({project_id:p.slug,prompt:prompt,mode:mode,session_id:activeThread})});
    var reader=res.body.getReader();var dec=new TextDecoder();var leftover="";
    while(true){
      var r=await reader.read();if(r.done)break;
      leftover+=dec.decode(r.value,{stream:true});
      // Split on double newline (SSE boundary)
      var chunks=leftover.split("\n\n");leftover=chunks.pop()||"";
      for(var ci=0;ci<chunks.length;ci++){
        var chunk=chunks[ci].trim();if(!chunk)continue;
        // Extract JSON from "data: {...}" line
        var json=chunk;
        if(json.indexOf("data:")===0)json=json.slice(5).trim();
        if(!json.startsWith("{"))continue;
        try{
          var ev=JSON.parse(json);var k=ev.kind||"";
          if(k==="phase"){
            var ph=document.createElement("div");ph.className="chat-phase";ph.textContent=(ev.text||"")+" · 执行中";ae.appendChild(ph);setTimeout(function(){ph.style.opacity="0";setTimeout(function(){ph.remove();},300);},3000);
          }else if(k==="text"){
            if(!thinkingBlock){thinkingBlock=thinkBlock();ae.appendChild(thinkingBlock);}
            var bd=thinkingBlock.querySelector(".chat-thinking-body");
            textBuf+=ev.text||"";bd.textContent=textBuf;bd.scrollTop=bd.scrollHeight;
          }else if(k==="tool"){
            if(thinkingBlock){thinkingBlock.classList.remove("open");thinkingBlock=null;textBuf="";}
            if(currentToolEl)updTool(currentToolEl,"",true);
            currentToolEl=toolCard(ev.tool?.name||ev.name||"tool",ev.tool?.input||ev.input||{});ae.appendChild(currentToolEl);
          }else if(k==="tool_result"||k==="tool_output"){
            if(currentToolEl){var out=currentToolEl.querySelector(".chat-tool-output");out.textContent=(ev.text||"").slice(0,2000);updTool(currentToolEl,"",true);}
          }else if(k==="error"){
            var errEl=document.createElement("div");errEl.style.color="#b42318";errEl.style.fontSize="13px";errEl.textContent="❌ "+(ev.text||"err");ae.appendChild(errEl);
          }else if(k==="turn_done"){
            if(currentToolEl)updTool(currentToolEl,"",true);
            if(thinkingBlock){thinkingBlock.classList.remove("open");thinkingBlock=null;textBuf="";}
          }
        }catch(_){}
      }
    }
  }catch(e){var errEl=document.createElement("div");errEl.style.color="#b42318";errEl.textContent="错误: "+e.message;ae.appendChild(errEl);}
  currentMsgEl=null;currentToolEl=null;thinkingBlock=null;textBuf="";refreshFiles();
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
    var totalMcp=0,tools={},models={};
    data.records.forEach(function(r){
      if(r.kind==="mcp_call"||(r.mcp_calls||[]).length>0)totalMcp++;
      (r.mcp_calls||[]).forEach(function(m){tools[m.tool]=1;});
      if(r.model)models[r.model]=1;
    });
    var summary='<div class="pv-summary">📋 '+data.records.length+' 记录 · 🔗 '+totalMcp+' MCP调用 · 🧠 '+Object.keys(models).join(", ")+' · 🔧 '+Object.keys(tools).slice(0,5).join(", ")+(Object.keys(tools).length>5?"…":"")+'</div>';
    var rows=data.records.slice(0,10).map(function(r){
      var mcp=(r.mcp_calls||[]).map(function(m){return m.tool+'("'+(m.query||'').slice(0,30)+'")';}).join(', ');
      return '<div class="pv-row"><span class="pv ts">'+(r.ts||'').slice(0,19).replace('T',' ')+'</span><span class="pv tag">'+(r.kind||'artifact')+'</span><span class="pv model">'+(r.model||'—')+'</span>'+(mcp?'<span class="pv mcp">🔗 '+mcp+'</span>':'')+(r.content_hash?'<span class="pv hash">#'+r.content_hash.slice(7,15)+'</span>':'')+'</div>';
    }).join('');
    return summary+rows+(data.records.length>10?'<div class="pv-empty">… 还有 '+(data.records.length-10)+' 条</div>':'');
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


function wireChips() {
  document.querySelectorAll(".chip").forEach(function(btn) {
    btn.addEventListener("click", async function() {
      if (this.dataset.brief) {
        var p = await ensureProject();
        var res = await api("/api/lab/brief", { method: "POST", body: JSON.stringify({ project_id: p.slug, topic: this.dataset.brief }) });
        appendMsg("agent", "Brief 已写入 " + res.path);
        setTimeout(refreshFiles, 1500);
        return;
      }
      if (this.dataset.prompt) streamChat(this.dataset.prompt).catch(function(e) { appendMsg("agent", e.message); });
    });
  });
}

// ── Init ──
(async function(){
  try{await refreshHealth();await loadProjects();renderThreadTabs();}catch(e){$("inspectorBody").innerHTML='<div class="ft-err">'+e.message+'</div>';}
  setTimeout(function(){var s=$("splash");if(s)s.classList.add("hide");},1200);
})();

// ── Syntax highlighting (simple) ──
function highlightCode(code, lang){
  if (!code) return "";
  var h = escHtml(code);
  if (lang === "python" || lang === "py" || lang === "go" || lang === "js" || lang === "ts" || lang === "rust" || lang === "sh" || lang === "bash" || !lang){
    h = h.replace(/\b(import|from|def|class|return|if|else|elif|for|while|try|except|finally|with|as|yield|raise|pass|break|continue|and|or|not|in|is|None|True|False|func|var|let|const|function|async|await|export|default|package|type|struct|interface|map|chan|go|defer|select|switch|case|range|fn|impl|use|mod|pub|mut|self|print|echo|exit|return)\b/g,'<span class="kw">$1</span>');
    h = h.replace(/(["'`])(?:(?!\1).)*\1/g,'<span class="str">$&</span>');
    h = h.replace(/(\d+\.?\d*)/g,'<span class="num">$1</span>');
    h = h.replace(/(#|\/\/).*$/gm,'<span class="cm">$&</span>');
    h = h.replace(/\b([a-zA-Z_]\w*)\s*\(/g,'<span class="fn">$1</span>(');
  }
  return h;
}

// Override the MD code block rendering
var _origRenderMD = renderMD;
renderMD = function(text){
  if (!text) return "";
  var html = _origRenderMD(text);
  // Add syntax highlight to code blocks
  html = html.replace(/<pre><code>([\s\S]*?)<\/code><\/pre>/g, function(m, code){
    var lang = "auto";
    var codeText = code.replace(/^\n+/, "");
    return '<pre><code>'+highlightCode(codeText, lang)+'</code></pre>';
  });
  return html;
};

// ── Diff renderer ──
function renderDiff(diffText){
  if (!diffText) return "";
  var lines = diffText.split("\n");
  var html = '<div class="diff-block"><div class="diff-hd">📄 文件变更</div><div class="diff-body">';
  for (var i = 0; i < lines.length; i++){
    var line = lines[i];
    var cls = "";
    if (line.startsWith("+")) cls = "add";
    else if (line.startsWith("-")) cls = "del";
    else if (line.startsWith("@@")) cls = "hdr";
    html += '<div class="diff-line '+cls+'">'+escHtml(line)+'</div>';
  }
  html += '</div></div>';
  return html;
}

// ── Git status ──
async function checkGitStatus(){
  try {
    var r = await api("/api/lab/files?project_id="+activeProject.slug);
    var files = r.files||[];
    var dirty = files.length > 0;
    var badge = document.querySelector(".git-badge");
    if (badge){ badge.textContent = dirty ? "● "+files.length+" changed" : "✓ clean"; badge.className = "git-badge"+(dirty?" dirty":""); }
  } catch(_){}
}

// Add diff rendering to chat tool output
var _origHandleSSE_streamChat_vars = null;
// Hook into streamChat to render diffs
(function(){
  var orig = streamChat;
  // Watch for diff in tool output
  setInterval(function(){
    document.querySelectorAll(".chat-tool-output").forEach(function(el){
      var text = el.textContent||"";
      if (text.startsWith("diff ") || text.includes("@@ -") || text.includes("+++ ")){
        el.innerHTML = renderDiff(text);
      }
    });
  }, 500);
})();
