const $ = (id) => document.getElementById(id);
const API = "";
const FETCH_TIMEOUT_MS = 30000;
let mode = "proxy";
let busy = false;
let keys = {};
let evtSrc = null;
let sseBackoff = 2000;
let modalLogWhich = "proxy";
let panelStartedAt = Date.now();

const KEY_LABELS = {
  deepseek: "DEEPSEEK_API_KEY",
  qwen: "DASHSCOPE_API_KEY",
  moonshot: "MOONSHOT_API_KEY",
  zhipu: "ZHIPU_API_KEY",
};

function badge(state) {
  if (state === "green") return { text: "[OK]", cls: "ok" };
  if (state === "red") return { text: "[ERR]", cls: "err" };
  return { text: "[WARN]", cls: "warn" };
}

function fmtUptime(ms) {
  const s = Math.max(0, Math.floor(ms / 1000));
  const h = String(Math.floor(s / 3600)).padStart(2, "0");
  const m = String(Math.floor((s % 3600) / 60)).padStart(2, "0");
  const sec = String(s % 60).padStart(2, "0");
  return `${h}:${m}:${sec}`;
}

function setBadge(id, state) {
  const el = $(id);
  if (!el) return;
  const b = badge(state);
  el.textContent = b.text;
  el.className = "badge " + b.cls;
}

function setBusy(on) {
  busy = on;
  ["heroBtn", "stopBtn", "saveKeyBtn", "verifyKeyBtn"].forEach((id) => {
    const el = $(id);
    if (el) el.disabled = on;
  });
}

function setMsg(text, kind) {
  const el = $("msg");
  const prefix = kind === "ok" ? "[OK] " : kind === "err" ? "[ERR] " : "[LOG] ";
  el.textContent = prefix + text;
  el.className = "msg" + (kind ? " " + kind : "");
}

function setConn(live) {
  const el = $("connDot");
  el.textContent = live ? "[SSE:OK]" : "[SSE:DOWN]";
  el.className = "conn" + (live ? " live" : " dead");
  el.title = live ? "event stream connected" : "reconnecting…";
}

function updateTelemetry(extra = {}) {
  const provider = extra.provider || $("provider")?.value || "—";
  const proxyPort = extra.proxy_port ?? ($("proxyPort")?.value || "—");
  const sandboxPort = extra.sandbox_port ?? ($("sandboxPort")?.value || "—");
  const m = extra.mode || mode;
  $("telProvider").textContent = String(provider).toUpperCase();
  $("telProxy").textContent = ":" + proxyPort;
  $("telSandbox").textContent = ":" + sandboxPort;
  $("telMode").textContent = m === "official" ? "OFFICIAL" : "PROXY";
}

async function api(path, opts = {}) {
  const ctrl = new AbortController();
  const timer = setTimeout(() => ctrl.abort(), FETCH_TIMEOUT_MS);
  try {
    const res = await fetch(API + path, {
      headers: { "Content-Type": "application/json", ...(opts.headers || {}) },
      signal: ctrl.signal,
      ...opts,
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(data.error || res.statusText);
    return data;
  } catch (e) {
    if (e.name === "AbortError") throw new Error("request timeout");
    throw e;
  } finally {
    clearTimeout(timer);
  }
}

function applyStatus(st) {
  setBadge("bdProxy", st.proxy);
  setBadge("bdSandbox", st.sandbox);
  setBadge("bdUpstream", st.upstream);
  updateTelemetry({
    provider: st.provider,
    mode: st.mode,
    proxy_port: st.proxy_port,
    sandbox_port: st.sandbox_port,
  });
  const pct = Number(st.cache_session_hit_pct || 0);
  if (pct > 0 || Number(st.cache_hit_tokens) > 0) {
    $("cacheBar").hidden = false;
    $("cacheFill").style.width = Math.min(100, pct) + "%";
    $("cacheTxt").textContent =
      `CACHE ${pct}% | LAST ${st.cache_last_hit_pct || 0}% | ${st.cache_hit_tokens || 0} TOK`;
  }
  const url = st.url || "";
  const bar = $("urlBar");
  if (url) {
    bar.hidden = false;
    bar.href = url;
    bar.textContent = "> " + url;
  } else {
    bar.hidden = true;
  }
}

function applyMode(m) {
  mode = m;
  $("shell").classList.toggle("mode-official", m === "official");
  document.querySelectorAll(".seg").forEach((b) => {
    b.classList.toggle("active", b.dataset.mode === m);
  });
  $("heroBtn").textContent = m === "official" ? "OPEN_OFFICIAL" : "INIT_SEQUENCE";
  updateTelemetry({ mode: m });
}

async function loadConfig() {
  const cfg = await api("/api/config");
  $("provider").innerHTML = (cfg.providers || [])
    .map((p) => `<option value="${p.id}">${p.label}</option>`)
    .join("");
  $("provider").value = cfg.provider || "deepseek";
  $("proxyPort").value = cfg.proxy_port ?? 18991;
  $("sandboxPort").value = cfg.sandbox_port ?? 8990;
  $("cacheBoost").checked = cfg.cache_boost !== false;
  keys = cfg.keys || {};
  applyMode(cfg.mode === "official" ? "official" : "proxy");
  reflectProvider();
  updateTelemetry({
    provider: cfg.provider,
    proxy_port: cfg.proxy_port,
    sandbox_port: cfg.sandbox_port,
    mode: cfg.mode,
  });
}

function reflectProvider() {
  const p = $("provider").value;
  $("keyLabel").textContent = KEY_LABELS[p] || "API_KEY";
  const masked = keys[p] || "";
  $("keyInput").value = "";
  $("keyInput").placeholder = masked ? `stored: ${masked}` : "paste key — local only";
}

async function persistSettings() {
  const proxyPort = parseInt($("proxyPort").value, 10) || 18991;
  const sandboxPort = parseInt($("sandboxPort").value, 10) || 8990;
  if (proxyPort === sandboxPort) {
    throw new Error("proxy and sandbox ports must differ");
  }
  if (proxyPort === 8765 || sandboxPort === 8765) {
    throw new Error("port 8765 reserved for real Science instance");
  }
  await api("/api/config", {
    method: "PUT",
    body: JSON.stringify({
      provider: $("provider").value,
      proxy_port: proxyPort,
      sandbox_port: sandboxPort,
      cache_boost: $("cacheBoost").checked,
    }),
  });
  updateTelemetry({ proxy_port: proxyPort, sandbox_port: sandboxPort });
}

async function switchMode(m) {
  if (m === mode) return;
  setBusy(true);
  try {
    await api("/api/mode", { method: "PUT", body: JSON.stringify({ mode: m }) });
    applyMode(m);
    setMsg(m === "official" ? "switched to OFFICIAL mode" : "switched to PROXY mode", "ok");
  } catch (e) {
    setMsg(String(e.message || e), "err");
  } finally {
    setBusy(false);
  }
}

async function verifyKey() {
  setBusy(true);
  try {
    const v = await api("/api/verify", { method: "POST", body: "{}" });
    setMsg(v.ok ? v.hint : v.hint, v.ok ? "ok" : "err");
  } catch (e) {
    setMsg(String(e.message || e), "err");
  } finally {
    setBusy(false);
  }
}

async function saveKey() {
  const key = $("keyInput").value.trim();
  if (!key) {
    setMsg("API key required", "err");
    return;
  }
  setBusy(true);
  try {
    await persistSettings();
    const r = await api("/api/key", {
      method: "POST",
      body: JSON.stringify({ provider: $("provider").value, key }),
    });
    keys[$("provider").value] = r.masked;
    reflectProvider();
    await verifyKey();
  } catch (e) {
    setMsg(String(e.message || e), "err");
    setBusy(false);
  }
}

async function oneClick() {
  setBusy(true);
  setMsg("starting proxy + sandbox…");
  try {
    if ($("keyInput").value.trim()) {
      await persistSettings();
      const key = $("keyInput").value.trim();
      const r = await api("/api/key", {
        method: "POST",
        body: JSON.stringify({ provider: $("provider").value, key }),
      });
      keys[$("provider").value] = r.masked;
      reflectProvider();
    } else {
      await persistSettings();
    }
    const r = await api("/api/start", { method: "POST", body: "{}" });
    setMsg((r.msg || "runtime online") + (r.url ? `\n> ${r.url}` : ""), "ok");
    if (r.url) applyStatus({ proxy: "green", sandbox: "green", upstream: "green", url: r.url });
  } catch (e) {
    setMsg(String(e.message || e), "err");
  } finally {
    setBusy(false);
  }
}

async function heroClick() {
  if (mode === "official") {
    setBusy(true);
    try {
      await api("/api/official", { method: "POST", body: "{}" });
      setMsg("official Claude Science opened", "ok");
    } catch (e) {
      setMsg(String(e.message || e), "err");
    } finally {
      setBusy(false);
    }
  } else {
    await oneClick();
  }
}

async function stopAll() {
  setBusy(true);
  try {
    await api("/api/stop", { method: "POST", body: "{}" });
    setMsg("proxy + sandbox halted", "ok");
    applyStatus({ proxy: "amber", sandbox: "amber", upstream: "amber", url: "" });
  } catch (e) {
    setMsg(String(e.message || e), "err");
  } finally {
    setBusy(false);
  }
}

async function runDoctor() {
  setBusy(true);
  try {
    const r = await api("/api/doctor");
    const lines = (r.results || []).map((x) => {
      const lvl = x.level || x.Level || "";
      const msg = x.message || x.Message || "";
      const tag = lvl === "pass" ? "[OK]" : lvl === "fail" ? "[ERR]" : "[WARN]";
      return `${tag} ${msg}`;
    });
    showModal("DIAGNOSTIC", lines.join("\n") + `\n\nwarnings=${r.warnings} failures=${r.failures}`);
  } catch (e) {
    setMsg(String(e.message || e), "err");
  } finally {
    setBusy(false);
  }
}

async function fetchLogs(which) {
  const r = await api(`/api/logs?which=${which}`);
  return r.text || r.error || "(empty)";
}

async function showLogs() {
  setBusy(true);
  modalLogWhich = "proxy";
  try {
    $("modalTabs").hidden = false;
    document.querySelectorAll("#modalTabs .tab").forEach((b) => {
      b.classList.toggle("active", b.dataset.log === "proxy");
    });
    const text = await fetchLogs("proxy");
    showModal("LOGS", text);
  } catch (e) {
    setMsg(String(e.message || e), "err");
  } finally {
    setBusy(false);
  }
}

async function showResearch() {
  setBusy(true);
  try {
    const v = await api("/api/research?verify=1");
    const summary = [
      v.healthy ? "[OK] research pack ready" : "[ERR] assets incomplete",
      `runtime=${v.runtime_version || "—"}`,
      `db_clients=${v.bio_lib_packages || 0}`,
      `domains=${v.domains || 0} tools=${v.domain_tools || 0}`,
      `skills=${v.skills || 0}`,
      v.org_pack_seeded ? `org_pack workspaces=${v.workspaces}` : "org_pack=not_seeded",
    ].join("\n");
    const detail = await api("/api/research");
    showModal("RESEARCH_PACK", summary + "\n\n" + JSON.stringify(detail, null, 2).slice(0, 6000));
  } catch (e) {
    setMsg(String(e.message || e), "err");
  } finally {
    setBusy(false);
  }
}

function showModal(title, body) {
  $("modalTitle").textContent = title;
  $("modalBody").textContent = body;
  $("modal").showModal();
}

function connectSSE() {
  if (evtSrc) evtSrc.close();
  evtSrc = new EventSource("/api/events");
  evtSrc.onopen = () => {
    sseBackoff = 2000;
    setConn(true);
  };
  evtSrc.onmessage = (ev) => {
    try {
      applyStatus(JSON.parse(ev.data));
    } catch (_) {}
  };
  evtSrc.onerror = () => {
    setConn(false);
    evtSrc.close();
    setTimeout(connectSSE, sseBackoff);
    sseBackoff = Math.min(sseBackoff * 1.5, 30000);
  };
}

function tickUptime() {
  $("telUptime").textContent = fmtUptime(Date.now() - panelStartedAt);
}

async function init() {
  try {
    const [v, health] = await Promise.all([api("/api/version"), api("/api/health")]);
    $("verLabel").textContent = "v" + (v.version || "dev");
    window._releaseURL = v.release;
    window._issuesURL = v.issues;
    if (typeof health.uptime_ms === "number") {
      panelStartedAt = Date.now() - health.uptime_ms;
    }
    updateTelemetry({
      provider: health.provider,
      mode: health.mode,
      proxy_port: health.proxy_port,
      sandbox_port: health.sandbox_port,
    });
  } catch (_) {}
  await loadConfig();
  connectSSE();
  tickUptime();
  setInterval(tickUptime, 1000);
}

document.querySelectorAll(".seg").forEach((b) => {
  b.addEventListener("click", () => switchMode(b.dataset.mode));
});
$("provider").addEventListener("change", () => {
  reflectProvider();
  persistSettings().catch((e) => setMsg(e.message, "err"));
});
$("proxyPort").addEventListener("change", () => persistSettings().catch((e) => setMsg(e.message, "err")));
$("sandboxPort").addEventListener("change", () => persistSettings().catch((e) => setMsg(e.message, "err")));
$("cacheBoost").addEventListener("change", () => persistSettings().catch((e) => setMsg(e.message, "err")));
$("saveKeyBtn").addEventListener("click", saveKey);
$("verifyKeyBtn").addEventListener("click", verifyKey);
$("heroBtn").addEventListener("click", heroClick);
$("stopBtn").addEventListener("click", stopAll);
$("quitBtn").addEventListener("click", async () => {
  try { await api("/api/quit-proxy", { method: "POST", body: "{}" }); } catch (_) {}
  window.close();
});
$("openBrowserBtn").addEventListener("click", () =>
  api("/api/open-browser", { method: "POST", body: "{}" }).catch((e) => setMsg(e.message, "err"))
);
$("doctorBtn").addEventListener("click", runDoctor);
$("logsBtn").addEventListener("click", showLogs);
$("researchBtn").addEventListener("click", showResearch);
$("updateBtn").addEventListener("click", () => { if (window._releaseURL) window.open(window._releaseURL, "_blank"); });
$("reportBtn").addEventListener("click", () => { if (window._issuesURL) window.open(window._issuesURL, "_blank"); });
$("modalClose").addEventListener("click", () => $("modal").close());
document.querySelectorAll("#modalTabs .tab").forEach((btn) => {
  btn.addEventListener("click", async () => {
    modalLogWhich = btn.dataset.log;
    document.querySelectorAll("#modalTabs .tab").forEach((b) => {
      b.classList.toggle("active", b.dataset.log === modalLogWhich);
    });
    $("modalBody").textContent = "loading…";
    try {
      $("modalBody").textContent = await fetchLogs(modalLogWhich);
    } catch (e) {
      $("modalBody").textContent = String(e.message || e);
    }
  });
});

init().catch((e) => setMsg(String(e.message || e), "err"));