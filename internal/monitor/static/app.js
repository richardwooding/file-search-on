"use strict";

// Client-side history buffers for the three cache hit-rate sparklines.
const HISTORY = 60;
const hist = { attr: [], body: [], embed: [] };

function $(id) { return document.getElementById(id); }
function pct(x) { return (x * 100).toFixed(1) + "%"; }
function secs(x) { return x < 1 ? (x * 1000).toFixed(0) + "ms" : x.toFixed(2) + "s"; }

function dur(seconds) {
  const s = Math.floor(seconds);
  const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), sec = s % 60;
  if (h) return `${h}h ${m}m`;
  if (m) return `${m}m ${sec}s`;
  return `${sec}s`;
}

function bytes(n) {
  if (!n) return "—";
  const u = ["B", "KiB", "MiB", "GiB"];
  let i = 0, v = n;
  while (v >= 1024 && i < u.length - 1) { v /= 1024; i++; }
  return v.toFixed(i ? 1 : 0) + " " + u[i];
}

async function getJSON(path) {
  const r = await fetch(path, { cache: "no-store" });
  if (!r.ok) throw new Error(path + " " + r.status);
  return r.json();
}

// sparkline builds an inline SVG polyline from values in [0,1].
function sparkline(values, w = 200, h = 28) {
  if (!values.length) return "";
  const step = w / Math.max(values.length - 1, 1);
  const pts = values.map((v, i) => `${(i * step).toFixed(1)},${(h - v * h).toFixed(1)}`).join(" ");
  return `<svg class="spark" width="${w}" height="${h}" viewBox="0 0 ${w} ${h}" preserveAspectRatio="none">
    <polyline fill="none" stroke="var(--accent)" stroke-width="1.5" points="${pts}"/></svg>`;
}

function kv(k, v) { return `<div class="kv"><div class="k">${k}</div><div class="v">${v}</div></div>`; }

function indexLabel(o) {
  if (o.index_backend === "persistent" && o.index_path) {
    return `<span class="ix-persistent" title="${o.index_path}">🔒 persistent</span>`;
  }
  switch (o.index_fallback_reason) {
    case "lock_contention":
      return `<span class="ix-memory ix-warn" title="another file-search-on instance holds the writer lock">🧠 in-memory (lock contention)</span>`;
    case "no_index_flag":
      return `<span class="ix-memory" title="--no-index passed">🧠 in-memory (--no-index)</span>`;
    default:
      return `<span class="ix-memory">🧠 in-memory</span>`;
  }
}

function renderOverview(o) {
  $("hdr-version").textContent = o.version || "";
  $("hdr-mode").textContent = o.mode || "";
  $("hdr-uptime").textContent = "up " + dur(o.uptime_seconds || 0);
  $("overview").innerHTML = [
    kv("Mode", o.mode),
    kv("Uptime", dur(o.uptime_seconds || 0)),
    kv("Index", indexLabel(o)),
    kv("Index path", o.index_path || "—"),
    kv("Body cache cap", o.body_cache_cap ? bytes(o.body_cache_cap) : "in-memory"),
    kv("Workers (default)", o.num_cpu),
    kv("GOMAXPROCS", o.gomaxprocs),
    kv("Goroutines", o.goroutines),
    kv("Go", o.go_version),
    kv("PID", o.pid),
  ].join("");
}

function cacheCard(title, c, key) {
  hist[key].push(c.hit_rate || 0);
  if (hist[key].length > HISTORY) hist[key].shift();
  const warn = (n) => n ? '<span class="num warn">' + n + "</span>" : '<span class="num">0</span>';
  let rows = `
    <div class="lbl">hits</div><div class="num">${c.hits}</div>
    <div class="lbl">misses</div><div class="num">${c.misses}</div>
    <div class="lbl">puts</div><div class="num">${c.puts}</div>`;
  if ("stales" in c) rows += `<div class="lbl">stales</div><div class="num">${c.stales}</div>`;
  if ("evictions" in c) rows += `<div class="lbl">evictions</div>${warn(c.evictions)}`;
  if ("oversize" in c) rows += `<div class="lbl">oversize</div>${warn(c.oversize)}`;
  if ("model_mismatches" in c) rows += `<div class="lbl">model mismatch</div>${warn(c.model_mismatches)}`;
  if ("errors" in c) rows += `<div class="lbl">errors</div>${warn(c.errors)}`;
  return `<div class="cache-card">
    <div class="title"><span>${title}</span><span class="hr">${pct(c.hit_rate || 0)}</span></div>
    <div class="counters">${rows}</div>
    ${sparkline(hist[key])}
  </div>`;
}

function renderCache(d) {
  if (!d.available) { $("cache").innerHTML = '<div class="empty">No index attached.</div>'; return; }
  $("cache").innerHTML =
    cacheCard("Attributes", d.attr, "attr") +
    cacheCard("Body", d.body, "body") +
    cacheCard("Embeddings", d.embed, "embed");
}

function renderActivity(d) {
  if (!d.available) {
    $("inflight").textContent = "";
    $("activity-tools").innerHTML = `<div class="empty">${d.reason || "no activity"}</div>`;
    $("activity-recent").innerHTML = "";
    return;
  }
  const s = d.snapshot;
  $("inflight").textContent = `${s.in_flight} in-flight · ${s.total_calls} calls · ${s.total_errors} errors`;

  const tools = s.tools || [];
  if (!tools.length) {
    $("activity-tools").innerHTML = '<div class="empty">No tool calls yet.</div>';
  } else {
    let t = `<table><thead><tr><th>Tool</th><th class="num">calls</th><th class="num">err</th>
      <th class="num">cancel</th><th class="num">p50</th><th class="num">p95</th><th class="num">max</th></tr></thead><tbody>`;
    for (const x of tools) {
      t += `<tr><td>${x.tool}</td><td class="num">${x.count}</td><td class="num">${x.errors}</td>
        <td class="num">${x.cancels}</td><td class="num">${secs(x.p50_seconds)}</td>
        <td class="num">${secs(x.p95_seconds)}</td><td class="num">${secs(x.max_seconds)}</td></tr>`;
    }
    $("activity-tools").innerHTML = t + "</tbody></table>";
  }

  const recent = s.recent || [];
  if (!recent.length) { $("activity-recent").innerHTML = '<div class="empty">—</div>'; return; }
  $("activity-recent").innerHTML = recent.map((r) => {
    const when = new Date(r.at).toLocaleTimeString();
    const detail = r.outcome === "cancelled" && r.reason ? ` (${r.reason})` : "";
    const cnt = r.count ? ` · ${r.count} results` : "";
    return `<div class="row"><span class="when">${when}</span><span class="tool">${r.tool}</span>
      <span class="out-${r.outcome}">${r.outcome}${detail}</span><span>${secs(r.seconds)}${cnt}</span></div>`;
  }).join("");
}

function renderCapabilities(d) {
  const ct = d.content_types || { total: 0, families: [] };
  const fams = (ct.families || []).map((f) =>
    `<span class="chip" title="${(f.types || []).join(", ")}">${f.family} · ${f.count}</span>`).join("");
  const projects = (d.project_types || []).map((p) =>
    `<span class="chip" title="${p.description || ""}">${p.name}</span>`).join("");
  const ocr = d.ocr || {};
  const ocrLine = ocr.available
    ? `<span class="pill pill-ok">${ocr.active_provider}</span>`
    : `<span class="pill pill-unknown">none (registered: ${(ocr.registered || []).join(", ") || "—"})</span>`;
  const emb = d.embedder || {};
  const embLine = emb.reachable
    ? `<span class="pill pill-ok">reachable</span>`
    : `<span class="pill pill-unknown">unreachable</span>`;

  $("capabilities").innerHTML = `<div class="caps-grid">
    <div><h3 class="sub">Content types (${ct.total})</h3>${fams || '<div class="empty">—</div>'}</div>
    <div><h3 class="sub">Project types (${(d.project_types || []).length})</h3>${projects || '<div class="empty">—</div>'}</div>
    <div><h3 class="sub">OCR</h3>${ocrLine}</div>
    <div><h3 class="sub">Embedder</h3>
      <div>${embLine} <span class="muted">${emb.model || "no model"} @ ${emb.server || "—"}</span></div>
    </div>
  </div>`;
}

function setHealth(ok) {
  const el = $("health");
  el.textContent = ok ? "healthy" : "unreachable";
  el.className = "pill " + (ok ? "pill-ok" : "pill-err");
}

// shortDir trims a working dir to its last two path segments for compact
// display (e.g. /Users/me/Code/projA → …/Code/projA).
function shortDir(d) {
  if (!d) return "";
  const parts = d.split("/").filter(Boolean);
  return (parts.length > 2 ? "…/" : "/") + parts.slice(-2).join("/");
}

function peerBadge(p) {
  if (p.index_backend === "persistent") return "🔒 ";
  if (p.index_backend === "in-memory") return "🧠 ";
  return ""; // pre-#242 peer or unknown — no badge rather than wrong badge
}

function renderPeers(d) {
  const peers = (d && d.peers) || [];
  const el = $("peers");
  // A lone instance (just us) doesn't need a switcher.
  if (peers.length <= 1) { el.innerHTML = ""; return; }
  const opts = peers.map((p) => {
    const port = (p.url.match(/:(\d+)\//) || [])[1] || "?";
    const label = `${peerBadge(p)}${p.mode} ${shortDir(p.working_dir)} :${port}${p.is_self ? " (you)" : ""}`;
    return `<option value="${p.url}" ${p.is_self ? "selected" : ""}>${label}</option>`;
  }).join("");
  el.innerHTML = `<label class="peers-label">${peers.length} instances</label>
    <select id="peer-select">${opts}</select>`;
  const sel = $("peer-select");
  sel.onchange = () => {
    const url = sel.value;
    if (url && !peers.find((p) => p.url === url && p.is_self)) {
      window.open(url, "_blank");
      // reset selection back to self so the dropdown keeps showing "you"
      const self = peers.find((p) => p.is_self);
      if (self) sel.value = self.url;
    }
  };
}

async function tick() {
  try {
    const [o, c, a, caps, peers] = await Promise.all([
      getJSON("api/overview"), getJSON("api/cache"),
      getJSON("api/activity"), getJSON("api/capabilities"),
      getJSON("api/peers"),
    ]);
    renderOverview(o);
    renderCache(c);
    renderActivity(a);
    renderCapabilities(caps);
    renderPeers(peers);
    updateCacheBrowserCounts(c);
    setHealth(true);
  } catch (e) {
    setHealth(false);
  }
}

// --- Cache browser ---
//
// The browser does NOT poll on the 2s tick — refreshes only fire when
// the user changes the filter, switches tab, or paginates. It re-uses
// the live counts from /api/cache (which DOES poll) to keep the tab
// labels honest.

const cb = {
  bucket: "attrs",
  q: "",
  offset: 0,
  limit: 50,
  total: 0,
  attrCount: 0,
  bodyCount: 0,
};

function updateCacheBrowserCounts(cacheJSON) {
  // The bucket entry counts ride on /api/cache via Stats. cacheJSON shape:
  // { attr: { ... }, body: { ... }, attr_entries_count, body_entries_count }
  cb.attrCount = cacheJSON.attr_entries_count || 0;
  cb.bodyCount = cacheJSON.body_entries_count || 0;
  // Reflect in tab labels.
  document.getElementById("cb-tab-attrs").textContent = `Attributes (${cb.attrCount})`;
  document.getElementById("cb-tab-bodies").textContent = `Bodies (${cb.bodyCount})`;
}

async function refreshCacheBrowser() {
  const url = `api/cache/entries?bucket=${cb.bucket}&q=${encodeURIComponent(cb.q)}&limit=${cb.limit}&offset=${cb.offset}`;
  let resp;
  try {
    resp = await getJSON(url);
  } catch (e) {
    document.getElementById("cb-table").innerHTML = `<div class="empty">error: ${e.message}</div>`;
    return;
  }
  cb.total = resp.total || 0;
  renderCacheBrowserTable(resp.entries || []);
  updateCacheBrowserPager();
}

function renderCacheBrowserTable(entries) {
  if (!entries.length) {
    document.getElementById("cb-table").innerHTML = `<div class="empty">no entries</div>`;
    return;
  }
  const isAttrs = cb.bucket === "attrs";
  const rows = entries.map((e) => {
    const staleBadge = e.stale ? `<span class="cb-stale">stale</span>` : "";
    const mt = e.mod_time ? new Date(e.mod_time).toLocaleString() : "";
    const cell2 = isAttrs ? (e.content_type || "—") : bytes(e.size);
    const cell3 = isAttrs ? bytes(e.size) : (e.last_access ? new Date(e.last_access).toLocaleString() : "—");
    return `<tr class="cb-row" data-path="${escapeAttr(e.path)}">
      <td class="cb-path">${escapeHTML(e.path)}${staleBadge}</td>
      <td>${escapeHTML(cell2)}</td>
      <td>${escapeHTML(cell3)}</td>
      <td>${mt}</td>
    </tr>`;
  }).join("");
  const header = isAttrs
    ? `<th>path</th><th>content_type</th><th>size</th><th>mod_time</th>`
    : `<th>path</th><th>size</th><th>last_access</th><th>mod_time</th>`;
  document.getElementById("cb-table").innerHTML = `<table class="cb-table">
    <thead><tr>${header}</tr></thead>
    <tbody>${rows}</tbody>
  </table>`;
  // Wire row clicks → detail modal.
  document.querySelectorAll("#cb-table tr.cb-row").forEach((tr) => {
    tr.addEventListener("click", () => openDetail(tr.dataset.path));
  });
}

function updateCacheBrowserPager() {
  const pageStart = cb.total === 0 ? 0 : cb.offset + 1;
  const pageEnd = Math.min(cb.offset + cb.limit, cb.total);
  document.getElementById("cb-pageinfo").textContent = `${pageStart}–${pageEnd} of ${cb.total}`;
  document.getElementById("cb-prev").disabled = cb.offset === 0;
  document.getElementById("cb-next").disabled = pageEnd >= cb.total;
}

async function openDetail(path) {
  const url = `api/cache/entry?bucket=${cb.bucket}&path=${encodeURIComponent(path)}`;
  let resp;
  try {
    resp = await getJSON(url);
  } catch (e) {
    alert(`could not load detail: ${e.message}`);
    return;
  }
  renderDetail(resp);
  document.getElementById("cb-modal").hidden = false;
}

function renderDetail(d) {
  const isAttrs = cb.bucket === "attrs";
  const rows = [];
  const add = (k, v) => rows.push(`<div class="k">${escapeHTML(k)}</div><div class="v">${escapeHTML(String(v))}</div>`);
  add("path", d.path);
  if (isAttrs) {
    add("content_type", d.content_type || "—");
    add("size", bytes(d.size));
    add("mod_time", d.mod_time ? new Date(d.mod_time).toLocaleString() : "—");
    add("stale", d.stale ? "yes" : "no");
    if (d.hash) add("sha256", d.hash);
    if (d.md5) add("md5", d.md5);
    if (d.sha1) add("sha1", d.sha1);
    if (d.embed_model) add("embed_model", d.embed_model);
    if (d.has_vector) add("vector_dims", d.vector_dims);
    // Flatten Extra as additional rows.
    if (d.extra) {
      Object.keys(d.extra).sort().forEach((k) => add(k, formatExtra(d.extra[k])));
    }
  } else {
    add("size", bytes(d.size));
    add("mod_time", d.mod_time ? new Date(d.mod_time).toLocaleString() : "—");
    add("created_at", d.created_at ? new Date(d.created_at).toLocaleString() : "—");
    add("stale", d.stale ? "yes" : "no");
    add("body_length", bytes(d.body_length));
    if (d.truncated) add("truncated", "yes (preview cut at 64 KiB)");
  }
  let body = "";
  if (!isAttrs && d.body) {
    body = `<div class="cb-body-block">${escapeHTML(d.body)}</div>`;
  }
  document.getElementById("cb-modal-body").innerHTML = `<h3 style="margin-top:0">Cache entry — ${cb.bucket}</h3>
    <div class="cb-detail-grid">${rows.join("")}</div>${body}`;
}

function formatExtra(v) {
  if (v == null) return "—";
  if (typeof v === "object") return JSON.stringify(v);
  return String(v);
}

function escapeHTML(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}

function escapeAttr(s) {
  return escapeHTML(s);
}

// Wire up controls once at startup.
let cbDebounce = null;
function wireCacheBrowser() {
  document.querySelectorAll(".cb-tab").forEach((b) => {
    b.addEventListener("click", () => {
      document.querySelectorAll(".cb-tab").forEach((x) => x.classList.remove("cb-tab-active"));
      b.classList.add("cb-tab-active");
      cb.bucket = b.dataset.bucket;
      cb.offset = 0;
      refreshCacheBrowser();
    });
  });
  document.getElementById("cb-filter").addEventListener("input", (e) => {
    clearTimeout(cbDebounce);
    cbDebounce = setTimeout(() => {
      cb.q = e.target.value;
      cb.offset = 0;
      refreshCacheBrowser();
    }, 300);
  });
  document.getElementById("cb-prev").addEventListener("click", () => {
    cb.offset = Math.max(0, cb.offset - cb.limit);
    refreshCacheBrowser();
  });
  document.getElementById("cb-next").addEventListener("click", () => {
    cb.offset += cb.limit;
    refreshCacheBrowser();
  });
  document.getElementById("cb-modal-close").addEventListener("click", () => {
    document.getElementById("cb-modal").hidden = true;
  });
  document.getElementById("cb-modal").addEventListener("click", (e) => {
    // Dismiss when clicking the dim overlay (not the inner content).
    if (e.target === document.getElementById("cb-modal")) {
      document.getElementById("cb-modal").hidden = true;
    }
  });
  refreshCacheBrowser();
}

wireCacheBrowser();

tick();
setInterval(tick, 2000);
