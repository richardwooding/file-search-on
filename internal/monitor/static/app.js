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

function renderOverview(o) {
  $("hdr-version").textContent = o.version || "";
  $("hdr-mode").textContent = o.mode || "";
  $("hdr-uptime").textContent = "up " + dur(o.uptime_seconds || 0);
  $("overview").innerHTML = [
    kv("Mode", o.mode),
    kv("Uptime", dur(o.uptime_seconds || 0)),
    kv("Index", o.index_backing),
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

async function tick() {
  try {
    const [o, c, a, caps] = await Promise.all([
      getJSON("api/overview"), getJSON("api/cache"),
      getJSON("api/activity"), getJSON("api/capabilities"),
    ]);
    renderOverview(o);
    renderCache(c);
    renderActivity(a);
    renderCapabilities(caps);
    setHealth(true);
  } catch (e) {
    setHealth(false);
  }
}

tick();
setInterval(tick, 2000);
