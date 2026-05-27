# Monitoring dashboard

The long-running modes — the `mcp` server and the `watch` command — can serve a small **read-only** monitoring dashboard so you can watch file-search-on's internal state while it runs: is the cache working, what is the agent doing right now, is OCR / embedding available.

Enable it with `--monitor-addr`. It's **off by default**, binds **127.0.0.1 only** (the host part of the address is ignored — only the port matters), has **no auth**, and adds **no dependencies** (the UI is a single embedded page polling a JSON API).

```sh
# stdio MCP server (the common agent setup) + dashboard
file-search-on mcp --monitor-addr :9090

# HTTP MCP server + dashboard on a separate port
file-search-on mcp --transport http --addr :8080 --monitor-addr :9090

# watch mode + dashboard
file-search-on watch 'is_image && body.contains("error")' --ocr -d ~/Desktop --monitor-addr :9090
```

Then open **http://localhost:9090/**.

## Panels

| Panel | Shows |
| --- | --- |
| **Overview** | version, uptime, run mode (`mcp-stdio` / `mcp-http` / `watch`), PID, Go version, GOMAXPROCS, default worker count, index backing (path or in-memory), body-cache cap, goroutines |
| **Cache** | attribute / body / embedding cache counters with derived **hit-rate %** + sparklines; body evictions / oversize rejects and embed model-mismatches highlighted |
| **Activity** | live MCP tool-call feed (tool, elapsed, outcome, result count), per-tool call/error/cancel counts + p50/p95/max latency, in-flight gauge |
| **Capabilities** | registered content types grouped by family, project types, OCR provider availability, embedder model/server + reachability |

In **watch mode** there are no MCP tool calls, so the Activity panel shows a notice; Overview / Cache / Capabilities still populate.

## JSON API (scriptable)

The same data the UI renders is available as JSON — handy for scripts, smoke checks, or piping into `jq`:

```sh
curl -s localhost:9090/api/overview      | jq          # version, uptime, mode, index backing
curl -s localhost:9090/api/cache         | jq .attr    # attribute-cache hit rate + counters
curl -s localhost:9090/api/activity      | jq '.snapshot.tools'   # per-tool latency
curl -s localhost:9090/api/capabilities  | jq '.content_types.total'
curl -s localhost:9090/healthz                          # liveness: {status, uptime_seconds, index_open}
```

The cache numbers match the `index_stats` MCP tool for the same running index — the dashboard is just a friendlier, pollable view of it.

## Security notes

- **Loopback only.** The dashboard can surface searched file paths (in the activity feed), so it never binds a routable interface. Passing `--monitor-addr 0.0.0.0:9090` still binds `127.0.0.1:9090` (with a one-line warning). For remote/container monitoring, front it with your own authenticated proxy or an SSH tunnel.
- **Read-only.** There are no mutation endpoints — the dashboard can't change the server's behaviour, only observe it.
- **Negligible overhead.** Tool-call instrumentation is a timestamp + a bounded ring-buffer append per call; the cache panel reads atomic counters. With `--monitor-addr` unset there is zero instrumentation.

## Related

- [`indexing.md`](indexing.md) — the `--index-path` attribute cache whose hit rates the Cache panel visualises.
- [`watch.md`](watch.md) — continuous watching, one of the two modes the dashboard attaches to.
