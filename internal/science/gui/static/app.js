const $ = (id) => document.getElementById(id);
const API = "";
const FETCH_TIMEOUT_MS = 30000;
let mode = "proxy";
let busy = false;
let keys = {};
let evtSrc = null;
let sseBackoff = 2000;
let modalLogWhich = "proxy";

const KEY_LABELS = {
  deepseek: "DeepSeek API Key",
  qwen: "DashScope API Key",
  moonshot: "Moonshot API Key",
  zhipu: "智谱 API Key",
};

function setBusy(on) {
  busy = on;
  ["heroBtn", "stopBtn", "saveKeyBtn", "verifyKeyBtn"].forEach((id) => {
    const el = $(id);
    if (el) el.disabled = on;
  });
}

function setMsg(text, kind) {
  const el = $("msg");
  el.textContent = text;
  el.className = "msg" + (kind ? " " + kind : "");
}

function setConn(live) {
  const el = $("connDot");
  el.className = "conn" + (live ? " live" : " dead");
  el.title = live ? "实时连接正常" : "连接断开，自动重连中…";
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
    if (e.name === "AbortError") throw new Error("请求超时，请稍后重试");
    throw e;
  } finally {
    clearTimeout(timer);
  }
}

function setLight(el, state) {
  el.className = "lt " + ({ green: "g", amber: "a", red: "r" }[state] || "a");
}

function applyStatus(st) {
  setLight($("ltProxy"), st.proxy);
  setLight($("ltSandbox"), st.sandbox);
  setLight($("ltUpstream"), st.upstream);
  $("brandDot").className = "pulse" + (st.proxy === "green" ? " green" : "");
  const pct = Number(st.cache_session_hit_pct || 0);
  if (pct > 0 || Number(st.cache_hit_tokens) > 0) {
    $("cacheBar").hidden = false;
    $("cacheFill").style.width = Math.min(100, pct) + "%";
    $("cacheTxt").textContent = `缓存 ${pct}% · 上次 ${st.cache_last_hit_pct || 0}% · ${st.cache_hit_tokens || 0} tok`;
  }
  const url = st.url || "";
  const bar = $("urlBar");
  if (url) {
    bar.hidden = false;
    bar.href = url;
    bar.textContent = url;
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
  $("heroBtn").textContent = m === "official" ? "打开官方 Claude Science ↗" : "一键开始";
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
}

function reflectProvider() {
  const p = $("provider").value;
  $("keyLabel").textContent = KEY_LABELS[p] || "API Key";
  const masked = keys[p] || "";
  $("keyInput").value = "";
  $("keyInput").placeholder = masked ? `已存：${masked}` : "粘贴 key（仅存本地）";
}

async function persistSettings() {
  const proxyPort = parseInt($("proxyPort").value, 10) || 18991;
  const sandboxPort = parseInt($("sandboxPort").value, 10) || 8990;
  if (proxyPort === sandboxPort) {
    throw new Error("代理与沙箱端口不能相同");
  }
  if (proxyPort === 8765 || sandboxPort === 8765) {
    throw new Error("端口 8765 保留给真实 Science 实例");
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
}

async function switchMode(m) {
  if (m === mode) return;
  setBusy(true);
  try {
    await api("/api/mode", { method: "PUT", body: JSON.stringify({ mode: m }) });
    applyMode(m);
    setMsg(m === "official" ? "已切到官方模式" : "已切到第三方模式");
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
    setMsg(v.ok ? `✓ ${v.hint}` : `✗ ${v.hint}`, v.ok ? "ok" : "err");
  } catch (e) {
    setMsg(String(e.message || e), "err");
  } finally {
    setBusy(false);
  }
}

async function saveKey() {
  const key = $("keyInput").value.trim();
  if (!key) {
    setMsg("请输入 API key", "err");
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
  setMsg("正在启动代理与沙箱…");
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
    setMsg((r.msg || "已启动") + (r.url ? `\n${r.url}` : ""), "ok");
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
      setMsg("已打开官方 Claude Science", "ok");
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
    setMsg("已停止沙箱与代理", "ok");
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
      const icon = lvl === "pass" ? "✓" : lvl === "fail" ? "✗" : "⚠";
      return `${icon} ${msg}`;
    });
    showModal("自检", lines.join("\n") + `\n\nwarnings ${r.warnings}, failures ${r.failures}`);
  } catch (e) {
    setMsg(String(e.message || e), "err");
  } finally {
    setBusy(false);
  }
}

async function fetchLogs(which) {
  const r = await api(`/api/logs?which=${which}`);
  return r.text || r.error || "(空)";
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
    showModal("运行日志", text);
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
      v.healthy ? "✓ 资源包就绪" : "✗ 资源不完整",
      `runtime ${v.runtime_version || "—"}`,
      `db clients ${v.bio_lib_packages || 0}`,
      `domains ${v.domains || 0} · tools ${v.domain_tools || 0}`,
      `skills ${v.skills || 0}`,
      v.org_pack_seeded ? `org pack (${v.workspaces} workspaces)` : "org pack 未播种",
    ].join("\n");
    const detail = await api("/api/research");
    showModal("科研资源", summary + "\n\n" + JSON.stringify(detail, null, 2).slice(0, 6000));
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

async function init() {
  try {
    const v = await api("/api/version");
    $("verLabel").textContent = "v" + (v.version || "dev");
    window._releaseURL = v.release;
    window._issuesURL = v.issues;
  } catch (_) {}
  await loadConfig();
  connectSSE();
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
    $("modalBody").textContent = "加载中…";
    try {
      $("modalBody").textContent = await fetchLogs(modalLogWhich);
    } catch (e) {
      $("modalBody").textContent = String(e.message || e);
    }
  });
});

init().catch((e) => setMsg(String(e.message || e), "err"));