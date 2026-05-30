# Monitoring dashboard

The long-running modes — the `mcp` server and the `watch` command — serve a small **read-only** monitoring dashboard so you can watch file-search-on's internal state while it runs: is the cache working, what is the agent doing right now, is OCR / embedding available.

Since v0.65.0 it's **on by default** on a dynamic OS-assigned localhost port — many concurrent stdio agents each get their own dashboard without colliding. The server binds **127.0.0.1 only** (the host part of any address is ignored — only the port matters), has **no auth**, and adds **no dependencies** (the UI is a single embedded page polling a JSON API).

```sh
# stdio MCP server — dashboard auto-starts on a dynamic port
file-search-on mcp

# pin a fixed port instead of a dynamic one
file-search-on mcp --monitor-addr :9090

# opt out for hermetic / sandboxed runs
file-search-on mcp --no-monitor

# HTTP MCP server + dashboard (auto-starts, same as stdio)
file-search-on mcp --transport http --addr :8080

# watch mode + dashboard
file-search-on watch 'is_image && body.contains("error")' --ocr -d ~/Desktop
```

The chosen URL is printed to stderr (`monitor dashboard: http://127.0.0.1:<port>/`) — or, for an `mcp` server, ask it via the `monitor_info` tool. For a one-shot view of every active dashboard across concurrent instances, `file-search-on monitors` lists the live registry. The legacy `--monitor` bool is kept as a documented no-op for back-compat (same effect as no flag).

## Panels

| Panel | Shows |
| --- | --- |
| **Overview** | version, uptime, run mode (`mcp-stdio` / `mcp-http` / `watch`), PID, Go version, GOMAXPROCS, default worker count, **index backend** (🔒 persistent path / 🧠 in-memory with reason — `--no-index` opt-out or `lock_contention` fallback), body-cache cap, goroutines |
| **Cache** | attribute / body / embedding cache counters with derived **hit-rate %** + sparklines; body evictions / oversize rejects and embed model-mismatches highlighted |
| **Activity** | live MCP tool-call feed (tool, elapsed, outcome, result count), per-tool call/error/cancel counts + p50/p95/max latency, in-flight gauge |
| **Capabilities** | registered content types grouped by family, project types, OCR provider availability, embedder model/server + reachability |
| **Peer switcher** | header dropdown listing every other running instance's dashboard (mode · working dir · port); switch with one click. Hidden when only one instance is running |

In **watch mode** there are no MCP tool calls, so the Activity panel shows a notice; Overview / Cache / Capabilities still populate.

## Multiple concurrent instances

Running many `file-search-on mcp` agents at once? `--monitor` gives each its own dynamic port, and every dashboard discovers the others through a shared registry under the user cache dir (`<UserCacheDir>/file-search-on/monitors/`). Crashed instances self-prune on the next read; clean shutdowns deregister immediately.

The **`monitor_info`** MCP tool is the per-agent entry point:

```jsonc
// returns this server's dashboard URL + every sibling instance
{ "name": "monitor_info" }

// start this server's dashboard on a dynamic port if it wasn't
// launched with a monitor flag (idempotent — same URL on repeat calls)
{ "name": "monitor_info", "arguments": { "enable": true } }
```

Response shape:

```json
{
  "enabled": true,
  "url": "http://127.0.0.1:54211/",
  "peers": [
    {"pid": 97394, "url": "http://127.0.0.1:54211/", "mode": "mcp-stdio", "working_dir": "/Users/me/projA", "is_self": true},
    {"pid": 97396, "url": "http://127.0.0.1:54218/", "mode": "mcp-stdio", "working_dir": "/Users/me/projB"}
  ]
}
```

So an agent that was launched **without** any monitor flag can still be observed on demand — call `monitor_info{enable:true}` and open the returned URL. From there the peers panel lets you hop between every running agent's dashboard. (Watch mode has no MCP tools, so its dashboard must be enabled at launch with `--monitor` / `--monitor-addr`.)

### Listing dashboards from the shell

The `monitors` subcommand prints every active instance's dashboard (reading the same registry, pruning any dead entries as it goes) — no MCP round-trip needed:

```sh
file-search-on monitors                 # table: mode / pid / age / dir / url
file-search-on monitors -o bare         # one URL per line
file-search-on monitors -o json         # machine-readable

# open the most-recently-started dashboard in a browser (macOS)
file-search-on monitors -o bare | tail -1 | xargs open
```

```
MODE       PID    AGE   DIR                     URL
mcp-stdio  97394  1m3s  /Users/me/projA         http://127.0.0.1:54211/
mcp-stdio  97396  58s   /Users/me/projB         http://127.0.0.1:54218/
watch      97401  12s   /Users/me/Desktop       http://127.0.0.1:54223/
```

## JSON API (scriptable)

The same data the UI renders is available as JSON — handy for scripts, smoke checks, or piping into `jq`:

```sh
PORT=9090   # the dynamic port from stderr / monitor_info when using --monitor
curl -s localhost:$PORT/api/overview      | jq          # version, uptime, mode, index backing
curl -s localhost:$PORT/api/cache         | jq .attr    # attribute-cache hit rate + counters
curl -s localhost:$PORT/api/activity      | jq '.snapshot.tools'   # per-tool latency
curl -s localhost:$PORT/api/capabilities  | jq '.content_types.total'
curl -s localhost:$PORT/api/peers         | jq '.peers' # other running instances
curl -s localhost:$PORT/healthz                          # liveness: {status, uptime_seconds, index_open}
```

The cache numbers match the `index_stats` MCP tool for the same running index — the dashboard is just a friendlier, pollable view of it.

## Security notes

- **Loopback only.** The dashboard can surface searched file paths (in the activity feed), so it never binds a routable interface. Passing `--monitor-addr 0.0.0.0:9090` still binds `127.0.0.1:9090` (with a one-line warning). For remote/container monitoring, front it with your own authenticated proxy or an SSH tunnel.
- **Read-only.** There are no mutation endpoints — the dashboard can't change the server's behaviour, only observe it.
- **Negligible overhead.** Tool-call instrumentation is a timestamp + a bounded ring-buffer append per call; the cache panel reads atomic counters. (In `mcp` mode the collector is always attached so the Activity panel has history if you enable the dashboard mid-session via `monitor_info` — that cost is a tiny per-call append, dwarfed by any filesystem walk.)
- **Registry is per-user.** Peer registration files live under your user cache dir and are readable only by you; they hold the dashboard URL, mode, PID, and working directory of each running instance.

## Related

- [`indexing.md`](indexing.md) — the `--index-path` attribute cache whose hit rates the Cache panel visualises.
- [`watch.md`](watch.md) — continuous watching, one of the two modes the dashboard attaches to.
